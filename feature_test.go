package main

import (
	"encoding/json"
	"testing"

	"easytier-pro-gui/internal/alerts"
)

func TestParseTrafficCounters(t *testing.T) {
	raw := json.RawMessage(`[
		{"name":"traffic_bytes_rx","value":1000,"labels":{"network_name":"netA"}},
		{"name":"traffic_bytes_tx","value":2000,"labels":{"network_name":"netA"}},
		{"name":"traffic_bytes_rx","value":5,"labels":{"network_name":"netB"}},
		{"name":"traffic_bytes_rx","value":7,"labels":{"network_name":""}},
		{"name":"peer_rpc_client_rx","value":999,"labels":{"network_name":"netA"}}
	]`)
	nets, err := parseTrafficCounters(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(nets) != 2 {
		t.Fatalf("networks = %d, want 2", len(nets))
	}
	if nets["netA"].Rx != 1000 || nets["netA"].Tx != 2000 {
		t.Fatalf("netA = %+v", nets["netA"])
	}
	if nets["netB"].Rx != 5 {
		t.Fatalf("netB = %+v", nets["netB"])
	}
}

func TestParseTrafficCountersStringValues(t *testing.T) {
	raw := json.RawMessage(`[{"name":"traffic_bytes_rx","value":"42","labels":{"network_name":"n"}}]`)
	nets, err := parseTrafficCounters(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if nets["n"].Rx != 42 {
		t.Fatalf("rx = %d, want 42", nets["n"].Rx)
	}
}

func TestPingSuccessDetection(t *testing.T) {
	yes := []string{
		"Reply from 10.106.106.2: bytes=32 time=1ms TTL=64",
		"来自 10.106.106.2 的回复: 字节=32 时间<1ms TTL=64",
		"64 bytes from 10.106.106.2: icmp_seq=1 ttl=64 time=0.500 ms",
		"64 bytes from 10.106.106.2: icmp_seq=0 ttl=54 time=196.09 ms",
	}
	for _, s := range yes {
		if !pingReplyRe.MatchString(s) {
			t.Errorf("expected success match: %q", s)
		}
	}
	no := []string{
		"Packet needs to be fragmented but DF set.",
		"数据包需要分段但设置 DF。",
		"Request timed out.",
		"请求超时。",
		"From 10.106.106.1 icmp_seq=1 Destination Host Unreachable",
	}
	for _, s := range no {
		if pingReplyRe.MatchString(s) {
			t.Errorf("unexpected success match: %q", s)
		}
	}
}

func TestPeersToStatesGrouped(t *testing.T) {
	app := NewApp()
	raw := json.RawMessage(`[
		{"instance_id":"id1","instance_name":"net1","result":[
			{"hostname":"pc-a","cost":"p2p","lat_ms":"12.5"},
			{"hostname":"self","cost":"Local","lat_ms":"-"}
		]},
		{"instance_id":"id2","instance_name":"net2","result":[
			{"hostname":"pc-b","cost":"conn","lat_ms":"-"}
		]}
	]`)
	states := app.peersToStates(raw)
	if len(states) != 2 {
		t.Fatalf("states = %d, want 2: %+v", len(states), states)
	}
	a, ok := states[alerts.Key("net1", "pc-a")]
	if !ok || !a.Online || !a.LatencyKnown || a.LatencyMs != 12.5 {
		t.Fatalf("pc-a state = %+v", a)
	}
	if _, ok := states[alerts.Key("net2", "pc-b")]; !ok {
		t.Fatal("pc-b missing")
	}
	// Local peer and unknown latency are excluded / flagged accordingly.
	for k, s := range states {
		if s.Name == "self" {
			t.Fatalf("local peer leaked into states: %s", k)
		}
	}
	b := states[alerts.Key("net2", "pc-b")]
	if b.LatencyKnown {
		t.Fatalf("pc-b latency should be unknown: %+v", b)
	}
}

func TestPeersToStatesFlat(t *testing.T) {
	app := NewApp()
	raw := json.RawMessage(`[
		{"hostname":"pc-x","cost":"p2p","lat_ms":"88"}
	]`)
	states := app.peersToStates(raw)
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	for _, s := range states {
		if s.Name != "pc-x" || !s.Online || s.LatencyMs != 88 {
			t.Fatalf("state = %+v", s)
		}
	}
}
