package main

import (
	"archive/zip"
	"runtime/debug"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"easytier-pro-gui/internal/alerts"
	"easytier-pro-gui/internal/audit"
	"easytier-pro-gui/internal/auth"
	"easytier-pro-gui/internal/configmgr"
	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/coremgr"
	"easytier-pro-gui/internal/applog"
	"easytier-pro-gui/internal/devices"
	"easytier-pro-gui/internal/easytier"
	"easytier-pro-gui/internal/fleet"
	"easytier-pro-gui/internal/hostsfile"
	"easytier-pro-gui/internal/lease"
	"easytier-pro-gui/internal/settings"
	"easytier-pro-gui/internal/traffic"
	"easytier-pro-gui/internal/tunnel"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"easytier-pro-gui/internal/webcert"
	"easytier-pro-gui/internal/webserver"
)

// App is the root Wails-bound application object.
type App struct {
	ctx context.Context

	startHidden bool // launched with --minimized: stay in the tray

	procs    *core.Group
	cli      *easytier.Client
	config   *configmgr.Manager
	web      *webserver.Server
	settings *settings.Store

	mu         sync.Mutex
	lastLog    []string
	statusMu   sync.Mutex
	coreStatus string // "stopped" | "starting" | "running" | "error"

	verMu     sync.Mutex
	verCached easytier.Version

	traffic   *traffic.Store
	hosts     *hostsfile.Store
	accounts  *auth.AccountStore
	fleet     *fleet.Store
	audit     *audit.Store
	dnsOn     bool // MagicDNS block currently written to the hosts file
	alertMu   sync.Mutex
	alertPrev map[string]alerts.PeerState

	webdavMu sync.Mutex // serializes push/pull (manual + auto sync)
	settingsMu sync.Mutex // serializes fleet pin writes into settings

	// sticky-DHCP: last assigned virtual IP per network, re-applied on start
	leases   *lease.Store
	injectMu sync.Mutex
	injected map[string]string // network name -> injected CIDR (this start)
	injectAt time.Time

	tunnels *tunnel.Manager
	cores   *coremgr.Manager // downloaded easytier-core versions (lazy)

	// device admission list + leveled application log + quota state
	devices *devices.Store
	appLog  *applog.Logger
	devMu   sync.Mutex
	devSeen map[string]bool // unknown/denied peers already alerted for
	quotaMu sync.Mutex
	quotaYM string // "2006-01" the alert flags belong to
	quotaWarn, quotaExceed bool

	// eventSink delivers app events to the frontend. The GUI wires it to
	// Wails EventsEmit; headless mode leaves it unset (the web UI polls).
	eventSink func(event string, data any)
}

// SetEventSink registers the frontend event dispatcher (GUI mode only).
func (a *App) SetEventSink(fn func(event string, data any)) {
	a.eventSink = fn
}

// NewApp creates the application and wires up subsystems.
func NewApp() *App {
	a := &App{
		procs:      core.NewGroup(),
		config:     configmgr.NewManager(core.DefaultPaths.ConfigDir()),
		settings:   settings.NewStore(core.DefaultPaths.AppDataDir()),
		accounts:   auth.NewAccountStore(core.DefaultPaths.AppDataDir()),
		coreStatus: "stopped",
	}
	return a
}

// instanceRef identifies one running network instance for queries.
type instanceRef struct {
	ID     string
	Name   string
	Portal string
}

// runningInstances lists our currently-running instances with their rpc
// portals (portal assigned at process start).
func (a *App) runningInstances() []instanceRef {
	out := []instanceRef{}
	nets, err := a.config.List()
	if err != nil {
		return out
	}
	byID := map[string]string{}
	for _, c := range nets {
		byID[c.InstanceID] = c.Network
	}
	for _, id := range a.procs.RunningIDs() {
		name := byID[id]
		if name == "" {
			name = id
		}
		out = append(out, instanceRef{ID: id, Name: name, Portal: a.procs.PortalOf(id)})
	}
	return out
}

// queryAll runs fn against every running instance's rpc portal and merges
// the results into the grouped [{instance_id, instance_name, result}] shape
// the UI and helpers already parse. Falls back to the base portal when an
// external (service-mode) core is running instead of ours.
func (a *App) queryAll(fn func(*easytier.Client) (json.RawMessage, error)) (json.RawMessage, error) {
	insts := a.runningInstances()
	groups := []map[string]any{}
	if len(insts) == 0 {
		if core.PingRpc(core.RpcPortal) {
			insts = append(insts, instanceRef{ID: "external", Name: "external", Portal: core.RpcPortal})
		} else {
			return nil, fmt.Errorf("easytier-core is not running")
		}
	}
	for _, in := range insts {
		cli := easytier.New(core.DefaultPaths.CliPath(), in.Portal)
		raw, err := fn(cli)
		if err != nil {
			continue
		}
		groups = append(groups, map[string]any{"instance_id": in.ID, "instance_name": in.Name, "result": raw})
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("no running instance answered")
	}
	return json.Marshal(groups)
}

// mergedTrafficCounters aggregates per-network traffic counters across all
// running instances.
func (a *App) mergedTrafficCounters() (map[string]traffic.Counter, error) {
	raw, err := a.queryAll((*easytier.Client).QueryStats)
	if err != nil {
		return nil, err
	}
	var groups []struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	out := map[string]traffic.Counter{}
	for _, g := range groups {
		if counters, err := parseTrafficCounters(g.Result); err == nil {
			for net, c := range counters {
				acc := out[net]
				acc.Rx += c.Rx
				acc.Tx += c.Tx
				out[net] = acc
			}
		}
	}
	return out, nil
}

// Init starts every subsystem except the GUI (tray/window): resource
// extraction, settings, web server, background loops. Shared by the desktop
// app and the headless server binary.
func (a *App) Init() {
	// Extract bundled easytier binaries + drivers into app-data (first launch).
	if err := ensureEmbeddedResources(); err != nil {
		println("failed to extract embedded resources:", err.Error())
	}
	_ = core.DefaultPaths.ConfigDir() // ensure dirs exist

	// Watch public-tunnel liveness so a dead connector shows as failed
	// instead of running forever.
	a.tunnelManager().StartHealthWatch(30 * time.Second)

	// Apply persisted settings (paths, web bind) and restart the web server
	// so a user-configured bind address takes effect.
	if cfg, err := a.settings.Load(); err == nil {
		a.applySettings(cfg)
	} else {
		println("failed to load settings:", err.Error())
	}

	// Background sampling loop: traffic history + peer watchdog.
	a.traffic = traffic.NewStore(filepath.Join(core.DefaultPaths.AppDataDir(), "traffic"))
	a.leases = lease.NewStore(core.DefaultPaths.AppDataDir())
	a.hosts = hostsfile.NewStore()
	a.accounts = auth.NewAccountStore(core.DefaultPaths.AppDataDir())
	a.fleet = fleet.NewStore(core.DefaultPaths.AppDataDir())
	a.audit = audit.NewStore(core.DefaultPaths.AppDataDir())
	a.devices = devices.NewStore(core.DefaultPaths.AppDataDir())
	a.appLog = applog.New(filepath.Join(core.DefaultPaths.LogDir(), "app"), applog.LevelInfo)
	if a.web != nil {
		a.web.SetFleet(a.fleet)
	}
	a.goSafe("fleetLoop", a.fleetLoop)

	// One-time migration: the pre-multi-user owner account (settings fields)
	// becomes the first admin in the account store, so user lists and the
	// last-admin protection see it.
	if user, hash := a.WebAccount(); user != "" && hash != "" {
		if _, ok := a.accounts.Get(user); !ok {
			_ = a.accounts.Put(auth.Account{Username: user, PasswordHash: hash, Role: "admin"})
		}
	}
	go a.traffic.Prune(30)
	// Windows: remove stale EasyTier firewall rules once at startup (the
	// random per-restart adapter names used to accumulate 8 rules/restart).
	go func() {
		if n, err := firewallCleanup(); err == nil && n > 0 {
			println("firewall cleanup: removed", n, "stale rules")
			a.alog("firewall.cleanup", fmt.Sprintf("removed %d stale rules", n))
		}
	}()
	a.goSafe("backgroundLoop", a.backgroundLoop)

	// If a core is already running (e.g. service mode), reflect that.
	a.goSafe("initialCoreStatus", func() {
		time.Sleep(500 * time.Millisecond)
		a.refreshCoreStatus()
	})
}

// applySettings applies live-effect settings: custom paths, web bind, token.
func (a *App) applySettings(cfg settings.Settings) {
	// Persist the web token on first run so remote sessions stay stable.
	if a.web != nil && cfg.WebToken != "" {
		a.web.SetToken(cfg.WebToken)
	}
	// Core version override: point the core/cli at the downloaded release
	// chosen in settings ("" = bundled version).
	if cfg.CoreVersion != "" {
		dir := a.coreManager().VersionDir(cfg.CoreVersion)
		if _, err := os.Stat(filepath.Join(dir, coremgr.CoreBinaryName())); err == nil {
			core.DefaultPaths.SetCoreOverride(dir)
		}
	}
	if cfg.ConfigDir != "" {
		core.DefaultPaths.SetOverride("", "", "", cfg.ConfigDir, "")
		a.config = configmgr.NewManager(core.DefaultPaths.ConfigDir())
	}
	if cfg.LogDir != "" {
		core.DefaultPaths.SetOverride("", "", "", "", cfg.LogDir)
	}
	// Application log level follows settings (trace..error).
	if a.appLog != nil {
		a.appLog.SetLevel(applog.ParseLevel(cfg.AppLogLevel))
		a.appLog.Infof("app", "settings applied (log level %s)", cfg.AppLogLevel)
	}
	if a.web != nil {
		a.web.SetBind(cfg.WebBind, cfg.WebPort)
		if cert := a.webCertFor(cfg); cert != nil {
			a.web.SetTLSCert(cert)
		} else {
			a.web.SetTLSCert(nil)
		}
		if err := a.web.Restart(); err != nil {
			println("failed to restart web server with settings:", err.Error())
		}
		if cfg.WebToken == "" {
			cfg.WebToken = a.web.Token()
			_ = a.settings.Save(cfg)
		}
	}
}

// SetWebAssets configures the embedded web-management server with the built
// frontend files. Called from main() before wails.Run.
func (a *App) SetWebAssets(assets fs.FS) {
	a.web = webserver.NewServer(a, assets)
	if err := a.web.Start(); err != nil {
		println("failed to start web server:", err.Error())
	}
}

// WebInfo exposes the web-management endpoint to the UI.
func (a *App) WebInfo() map[string]string {
	if a.web == nil {
		return map[string]string{"running": "false"}
	}
	return map[string]string{
		"running": "true",
		"addr":    a.web.Addr(),
		"token":   a.web.Token(),
	}
}

// ---- settings ----

// GetSettings returns the persisted settings as JSON.
func (a *App) GetSettings() (string, error) {
	cfg, err := a.settings.Load()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveSettings persists settings and applies live effects (web bind, paths).
func (a *App) SaveSettings(raw string) error {
	a.alog("settings.update", "")
	var cfg settings.Settings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fmt.Errorf("invalid settings JSON: %w", err)
	}
	old, _ := a.settings.Load()
	configDirChanged := cfg.ConfigDir != "" && cfg.ConfigDir != old.ConfigDir
	if err := a.settings.Save(cfg); err != nil {
		return err
	}
	a.applySettings(cfg)
	// Autostart toggle applies immediately to the OS (registry / XDG / launchd).
	if cfg.AutoStart != old.AutoStart {
		if err := setAutoStart(cfg.AutoStart); err != nil {
			return fmt.Errorf("failed to update autostart: %w", err)
		}
	}
	// A new config-dir only takes effect on the next core start.
	if configDirChanged && a.procs.Any() {
		return a.RestartCore()
	}
	return nil
}

// ---- web account (management-plane auth) ----

// WebAccount returns the web admin username and password hash for the
// embedded web server.
func (a *App) WebAccount() (string, string) {
	cfg, err := a.settings.Load()
	if err != nil {
		return "", ""
	}
	return cfg.WebUsername, cfg.WebPasswordHash
}

// HasWebAccount reports whether a web admin account has been configured.
func (a *App) HasWebAccount() bool {
	user, hash := a.WebAccount()
	return user != "" && hash != ""
}

// SetWebAccount persists the web admin username and password hash.
func (a *App) SetWebAccount(username, passwordHash string) error {
	cfg, err := a.settings.Load()
	if err != nil {
		return err
	}
	cfg.WebUsername = username
	cfg.WebPasswordHash = passwordHash
	return a.settings.Save(cfg)
}

// SetWebToken persists a rotated API access token.
func (a *App) SetWebToken(token string) error {
	cfg, err := a.settings.Load()
	if err != nil {
		return err
	}
	cfg.WebToken = token
	return a.settings.Save(cfg)
}

// SetWebAccountPassword creates or changes the web admin account from the
// desktop UI (native path; the web path goes through /api/auth/password).
func (a *App) SetWebAccountPassword(current, next, username string) error {
	user, hash := a.WebAccount()
	if user != "" && hash != "" && !auth.VerifyPassword(current, hash) {
		return fmt.Errorf("current password wrong")
	}
	if len(next) < 6 {
		return fmt.Errorf("password too short (min 6)")
	}
	name := strings.TrimSpace(username)
	if name == "" {
		name = user
	}
	if name == "" {
		return fmt.Errorf("username required")
	}
	newHash, err := auth.HashPassword(next)
	if err != nil {
		return err
	}
	return a.SetWebAccount(name, newHash)
}

// RotateWebToken replaces the API access token and applies it live.
func (a *App) RotateWebToken() (string, error) {
	tok := auth.NewToken()
	if err := a.SetWebToken(tok); err != nil {
		return "", err
	}
	if a.web != nil {
		a.web.SetToken(tok)
	}
	return tok, nil
}

// ---- web accounts (multi-user) ----

// Authenticate verifies credentials against the account store, falling back
// to the legacy owner account from settings (upgraded to admin on first
// password change).
func (a *App) Authenticate(username, password string) (auth.Account, bool) {
	if a.accounts == nil {
		return auth.Account{}, false
	}
	if acc, ok := a.accounts.Authenticate(username, password); ok {
		return acc, true
	}
	user, hash := a.WebAccount()
	if user != "" && username == user && auth.VerifyPassword(password, hash) {
		return auth.Account{Username: user, Role: "admin"}, true
	}
	return auth.Account{}, false
}

// Accounts lists all web accounts (hashes included; the web layer strips them).
func (a *App) Accounts() []auth.Account {
	if a.accounts == nil {
		return nil
	}
	return a.accounts.List()
}

// SaveAccount creates or updates a web account.
func (a *App) SaveAccount(acc auth.Account, password string) error {
	if a.accounts == nil {
		return fmt.Errorf("account store not initialized")
	}
	return a.accounts.Upsert(acc, password)
}

// DeleteAccount removes a web account (never the last remaining one).
func (a *App) DeleteAccount(username string) error {
	if a.accounts == nil {
		return fmt.Errorf("account store not initialized")
	}
	return a.accounts.Delete(username)
}

// ---- web accounts: native bindings for the Users/Devices pages ----

// ListUsers returns all accounts without password hashes.
func (a *App) ListUsers() ([]map[string]any, error) {
	out := []map[string]any{}
	for _, acc := range a.Accounts() {
		out = append(out, map[string]any{
			"username": acc.Username, "role": acc.Role,
			"networks": acc.Networks, "created": acc.Created,
		})
	}
	return out, nil
}

// SaveUser creates or updates an account (native GUI path; admin only by
// convention — the desktop app is the trusted owner).
func (a *App) SaveUser(username, password, role string, networks []string) error {
	if role != "admin" && role != "viewer" {
		role = "viewer"
	}
	return a.SaveAccount(auth.Account{
		Username: strings.TrimSpace(username), Role: role, Networks: networks,
	}, password)
}

// DeleteUser removes an account and kicks its sessions.
func (a *App) DeleteUser(username string) error {
	if err := a.DeleteAccount(username); err != nil {
		return err
	}
	if a.web != nil {
		a.web.KickUser(username)
	}
	return nil
}

// ListSessions returns active web logins (native GUI sees all devices).
func (a *App) ListSessions() ([]map[string]any, error) {
	out := []map[string]any{}
	if a.web == nil {
		return out, nil
	}
	for _, sess := range a.web.SessionsList() {
		out = append(out, map[string]any{
			"id": sess.ID, "user": sess.User, "role": sess.Role,
			"ip": sess.IP, "ua": sess.UA,
			"created":   sess.Created.UTC().Format(time.RFC3339),
			"last_seen": sess.LastSeen.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

// RevokeSession kicks one device login.
func (a *App) RevokeSession(id string) error {
	if a.web == nil {
		return nil
	}
	return a.web.SessionRevoke(id)
}

// RevokeOtherSessions kicks every web login (native GUI owns the desktop).
func (a *App) RevokeOtherSessions() (int, error) {
	if a.web == nil {
		return 0, nil
	}
	return a.web.SessionsRevokeAllExcept(""), nil
}

// SetAccountPassword changes one account's password after verifying the
// current one. When no account exists yet (first-time setup) the very first
// account is created without a current password, as role admin. Legacy owner
// accounts migrate into the store on first change.
func (a *App) SetAccountPassword(username, current, next string) error {
	if a.accounts == nil {
		return fmt.Errorf("account store not initialized")
	}
	if acc, ok := a.accounts.Get(username); ok {
		if !auth.VerifyPassword(current, acc.PasswordHash) {
			return fmt.Errorf("current password wrong")
		}
		return a.accounts.Upsert(acc, next)
	}
	// First-time setup: no store accounts and no legacy owner yet.
	user, hash := a.WebAccount()
	if len(a.accounts.List()) == 0 && user == "" {
		return a.accounts.Upsert(auth.Account{Username: username, Role: "admin"}, next)
	}
	if username == user && auth.VerifyPassword(current, hash) {
		return a.accounts.Upsert(auth.Account{Username: username, Role: "admin"}, next)
	}
	return auth.ErrNotFound
}

// OpenDir opens a folder in the system file manager.
func (a *App) OpenDir(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ---- WebDAV cloud sync ----

// webdavFilePath returns the remote file name inside the WebDAV folder.
const webdavFileName = "easytier-pro-backup.zip"

// cloudflared binary locations for quick tunnels (per-platform download URL).
var cloudflaredBinName = map[string]string{
	"windows": "cloudflared.exe",
	"linux":   "cloudflared",
	"darwin":  "cloudflared",
}

var cloudflaredDownloadURL = map[string]string{
	"windows": "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe",
	"linux":   "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64",
	"darwin":  "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-darwin-amd64.tgz",
}

func (a *App) tunnelManager() *tunnel.Manager {
	if a.tunnels == nil {
		a.tunnels = tunnel.NewManager(a.cloudflaredPath())
	}
	return a.tunnels
}

func (a *App) cloudflaredPath() string {
	name := cloudflaredBinName[runtime.GOOS]
	if name == "" {
		return ""
	}
	return filepath.Join(core.DefaultPaths.AppDataDir(), "runtime", "cloudflared", name)
}

// CloudflaredStatus reports whether the binary is available and where.
func (a *App) CloudflaredStatus() map[string]string {
	m := a.tunnelManager()
	if m.BinInstalled() {
		return map[string]string{"installed": "true", "path": a.cloudflaredPath()}
	}
	return map[string]string{
		"installed": "false",
		"path":      a.cloudflaredPath(), // shown as the install destination in the UI
		"url":       cloudflaredDownloadURL[runtime.GOOS],
	}
}

// TunnelPeers lists devices currently reachable in the mesh (hostname +
// virtual IP), for the public-tunnel target picker.
func (a *App) TunnelPeers() []tunnel.Peer {
	configs, err := a.config.List()
	if err != nil {
		return []tunnel.Peer{}
	}
	out := []tunnel.Peer{}
	for net, peers := range a.trayPeersByNetwork(configs) {
		for _, p := range peers {
			out = append(out, tunnel.Peer{Network: net, Hostname: p.Hostname, IP: p.IP})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Network != out[j].Network {
			return out[i].Network < out[j].Network
		}
		return out[i].Hostname < out[j].Hostname
	})
	return out
}

// InstallCloudflared downloads the official binary into the app runtime dir.
func (a *App) InstallCloudflared() error {
	url := cloudflaredDownloadURL[runtime.GOOS]
	if url == "" {
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
	if runtime.GOOS == "darwin" {
		return fmt.Errorf("macOS: please install cloudflared via brew and put it on PATH")
	}
	return tunnel.Download(url, a.cloudflaredPath())
}

// ListTunnels returns all tunnels of this instance.
func (a *App) ListTunnels() ([]tunnel.Tunnel, error) { return a.tunnelManager().List(), nil }

// StartTunnel exposes a target address on a public trycloudflare URL.
func (a *App) StartTunnel(target string) (tunnel.Tunnel, error) {
	a.alog("tunnel.start", target)
	t, err := a.tunnelManager().Start(target)
	if err != nil {
		return tunnel.Tunnel{}, err
	}
	return *t, nil
}

// StartSSHTunnel exposes a target via a localhost.run reverse SSH tunnel
// (system ssh client, anonymous free tier, no extra binary needed).
func (a *App) StartSSHTunnel(target string) (tunnel.Tunnel, error) {
	a.alog("tunnel.start_ssh", target)
	t, err := a.tunnelManager().StartSSH(target)
	if err != nil {
		return tunnel.Tunnel{}, err
	}
	return *t, nil
}

// SSHStatus reports whether the system ssh client (localhost.run provider)
// is available and where.
func (a *App) SSHStatus() map[string]string {
	if p := a.tunnelManager().SSHPath(); p != "" {
		return map[string]string{"installed": "true", "path": p}
	}
	return map[string]string{"installed": "false"}
}

// StopTunnel kills one tunnel by id (the record is kept as history).
func (a *App) StopTunnel(id string) error {
	a.alog("tunnel.stop", id)
	return a.tunnelManager().Stop(id)
}

// RetargetTunnel re-creates a tunnel with a corrected target: the old
// process is replaced and a fresh public URL is assigned.
func (a *App) RetargetTunnel(id, target string) (tunnel.Tunnel, error) {
	a.alog("tunnel.retarget", id+" -> "+target)
	nt, err := a.tunnelManager().Retarget(id, target)
	if err != nil {
		return tunnel.Tunnel{}, err
	}
	return *nt, nil
}

// ClearTunnelHistory removes stopped/failed tunnel records.
func (a *App) ClearTunnelHistory() (int, error) {
	n := a.tunnelManager().ClearHistory()
	a.alog("tunnel.clear_history", fmt.Sprintf("%d records", n))
	return n, nil
}

// ---- WebDAV sync: per-device fields never leave the machine ----
//
// The archive carries everything EXCEPT machine-local state: virtual IPs
// (user-set or sticky-DHCP injected), the DHCP flag and the device hostname
// inside config TOMLs, plus per-device application settings (paths, ports,
// tokens, autostart, MagicDNS, WebDAV credentials). On restore, local
// per-device values are re-applied by instance id so a device keeps its own
// identity while everything else follows the cloud. The TOML sanitization
// helpers live in internal/configmgr (shared with fleet remote commands).

// sharedSettings returns the settings subset that travels between devices:
// appearance (custom CSS) and alert rules. Everything else — bind address,
// port, API token, accounts, paths, autostart, MagicDNS, WebDAV credentials,
// sticky-DHCP leases — is machine-local and excluded.
// sharedSettings marshals only the cross-device settings subset (custom CSS
// and alert rules) — an explicit projection so nothing else can leak.
func sharedSettingsJSON(src settings.Settings) ([]byte, error) {
	return json.MarshalIndent(map[string]any{
		"custom_css": src.CustomCSS,
		"alerts":     src.Alerts,
	}, "", "  ")
}

// webdavArchive builds an in-memory zip of all configs + settings.json,
// sanitized for cross-device travel (see the per-device notes above).
func (a *App) webdavArchive() ([]byte, error) {
	configs, err := a.config.List()
	if err != nil {
		return nil, err
	}
	cur, err := a.settings.Load()
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	write := func(name, content string) error {
		w, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(w, content)
		return err
	}

	for _, cfg := range configs {
		state := ".toml"
		if !cfg.Enabled {
			state = ".toml.disabled"
		}
		name := filepath.Join("configs", cfg.InstanceID+state)
		if err := write(name, configmgr.SharedTOML(cfg.Raw)); err != nil {
			return nil, err
		}
	}
	sj, err := sharedSettingsJSON(cur)
	if err != nil {
		return nil, err
	}
	if err := write("settings.json", string(sj)); err != nil {
		return nil, err
	}
	// Multi-device identity (opt-in): the account database travels with the
	// backup; restore merges per username with newest-updated wins.
	if cur.Webdav.SyncAccounts && a.accounts != nil {
		accs, _ := json.MarshalIndent(a.accounts.List(), "", "  ")
		if err := write("web-accounts.json", string(accs)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// webdavHTTP performs an authenticated HTTP request against the WebDAV server.
func webdavHTTP(cfg settings.WebdavConfig, method, path string, body []byte) ([]byte, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("WebDAV server URL not configured")
	}
	url := strings.TrimRight(cfg.ServerURL, "/") + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("WebDAV request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("WebDAV %s %s -> HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// WebdavPush uploads all configs + settings as a zip to the WebDAV folder.
func (a *App) WebdavPush() (string, error) {
	a.webdavMu.Lock()
	defer a.webdavMu.Unlock()
	cfg, err := a.settings.Load()
	if err != nil {
		return "", err
	}
	if cfg.Webdav.ServerURL == "" {
		return "", fmt.Errorf("WebDAV server URL not configured in Settings")
	}
	archive, err := a.webdavArchive()
	if err != nil {
		return "", err
	}
	if _, err := webdavHTTP(cfg.Webdav, http.MethodPut, webdavFileName, archive); err != nil {
		return "", err
	}
	cfg.Webdav.LastSync = time.Now().Format(time.RFC3339)
	_ = a.settings.Save(cfg)
	return "uploaded", nil
}

// WebdavPull downloads the backup zip and restores configs + settings.
func (a *App) WebdavPull() (string, error) {
	a.webdavMu.Lock()
	defer a.webdavMu.Unlock()
	cfg, err := a.settings.Load()
	if err != nil {
		return "", err
	}
	if cfg.Webdav.ServerURL == "" {
		return "", fmt.Errorf("WebDAV server URL not configured in Settings")
	}
	data, err := webdavHTTP(cfg.Webdav, http.MethodGet, webdavFileName, nil)
	if err != nil {
		return "", err
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("invalid backup archive: %w", err)
	}

	restored := 0
	var remoteSettings settings.Settings
	haveSettings := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}

		name := filepath.ToSlash(f.Name)
		switch {
		case name == "web-accounts.json":
			if cfg.Webdav.SyncAccounts && a.accounts != nil {
				var remote []auth.Account
				if json.Unmarshal(content, &remote) == nil && len(remote) > 0 {
					a.accounts.MergeAccounts(remote)
				}
			}
		case name == "settings.json":
			if json.Unmarshal(content, &remoteSettings) == nil {
				haveSettings = true
			}
		case strings.HasPrefix(name, "configs/"):
			fileName := filepath.Base(name)
			if strings.HasSuffix(fileName, ".toml") || strings.HasSuffix(fileName, ".toml.disabled") {
				enabled := !strings.HasSuffix(fileName, ".disabled")
				// Defensive: never trust per-device fields from the cloud.
				cloud := configmgr.SharedTOML(string(content))
				iid := configmgr.InstanceID(cloud)
				// Keep THIS device's identity: its own IP/DHCP/hostname.
				if iid != "" {
					if local, err := a.config.Get(iid); err == nil {
						cloud = configmgr.ReapplyDeviceFields(cloud, configmgr.ExtractDeviceFields(local.Raw))
					}
				}
				cfgFile := configmgr.ConfigFile{InstanceID: iid, Raw: cloud, Enabled: enabled}
				if err := a.config.Save(cfgFile); err == nil {
					restored++
				}
			}
		}
	}

	// Merge only the shared settings subset (custom CSS, alert rules); all
	// per-device settings (ports, tokens, paths, WebDAV credentials, …)
	// stay exactly as they are on this machine.
	if haveSettings {
		local, err := a.settings.Load()
		if err == nil {
			local.CustomCSS = remoteSettings.CustomCSS
			local.Alerts = remoteSettings.Alerts
			_ = a.settings.Save(local)
			a.applySettings(local)
		}
	}
	return fmt.Sprintf("restored %d configs", restored), nil
}

// ---- web TLS (self-signed certificate) ----

// webCertDir is where the self-signed web certificate lives.
func webCertDir() string { return filepath.Join(core.DefaultPaths.AppDataDir(), "webcert") }

// webCertFor returns the TLS certificate when the settings enable HTTPS,
// generating the self-signed pair on first use. Never returns an error:
// HTTPS silently falls back to plain HTTP when cert management fails.
func (a *App) webCertFor(cfg settings.Settings) *tls.Certificate {
	if !cfg.WebHTTPS {
		return nil
	}
	pair, err := webcert.Ensure(webCertDir(), "")
	if err != nil {
		println("webcert:", err.Error())
		return nil
	}
	cert := pair.Cert
	return &cert
}

// WebTLSStatus reports the HTTPS mode and certificate fingerprint.
func (a *App) WebTLSStatus() map[string]string {
	cfg, err := a.settings.Load()
	if err != nil {
		return map[string]string{"enabled": "false"}
	}
	out := map[string]string{"enabled": fmt.Sprintf("%t", cfg.WebHTTPS)}
	pair, err := webcert.Ensure(webCertDir(), "")
	if err == nil {
		out["fingerprint"] = pair.Fingerprint
		out["cert_path"] = pair.CertPath
	}
	return out
}

// RotateWebTLSCert deletes and re-mints the self-signed pair, restarting the
// web server. Agents that pinned the old fingerprint fail closed until their
// pin is cleared (ClearFleetPin) — an intentional anti-impersonation trade.
func (a *App) RotateWebTLSCert() error {
	webcert.Remove(webCertDir())
	if _, err := webcert.Ensure(webCertDir(), ""); err != nil {
		return err
	}
	a.alog("web.tls_rotate", "")
	cfg, err := a.settings.Load()
	if err != nil {
		return err
	}
	if a.web != nil {
		a.web.SetTLSCert(a.webCertFor(cfg))
		if err := a.web.Restart(); err != nil {
			return err
		}
	}
	return nil
}

// fleetClient builds an HTTP client for hub communication. Plain HTTP keeps
// system defaults; HTTPS pins the hub's self-signed leaf certificate
// fingerprint: captured on first successful connect (trust on first use),
// enforced on every later call — an impostor cannot slide in between.
func (a *App) fleetClient(base string) *http.Client {
	if !strings.HasPrefix(base, "https://") {
		return &http.Client{Timeout: 10 * time.Second}
	}
	pinned := ""
	if cfg, err := a.settings.Load(); err == nil {
		pinned = cfg.Fleet.CertPin
	}
	tcfg := &tls.Config{
		// CA validation is replaced by explicit fingerprint pinning below.
		InsecureSkipVerify: true,
	}
	tcfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("hub presented no certificate")
		}
		fp := webcert.Fingerprint(rawCerts[0])
		if pinned == "" {
			// first use: remember this server as the trusted one
			a.fleetStorePin(fp)
			return nil
		}
		if !strings.EqualFold(fp, pinned) {
			return fmt.Errorf("hub certificate fingerprint changed (expected %s, got %s) — if you rotated the hub certificate, clear the pin on this device", pinned[:16], fp[:16])
		}
		return nil
	}
	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{TLSClientConfig: tcfg},
	}
}

// fleetStorePin persists the hub certificate fingerprint (TOFU capture).
func (a *App) fleetStorePin(fingerprint string) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	cfg, err := a.settings.Load()
	if err != nil || cfg.Fleet.CertPin == fingerprint {
		return
	}
	cfg.Fleet.CertPin = fingerprint
	_ = a.settings.Save(cfg)
}

// ClearFleetPin forgets the pinned hub certificate (after an intentional
// hub certificate rotation) so the next connect re-captures it.
func (a *App) ClearFleetPin() error {
	a.fleetStorePin("")
	a.alog("fleet.clear_pin", "")
	return nil
}

// fleetSelfEnroll logs in to the hub with an admin account and enrolls this
// device as an agent (returns the agent token).
func (a *App) fleetSelfEnroll(hubURL, user, pass, name string) (string, error) {
	client := a.fleetClient(hubURL)
	lb, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	lr, err := client.Post(hubURL+"/api/auth/login", "application/json", bytes.NewReader(lb))
	if err != nil {
		return "", err
	}
	defer lr.Body.Close()
	var lresp struct {
		Session string `json:"session"`
		User    string `json:"username"`
	}
	if err := json.NewDecoder(lr.Body).Decode(&lresp); err != nil || lr.StatusCode >= 300 {
		return "", fmt.Errorf("hub login failed (HTTP %d)", lr.StatusCode)
	}
	eb, _ := json.Marshal(map[string]string{"name": name})
	req, _ := http.NewRequest(http.MethodPost, hubURL+"/api/fleet/self-enroll", bytes.NewReader(eb))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Session", lresp.Session)
	er, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer er.Body.Close()
	var eresp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(er.Body).Decode(&eresp); err != nil || er.StatusCode >= 300 {
		return "", fmt.Errorf("self-enroll failed (HTTP %d)", er.StatusCode)
	}
	if eresp.Token == "" {
		return "", fmt.Errorf("hub returned empty token")
	}
	return eresp.Token, nil
}

// ---- WebDAV periodic auto-sync ----

// webdavLoop periodically pulls (merge) and pushes the backup when auto-sync
// is enabled. Pull-before-push so remote account/config changes are merged
// locally before the merged state travels back to the cloud.
func (a *App) webdavLoop() {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		a.webdavAutoSync()
	}
}

func (a *App) webdavAutoSync() {
	cfg, err := a.settings.Load()
	if err != nil || !cfg.Webdav.AutoSync || cfg.Webdav.ServerURL == "" {
		return
	}
	a.webdavMu.Lock()
	defer a.webdavMu.Unlock()
	msg := ""
	if _, err := a.WebdavPull(); err != nil {
		a.alog("webdav.autosync", "pull failed: "+err.Error())
		return
	}
	if _, err := a.WebdavPush(); err != nil {
		a.alog("webdav.autosync", "push failed: "+err.Error())
		return
	}
	msg = "ok"
	a.alog("webdav.autosync", msg)
}

// ---- fleet agent: heartbeat to the management hub + execute commands ----

// fleetLoop runs while the app is alive: report state to the hub, pull and
// execute pending commands (join/leave network, start/stop core). With the
// hub down the loop just retries; local behavior never depends on it.
func (a *App) fleetLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.fleetBeat()
	}
}

func (a *App) fleetBeat() {
	cfg, err := a.settings.Load()
	if err != nil || !cfg.Fleet.Enabled || cfg.Fleet.ServerURL == "" || cfg.Fleet.Token == "" {
		return
	}
	base := strings.TrimRight(cfg.Fleet.ServerURL, "/")
	name := strings.TrimSpace(cfg.Fleet.Name)
	if name == "" {
		name = a.localHostname()
	}

	// Account-based self-enrollment: no manual token copy needed. The agent
	// signs in to the hub with a (same-account) admin login once and enrolls
	// itself; the returned agent token is persisted for all future beats.
	if cfg.Fleet.Token == "" && cfg.Fleet.HubUser != "" && cfg.Fleet.HubPass != "" {
		if tok, err := a.fleetSelfEnroll(base, cfg.Fleet.HubUser, cfg.Fleet.HubPass, name); err != nil {
			a.alog("fleet.self_enroll.fail", err.Error())
		} else {
			cfg.Fleet.Token = tok
			_ = a.settings.Save(cfg)
			a.alog("fleet.self_enroll", "enrolled as "+name)
			return // enroll this cycle; heartbeat resumes next tick
		}
	}

	// Report this device's state: version, virtual IPs, networks, autostart.
	var nets []string
	if list, lerr := a.config.List(); lerr == nil {
		for _, c := range list {
			if c.Enabled {
				nets = append(nets, c.Network)
			}
		}
	}
	hb := fleet.Heartbeat{
		Name:      name,
		OS:        runtime.GOOS + "/" + runtime.GOARCH,
		Version:   a.Versions().Core,
		IPv4:      strings.Join(a.localVirtualIPs(), " "),
		Networks:  nets,
		AutoStart: getAutoStart(),
	}
	body, _ := json.Marshal(hb)
	req, err := http.NewRequest(http.MethodPost, base+"/api/agent/heartbeat", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", cfg.Fleet.Token)
	client := a.fleetClient(base)
	resp, err := client.Do(req)
	if err != nil {
		return // hub unreachable; try again next tick
	}
	var pending []fleet.Command
	_ = json.NewDecoder(resp.Body).Decode(&pending)
	resp.Body.Close()

	for _, cmd := range pending {
		a.fleetExecute(cmd, base, cfg.Fleet.Token)
	}
}

// fleetExecute performs one remote command and reports the outcome.
func (a *App) fleetExecute(cmd fleet.Command, hubURL, token string) {
	var status, result string
	switch cmd.Action {
	case fleet.ActionStartNetwork, fleet.ActionStopNetwork:
		running := cmd.Action == fleet.ActionStartNetwork
		id := cmd.NetworkID
		if id == "" && cmd.NetworkTOML != "" {
			id = configmgr.InstanceID(cmd.NetworkTOML)
		}
		var err error
		if cmd.NetworkTOML != "" {
			// Install/refresh the shared config, preserving this device's
			// own IP/DHCP/hostname when the instance already exists locally.
			shared := configmgr.SharedTOML(cmd.NetworkTOML)
			if iid := configmgr.InstanceID(shared); iid != "" {
				if local, gerr := a.config.Get(iid); gerr == nil {
					shared = configmgr.ReapplyDeviceFields(shared, configmgr.ExtractDeviceFields(local.Raw))
				}
			}
			err = a.config.Save(configmgr.ConfigFile{InstanceID: configmgr.InstanceID(shared), Raw: shared, Enabled: running})
		} else {
			err = a.config.SetEnabled(id, running)
		}
		if err == nil {
			err = a.SetNetworkRunning(id, running)
		}
		if err != nil {
			status, result = fleet.StatusFailed, err.Error()
		} else {
			status, result = fleet.StatusDone, "ok"
		}
	case fleet.ActionDeleteNetwork:
		id := cmd.NetworkID
		if id == "" && cmd.NetworkTOML != "" {
			id = configmgr.InstanceID(cmd.NetworkTOML)
		}
		_ = a.SetNetworkRunning(id, false) // stop first if running
		if err := a.config.Delete(id); err != nil {
			status, result = fleet.StatusFailed, err.Error()
		} else {
			status, result = fleet.StatusDone, "deleted"
		}
	case fleet.ActionStartCore:
		if err := a.StartCore(); err != nil {
			status, result = fleet.StatusFailed, err.Error()
		} else {
			status, result = fleet.StatusDone, "ok"
		}
	case fleet.ActionStopCore:
		if err := a.StopCore(); err != nil {
			status, result = fleet.StatusFailed, err.Error()
		} else {
			status, result = fleet.StatusDone, "ok"
		}
	case fleet.ActionStartTunnel:
		t, err := a.tunnelManager().Start(cmd.Target)
		if err != nil {
			status, result = fleet.StatusFailed, err.Error()
		} else {
			status = fleet.StatusDone
			for i := 0; i < 15 && t.PublicURL == ""; i++ {
				time.Sleep(1 * time.Second)
			}
			result = t.PublicURL
			if result == "" {
				result = "tunnel starting, URL pending"
			}
		}
	case fleet.ActionStopTunnel:
		n := a.tunnelManager().StopByTarget(cmd.Target)
		status, result = fleet.StatusDone, fmt.Sprintf("stopped %d tunnel(s)", n)
	default:
		status, result = fleet.StatusFailed, "unknown action: "+cmd.Action
	}

	body, _ := json.Marshal(map[string]string{"id": cmd.ID, "status": status, "result": result})
	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/agent/result", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-Token", token)
	client := a.fleetClient(hubURL)
	_, _ = client.Do(req)
}

// localVirtualIPs lists this device's current virtual IPv4 addresses.
func (a *App) localVirtualIPs() []string {
	var out []string
	for _, ni := range a.nodeInstances() {
		if ni.IPv4 != "" && ni.IPv4 != "DHCP" {
			out = append(out, ni.IPv4)
		}
	}
	return out
}

// localHostname returns this machine's hostname for fleet display.
func (a *App) localHostname() string {
	if ni := a.nodeInstances(); len(ni) > 0 && ni[0].Hostname != "" {
		return ni[0].Hostname
	}
	h, _ := os.Hostname()
	return h
}

// ---- fleet management surface (hub side, proxied to the web UI) ----

// ListAgents returns all enrolled devices.
func (a *App) ListAgents() ([]fleet.Agent, error) {
	if a.fleet == nil {
		return nil, nil
	}
	return a.fleet.Agents(), nil
}

// AgentEnrollment is the CreateAgent result: the raw token is shown once.
type AgentEnrollment struct {
	Agent fleet.Agent `json:"agent"`
	Token string      `json:"token"`
}

// CreateAgent enrolls a device and returns its one-time token.
func (a *App) CreateAgent(name string) (AgentEnrollment, error) {
	if a.fleet == nil {
		a.fleet = fleet.NewStore(core.DefaultPaths.AppDataDir())
	}
	agent, token, err := a.fleet.CreateAgent(name)
	return AgentEnrollment{Agent: agent, Token: token}, err
}

// DeleteAgent removes an enrolled device.
func (a *App) DeleteAgent(id string) error {
	a.alog("fleet.agent_delete", id)
	if a.fleet == nil {
		return nil
	}
	return a.fleet.DeleteAgent(id)
}

// AgentCommand queues a remote command (network join/leave, core, tunnels)
// for one device.
func (a *App) AgentCommand(agentID, action, networkID, networkTOML, target string) (fleet.Command, error) {
	if a.fleet == nil {
		return fleet.Command{}, fmt.Errorf("fleet not initialized")
	}
	return a.fleet.Enqueue(fleet.Command{
		AgentID: agentID, Action: action, NetworkID: networkID, NetworkTOML: networkTOML, Target: target,
	})
}

// AgentCommands lists recent commands.
func (a *App) AgentCommands() ([]fleet.Command, error) {
	if a.fleet == nil {
		return nil, nil
	}
	return a.fleet.Commands(), nil
}

// ---- audit ----

// AuditLog records one audit entry (called by the web layer with the session
// actor, and internally with actor "app").
func (a *App) AuditLog(actor, ip, event, detail string) {
	if a.audit == nil {
		a.audit = audit.NewStore(core.DefaultPaths.AppDataDir())
	}
	a.audit.Log(actor, ip, event, detail)
}

// AuditTail returns the newest audit entries (admin web view).
func (a *App) AuditTail(limit int) ([]audit.Entry, error) {
	if a.audit == nil {
		return []audit.Entry{}, nil
	}
	return a.audit.Tail(limit), nil
}

// alog records an app-originated action in the audit log.
func (a *App) alog(event, detail string) {
	if a.audit == nil {
		a.audit = audit.NewStore(core.DefaultPaths.AppDataDir())
	}
	a.audit.Log("app", "", event, detail)
	// Mirror into the leveled application log (info by default; errors are
	// recognizable by the event suffix convention ".fail"/".error").
	level := applog.LevelInfo
	if strings.HasSuffix(event, ".fail") || strings.HasSuffix(event, ".error") {
		level = applog.LevelError
	}
	if a.appLog != nil {
		a.appLog.Log(level, "app", "%s %s", event, detail)
	}
}

// ---- metrics: Prometheus text exposition ----

// Metrics renders the /api/metrics payload. sessionsActive comes from the
// web layer; everything else is gathered locally.
func (a *App) Metrics(activeSessions int) (string, error) {
	up := 0
	if a.procs.Any() {
		up = 1
	}
	var b strings.Builder
	w := func(line string) { b.WriteString(line + "\n") }

	w("# HELP easytier_pro_core_up Whether easytier-core is running.")
	w("# TYPE easytier_pro_core_up gauge")
	w(fmt.Sprintf("easytier_pro_core_up %d", up))

	nets, _ := a.config.List()
	running := 0
	for _, c := range nets {
		if c.Enabled {
			running++
		}
	}
	w("# TYPE easytier_pro_networks_running gauge")
	w(fmt.Sprintf("easytier_pro_networks_running %d", running))

	if up == 1 {
		if peers := a.trayPeersByNetwork(nets); peers != nil {
			w("# TYPE easytier_pro_peers gauge")
			for net, list := range peers {
				w(fmt.Sprintf("easytier_pro_peers{network=%q} %d", net, len(list)))
			}
		}
		if counters, perr := a.mergedTrafficCounters(); perr == nil {
			w("# TYPE easytier_pro_traffic_bytes_total counter")
			for net, c := range counters {
				w(fmt.Sprintf("easytier_pro_traffic_bytes_rx_total{network=%q} %d", net, c.Rx))
				w(fmt.Sprintf("easytier_pro_traffic_bytes_tx_total{network=%q} %d", net, c.Tx))
			}
		}
	}

	fleetOnline := 0
	if a.fleet != nil {
		for _, ag := range a.fleet.Agents() {
			if ag.Online() {
				fleetOnline++
			}
		}
	}
	w("# TYPE easytier_pro_fleet_agents_online gauge")
	w(fmt.Sprintf("easytier_pro_fleet_agents_online %d", fleetOnline))

	tunnelsActive := 0
	if a.tunnels != nil {
		for _, t := range a.tunnels.List() {
			if t.Status == tunnel.StatusRunning || t.Status == tunnel.StatusStarting {
				tunnelsActive++
			}
		}
	}
	w("# TYPE easytier_pro_tunnels_active gauge")
	w(fmt.Sprintf("easytier_pro_tunnels_active %d", tunnelsActive))

	w("# TYPE easytier_pro_sessions_active gauge")
	w(fmt.Sprintf("easytier_pro_sessions_active %d", activeSessions))

	w("# TYPE easytier_pro_build_info gauge")
	w(fmt.Sprintf("easytier_pro_build_info{os=%q,arch=%q,core=%q} 1", runtime.GOOS, runtime.GOARCH, a.Versions().Core))

	return b.String(), nil
}

func (a *App) onCoreStatusChanged(running bool) {
	defer a.recoverPanic("onCoreStatusChanged")
	a.statusMu.Lock()
	if running {
		a.coreStatus = "running"
	} else if a.coreStatus != "error" {
		a.coreStatus = "stopped"
	}
	status := a.coreStatus
	a.statusMu.Unlock()
	a.emit("core-status", status)
	a.updateTrayStatus()
}

func (a *App) onCoreExit(err error) {
	defer a.recoverPanic("onCoreExit")
	if err != nil {
		a.statusMu.Lock()
		a.coreStatus = "error"
		msg := err.Error()
		a.statusMu.Unlock()
		a.emit("core-error", msg)
	}
	// Other instances may still be running: report the aggregate state.
	a.emit("core-status", a.refreshCoreStatus())
}

func (a *App) onCoreLog(line string) {
	defer a.recoverPanic("onCoreLog")
	a.mu.Lock()
	a.lastLog = append(a.lastLog, line)
	if len(a.lastLog) > 1000 {
		a.lastLog = a.lastLog[len(a.lastLog)-1000:]
	}
	a.mu.Unlock()
	a.emit("core-log", line)
}

func (a *App) refreshCoreStatus() string {
	// Read process state without holding statusMu to avoid a lock-ordering
	// deadlock with process callbacks (which take statusMu then proc.mu).
	running := a.procs.Any()

	a.statusMu.Lock()
	if running {
		a.coreStatus = "running"
	} else {
		a.coreStatus = "stopped"
	}
	status := a.coreStatus
	a.statusMu.Unlock()
	return status
}

// CoreStatus returns the current core lifecycle state.
func (a *App) CoreStatus() string {
	return a.refreshCoreStatus()
}

// StartCore launches one easytier-core process per enabled network instance
// (per-network process model: instances are independent).
func (a *App) StartCore() (err error) {
	defer a.recoverToErr("StartCore", &err)
	// Without elevation easytier-core cannot create the TUN adapter; it
	// would linger in the background while every network stays broken.
	// Refuse with a clear message instead (the manifest normally forces
	// the UAC prompt; this covers portable/manual launches).
	if runtime.GOOS == "windows" && !isElevated() {
		return fmt.Errorf("需要管理员权限：请右键以管理员身份运行，或在弹出的 UAC 对话框中确认")
	}
	a.alog("core.start", "")
	a.statusMu.Lock()
	a.coreStatus = "starting"
	a.statusMu.Unlock()
	a.emit("core-status", "starting")
	return a.reconcileInstances(nil)
}

// StopCore terminates every instance process (also stops a service-mode
// core holding the base portal).
func (a *App) StopCore() error {
	a.alog("core.stop", "")
	a.procs.StopAll()
	_ = core.StopExternal()
	a.refreshCoreStatus()
	return nil
}

// RestartCore stops everything and starts fresh. Reserved for global
// changes (config dir, binary update) — single-network operations use
// reconcileInstances and never disturb other running networks.
func (a *App) RestartCore() (err error) {
	defer a.recoverToErr("RestartCore", &err)
	a.alog("core.restart", "")
	a.applyLeases() // sticky-DHCP: re-apply last assigned virtual IPs
	a.statusMu.Lock()
	a.coreStatus = "starting"
	a.statusMu.Unlock()
	a.emit("core-status", "starting")
	a.procs.StopAll()
	_ = core.StopExternal()
	return a.reconcileInstances(nil)
}

// ---- elevation ----

// IsAdmin reports whether the process runs with administrator privileges.
// easytier-core needs this on Windows to create the TUN virtual adapter.
func (a *App) IsAdmin() bool {
	return isElevated()
}

// RelaunchAsAdmin restarts the GUI elevated (UAC prompt) and lets the current
// instance exit. Used when the core cannot create its virtual adapter.
// ShellExecute "runas" returns before the UAC prompt is answered; only the
// request being accepted (error is nil) means the new instance is coming up,
// so that is the point at which this instance stands down. The elevated
// successor waits for the singleton mutex while this instance unwinds.
func (a *App) RelaunchAsAdmin() error {
	if err := relaunchElevated(); err != nil {
		return err
	}
	go func() {
		a.tunnelManager().StopAll()
		_ = a.StopCore()
		if a.ctx != nil {
			wailsruntime.Quit(a.ctx)
		}
		os.Exit(0)
	}()
	return nil
}

// ---- diagnostics ----

// PingResult is the parsed outcome of a PingDiagnostic run.
type PingResult struct {
	Host        string    `json:"host"`
	Sent        int       `json:"sent"`
	Received    int       `json:"received"`
	LossPercent float64   `json:"loss_percent"`
	MinMs       float64   `json:"min_ms"`
	MaxMs       float64   `json:"max_ms"`
	AvgMs       float64   `json:"avg_ms"`
	JitterMs    float64   `json:"jitter_ms"`
	Rtts        []float64 `json:"rtts"`
}

// safeHost validates a hostname/IP before it is passed to an OS command
// (ssh/ping/mstsc). Hosts can originate from the DATA PLANE (a peer's
// advertised hostname), so a value starting with '-' could inject options
// into ssh (e.g. -oProxyCommand=... -> RCE) or add ping flags.
func safeHost(host string) error {
	h := strings.TrimSpace(host)
	if h == "" {
		return fmt.Errorf("empty host")
	}
	if len(h) > 253 {
		return fmt.Errorf("host too long")
	}
	if strings.HasPrefix(h, "-") {
		return fmt.Errorf("host must not start with '-'")
	}
	for _, r := range h {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '.' || r == '_' || r == '-' || r == ':'
		if !ok {
			return fmt.Errorf("host contains invalid character %q", r)
		}
	}
	return nil
}

// PingDiagnostic runs a ping against a host (virtual or physical) and returns
// parsed latency/loss/jitter statistics. Works with both Chinese and English
// ping output.
func (a *App) PingDiagnostic(host string, count int) (PingResult, error) {
	if err := safeHost(host); err != nil {
		return PingResult{}, err
	}
	if count <= 0 || count > 100 {
		count = 4
	}
	args := []string{"-n", strconv.Itoa(count), "-w", "2000", host}
	if runtime.GOOS != "windows" {
		args = []string{"-c", strconv.Itoa(count), "-W", "2", host}
	}
	cmd := exec.Command("ping", args...)
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		// ping exits non-zero when the target is unreachable; output is still useful.
		if out == nil && err.Error() == "exit status 1" {
			// keep parsing empty output
		}
	}
	// Chinese Windows emits GBK-encoded ping output; decode when the raw
	// output does not parse (fallback keeps non-GBK locales untouched).
	text := string(out)
	if res, perr := parsePingOutput(host, text); perr == nil {
		return res, nil
	}
	decoded, derr := decodeConsoleText(out)
	if derr == nil {
		return parsePingOutput(host, decoded)
	}
	return parsePingOutput(host, text)
}

// decodeConsoleText converts GBK/GB18030 console output to UTF-8. Returns an
// error if the bytes are not valid GBK (e.g. already UTF-8 or English text).
func decodeConsoleText(b []byte) (string, error) {
	if len(b) == 0 {
		return "", fmt.Errorf("empty output")
	}
	dec := simplifiedchinese.GB18030.NewDecoder()
	r := transform.NewReader(bytes.NewReader(b), dec)
	out, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// parsePingOutput extracts round-trip times and summary stats from ping output
// in either English or Chinese.
func parsePingOutput(host, output string) (PingResult, error) {
	res := PingResult{Host: host}
	lines := strings.Split(output, "\n")

	timeRe := regexp.MustCompile(`(?i)(?:time|时间)[=<](\d+(?:\.\d+)?)\s*ms`)
	statsRe := regexp.MustCompile(`(?i)(?:minimum|最短)\s*[=:]\s*(\d+(?:\.\d+)?)\s*ms\s*,\s*(?:maximum|最长)\s*[=:]\s*(\d+(?:\.\d+)?)\s*ms\s*,\s*(?:average|平均)\s*[=:]\s*(\d+(?:\.\d+)?)\s*ms`)
	lostRe := regexp.MustCompile(`(?i)\((\d+(?:\.\d+)?)%\s*(?:loss|丢失)\)`)

	for _, line := range lines {
		if m := timeRe.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				res.Rtts = append(res.Rtts, v)
			}
		}
		if m := statsRe.FindStringSubmatch(line); m != nil {
			if a, err := strconv.ParseFloat(m[1], 64); err == nil {
				res.MinMs = a
			}
			if b, err := strconv.ParseFloat(m[2], 64); err == nil {
				res.MaxMs = b
			}
			if c, err := strconv.ParseFloat(m[3], 64); err == nil {
				res.AvgMs = c
			}
		}
		if m := lostRe.FindStringSubmatch(line); m != nil {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil {
				res.LossPercent = v
			}
		}
	}
	res.Sent = countPingsSent(lines)
	res.Received = len(res.Rtts)
	if res.Sent > 0 {
		if res.Received < res.Sent && res.LossPercent == 0 {
			res.LossPercent = float64(res.Sent-res.Received) / float64(res.Sent) * 100
		}
	}
	if len(res.Rtts) >= 2 {
		// jitter = mean absolute deviation between consecutive RTTs
		var sum float64
		for i := 1; i < len(res.Rtts); i++ {
			d := res.Rtts[i] - res.Rtts[i-1]
			if d < 0 {
				d = -d
			}
			sum += d
		}
		res.JitterMs = sum / float64(len(res.Rtts)-1)
	}
	if len(res.Rtts) == 0 {
		return res, fmt.Errorf("no reply from %s", host)
	}
	return res, nil
}

// countPingsSent heuristically reads the sent count from a ping summary line.
func countPingsSent(lines []string) int {
	for _, line := range lines {
		m := regexp.MustCompile(`(?i)(?:sent|已发送)\s*[=:]\s*(\d+)`).FindStringSubmatch(line)
		if m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				return v
			}
		}
	}
	return 0
}

// LaunchService opens a node service with the OS-native client:
//   - rdp  -> Windows Remote Desktop (mstsc)
//   - ssh  -> ssh in a new terminal
//   - http/https -> default browser
func (a *App) LaunchService(host string, port int, proto string) error {
	if err := safeHost(host); err != nil {
		return err
	}
	switch strings.ToLower(proto) {
	case "rdp":
		cmd := exec.Command("mstsc", "/v:"+host+":"+strconv.Itoa(port))
		return cmd.Start()
	case "ssh":
		cmd := exec.Command("ssh", "-p", strconv.Itoa(port), host)
		return cmd.Start()
	case "http", "https":
		scheme := strings.ToLower(proto)
		url := scheme + "://" + host
		if port > 0 && port != 80 && port != 443 {
			url += ":" + strconv.Itoa(port)
		}
		cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		return cmd.Start()
	default:
		return fmt.Errorf("unsupported service protocol: %s", proto)
	}
}

// LocalSubnets returns the IPv4 CIDRs of all non-loopback local interfaces.
func (a *App) LocalSubnets() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var subnets []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				subnets = append(subnets, ipnet.String())
			}
		}
	}
	return subnets, nil
}

// CheckSubnetConflict reports whether the given CIDR overlaps any local
// physical interface subnet (e.g. adding a proxy subnet that collides with
// the LAN would break routing).
func (a *App) CheckSubnetConflict(cidr string) (bool, error) {
	_, want, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return false, fmt.Errorf("invalid CIDR: %w", err)
	}
	subnets, err := a.LocalSubnets()
	if err != nil {
		return false, err
	}
	for _, s := range subnets {
		_, have, err := net.ParseCIDR(s)
		if err != nil {
			continue
		}
		if want.Contains(have.IP) || have.Contains(want.IP) {
			return true, nil
		}
	}
	return false, nil
}

// ---- status queries (via easytier-cli) ----

// NodeInfo returns the local node status for every running instance.
func (a *App) NodeInfo() (json.RawMessage, error) {
	return a.queryAll((*easytier.Client).QueryNodeInfo)
}

// Peers returns the peer list for every running instance.
func (a *App) Peers() (json.RawMessage, error) {
	return a.queryAll((*easytier.Client).QueryPeers)
}

// Routes returns the routing table for every running instance.
func (a *App) Routes() (json.RawMessage, error) {
	return a.queryAll((*easytier.Client).QueryRoutes)
}

// Stats returns traffic statistics for every running instance.
func (a *App) Stats() (json.RawMessage, error) {
	return a.queryAll((*easytier.Client).QueryStats)
}

// VpnPortal returns WireGuard portal info for every running instance.
func (a *App) VpnPortal() (json.RawMessage, error) {
	return a.queryAll((*easytier.Client).QueryVpnPortal)
}

// Versions reports the bundled binary versions (fast, does not need a running
// core). Results are cached: binaries never change during a process lifetime.
func (a *App) Versions() easytier.Version {
	a.verMu.Lock()
	defer a.verMu.Unlock()
	if a.verCached.Core != "" || a.verCached.Cli != "" {
		return a.verCached
	}
	v := easytier.Version{}
	v.Core = easytier.BinaryVersion(core.DefaultPaths.CorePath())
	v.Cli = easytier.BinaryVersion(core.DefaultPaths.CliPath())
	a.verCached = v
	return v
}

// ---- core version management (download / update / switch) ----

// coreManager lazily creates the version manager rooted at appdata/cores.
func (a *App) coreManager() *coremgr.Manager {
	if a.cores == nil {
		a.cores = coremgr.NewManager(filepath.Join(core.DefaultPaths.AppDataDir(), "cores"))
	}
	return a.cores
}

// coreReleaseView is a release annotated with the asset this platform
// would download (empty when the release has no matching build).
type coreReleaseView struct {
	coremgr.Release
	AssetName string `json:"asset_name"`
	AssetSize int64  `json:"asset_size"`
}

// CoreReleases lists the newest official EasyTier releases with a marker for
// the asset matching this platform ("" when the release has none). Also
// returns the installed versions and the currently active tag.
func (a *App) CoreReleases() map[string]any {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rel, err := coremgr.FetchReleases(ctx)
	views := []coreReleaseView{}
	for _, r := range rel {
		v := coreReleaseView{Release: r}
		if asset := r.AssetFor(runtime.GOOS, runtime.GOARCH); asset != nil {
			v.AssetName = asset.Name
			v.AssetSize = asset.Size
		}
		views = append(views, v)
	}
	out := map[string]any{
		"releases": views,
		"error":    "",
	}
	if err != nil {
		out["releases"] = []coreReleaseView{}
		out["error"] = err.Error()
	}
	cfg, _ := a.settings.Load()
	out["installed"] = a.coreManager().Installed(cfg.CoreVersion)
	out["active"] = cfg.CoreVersion
	out["bundled"] = a.Versions().Core
	out["mirror"] = cfg.CoreMirror
	return out
}

// CoreInstall downloads the given release for this platform (through the
// configured mirror), verifies the published sha256 and extracts core+cli.
// The release becomes the active version and the core restarts. Progress is
// emitted as "core-download" events (loaded/total bytes).
func (a *App) CoreInstall(tag string) error {
	a.alog("core.install", tag)
	rel, err := a.findRelease(tag)
	if err != nil {
		return err
	}
	cfg, _ := a.settings.Load()
	a.emit("core-download", map[string]any{"tag": tag, "loaded": 0, "total": 0})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	dir, err := a.coreManager().Install(ctx, *rel, cfg.CoreMirror, func(loaded, total int64) {
		a.emit("core-download", map[string]any{"tag": tag, "loaded": loaded, "total": total})
	})
	if err != nil {
		a.emit("core-download", map[string]any{"tag": tag, "error": err.Error()})
		return err
	}

	// Activate + persist, then restart the core so the new binary takes over.
	cfg.CoreVersion = rel.Tag
	if err := a.settings.Save(cfg); err != nil {
		return err
	}
	core.DefaultPaths.SetCoreOverride(dir)
	a.verMu.Lock()
	a.verCached = easytier.Version{} // re-probe against the new binary
	a.verMu.Unlock()
	return a.RestartCore()
}

// CoreSetActive switches the active core version (must already be installed)
// and restarts the core. Empty tag reverts to the bundled version.
func (a *App) CoreSetActive(tag string) error {
	a.alog("core.version", tag)
	if tag != "" {
		dir := a.coreManager().VersionDir(tag)
		if _, err := os.Stat(filepath.Join(dir, coremgr.CoreBinaryName())); err != nil {
			return fmt.Errorf("version %s is not installed", tag)
		}
		core.DefaultPaths.SetCoreOverride(dir)
	} else {
		core.DefaultPaths.SetCoreOverride("")
	}
	cfg, _ := a.settings.Load()
	cfg.CoreVersion = tag
	if err := a.settings.Save(cfg); err != nil {
		return err
	}
	a.verMu.Lock()
	a.verCached = easytier.Version{}
	a.verMu.Unlock()
	return a.RestartCore()
}

// CoreInstalled lists downloaded core versions with the active marker.
func (a *App) CoreInstalled() []coremgr.InstallInfo {
	cfg, _ := a.settings.Load()
	return a.coreManager().Installed(cfg.CoreVersion)
}

// CoreDelete removes a downloaded version. The active version cannot be
// deleted; switch back to the bundled version first.
func (a *App) CoreDelete(tag string) error {
	a.alog("core.delete", tag)
	cfg, _ := a.settings.Load()
	if cfg.CoreVersion == tag {
		return fmt.Errorf("version %s is active — switch away first", tag)
	}
	dir := a.coreManager().VersionDir(tag)
	if _, err := os.Stat(filepath.Join(dir, coremgr.CoreBinaryName())); err != nil {
		return fmt.Errorf("version %s is not installed", tag)
	}
	return os.RemoveAll(dir)
}

// ---- device admission list (allowlist / blacklist) ----

// DevicesList returns the admission list.
func (a *App) DevicesList() []devices.Device {
	if a.devices == nil {
		return []devices.Device{}
	}
	return a.devices.List()
}

// DeviceApprove allows a peer (optionally with a credential lifetime in
// days; 0 = no expiry). A previously written ACL deny rule is lifted and
// the core restarts so the relaxed rules apply.
func (a *App) DeviceApprove(peerID string, days int) (err error) {
	defer a.recoverToErr("DeviceApprove", &err)
	a.alog("device.approve", fmt.Sprintf("%s (%dd)", peerID, days))
	if a.devices == nil {
		return fmt.Errorf("device store unavailable")
	}
	dev, err := a.devices.Approve(peerID, "", "", "", days, "")
	if err != nil {
		return err
	}
	if err := a.applyDeviceACL(dev.Network); err != nil {
		return err
	}
	return a.RestartCore()
}

// DeviceDeny blacklists a peer: the entry moves to the deny list and an ACL
// drop rule is generated in its network config, then the core restarts so
// the rule takes effect.
func (a *App) DeviceDeny(peerID string) (err error) {
	defer a.recoverToErr("DeviceDeny", &err)
	a.alog("device.deny", peerID)
	if a.devices == nil {
		return fmt.Errorf("device store unavailable")
	}
	network := ""
	if dev, ok := a.devices.Get(peerID); ok {
		network = dev.Network
	}
	dev, err := a.devices.Deny(peerID, "", network, "", "")
	if err != nil {
		return err
	}
	if err := a.applyDeviceACL(dev.Network); err != nil {
		return err
	}
	return a.RestartCore()
}

// DeviceDenyCurrent blacklists a connected peer, carrying over the
// hostname/network/IP observed in the last admission scan.
func (a *App) DeviceDenyCurrent(peerID, hostname, network, ipv4 string) (err error) {
	defer a.recoverToErr("DeviceDenyCurrent", &err)
	a.alog("device.deny", peerID+" "+hostname)
	if a.devices == nil {
		return fmt.Errorf("device store unavailable")
	}
	dev, err := a.devices.Deny(peerID, hostname, network, ipv4, "manual")
	if err != nil {
		return err
	}
	if err := a.applyDeviceACL(dev.Network); err != nil {
		return err
	}
	return a.RestartCore()
}

// DeviceRemove forgets an entry (the peer becomes unknown again; a stale
// deny rule is lifted if its virtual IP is known).
func (a *App) DeviceRemove(peerID string) (err error) {
	defer a.recoverToErr("DeviceRemove", &err)
	a.alog("device.remove", peerID)
	if a.devices == nil {
		return fmt.Errorf("device store unavailable")
	}
	network := ""
	if dev, ok := a.devices.Get(peerID); ok {
		network = dev.Network
	}
	if !a.devices.Remove(peerID) {
		return fmt.Errorf("device not found")
	}
	if err := a.applyDeviceACL(network); err != nil {
		return err
	}
	return a.RestartCore()
}

// AppLogTail returns the newest lines of the leveled application log.
func (a *App) AppLogTail(limit int) []string {
	if a.appLog == nil {
		return []string{}
	}
	return applog.Tail(a.appLog, limit)
}

// findRelease fetches the release list and picks the tag (accepts both
// "v2.6.4" and "2.6.4" spellings).
func (a *App) findRelease(tag string) (*coremgr.Release, error) {
	tag = strings.TrimSpace(tag)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel, err := coremgr.FetchReleases(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rel {
		if rel[i].Tag == tag || strings.TrimPrefix(rel[i].Tag, "v") == strings.TrimPrefix(tag, "v") {
			return &rel[i], nil
		}
	}
	return nil, fmt.Errorf("release %s not found in the newest releases", tag)
}

// AppInfo returns environment info shown in the UI.
func (a *App) AppInfo() map[string]string {
	return map[string]string{
		"os":         runtime.GOOS,
		"arch":       runtime.GOARCH,
		"go_version": runtime.Version(),
		"app_data":   core.DefaultPaths.AppDataDir(),
		"config_dir": core.DefaultPaths.ConfigDir(),
		"log_dir":    core.DefaultPaths.LogDir(),
		"rpc_portal": core.RpcPortal,
	}
}

// CoreLog returns recent core log lines. For a GUI-launched core these come
// from the live stdout pipe; for an external core (service mode, leftover) the
// file log is tailed so the Settings page still shows usable output.
func (a *App) CoreLog() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.lastLog) > 0 {
		return append([]string(nil), a.lastLog...)
	}
	// external core: read from the file log
	logFile := filepath.Join(core.DefaultPaths.LogDir(), "easytier.log")
	lines := readLogTail(logFile, 200)
	return lines
}

// readLogTail returns the last n lines of a file, or an empty slice on error.
func readLogTail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

// ---- background sampling: traffic history + peer watchdog ----

// backgroundLoop samples traffic, runs the peer watchdog, records DHCP
// leases, syncs MagicDNS hosts entries and keeps the tray menu in sync every
// 10 s while the GUI is alive.
func (a *App) backgroundLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.sampleTraffic()
		a.runWatchdog()
		a.scanAdmission()
		a.checkQuota()
		a.recordLeases()
		a.checkInjected()
		a.syncTrayMenu()
		a.syncHostsFile()
		// Self-heal: when the web server could not bind at startup (port
		// still held by a leftover process), retry every tick until it
		// comes up instead of staying down for the whole app run.
		if a.web != nil && !a.web.Running() {
			if err := a.web.Start(); err == nil {
				a.alog("web.recovered", "listening on "+a.web.Addr())
			}
		}
		// Persist web sessions every tick (10 s) so a crash or quick
		// restart never loses newer logins; the write is tiny.
		if a.web != nil {
			a.web.SaveSessions()
		}
	}
}

// syncHostsFile maintains the MagicDNS-lite hosts block: hostname → virtual
// IP for every connected peer. Disabled state cleans the block up once.
func (a *App) syncHostsFile() {
	if a.hosts == nil {
		return
	}
	cfg, err := a.settings.Load()
	if err != nil {
		return
	}
	if !cfg.MagicDNS {
		if a.dnsOn {
			_ = a.hosts.Remove()
			a.dnsOn = false
			println("magicdns: hosts block removed")
		}
		return
	}
	if !a.coreUp() {
		return
	}
	configs, err := a.config.List()
	if err != nil {
		return
	}
	entries := map[string]string{}
	for _, peers := range a.trayPeersByNetwork(configs) {
		for _, p := range peers {
			if p.IP == "" || p.Hostname == "" {
				continue
			}
			name := strings.ToLower(p.Hostname)
			if _, exists := entries[name]; !exists {
				entries[name] = p.IP
			}
		}
	}
	if err := a.hosts.Update(entries); err != nil {
		println("magicdns: hosts update failed:", err.Error())
		return
	}
	a.dnsOn = true
}

func (a *App) coreUp() bool {
	return a.procs.Any()
}

// TrafficHistory returns aggregated traffic for the last `days` days.
func (a *App) TrafficHistory(days int) (traffic.History, error) {
	if a.traffic == nil {
		a.traffic = traffic.NewStore(filepath.Join(core.DefaultPaths.AppDataDir(), "traffic"))
	}
	return a.traffic.History(days)
}

func (a *App) sampleTraffic() {
	if a.traffic == nil || !a.coreUp() {
		return
	}
	nets, err := a.mergedTrafficCounters()
	if err != nil || len(nets) == 0 {
		return
	}
	_ = a.traffic.Sample(nets)
}

// parseTrafficCounters extracts cumulative traffic_bytes_rx/tx per network
// from the Prometheus-style stats output of easytier-cli.
func parseTrafficCounters(raw json.RawMessage) (map[string]traffic.Counter, error) {
	var metrics []struct {
		Name   string `json:"name"`
		Value  any    `json:"value"`
		Labels struct {
			NetworkName string `json:"network_name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(raw, &metrics); err != nil {
		return nil, err
	}
	out := map[string]traffic.Counter{}
	for _, m := range metrics {
		net := m.Labels.NetworkName
		if net == "" {
			continue
		}
		v := toU64(m.Value)
		c := out[net]
		switch m.Name {
		case "traffic_bytes_rx":
			c.Rx += v
		case "traffic_bytes_tx":
			c.Tx += v
		}
		out[net] = c
	}
	return out, nil
}

func toU64(v any) uint64 {
	switch n := v.(type) {
	case float64:
		return uint64(n)
	case string:
		u, _ := strconv.ParseUint(strings.TrimSpace(n), 10, 64)
		return u
	case json.Number:
		u, _ := strconv.ParseUint(n.String(), 10, 64)
		return u
	}
	return 0
}

// runWatchdog polls the peer list, detects offline/latency transitions and
// delivers notifications (desktop + optional webhook) for new events.
func (a *App) runWatchdog() {
	cfg, err := a.settings.Load()
	if err != nil || !cfg.Alerts.Enabled {
		return
	}
	if !cfg.Alerts.OfflineNotify && !cfg.Alerts.HighLatencyNotify {
		return
	}
	if !a.coreUp() {
		return
	}
	raw, err := a.queryAll((*easytier.Client).QueryPeers)
	if err != nil {
		return // core busy/down: keep the previous snapshot
	}
	cur := a.peersToStates(raw)
	a.alertMu.Lock()
	prev := a.alertPrev
	a.alertPrev = cur
	a.alertMu.Unlock()
	if len(prev) == 0 && len(cur) == 0 {
		return
	}
	events := alerts.Detect(prev, cur, alerts.Config{
		OfflineEnabled:     cfg.Alerts.OfflineNotify,
		LatencyEnabled:     cfg.Alerts.HighLatencyNotify,
		LatencyThresholdMs: cfg.Alerts.HighLatencyMs,
	})
	for _, ev := range events {
		title := "EasyTier Pro"
		switch ev.Kind {
		case "offline":
			title += " · 节点离线"
		case "latency":
			title += " · 高延迟"
		}
		_ = desktopNotify(title, ev.Detail)
		if cfg.Alerts.WebhookURL != "" {
			go func(ev alerts.Event, url string) { _ = alerts.PostWebhook(url, ev) }(ev, cfg.Alerts.WebhookURL)
		}
		a.emit("watchdog:alert", ev)
	}
}

// peersToStates flattens the peer list (single-network flat array or grouped
// per-instance array) into alert states, excluding the local peer.
func (a *App) peersToStates(raw json.RawMessage) map[string]alerts.PeerState {
	states := map[string]alerts.PeerState{}
	var groups []struct {
		InstanceID   string          `json:"instance_id"`
		InstanceName string          `json:"instance_name"`
		Result       json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &groups); err == nil && len(groups) > 0 && len(groups[0].Result) > 0 {
		for _, g := range groups {
			name := g.InstanceName
			if name == "" {
				name = g.InstanceID
			}
			for _, p := range parseRawPeers(g.Result) {
				addPeerState(states, name, p)
			}
		}
		return states
	}
	for _, p := range parseRawPeers(raw) {
		addPeerState(states, a.primaryNetworkName(), p)
	}
	return states
}

func addPeerState(states map[string]alerts.PeerState, network string, p rawPeer) {
	if p.Cost == "Local" || p.Hostname == "" {
		return
	}
	st := alerts.PeerState{Network: network, Name: p.Hostname, Online: true}
	if ms, err := strconv.ParseFloat(strings.TrimSpace(p.LatMs), 64); err == nil && ms >= 0 {
		st.LatencyMs = ms
		st.LatencyKnown = true
	}
	states[alerts.Key(network, p.Hostname)] = st
}

// ---- device admission scan (unknown peer alert / blacklist) ----

// scanAdmission polls the peer list and checks every remote peer against the
// device admission list. Unknown peers raise an alert once per peer; denied
// or credential-expired peers are blacklisted via an ACL drop rule in the
// network config (first matching rule wins, so the drop sits on top).
func (a *App) scanAdmission() {
	if a.devices == nil {
		return
	}
	// Credential expiry sweep: expired allows become denies (once).
	for _, d := range a.devices.ExpireDue(time.Now()) {
		a.alog("device.expired", d.PeerID+" "+d.Name)
		if a.appLog != nil {
			a.appLog.Warnf("devices", "credential expired: %s (%s) — moved to deny list", d.Name, d.PeerID)
		}
		a.emit("devices:expired", d)
		if err := a.applyDeviceACL(d.Network); err == nil {
			_ = a.RestartCore()
		}
	}
	if !a.coreUp() {
		return
	}
	raw, err := a.queryAll((*easytier.Client).QueryPeers)
	if err != nil {
		return
	}
	type namedPeers struct {
		InstanceID   string          `json:"instance_id"`
		InstanceName string          `json:"instance_name"`
		Result       json.RawMessage `json:"result"`
	}
	var groups []namedPeers
	single := parseRawPeers(raw)
	check := func(network string, peers []rawPeer) {
		for _, p := range peers {
			id := p.peerKey()
			if id == "" || p.Cost == "Local" {
				continue
			}
			dev, known := a.devices.Get(id)
			if !known {
				// Unknown peer: alert once per peer per run.
				a.devMu.Lock()
				seen := a.devSeen[id]
				a.devSeen[id] = true
				a.devMu.Unlock()
				if !seen {
					a.alog("device.unknown", id+" "+p.Hostname)
					if a.appLog != nil {
						a.appLog.Warnf("devices", "unknown peer joined network %s: %s (peer %s)", network, p.Hostname, id)
					}
					a.emit("devices:unknown", map[string]string{
						"peer_id": id, "hostname": p.Hostname, "ipv4": p.IPv4, "network": network,
					})
					_ = desktopNotify("EasyTier Pro · 陌生设备入网",
						fmt.Sprintf("节点 %s（%s）加入了网络 %s，可在「设备准入」中放行或拉黑。", p.Hostname, p.IPv4, network))
				}
				continue
			}
			// Known: keep metadata fresh for the ACL generator (status
			// untouched — Touch never flips allow/deny).
			if dev.IPv4 != p.IPv4 || dev.Name != p.Hostname {
				dev, _ = a.devices.Touch(id, p.Hostname, network, p.IPv4)
			}
			if dev.Status == devices.StatusDeny {
				if err := a.applyDeviceACL(dev.Network); err == nil && dev.IPv4 != "" {
					a.alog("device.blocked", id+" "+p.Hostname)
					if a.appLog != nil {
						a.appLog.Warnf("devices", "denied peer connected: %s (%s) — ACL drop active", p.Hostname, dev.IPv4)
					}
				}
			}
		}
	}
	if len(groups) == 0 {
		_ = json.Unmarshal(raw, &groups)
	}
	if len(groups) > 0 && len(groups[0].Result) > 0 {
		for _, g := range groups {
			name := g.InstanceName
			if cfg, err := a.config.Get(g.InstanceID); err == nil && cfg.Network != "" {
				name = cfg.Network
			}
			check(name, parseRawPeers(g.Result))
		}
	} else if len(single) > 0 {
		check(a.primaryNetworkName(), single)
	}
}

// applyDeviceACL rewrites the "[acl]" section of the given network config so
// every denied device gets a top-priority Drop rule (matched by virtual IP).
// An empty network applies to the first enabled config. Existing user ACL
// chains are preserved.
func (a *App) applyDeviceACL(network string) error {
	configs, err := a.config.List()
	if err != nil {
		return err
	}
	target := -1
	for i, c := range configs {
		if !c.Enabled {
			continue
		}
		if network == "" || c.Network == network {
			target = i
			if network != "" {
				break
			}
		}
	}
	if target < 0 {
		return fmt.Errorf("no enabled config for network %q", network)
	}
	cfg := configs[target]

	// Collect drop rules from the deny list.
	drops := []string{}
	for _, d := range a.devices.List() {
		if d.Status == devices.StatusDeny && d.IPv4 != "" {
			ip := d.IPv4
			if !strings.Contains(ip, "/") {
				ip += "/32"
			}
			drops = append(drops, ip)
		}
	}
	if len(drops) == 0 {
		return nil // nothing to enforce
	}

	// Parse the existing [acl] section and inject/replace the managed chain.
	raw := cfg.Raw
	lines := strings.Split(raw, "\n")
	var out []string
	inAcl := false
	inserted := false
 managed := fmt.Sprintf(`[acl]
chains = [{ name = "easytier-pro-blocklist", chain_type = "Inbound", enabled = true, default_action = "Allow", rules = [{ name = "blocked-devices", priority = 1, enabled = true, protocol = "Any", source_ips = [%s], action = "Drop" }] }]`,
		strings.Join(func() []string {
			q := make([]string, len(drops))
			for i, d := range drops {
				q[i] = fmt.Sprintf("%q", d)
			}
			return q
		}(), ", "))
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == "[acl]" {
			inAcl = true
			out = append(out, managed)
			inserted = true
			continue
		}
		if inAcl {
			if strings.HasPrefix(t, "[") { // next section begins
				inAcl = false
				out = append(out, ln)
				continue
			}
			// Skip managed chain lines; keep foreign acl content best-effort
			// (inline-table style is handled by the frontend editor instead).
			continue
		}
		out = append(out, ln)
	}
	if !inserted {
		out = append(out, "", managed)
	}
	cfg.Raw = strings.Join(out, "\n")
	if err := a.config.Save(cfg); err != nil {
		return err
	}
	a.alog("device.acl", fmt.Sprintf("%s: %d blocked", cfg.Network, len(drops)))
	if a.appLog != nil {
		a.appLog.Infof("devices", "ACL blocklist updated for %s (%d rules)", cfg.Network, len(drops))
	}
	return nil
}

// ---- monthly traffic quota check ----

// checkQuota sums the current calendar month's traffic from the daily
// history and notifies once per threshold crossing (warn percent, then 100%).
func (a *App) checkQuota() {
	cfg, err := a.settings.Load()
	if err != nil || !cfg.Quota.Enabled || cfg.Quota.MonthlyGB <= 0 {
		return
	}
	hist, err := a.traffic.History(31)
	if err != nil {
		return
	}
	now := time.Now()
	ym := now.Format("2006-01")
	var monthRx, monthTx uint64
	for _, d := range hist.Days {
		if strings.HasPrefix(d.Date, ym) {
			monthRx += d.Rx
			monthTx += d.Tx
		}
	}
	used := float64(monthRx+monthTx) / (1024 * 1024 * 1024)
	pct := int(used / cfg.Quota.MonthlyGB * 100)

	a.quotaMu.Lock()
	if a.quotaYM != ym { // new month: re-arm both alerts
		a.quotaYM = ym
		a.quotaWarn = false
		a.quotaExceed = false
	}
	warnAt := cfg.Quota.WarnPercent
	if warnAt <= 0 {
		warnAt = 80
	}
	fireWarn := !a.quotaWarn && pct >= warnAt && pct < 100
	fireExceed := !a.quotaExceed && pct >= 100
	if fireWarn {
		a.quotaWarn = true
	}
	if fireExceed {
		a.quotaExceed = true
	}
	a.quotaMu.Unlock()

	if !fireWarn && !fireExceed {
		return
	}
	title := "EasyTier Pro · 流量提醒"
	detail := fmt.Sprintf("本月已用 %.2f GB / %.0f GB（%d%%）", used, cfg.Quota.MonthlyGB, pct)
	if fireExceed {
		title = "EasyTier Pro · 流量超额"
		detail = fmt.Sprintf("本月流量已超额：%.2f GB / %.0f GB（%d%%）", used, cfg.Quota.MonthlyGB, pct)
	}
	_ = desktopNotify(title, detail)
	if cfg.Alerts.WebhookURL != "" {
		go func() {
			_ = alerts.PostWebhook(cfg.Alerts.WebhookURL, alerts.Event{
				Kind: "quota", Detail: detail, Time: now.Format("2006-01-02 15:04:05"),
			})
		}()
	}
	a.emit("quota:alert", map[string]any{"used_gb": used, "quota_gb": cfg.Quota.MonthlyGB, "percent": pct, "exceeded": fireExceed})
	if a.appLog != nil {
		a.appLog.Warnf("quota", "%s", detail)
	}
}

type rawPeer struct {
	PeerID   string `json:"peer_id"`
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	IPv4     string `json:"ipv4"`
	Cost     string `json:"cost"`
	LatMs    string `json:"lat_ms"`
}

// peerKey returns the peer identity string (peer_id preferred, id fallback).
func (p rawPeer) peerKey() string {
	if p.PeerID != "" {
		return p.PeerID
	}
	return p.ID
}

func parseRawPeers(raw json.RawMessage) []rawPeer {
	var arr []rawPeer
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil
	}
	return arr
}

func (a *App) primaryNetworkName() string {
	if list, err := a.config.List(); err == nil {
		for _, c := range list {
			if c.Enabled {
				return c.Network
			}
		}
	}
	return "默认网络"
}

// ---- MTU path probing ----

// ProbeMTU discovers the path MTU to a peer with DF (don't-fragment) pings,
// binary-searching the payload size. Returns the MTU (payload + 28 bytes of
// IP+ICMP headers). Works on Windows/Linux/macOS ping variants.
func (a *App) ProbeMTU(target string) (int, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return 0, fmt.Errorf("target is empty")
	}
	lo, hi := 92, 1472 // IPv4 payload bounds for plausible MTUs (120..1500)
	best := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		ok, err := dfPing(target, mid)
		if err != nil {
			return 0, err
		}
		if ok {
			best = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if best == 0 {
		return 0, fmt.Errorf("无法探测路径 MTU：目标不可达（或 ping 被防火墙拦截）")
	}
	return best + 28, nil
}

var pingReplyRe = regexp.MustCompile(`(?i)ttl\s*[=:]\s*\d+`)

// dfPing sends a single don't-fragment ping with the given payload size and
// reports whether an ICMP echo reply arrived (a TTL field in the output).
func dfPing(target string, payload int) (bool, error) {
	var args []string
	switch runtime.GOOS {
	case "windows":
		args = []string{"-n", "1", "-w", "1500", "-f", "-l", strconv.Itoa(payload), target}
	case "darwin":
		args = []string{"-c", "1", "-W", "2000", "-D", "-s", strconv.Itoa(payload), target}
	default:
		args = []string{"-c", "1", "-W", "2", "-M", "do", "-s", strconv.Itoa(payload), target}
	}
	cmd := exec.Command("ping", args...)
	hideConsole(cmd)
	out, err := cmd.Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			// ping binary missing or blocked — that is a hard failure
			return false, fmt.Errorf("ping 执行失败: %w", err)
		}
		// non-zero exit usually means "fragmentation required" / timeout:
		// the output still gets parsed below.
	}
	text := string(out)
	if pingReplyRe.MatchString(text) {
		return true, nil
	}
	// Chinese Windows emits GBK-encoded ping output; decode and retry.
	if decoded, derr := decodeConsoleText(out); derr == nil {
		return pingReplyRe.MatchString(decoded), nil
	}
	return false, nil
}

// SendTestNotify shows a desktop notification so the user can verify the
// alert pipeline end to end.
func (a *App) SendTestNotify() error {
	return desktopNotify("EasyTier Pro", "这是一条测试通知 · desktop notifications are working")
}

// ---- sticky-DHCP: remember and re-apply last assigned virtual IPs ----

var (
	reDHCPTrue   = regexp.MustCompile(`(?m)^(\s*)dhcp\s*=\s*true\s*$`)
	reHasStatic  = regexp.MustCompile(`(?m)^\s*ipv4\s*=`)
	reDHCPOff    = regexp.MustCompile(`(?m)^\s*dhcp\s*=\s*false\s*$`)
	reTOMLString = regexp.MustCompile(`^\s*"(.*)"\s*$`)
)

// applyLeases injects the remembered virtual IP into every enabled DHCP
// config before the core starts, so a device keeps the address it had last
// time regardless of join order. Static configs are never touched.
func (a *App) applyLeases() {
	if a.leases == nil {
		return
	}
	list, err := a.config.List()
	if err != nil {
		return
	}
	a.injectMu.Lock()
	a.injected = map[string]string{}
	a.injectAt = time.Now()
	a.injectMu.Unlock()
	for _, c := range list {
		if !c.Enabled || c.Network == "" {
			continue
		}
		if reHasStatic.MatchString(c.Raw) || reDHCPOff.MatchString(c.Raw) {
			continue
		}
		l, ok := a.leases.Get(c.Network)
		if !ok || l.IP == "" {
			continue
		}
		prefix := l.Prefix
		if prefix <= 0 || prefix > 32 {
			prefix = 24
		}
		cidr := fmt.Sprintf("%s/%d", l.IP, prefix)
		newRaw, changed := injectStaticIP(c.Raw, cidr)
		if !changed {
			continue
		}
		c.Raw = newRaw
		if err := a.config.Save(c); err == nil {
			a.injectMu.Lock()
			a.injected[c.Network] = cidr
			a.injectMu.Unlock()
			println("sticky-dhcp: reserved", c.Network, cidr)
		}
	}
}

// injectStaticIP flips `dhcp = true` to `dhcp = false` and adds the
// `ipv4 = "cidr"` line right after it (both stay in the top-level section).
func injectStaticIP(raw, cidr string) (string, bool) {
	line := "${1}dhcp = false\n${1}ipv4 = \"" + cidr + "\""
	if reDHCPTrue.MatchString(raw) {
		return reDHCPTrue.ReplaceAllString(raw, line), true
	}
	// No dhcp key (older config): prepend both at the top, which is always
	// the top-level TOML section.
	return "dhcp = false\nipv4 = \"" + cidr + "\"\n" + raw, true
}

// recordLeases stores the currently assigned virtual IP per running network
// so the next start can reserve it again.
func (a *App) recordLeases() {
	if a.leases == nil || !a.coreUp() {
		return
	}
	for _, ni := range a.nodeInstances() {
		cidr := ni.IPv4
		if cidr == "" || cidr == "DHCP" {
			continue
		}
		ip, prefix, ok := splitCIDR(cidr)
		if !ok {
			continue
		}
		a.leases.Put(ni.Network, lease.Lease{IP: ip, Prefix: prefix, Hostname: ni.Hostname})
	}
}

// checkInjected reverts leases that did not materialise (the reserved IP was
// taken by another device while we were offline): strip the static address,
// forget the lease and restart once in DHCP mode so the network still comes
// up instead of failing forever.
func (a *App) checkInjected() {
	a.injectMu.Lock()
	inj := a.injected
	at := a.injectAt
	a.injectMu.Unlock()
	if len(inj) == 0 || time.Since(at) < 25*time.Second || time.Since(at) > 3*time.Minute {
		return
	}
	running := map[string]bool{}
	for _, ni := range a.nodeInstances() {
		running[ni.Network] = true
	}
	var stale []string
	for net := range inj {
		if !running[net] {
			stale = append(stale, net)
		}
	}
	if len(stale) == 0 {
		return
	}
	reverted := false
	for _, net := range stale {
		a.injectMu.Lock()
		cidr := inj[net]
		delete(a.injected, net)
		a.injectMu.Unlock()
		if a.revertLease(net, cidr) {
			reverted = true
		}
	}
	if reverted {
		println("sticky-dhcp: reserved IP unavailable, falling back to DHCP")
		go func() { _ = a.RestartCore() }()
	}
}

// revertLease removes the injected static address from the config file and
// forgets the lease. Returns true when something was actually reverted.
func (a *App) revertLease(network, cidr string) bool {
	_ = a.leases.Forget(network)
	list, err := a.config.List()
	if err != nil {
		return false
	}
	changed := false
	for _, c := range list {
		if c.Network != network || !strings.Contains(c.Raw, cidr) {
			continue
		}
		lineRe := regexp.MustCompile(`(?m)^\s*ipv4\s*=\s*"` + regexp.QuoteMeta(cidr) + `"\s*\n`)
		newRaw := lineRe.ReplaceAllString(c.Raw, "")
		newRaw = reDHCPOff.ReplaceAllStringFunc(newRaw, func(m string) string {
			// Only flip back when no other ipv4 remains (user may have set one).
			if reHasStatic.MatchString(newRaw) {
				return m
			}
			changed = true
			idx := strings.Index(m, "=")
			return m[:idx+1] + " true"
		})
		if newRaw != c.Raw {
			c.Raw = newRaw
			if err := a.config.Save(c); err == nil {
				changed = true
			}
		}
	}
	return changed
}

// nodeInstance is one running network as seen from the node-info endpoint.
type nodeInstance struct {
	Network  string
	Hostname string
	IPv4     string
}

// nodeInstances flattens the node-info payload (grouped per instance or a
// single flat object) into running-network views.
func (a *App) nodeInstances() []nodeInstance {
	raw, err := a.queryAll((*easytier.Client).QueryNodeInfo)
	if err != nil {
		return nil
	}
	var out []nodeInstance
	var groups []struct {
		InstanceID   string      `json:"instance_id"`
		InstanceName string      `json:"instance_name"`
		Result       interface{} `json:"result"`
	}
	if err := json.Unmarshal(raw, &groups); err == nil && len(groups) > 0 {
		for _, g := range groups {
			m, _ := g.Result.(map[string]interface{})
			out = append(out, nodeFromMap(m, g.InstanceName))
		}
		return out
	}
	var m map[string]interface{}
	if json.Unmarshal(raw, &m) == nil {
		out = append(out, nodeFromMap(m, a.primaryNetworkName()))
	}
	return out
}

func nodeFromMap(m map[string]interface{}, fallback string) nodeInstance {
	ni := nodeInstance{Network: fallback}
	if v, _ := m["network_name"].(string); v != "" {
		ni.Network = v
	}
	if v, _ := m["hostname"].(string); v != "" {
		ni.Hostname = v
	}
	ni.IPv4, _ = m["ipv4_addr"].(string)
	return ni
}

func splitCIDR(s string) (ip string, prefix int, ok bool) {
	s = strings.TrimSpace(s)
	i := strings.LastIndex(s, "/")
	if i < 0 {
		if net.ParseIP(s) == nil {
			return "", 0, false
		}
		return s, 24, true
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p < 0 || p > 32 || net.ParseIP(s[:i]) == nil {
		return "", 0, false
	}
	return s[:i], p, true
}

// ListLeases returns the sticky-DHCP table for the settings UI.
func (a *App) ListLeases() (map[string]lease.Lease, error) {
	if a.leases == nil {
		return map[string]lease.Lease{}, nil
	}
	return a.leases.All(), nil
}

// ForgetLease drops the remembered address of one network (back to pure DHCP).
func (a *App) ForgetLease(network string) error {
	if a.leases == nil {
		return nil
	}
	a.leases.Forget(network)
	return nil
}

// ---- config management ----

// ListConfigs returns the stored network configs.
func (a *App) ListConfigs() ([]configmgr.ConfigFile, error) {
	return a.config.List()
}

// SaveConfig persists a network config to config-dir.
func (a *App) SaveConfig(cfg configmgr.ConfigFile) error {
	a.alog("config.save", cfg.Network+" ("+cfg.InstanceID+")")
	return a.config.Save(cfg)
}

// GetConfig returns a single config by instance id.
func (a *App) GetConfig(id string) (configmgr.ConfigFile, error) {
	return a.config.Get(id)
}

// DeleteConfig removes a config by instance id. A running instance is
// stopped first — deleting a live network must not leave an orphan core
// process serving a config that no longer exists.
func (a *App) DeleteConfig(id string) error {
	a.alog("config.delete", id)
	if err := a.procs.StopInstance(id); err == nil {
		a.refreshCoreStatus()
	}
	return a.config.Delete(id)
}

// SetConfigEnabled enables or disables a config and brings the instance
// process in line (start on enable, stop on disable). Delegates to
// SetNetworkRunning so every toggle path — desktop UI and web API —
// synchronizes the flag AND the process. Other networks are untouched.
func (a *App) SetConfigEnabled(id string, enabled bool) error {
	return a.SetNetworkRunning(id, enabled)
}

// ApplyConfigs persists configs and reconciles the running instances so
// changes take effect: new/changed networks start/restart alone, disabled
// ones stop alone — other running networks are never disturbed.
func (a *App) ApplyConfigs(configs []configmgr.ConfigFile, enabledIDs []string) error {
	changed := map[string]bool{}
	for _, cfg := range configs {
		if old, err := a.config.Get(cfg.InstanceID); err != nil || old.Raw != cfg.Raw {
			changed[cfg.InstanceID] = true
		}
		if err := a.config.Save(cfg); err != nil {
			return err
		}
	}

	// reconcile enabled/disabled states
	all, err := a.config.List()
	if err != nil {
		return err
	}
	enabledSet := map[string]bool{}
	for _, id := range enabledIDs {
		enabledSet[id] = true
	}
	for _, cfg := range all {
		want := enabledSet[cfg.InstanceID]
		if cfg.Enabled != want {
			if err := a.config.SetEnabled(cfg.InstanceID, want); err != nil {
				return err
			}
		}
	}

	return a.reconcileInstances(changed)
}

// SetNetworkRunning enables or disables a single network. Only that
// instance's process is started/stopped — other networks keep running.
func (a *App) SetNetworkRunning(id string, running bool) error {
	a.alog("network.toggle", id+" -> "+map[bool]string{true: "start", false: "stop"}[running])
	if err := a.config.SetEnabled(id, running); err != nil {
		return err
	}
	if running {
		if a.procs.IsRunning(id) {
			return nil
		}
		a.applyLeases() // sticky-DHCP for the newly started instance
		err := a.procs.StartInstance(id, a.onCoreLog, a.onCoreStatusChanged, a.onCoreExit)
		a.refreshCoreStatus()
		return err
	}
	err := a.procs.StopInstance(id)
	a.refreshCoreStatus()
	return err
}

// reconcileInstances brings the running process set in line with the
// enabled configs: start missing, stop disabled, restart instances whose
// config content changed (changedIDs). Never touches other instances.
func (a *App) reconcileInstances(changed map[string]bool) error {
	a.applyLeases() // sticky-DHCP: re-apply last assigned virtual IPs
	if err := a.healListenerCollisions(); err != nil {
		println("listener heal:", err.Error())
	}
	nets, err := a.config.List()
	if err != nil {
		return err
	}
	var firstErr error
	for _, c := range nets {
		if !c.Enabled {
			if a.procs.IsRunning(c.InstanceID) {
				_ = a.procs.StopInstance(c.InstanceID)
			}
			continue
		}
		wasRunning := a.procs.IsRunning(c.InstanceID)
		if !wasRunning {
			if err := a.procs.StartInstance(c.InstanceID, a.onCoreLog, a.onCoreStatusChanged, a.onCoreExit); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			a.alog("instance.start", c.Network+" ("+c.InstanceID+")")
		} else if changed[c.InstanceID] {
			_ = a.procs.StopInstance(c.InstanceID)
			if err := a.procs.StartInstance(c.InstanceID, a.onCoreLog, a.onCoreStatusChanged, a.onCoreExit); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			} else {
				a.alog("instance.restart", c.Network+" ("+c.InstanceID+")")
			}
		}
	}
	a.refreshCoreStatus()
	return firstErr
}

// healListenerCollisions guarantees every enabled config has explicit,
// unique listener ports: per-instance core processes must not all grab the
// default 11010. Configs that already declare listeners keep them; only
// actual duplicates are rewritten (the later instance moves).
func (a *App) healListenerCollisions() error {
	nets, err := a.config.List()
	if err != nil {
		return err
	}
	used := map[string]string{} // "host:port" -> instance id
	nextPort := 11010
	taken := func(p int) bool {
		_, busy := used[fmt.Sprintf("0.0.0.0:%d", p)]
		return busy
	}
	for _, c := range nets {
		if !c.Enabled {
			continue
		}
		ports := listenerPorts(c.Raw)
		if len(ports) == 0 {
			// no explicit listeners: the core would bind the default set
			// (11010 tcp/udp) — give this instance its own pair.
			for taken(nextPort) {
				nextPort++
			}
			raw, err := configmgr.InjectListeners(c.Raw, nextPort)
			if err != nil {
				return err
			}
			used[fmt.Sprintf("0.0.0.0:%d", nextPort)] = c.InstanceID
			nextPort++
			if err := a.config.Save(configmgr.ConfigFile{InstanceID: c.InstanceID, Raw: raw, Enabled: true}); err != nil {
				return err
			}
			a.alog("instance.listeners", c.InstanceID+" -> "+strconv.Itoa(nextPort-1))
			continue
		}
		for _, p := range ports {
			key := "0.0.0.0:" + strconv.Itoa(p)
			if owner, busy := used[key]; busy && owner != c.InstanceID {
				// collision: move this instance's listeners to a free port
				for taken(nextPort) {
					nextPort++
				}
				raw, err := configmgr.ReplaceListenerPorts(c.Raw, nextPort)
				if err != nil {
					return err
				}
				used[fmt.Sprintf("0.0.0.0:%d", nextPort)] = c.InstanceID
				nextPort++
				if err := a.config.Save(configmgr.ConfigFile{InstanceID: c.InstanceID, Raw: raw, Enabled: true}); err != nil {
					return err
				}
				a.alog("instance.listeners", c.InstanceID+" moved -> "+strconv.Itoa(nextPort-1))
				break // one rewrite per colliding instance is enough
			}
			used[key] = c.InstanceID
		}
	}
	return nil
}

// listenerPorts extracts the numeric ports from a config's listeners array
// (empty when the key is absent or the list is empty).
func listenerPorts(raw string) []int {
	m := regexp.MustCompile(`(?m)^\s*listeners\s*=\s*\[([^\]]*)\]`).FindStringSubmatch(raw)
	if m == nil {
		return nil
	}
	out := []int{}
	for _, sm := range regexp.MustCompile(`:(\d+)`).FindAllStringSubmatch(m[1], -1) {
		if p, err := strconv.Atoi(sm[1]); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// ---- events ----

func (a *App) emit(event string, data any) {
	if a.eventSink == nil {
		return
	}
	// EventsEmit into a torn-down webview must never kill the process.
	defer a.recoverPanic("emit:" + event)
	a.eventSink(event, data)
}

// ---- panic containment ----
// The GUI hosts long-running background loops and child-process callbacks;
// a single panic there used to take the whole window down while the core
// children kept running (the "GUI crashed, core alive" symptom). Every
// goroutine and callback funnels through recoverPanic, which writes the
// stack to the app log plus a standalone panic-*.log for diagnosis.

func (a *App) recoverPanic(where string) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		msg := fmt.Sprintf("panic in %s: %v\n%s", where, r, stack)
		if a.appLog != nil {
			a.appLog.Errorf("panic", "%s", msg)
		}
		writePanicDump(where, msg)
	}
}

// goSafe runs fn in a guarded goroutine.
func (a *App) goSafe(name string, fn func()) {
	go func() {
		defer a.recoverPanic(name)
		fn()
	}()
}

// recoverToErr guards a Wails-bound call: a panic becomes a returned error
// (surfaced in the UI) instead of a process crash.
func (a *App) recoverToErr(where string, errp *error) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		msg := fmt.Sprintf("panic in %s: %v\n%s", where, r, stack)
		if a.appLog != nil {
			a.appLog.Errorf("panic", "%s", msg)
		}
		writePanicDump(where, msg)
		if errp != nil {
			*errp = fmt.Errorf("internal error (%s), see app log", where)
		}
	}
}

func writePanicDump(where, msg string) {
	dir := filepath.Join(core.DefaultPaths.LogDir(), "app")
	_ = os.MkdirAll(dir, 0o755)
	name := fmt.Sprintf("panic-%s.log", time.Now().Format("20060102-150405"))
	_ = os.WriteFile(filepath.Join(dir, name), []byte("where: "+where+"\n\n"+msg), 0o644)
}
