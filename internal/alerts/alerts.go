// Package alerts detects peer state transitions from polled peer lists and
// turns them into notification events (node offline, latency above threshold).
package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PeerState is one remote peer as seen in a single poll.
type PeerState struct {
	Network      string
	Name         string
	Online       bool
	LatencyMs    float64
	LatencyKnown bool
}

// Config gates which transitions produce events.
type Config struct {
	OfflineEnabled     bool
	LatencyEnabled     bool
	LatencyThresholdMs float64
}

// Event is one alert to deliver (desktop notification / webhook / UI toast).
type Event struct {
	Kind    string `json:"kind"` // "offline" | "latency"
	Network string `json:"network"`
	Name    string `json:"name"`
	Detail  string `json:"detail"`
	Time    string `json:"time"`
}

// Key builds the state-map key for a peer.
func Key(network, name string) string {
	return network + "\x00" + name
}

// Detect compares the previous and current peer snapshots (keyed by network +
// peer name) and returns events to notify about. Only transitions fire: a
// peer that stays offline or stays slow produces no repeated events.
func Detect(prev, cur map[string]PeerState, cfg Config) []Event {
	var evs []Event
	now := time.Now().Format("2006-01-02 15:04:05")
	// Peers that vanished from the current snapshot went offline.
	for k, p := range prev {
		if !cfg.OfflineEnabled || !p.Online {
			continue
		}
		if c, ok := cur[k]; ok && c.Online {
			continue
		}
		evs = append(evs, Event{
			Kind: "offline", Network: p.Network, Name: p.Name,
			Detail: fmt.Sprintf("节点 %s 离线（网络 %s）", p.Name, p.Network),
			Time:   now,
		})
	}
	for k, c := range cur {
		p, had := prev[k]
		if cfg.LatencyEnabled && c.Online && c.LatencyKnown && c.LatencyMs >= cfg.LatencyThresholdMs {
			if !had || !p.LatencyKnown || p.LatencyMs < cfg.LatencyThresholdMs {
				evs = append(evs, Event{
					Kind: "latency", Network: c.Network, Name: c.Name,
					Detail: fmt.Sprintf("节点 %s 延迟 %.0f ms（阈值 %.0f ms，网络 %s）", c.Name, c.LatencyMs, cfg.LatencyThresholdMs, c.Network),
					Time:   now,
				})
			}
		}
	}
	return evs
}

// PostWebhook delivers an event to a user-configured HTTP endpoint as JSON.
// Empty URL is a silent no-op so callers can fire-and-forget.
func PostWebhook(url string, ev Event) error {
	if strings.TrimSpace(url) == "" {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
