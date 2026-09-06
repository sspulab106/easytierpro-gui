// Package auth provides the web-management account primitives: scrypt
// password hashing, sliding-expiry sessions and a per-IP login rate limiter.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

// HashPassword derives a scrypt hash (format s1$N$r$p$salt$hash, all base64
// where applicable). Cost parameters are embedded so verification stays
// forward-compatible with cost bumps.
func HashPassword(pw string) (string, error) {
	const N, r, p = 32768, 8, 1
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk, err := scrypt.Key([]byte(pw), salt, N, r, p, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("s1$%d$%d$%d$%s$%s",
		N, r, p,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk)), nil
}

// VerifyPassword checks pw against a stored hash in constant time.
func VerifyPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[0] != "s1" {
		return false
	}
	N, err1 := strconv.Atoi(parts[1])
	r, err2 := strconv.Atoi(parts[2])
	p, err3 := strconv.Atoi(parts[3])
	salt, err4 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err5 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		return false
	}
	dk, err := scrypt.Key([]byte(pw), salt, N, r, p, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, want) == 1
}

// NewToken returns a random 256-bit hex token.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// Session is one active web login with its device metadata.
type Session struct {
	ID       string    `json:"id"`
	User     string    `json:"user"`
	Role     string    `json:"role"` // "admin" | "operator" | "viewer"
	Networks []string  `json:"networks,omitempty"` // operator grant: networks this user may operate
	IP       string    `json:"ip"`
	UA       string    `json:"ua"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

// SessionStore keeps sessions with a sliding expiry window, persisted to
// <dir>/web-sessions.json (0600) so browser sessions survive an application
// restart. Token IDs are 256-bit random values; the store is loaded with
// expired entries pruned, and every mutation is flushed to disk. Sliding
// LastSeen refreshes stay in memory and are persisted lazily by Save() to
// avoid a disk write on every authenticated request.
type SessionStore struct {
	mu   sync.Mutex
	ttl  time.Duration
	path string
	sess map[string]*Session
}

// NewSessionStore creates a store with the given sliding TTL. dir may be ""
// for a memory-only store (tests).
func NewSessionStore(ttl time.Duration, dir string) *SessionStore {
	s := &SessionStore{ttl: ttl, sess: map[string]*Session{}}
	if dir != "" {
		s.path = filepath.Join(dir, "web-sessions.json")
		s.load()
	}
	return s
}

// load reads persisted sessions, dropping expired ones.
func (s *SessionStore) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var all []Session
	if json.Unmarshal(data, &all) != nil {
		return
	}
	now := time.Now()
	for _, si := range all {
		if now.Sub(si.LastSeen) <= s.ttl {
			cp := si
			s.sess[si.ID] = &cp
		}
	}
}

// saveLocked writes the session file. Caller holds mu.
func (s *SessionStore) saveLocked() {
	if s.path == "" {
		return
	}
	all := make([]Session, 0, len(s.sess))
	for _, si := range s.sess {
		all = append(all, *si)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// Save persists the current sessions (including sliding LastSeen refreshes).
// Call it periodically (background loop) rather than per-request.
func (s *SessionStore) Save() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveLocked()
}

// Create registers a new session (ID/Created/LastSeen are filled in) and
// returns its token.
func (s *SessionStore) Create(info Session) string {
	tok := NewToken()
	now := time.Now()
	info.ID = tok
	info.Created = now
	info.LastSeen = now
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sess[tok] = &info
	s.saveLocked()
	return tok
}

// Get returns the session if the token is active, sliding its expiry.
func (s *SessionStore) Get(token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	si, ok := s.sess[token]
	if !ok {
		return Session{}, false
	}
	if time.Since(si.LastSeen) > s.ttl {
		delete(s.sess, token)
		s.saveLocked()
		return Session{}, false
	}
	si.LastSeen = time.Now() // sliding window
	return *si, true
}

// List returns all active sessions sorted by creation time.
func (s *SessionStore) List() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.sess))
	for _, si := range s.sess {
		if time.Since(si.LastSeen) > s.ttl {
			continue
		}
		out = append(out, *si)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Delete removes a session (logout / revoke).
func (s *SessionStore) Delete(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sess[token]; !ok {
		return false
	}
	delete(s.sess, token)
	s.saveLocked()
	return true
}

// DeleteExcept revokes every session except the given token; returns the
// number revoked ("log out other devices").
func (s *SessionStore) DeleteExcept(keep string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for tok := range s.sess {
		if tok != keep {
			delete(s.sess, tok)
			n++
		}
	}
	if n > 0 {
		s.saveLocked()
	}
	return n
}

// DeleteUser revokes all sessions of one user (called when an account is
// removed); returns the number revoked.
func (s *SessionStore) DeleteUser(user string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for tok, si := range s.sess {
		if si.User == user {
			delete(s.sess, tok)
			n++
		}
	}
	if n > 0 {
		s.saveLocked()
	}
	return n
}

// RateLimiter throttles failed logins per key (client IP): after maxFails
// failures inside the window, attempts are locked out for a doubling penalty
// (lock→2×lock, capped at maxLock).
type RateLimiter struct {
	mu       sync.Mutex
	fails    map[string]*failState
	maxFails int
	window   time.Duration
	lock     time.Duration
	maxLock  time.Duration
}

type failState struct {
	count       int
	firstFail   time.Time
	lockedUntil time.Time
}

// NewRateLimiter builds a limiter: lock for 5 min after 5 failures within
// 15 min, penalty doubling up to 30 min.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{
		fails:    map[string]*failState{},
		maxFails: 5,
		window:   15 * time.Minute,
		lock:     5 * time.Minute,
		maxLock:  30 * time.Minute,
	}
}

// Allowed reports whether a login attempt from key may proceed, and if not,
// how long until it may.
func (l *RateLimiter) Allowed(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, ok := l.fails[key]
	if !ok {
		return true, 0
	}
	if time.Now().Before(st.lockedUntil) {
		return false, time.Until(st.lockedUntil)
	}
	return true, 0
}

// Fail records a failed attempt from key and applies lockout when exceeded.
func (l *RateLimiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	st, ok := l.fails[key]
	if !ok || now.Sub(st.firstFail) > l.window {
		st = &failState{firstFail: now}
		l.fails[key] = st
	}
	st.count++
	if st.count >= l.maxFails {
		penalty := l.lock << (st.count / l.maxFails) // 5m, 10m, 20m, 40m…
		if penalty > l.maxLock {
			penalty = l.maxLock
		}
		st.lockedUntil = now.Add(penalty)
	}
	// Bound memory: forget stale entries occasionally.
	if len(l.fails) > 4096 {
		for k, v := range l.fails {
			if now.Sub(v.firstFail) > l.window && now.After(v.lockedUntil) {
				delete(l.fails, k)
			}
		}
	}
}

// Reset clears the failure counter for key (successful login).
func (l *RateLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}
