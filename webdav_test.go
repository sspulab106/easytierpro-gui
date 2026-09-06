package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"easytier-pro-gui/internal/configmgr"
	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/settings"
)

// TestWebdavSync exercises push/pull against a minimal in-process WebDAV
// server: PUT stores the zip, GET returns it, basic auth is required.
func TestWebdavSync(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "configs")
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	core.DefaultPaths.SetOverride("", "", dir, cfgDir, logDir)

	app := NewApp()
	cfg := configmgr.ConfigFile{
		InstanceID:   "11111111-2222-3333-4444-555555555555",
		InstanceName: "test",
		Network:      "test-net",
		Enabled:      true,
		Raw:          "instance_name = \"test\"\ninstance_id = \"11111111-2222-3333-4444-555555555555\"\n[network_identity]\nnetwork_name = \"test-net\"\nnetwork_secret = \"s\"\n",
	}
	if err := app.config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	var stored []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "u" || pass != "p" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodPut:
			stored, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
		case http.MethodGet:
			if _, err := w.Write(stored); err != nil {
				t.Logf("write: %v", err)
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	st := settings.Settings{
		Webdav: settings.WebdavConfig{ServerURL: srv.URL, Username: "u", Password: "p"},
	}
	if err := app.settings.Save(st); err != nil {
		t.Fatal(err)
	}

	if _, err := app.WebdavPush(); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(stored) == 0 {
		t.Fatal("push: nothing uploaded")
	}

	if err := app.config.Delete(cfg.InstanceID); err != nil {
		t.Fatal(err)
	}
	msg, err := app.WebdavPull()
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	t.Logf("pull msg: %s", msg)

	cfgs, err := app.config.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config restored, got %d", len(cfgs))
	}
	if cfgs[0].InstanceID != cfg.InstanceID {
		t.Fatalf("restored wrong config: %s", cfgs[0].InstanceID)
	}

	// Pull must keep local WebDAV credentials even when the remote settings
	// payload was created without them.
	got, err := app.settings.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Webdav.ServerURL != srv.URL {
		t.Fatalf("WebDAV creds lost after pull: %+v", got.Webdav)
	}
}
