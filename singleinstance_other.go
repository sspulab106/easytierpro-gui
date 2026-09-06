//go:build !windows && !headless

package main

import "time"

// acquireSingleInstance is a no-op outside Windows: the desktop build there
// is developer-only and the headless server manages its own lifecycle.
func acquireSingleInstance() (release func(), running bool) {
	return func() {}, false
}

// watchActivateRequests is a no-op outside Windows.
func (a *App) watchActivateRequests() {}

// waitForSingletonRelease is a no-op outside Windows: there is no singleton
// mutex to wait for.
func waitForSingletonRelease(time.Duration) {}
