package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"

	"path/filepath"
	"testing"
	"time"

	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/webserver"
)

// newAuthTestServer builds an App + web server against an isolated data dir
// and returns a client bound to it.
func newAuthTestServer(t *testing.T) (*webserver.Server, *http.Client) {
	t.Helper()
	tmp := t.TempDir()
	old := core.DefaultPaths
	core.DefaultPaths = &core.Paths{}
	core.DefaultPaths.SetOverride(
		filepath.Join(tmp, "bin"),
		filepath.Join(tmp, "drivers"),
		tmp,
		filepath.Join(tmp, "configs"),
		filepath.Join(tmp, "logs"),
	)
	t.Cleanup(func() { core.DefaultPaths = old })

	app := NewApp()
	srv := webserver.NewServer(app, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(srv.Stop)

	jar, _ := cookiejar.New(nil)
	return srv, &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

func authDo(t *testing.T, c *http.Client, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// authDoAny is like authDo but accepts array responses.
func authDoAny(t *testing.T, c *http.Client, method, path, token string, body any) (int, []any) {
	t.Helper()
	code, raw := authDoRaw(t, c, method, path, token, body)
	var out []any
	_ = json.Unmarshal(raw, &out)
	return code, out
}

func authDoRaw(t *testing.T, c *http.Client, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		bs, _ := json.Marshal(body)
		rd = bytes.NewReader(bs)
	}
	req, err := http.NewRequest(method, path, rd)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestWebAuthFlow(t *testing.T) {
	srv, client := newAuthTestServer(t)
	base := "http://" + srv.Addr()

	// Legacy bootstrap: token served until an account exists.
	code, out := authDo(t, client, "GET", base+"/webconfig.json", "", nil)
	if code != 200 || out["auth_required"] != false {
		t.Fatalf("bootstrap webconfig = %d %v", code, out)
	}
	bootToken, _ := out["token"].(string)
	if bootToken == "" {
		t.Fatal("bootstrap token missing")
	}

	// Without any credential the API rejects.
	if code, _ := authDo(t, client, "GET", base+"/api/status", "", nil); code != 401 {
		t.Fatalf("status without creds = %d, want 401", code)
	}
	// Legacy token works (API token semantics).
	if code, _ := authDo(t, client, "GET", base+"/api/status", bootToken, nil); code != 200 {
		t.Fatalf("status with bootstrap token = %d, want 200", code)
	}

	// Create the admin account (authenticated via bootstrap token).
	code, out = authDo(t, client, "POST", base+"/api/auth/password", bootToken, map[string]string{
		"username": "admin", "new_password": "hunter22",
	})
	if code != 200 {
		t.Fatalf("account setup = %d %v", code, out)
	}

	// Now /webconfig.json must NOT leak the token.
	code, out = authDo(t, client, "GET", base+"/webconfig.json", "", nil)
	if code != 200 || out["auth_required"] != true {
		t.Fatalf("post-setup webconfig = %d %v", code, out)
	}
	if _, has := out["token"]; has {
		t.Fatalf("token leaked after account setup: %v", out)
	}

	// Login with wrong password is rejected.
	code, _ = authDo(t, client, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "wrongpw",
	})
	if code != 401 {
		t.Fatalf("wrong-password login = %d, want 401", code)
	}

	// Login with correct password sets a session cookie.
	code, out = authDo(t, client, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "hunter22",
	})
	if code != 200 {
		t.Fatalf("login = %d %v", code, out)
	}

	// The cookie grants API access without any header.
	if code, _ := authDo(t, client, "GET", base+"/api/auth/me", "", nil); code != 200 {
		t.Fatal("session cookie not accepted for /api/auth/me")
	}
	if code, _ := authDo(t, client, "GET", base+"/api/status", "", nil); code != 200 {
		t.Fatal("session cookie not accepted for /api/status")
	}

	// Password change requires the current password.
	code, _ = authDo(t, client, "POST", base+"/api/auth/password", "", map[string]string{
		"current_password": "nope", "new_password": "newpass99",
	})
	if code != 401 {
		t.Fatalf("password change with wrong current = %d, want 401", code)
	}
	code, _ = authDo(t, client, "POST", base+"/api/auth/password", "", map[string]string{
		"current_password": "hunter22", "new_password": "newpass99",
	})
	if code != 200 {
		t.Fatalf("password change = %d", code)
	}

	// Logout invalidates the session.
	if code, _ := authDo(t, client, "POST", base+"/api/auth/logout", "", nil); code != 200 {
		t.Fatal("logout failed")
	}
	if code, _ := authDo(t, client, "GET", base+"/api/auth/me", "", nil); code != 401 {
		t.Fatal("session valid after logout")
	}

	// Rotate the API token: the old one stops working, the new one works.
	code, out = authDo(t, client, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "newpass99",
	})
	if code != 200 {
		t.Fatalf("re-login = %d", code)
	}
	code, out = authDo(t, client, "POST", base+"/api/auth/rotate-token", "", nil)
	if code != 200 {
		t.Fatalf("rotate = %d %v", code, out)
	}
	// cookie-less client exercises only the header credential
	plain := &http.Client{Timeout: 10 * time.Second}
	newTok, _ := out["token"].(string)
	if newTok == "" || newTok == bootToken {
		t.Fatal("token not rotated")
	}
	if code, _ := authDo(t, plain, "GET", base+"/api/status", bootToken, nil); code != 401 {
		t.Fatal("old token still accepted after rotate")
	}
	if code, _ := authDo(t, plain, "GET", base+"/api/status", newTok, nil); code != 200 {
		t.Fatal("new token rejected")
	}
}

func TestLoginRateLimit(t *testing.T) {
	srv, client := newAuthTestServer(t)
	base := "http://" + srv.Addr()

	// Bootstrap an account with a direct token: use a second client to set up
	// via the bootstrap token.
	_, boot := authDo(t, client, "GET", base+"/webconfig.json", "", nil)
	tok, _ := boot["token"].(string)
	authDo(t, client, "POST", base+"/api/auth/password", tok, map[string]string{
		"username": "admin", "new_password": "right-pw",
	})

	var last int
	for i := 0; i < 6; i++ {
		code, _ := authDo(t, client, "POST", base+"/api/auth/login", "", map[string]string{
			"username": "admin", "password": fmt.Sprintf("wrong-%d", i),
		})
		last = code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("after 5 wrong logins last code = %d, want 429", last)
	}
	// Even the correct password is refused while locked out.
	if code, _ := authDo(t, client, "POST", base+"/api/auth/login", "", map[string]string{
		"username": "admin", "password": "right-pw",
	}); code != http.StatusTooManyRequests {
		t.Fatalf("correct password during lockout = %d, want 429", code)
	}
}
