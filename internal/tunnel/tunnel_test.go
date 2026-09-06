package tunnel

import "testing"

func TestNormalizeTargetRepairsFullWidth(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"http://127.0.0.1：56000", "http://127.0.0.1:56000", true},   // full-width colon
		{"http://127.0.0.1：５６０００", "http://127.0.0.1:56000", true},   // full-width colon + digits
		{"tcp://１０.１.１.１：２２", "tcp://10.1.1.1:22", true},             // fully CJK-typed
		{"127.0.0.1:8080", "http://127.0.0.1:8080", true},            // default scheme
		{"tcp://10.106.106.5:22", "tcp://10.106.106.5:22", true},     // unchanged
		{"ssh://10.0.0.1:22", "ssh://10.0.0.1:22", true},             // ssh allowed
		{"https://example.local:443", "https://example.local:443", true}, // https allowed
		{"　http://a.b：80　", "http://a.b:80", true},                   // ideographic spaces trimmed
		{"ftp://x:21", "", false},                                    // unsupported scheme
		{"", "", false},                                              // empty
	}
	for _, c := range cases {
		got, err := normalizeTarget(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("normalizeTarget(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("normalizeTarget(%q) = %q; want error", c.in, got)
		}
	}
}

func TestSSHURLRe(t *testing.T) {
	line := "02dcd05bb0978b.lhr.life tunneled with tls termination, https://02dcd05bb0978b.lhr.life"
	if got := sshURLRe.FindString(line); got != "https://02dcd05bb0978b.lhr.life" {
		t.Fatalf("sshURLRe = %q", got)
	}
	if sshURLRe.FindString("https://abc.trycloudflare.com") != "" {
		t.Fatal("sshURLRe must not match cloudflared URLs")
	}
}
