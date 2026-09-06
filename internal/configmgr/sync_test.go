package configmgr

import (
	"strings"
	"testing"
)


func TestInjectListeners(t *testing.T) {
	raw := "instance_id = \"aaa\"\n\n[network_identity]\nnetwork_name = \"n\"\n"
	out, err := InjectListeners(raw, 11010)
	if err != nil {
		t.Fatal(err)
	}
	// must land BEFORE any [table] header
	if strings.Index(out, "listeners") > strings.Index(out, "[network_identity]") {
		t.Fatalf("listeners injected after table header:\n%s", out)
	}
	if !strings.Contains(out, "tcp://0.0.0.0:11010") || !strings.Contains(out, "udp://0.0.0.0:11010") {
		t.Fatalf("missing listener entries:\n%s", out)
	}
	if !HasListeners(out) {
		t.Fatal("HasListeners = false after inject")
	}
}

func TestReplaceListenerPorts(t *testing.T) {
	raw := "listeners = [\"tcp://0.0.0.0:11010\", \"udp://0.0.0.0:11010\", \"wss://example.com\"]\ninstance_id = \"b\"\n"
	out, err := ReplaceListenerPorts(raw, 11042)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "11010") {
		t.Fatalf("old port kept:\n%s", out)
	}
	if !strings.Contains(out, "tcp://0.0.0.0:11042") || !strings.Contains(out, "udp://0.0.0.0:11042") {
		t.Fatalf("new port missing:\n%s", out)
	}
	if !strings.Contains(out, "wss://example.com") {
		t.Fatalf("wss listener dropped:\n%s", out)
	}
}
