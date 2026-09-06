package configmgr

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"easytier-pro-gui/internal/core"
)

// TestCoreLoadsManagedConfig verifies a config written by the Manager is
// actually loaded by easytier-core on startup (integration).
func TestCoreLoadsManagedConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	rpcAddr, restorePort, err := core.WithTestPort()
	if err != nil {
		t.Fatal(err)
	}
	defer restorePort()

	tmp := t.TempDir()
	old := core.DefaultPaths
	core.DefaultPaths = &core.Paths{}
	core.DefaultPaths.SetOverride(
		filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", "bin"),
		filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", "drivers"),
		tmp,
		filepath.Join(tmp, "configs"),
		filepath.Join(tmp, "logs"),
	)
	defer func() { core.DefaultPaths = old }()

	// Write a relay-only network config (no TUN so admin is not required).
	m := NewManager(core.DefaultPaths.ConfigDir())
	raw := `instance_id = "33333333-4444-4444-4444-555555555555"
instance_name = "integration-node"

[network_identity]
network_name = "integration-test-net"
network_secret = "test-secret"
`
	if err := m.Save(ConfigFile{InstanceID: "33333333-4444-4444-4444-555555555555", Enabled: true, Raw: raw}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// capture core logs
	var mu sync.Mutex
	logs := []string{}
	p := core.NewProcess()
	p.SetLogCallback(func(line string) {
		mu.Lock()
		logs = append(logs, line)
		mu.Unlock()
	})
	if err := p.Start(); err != nil {
		t.Fatalf("start core: %v", err)
	}
	defer p.Stop()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if core.PingRpc(rpcAddr) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !core.PingRpc(rpcAddr) {
		t.Fatal("core RPC portal not reachable")
	}

	// Give core a moment to finish loading configs.
	time.Sleep(1 * time.Second)

	mu.Lock()
	joined := strings.Join(logs, "\n")
	mu.Unlock()

	if !strings.Contains(joined, "integration-test-net") {
		t.Fatalf("core log did not show the managed network; logs:\n%s", joined)
	}
}
