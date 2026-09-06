package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"easytier-pro-gui/internal/configmgr"
	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/settings"
)

const deviceCfgTOML = `instance_name = "test"
instance_id = "11111111-2222-3333-4444-555555555555"
hostname = "D9"
dhcp = false
ipv4 = "10.106.106.2/24"
[network_identity]
network_name = "test-net"
network_secret = "s"
`

// davFixture is one fake WebDAV drive plus two devices (apps) bound to it.
type davFixture struct {
	A, B    *App
	stored  []byte
	davURL  string
	cleanup func()
}

func newDavFixture(t *testing.T) *davFixture {
	t.Helper()
	fx := &davFixture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			fx.stored, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			_, _ = w.Write(fx.stored)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	fx.davURL = srv.URL
	fx.cleanup = srv.Close

	mkDevice := func() *App {
		dir := t.TempDir()
		core.DefaultPaths.SetOverride("", "", dir, filepath.Join(dir, "configs"), filepath.Join(dir, "logs"))
		app := NewApp()
		if err := app.settings.Save(settings.Settings{
			Webdav: settings.WebdavConfig{ServerURL: fx.davURL},
		}); err != nil {
			t.Fatal(err)
		}
		return app
	}
	fx.A = mkDevice()
	fx.B = mkDevice()
	return fx
}

// storedZip returns the decoded content of every file inside the archive
// currently stored on the fake drive.
func (fx *davFixture) storedZip(t *testing.T) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(fx.stored), int64(len(fx.stored)))
	if err != nil {
		t.Fatalf("stored archive: %v", err)
	}
	var blob strings.Builder
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		c, _ := io.ReadAll(rc)
		rc.Close()
		blob.Write(c)
	}
	return []byte(blob.String())
}

func TestWebdavPerDeviceSync(t *testing.T) {
	fx := newDavFixture(t)
	defer fx.cleanup()

	// Device A: a config with its own IP, DHCP-off and hostname.
	if err := fx.A.config.Save(configmgr.ConfigFile{
		InstanceID: "11111111-2222-3333-4444-555555555555",
		Network:    "test-net",
		Enabled:    true,
		Raw:        deviceCfgTOML,
	}); err != nil {
		t.Fatal(err)
	}
	if err := fx.A.settings.Save(settings.Settings{
		CustomCSS: ".card { color: red }",
		WebToken:  "device-a-token",
		WebPort:   12345,
		Webdav:    settings.WebdavConfig{ServerURL: fx.davURL},
	}); err != nil {
		t.Fatal(err)
	}

	// Push from A, then scan what actually landed on the drive: per-device
	// fields must not travel; shared ones must.
	if _, err := fx.A.WebdavPush(); err != nil {
		t.Fatalf("push: %v", err)
	}
	all := string(fx.storedZip(t))
	for _, forbidden := range []string{"10.106.106.2", "\"D9\"", "device-a-token", "12345"} {
		if strings.Contains(all, forbidden) {
			t.Fatalf("archive leaked per-device data: %q", forbidden)
		}
	}
	if !strings.Contains(all, ".card { color: red }") {
		t.Fatal("shared CSS missing from archive")
	}

	// Device B pulls: gets a clean DHCP config (no A's IP/hostname).
	if _, err := fx.B.WebdavPull(); err != nil {
		t.Fatalf("device B pull: %v", err)
	}
	cfgs, err := fx.B.config.List()
	if err != nil || len(cfgs) != 1 {
		t.Fatalf("device B configs = %v", cfgs)
	}
	b := cfgs[0].Raw
	if strings.Contains(b, "10.106.106.2") || strings.Contains(b, `"D9"`) {
		t.Fatalf("device B inherited device A identity:\n%s", b)
	}
	if !strings.Contains(b, "dhcp = true") || !strings.Contains(b, "test-net") {
		t.Fatalf("device B config incomplete:\n%s", b)
	}
	// Shared settings travel; per-device ones do not.
	bset, err := fx.B.settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if bset.CustomCSS != ".card { color: red }" {
		t.Fatalf("device B missing shared CSS: %+v", bset)
	}
	if bset.WebToken == "device-a-token" || bset.WebPort == 12345 {
		t.Fatalf("device B inherited A's per-device settings: %+v", bset)
	}

	// Device A pulls its own archive: keeps its own IP/hostname/DHCP-off.
	if _, err := fx.A.WebdavPull(); err != nil {
		t.Fatalf("device A pull: %v", err)
	}
	again, err := fx.A.config.List()
	if err != nil || len(again) != 1 {
		t.Fatalf("device A configs = %v", again)
	}
	a := again[0].Raw
	for _, want := range []string{`ipv4 = "10.106.106.2/24"`, `hostname = "D9"`, "dhcp = false", "test-net"} {
		if !strings.Contains(a, want) {
			t.Fatalf("device A lost %q after restore:\n%s", want, a)
		}
	}
	// Per-device settings stay local on A too.
	aseta, err := fx.A.settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if aseta.WebToken != "device-a-token" || aseta.WebPort != 12345 {
		t.Fatalf("device A per-device settings clobbered: %+v", aseta)
	}
}

func TestSharedConfigTOMLUnits(t *testing.T) {
	out := configmgr.SharedTOML(deviceCfgTOML)
	if strings.Contains(out, "ipv4") || strings.Contains(out, `"D9"`) || strings.Contains(out, "dhcp = false") {
		t.Fatalf("per-device fields survived:\n%s", out)
	}
	if !strings.Contains(out, "dhcp = true") || !strings.Contains(out, "network_secret = \"s\"") || !strings.Contains(out, "instance_id") {
		t.Fatalf("shared fields lost:\n%s", out)
	}

	f := configmgr.ExtractDeviceFields(deviceCfgTOML)
	if f.DHCP != "false" || f.IPv4 != `"10.106.106.2/24"` || f.Hostname != `"D9"` {
		t.Fatalf("device fields = %+v", f)
	}
	merged := configmgr.ReapplyDeviceFields(configmgr.SharedTOML(deviceCfgTOML), f)
	for _, want := range []string{`ipv4 = "10.106.106.2/24"`, `hostname = "D9"`, "dhcp = false", "network_secret = \"s\""} {
		if !strings.Contains(merged, want) {
			t.Fatalf("reapply lost %q:\n%s", want, merged)
		}
	}
	// The merged keys must stay in the top-level section (before any table).
	if strings.Index(merged, "[network_identity]") < strings.Index(merged, "ipv4 =") {
		t.Fatalf("ipv4 landed inside a table:\n%s", merged)
	}
}

func TestSharedSettingsProjection(t *testing.T) {
	src := settings.Settings{
		CustomCSS: "x",
		WebToken:  "t",
		AutoStart: true,
		Alerts:    settings.AlertConfig{Enabled: true, OfflineNotify: true},
	}
	blob, err := sharedSettingsJSON(src)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"web_token", "auto_start", "webdav", "magic_dns", "log_dir", "web_password_hash", "web_username"} {
		if _, ok := got[forbidden]; ok {
			t.Fatalf("per-device setting %q would sync", forbidden)
		}
	}
	if got["custom_css"] != "x" {
		t.Fatalf("custom css lost: %v", got)
	}
}
