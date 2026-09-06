package configmgr

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleToml = `instance_id = "11111111-2222-3333-4444-555555555555"
instance_name = "node-a"
ipv4 = "10.144.144.1"
listeners = ["tcp://0.0.0.0:11010"]

[network_identity]
network_name = "testnet"
network_secret = "secret"
`

func TestManagerCRUD(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	// Save a new config
	cfg := ConfigFile{
		InstanceID:   "11111111-2222-3333-4444-555555555555",
		InstanceName: "node-a",
		Enabled:      true,
		Raw:          sampleToml,
	}
	if err := m.Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	path := filepath.Join(dir, "11111111-2222-3333-4444-555555555555.toml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file on disk: %v", err)
	}

	list, err := m.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 config, got %d", len(list))
	}
	if list[0].Network != "testnet" || list[0].IPv4 != "10.144.144.1" {
		t.Fatalf("parsed meta wrong: %+v", list[0])
	}
	if !list[0].Enabled {
		t.Fatal("config should be enabled")
	}

	// Disable -> rename
	if err := m.SetEnabled(list[0].InstanceID, false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	disabledPath := filepath.Join(dir, "11111111-2222-3333-4444-555555555555.toml.disabled")
	if _, err := os.Stat(disabledPath); err != nil {
		t.Fatalf("expected disabled file: %v", err)
	}
	list, _ = m.List()
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("expected 1 disabled config, got %+v", list)
	}

	// Delete
	if err := m.Delete(list[0].InstanceID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = m.List()
	if len(list) != 0 {
		t.Fatalf("expected 0 configs after delete, got %d", len(list))
	}
}

func TestInjectIDWhenMissing(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)
	raw := `instance_name = "x"\n[network_identity]\nnetwork_name = "n"\n`

	if err := m.Save(ConfigFile{Enabled: true, Raw: raw}); err == nil {
		t.Fatal("expected error when no instance id derivable")
	}

	// with id provided externally, injection should add it
	if err := m.Save(ConfigFile{InstanceID: "22222222-2222-2222-2222-222222222222", Enabled: true, Raw: raw}); err != nil {
		t.Fatalf("save with explicit id: %v", err)
	}
	got, err := m.Get("22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Raw == "" || got.Raw == raw {
		t.Fatalf("expected instance_id to be injected, got: %q", got.Raw)
	}
}
