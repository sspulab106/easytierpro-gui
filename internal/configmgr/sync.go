// Cross-device TOML sanitization shared by WebDAV cloud sync and fleet
// remote commands: per-device fields (virtual IP, DHCP flag, hostname) never
// travel between machines.
package configmgr

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reLineIPv4 = regexp.MustCompile(`(?m)^\s*ipv4\s*=.*$`)
	reLineHost = regexp.MustCompile(`(?m)^\s*hostname\s*=.*$`)
	reLineDHCP = regexp.MustCompile(`(?m)^\s*dhcp\s*=.*$`)
	reCfgIID   = regexp.MustCompile(`(?m)^\s*instance_id\s*=\s*"([^"]+)"`)
)

// SharedTOML strips per-device fields from a config TOML: every device
// resolves its own address via DHCP (or its sticky-DHCP lease) on start.
func SharedTOML(raw string) string {
	out := reLineIPv4.ReplaceAllString(raw, "")
	out = reLineHost.ReplaceAllString(out, "")
	out = reLineDHCP.ReplaceAllString(out, "dhcp = true")
	return out
}

// InstanceID extracts the instance_id from a config TOML ("" if absent).
func InstanceID(raw string) string {
	if m := reCfgIID.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return ""
}

// DeviceFields holds the per-device parts of a local config.
type DeviceFields struct {
	DHCP     string // "true"/"false"
	IPv4     string // TOML value including quotes, e.g. "10.1.1.7/24"
	Hostname string // TOML value including quotes
}

// ExtractDeviceFields reads a device's own identity from a local config.
func ExtractDeviceFields(raw string) DeviceFields {
	f := DeviceFields{DHCP: "true"}
	if m := reLineIPv4.FindString(raw); m != "" {
		f.IPv4 = trimField(m)
	}
	if m := reLineHost.FindString(raw); m != "" {
		f.Hostname = trimField(m)
	}
	if m := reLineDHCP.FindString(raw); m != "" {
		if trimField(m) == "false" {
			f.DHCP = "false"
		}
	}
	return f
}

// ReapplyDeviceFields merges a device's own identity back into a shared
// (sanitized) config after a cloud restore.
func ReapplyDeviceFields(shared string, f DeviceFields) string {
	out := SharedTOML(shared)
	if f.IPv4 != "" && f.DHCP == "false" {
		out = reLineDHCP.ReplaceAllString(out, "dhcp = false\nipv4 = "+f.IPv4)
	}
	if f.Hostname != "" {
		if reLineDHCP.MatchString(out) {
			out = reLineDHCP.ReplaceAllStringFunc(out, func(m string) string {
				return m + "\nhostname = " + f.Hostname
			})
		} else {
			out = "hostname = " + f.Hostname + "\n" + out
		}
	}
	return out
}

func trimField(line string) string {
	v := strings.TrimSpace(strings.SplitN(line, "=", 2)[1])
	return v
}

// ---- per-instance listener healing ----
//
// Per-instance core processes must not share listener ports: a config with
// no explicit listeners would let every process grab the default 11010 and
// all but the first would fail to start.

var (
	reListeners = regexp.MustCompile(`(?m)^\s*listeners\s*=\s*\[[^\]]*\]\s*$`)
	reCfgIIDAny = regexp.MustCompile(`(?m)^\s*instance_id\s*=.*$`)
)

// HasListeners reports whether the config declares an explicit listeners
// array (even an empty one, which means "bind nothing").
func HasListeners(raw string) bool {
	return reListeners.MatchString(raw)
}

// InjectListeners adds `listeners = [tcp/udp 0.0.0.0:<port>]` right after
// the instance_id line (top-level TOML keys must precede any [table]).
func InjectListeners(raw string, port int) (string, error) {
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid listener port %d", port)
	}
	entry := fmt.Sprintf("listeners = [\"tcp://0.0.0.0:%d\", \"udp://0.0.0.0:%d\"]", port, port)
	if reListeners.MatchString(raw) {
		return reListeners.ReplaceAllString(raw, entry), nil
	}
	loc := reCfgIIDAny.FindStringIndex(raw)
	if loc == nil {
		return entry + "\n" + raw, nil
	}
	end := loc[1]
	if end < len(raw) && raw[end] == '\r' {
		end++
	}
	return raw[:end] + "\n" + entry + raw[end:], nil
}

// ReplaceListenerPorts rewrites an existing listeners array to use the given
// port (tcp + udp), preserving any other schemes (wss/ws entries).
func ReplaceListenerPorts(raw string, port int) (string, error) {
	if port <= 0 || port > 65535 {
		return "", fmt.Errorf("invalid listener port %d", port)
	}
	m := reListeners.FindString(raw)
	if m == "" {
		return InjectListeners(raw, port)
	}
	// keep non-tcp/udp entries as-is
	var keep []string
	for _, e := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(m, -1) {
		if !strings.HasPrefix(e[1], "tcp://") && !strings.HasPrefix(e[1], "udp://") {
			keep = append(keep, "\""+e[1]+"\"")
		}
	}
	entries := append(keep,
		fmt.Sprintf("\"tcp://0.0.0.0:%d\"", port),
		fmt.Sprintf("\"udp://0.0.0.0:%d\"", port))
	entry := "listeners = [" + strings.Join(entries, ", ") + "]"
	return reListeners.ReplaceAllString(raw, entry), nil
}
