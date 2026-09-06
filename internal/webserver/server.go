package webserver

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"easytier-pro-gui/internal/audit"
	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/auth"
	"easytier-pro-gui/internal/configmgr"
	"easytier-pro-gui/internal/coremgr"
	"easytier-pro-gui/internal/devices"
	"easytier-pro-gui/internal/easytier"
	"easytier-pro-gui/internal/fleet"
	"easytier-pro-gui/internal/lease"
	"easytier-pro-gui/internal/traffic"
	"easytier-pro-gui/internal/tunnel"
)

// Handler is the minimal backend surface the web API mirrors.
type Handler interface {
	NodeInfo() (json.RawMessage, error)
	Peers() (json.RawMessage, error)
	Routes() (json.RawMessage, error)
	Stats() (json.RawMessage, error)
	VpnPortal() (json.RawMessage, error)
	CoreStatus() string
	StartCore() error
	StopCore() error
	RestartCore() error
	AppInfo() map[string]string
	Versions() easytier.Version
	ListConfigs() ([]configmgr.ConfigFile, error)
	SaveConfig(cfg configmgr.ConfigFile) error
	GetConfig(id string) (configmgr.ConfigFile, error)
	DeleteConfig(id string) error
	SetConfigEnabled(id string, enabled bool) error
	ApplyConfigs(configs []configmgr.ConfigFile, enabledIDs []string) error
	GetSettings() (string, error)
	SaveSettings(raw string) error
	OpenDir(path string) error
	WebdavPush() (string, error)
	WebdavPull() (string, error)
	TrafficHistory(days int) (traffic.History, error)
	ProbeMTU(target string) (int, error)
	SendTestNotify() error
	ListLeases() (map[string]lease.Lease, error)
	ForgetLease(network string) error
	SetNetworkRunning(id string, running bool) error
	WebAccount() (string, string) // username, password hash
	SetWebAccount(username, passwordHash string) error
	SetWebToken(token string) error
	Authenticate(username, password string) (auth.Account, bool)
	AuditLog(actor, ip, event, detail string)
	AuditTail(limit int) ([]audit.Entry, error)
	Metrics(activeSessions int) (string, error)
	WebTLSStatus() map[string]string
	RotateWebTLSCert() error
	ClearFleetPin() error
	ListTunnels() ([]tunnel.Tunnel, error)
	StartTunnel(target string) (tunnel.Tunnel, error)
	StartSSHTunnel(target string) (tunnel.Tunnel, error)
	SSHStatus() map[string]string
	StopTunnel(id string) error
	RetargetTunnel(id, target string) (tunnel.Tunnel, error)
	ClearTunnelHistory() (int, error)
	CloudflaredStatus() map[string]string
	InstallCloudflared() error
	TunnelPeers() []tunnel.Peer
	Accounts() []auth.Account
	SaveAccount(a auth.Account, password string) error
	DeleteAccount(username string) error
	SetAccountPassword(username, current, next string) error
	CoreReleases() map[string]any
	CoreInstalled() []coremgr.InstallInfo
	CoreInstall(tag string) error
	CoreSetActive(tag string) error
	CoreDelete(tag string) error
	DevicesList() []devices.Device
	DeviceApprove(peerID string, days int) error
	DeviceDenyCurrent(peerID, hostname, network, ipv4 string) error
	DeviceRemove(peerID string) error
	AppLogTail(limit int) []string
}

// Server serves the web management UI + JSON API over HTTP.
type Server struct {
	h       Handler
	assets  fs.FS
	token   string
	mu      sync.Mutex
	ln      net.Listener
	started bool
	cfgBind string // configured bind address
	cfgPort int    // configured port (0 = random)
	tlsCert *tls.Certificate // when set, serve HTTPS instead of plain HTTP
	mux     *http.ServeMux

	sessions *auth.SessionStore
	limiter  *auth.RateLimiter
	fleet    *fleet.Store
}

// NewServer creates a server with the given handler and assets.
// Use SetBind before Start to configure the address, or Start() will use
// 127.0.0.1:0 (default).
func NewServer(h Handler, assets fs.FS) *Server {
	token := randomToken()
	return &Server{
		h:        h,
		assets:   assets,
		token:    token,
		// 30-day sliding window: browser logins survive an application
		// restart (sessions persist to web-sessions.json) without the
		// cookie expiring underneath a still-valid server session.
		sessions: auth.NewSessionStore(30*24*time.Hour, core.DefaultPaths.AppDataDir()),
		limiter:  auth.NewRateLimiter(),
	}
}

// SetBind configures the bind address and port for the next Start or Restart.
func (s *Server) SetBind(addr string, port int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfgBind = addr
	s.cfgPort = port
}

// SetTLSCert enables (cert != nil) or disables HTTPS for the next
// Start/Restart. A running server must be restarted to pick it up.
func (s *Server) SetTLSCert(cert *tls.Certificate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tlsCert = cert
}

// SetToken persists the auth token (e.g. from settings).
func (s *Server) SetToken(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if token != "" {
		s.token = token
	}
}

// Token returns the auth token used by the web UI.
func (s *Server) Token() string { s.mu.Lock(); defer s.mu.Unlock(); return s.token }

// KickUser revokes all sessions of one account (called on user deletion).
func (s *Server) KickUser(user string) int { return s.sessions.DeleteUser(user) }

// SessionRevoke revokes one session by ID (native GUI path).
func (s *Server) SessionRevoke(id string) error {
	if !s.sessions.Delete(id) {
		return fmt.Errorf("session not found")
	}
	return nil
}

// SessionsRevokeAllExcept revokes every session except keep; native GUI path
// passes "" to revoke everything.
func (s *Server) SessionsRevokeAllExcept(keep string) int { return s.sessions.DeleteExcept(keep) }

// SessionsList returns all active sessions (native GUI path).
func (s *Server) SessionsList() []auth.Session { return s.sessions.List() }

// SaveSessions flushes session state (including sliding-expiry refreshes)
// to disk so logins survive an application restart.
func (s *Server) SaveSessions() {
	s.sessions.Save()
}

// SetFleet attaches the device-management store (agent heartbeat + commands).
func (s *Server) SetFleet(fs2 *fleet.Store) { s.fleet = fs2 }

// Addr returns the bound address once running.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Start binds on the configured address (or 127.0.0.1:0) and serves the API.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return nil
	}

	bind := s.cfgBind
	if bind == "" {
		bind = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bind, s.cfgPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if s.tlsCert != nil {
		ln = tls.NewListener(ln, &tls.Config{
			Certificates: []tls.Certificate{*s.tlsCert},
			MinVersion:   tls.VersionTLS12,
		})
	}
	s.ln = ln
	s.started = true
	s.cfgBind = bind
	s.cfgPort = ln.Addr().(*net.TCPAddr).Port

	s.mux = http.NewServeMux()
	s.registerRoutes()

	go http.Serve(ln, s.mux)
	go s.writeInfoFile(ln.Addr().String())
	return nil
}

// Running reports whether the server currently holds a live listener.
// A Start() that failed on bind leaves it false so callers (the app's
// background loop) can retry until the port frees up.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

// Restart stops the server and starts it again with the current bind/port
// configuration. Returns nil when the server was not running.
func (s *Server) Restart() error {
	s.mu.Lock()
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
		s.started = false
	}
	s.mu.Unlock()
	return s.Start()
}

// Stop closes the listener.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		_ = s.ln.Close()
		s.ln = nil
		s.started = false
	}
}

func (s *Server) registerRoutes() {
	mux := s.mux
	mux.HandleFunc("/webconfig.json", s.handleWebConfig)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/me", s.requireToken(s.handleAuthMe))
	mux.HandleFunc("/api/auth/password", s.requireToken(s.handleAuthPassword))
	mux.HandleFunc("/api/auth/sessions", s.requireToken(s.handleSessions))
	mux.HandleFunc("/api/auth/sessions/revoke", s.requireToken(s.handleSessionRevoke))
	mux.HandleFunc("/api/auth/sessions/revoke-others", s.requireToken(s.handleSessionRevokeOthers))
	mux.HandleFunc("/api/users", s.requireToken(s.handleUsers))
	mux.HandleFunc("/api/auth/rotate-token", s.requireAdmin(s.handleRotateToken))
	mux.HandleFunc("/api/status", s.requireToken(s.handleStatus))
	mux.HandleFunc("/api/node", s.requireToken(s.handleNode))
	mux.HandleFunc("/api/peers", s.requireToken(s.handlePeers))
	mux.HandleFunc("/api/routes", s.requireToken(s.handleRoutes))
	mux.HandleFunc("/api/stats", s.requireToken(s.handleStats))
	mux.HandleFunc("/api/configs", s.requireToken(s.handleConfigs))
	mux.HandleFunc("/api/config", s.requireToken(s.handleConfig))
	mux.HandleFunc("/api/apply", s.requireAdmin(s.handleApply))
	mux.HandleFunc("/api/start", s.requireAdmin(s.handleStart))
	mux.HandleFunc("/api/stop", s.requireAdmin(s.handleStop))
	mux.HandleFunc("/api/restart", s.requireAdmin(s.handleRestart))
	mux.HandleFunc("/api/settings", s.requireToken(s.handleSettings))
	mux.HandleFunc("/api/open-dir", s.requireAdmin(s.handleOpenDir))
	mux.HandleFunc("/api/webdav/push", s.requireAdmin(s.handleWebdavPush))
	mux.HandleFunc("/api/webdav/pull", s.requireAdmin(s.handleWebdavPull))
	mux.HandleFunc("/api/traffic", s.requireToken(s.handleTraffic))
	mux.HandleFunc("/api/leases", s.requireToken(s.handleLeases))
	mux.HandleFunc("/api/network-running", s.requireToken(s.handleNetworkRunning))
	mux.HandleFunc("/api/tools/mtu-probe", s.requireToken(s.handleMTUProbe))
	mux.HandleFunc("/api/tools/notify-test", s.requireAdmin(s.handleNotifyTest))
	mux.HandleFunc("/api/fleet", s.requireToken(s.handleFleet))
	mux.HandleFunc("/api/fleet/command", s.requireToken(s.handleFleetCommand))
	mux.HandleFunc("/api/fleet/self-enroll", s.requireToken(s.handleFleetSelfEnroll))
	mux.HandleFunc("/api/agent/heartbeat", s.handleAgentHeartbeat)
	mux.HandleFunc("/api/agent/result", s.handleAgentResult)
	mux.HandleFunc("/api/tunnels", s.requireToken(s.handleTunnels))
	mux.HandleFunc("/api/tunnels/install", s.requireToken(s.handleTunnelInstall))
	mux.HandleFunc("/api/tunnel-peers", s.requireToken(s.handleTunnelPeers))
	mux.HandleFunc("/api/web-tls", s.requireToken(s.handleWebTLS))
	mux.HandleFunc("/api/web-tls/rotate", s.requireAdmin(s.handleWebTLSRotate))
	mux.HandleFunc("/api/fleet/clear-pin", s.requireToken(s.handleClearFleetPin))
	mux.HandleFunc("/api/audit", s.requireAdmin(s.handleAudit))
	mux.HandleFunc("/api/metrics", s.requireToken(s.handleMetrics))
	mux.HandleFunc("/api/core/releases", s.requireAdmin(s.handleCoreReleases))
	mux.HandleFunc("/api/core/installed", s.requireAdmin(s.handleCoreInstalled))
	mux.HandleFunc("/api/core/install", s.requireAdmin(s.handleCoreInstall))
	mux.HandleFunc("/api/core/active", s.requireAdmin(s.handleCoreActive))
	mux.HandleFunc("/api/core/delete", s.requireAdmin(s.handleCoreDelete))
	mux.HandleFunc("/api/devices", s.requireAdmin(s.handleDevices))
	mux.HandleFunc("/api/devices/act", s.requireAdmin(s.handleDeviceAct))
	mux.HandleFunc("/api/applog", s.requireAdmin(s.handleAppLog))
	mux.Handle("/", http.FileServer(http.FS(s.assets)))
}

// grantNetworks returns the caller's per-network read grant: ok=false means
// unrestricted (admin, API token, or operator/viewer with an empty grant —
// same semantics as the operator write grant). Otherwise the map lists the
// network names the account is bound to.
func (s *Server) grantNetworks(r *http.Request) (map[string]bool, bool) {
	if s.authorizedTokenOnly(r) {
		return nil, false
	}
	sess, ok := s.currentSession(r)
	if !ok || sess.Role == "admin" {
		return nil, false
	}
	if len(sess.Networks) == 0 {
		return nil, false
	}
	m := make(map[string]bool, len(sess.Networks))
	for _, n := range sess.Networks {
		m[n] = true
	}
	return m, true
}

// filterRows drops per-instance rows outside the caller's network grant.
// raw is the multi-instance array shape [{instance_id,...,result}]; unknown
// shapes pass through untouched. Applied to every read endpoint so viewers
// and scoped operators only observe their own networks.
func (s *Server) filterRows(r *http.Request, raw json.RawMessage) json.RawMessage {
	allowed, restricted := s.grantNetworks(r)
	if !restricted {
		return raw
	}
	nets := map[string]string{}
	if cfgs, err := s.h.ListConfigs(); err == nil {
		for _, c := range cfgs {
			nets[c.InstanceID] = c.Network
		}
	}
	var rows []struct {
		InstanceID string          `json:"instance_id"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil || len(rows) == 0 {
		return raw
	}
	kept := make([]json.RawMessage, 0, len(rows))
	for _, row := range rows {
		if allowed[nets[row.InstanceID]] || allowed[row.InstanceID] {
			if b, err := json.Marshal(row); err == nil {
				kept = append(kept, b)
			}
		}
	}
	out, _ := json.Marshal(kept)
	return out
}

// filterConfigs keeps only the configs inside the caller's network grant.
func (s *Server) filterConfigs(r *http.Request, configs []configmgr.ConfigFile) []configmgr.ConfigFile {
	allowed, restricted := s.grantNetworks(r)
	if !restricted {
		return configs
	}
	out := configs[:0]
	for _, c := range configs {
		if allowed[c.Network] || allowed[c.InstanceID] {
			out = append(out, c)
		}
	}
	return out
}

// requireAdmin wraps a handler so only admin-role sessions (or API-token
// callers) may pass; other authenticated users get 403.
func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireToken(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAdmin(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
			return
		}
		next(w, r)
	})
}

func (s *Server) handleLeases(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		leases, err := s.h.ListLeases()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, leases)
	case http.MethodPost:
		var body struct {
			Network string `json:"network"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || strings.TrimSpace(body.Network) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid network"})
			return
		}
		if !s.canOperateNetwork(r, body.Network) {
			s.denyNetwork(w, r, body.Network, "lease-forget")
			return
		}
		if err := s.h.ForgetLease(body.Network); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET or POST required"})
	}
}

func (s *Server) handleNetworkRunning(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		ID      string `json:"id"`
		Running bool   `json:"running"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	network := body.ID
	if cfg, err := s.h.GetConfig(body.ID); err == nil && cfg.Network != "" {
		network = cfg.Network
	}
	if !s.canOperateNetwork(r, network) {
		s.denyNetwork(w, r, network, "network-running")
		return
	}
	if err := s.h.SetNetworkRunning(body.ID, body.Running); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleFleet is the hub-side device management API (admin): list agents +
// recent commands, enroll a new agent, delete one.
func (s *Server) handleFleet(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) || s.fleet == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		agents := s.fleet.Agents()
		if agents == nil {
			agents = []fleet.Agent{}
		}
		commands := s.fleet.Commands()
		if commands == nil {
			commands = []fleet.Command{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"agents":   agents,
			"commands": commands,
		})
	case http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		agent, token, err := s.fleet.CreateAgent(strings.TrimSpace(body.Name))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// The raw token is returned exactly once.
		writeJSON(w, http.StatusOK, map[string]any{"agent": agent, "token": token})
	case http.MethodDelete:
		if err := s.fleet.DeleteAgent(r.URL.Query().Get("id")); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET/POST/DELETE required"})
	}
}

// handleFleetCommand queues a remote command for one enrolled device (admin).
func (s *Server) handleFleetCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	if !s.isAdmin(r) || s.fleet == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	var body struct {
		AgentID     string `json:"agent_id"`
		Action      string `json:"action"`
		NetworkID   string `json:"network_id"`
		NetworkTOML string `json:"network_toml"`
		Target      string `json:"target"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	// The pushed TOML must not carry this hub's per-device identity.
	if body.NetworkTOML != "" {
		body.NetworkTOML = configmgr.SharedTOML(body.NetworkTOML)
	}
	cmd, err := s.fleet.Enqueue(fleet.Command{
		AgentID: body.AgentID, Action: body.Action,
		NetworkID: body.NetworkID, NetworkTOML: body.NetworkTOML, Target: body.Target,
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, cmd)
}

// handleAgentHeartbeat is the agent check-in: authenticate by token, refresh
// the reported state, hand back pending commands.
func (s *Server) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.fleet == nil {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "fleet unavailable"})
		return
	}
	agent, ok := s.fleet.Authenticate(r.Header.Get("X-Agent-Token"))
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid agent token"})
		return
	}
	var hb fleet.Heartbeat
	if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&hb); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	commands := s.fleet.Heartbeat(agent.ID, hb)
	if commands == nil {
		commands = []fleet.Command{}
	}
	writeJSON(w, http.StatusOK, commands)
}

// handleAgentResult records a command outcome reported by the agent.
func (s *Server) handleAgentResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.fleet == nil {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "fleet unavailable"})
		return
	}
	if _, ok := s.fleet.Authenticate(r.Header.Get("X-Agent-Token")); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid agent token"})
		return
	}
	var body struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Result string `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Status != fleet.StatusDone && body.Status != fleet.StatusFailed {
		body.Status = fleet.StatusDone
	}
	if err := s.fleet.SetResult(body.ID, body.Status, body.Result); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleTunnels manages cloudflared quick tunnels (admin): list, start, stop.
// Every path here is admin-gated — exposing a port publicly is a privileged
// operation.
func (s *Server) handleTunnels(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		tunnels, err := s.h.ListTunnels()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tunnels == nil {
			tunnels = []tunnel.Tunnel{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"tunnels": tunnels, "cloudflared": s.h.CloudflaredStatus(), "ssh": s.h.SSHStatus()})
	case http.MethodPost:
		var body struct {
			Target   string `json:"target"`
			Provider string `json:"provider"` // "cloudflared" (default) | "ssh" (localhost.run)
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		var t tunnel.Tunnel
		var err error
		if body.Provider == "ssh" {
			t, err = s.h.StartSSHTunnel(body.Target)
		} else {
			t, err = s.h.StartTunnel(body.Target)
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, t)
	case http.MethodDelete:
		if err := s.h.StopTunnel(r.URL.Query().Get("id")); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodPut:
		var body struct {
			ID     string `json:"id"`
			Target string `json:"target"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		t, err := s.h.RetargetTunnel(body.ID, body.Target)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, t)
	case http.MethodPatch:
		n, err := s.h.ClearTunnelHistory()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]int{"cleared": n})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET/POST/PUT/PATCH/DELETE required"})
	}
}

// handleTunnelInstall downloads the cloudflared binary (admin, blocking).
func (s *Server) handleTunnelInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	if !s.isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	if err := s.h.InstallCloudflared(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.h.CloudflaredStatus())
}

// handleTunnelPeers serves the mesh devices for the tunnel target picker.
func (s *Server) handleTunnelPeers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.h.TunnelPeers())
}

// handleWebTLS reports the HTTPS mode + self-signed cert fingerprint.
func (s *Server) handleWebTLS(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.h.WebTLSStatus())
}

// handleWebTLSRotate re-mints the self-signed certificate (admin only).
func (s *Server) handleWebTLSRotate(w http.ResponseWriter, r *http.Request) {
	if err := s.h.RotateWebTLSCert(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.h.WebTLSStatus())
}

// handleClearFleetPin forgets the pinned hub certificate on this agent.
func (s *Server) handleClearFleetPin(w http.ResponseWriter, r *http.Request) {
	if err := s.h.ClearFleetPin(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAudit serves the newest audit entries (admin only).
func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 5000 {
		limit = v
	}
	entries, err := s.h.AuditTail(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

// handleMetrics renders Prometheus text exposition. Gated like every other
// endpoint (session cookie, X-Auth-Token, or Authorization: Bearer).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := s.h.Metrics(len(s.sessions.List()))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// handleFleetSelfEnroll lets a device that is already logged in with an
// admin account enroll itself as an agent (no manual token copying).
func (s *Server) handleFleetSelfEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	if !s.isAdmin(r) || s.fleet == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	sess, _ := s.currentSession(r)
	actor := "api"
	if sess.User != "" {
		actor = sess.User
	}
	agent, token, err := s.fleet.CreateAgent(strings.TrimSpace(body.Name))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	s.h.AuditLog(actor, clientKey(r), "fleet.self_enroll", agent.ID+" ("+agent.Name+")")
	// The raw token is returned exactly once.
	writeJSON(w, http.StatusOK, map[string]any{"agent": agent, "token": token})
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {

	days := 7
	if v, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && v > 0 {
		days = v
	}
	h, err := s.h.TrafficHistory(days)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h)
}

func (s *Server) handleMTUProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || strings.TrimSpace(body.Target) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid target"})
		return
	}
	mtu, err := s.h.ProbeMTU(body.Target)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"mtu": mtu})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, _ *http.Request) {
	if err := s.h.SendTestNotify(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// writeInfoFile persists the endpoint + token to app-data so the UI and tests
// can discover the web management address.
func (s *Server) writeInfoFile(addr string) {
	base := os.Getenv("APPDATA")
	if base == "" {
		return
	}
	dir := filepath.Join(base, "easytier-pro-gui")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	payload := fmt.Sprintf(`{"addr":"%s","token":"%s"}`, addr, s.token)
	_ = os.WriteFile(filepath.Join(dir, "web.info"), []byte(payload), 0o600)
}

func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

// sessionID extracts the session token from the auth cookie or X-Session header.
func (s *Server) sessionID(r *http.Request) string {
	if c, err := r.Cookie("et_session"); err == nil && c.Value != "" {
		return c.Value
	}
	return r.Header.Get("X-Session")
}

// currentSession returns the caller's session when authenticated via cookie.
func (s *Server) currentSession(r *http.Request) (auth.Session, bool) {
	return s.sessions.Get(s.sessionID(r))
}

// isAdmin reports whether the caller may perform management writes: an
// admin-role session or an API-token request.
func (s *Server) isAdmin(r *http.Request) bool {
	if r.Header.Get("X-Auth-Token") != "" && s.authorizedTokenOnly(r) {
		return true
	}
	if sess, ok := s.currentSession(r); ok {
		return sess.Role == "admin"
	}
	return false
}

// callerRole returns the authenticated caller's role; API-token callers act
// as the owner (admin).
func (s *Server) callerRole(r *http.Request) string {
	if s.authorizedTokenOnly(r) {
		return "admin"
	}
	if sess, ok := s.currentSession(r); ok {
		return sess.Role
	}
	return ""
}

// callerUser returns the display name for audit entries.
func (s *Server) callerUser(r *http.Request) string {
	if sess, ok := s.currentSession(r); ok {
		return sess.User
	}
	return "token"
}

// canOperateNetwork reports whether the caller may start/stop/edit one
// network: admins (and API tokens) always may; operators only when the
// network is in their grant — an empty grant means every network. Viewers
// never may.
func (s *Server) canOperateNetwork(r *http.Request, network string) bool {
	if s.callerRole(r) != "operator" {
		return s.isAdmin(r)
	}
	sess, ok := s.currentSession(r)
	if !ok {
		return false
	}
	if len(sess.Networks) == 0 {
		return true
	}
	for _, n := range sess.Networks {
		if n == network {
			return true
		}
	}
	return false
}

// denyNetwork writes the 403 and audits the denied attempt.
func (s *Server) denyNetwork(w http.ResponseWriter, r *http.Request, network, action string) {
	s.h.AuditLog(s.callerUser(r), clientKey(r), "network.denied", action+" "+network)
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized for network " + network})
}

// accountConfigured reports whether a web admin account (password auth) is
// set up. Before setup the UI falls back to the legacy token bootstrap.
func (s *Server) accountConfigured() bool {
	if len(s.h.Accounts()) > 0 {
		return true
	}
	user, hash := s.h.WebAccount()
	return user != "" && hash != ""
}

// authorized validates the request: a matching API token (X-Auth-Token) or a
// valid login session (cookie / X-Session).
func (s *Server) authorized(r *http.Request) bool {
	if s.authorizedTokenOnly(r) {
		return true
	}
	if sid := s.sessionID(r); sid != "" {
		if _, ok := s.sessions.Get(sid); ok {
			return true
		}
	}
	return false
}

// authorizedTokenOnly checks the static API-token header (X-Auth-Token, or a
// standard Authorization: Bearer <token> for Prometheus/CI scrapers).
func (s *Server) authorizedTokenOnly(r *http.Request) bool {
	tok := r.Header.Get("X-Auth-Token")
	if tok == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			tok = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	if tok != "" && s.token != "" &&
		subtle.ConstantTimeCompare([]byte(tok), []byte(s.token)) == 1 {
		return true
	}
	return false
}

func (s *Server) handleWebConfig(w http.ResponseWriter, _ *http.Request) {
	// Once an admin account exists the static token is no longer handed out
	// here — knowing the address alone must not grant management access.
	if s.accountConfigured() {
		writeJSON(w, http.StatusOK, map[string]any{"auth_required": true})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": false, "token": s.token})
}

// clientKey identifies the login rate-limit bucket (client IP).
func clientKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	if !s.accountConfigured() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no web account configured"})
		return
	}
	ip := clientKey(r)
	if ok, wait := s.limiter.Allowed(ip); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "too many failed attempts", "retry_after_seconds": int(wait.Seconds()) + 1,
		})
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	acc, ok := s.h.Authenticate(body.Username, body.Password)
	if !ok {
		s.limiter.Fail(ip)
		s.h.AuditLog(body.Username, ip, "login.fail", "wrong credentials")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "wrong username or password"})
		return
	}
	s.limiter.Reset(ip)
	s.h.AuditLog(acc.Username, ip, "login.ok", "")
	sid := s.sessions.Create(auth.Session{
		User:     acc.Username,
		Role:     acc.Role,
		Networks: acc.Networks,
		IP:       ip,
		UA:       r.UserAgent(),
	})
	s.setSessionCookie(w, sid, 30*24*3600) // match the 30-day sliding TTL
	writeJSON(w, http.StatusOK, map[string]any{
		"username": acc.Username, "session": sid,
		"role": acc.Role, "networks": acc.Networks,
	})
}

// setSessionCookie writes the session cookie; Secure is set when the server
// runs in TLS mode so the cookie never rides a plaintext request.
func (s *Server) setSessionCookie(w http.ResponseWriter, value string, maxAge int) {
	s.mu.Lock()
	tlsMode := s.tlsCert != nil
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name: "et_session", Value: value, Path: "/",
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
		Secure: tlsMode,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sid := s.sessionID(r); sid != "" {
		s.sessions.Delete(sid)
	}
	s.setSessionCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if sess, ok := s.currentSession(r); ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"username": sess.User, "role": sess.Role, "networks": sess.Networks,
		})
		return
	}
	// API-token callers act as the owner (admin).
	user, _ := s.h.WebAccount()
	writeJSON(w, http.StatusOK, map[string]any{
		"username": user, "role": "admin",
	})
}

// handleAuthPassword is self-service password change for the logged-in user
// (any role). Admin-created accounts are changed via /api/users.
func (s *Server) handleAuthPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
		Username        string `json:"username"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if len(body.NewPassword) < 6 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password too short (min 6)"})
		return
	}
	user := body.Username
	if sess, ok := s.currentSession(r); ok && user == "" {
		user = sess.User
	}
	if user == "" {
		user, _ = s.h.WebAccount()
	}
	if user == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username required"})
		return
	}
	if err := s.h.SetAccountPassword(user, body.CurrentPassword, body.NewPassword); err != nil {
		s.h.AuditLog(user, clientKey(r), "auth.password_change.fail", err.Error())
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	s.h.AuditLog(user, clientKey(r), "auth.password_change", "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSessions lists active logins. Admins see all devices; viewers only
// their own.
func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	caller, hasCaller := s.currentSession(r)
	out := []map[string]any{}
	for _, sess := range s.sessions.List() {
		if hasCaller && caller.Role != "admin" && sess.User != caller.User {
			continue
		}
		out = append(out, map[string]any{
			"id": sess.ID, "user": sess.User, "role": sess.Role,
			"ip": sess.IP, "ua": sess.UA,
			"created":   sess.Created.UTC().Format(time.RFC3339),
			"last_seen": sess.LastSeen.UTC().Format(time.RFC3339),
			"current":   hasCaller && sess.ID == caller.ID,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionRevoke revokes one login session: admins may revoke any
// device, users may revoke their own.
func (s *Server) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	caller, hasCaller := s.currentSession(r)
	if !hasCaller && !s.isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	if hasCaller && caller.Role != "admin" {
		// Non-admin may only revoke own sessions.
		target, ok := s.sessions.Get(body.ID)
		if !ok || target.User != caller.User {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "not your session"})
			return
		}
	}
	s.h.AuditLog(caller.User, clientKey(r), "session.revoke", body.ID)
	if !s.sessions.Delete(body.ID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSessionRevokeOthers revokes every other login of the same user
// (admins revoke all other sessions regardless of user).
func (s *Server) handleSessionRevokeOthers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	caller, hasCaller := s.currentSession(r)
	if !hasCaller {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "session required"})
		return
	}
	revoked := 0
	for _, sess := range s.sessions.List() {
		if sess.ID == caller.ID {
			continue
		}
		if caller.Role == "admin" || sess.User == caller.User {
			if s.sessions.Delete(sess.ID) {
				revoked++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "revoked": revoked})
}

// handleUsers manages web accounts (admin only): list / upsert / delete.
// Password hashes never leave the server.
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		list := []map[string]any{}
		for _, a := range s.h.Accounts() {
			list = append(list, map[string]any{
				"username": a.Username, "role": a.Role,
				"networks": a.Networks, "created": a.Created,
			})
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body struct {
			Username string   `json:"username"`
			Password string   `json:"password"`
			Role     string   `json:"role"`
			Networks []string `json:"networks"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 16384)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		body.Username = strings.TrimSpace(body.Username)
		if body.Username == "" || len(body.Username) > 64 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid username"})
			return
		}
		if body.Role != "admin" && body.Role != "operator" && body.Role != "viewer" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be admin, operator or viewer"})
			return
		}
		if err := s.h.SaveAccount(auth.Account{
			Username: body.Username, Role: body.Role, Networks: body.Networks,
		}, body.Password); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		s.h.AuditLog(body.Username, clientKey(r), "user.create", "role="+body.Role)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
			return
		}
		if err := s.h.DeleteAccount(name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		// Kick any active logins of the removed account.
		s.sessions.DeleteUser(name)
		s.h.AuditLog(name, clientKey(r), "user.delete", "")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET/POST/DELETE required"})
	}
}

// handleRotateToken replaces the API access token (X-Auth-Token for scripts).
func (s *Server) handleRotateToken(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	tok := hex.EncodeToString(b)
	if err := s.h.SetWebToken(tok); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.SetToken(tok)
	s.h.AuditLog("admin", clientKey(r), "auth.rotate_token", "")
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   s.h.CoreStatus(),
		"addr":     s.h.AppInfo(),
		"versions": s.h.Versions(),
	})
}

func (s *Server) handleNode(w http.ResponseWriter, _ *http.Request) {
	v, err := s.h.NodeInfo()
	if err != nil {
		if isCoreDown(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("null"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(v)
}

func (s *Server) handlePeers(w http.ResponseWriter, _ *http.Request) {
	v, err := s.h.Peers()
	if err != nil {
		if isCoreDown(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(v)
}

func (s *Server) handleRoutes(w http.ResponseWriter, _ *http.Request) {
	v, err := s.h.Routes()
	if err != nil {
		if isCoreDown(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(v)
}

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request) {
	v, err := s.h.Stats()
	if err != nil {
		if isCoreDown(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(v)
}

// isCoreDown reports whether err is the expected "core not running" condition
// so polled data endpoints can return empty 200s instead of noisy 500s.
func isCoreDown(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not running")
}

func (s *Server) handleConfigs(w http.ResponseWriter, r *http.Request) {
	configs, err := s.h.ListConfigs()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, s.filterConfigs(r, configs))
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		cfg, err := s.h.GetConfig(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut, http.MethodPost:
		var body struct {
			ID      string `json:"id"`
			Raw     string `json:"raw"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		network := body.ID
		if cfg, err := s.h.GetConfig(body.ID); err == nil && cfg.Network != "" {
			network = cfg.Network
		}
		if !s.canOperateNetwork(r, network) {
			s.denyNetwork(w, r, network, "config-write")
			return
		}
		cfg := configmgr.ConfigFile{
			InstanceID: body.ID,
			Enabled:    body.Enabled,
			Raw:        body.Raw,
		}
		// If raw is empty this is an enable/disable toggle: flip the flag and
		// bring the instance process in line (SetConfigEnabled starts/stops
		// just that instance — no global restart, other networks unaffected).
		if strings.TrimSpace(body.Raw) == "" {
			if err := s.h.SetConfigEnabled(body.ID, body.Enabled); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "toggled"})
			return
		}
		if err := s.h.SaveConfig(cfg); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		network := id
		if cfg, err := s.h.GetConfig(id); err == nil && cfg.Network != "" {
			network = cfg.Network
		}
		if !s.canOperateNetwork(r, network) {
			s.denyNetwork(w, r, network, "config-delete")
			return
		}
		if err := s.h.DeleteConfig(id); err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

// handleApply persists configs and reconciles running instances in one call
// (browser fallback for App.ApplyConfigs). Per-instance reconcile means only
// changed networks restart — never a global stop of every running network.
func (s *Server) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return
	}
	var body struct {
		Configs    []configmgr.ConfigFile `json:"configs"`
		EnabledIDs []string               `json:"enabled_ids"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if err := s.h.ApplyConfigs(body.Configs, body.EnabledIDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleStart(w http.ResponseWriter, _ *http.Request) {
	if err := s.h.StartCore(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func (s *Server) handleStop(w http.ResponseWriter, _ *http.Request) {
	if err := s.h.StopCore(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) handleRestart(w http.ResponseWriter, _ *http.Request) {
	if err := s.h.RestartCore(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && !s.isAdmin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin role required"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		raw, err := s.h.GetSettings()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(raw))
	case http.MethodPut:
		var body struct {
			Raw string `json:"raw"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := s.h.SaveSettings(body.Raw); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (s *Server) handleOpenDir(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path required"})
		return
	}
	if err := s.h.OpenDir(path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "opened"})
}

func (s *Server) handleWebdavPush(w http.ResponseWriter, _ *http.Request) {
	msg, err := s.h.WebdavPush()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"msg": msg})
}

func (s *Server) handleWebdavPull(w http.ResponseWriter, _ *http.Request) {
	msg, err := s.h.WebdavPull()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"msg": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func randomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// FormatDuration is a small helper kept for API parity / logging.
func FormatDuration(d time.Duration) string {
	return fmt.Sprintf("%s", d.Truncate(time.Second))
}

// ---- core version management (admin only) ----

func (s *Server) handleCoreReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.h.CoreReleases())
}

func (s *Server) handleCoreInstalled(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, s.h.CoreInstalled())
}

func (s *Server) handleCoreInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag required"})
		return
	}
	if err := s.h.CoreInstall(body.Tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "installed"})
}

func (s *Server) handleCoreActive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.h.CoreSetActive(body.Tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "switched"})
}

func (s *Server) handleCoreDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tag == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tag required"})
		return
	}
	if err := s.h.CoreDelete(body.Tag); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- device admission API (admin) ----

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	list := s.h.DevicesList()
	if list == nil {
		list = []devices.Device{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleDeviceAct performs one admission action:
// POST {action: "approve"|"deny"|"remove", peer_id, days?, hostname?, network?, ipv4?}
func (s *Server) handleDeviceAct(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var body struct {
		Action   string `json:"action"`
		PeerID   string `json:"peer_id"`
		Days     int    `json:"days"`
		Hostname string `json:"hostname"`
		Network  string `json:"network"`
		IPv4     string `json:"ipv4"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PeerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "peer_id required"})
		return
	}
	var err error
	switch body.Action {
	case "approve":
		err = s.h.DeviceApprove(body.PeerID, body.Days)
	case "deny":
		err = s.h.DeviceDenyCurrent(body.PeerID, body.Hostname, body.Network, body.IPv4)
	case "remove":
		err = s.h.DeviceRemove(body.PeerID)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action must be approve, deny or remove"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleAppLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	limit := 200
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	writeJSON(w, http.StatusOK, s.h.AppLogTail(limit))
}
