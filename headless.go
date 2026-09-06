//go:build headless

// Headless server entry point for machines without a desktop (e.g. Linux
// servers). Same engine as the desktop app — web management UI, core process
// control, watchdog, traffic history, sticky-DHCP — without window or tray.
//
//	go build -tags headless -o easytier-pro-server .
//
// Then manage everything from http://<server>:<port>/ in a browser.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"easytier-pro-gui/internal/core"
)

func main() {
	var (
		bind    = flag.String("bind", "", "web management bind address (default: settings.json, fallback 127.0.0.1)")
		port    = flag.Int("port", 0, "web management port (default: settings.json, fallback random)")
		dataDir = flag.String("data-dir", "", "app data directory (default: ~/.local/share/easytier-pro-gui)")
		version = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()
	if *version {
		fmt.Println("easytier-pro-server")
		return
	}

	// Data dir override must happen before NewApp resolves paths.
	if *dataDir != "" {
		core.DefaultPaths.SetOverride("", "", *dataDir, "", "")
	}

	app := NewApp()
	app.SetWebAssets(webAssets)
	app.Init()

	// Command-line bind/port win over settings.json for systemd/container
	// deployments that want a fixed address.
	if *bind != "" || *port != 0 {
		if cfg, err := app.settings.Load(); err == nil {
			if *bind != "" {
				cfg.WebBind = *bind
			}
			if *port != 0 {
				cfg.WebPort = *port
			}
			_ = app.settings.Save(cfg)
			app.applySettings(cfg)
		}
	}

	fmt.Println("==========================================================")
	fmt.Println(" EasyTier Pro server started")
	fmt.Println(" Web management:  http://" + app.web.Addr())
	user, hash := app.WebAccount()
	if user == "" && hash == "" && len(app.Accounts()) == 0 {
		fmt.Println(" Bootstrap token: " + app.web.Token())
		fmt.Println(" (open the URL once to create an admin account)")
	} else {
		fmt.Println(" Account:         " + user + " (password login)")
	}
	fmt.Println(" Data directory:  " + core.DefaultPaths.AppDataDir())
	fmt.Println("==========================================================")

	// Graceful shutdown: stop the managed core on SIGINT/SIGTERM.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	fmt.Println("shutting down…")
	app.tunnelManager().StopAll()
	_ = app.StopCore()
	os.Exit(0)
}
