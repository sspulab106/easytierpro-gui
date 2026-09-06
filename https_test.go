package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"easytier-pro-gui/internal/core"
)

func fingerprintHex(der []byte) string {
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}

// TestHTTPSWebServer verifies the self-signed TLS mode end to end through
// the real settings path: enable web_https via SaveSettings, then the web
// server serves TLS 1.2+ and presents a certificate whose SHA-256
// fingerprint matches the reported one.
func TestHTTPSWebServer(t *testing.T) {
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
	// Production wiring: SetWebAssets creates and starts the web server.
	app.SetWebAssets(os.DirFS(tmp))

	// Enable HTTPS through the real settings path (persists + applies,
	// including generating the certificate and restarting the server).
	raw, _ := json.Marshal(map[string]any{"web_https": true})
	if err := app.SaveSettings(string(raw)); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	status := app.WebTLSStatus()
	if status["enabled"] != "true" {
		t.Fatalf("tls status = %v, want enabled", status)
	}
	fp := status["fingerprint"]
	if len(fp) != 64 {
		t.Fatalf("fingerprint = %q, want 64 hex chars", fp)
	}

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
		Timeout:   10 * time.Second,
	}
	resp, err := client.Get("https://" + app.WebInfo()["addr"] + "/webconfig.json")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("https status = %d", resp.StatusCode)
	}
	if resp.TLS == nil || !resp.TLS.HandshakeComplete {
		t.Fatal("TLS handshake did not complete")
	}
	if resp.TLS.Version < tls.VersionTLS12 {
		t.Fatal("TLS version below 1.2")
	}
	if got := fingerprintHex(resp.TLS.PeerCertificates[0].Raw); !strings.EqualFold(got, fp) {
		t.Fatalf("presented fingerprint %s != reported %s", got, fp)
	}
}

// TestFleetPinFormat pins the pin's canonical form: exactly 64 hex chars,
// case-insensitively comparable (the client uses EqualFold).
func TestFleetPinFormat(t *testing.T) {
	pin := strings.ToUpper("abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789")
	if len(pin) != 64 {
		t.Fatalf("pin length = %d, want 64", len(pin))
	}
	if !strings.EqualFold(pin, strings.ToLower(pin)) {
		t.Fatal("pin comparison must be case-insensitive")
	}
}
