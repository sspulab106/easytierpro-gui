//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// isElevated reports whether the process runs as root. easytier-core needs
// root to create the TUN device (/dev/net/tun) on Linux.
func isElevated() bool {
	return os.Geteuid() == 0
}

// relaunchElevated restarts the GUI via pkexec — the polkit agent shows the
// system's own authentication dialog — and returns once the request has been
// accepted. The caller then exits and the elevated instance takes over.
func relaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return errors.New("pkexec not found — install polkit or run the app with sudo")
	}
	return exec.Command("pkexec", exe).Start()
}

// selfRelaunched reports whether this process was spawned as the elevated
// successor (guards the self-elevation attempt against loops).
func selfRelaunched() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--elevated-relaunch" {
			return true
		}
	}
	return false
}

// rootOwned reports whether the executable file belongs to root — polkit's
// pkexec refuses to run anything else, so only deb-installed builds
// self-elevate; portable builds show the warning banner instead.
func rootOwned() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	st, err := os.Stat(exe)
	if err != nil {
		return false
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	return ok && sys.Uid == 0
}

// maybeSelfElevate mirrors the Windows UAC prompt: when the GUI is started
// unprivileged from an installed (root-owned) location in a graphical
// session, raise itself to root via pkexec before anything else. It blocks
// until the polkit dialog is resolved — on success the elevated successor
// takes over and this process exits; on cancellation the caller continues
// unprivileged and the warning banner shows.
func maybeSelfElevate() {
	if isElevated() || selfRelaunched() {
		return
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}
	if !rootOwned() {
		return
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return
	}
	exe, _ := os.Executable()
	cmd := exec.Command("pkexec", exe, "--elevated-relaunch")
	if err := cmd.Start(); err != nil {
		return
	}
	if err := cmd.Wait(); err == nil {
		os.Exit(0)
	}
	// Auth cancelled or failed: continue unprivileged (banner explains).
}

func hideConsole(_ *exec.Cmd) {}
