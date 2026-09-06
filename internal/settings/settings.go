// Package settings persists the GUI's application settings as JSON in app-data.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// WebdavConfig holds the cloud sync credentials.
type WebdavConfig struct {
	ServerURL string `json:"server_url"` // e.g. https://nextcloud.example.com/remote.php/dav/files/user/easytier-pro
	Username  string `json:"username"`
	Password  string `json:"password"`
	LastSync  string `json:"last_sync"` // ISO-8601 timestamp of last successful push/pull
	SyncAccounts bool `json:"sync_accounts"` // include web accounts in the archive (multi-device identity)
	AutoSync     bool `json:"auto_sync"`     // periodic pull-merge + push cycle
}

// FleetConfig connects this device to a management hub as an agent: it
// heartbeats in and executes remote commands (join/leave networks).
type FleetConfig struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
	Name      string `json:"name"` // display name (default: hostname)
	HubUser   string `json:"hub_user,omitempty"` // hub account for automatic self-enrollment
	HubPass   string `json:"hub_pass,omitempty"`
	CertPin   string `json:"cert_pin,omitempty"` // pinned hub TLS cert fingerprint (TOFU)
}

// QuotaConfig holds the monthly traffic quota alert preferences.
type QuotaConfig struct {
	Enabled     bool    `json:"enabled"`
	MonthlyGB   float64 `json:"monthly_gb"`    // total (rx+tx) per calendar month
	WarnPercent int     `json:"warn_percent"`  // notify at this usage percentage (default 80)
}

// AlertConfig holds the peer-watchdog notification preferences.
type AlertConfig struct {
	Enabled           bool    `json:"enabled"`
	OfflineNotify     bool    `json:"offline_notify"`
	HighLatencyNotify bool    `json:"high_latency_notify"`
	HighLatencyMs     float64 `json:"high_latency_ms"` // alert when RTT exceeds this
	WebhookURL        string  `json:"webhook_url"`     // optional POST target for events
}

// Settings is the persisted application configuration.
type Settings struct {
	// Web management server
	WebBind  string `json:"web_bind"`  // "127.0.0.1" (default) or "0.0.0.0"
	WebPort  int    `json:"web_port"`  // 0 = random free port
	WebToken string `json:"web_token"` // API access token (X-Auth-Token header)
	WebHTTPS bool   `json:"web_https"` // serve the web API over TLS with the managed self-signed certificate

	// Web admin account (password auth). Empty hash = not configured yet:
	// the web UI then falls back to token-only access and prompts setup.
	WebUsername     string `json:"web_username"`
	WebPasswordHash string `json:"web_password_hash"`

	// MagicDNS-lite: keep a hosts-file block mapping peer hostnames to IPs
	MagicDNS bool `json:"magic_dns"`

	// Fleet: connect this device as an agent to a management hub
	Fleet FleetConfig `json:"fleet"`

	// Paths
	LogDir    string `json:"log_dir"`    // empty = default (appdata/logs)
	ConfigDir string `json:"config_dir"` // empty = default (appdata/configs)

	// Custom CSS injected into the web UI
	CustomCSS string `json:"custom_css"`

	// Core version management: which downloaded EasyTier release the GUI
	// launches ("" = bundled version), and an optional download mirror
	// prefix (e.g. https://ghproxy.example.com/) prepended to GitHub URLs.
	CoreVersion string `json:"core_version"`
	CoreMirror  string `json:"core_mirror"`

	// Launch the GUI (minimized to tray) at login
	AutoStart bool `json:"auto_start"`

	// Peer watchdog notifications
	Alerts AlertConfig `json:"alerts"`

	// Monthly traffic quota alerts
	Quota QuotaConfig `json:"quota"`

	// Application log level ("trace"|"debug"|"info"|"warn"|"error")
	AppLogLevel string `json:"app_log_level"`

	// WebDAV cloud sync
	Webdav WebdavConfig `json:"webdav"`
}

// Store manages loading and saving the settings file.
type Store struct {
	mu       sync.Mutex
	filePath string
	cached   Settings
	loaded   bool
}

// NewStore creates a store backed by a JSON file in the given directory.
func NewStore(dir string) *Store {
	return &Store{
		filePath: filepath.Join(dir, "settings.json"),
	}
}

// Load reads settings from disk. Returns the default when the file does not
// exist yet.
func (s *Store) Load() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loaded {
		return s.cached, nil
	}

	cfg := defaultSettings()
	data, err := os.ReadFile(s.filePath)
	if err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			// Corrupt file — return defaults but do not erase the file.
			return cfg, fmt.Errorf("settings file corrupt: %w", err)
		}
	}
	// Apply defaults for missing fields
	cfg = applyDefaults(cfg)

	s.cached = cfg
	s.loaded = true
	return cfg, nil
}

// Save writes the settings to disk.
func (s *Store) Save(cfg Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg = applyDefaults(cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.filePath, data, 0o644); err != nil {
		return err
	}
	s.cached = cfg
	s.loaded = true
	return nil
}

// Path returns the settings file path.
func (s *Store) Path() string { return s.filePath }

func defaultSettings() Settings {
	return Settings{
		WebBind: "127.0.0.1",
		WebPort: 0,
	}
}

func applyDefaults(cfg Settings) Settings {
	if cfg.WebBind == "" {
		cfg.WebBind = "127.0.0.1"
	}
	if cfg.Alerts.HighLatencyMs <= 0 {
		cfg.Alerts.HighLatencyMs = 200
	}
	if cfg.Quota.WarnPercent <= 0 {
		cfg.Quota.WarnPercent = 80
	}
	if cfg.AppLogLevel == "" {
		cfg.AppLogLevel = "info"
	}
	return cfg
}
