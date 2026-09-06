//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

// hideConsole prevents a spawned console app from flashing a window.
func hideConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// isElevated reports whether the current process runs with administrator
// privileges (required by easytier-core to create the wintun virtual adapter).
func isElevated() bool {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return false
	}
	defer token.Close()

	// TokenElevation (20): returns a nonzero value when the token is elevated.
	var elevation uint32
	var size uint32
	err = windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&size,
	)
	if err != nil {
		return false
	}
	return elevation != 0
}

// maybeSelfElevate is a no-op on Windows: elevation is handled before the
// process starts by the embedded requireAdministrator manifest (UAC prompt).
func maybeSelfElevate() {}

// relaunchElevated restarts the current executable elevated via the shell
// "runas" verb, which is what actually shows the UAC consent prompt. It
// returns after the shell accepts the request; the elevated instance then
// waits for this one to release the singleton mutex (see --elevated-relaunch
// in main.go) and the caller exits this instance to hand over.
func relaunchElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}
	dir, err := os.Getwd()
	if err != nil {
		dir = ""
	}
	cwd, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, cwd, windows.SW_SHOWNORMAL)
}
