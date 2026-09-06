//go:build !headless

// Desktop entry point (Wails window + tray). For the headless Linux server
// build see headless.go: go build -tags headless
package main

import (
	"context"
	"os"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// startup wires the Wails context: events flow to the frontend and the tray
// can show the window.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.SetEventSink(func(event string, data any) {
		runtime.EventsEmit(ctx, event, data)
	})
	a.Init()
	a.StartTray()
	// Wails can leave the window created-but-hidden when the process was
	// started elevated or from a hidden parent (UAC relaunch), which makes
	// the app unreachable behind a tray icon that may also fail to show.
	// Force it visible for normal launches; --minimized autostart stays in
	// the tray.
	if !a.startHidden {
		runtime.WindowShow(ctx)
	}
}

func main() {
	// Linux/macOS: mirror the Windows UAC prompt — an installed (root-owned)
	// GUI started unprivileged in a desktop session raises itself via pkexec
	// before anything else; the successor exits the wait in --elevated-relaunch.
	maybeSelfElevate()
	// UAC relaunch hand-off: the elevated copy starts while the old instance
	// is still shutting down, so wait for it to release the singleton mutex
	// before taking over (bounded wait; the mutex is also freed on a crash).
	for _, arg := range os.Args[1:] {
		if arg == "--elevated-relaunch" {
			waitForSingletonRelease(15 * time.Second)
			break
		}
	}
	// Single-instance guard: a second launch would fight the running one for
	// the web port and the per-network core listeners (bind error 10048).
	// The second process instead activates the existing window and exits.
	releaseSingleton, running := acquireSingleInstance()
	if running {
		return
	}
	defer releaseSingleton()

	app := NewApp()
	app.SetWebAssets(webAssets)

	// Launched at login by the autostart entry: start hidden in the tray.
	startHidden := false
	for _, arg := range os.Args[1:] {
		if arg == "--minimized" || arg == "--hidden" || arg == "-m" {
			startHidden = true
		}
	}
	app.startHidden = startHidden
	// Surface the window on demand when a second launch happens later.
	app.watchActivateRequests()

	err := wails.Run(&options.App{
		Title:       "EasyTier Pro",
		Width:       1200,
		Height:      800,
		MinWidth:    900,
		MinHeight:   600,
		StartHidden: startHidden,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour:  &options.RGBA{R: 15, G: 23, B: 42, A: 1},
		HideWindowOnClose: true,
		OnStartup:         app.startup,
		OnShutdown: func(ctx context.Context) {
			// Stop the child core and any public tunnels when the app really
			// exits (tray Quit) — a quick tunnel must not outlive the app.
			app.tunnelManager().StopAll()
			_ = app.StopCore()
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
