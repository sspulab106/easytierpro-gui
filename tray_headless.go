//go:build headless

package main

// Headless stubs for the tray UI: the server has no tray, so the background
// loop's tray sync hooks are no-ops. Data functions live in peers_data.go.

// updateTrayStatus is a no-op without a tray.
func (a *App) updateTrayStatus() {}

// syncTrayMenu is a no-op without a tray.
func (a *App) syncTrayMenu() {}
