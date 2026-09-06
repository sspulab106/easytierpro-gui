// Package traffic persists per-network byte counters sampled from the core's
// stats endpoint, so usage survives restarts and can be charted over time.
// Samples are appended as JSON lines to one file per day under the traffic
// directory (e.g. 2026-08-29.jsonl); aggregation happens on read.
package traffic

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Counter holds cumulative byte counters for one virtual network.
type Counter struct {
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}

// Sample is one point-in-time snapshot of cumulative counters per network.
type Sample struct {
	Unix int64              `json:"ts"`
	Nets map[string]Counter `json:"nets"`
}

// Point is an aggregated delta over a time bucket.
type Point struct {
	Ts int64  `json:"ts"` // bucket start, unix seconds
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}

// DayTotal aggregates one calendar day.
type DayTotal struct {
	Date string `json:"date"` // YYYY-MM-DD
	Rx   uint64 `json:"rx"`
	Tx   uint64 `json:"tx"`
}

// History is the query result for the dashboard's traffic card.
type History struct {
	Hours24 []Point    `json:"hours_24"`
	Days    []DayTotal `json:"days"`
	TodayRx uint64     `json:"today_rx"`
	TodayTx uint64     `json:"today_tx"`
	WeekRx  uint64     `json:"week_rx"`
	WeekTx  uint64     `json:"week_tx"`
}

// Store manages the sample files in a directory.
type Store struct {
	mu  sync.Mutex
	dir string
}

// NewStore creates a store rooted at dir (created lazily on first write).
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Sample appends one snapshot to the current day's JSONL file.
func (s *Store) Sample(nets map[string]Counter) error {
	if len(nets) == 0 {
		return nil
	}
	now := time.Now()
	data, err := json.Marshal(Sample{Unix: now.Unix(), Nets: nets})
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.dir, now.Format("2006-01-02")+".jsonl"),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}

// History aggregates the last `days` days (1..90) into hourly and daily
// buckets. Cumulative counters reset when the core restarts; a counter going
// backwards is treated as a fresh counter and its current value counted as
// the delta (slight undercount right after restarts, never an overflow).
func (s *Store) History(days int) (History, error) {
	if days <= 0 || days > 90 {
		days = 7
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	start := now.AddDate(0, 0, -(days - 1))
	startDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, now.Location())

	var recs []Sample
	for d := startDay; !d.After(now); d = d.AddDate(0, 0, 1) {
		recs = append(recs, loadDay(filepath.Join(s.dir, d.Format("2006-01-02")+".jsonl"))...)
	}
	return aggregate(recs, now, days), nil
}

// Prune deletes sample files older than keepDays.
func (s *Store) Prune(keepDays int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
		}
	}
}

func loadDay(path string) []Sample {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec Sample
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Unix > 0 {
			out = append(out, rec)
		}
	}
	return out
}

// aggregate turns chronological samples into the dashboard history. The
// deltas between consecutive snapshots feed hourly buckets for the last 24 h
// and daily buckets for the requested range.
func aggregate(recs []Sample, now time.Time, days int) History {
	sort.Slice(recs, func(i, j int) bool { return recs[i].Unix < recs[j].Unix })

	type netPrev struct{ rx, tx uint64 }
	prev := map[string]netPrev{}

	hourRx := map[int64]uint64{}
	hourTx := map[int64]uint64{}
	dayRx := map[string]uint64{}
	dayTx := map[string]uint64{}

	for _, rec := range recs {
		ts := time.Unix(rec.Unix, 0)
		hour := ts.Truncate(time.Hour).Unix()
		day := ts.Format("2006-01-02")
		for net, c := range rec.Nets {
			p, seen := prev[net]
			var drx, dtx uint64
			switch {
			case !seen:
				// First sighting of this network: count only what arrives
				// after this sample to avoid crediting pre-GUI traffic.
			case c.Rx >= p.rx && c.Tx >= p.tx:
				drx, dtx = c.Rx-p.rx, c.Tx-p.tx
			default:
				// Counter reset (core restart): count the new absolute value.
				drx, dtx = c.Rx, c.Tx
			}
			prev[net] = netPrev{rx: c.Rx, tx: c.Tx}
			if drx == 0 && dtx == 0 {
				continue
			}
			hourRx[hour] += drx
			hourTx[hour] += dtx
			dayRx[day] += drx
			dayTx[day] += dtx
		}
	}

	h := History{}

	// 24 hourly buckets ending with the current hour, zero-filled.
	hStart := now.Truncate(time.Hour).Add(-23 * time.Hour)
	for i := 0; i < 24; i++ {
		t := hStart.Add(time.Duration(i) * time.Hour).Unix()
		h.Hours24 = append(h.Hours24, Point{Ts: t, Rx: hourRx[t], Tx: hourTx[t]})
	}

	// Daily buckets, oldest first.
	today := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -(days - 1))
	for d := start; !d.After(now); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		h.Days = append(h.Days, DayTotal{Date: key, Rx: dayRx[key], Tx: dayTx[key]})
		if key == today {
			h.TodayRx, h.TodayTx = dayRx[key], dayTx[key]
		}
		if !d.Before(start) {
			h.WeekRx += dayRx[key]
			h.WeekTx += dayTx[key]
		}
	}
	return h
}
