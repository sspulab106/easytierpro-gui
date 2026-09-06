package alerts

import "testing"

func cfg() Config {
	return Config{OfflineEnabled: true, LatencyEnabled: true, LatencyThresholdMs: 100}
}

func TestDetectOfflineTransition(t *testing.T) {
	prev := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true},
		Key("net", "b"): {Network: "net", Name: "b", Online: true},
	}
	cur := map[string]PeerState{
		Key("net", "b"): {Network: "net", Name: "b", Online: true},
		// "a" disappeared → offline event
	}
	evs := Detect(prev, cur, cfg())
	if len(evs) != 1 || evs[0].Kind != "offline" || evs[0].Name != "a" {
		t.Fatalf("events = %+v, want one offline for a", evs)
	}

	// Same snapshot again → no repeated event.
	evs = Detect(cur, cur, cfg())
	if len(evs) != 0 {
		t.Fatalf("steady state produced %+v, want none", evs)
	}

	// First sight of a peer is never an offline event.
	evs = Detect(nil, cur, cfg())
	if len(evs) != 0 {
		t.Fatalf("cold start produced %+v, want none", evs)
	}
}

func TestDetectLatencyCrossing(t *testing.T) {
	prev := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true, LatencyMs: 20, LatencyKnown: true},
	}
	cur := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true, LatencyMs: 350, LatencyKnown: true},
	}
	evs := Detect(prev, cur, cfg())
	if len(evs) != 1 || evs[0].Kind != "latency" {
		t.Fatalf("events = %+v, want one latency event", evs)
	}

	// Stays high → no repeat.
	if evs := Detect(cur, cur, cfg()); len(evs) != 0 {
		t.Fatalf("repeat events = %+v, want none", evs)
	}

	// Recovers, then degrades again → fires again.
	recovered := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true, LatencyMs: 15, LatencyKnown: true},
	}
	if evs := Detect(cur, recovered, cfg()); len(evs) != 0 {
		t.Fatalf("recovery events = %+v, want none", evs)
	}
	if evs := Detect(recovered, cur, cfg()); len(evs) != 1 {
		t.Fatalf("re-cross events = %+v, want one", evs)
	}
}

func TestDetectGates(t *testing.T) {
	prev := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true, LatencyMs: 10, LatencyKnown: true},
	}
	cur := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true, LatencyMs: 500, LatencyKnown: true},
	}
	// Latency alerts disabled → nothing.
	off := Config{OfflineEnabled: true, LatencyEnabled: false, LatencyThresholdMs: 100}
	if evs := Detect(prev, cur, off); len(evs) != 0 {
		t.Fatalf("disabled latency produced %+v", evs)
	}
	// Unknown latency is never an event.
	unknown := map[string]PeerState{
		Key("net", "a"): {Network: "net", Name: "a", Online: true},
	}
	if evs := Detect(prev, unknown, cfg()); len(evs) != 0 {
		t.Fatalf("unknown latency produced %+v", evs)
	}
}
