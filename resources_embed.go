package main

import (
	"bytes"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"

	"easytier-pro-gui/internal/core"
)

// ensureEmbeddedResources extracts the bundled easytier binaries (and TUN
// drivers on Windows) into the app-data runtime dir on first launch (or when
// content changed), and points the core paths at the extracted locations.
// On platforms without a bundled core it is a no-op and the GUI falls back
// to a system-wide easytier-core/easytier-cli on PATH.
func ensureEmbeddedResources() error {
	if !haveEmbeddedCore {
		return nil
	}
	runtimeDir := filepath.Join(core.DefaultPaths.AppDataDir(), "runtime")
	binDir := filepath.Join(runtimeDir, "bin")
	drvDir := filepath.Join(runtimeDir, "drivers")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(drvDir, 0o755); err != nil {
		return err
	}

	if err := extractFS(embeddedBin, embeddedBinRoot, binDir); err != nil {
		return err
	}
	if embeddedDriversRoot != "" {
		if err := extractFS(embeddedDrivers, embeddedDriversRoot, drvDir); err != nil {
			return err
		}
	}

	core.DefaultPaths.SetOverride(binDir, drvDir, "", "", "")
	return nil
}

// extractFS copies all files (recursively) from an embedded tree into dst.
// Existing files are compared by SHA-256 so a newer bundled binary replaces a
// stale extracted one (this is how core upgrades propagate to installed apps).
func extractFS(src fs.FS, root, dst string) error {
	return fs.WalkDir(src, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		data, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		if existing, err := os.ReadFile(target); err == nil {
			// Same content already extracted — nothing to do.
			if bytes.Equal(sha256sum(data), sha256sum(existing)) {
				return nil
			}
			// Stale binary: overwrite (best effort; ignore lock errors).
			_ = os.Remove(target)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o755)
	})
}

func sha256sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}
