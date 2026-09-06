//go:build !windows

package main

// firewallCleanup is a Windows-only maintenance task (no-op elsewhere).
func firewallCleanup() (int, error) { return 0, nil }
