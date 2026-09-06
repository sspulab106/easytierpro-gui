package traffic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAggregateDeltasAndReset(t *testing.T) {
	// Fixed clock at local noon keeps hour/day bucketing deterministic
	// regardless of when the test runs.
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.Local)
	h := func(hourAgo int) int64 { return now.Truncate(time.Hour).Add(-time.Duration(hourAgo) * time.Hour).Unix() }

	// Counter grows 100→300 (delta 200), then resets to 50 (delta counted as 50).
	recs := []Sample{
		{Unix: h(2) + 60, Nets: map[string]Counter{"net": {Rx: 100, Tx: 100}}},
		{Unix: h(1) + 60, Nets: map[string]Counter{"net": {Rx: 300, Tx: 350}}},
		{Unix: h(0) + 60, Nets: map[string]Counter{"net": {Rx: 50, Tx: 60}}},
	}
	got := aggregate(recs, now, 3)

	var sum uint64
	for _, p := range got.Hours24 {
		sum += p.Rx
	}
	if sum != 200+50 {
		t.Fatalf("hourly rx sum = %d, want 250", sum)
	}
	if got.TodayRx != 250 {
		t.Fatalf("TodayRx = %d, want 250", got.TodayRx)
	}
	if len(got.Days) != 3 {
		t.Fatalf("days = %d, want 3", len(got.Days))
	}
	var week uint64
	for _, d := range got.Days {
		week += d.Rx
	}
	if got.WeekRx != week {
		t.Fatalf("WeekRx = %d, want %d", got.WeekRx, week)
	}
}

func TestStoreSampleAndHistory(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)

	if err := s.Sample(map[string]Counter{
		"a": {Rx: 1000, Tx: 2000},
		"b": {Rx: 1, Tx: 2},
	}); err != nil {
		t.Fatalf("sample: %v", err)
	}
	// Empty samples must not create files.
	if err := s.Sample(map[string]Counter{}); err != nil {
		t.Fatalf("empty sample: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 sample file, got %d", len(entries))
	}
	// The file must be valid JSONL.
	data, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	var rec Sample
	if err := json.Unmarshal(firstLine(t, string(data)), &rec); err != nil {
		t.Fatalf("bad sample line: %v", err)
	}
	if rec.Nets["a"].Rx != 1000 {
		t.Fatalf("rx = %d, want 1000", rec.Nets["a"].Rx)
	}

	hist, err := s.History(7)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(hist.Hours24) != 24 {
		t.Fatalf("hours = %d, want 24", len(hist.Hours24))
	}
	// First sighting is not counted (only deltas afterwards), so totals are 0
	// for a single sample — but no error and correct bucket count.
	if hist.TodayRx != 0 {
		t.Fatalf("TodayRx = %d, want 0 for a single sample", hist.TodayRx)
	}
}

func TestStorePrune(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "2020-01-01.jsonl")
	if err := os.WriteFile(old, []byte(`{"ts":1,"nets":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Touch the mtime to 2020 so Prune removes it.
	past := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	NewStore(dir).Prune(30)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old sample file was not pruned")
	}
}

func firstLine(t *testing.T, s string) []byte {
	t.Helper()
	for i, r := range s {
		if r == '\n' {
			return []byte(s[:i])
		}
	}
	t.Fatal("no newline-terminated line")
	return nil
}
