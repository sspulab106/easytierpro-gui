package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestAuditMetricsRoutes guards the registration of /api/audit and
// /api/metrics: both handlers once existed but were never wired into the
// mux, so requests fell through to the static file server as 404s.
func TestAuditMetricsRoutes(t *testing.T) {
	srv, client := newAuthTestServer(t)
	base := "http://" + srv.Addr()

	// Legacy bootstrap token (no account configured yet).
	code, out := authDo(t, client, "GET", base+"/webconfig.json", "", nil)
	if code != 200 {
		t.Fatalf("webconfig = %d", code)
	}
	token, _ := out["token"].(string)
	if token == "" {
		t.Fatal("bootstrap token missing")
	}

	// Metrics: token-authenticated scrape returns Prometheus text.
	code, raw := authDoRaw(t, client, "GET", base+"/api/metrics", token, nil)
	if code != 200 {
		t.Fatalf("metrics with token = %d, want 200", code)
	}
	if !strings.Contains(string(raw), "easytier_pro_") {
		t.Fatalf("metrics body missing easytier_pro_ prefix: %q", string(raw)[:min(200, len(raw))])
	}

	// Bearer variant (Prometheus/CI style) must work too.
	req, err := http.NewRequest("GET", base+"/api/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("metrics with Bearer = %d, want 200", resp.StatusCode)
	}

	// Audit: token counts as admin, returns a JSON array.
	code, body := authDoRaw(t, client, "GET", base+"/api/audit?limit=10", token, nil)
	if code != 200 {
		t.Fatalf("audit with token = %d, want 200", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "[") {
		t.Fatalf("audit body not a JSON array: %q", string(body)[:min(100, len(body))])
	}

	// No credentials anywhere -> 401 (not 404).
	if code, _ := authDo(t, client, "GET", base+"/api/metrics", "", nil); code != 401 {
		t.Fatalf("metrics without creds = %d, want 401", code)
	}
	if code, _ := authDo(t, client, "GET", base+"/api/audit", "", nil); code != 401 {
		t.Fatalf("audit without creds = %d, want 401", code)
	}
}
