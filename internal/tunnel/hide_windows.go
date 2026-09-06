//go:build windows

package tunnel

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	jobOnce sync.Once
	job     windows.Handle
)

// ensureJob creates one process-wide Job Object with KILL_ON_JOB_CLOSE: every
// tunnel child (cloudflared / ssh) assigned to it dies even when this app is
// killed forcefully — quick tunnels must never outlive their manager.
func ensureJob() {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			_ = windows.CloseHandle(h)
			return
		}
		job = h
	})
}

// hide prevents cloudflared/ssh from flashing a console window on Windows.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
}

// assignJob puts a started process into the kill-on-close job. Best effort;
// called right after cmd.Start().
func assignJob(cmd *exec.Cmd) {
	ensureJob()
	if job == 0 || cmd.Process == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.AssignProcessToJobObject(job, h)
}
