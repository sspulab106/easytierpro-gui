//go:build windows

package core

import (
	"net"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// hideWindow prevents the child process from creating a visible console window.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// killByPID terminates a process by PID, trying a graceful close first and
// falling back to a forced kill. On Windows taskkill without /F posts a close
// request; a windowless console core can only be force-killed, which is fine.
func killByPID(pid int) error {
	if pid <= 0 {
		return errInvalidPID
	}
	soft := exec.Command("taskkill", "/PID", strconv.Itoa(pid))
	hideWindow(soft)
	if err := soft.Run(); err == nil {
		return nil
	}
	hard := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	hideWindow(hard)
	return hard.Run()
}

// pidListeningOn returns the PID of the process listening on addr (host:port)
// using netstat output.
func pidListeningOn(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	ns := exec.Command("netstat", "-ano")
	hideWindow(ns)
	out, err := ns.Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// TCP  127.0.0.1:15888  0.0.0.0:0  LISTENING  39212
		if len(fields) >= 5 && fields[0] == "TCP" && strings.HasSuffix(fields[1], ":"+port) && fields[3] == "LISTENING" {
			if pid, err := strconv.Atoi(fields[4]); err == nil {
				return pid
			}
		}
	}
	return 0
}
