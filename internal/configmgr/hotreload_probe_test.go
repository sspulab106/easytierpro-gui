package configmgr

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"easytier-pro-gui/internal/core"
	"easytier-pro-gui/internal/easytier"
)

// TestConfigDirHotReload answers whether easytier-core watches --config-dir:
// a second .toml dropped into the directory while the core is running should
// start its network instance WITHOUT a core restart.
func TestConfigDirHotReload(t *testing.T) {
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

	netA := `instance_id = "aaaa1111-0000-0000-0000-000000000001"
instance_name = "hot-a"
dhcp = false
ipv4 = "10.60.0.1/24"
listeners = [] # avoid port conflicts with the running GUI/core
no_tun = true

[network_identity]
network_name = "hot-a-net"
network_secret = "s"
`
	netB := `instance_id = "bbbb2222-0000-0000-0000-000000000002"
instance_name = "hot-b"
dhcp = false
ipv4 = "10.60.1.1/24"
listeners = []
no_tun = true

[network_identity]
network_name = "hot-b-net"
network_secret = "s"
`

	m := NewManager(core.DefaultPaths.ConfigDir())
	if err := m.Save(ConfigFile{InstanceID: "aaaa1111-0000-0000-0000-000000000001", Raw: netA, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	proc := core.NewProcess()
	if err := proc.Start(); err != nil {
		t.Fatalf("core start: %v", err)
	}
	defer proc.Stop()

	cli := easytier.New(core.DefaultPaths.CliPath(), rpcAddr)
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := cli.QueryNodeInfo(); err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Drop network B into the live config-dir — no restart.
	if err := m.Save(ConfigFile{InstanceID: "bbbb2222-0000-0000-0000-000000000002", Raw: netB, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	// Does B appear without a restart?
	found := false
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := cli.QueryNodeInfo()
		if err == nil && len(raw) > 0 {
			if os.Getenv("HOTRELOAD_DEBUG") != "" {
				t.Logf("node: %s", string(raw))
			}
		}
		cfg, err := cli.QueryNodeConfig()
		if err == nil && containsInstanceName(string(cfg), "hot-b") {
			found = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Logf("hot-reload of a new config file applied without restart: %v", found)
	if !found {
		t.Skip("core does not watch --config-dir; per-network process model stays")
	}
}

func containsInstanceName(cfgJSON, name string) bool {
	return len(cfgJSON) > 0 && (indexOf(cfgJSON, name) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
