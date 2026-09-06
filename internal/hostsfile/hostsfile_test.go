package hostsfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpdateCreatesAndReplacesBlock(t *testing.T) {
	path := writeTemp(t, "127.0.0.1 localhost\n")
	s := NewStoreAt(path)

	if err := s.Update(map[string]string{"GSJPC": "10.106.106.128", "d9": "10.106.106.2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !s.HasBlock() {
		t.Fatal("block missing after update")
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if !strings.Contains(text, "127.0.0.1 localhost") {
		t.Fatal("existing entry lost")
	}
	if !strings.Contains(text, "10.106.106.128\tgsjpc") {
		t.Fatalf("hostname not normalized:\n%s", text)
	}
	if !strings.Contains(text, "10.106.106.2\td9") {
		t.Fatal("entry missing")
	}

	// Changing entries replaces the block, not the whole file.
	if err := s.Update(map[string]string{"d9": "10.106.106.2"}); err != nil {
		t.Fatalf("update2: %v", err)
	}
	data, _ = os.ReadFile(path)
	text = string(data)
	if strings.Contains(text, "gsjpc") {
		t.Fatal("stale entry kept")
	}
	if !strings.Contains(text, "127.0.0.1 localhost") || !strings.Contains(text, "10.106.106.2\td9") {
		t.Fatalf("unexpected content:\n%s", text)
	}
}

func TestUpdateSkipsReservedAndInvalidNames(t *testing.T) {
	path := writeTemp(t, "192.168.1.1 printer\n")
	s := NewStoreAt(path)
	if err := s.Update(map[string]string{"printer": "10.0.0.5", "bad_name!": "10.0.0.6", "-nope": "10.0.0.7", "ok": "10.0.0.8"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	data, _ := os.ReadFile(path)
	text := string(data)
	if strings.Contains(text, "10.0.0.5") || strings.Contains(text, "10.0.0.6") || strings.Contains(text, "10.0.0.7") {
		t.Fatalf("reserved/invalid names leaked:\n%s", text)
	}
	if !strings.Contains(text, "10.0.0.8\tok") {
		t.Fatal("valid entry missing")
	}
}

func TestRemoveStripsBlock(t *testing.T) {
	path := writeTemp(t, "127.0.0.1 localhost\n")
	s := NewStoreAt(path)
	_ = s.Update(map[string]string{"a": "10.0.0.1"})
	_ = s.Update(map[string]string{"b": "10.0.0.2"})
	if err := s.Remove(); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if s.HasBlock() {
		t.Fatal("block survived Remove")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "127.0.0.1 localhost") {
		t.Fatalf("outside content lost:\n%s", string(data))
	}
	if strings.Contains(string(data), "10.0.0.1") || strings.Contains(string(data), "10.0.0.2") {
		t.Fatalf("block entries survived:\n%s", string(data))
	}
}

func TestUpdateNoopWhenUnchanged(t *testing.T) {
	path := writeTemp(t, "")
	s := NewStoreAt(path)
	_ = s.Update(map[string]string{"a": "10.0.0.1"})
	fi1, _ := os.Stat(path)
	_ = s.Update(map[string]string{"a": "10.0.0.1"})
	fi2, _ := os.Stat(path)
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Fatal("unchanged update rewrote the file")
	}
}
