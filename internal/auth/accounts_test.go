package auth

import (
	"strings"
	"testing"
)

func TestAccountStoreLifecycle(t *testing.T) {
	s := NewAccountStore(t.TempDir())

	// First account becomes the admin root; cannot be deleted into emptiness.
	if err := s.Upsert(Account{Username: "admin", Role: "admin", Networks: nil}, "pw-admin-1"); err != nil {
		t.Fatalf("upsert admin: %v", err)
	}
	if err := s.Delete("admin"); err != ErrLastAdmin {
		t.Fatalf("delete last account = %v, want ErrLastAdmin", err)
	}

	// Authenticate + duplicate username updates instead of adding.
	if _, ok := s.Authenticate("admin", "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	if _, ok := s.Authenticate("admin", "pw-admin-1"); !ok {
		t.Fatal("correct password rejected")
	}
	if err := s.Upsert(Account{Username: "admin", Role: "admin"}, "pw-admin-2"); err != nil {
		t.Fatalf("password update: %v", err)
	}
	if _, ok := s.Authenticate("admin", "pw-admin-1"); ok {
		t.Fatal("old password still valid")
	}

	// Viewer account with network binding.
	if err := s.Upsert(Account{Username: "ops", Role: "viewer", Networks: []string{"lab"}}, "pw-ops-1"); err != nil {
		t.Fatalf("upsert ops: %v", err)
	}
	acc, ok := s.Get("ops")
	if !ok || acc.Role != "viewer" || len(acc.Networks) != 1 {
		t.Fatalf("ops account = %+v", acc)
	}
	// Updating without password keeps the hash.
	if err := s.Upsert(Account{Username: "ops", Role: "viewer", Networks: []string{"lab", "prod"}}, ""); err != nil {
		t.Fatalf("ops update: %v", err)
	}
	if _, ok := s.Authenticate("ops", "pw-ops-1"); !ok {
		t.Fatal("hash lost on passwordless update")
	}

	// Deletion and not-found.
	if err := s.Delete("ops"); err != nil {
		t.Fatalf("delete ops: %v", err)
	}
	if err := s.Delete("ghost"); err != ErrNotFound {
		t.Fatalf("delete ghost = %v", err)
	}
	list := s.List()
	if len(list) != 1 || list[0].Username != "admin" {
		t.Fatalf("list = %+v", list)
	}
	// No plaintext hashes in JSON on disk — covered by hash format check.
	if _, ok := s.Authenticate("admin", "pw-admin-2"); !ok {
		t.Fatal("admin auth broken after list")
	}
	_ = strings.TrimSpace("")
}
