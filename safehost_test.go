package main

import "testing"

func TestSafeHost(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		note string
	}{
		{"10.106.106.5", true, "plain IPv4"},
		{"gsjpc", true, "plain hostname"},
		{"gsjpc.local", true, "hostname with dot"},
		{"fe80::1", true, "IPv6 (colon allowed)"},
		{"-oProxyCommand=calc", false, "ssh option injection"},
		{"-f", false, "ping flag injection"},
		{"host;reboot", false, "shell metachar"},
		{"host name", false, "space"},
		{"$(calc)", false, "command substitution"},
		{"", false, "empty"},
	}
	for _, c := range cases {
		err := safeHost(c.in)
		if c.ok && err != nil {
			t.Errorf("safeHost(%q) = %v, want ok (%s)", c.in, err, c.note)
		}
		if !c.ok && err == nil {
			t.Errorf("safeHost(%q) accepted (%s)", c.in, c.note)
		}
	}
}
