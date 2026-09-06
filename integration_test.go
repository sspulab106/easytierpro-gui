package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/easytier"
)

// TestStartCoreAndQuery verifies the full path: spawn core, then query status
// through the easytier-cli wrapper.
func TestStartCoreAndQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rpcAddr, restorePort, err := core.WithTestPort()
	if err != nil {
		t.Fatal(err)
	}
	defer restorePort()

	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, "configs")
	logDir := filepath.Join(tmp, "logs")
	old := core.DefaultPaths
	core.DefaultPaths = &core.Paths{}
	core.DefaultPaths.SetOverride(
		filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", "bin"),
		filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", "drivers"),
		tmp,
		cfgDir,
		logDir,
	)
	defer func() { core.DefaultPaths = old }()

	// Write a config with a unique listener port so the core does not create
	// a default instance that conflicts with any already-running core.
	listenerPort, err := core.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`instance_name = "int-test"
instance_id = "00000000-0000-0000-0000-000000000001"
dhcp = true
listeners = ["tcp://127.0.0.1:%d", "udp://127.0.0.1:%d"]

[network_identity]
network_name = "int-test"
`, listenerPort, listenerPort)
	if err := os.WriteFile(filepath.Join(cfgDir, "int-test.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	p := core.NewProcess()
	if err := p.Start(); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer p.Stop()

	// wait for RPC portal
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if core.PingRpc(rpcAddr) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	cli := easytier.New(core.DefaultPaths.CliPath(), rpcAddr)

	node, err := cli.QueryNodeInfo()
	if err != nil {
		t.Fatalf("node info after start: %v", err)
	}
	t.Logf("node info: %s", node)

	peers, err := cli.QueryPeers()
	if err != nil {
		t.Fatalf("peers after start: %v", err)
	}
	t.Logf("peers: %s", peers)

	ver, err := cli.QueryVersion()
	if err != nil {
		t.Fatalf("version after start: %v", err)
	}
	t.Logf("core version: %s", ver)
	if ver == "" {
		t.Fatal("expected non-empty core version")
	}
}
