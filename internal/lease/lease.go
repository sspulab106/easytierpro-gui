// Package lease remembers the last DHCP-assigned virtual IP per network so
// the GUI can re-apply it as a static address on the next start ("sticky
// DHCP"): without this, the address a device gets depends on join order and
// may shuffle between reboots.
package lease

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Lease is one remembered virtual address.
type Lease struct {
	IP       string `json:"ip"`
	Prefix   int    `json:"prefix"`
	Hostname string `json:"hostname"`
	Updated  string `json:"updated"` // RFC3339
}

// Store persists leases as a single JSON file in the app-data dir.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore creates a store backed by <dir>/leases.json.
func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(dir, "leases.json")}
}

func (s *Store) load() map[string]Lease {
	out := map[string]Lease{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// All returns every remembered lease keyed by network name.
func (s *Store) All() map[string]Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load()
}

// Get returns the lease for a network.
func (s *Store) Get(network string) (Lease, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.load()[network]
	return l, ok
}

// Put records/refreshes the lease for a network.
func (s *Store) Put(network string, l Lease) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	l.Updated = time.Now().Format(time.RFC3339)
	all[network] = l
	s.save(all)
}

// Forget drops the lease for a network (returns true if it existed).
func (s *Store) Forget(network string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	if _, ok := all[network]; !ok {
		return false
	}
	delete(all, network)
	s.save(all)
	return true
}

func (s *Store) save(all map[string]Lease) {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	_ = os.WriteFile(s.path, data, 0o644)
}
