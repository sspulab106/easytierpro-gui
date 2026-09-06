//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
)

var fwMu sync.Mutex

const createNoWindowFlag = 0x08000000

// mustDecodeConsole decodes netsh GBK output (Chinese Windows) to UTF-8.
func mustDecodeConsole(b []byte) string {
	if s, err := decodeConsoleText(b); err == nil {
		return s
	}
	return string(b)
}

func hideNetsh(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindowFlag,
	}
}

var (
	reRuleNameEasyTierAdapter = regexp.MustCompile(`(?i)^EasyTier\s+(et_[0-9A-Za-z_]+)\s+-`)
	reRuleNameEasyTierAny     = regexp.MustCompile(`(?i)^EasyTier\s`)
)

// firewallCleanup deletes stale "EasyTier <adapter> - ..." firewall rules
// left behind by easytier-core restarts: each start used to create a
// randomly-named TUN adapter (et_9_xxxx) → Windows added 8 new allow rules
// per restart. The fix has two halves:
//  1. configs now pin dev_name = "et_p_<instance>" (see configmgr), so the
//     adapter name — and its rules — stay stable across restarts;
//  2. this cleanup removes rules tied to adapters that no longer exist.
//
// Returns how many rules were removed. Safe to run repeatedly.
func firewallCleanup() (int, error) {
	fwMu.Lock()
	defer fwMu.Unlock()

	current, err := currentTunAdapters()
	if err != nil {
		return 0, err
	}

	sr := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name=all")
	hideNetsh(sr)
	out, err := sr.Output()
	if err != nil {
		return 0, err
	}
	names := staleEasyTierRuleNames(mustDecodeConsole(out), current)

	removed := 0
	for _, name := range names {
		del := exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", fmt.Sprintf("name=%q", name))
		hideNetsh(del)
		if err := del.Run(); err == nil {
			removed++
		}
	}
	return removed, nil
}

// currentTunAdapters lists existing easytier TUN adapter names (lowercase).
func currentTunAdapters() (map[string]bool, error) {
	ps := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-NetAdapter | Where-Object { $_.Name -like 'et_*' }).Name")
	hideNetsh(ps)
	out, err := ps.Output()
	if err != nil {
		return nil, err
	}
	current := map[string]bool{}
	for _, line := range strings.Split(mustDecodeConsole(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			current[strings.ToLower(name)] = true
		}
	}
	return current, nil
}

// staleEasyTierRuleNames extracts deletable rule names: EasyTier rules tied
// to an adapter (et_...) that no longer exists. Program-level rules
// ("EasyTier <path> ...") and our stable et_p_* scheme are always kept.
func staleEasyTierRuleNames(netshOutput string, currentAdapters map[string]bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(netshOutput, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		if key != "rule name" && key != "规则名称" {
			continue
		}
		name := strings.TrimSpace(line[idx+1:])
		if !reRuleNameEasyTierAny.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		m := reRuleNameEasyTierAdapter.FindStringSubmatch(name)
		if m == nil {
			continue // program-level rule → keep
		}
		adapter := strings.ToLower(m[1])
		if strings.HasPrefix(adapter, "et_p_") || currentAdapters[adapter] {
			continue // stable scheme or adapter still exists → keep
		}
		out = append(out, name)
	}
	return out
}
