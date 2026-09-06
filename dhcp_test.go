package main

import (
	"regexp"
	"strings"
	"testing"
)

const dhcpRaw = `instance_id = "aaa"
instance_name = "net1"
hostname = "pc-a"
dhcp = true
listeners = ["tcp://0.0.0.0:11010"]

[[peer]]
uri = "tcp://server:11010"
`

func TestInjectStaticIP(t *testing.T) {
	out, changed := injectStaticIP(dhcpRaw, "10.1.1.7/24")
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.Contains(out, `dhcp = false`) {
		t.Fatalf("dhcp flag not flipped:\n%s", out)
	}
	if !strings.Contains(out, `ipv4 = "10.1.1.7/24"`) {
		t.Fatalf("ipv4 line missing:\n%s", out)
	}
	// The injected keys must stay in the top-level section: they must appear
	// BEFORE the first [[table]] header.
	if strings.Index(out, "[[peer]]") < strings.Index(out, `ipv4 = "10.1.1.7/24"`) {
		t.Fatalf("injected keys landed inside a table:\n%s", out)
	}

	// Injecting twice must not stack keys.
	out2, changed2 := injectStaticIP(out, "10.1.1.8/24")
	if !changed2 || !strings.Contains(out2, `ipv4 = "10.1.1.8/24"`) {
		t.Fatalf("second inject failed:\n%s", out2)
	}
}

func TestInjectStaticIPNoDhcpKey(t *testing.T) {
	raw := "instance_id = \"x\"\n"
	out, changed := injectStaticIP(raw, "10.1.1.9/24")
	if !changed || !strings.HasPrefix(out, "dhcp = false\nipv4 = \"10.1.1.9/24\"") {
		t.Fatalf("prepend inject failed:\n%s", out)
	}
}

func TestRevertLeaseRestoresDHCP(t *testing.T) {
	injected, _ := injectStaticIP(dhcpRaw, "10.1.1.7/24")

	lineRe := dhcpInjectLineRe("10.1.1.7/24")
	restored := lineRe.ReplaceAllString(injected, "")
	if lineRe.MatchString(restored) {
		t.Fatalf("injected line not removed:\n%s", restored)
	}
	restored = reDHCPOff.ReplaceAllStringFunc(restored, func(m string) string {
		if reHasStatic.MatchString(restored) {
			return m
		}
		idx := strings.Index(m, "=")
		return m[:idx+1] + " true"
	})
	if !strings.Contains(restored, "dhcp = true") {
		t.Fatalf("dhcp not restored:\n%s", restored)
	}
	if strings.Contains(restored, `ipv4 = "10.1.1.7/24"`) {
		t.Fatalf("ipv4 line survived:\n%s", restored)
	}
	if !strings.Contains(restored, "[[peer]]") {
		t.Fatal("peer table lost")
	}
}

// dhcpInjectLineRe mirrors the regex used by revertLease.
func dhcpInjectLineRe(cidr string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^\s*ipv4\s*=\s*"` + regexp.QuoteMeta(cidr) + `"\s*\n`)
}

func TestSplitCIDR(t *testing.T) {
	cases := []struct {
		in     string
		ip     string
		prefix int
		ok     bool
	}{
		{"10.1.1.2/24", "10.1.1.2", 24, true},
		{"10.1.1.2", "10.1.1.2", 24, true},
		{"fd::1", "fd::1", 24, true},
		{"DHCP", "", 0, false},
		{"", "", 0, false},
		{"10.1.1.2/99", "", 0, false},
	}
	for _, c := range cases {
		ip, p, ok := splitCIDR(c.in)
		if ok != c.ok || (ok && (ip != c.ip || p != c.prefix)) {
			t.Errorf("splitCIDR(%q) = %q,%d,%v want %q,%d,%v", c.in, ip, p, ok, c.ip, c.prefix, c.ok)
		}
	}
}
