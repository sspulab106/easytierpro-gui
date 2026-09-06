package auth

import (
	"testing"
	"time"
)

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !VerifyPassword("s3cret-pw", hash) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong", hash) {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("s3cret-pw", "garbage") {
		t.Fatal("malformed hash accepted")
	}
	// Hashes must be salted: two runs differ.
	h2, _ := HashPassword("s3cret-pw")
	if h2 == hash {
		t.Fatal("hash not salted")
	}
}

func TestSessionStore(t *testing.T) {
	s := NewSessionStore(50 * time.Millisecond, "")
	tok := s.Create(Session{User: "admin", Role: "admin"})
	got, ok := s.Get(tok)
	if !ok {
		t.Fatal("fresh session invalid")
	}
	if got.User != "admin" || got.Role != "admin" || got.ID == "" {
		t.Fatalf("session metadata lost: %+v", got)
	}
	s.Delete(tok)
	if _, ok := s.Get(tok); ok {
		t.Fatal("deleted session valid")
	}

	tok2 := s.Create(Session{User: "a"})
	s.Create(Session{User: "a"})
	if n := s.DeleteExcept(tok2); n != 1 {
		t.Fatalf("DeleteExcept revoked %d, want 1", n)
	}
	tokB := s.Create(Session{User: "b"})
	if n := s.DeleteUser("b"); n != 1 {
		t.Fatalf("DeleteUser(b) = %d, want 1", n)
	}
	if _, ok := s.Get(tokB); ok {
		t.Fatal("session of deleted user still active")
	}

	time.Sleep(60 * time.Millisecond)
	if _, ok := s.Get(tok2); ok {
		t.Fatal("expired session valid")
	}
	if _, ok := s.Get(""); ok {
		t.Fatal("empty token accepted")
	}
}

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter()
	ip := "10.0.0.9"
	for i := 0; i < 4; i++ {
		if ok, _ := l.Allowed(ip); !ok {
			t.Fatalf("locked out after %d failures", i+1)
		}
		l.Fail(ip)
	}
	if ok, _ := l.Allowed(ip); !ok {
		t.Fatal("locked out before threshold")
	}
	l.Fail(ip) // 5th failure → lockout
	if ok, wait := l.Allowed(ip); ok || wait <= 0 {
		t.Fatal("no lockout after 5 failures")
	}
	l.Reset(ip)
	if ok, _ := l.Allowed(ip); !ok {
		t.Fatal("still locked after reset")
	}
	// Independent buckets.
	if ok, _ := l.Allowed("10.0.0.8"); !ok {
		t.Fatal("other IP locked")
	}
}

func TestSessionStorePersistence(t *testing.T) {
	dir := t.TempDir()
	s := NewSessionStore(time.Hour, dir)
	tok := s.Create(Session{User: "admin", Role: "admin"})
	// Force a flush so a new store (simulating an app restart) sees it.
	s.Save()

	s2 := NewSessionStore(time.Hour, dir)
	got, ok := s2.Get(tok)
	if !ok {
		t.Fatal("session did not survive restart")
	}
	if got.User != "admin" || got.Role != "admin" {
		t.Fatalf("session metadata lost across restart: %+v", got)
	}

	// Deleting in the new store must persist too.
	s2.Delete(tok)
	s3 := NewSessionStore(time.Hour, dir)
	if _, ok := s3.Get(tok); ok {
		t.Fatal("deleted session came back after restart")
	}

	// Expired sessions must be dropped at load time, not resurrected:
	// write one with a short-lived store, let it age out, then reload
	// with the same short TTL.
	s4 := NewSessionStore(40 * time.Millisecond, dir)
	s4.Create(Session{User: "x"})
	s4.Save()
	time.Sleep(60 * time.Millisecond)
	s5 := NewSessionStore(40 * time.Millisecond, dir)
	if n := len(s5.List()); n != 0 {
		t.Fatalf("expired sessions resurrected: %d remain", n)
	}
}
