// Package fleet implements the multi-device management plane: one instance
// acts as the hub (its web UI is the console), every other device runs as an
// agent that heartbeats in and polls for commands (join/leave networks,
// start/stop the core). Agents authenticate with a hub-issued token.
package fleet

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

const onlineWindow = 45 * time.Second

// Command actions.
const (
	ActionStartNetwork = "start_network"
	ActionStopNetwork  = "stop_network"
	ActionDeleteNetwork  = "delete_network"
	ActionStartCore    = "start_core"
	ActionStopCore     = "stop_core"
	ActionStartTunnel  = "start_tunnel"
	ActionStopTunnel   = "stop_tunnel"
)

// Command statuses.
const (
	StatusPending   = "pending"
	StatusDelivered = "delivered"
	StatusDone      = "done"
	StatusFailed    = "failed"
)

// Agent is one enrolled device.
type Agent struct {
	ID        string    `json:"id"`
	TokenHash string    `json:"-"` // sha256 hex; the raw token is shown once
	Name      string    `json:"name"`
	OS        string    `json:"os"`
	Version   string    `json:"version"`
	IPv4      string    `json:"ipv4"`
	Networks  []string  `json:"networks"`
	AutoStart bool      `json:"auto_start"`
	Note      string    `json:"note"`
	LastSeen  time.Time `json:"last_seen"`
}

// Online reports whether the agent heartbeated recently.
func (a Agent) Online() bool { return time.Since(a.LastSeen) < onlineWindow }

// Command is one remote operation for an agent.
type Command struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	Action      string    `json:"action"`
	NetworkID   string    `json:"network_id,omitempty"`
	NetworkTOML string    `json:"network_toml,omitempty"`
	Target      string    `json:"target,omitempty"` // tunnel target for start/stop_tunnel
	Status      string    `json:"status"`
	Result      string    `json:"result,omitempty"`
	Created     time.Time `json:"created"`
}

// Heartbeat is what an agent reports on every check-in.
type Heartbeat struct {
	Name      string   `json:"name"`
	OS        string   `json:"os"`
	Version   string   `json:"version"`
	IPv4      string   `json:"ipv4"`
	Networks  []string `json:"networks"`
	AutoStart bool     `json:"auto_start"`
}

// Store persists agents + commands as a single JSON file (hub side).
type Store struct {
	mu       sync.Mutex
	path     string
	agents   []Agent
	commands []Command
}

type fileState struct {
	Agents   []Agent   `json:"agents"`
	Commands []Command `json:"commands"`
}

// NewStore creates the fleet store (fleet.json in the hub's data dir).
func NewStore(dir string) *Store {
	s := &Store{path: filepath.Join(dir, "fleet.json")}
	s.reload()
	return s
}

func (s *Store) reload() fileState {
	var st fileState
	data, err := os.ReadFile(s.path)
	if err == nil {
		_ = json.Unmarshal(data, &st)
	}
	s.agents = st.Agents
	s.commands = st.Commands
	return st
}

func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fileState{Agents: s.agents, Commands: s.commands}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// CreateAgent enrolls a new device and returns the one-time token.
func (s *Store) CreateAgent(name string) (Agent, string, error) {
	tok := newToken()
	id := uuid.NewString()
	agent := Agent{
		ID:        id,
		TokenHash: hashToken(tok),
		Name:      name,
		LastSeen:  time.Time{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents = append(s.agents, agent)
	if err := s.saveLocked(); err != nil {
		return Agent{}, "", err
	}
	return agent, tok, nil
}

// Agents lists all enrolled devices, offline ones last.
func (s *Store) Agents() []Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Agent(nil), s.agents...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Online() != out[j].Online() {
			return out[i].Online()
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// DeleteAgent removes an agent and its queued commands.
func (s *Store) DeleteAgent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.agents[:0]
	found := false
	for _, a := range s.agents {
		if a.ID == id {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return errors.New("agent not found")
	}
	s.agents = kept
	cmds := s.commands[:0]
	for _, c := range s.commands {
		if c.AgentID != id {
			cmds = append(cmds, c)
		}
	}
	s.commands = cmds
	return s.saveLocked()
}

// Authenticate resolves an agent by its raw token.
func (s *Store) Authenticate(token string) (Agent, bool) {
	h := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.agents {
		if subtle.ConstantTimeCompare([]byte(a.TokenHash), []byte(h)) == 1 {
			return a, true
		}
	}
	return Agent{}, false
}

// Heartbeat refreshes an agent's reported state and returns pending commands
// (marking them delivered).
func (s *Store) Heartbeat(agentID string, hb Heartbeat) []Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i, a := range s.agents {
		if a.ID != agentID {
			continue
		}
		if hb.Name != "" {
			s.agents[i].Name = hb.Name
		}
		s.agents[i].OS = hb.OS
		s.agents[i].Version = hb.Version
		s.agents[i].IPv4 = hb.IPv4
		s.agents[i].Networks = hb.Networks
		s.agents[i].AutoStart = hb.AutoStart
		s.agents[i].LastSeen = now
		break
	}
	var out []Command
	for i, c := range s.commands {
		if c.AgentID == agentID && c.Status == StatusPending {
			s.commands[i].Status = StatusDelivered
			out = append(out, s.commands[i])
		}
	}
	_ = s.saveLocked()
	return out
}

// Enqueue adds a command for an agent.
func (s *Store) Enqueue(cmd Command) (Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, a := range s.agents {
		if a.ID == cmd.AgentID {
			found = true
			break
		}
	}
	if !found {
		return Command{}, errors.New("agent not found")
	}
	cmd.ID = uuid.NewString()
	cmd.Status = StatusPending
	cmd.Created = time.Now()
	s.commands = append(s.commands, cmd)
	// Bound the history: drop finished commands older than a day.
	kept := s.commands[:0]
	for _, c := range s.commands {
		if c.Status == StatusDone || c.Status == StatusFailed {
			if time.Since(c.Created) > 24*time.Hour {
				continue
			}
		}
		kept = append(kept, c)
	}
	s.commands = kept
	if err := s.saveLocked(); err != nil {
		return Command{}, err
	}
	return cmd, nil
}

// SetResult records a command outcome reported by its agent.
func (s *Store) SetResult(commandID, status, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.commands {
		if c.ID == commandID {
			s.commands[i].Status = status
			s.commands[i].Result = result
			return s.saveLocked()
		}
	}
	return errors.New("command not found")
}

// Commands lists recent commands (newest first).
func (s *Store) Commands() []Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Command(nil), s.commands...)
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	if len(out) > 50 {
		out = out[:50]
	}
	return out
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func newToken() string {
	return uuid.NewString() + uuid.NewString()
}
