// Package audit is the append-only activity log: every security-relevant
// action (logins, config changes, core lifecycle, tunnels, fleet commands)
// lands here as a JSON line, queryable from the web console.
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is one audit record.
type Entry struct {
	Time   string `json:"time"`
	Actor  string `json:"actor"`  // who: username / "app" / agent name
	IP     string `json:"ip,omitempty"`
	Event  string `json:"event"` // login.ok / config.save / tunnel.start / ...
	Detail string `json:"detail,omitempty"`
}

// Store is a JSONL audit log capped at maxLines (oldest lines are trimmed
// when the cap is exceeded).
type Store struct {
	mu       sync.Mutex
	path     string
	maxLines int
}

// NewStore creates the audit log (audit.jsonl in the app data dir).
func NewStore(dir string) *Store {
	return &Store{path: filepath.Join(dir, "audit.jsonl"), maxLines: 5000}
}

// Log appends one entry; never fails loudly (best-effort).
func (s *Store) Log(actor, ip, event, detail string) {
	e := Entry{
		Time:   time.Now().Format(time.RFC3339Nano),
		Actor:  actor,
		IP:     ip,
		Event:  event,
		Detail: detail,
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()
	s.trimLocked()
}

// trimLocked rewrites the file keeping the newest maxLines entries when it
// grows past 1.5× the cap (amortized O(n log) instead of every write).
func (s *Store) trimLocked() {
	if s.maxLines <= 0 {
		return
	}
	f, err := os.Open(s.path)
	if err != nil {
		return
	}
	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		count++
	}
	f.Close()
	if count < s.maxLines+s.maxLines/2 {
		return
	}
	entries := s.Tail(s.maxLines)
	tmp := s.path + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	for _, e := range entries {
		data, _ := json.Marshal(e)
		out.Write(append(data, '\n'))
	}
	out.Close()
	_ = os.Rename(tmp, s.path)
}

// Tail returns the newest n entries, newest first.
func (s *Store) Tail(n int) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		n = 200
	}
	f, err := os.Open(s.path)
	if err != nil {
		return []Entry{}
	}
	defer f.Close()
	var all []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Event != "" {
			all = append(all, e)
		}
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	// newest first
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if all == nil {
		all = []Entry{}
	}
	return all
}
