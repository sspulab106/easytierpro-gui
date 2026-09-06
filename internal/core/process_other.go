//go:build !windows

package core

import (
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func hideWindow(cmd *exec.Cmd) {
	// Not needed on non-Windows platforms.
}

// killByPID terminates a process by PID: SIGTERM first, SIGKILL after a
// short grace period (the core has no window to close politely).
func killByPID(pid int) error {
	if pid <= 0 {
		return errInvalidPID
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return nil // process is gone
		}
		time.Sleep(100 * time.Millisecond)
	}
	return syscall.Kill(pid, syscall.SIGKILL)
}

// pidListeningOn returns the PID of the process listening on addr (host:port)
// via ss (Linux) or lsof (macOS/BSD). Parsing the owner PID may require
// privileges; 0 means "not found", not "error".
func pidListeningOn(addr string) int {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin", "freebsd", "openbsd", "netbsd":
		cmd = exec.Command("lsof", "-nP", "-iTCP:"+port, "-sTCP:LISTEN")
	default:
		cmd = exec.Command("ss", "-ltnp")
	}
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, ":"+port) {
			continue
		}
		// ss: users:(("easytier-core",pid=22708,fd=9))
		if i := strings.Index(line, "pid="); i >= 0 {
			rest := line[i+4:]
			if end := strings.IndexAny(rest, ",)"); end > 0 {
				if pid, err := strconv.Atoi(strings.TrimSpace(rest[:end])); err == nil {
					return pid
				}
			}
		}
		// lsof: easytier-core 22708 admin 5u IPv4 ... TCP 127.0.0.1:15888 (LISTEN)
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if pid, err := strconv.Atoi(fields[1]); err == nil && pid > 1 {
				return pid
			}
		}
	}
	return 0
}
