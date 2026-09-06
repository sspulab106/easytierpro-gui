package configmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// instanceIDRe validates that an instance id contains only safe characters so
// it cannot be used for path traversal.
var instanceIDRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func validateID(id string) error {
	if !instanceIDRe.MatchString(id) {
		return fmt.Errorf("invalid instance_id: %q — must be 1-64 alphanumeric characters, hyphens or underscores", id)
	}
	return nil
}

// ConfigFile is a network instance configuration stored in config-dir.
type ConfigFile struct {
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Network      string `json:"network"`
	IPv4         string `json:"ipv4"`
	Enabled      bool   `json:"enabled"`
	Path         string `json:"path"`
	Raw          string `json:"raw"`
}

// Manager provides CRUD over TOML config files in a config-dir.
type Manager struct {
	dir string
}

func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

func (m *Manager) Dir() string {
	return m.dir
}

// meta is the subset of TOML fields needed for the UI.
type meta struct {
	InstanceID      string `toml:"instance_id"`
	InstanceName    string `toml:"instance_name"`
	Network         string `toml:"network"`
	IPv4            string `toml:"ipv4"`
	NetworkIdentity struct {
		NetworkName string `toml:"network_name"`
	} `toml:"network_identity"`
}

func parseMeta(raw string) meta {
	var m meta
	_ = toml.Unmarshal([]byte(raw), &m)
	return m
}

// List returns every config file present in config-dir.
// Enabled configs are *.toml; disabled ones are *.toml.disabled.
func (m *Manager) List() ([]ConfigFile, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}

	configs := make([]ConfigFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		enabled := true
		if strings.HasSuffix(name, ".disabled") {
			enabled = false
			name = strings.TrimSuffix(name, ".disabled")
		}
		if !strings.HasSuffix(name, ".toml") {
			continue
		}
		path := filepath.Join(m.dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		raw := string(data)
		m := parseMeta(raw)
		id := m.InstanceID
		if id == "" {
			id = strings.TrimSuffix(name, ".toml")
		}
		netName := m.Network
		if netName == "" {
			netName = m.NetworkIdentity.NetworkName
		}
		configs = append(configs, ConfigFile{
			InstanceID:   id,
			InstanceName: m.InstanceName,
			Network:      netName,
			IPv4:         m.IPv4,
			Enabled:      enabled,
			Path:         path,
			Raw:          raw,
		})
	}
	sort.Slice(configs, func(i, j int) bool {
		return configs[i].InstanceName < configs[j].InstanceName
	})
	return configs, nil
}

// Save writes (or updates) a config file, applying the instance-id naming
// convention expected by easytier-core.
func (m *Manager) Save(cfg ConfigFile) error {
	raw := cfg.Raw
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("config content is empty")
	}

	id := cfg.InstanceID
	rawMetaID := parseMeta(raw).InstanceID
	// The instance_id declared inside the TOML is authoritative for naming,
	// so files never disagree with their content.
	if rawMetaID != "" {
		id = rawMetaID
	}
	if id == "" {
		return fmt.Errorf("cannot determine instance id from config")
	}
	if err := validateID(id); err != nil {
		return err
	}

	// inject instance_id into TOML if missing
	if !containsID(raw) {
		raw = injectID(raw, id)
	}
	// Pin the TUN adapter name to a stable per-instance value. Without this
	// easytier-core generates a random adapter name on every start and
	// Windows Firewall accumulates 8 allow rules per restart.
	raw = injectDevName(raw, id)

	fileName := id + ".toml"
	if !cfg.Enabled {
		fileName = id + ".toml.disabled"
	}
	// Atomic write: tmp + rename, then remove the opposite state file. Never
	// delete the old file first — a crash mid-write must not lose the config.
	target := filepath.Join(m.dir, fileName)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, []byte(raw), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return err
	}
	// Remove the other state file so the same instance never exists in both
	// enabled and disabled forms.
	other := id + ".toml"
	if cfg.Enabled {
		other = id + ".toml.disabled"
	}
	_ = os.Remove(filepath.Join(m.dir, other))
	return nil
}

// Delete removes a config file regardless of enabled state.
func (m *Manager) Delete(instanceID string) error {
	if err := validateID(instanceID); err != nil {
		return err
	}
	found := false
	for _, state := range []string{".toml", ".toml.disabled"} {
		p := filepath.Join(m.dir, instanceID+state)
		if err := os.Remove(p); err == nil {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("config %s not found", instanceID)
	}
	return nil
}

// SetEnabled toggles a config between active and disabled file states.
func (m *Manager) SetEnabled(instanceID string, enabled bool) error {
	if err := validateID(instanceID); err != nil {
		return err
	}
	active := filepath.Join(m.dir, instanceID+".toml")
	disabled := filepath.Join(m.dir, instanceID+".toml.disabled")

	if enabled {
		if _, err := os.Stat(disabled); err == nil {
			return os.Rename(disabled, active)
		}
		if _, err := os.Stat(active); err == nil {
			return nil
		}
		return fmt.Errorf("config %s not found", instanceID)
	}

	if _, err := os.Stat(active); err == nil {
		return os.Rename(active, disabled)
	}
	if _, err := os.Stat(disabled); err == nil {
		return nil
	}
	return fmt.Errorf("config %s not found", instanceID)
}

// Get returns the raw TOML content of a config by instance id.
func (m *Manager) Get(instanceID string) (ConfigFile, error) {
	if err := validateID(instanceID); err != nil {
		return ConfigFile{}, err
	}
	for _, state := range []string{".toml", ".toml.disabled"} {
		p := filepath.Join(m.dir, instanceID+state)
		if data, err := os.ReadFile(p); err == nil {
			m := parseMeta(string(data))
			return ConfigFile{
				InstanceID:   instanceID,
				InstanceName: m.InstanceName,
				Network:      m.Network,
				Enabled:      state == ".toml",
				Raw:          string(data),
			}, nil
		}
	}
	return ConfigFile{}, fmt.Errorf("config %s not found", instanceID)
}

func (m *Manager) removeForID(id string) {
	for _, state := range []string{".toml", ".toml.disabled"} {
		_ = os.Remove(filepath.Join(m.dir, id+state))
	}
}

func containsID(raw string) bool {
	// Match a real `instance_id = ...` assignment, not e.g. `instance_id_extra = 1`.
	re := regexp.MustCompile(`(?m)^\s*instance_id\s*=\s*`)
	return re.MatchString(raw)
}

func injectID(raw, id string) string {
	var b strings.Builder
	b.WriteString("instance_id = ")
	b.WriteString(fmt.Sprintf("%q", id))
	b.WriteString("\n")
	b.WriteString(raw)
	return b.String()
}

var (
	reDevName = regexp.MustCompile(`(?m)^\s*dev_name\s*=\s*`)
	reDash    = regexp.MustCompile(`-`)
)

// injectDevName pins `dev_name = "et_p_<8 hex of instance id>"` unless the
// user set one explicitly. Deterministic per instance, so the TUN adapter
// (and its Windows Firewall rules) survives restarts unchanged.
func injectDevName(raw, id string) string {
	if reDevName.MatchString(raw) {
		return raw
	}
	stable := "et_p_" + reDash.ReplaceAllString(id, "")[:8]
	line := fmt.Sprintf("dev_name = %q\n", stable)
	// Keep it in the top-level section: right after instance_id if present,
	// otherwise at the very top.
	if loc := reDevNameLoc.FindStringIndex(raw); loc != nil {
		return raw[:loc[1]] + "\n" + line + raw[loc[1]:]
	}
	if loc := reIDLoc.FindStringIndex(raw); loc != nil {
		return raw[:loc[1]] + "\n" + line + raw[loc[1]:]
	}
	return line + raw
}

var (
	reDevNameLoc = regexp.MustCompile(`(?m)^\s*instance_name\s*=.*$`)
	reIDLoc      = regexp.MustCompile(`(?m)^\s*instance_id\s*=.*$`)
)
