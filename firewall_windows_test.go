//go:build windows

package main

import (
	"strings"
	"testing"
)

const sampleNetsh = `规则名称:                             EasyTier et_9_bjii - ALL Protocol (Outbound)
----------------------------------------------------------------------
已启用:                                是
配置文件:                              专用
----------------------------------------------------------------------
规则名称:                             EasyTier et_9_szsz - TCP Protocol (Inbound)
----------------------------------------------------------------------
规则名称:                             EasyTier et_p_11111111 - TCP Protocol (Inbound)
----------------------------------------------------------------------
规则名称:                             EasyTier C:\AppData\runtime\bin\easytier-core.exe (Inbound)
----------------------------------------------------------------------
规则名称:                             Some Other App (Inbound)
----------------------------------------------------------------------
`

func TestStaleEasyTierRuleNames(t *testing.T) {
	current := map[string]bool{"et_9_bjii": true} // adapter still exists
	got := staleEasyTierRuleNames(sampleNetsh, current)
	if len(got) != 1 || !strings.Contains(got[0], "et_9_szsz") {
		t.Fatalf("want only the gone-adapter rule (et_9_szsz), got %v", got)
	}
	all := strings.Join(got, "\n")
	for _, kept := range []string{"et_9_bjii", "et_p_11111111", "easytier-core.exe", "Some Other App"} {
		if strings.Contains(all, kept) {
			t.Fatalf("%q must be kept, got %v", kept, got)
		}
	}
}

func TestStaleEasyTierRuleNamesRemovesGoneAdapter(t *testing.T) {
	sample := `规则名称:                             EasyTier et_9_axse - TCP Protocol (Inbound)
----------------------------------------------------------------------
规则名称:                             EasyTier et_9_axse - UDP Protocol (Outbound)
----------------------------------------------------------------------
`
	current := map[string]bool{"et_9_olro": true} // axse adapter no longer exists
	got := staleEasyTierRuleNames(sample, current)
	if len(got) != 2 {
		t.Fatalf("want 2 stale rules, got %v", got)
	}
}
