//go:build windows && !headless

package main

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// singletonMutexName is a per-session (Local\) named mutex: a second desktop
// instance must never start, because the two would fight over the web port
// and the per-network core listeners (bind error 10048).
const singletonMutexName = `Local\easytier-pro-gui-singleton`

// activateEventName is signaled by a second launch to ask the running
// instance to bring its window up. The event is created by the first
// instance, so a non-elevated launcher can activate an elevated one —
// showing the window cross-process directly is blocked by UIPI in that
// direction, while signaling a same-user event is not.
const activateEventName = `Local\easytier-pro-gui-activate`

// errAlreadyRunning reports that another instance of this app is running
// (and has been asked to surface its window).
var errAlreadyRunning = errors.New("easytier-pro-gui is already running")

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	mbInfoFlag = 0x40 // MB_ICONINFORMATION
)

// acquireSingleInstance takes the singleton mutex. It reports running=true
// when another instance already owns the mutex — in that case the existing
// window has been brought to the front and the caller must exit.
// Otherwise it returns a release func for the caller to defer.
func acquireSingleInstance() (release func(), running bool) {
	name, err := windows.UTF16PtrFromString(singletonMutexName)
	if err != nil {
		// Never keep the app from starting over our own naming bug.
		return func() {}, false
	}
	h, err := windows.CreateMutex(nil, true, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if h != 0 {
			_ = windows.CloseHandle(h)
		}
		activateExistingInstance()
		return nil, true
	}
	if err != nil || h == 0 {
		// Mutex creation failed for an unexpected reason (permissions,
		// sandboxing). Log and continue rather than refusing to start.
		println("singleton mutex unavailable:", fmt.Sprint(err))
		return func() {}, false
	}
	return func() {
		_ = windows.ReleaseMutex(h)
		_ = windows.CloseHandle(h)
	}, false
}

// waitForSingletonRelease blocks until the singleton mutex is gone — the
// previous instance has exited — or the timeout elapses. A UAC relaunch
// starts the new process while the old one is still shutting down, so the
// --elevated-relaunch instance waits here instead of activating the dying
// window and exiting. The mutex is kernel-owned, so it is also freed when
// the previous instance crashes.
func waitForSingletonRelease(timeout time.Duration) {
	name, err := windows.UTF16PtrFromString(singletonMutexName)
	if err != nil {
		return
	}
	deadline := time.Now().Add(timeout)
	for {
		h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
		if err != nil {
			return // no mutex: previous instance has exited
		}
		_ = windows.CloseHandle(h)
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// watchActivateRequests listens for second-launch activation signals and
// surfaces the main window. It runs for the whole process lifetime, which
// also gives the user a way back in when the tray icon is not reachable.
func (a *App) watchActivateRequests() {
	name, err := windows.UTF16PtrFromString(activateEventName)
	if err != nil {
		return
	}
	h, err := windows.CreateEvent(nil, 0, 0, name) // auto-reset, unsignaled
	if err != nil || h == 0 {
		println("activate event unavailable:", fmt.Sprint(err))
		return
	}
	go func() {
		defer windows.CloseHandle(h)
		for {
			state, err := windows.WaitForSingleObject(h, windows.INFINITE)
			if err != nil || state != windows.WAIT_OBJECT_0 {
				return
			}
			if a.ctx == nil {
				return
			}
			runtime.WindowUnminimise(a.ctx)
			runtime.WindowShow(a.ctx)
		}
	}()
}

// activateExistingInstance asks the running instance to show its window
// (via the activate event, then a best-effort direct ShowWindow) and tells
// the user what happened.
func activateExistingInstance() {
	if name, err := windows.UTF16PtrFromString(activateEventName); err == nil {
		if h, err := windows.CreateEvent(nil, 0, 0, name); err == nil && h != 0 {
			_ = windows.SetEvent(h)
			_ = windows.CloseHandle(h)
		}
	}
	cls, err := syscall.UTF16PtrFromString("wailsWindow")
	if err == nil {
		hwnd, _, _ := user32.NewProc("FindWindowW").Call(
			uintptr(unsafe.Pointer(cls)), 0)
		if hwnd != 0 {
			const swRestore = 9
			user32.NewProc("ShowWindow").Call(hwnd, swRestore)
			user32.NewProc("SetForegroundWindow").Call(hwnd)
		}
	}
	text, _ := syscall.UTF16PtrFromString(
		"EasyTier Pro 已在运行，已为您打开主窗口。\n如果窗口没有出现，请检查任务栏右下角的 EasyTier Pro 图标。")
	caption, _ := syscall.UTF16PtrFromString("EasyTier Pro")
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(caption)), uintptr(mbInfoFlag))
}
