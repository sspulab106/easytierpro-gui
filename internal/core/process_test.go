package core

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// resolveResourcesDir uses the PROJECT_ROOT env var or the test source dir.
func resolveResourcesDir(t *testing.T, sub string) string {
	t.Helper()
	root := os.Getenv("PROJECT_ROOT")
	if root == "" {
		// fallback: try CWD, which when running from the project dir works
		cwd, err := os.Getwd()
		if err == nil {
			root = cwd
		}
	}
	candidate := filepath.Join(root, "resources", sub)
	if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
		return candidate
	}
	// next fallback: relative to the test source file's location
	candidate = filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", sub)
	if fi, err := os.Stat(candidate); err == nil && fi.IsDir() {
		return candidate
	}
	t.Fatal("resources dir not found")
	return ""
}

// TestProcessStartStop verifies the core subprocess lifecycle works end to end.
func TestProcessStartStop(t *testing.T) {
	// Use a dedicated RPC port so parallel package tests don't contend.
	rpcAddr, restorePort, err := WithTestPort()
	if err != nil {
		t.Fatal(err)
	}
	defer restorePort()

	// Point paths at a temp dir so we don't touch real app data.
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "configs")
	logDir := filepath.Join(tmp, "logs")
	old := DefaultPaths
	DefaultPaths = &Paths{}
	DefaultPaths.SetOverride(
		resolveResourcesDir(t, "bin"),
		resolveResourcesDir(t, "drivers"),
		tmp,
		cfgDir,
		logDir,
	)
	defer func() { DefaultPaths = old }()

	// The core creates a default instance with fixed listeners (11010-11013)
	// when the config-dir is empty. Write an explicit config with a unique
	// listener port so the test does not conflict with a core already running
	// on the default ports.
	listenerPort, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`instance_name = "proc-test"
instance_id = "00000000-0000-0000-0000-000000000001"
dhcp = true
listeners = ["tcp://127.0.0.1:%d", "udp://127.0.0.1:%d"]

[network_identity]
network_name = "proc-test"
`, listenerPort, listenerPort)
	if err := os.WriteFile(filepath.Join(cfgDir, "proc-test.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewProcess()
	if err := p.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer p.Stop()

	// core should bind the RPC portal shortly after start
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if PingRpc(rpcAddr) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !PingRpc(rpcAddr) {
		t.Fatal("core RPC portal never became reachable")
	}

	if err := p.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if p.Running() {
		t.Fatal("core still running after Stop")
	}
}
