// Package devices keeps the device admission list: peer IDs the admin has
// explicitly allowed (optionally with a credential expiry date) or denied.
// Unknown peers joining the mesh are surfaced as alerts; denied or expired
// peers are turned into ACL drop rules by the caller.
package devices

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status values for a device entry.
const (
	StatusAllow = "allow"
	StatusDeny  = "deny"
)

// Device is one known peer in the admission list.
type Device struct {
	PeerID  string `json:"peer_id"` // easytier peer id (decimal string)
	Name    string `json:"name,omitempty"`
	Network string `json:"network,omitempty"`
	IPv4    string `json:"ipv4,omitempty"`
	Status  string `json:"status"` // allow | deny
	Added   string `json:"added"`  // RFC3339
	Expires string `json:"expires,omitempty"` // RFC3339; empty = no expiry
	Note    string `json:"note,omitempty"`
}

// Expired reports whether the allow credential is past its expiry date.
func (d Device) Expired(now time.Time) bool {
	if d.Status != StatusAllow || d.Expires == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, d.Expires)
	if err != nil {
		// Plain date fallback (YYYY-MM-DD): expires at end of that day.
		t, err = time.ParseInLocation("2006-01-02", d.Expires, time.Local)
		if err != nil {
			return false
		}
		t = t.Add(24 * time.Hour)
	}
	return now.After(t)
}

// Store persists the admission list as JSON in <dir>/devices.json.
type Store struct {
	mu   sync.Mutex
	path string
	list []Device
}

// NewStore creates a store backed by a JSON file in the given directory.
func NewStore(dir string) *Store {
	s := &Store{path: filepath.Join(dir, "devices.json")}
	s.load()
	return s
}

func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &s.list)
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns the admission list sorted by added date (newest first).
func (s *Store) List() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Device, len(s.list))
	copy(out, s.list)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Added > out[j].Added
	})
	return out
}

// Get returns the entry for a peer ID.
func (s *Store) Get(peerID string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.list {
		if d.PeerID == peerID {
			return d, true
		}
	}
	return Device{}, false
}

// Approve allows a peer, optionally with a credential lifetime in days
// (0 = no expiry). Existing entries are updated in place.
func (s *Store) Approve(peerID, name, network, ipv4 string, days int, note string) (Device, error) {
	if strings.TrimSpace(peerID) == "" {
		return Device{}, os.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	dev := Device{PeerID: peerID, Status: StatusAllow, Added: now.Format(time.RFC3339)}
	if i := s.indexOf(peerID); i >= 0 {
		dev = s.list[i]
	}
	dev.Status = StatusAllow
	if name != "" {
		dev.Name = name
	}
	if network != "" {
		dev.Network = network
	}
	if ipv4 != "" {
		dev.IPv4 = ipv4
	}
	if days > 0 {
		dev.Expires = now.AddDate(0, 0, days).Format(time.RFC3339)
	} else {
		dev.Expires = ""
	}
	dev.Note = note
	if i := s.indexOf(peerID); i >= 0 {
		s.list[i] = dev
	} else {
		s.list = append(s.list, dev)
	}
	return dev, s.save()
}

// Touch refreshes the observed metadata (hostname/network/IP) of an entry
// without changing its status. Returns the updated entry.
func (s *Store) Touch(peerID, name, network, ipv4 string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(peerID)
	if i < 0 {
		return Device{}, false
	}
	if name != "" {
		s.list[i].Name = name
	}
	if network != "" {
		s.list[i].Network = network
	}
	if ipv4 != "" {
		s.list[i].IPv4 = ipv4
	}
	_ = s.save()
	return s.list[i], true
}

// Deny marks a peer as denied (blacklisted). Keeps known metadata so the
// caller can generate the ACL drop rule later.
func (s *Store) Deny(peerID, name, network, ipv4, note string) (Device, error) {
	if strings.TrimSpace(peerID) == "" {
		return Device{}, os.ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	dev := Device{PeerID: peerID, Status: StatusDeny, Added: time.Now().Format(time.RFC3339)}
	if i := s.indexOf(peerID); i >= 0 {
		dev = s.list[i]
	}
	dev.Status = StatusDeny
	dev.Expires = "" // a deny has no expiry
	if name != "" {
		dev.Name = name
	}
	if network != "" {
		dev.Network = network
	}
	if ipv4 != "" {
		dev.IPv4 = ipv4
	}
	if note != "" {
		dev.Note = note
	}
	if i := s.indexOf(peerID); i >= 0 {
		s.list[i] = dev
	} else {
		s.list = append(s.list, dev)
	}
	return dev, s.save()
}

// Remove deletes an entry entirely (the peer becomes unknown again).
func (s *Store) Remove(peerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.indexOf(peerID)
	if i < 0 {
		return false
	}
	s.list = append(s.list[:i], s.list[i+1:]...)
	return s.save() == nil
}

// ExpireDue moves expired allow entries to the deny list (credential
// lifetime reached) and returns them so the caller can raise alerts and
// regenerate ACL rules.
func (s *Store) ExpireDue(now time.Time) []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []Device
	changed := false
	for i := range s.list {
		d := s.list[i]
		if d.Expired(now) {
			d.Status = StatusDeny
			d.Expires = ""
			if d.Note == "" {
				d.Note = "credential expired"
			} else {
				d.Note = d.Note + " (credential expired)"
			}
			s.list[i] = d
			expired = append(expired, d)
			changed = true
		}
	}
	if changed {
		_ = s.save()
	}
	return expired
}

func (s *Store) indexOf(peerID string) int {
	for i := range s.list {
		if s.list[i].PeerID == peerID {
			return i
		}
	}
	return -1
}
