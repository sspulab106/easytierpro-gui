package main

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"
)

// newClient returns an independent cookie jar client (a separate "device").
func newClient(_ string) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func TestUsersAndSessions(t *testing.T) {
	srv, admin := newAuthTestServer(t)
	base := "http://" + srv.Addr()

	// Bootstrap the admin account.
	_, boot := authDo(t, admin, "GET", base+"/webconfig.json", "", nil)
	tok, _ := boot["token"].(string)
	if code, _ := authDo(t, admin, "POST", base+"/api/auth/password", tok, map[string]string{
		"username": "admin", "new_password": "admin-pw-1",
	}); code != 200 {
		t.Fatal("admin setup failed")
	}
	if code, _ := authDo(t, admin, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "admin-pw-1",
	}); code != 200 {
		t.Fatal("admin login failed")
	}

	// Admin creates a viewer bound to one network.
	code, out := authDo(t, admin, "POST", base+"/api/users", "", map[string]any{
		"username": "ops", "password": "ops-pw-1", "role": "viewer", "networks": []string{"lab"},
	})
	if code != 200 {
		t.Fatalf("create viewer = %d %v", code, out)
	}

	// Viewer logs in from its own "device".
	ops := newClient(srv.Addr())
	if code, _ := authDo(t, ops, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "ops", "password": "ops-pw-1",
	}); code != 200 {
		t.Fatal("viewer login failed")
	}

	// Viewer reads fine but cannot write.
	if code, _ := authDo(t, ops, "GET", base+"/api/status", "", nil); code != 200 {
		t.Fatal("viewer read blocked")
	}
	if code, _ := authDo(t, ops, "POST", base+"/api/start", "", nil); code != http.StatusForbidden {
		t.Fatalf("viewer start core = %d, want 403", code)
	}
	if code, _ := authDo(t, ops, "POST", base+"/api/users", "", map[string]any{"username": "x", "role": "admin", "password": "xxxxxx"}); code != http.StatusForbidden {
		t.Fatalf("viewer create user = %d, want 403", code)
	}

	// Viewer sees only its own session; admin sees both.
	code, ownSessions := authDoAny(t, ops, "GET", base+"/api/auth/sessions", "", nil)
	if code != 200 || len(ownSessions) != 1 {
		t.Fatalf("viewer sessions = %d %v", code, ownSessions)
	}
	_, allSessions := authDoAny(t, admin, "GET", base+"/api/auth/sessions", "", nil)
	if len(allSessions) != 2 {
		t.Fatalf("admin sessions = %v", allSessions)
	}

	// Admin lists users (hashes must not leak) and deletes the viewer.
	code, users := authDoAny(t, admin, "GET", base+"/api/users", "", nil)
	if code != 200 || len(users) != 2 {
		t.Fatalf("users list = %d %v", code, users)
	}
	for _, u := range users {
		if _, has := u.(map[string]any)["password_hash"]; has {
			t.Fatal("password hash leaked via users list")
		}
	}
	if code, _ := authDo(t, admin, "DELETE", base+"/api/users?name=ops", "", nil); code != 200 {
		t.Fatal("delete viewer failed")
	}
	// The deleted viewer's session must be kicked.
	if code, _ := authDo(t, ops, "GET", base+"/api/status", "", nil); code != 401 {
		t.Fatalf("deleted viewer session still valid, got %d", code)
	}

	// Admin logs in from a second device, then revokes other sessions.
	second := newClient(srv.Addr())
	if code, _ := authDo(t, second, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "admin-pw-1",
	}); code != 200 {
		t.Fatal("second device login failed")
	}
	code, out = authDo(t, second, "POST", base+"/api/auth/sessions/revoke-others", "", nil)
	if code != 200 {
		t.Fatalf("revoke-others = %d %v", code, out)
	}
	if out["revoked"].(float64) != 1 {
		t.Fatalf("revoked = %v, want 1", out["revoked"])
	}
	if code, _ := authDo(t, admin, "GET", base+"/api/status", "", nil); code != 401 {
		t.Fatal("first admin session survived revoke-others")
	}
	if code, _ := authDo(t, second, "GET", base+"/api/status", "", nil); code != 200 {
		t.Fatal("current session killed by revoke-others")
	}

	// Password change requires the current one (self-service on any role).
	if code, _ := authDo(t, second, "POST", base+"/api/auth/password", "", map[string]string{
		"current_password": "wrong", "new_password": "admin-pw-2",
	}); code != 401 {
		t.Fatal("password change accepted wrong current password")
	}
	if code, _ := authDo(t, second, "POST", base+"/api/auth/password", "", map[string]string{
		"current_password": "admin-pw-1", "new_password": "admin-pw-2",
	}); code != 200 {
		t.Fatal("password change failed")
	}
}
