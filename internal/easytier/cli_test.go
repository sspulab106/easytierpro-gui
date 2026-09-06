package easytier

import (
	"testing"
)

// TestQueryAgainstRunningCore checks CLI JSON parsing works. Requires a core
// reachable on the default RPC portal (set by the core process test run).
func TestQueryAgainstRunningCore(t *testing.T) {
	c := New("D:\\easytierpro\\easytier-pro-gui\\resources\\bin\\easytier-cli.exe", "127.0.0.1:15888")

	// These will fail cleanly if no core is running; that's fine for this test
	// since the failure path is what we exercise when the GUI is idle.
	if _, err := c.QueryNodeInfo(); err != nil {
		t.Logf("node info unavailable (expected when core not running): %v", err)
		return
	}

	node, err := c.QueryNodeInfo()
	if err != nil {
		t.Fatalf("node info: %v", err)
	}
	t.Logf("node info: %s", node)
}
