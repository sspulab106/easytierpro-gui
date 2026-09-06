package easytier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Client wraps the bundled easytier-cli binary, always requesting JSON output.
type Client struct {
	cliPath string
	rpc     string

	mu    sync.Mutex
	cache map[string]cachedResult // short-TTL result cache (poll coalescing)
}

// cachedResult is a single cached CLI response.
type cachedResult struct {
	raw json.RawMessage
	err error
	ts  time.Time
}

// cacheTTL bounds how long a CLI result is reused. The dashboard polls several
// endpoints every 3 s; coalescing bursts (multiple endpoints within the TTL)
// cuts subprocess spawns significantly.
const cacheTTL = 400 * time.Millisecond

const maxCacheEntries = 32

func New(cliPath, rpcAddr string) *Client {
	if cliPath == "" {
		cliPath = "easytier-cli"
	}
	if rpcAddr == "" {
		rpcAddr = "127.0.0.1:15888"
	}
	return &Client{cliPath: cliPath, rpc: rpcAddr, cache: make(map[string]cachedResult)}
}

// Run executes easytier-cli <args...> with -o json and decodes stdout into v.
// raw, when non-nil, receives the raw JSON payload first. Results are cached
// for cacheTTL so concurrent pollers share one subprocess spawn.
func (c *Client) Run(args ...string) (json.RawMessage, error) {
	key := strings.Join(args, "\x00")

	c.mu.Lock()
	if hit, ok := c.cache[key]; ok && time.Since(hit.ts) < cacheTTL {
		c.mu.Unlock()
		return hit.raw, hit.err
	}
	c.mu.Unlock()

	raw, err := c.exec(args...)

	c.mu.Lock()
	if len(c.cache) >= maxCacheEntries {
		// Evict the oldest entry to bound memory.
		var oldestKey string
		var oldest time.Time
		for k, v := range c.cache {
			if oldestKey == "" || v.ts.Before(oldest) {
				oldestKey, oldest = k, v.ts
			}
		}
		delete(c.cache, oldestKey)
	}
	c.cache[key] = cachedResult{raw: raw, err: err, ts: time.Now()}
	c.mu.Unlock()
	return raw, err
}

// exec spawns easytier-cli once. Run() is the cached entry point.
func (c *Client) exec(args ...string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmdArgs := append([]string{"-o", "json", "-p", c.rpc}, args...)
	cmd := exec.CommandContext(ctx, c.cliPath, cmdArgs...)
	hideWindow(cmd) // Windows: avoid flashing a console window on each poll

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("easytier-cli %v failed: %s", args, msg)
	}

	return json.RawMessage(stdout.Bytes()), nil
}

// RunInto runs the command and unmarshals the JSON result into out.
func (c *Client) RunInto(out any, args ...string) error {
	raw, err := c.Run(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("failed to parse easytier-cli %v output: %w", args, err)
	}
	return nil
}
