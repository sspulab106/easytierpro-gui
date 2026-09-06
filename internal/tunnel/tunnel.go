// Package tunnel manages cloudflared quick tunnels ("try cloudflare"):
// each tunnel spawns a local cloudflared process that exposes a target
// address (e.g. http://127.0.0.1:8080 or a tailnet virtual IP) on a random
// public https://<name>.trycloudflare.com URL. The public URL is parsed from
// the process output.
package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Status values.
const (
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusStopped  = "stopped"
	StatusFailed   = "failed"
)

// Tunnel providers ("kind").
const (
	KindCloudflared = "cloudflared" // quick tunnel via the cloudflared binary
	KindSSH         = "ssh"         // reverse tunnel via localhost.run + system ssh
)

// Tunnel is one managed tunnel process.
type Tunnel struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"` // cloudflared | ssh
	Target    string    `json:"target"`
	PublicURL string    `json:"public_url"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Created   time.Time `json:"created"`

	cmd *exec.Cmd
}

// Peer is one mesh device offered as a tunnel target in the UI picker
// (hostname + virtual IP; the user appends the service port).
type Peer struct {
	Network  string `json:"network"`
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

var urlRe = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.trycloudflare\.com`)

// sshURLRe matches localhost.run's anonymous-domain line, e.g.
// "02dcd05bb0978b.lhr.life tunneled with tls termination, https://02dcd05bb0978b.lhr.life".
var sshURLRe = regexp.MustCompile(`https://[a-zA-Z0-9-]+\.lhr\.life`)

// Manager owns all tunnels of this instance. Tunnels are process-bound and
// intentionally not persisted: they die with the app (temporary by design).
type Manager struct {
	mu      sync.Mutex
	seq     int
	binPath string
	items   map[string]*Tunnel
}

// NewManager creates a manager. binPath may be empty → resolved per start
// (PATH first, then the caller-provided default location).
func NewManager(binPath string) *Manager {
	return &Manager{binPath: binPath, items: map[string]*Tunnel{}}
}

// normalizeTarget repairs and validates a user-typed target: full-width
// punctuation from CJK input methods is converted (：→:, ／→/, ０-９→0-9),
// the scheme defaults to http://, and only schemes cloudflared quick
// tunnels actually accept are allowed.
func normalizeTarget(target string) (string, error) {
	t := strings.TrimSpace(target)
	t = strings.Map(func(r rune) rune {
		switch r {
		case '：': // U+FF1A full-width colon
			return ':'
		case '／': // U+FF0F full-width slash
			return '/'
		case '　': // U+3000 ideographic space
			return -1
		}
		if r >= 0xFF10 && r <= 0xFF19 { // full-width digits ０-９
			return r - 0xFF10 + '0'
		}
		return r
	}, t)
	t = strings.TrimSpace(t)
	if t == "" {
		return "", fmt.Errorf("target is empty")
	}
	if !strings.Contains(t, "://") {
		t = "http://" + t
	}
	scheme := t[:strings.Index(t, "://")]
	switch scheme {
	case "http", "https", "tcp", "ssh":
	default:
		return "", fmt.Errorf("unsupported scheme %q (use http://, https:// or tcp://)", scheme)
	}
	return t, nil
}

// Start launches a quick tunnel for target and returns it immediately; the
// public URL appears asynchronously once cloudflared registers it.
func (m *Manager) Start(target string) (*Tunnel, error) {
	bin := m.resolveBin()
	if bin == "" {
		return nil, fmt.Errorf("cloudflared not installed (use the install button or put it on PATH)")
	}
	norm, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, "tunnel", "--no-autoupdate", "--protocol", "http2", "--url", norm)
	hide(cmd)
	return m.launch(KindCloudflared, norm, cmd, urlRe, "cloudflared")
}

// StartSSH opens a localhost.run reverse tunnel with the system ssh client:
// `ssh -R 80:<host>:<port> nokey@localhost.run`. Free anonymous tier is
// HTTP(S) only (TLS terminated at the edge), so tcp:// targets are rejected.
func (m *Manager) StartSSH(target string) (*Tunnel, error) {
	bin := m.SSHPath()
	if bin == "" {
		return nil, fmt.Errorf("ssh client not found (Windows 10 1809+ bundles OpenSSH; otherwise install it)")
	}
	norm, err := normalizeTarget(target)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(norm, "tcp://") {
		return nil, fmt.Errorf("localhost.run free tier carries HTTP(S) traffic only — use the cloudflared provider for raw tcp:// targets")
	}
	hostport := strings.TrimPrefix(strings.TrimPrefix(norm, "https://"), "http://")

	cmd := exec.Command(bin,
		"-o", "BatchMode=yes",              // never hang on credential prompts (anonymous needs none)
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ExitOnForwardFailure=yes",   // fail fast instead of a silent no-forward session
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-T",                               // no pty: we only read the URL from output
		"-R", "80:"+hostport,
		"nokey@localhost.run")
	hide(cmd)
	return m.launch(KindSSH, norm, cmd, sshURLRe, "ssh")
}

// launch spawns the process, registers the record and starts the URL scan +
// exit watchers shared by both providers.
func (m *Manager) launch(kind, norm string, cmd *exec.Cmd, re *regexp.Regexp, label string) (*Tunnel, error) {
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", label, err)
	}
	assignJob(cmd)

	m.mu.Lock()
	m.seq++
	t := &Tunnel{
		ID:      fmt.Sprintf("t%d", m.seq),
		Kind:    kind,
		Target:  norm,
		Status:  StatusStarting,
		Created: time.Now(),
		cmd:     cmd,
	}
	m.items[t.ID] = t
	m.mu.Unlock()

	// The public URL may arrive on either stream.
	go m.scan(t, re, stdout)
	go m.scan(t, re, stderr)
	go m.watchExit(t, cmd, label)
	return t, nil
}

// watchExit marks the tunnel stopped/failed once its process ends.
func (m *Manager) watchExit(t *Tunnel, cmd *exec.Cmd, label string) {
	err := cmd.Wait()
	m.mu.Lock()
	cur, ok := m.items[t.ID]
	if ok {
		cur.cmd = nil
		if cur.Status != StatusRunning || err != nil {
			cur.Status = StatusFailed
		} else {
			cur.Status = StatusStopped
		}
		if err != nil && err.Error() != "exit status 0" {
			cur.Error = err.Error()
		}
	}
	m.mu.Unlock()
	_ = label
}

// scan watches one output stream for the provider's public URL.
func (m *Manager) scan(t *Tunnel, re *regexp.Regexp, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if u := re.FindString(sc.Text()); u != "" {
			m.mu.Lock()
			t.PublicURL = u
			t.Status = StatusRunning
			m.mu.Unlock()
			return
		}
	}
}

// Stop kills a tunnel process.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.items[id]
	if !ok {
		return fmt.Errorf("tunnel not found")
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	t.Status = StatusStopped
	return nil
}

// StopAll kills every running tunnel (app shutdown cleanup — a quick tunnel
// must not outlive the app that started it).
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, t := range m.items {
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
			t.Status = StatusStopped
		}
	}
}

// StartHealthWatch polls every running tunnel's public URL and flips the
// status to failed when the edge stops answering (e.g. Cloudflare 530 after
// a connector lost its transport) — the UI would otherwise show a dead
// tunnel as running forever. Origin-side errors (502/503/504) still count
// as "tunnel alive": only edge-level failures degrade the status.
func (m *Manager) StartHealthWatch(interval time.Duration) {
	go func() {
		for {
			time.Sleep(interval)
			m.healthCheck()
		}
	}()
}

func (m *Manager) healthCheck() {
	type target struct {
		id  string
		url string
	}
	m.mu.Lock()
	targets := make([]target, 0, len(m.items))
	for _, t := range m.items {
		if t.cmd != nil && t.Status == StatusRunning && t.PublicURL != "" {
			targets = append(targets, target{t.ID, t.PublicURL})
		}
	}
	m.mu.Unlock()

	for _, tg := range targets {
		client := &http.Client{Timeout: 12 * time.Second}
		resp, err := client.Get(tg.url)
		if err != nil {
			// Network error on OUR side (local proxy hiccup etc.) — cannot
			// judge the tunnel; leave the status alone.
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		m.mu.Lock()
		t, ok := m.items[tg.id]
		if !ok {
			m.mu.Unlock()
			continue
		}
		switch {
		case resp.StatusCode == 530:
			// Edge lost the connector (typical after a QUIC session dies
			// behind a fake-IP proxy): the URL is dead until restarted.
			if t.Status == StatusRunning {
				t.Status = StatusFailed
				t.Error = "edge unreachable (HTTP 530): connector lost its transport — restart the tunnel"
			}
		default:
			// 200/404/502/503... all prove the edge<->connector path works;
			// 502/503 are origin-side problems, not tunnel problems.
			if t.Status == StatusFailed && strings.HasPrefix(t.Error, "edge unreachable") {
				t.Status = StatusRunning
				t.Error = ""
			}
		}
		m.mu.Unlock()
	}
}

// ClearHistory drops all stopped/failed records (running tunnels untouched).
func (m *Manager) ClearHistory() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, t := range m.items {
		if t.Status == StatusStopped || t.Status == StatusFailed {
			delete(m.items, id)
			n++
		}
	}
	return n
}

// List returns tunnels newest first.
func (m *Manager) List() []Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Tunnel, 0, len(m.items))
	for _, t := range m.items {
		out = append(out, *t)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Created.After(out[j-1].Created); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// StopByTarget kills every tunnel pointing at target (used by fleet
// stop_tunnel commands); returns the number stopped.
func (m *Manager) StopByTarget(target string) int {
	norm, err := normalizeTarget(target)
	if err != nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.items {
		if t.Target == norm && t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
			t.Status = StatusStopped
			n++
		}
	}
	return n
}

// Retarget atomically re-opens a tunnel with a new target (or the same one,
// i.e. a restart): the old process is killed and a fresh one started. The
// old record is REPLACED by the new tunnel — a quick tunnel always gets a
// fresh public URL, and one user intent maps to one list entry.
func (m *Manager) Retarget(id, newTarget string) (*Tunnel, error) {
	m.mu.Lock()
	t, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("tunnel not found")
	}
	old := *t
	m.mu.Unlock()

	// Re-open with the SAME provider kind the record was created with.
	var nt *Tunnel
	var err error
	if old.Kind == KindSSH {
		nt, err = m.StartSSH(newTarget)
	} else {
		nt, err = m.Start(newTarget)
	}
	if err != nil {
		return nil, err
	}
	if old.cmd != nil && old.cmd.Process != nil {
		_ = old.cmd.Process.Kill()
	}
	m.mu.Lock()
	delete(m.items, id) // the new record supersedes the old one
	m.mu.Unlock()
	return nt, nil
}

// resolveBin finds an executable cloudflared: PATH first, then the bundled
// location under the app data runtime dir.
func (m *Manager) resolveBin() string {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p
	}
	if m.binPath != "" {
		if _, err := os.Stat(m.binPath); err == nil {
			return m.binPath
		}
	}
	return ""
}

// SSHPath locates the system ssh client used for localhost.run tunnels.
func (m *Manager) SSHPath() string {
	if p, err := exec.LookPath("ssh"); err == nil {
		return p
	}
	return ""
}

// BinInstalled reports whether an executable cloudflared is available.
func (m *Manager) BinInstalled() bool { return m.resolveBin() != "" }

// Download installs the official cloudflared binary (GitHub latest release)
// to dst. Caller picks the platform-correct URL.
func Download(url, dst string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("download %s -> HTTP %d", url, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
