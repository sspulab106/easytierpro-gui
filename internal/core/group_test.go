package core

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"easytier-pro-gui/internal/easytier"
)

// TestGroupStartStopInstance verifies the per-instance process model with
// the real core binary: one config file -> one process on its own rpc
// portal, queryable and stoppable without touching anything else.
func tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return string(b)
}

func TestGroupStartStopInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tmp := t.TempDir()
	old := DefaultPaths
	DefaultPaths = &Paths{}
	DefaultPaths.SetOverride(
		filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", "bin"),
		filepath.Join("D:\\easytierpro\\easytier-pro-gui\\resources", "drivers"),
		tmp,
		tmp, // config dir: the instance config lives here
		filepath.Join(tmp, "logs"),
	)
	defer func() { DefaultPaths = old }()

	id := "cccc3333-0000-0000-0000-000000000003"
	cfg := "instance_id = \"" + id + "\"\n" +
		"instance_name = \"grp-a\"\n" +
		"dhcp = false\n" +
		"ipv4 = \"10.61.0.1/24\"\n" +
		"listeners = []\n\n" +
		"[network_identity]\n" +
		"network_name = \"grp-net\"\n" +
		"network_secret = \"s\"\n\n" +
		"[flags]\n" +
		"no_tun = true\n"
	if err := os.WriteFile(filepath.Join(tmp, id+".toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	g := NewGroup()
	logs := 0
	startErr := g.StartInstance(id,
		func(string) { logs++ },
		func(bool) {},
		func(error) {},
	)
	if startErr != nil {
		t.Fatalf("start instance: %v", startErr)
	}
	defer g.StopAll()

	if !g.IsRunning(id) {
		t.Fatal("instance not registered as running")
	}
	if g.Count() != 1 {
		t.Fatalf("count = %d, want 1", g.Count())
	}
	portal := g.PortalOf(id)
	if portal == "" {
		t.Fatal("portal not recorded")
	}

	// Wait for the rpc portal to come up, then query through the CLI.
	cli := easytier.New(DefaultPaths.CliPath(), portal)
	ok := false
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := cli.QueryNodeInfo(); err == nil {
			ok = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !ok {
		log, _ := os.ReadFile(filepath.Join(filepath.Join(tmp, "logs"), "easytier-core.log"))
		t.Fatalf("rpc portal %s never answered; core log tail: %s", portal, tail(log, 800))
	}

	// Stop only this instance.
	if err := g.StopInstance(id); err != nil {
		t.Fatalf("stop instance: %v", err)
	}
	if g.IsRunning(id) {
		t.Fatal("instance still running after stop")
	}
	// Portal must be released shortly (process gone).
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if !PingRpc(portal) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if PingRpc(portal) {
		t.Fatalf("portal %s still bound after stop", portal)
	}
}
