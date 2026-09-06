package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Account is one web-management user. Role gates writes: "admin" manages
// everything, "operator" runs/edits the networks in its grant (Networks —
// empty grant means all networks), "viewer" gets read-only access.
type Account struct {
	Username     string   `json:"username"`
	PasswordHash string   `json:"password_hash"`
	Role         string   `json:"role"` // "admin" | "operator" | "viewer"
	Networks     []string `json:"networks,omitempty"`
	Created      string   `json:"created"`
	Updated      string   `json:"updated,omitempty"` // last modification (account merge tie-breaker)
}

// AccountStore persists accounts as JSON in the app-data dir.
type AccountStore struct {
	mu   sync.Mutex
	path string
}

// NewAccountStore creates a store backed by <dir>/web-accounts.json.
func NewAccountStore(dir string) *AccountStore {
	return &AccountStore{path: filepath.Join(dir, "web-accounts.json")}
}

func (s *AccountStore) load() []Account {
	var out []Account
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(data, &out)
	return out
}

func (s *AccountStore) save(all []Account) error {
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// List returns all accounts sorted by name. The primary admin (first ever
// account, kept for the password-auth upgrade path) is included.
func (s *AccountStore) List() []Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	sort.Slice(all, func(i, j int) bool { return all[i].Username < all[j].Username })
	return all
}

// Authenticate verifies credentials; returns the account on success.
func (s *AccountStore) Authenticate(username, password string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.load() {
		if a.Username == username && VerifyPassword(password, a.PasswordHash) {
			return a, true
		}
	}
	return Account{}, false
}

// Get returns one account by username.
func (s *AccountStore) Get(username string) (Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.load() {
		if a.Username == username {
			return a, true
		}
	}
	return Account{}, false
}

// Upsert creates or updates an account. The password hash is set from
// password when non-empty; existing hash is kept otherwise.
func (s *AccountStore) Upsert(a Account, password string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	found := false
	for i, x := range all {
		if x.Username == a.Username {
			found = true
			if password != "" {
				hash, err := HashPassword(password)
				if err != nil {
					return err
				}
				a.PasswordHash = hash
			} else {
				a.PasswordHash = x.PasswordHash
			}
			if a.Created == "" {
				a.Created = x.Created
			}
			all[i] = a
			a.Updated = time.Now().Format(time.RFC3339)
			break
		}
	}
	if !found {
		if len(strings.TrimSpace(a.Username)) == 0 {
			return ErrInvalidUsername
		}
		if password == "" {
			return ErrPasswordRequired
		}
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		a.PasswordHash = hash
		if a.Created == "" {
			a.Created = time.Now().Format(time.RFC3339)
		}
		a.Updated = time.Now().Format(time.RFC3339)
		all = append(all, a)
		a.Updated = time.Now().Format(time.RFC3339)
	}
	return s.save(all)
}

// Put inserts or replaces an account whose password hash is already set
// (used for migrating the legacy owner account into the store).
func (s *AccountStore) Put(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	for i, x := range all {
		if x.Username == a.Username {
			if a.Created == "" {
				a.Created = x.Created
			}
			all[i] = a
			return s.save(all)
		}
	}
	if a.Created == "" {
		a.Created = time.Now().Format(time.RFC3339)
	}
	all = append(all, a)
	return s.save(all)
}

// MergeAccounts unions remote accounts into the local store (WebDAV
// multi-device account sync): per username, whichever copy has the newest
// Updated timestamp wins. Returns the number of accounts changed locally.
func (s *AccountStore) MergeAccounts(remote []Account) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	byName := map[string]int{}
	for i, a := range all {
		byName[a.Username] = i
	}
	changed := 0
	for _, r := range remote {
		if r.Username == "" {
			continue
		}
		i, exists := byName[r.Username]
		if !exists {
			all = append(all, r)
			byName[r.Username] = len(all) - 1
			changed++
			continue
		}
		local := all[i]
		if (r.Updated > local.Updated) && r.PasswordHash != "" {
			all[i] = r
			changed++
		}
	}
	if changed > 0 {
		_ = s.save(all)
	}
	return changed
}

// Delete removes an account by username; returns true when it existed.
// Deleting the last remaining admin account is refused.
func (s *AccountStore) Delete(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	kept := all[:0]
	found := false
	for _, a := range all {
		if a.Username == username {
			found = true
			continue
		}
		kept = append(kept, a)
	}
	if !found {
		return ErrNotFound
	}
	if len(kept) == 0 {
		return ErrLastAdmin
	}
	return s.save(kept)
}

// Errors returned by AccountStore mutations.
var (
	ErrNotFound         = errString("account not found")
	ErrLastAdmin        = errString("cannot delete the last account")
	ErrInvalidUsername  = errString("username is empty or invalid")
	ErrPasswordRequired = errString("password required for a new account")
)

type errString string

func (e errString) Error() string { return string(e) }
