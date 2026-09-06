//go:build !windows && !linux && !darwin

package main

import "errors"

func setAutoStart(enable bool) error {
	_ = enable
	return errors.New("autostart is not supported on this platform")
}

func getAutoStart() bool { return false }

func desktopNotify(title, body string) error {
	_ = title
	_ = body
	return errors.New("desktop notifications are not supported on this platform")
}

func copyToClipboard(text string) {
	_ = text
}
