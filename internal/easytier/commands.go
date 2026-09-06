package easytier

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// NodeInfo is a slice-agnostic view of `easytier-cli node info`.
// Keep as raw JSON to let the frontend interpret the protobuf-shaped payload.
type NodeInfo struct {
	Raw json.RawMessage `json:"raw"`
}

// Version describes the bundled binaries.
type Version struct {
	Core string `json:"core"`
	Cli  string `json:"cli"`
}

// Query returns the local node information.
func (c *Client) QueryNodeInfo() (json.RawMessage, error) {
	return c.Run("node", "info")
}

// QueryNodeConfig returns the effective node config.
func (c *Client) QueryNodeConfig() (json.RawMessage, error) {
	return c.Run("node", "config")
}

// QueryPeers returns the peer list.
func (c *Client) QueryPeers() (json.RawMessage, error) {
	return c.Run("peer")
}

// QueryRoutes returns the route table.
func (c *Client) QueryRoutes() (json.RawMessage, error) {
	return c.Run("route")
}

// QueryStats returns statistics.
func (c *Client) QueryStats() (json.RawMessage, error) {
	return c.Run("stats")
}

// QueryVpnPortal returns WireGuard portal info.
func (c *Client) QueryVpnPortal() (json.RawMessage, error) {
	return c.Run("vpn-portal")
}

// VersionString returns the core version string reported by the RPC node.
func (c *Client) QueryVersion() (string, error) {
	raw, err := c.QueryNodeInfo()
	if err != nil {
		return "", err
	}
	var node struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(raw, &node)
	return node.Version, nil
}

// BinaryVersion returns the version token of a binary (does not need a running
// core). The banner is "easytier-core 2.6.4-8428a89d"; we return "2.6.4-8428a89d".
func BinaryVersion(binPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binPath, "--version")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	banner := strings.TrimSpace(string(out))
	parts := strings.Fields(banner)
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return banner
}
