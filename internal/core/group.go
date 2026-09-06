package core

import (
	"path/filepath"
	"strconv"
	"sync"
)

// Group runs one easytier-core process per network instance, the same model
// as the official GUI: starting/stopping/restarting a network touches only
// its own process, so other networks never drop. Each process gets:
//   - its own config file (--config <id>.toml from the shared config dir)
//   - its own RPC portal (for per-instance management queries)
//   - the shared log dir (log lines are prefixed with the instance id)
type Group struct {
	mu      sync.Mutex
	subs    map[string]*Process // instance id -> process
	portals map[string]string   // instance id -> rpc portal actually assigned
	next    int                 // next rpc portal slot candidate
}

// NewGroup creates an empty per-instance process group.
func NewGroup() *Group {
	return &Group{subs: map[string]*Process{}, portals: map[string]string{}, next: 1}
}

// allocatePortal picks a free 127.0.0.1:158xx slot for a new instance
// process (callers hold the lock).
func (g *Group) allocatePortal() string {
	for {
		port := 15888 + g.next
		g.next++
		addr := "127.0.0.1:" + strconv.Itoa(port)
		if !PingRpc(addr) {
			return addr
		}
		// slot occupied by a foreign process: try the next one
	}
}

// instanceArgs builds the per-instance launch arguments.
func (g *Group) instanceArgs(instanceID, portal string) []string {
	return []string{
		"--config-file", filepath.Join(DefaultPaths.ConfigDir(), instanceID+".toml"),
		"--rpc-portal", portal,
		"--file-log-dir", DefaultPaths.LogDir(),
		"--file-log-level", "info",
	}
}

// StartInstance launches the core process for one network instance; the
// config file must already exist in the config dir. Running instances are
// left untouched.
func (g *Group) StartInstance(instanceID string, onLog func(string), onStatus func(bool), onExit func(error)) error {
	g.mu.Lock()
	if p, ok := g.subs[instanceID]; ok && p.Running() {
		g.mu.Unlock()
		return nil // already up
	}
	portal := g.allocatePortal()
	p := NewProcess()
	p.SetArgs(g.instanceArgs(instanceID, portal))
	// Register before Start so synchronous start callbacks see the instance.
	g.subs[instanceID] = p
	g.portals[instanceID] = portal
	g.mu.Unlock()

	p.SetLogCallback(func(line string) {
		if onLog != nil {
			onLog("[" + instanceID[:minLen(instanceID, 8)] + "] " + line)
		}
	})
	p.SetStatusCallback(onStatus)
	p.SetExitCallback(func(err error) {
		g.mu.Lock()
		if cur, ok := g.subs[instanceID]; ok && cur == p {
			delete(g.subs, instanceID)
			delete(g.portals, instanceID)
		}
		g.mu.Unlock()
		if onExit != nil {
			onExit(err)
		}
	})
	if err := p.Start(); err != nil {
		g.mu.Lock()
		delete(g.subs, instanceID)
		delete(g.portals, instanceID)
		g.mu.Unlock()
		return err
	}
	return nil
}

// StopInstance terminates only this instance's process; other networks keep
// running untouched.
func (g *Group) StopInstance(instanceID string) error {
	g.mu.Lock()
	p, ok := g.subs[instanceID]
	g.mu.Unlock()
	if !ok {
		return nil // not running: nothing to do
	}
	err := p.Stop()
	g.mu.Lock()
	delete(g.subs, instanceID)
	delete(g.portals, instanceID)
	g.mu.Unlock()
	return err
}

// RunningIDs lists the currently running instance ids.
func (g *Group) RunningIDs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.subs))
	for id, p := range g.subs {
		if p.Running() {
			out = append(out, id)
		}
	}
	return out
}

// IsRunning reports whether the instance has a live process of ours.
func (g *Group) IsRunning(instanceID string) bool {
	g.mu.Lock()
	p, ok := g.subs[instanceID]
	g.mu.Unlock()
	return ok && p.Running()
}

// PortalOf returns the rpc portal address the instance process was launched
// with (empty when it is not ours / not running).
func (g *Group) PortalOf(instanceID string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.portals[instanceID]
}

// StopAll terminates every instance process (app shutdown / global restart).
func (g *Group) StopAll() {
	g.mu.Lock()
	ps := make([]*Process, 0, len(g.subs))
	for _, p := range g.subs {
		ps = append(ps, p)
	}
	g.subs = map[string]*Process{}
	g.portals = map[string]string{}
	g.mu.Unlock()
	for _, p := range ps {
		_ = p.Stop()
	}
}

// Count returns how many instance processes are alive.
func (g *Group) Count() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, p := range g.subs {
		if p.Running() {
			n++
		}
	}
	return n
}

// Any reports whether at least one of our instance processes is alive, or a
// foreign (service-mode) core answers on the base portal.
func (g *Group) Any() bool {
	if g.Count() > 0 {
		return true
	}
	return PingRpc(RpcPortal)
}

// StopExternal kills a foreign easytier-core holding the base RPC portal
// (service-mode cores started outside this GUI).
func StopExternal() error {
	if !PingRpc(RpcPortal) {
		return nil
	}
	pid := pidListeningOn(RpcPortal)
	if pid <= 0 {
		return nil
	}
	return killByPID(pid)
}

func minLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}
