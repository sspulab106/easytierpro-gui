//go:build !windows

package easytier

import "os/exec"

func hideWindow(cmd *exec.Cmd) {
	// Not needed on non-Windows platforms.
}