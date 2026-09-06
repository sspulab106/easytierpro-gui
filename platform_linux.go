//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// getAutoStart reports whether the XDG autostart entry exists.
func getAutoStart() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".config", "autostart", "easytier-pro-gui.desktop"))
	return err == nil
}

// setAutoStart installs/removes an XDG autostart .desktop entry so the GUI
// (minimized to tray) launches at session login. Root-owned (deb-installed)
// builds launch through pkexec so the session start is elevated the same way
// as a manual launch; portable builds launch directly, unprivileged.
func setAutoStart(enable bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".config", "autostart", "easytier-pro-gui.desktop")
	if !enable {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	execLine := fmt.Sprintf(`"%s" --minimized`, exe)
	if rootOwned() {
		if _, err := exec.LookPath("pkexec"); err == nil {
			execLine = fmt.Sprintf(`pkexec "%s" --minimized`, exe)
		}
	}
	content := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=EasyTier Pro\nExec=%s\nTerminal=false\nX-GNOME-Autostart-enabled=true\nCategories=Network;\n", execLine)
	return os.WriteFile(path, []byte(content), 0o644)
}

// desktopNotify sends a desktop notification via the freedesktop
// notification daemon.
func desktopNotify(title, body string) error {
	return exec.Command("notify-send", "-a", "EasyTier Pro", title, body).Run()
}

// copyToClipboard places text on the clipboard via xclip (xsel fallback).
func copyToClipboard(text string) {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	if err := cmd.Run(); err == nil {
		return
	}
	_ = exec.Command("xsel", "--clipboard", "--input").Run()
}
