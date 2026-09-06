package main

import (
	"testing"
)

func TestPingDiagnostic(t *testing.T) {
	a := NewApp()
	res, err := a.PingDiagnostic("127.0.0.1", 3)
	if err != nil {
		t.Fatalf("ping localhost failed: %v", err)
	}
	if res.Received == 0 {
		t.Fatal("expected at least one reply from localhost")
	}
	if res.AvgMs <= 0 {
		t.Logf("avg ms = %v (may be 0 on loopback)", res.AvgMs)
	}
	t.Logf("sent=%d received=%d loss=%.1f%% avg=%.2f jitter=%.2f rtts=%v",
		res.Sent, res.Received, res.LossPercent, res.AvgMs, res.JitterMs, res.Rtts)
}

func TestLocalSubnets(t *testing.T) {
	a := NewApp()
	subnets, err := a.LocalSubnets()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("local subnets: %v", subnets)
	if len(subnets) == 0 {
		t.Fatal("expected at least one local subnet")
	}
	// 127.0.0.1/8 should never be reported
	for _, s := range subnets {
		if s == "127.0.0.1/8" {
			t.Errorf("loopback reported as local subnet: %s", s)
		}
	}
}

func TestCheckSubnetConflict(t *testing.T) {
	a := NewApp()
	subnets, _ := a.LocalSubnets()
	if len(subnets) == 0 {
		t.Skip("no local subnets to test against")
	}
	// A subnet that definitely overlaps the first local one.
	conflict, err := a.CheckSubnetConflict(subnets[0])
	if err != nil {
		t.Fatal(err)
	}
	if !conflict {
		t.Errorf("expected %s to conflict with itself", subnets[0])
	}
	// A private subnet that should not overlap typical LANs.
	noConflict, err := a.CheckSubnetConflict("203.0.113.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if noConflict {
		t.Error("203.0.113.0/24 should not conflict")
	}
	// Invalid input.
	if _, err := a.CheckSubnetConflict("not-a-cidr"); err == nil {
		t.Error("expected error for invalid cidr")
	}
}
