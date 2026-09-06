//go:build windows

package easytier

import (
	"os/exec"
	"syscall"
)

// hideWindow prevents the child (easytier-cli) from flashing a console window.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
