//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchAgentID = "com.easytierpro.gui"

// getAutoStart reports whether the LaunchAgent plist exists.
func getAutoStart() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, "Library", "LaunchAgents", launchAgentID+".plist"))
	return err == nil
}

// setAutoStart installs/removes a LaunchAgent plist that launches the GUI
// (minimized to the tray) at login.
func setAutoStart(enable bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, "Library", "LaunchAgents")
	plist := filepath.Join(dir, launchAgentID+".plist")
	if !enable {
		_ = exec.Command("launchctl", "unload", plist).Run()
		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array><string>%s</string><string>--minimized</string></array>
  <key>RunAtLoad</key><true/>
</dict></plist>`, launchAgentID, exe)
	if err := os.WriteFile(plist, []byte(content), 0o644); err != nil {
		return err
	}
	return exec.Command("launchctl", "load", plist).Run()
}

// desktopNotify shows a notification via AppleScript.
func desktopNotify(title, body string) error {
	esc := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		return strings.ReplaceAll(s, `"`, `\"`)
	}
	script := fmt.Sprintf(`display notification "%s" with title "%s"`, esc(body), esc(title))
	return exec.Command("osascript", "-e", script).Run()
}

// copyToClipboard places text on the pasteboard via pbcopy.
func copyToClipboard(text string) {
	cmd := exec.Command("pbcopy")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	_, _ = stdin.Write([]byte(text))
	_ = stdin.Close()
	_ = cmd.Wait()
}
