// Package hostsfile maintains a marked EasyTier Pro block inside the system
// hosts file, mapping peer hostnames to their virtual IPs — the local
// equivalent of Tailscale's MagicDNS: `ssh gsjpc` keeps working even when the
// address changes, because the block is re-synced from live peer data.
package hostsfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const (
	blockStart = "# >>> easytier-pro-gui (managed) >>>"
	blockEnd   = "# <<< easytier-pro-gui (managed) <<<"
)

// Path returns the hosts file location for the current platform.
func Path() string {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("SystemRoot")
		if base == "" {
			base = `C:\Windows`
		}
		return filepath.Join(base, "System32", "drivers", "etc", "hosts")
	default:
		return "/etc/hosts"
	}
}

var (
	reValidName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	reComment   = regexp.MustCompile(`#.*$`)
)

// Store points the sync at a specific hosts file (overridable in tests).
type Store struct {
	mu  sync.Mutex
	path string
}

// NewStore creates a store for the system hosts file.
func NewStore() *Store { return &Store{path: Path()} }

// NewStoreAt creates a store for an arbitrary file (tests).
func NewStoreAt(path string) *Store { return &Store{path: path} }

// Update replaces the managed block with the given hostname→IP entries.
// Hostnames that already resolve from entries outside our block are skipped
// to avoid fighting the system administrator. Empty mapping removes the block.
func (s *Store) Update(entries map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.path)
	if err != nil {
		// Windows has a default hosts file; missing file on unix is fine.
		if !os.IsNotExist(err) {
			return err
		}
		raw = nil
	}

	outside, _ := splitBlock(string(raw))

	// Names already defined by the system admin keep their meaning.
	reserved := map[string]bool{}
	for _, line := range strings.Split(outside, "\n") {
		line = strings.TrimSpace(reComment.ReplaceAllString(line, ""))
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue // no hostnames on this line
		}
		for _, name := range fields[1:] {
			reserved[strings.ToLower(name)] = true
		}
	}

	var block strings.Builder
	block.WriteString(blockStart + "\n")
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, raw := range names {
		name := strings.ToLower(raw)
		ip := entries[raw]
		if ip == "" || !reValidName.MatchString(name) || reserved[name] {
			continue
		}
		fmt.Fprintf(&block, "%s\t%s # easytier-pro\n", ip, name)
	}
	block.WriteString(blockEnd + "\n")

	newBody := strings.TrimRight(outside, "\n")
	if len(newBody) > 0 {
		newBody += "\n"
	}
	newBody += block.String()
	if newBody == string(raw) {
		return nil // nothing changed
	}
	return writeFile(s.path, []byte(newBody))
}

// Remove strips the managed block (used when MagicDNS is disabled).
func (s *Store) Remove() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	outside, _ := splitBlock(string(raw))
	newBody := strings.TrimRight(outside, "\n")
	if len(newBody) > 0 {
		newBody += "\n"
	}
	if newBody == string(raw) {
		return nil
	}
	return writeFile(s.path, []byte(newBody))
}

// HasBlock reports whether the managed block is present.
func (s *Store) HasBlock() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), blockStart)
}

// splitBlock separates the file into (outside, current block content).
func splitBlock(content string) (outside, block string) {
	i := strings.Index(content, blockStart)
	if i < 0 {
		return content, ""
	}
	j := strings.Index(content[i:], blockEnd)
	if j < 0 {
		// Unterminated block: reclaim everything from the marker.
		return content[:i], ""
	}
	absEnd := i + j + len(blockEnd)
	return content[:i], content[i:absEnd]
}

// writeFile rewrites the hosts file in place (preserves the file's ACL).
func writeFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
