// Command mkdeb assembles a Debian package (.deb) from one staged binary.
//
// The project cross-compiles on Windows, where dpkg-deb / GNU ar are not
// available, so the archive is written by hand: a .deb is an ar archive
// with exactly three members — debian-binary, control.tar.gz, data.tar.gz.
// The -list mode re-reads a package and prints its members for verification.
//
// Usage:
//
//	go run ./build/mkdeb -out easytier-pro_2.6.4_amd64.deb \
//	    -bin build/bin/easytier-pro-server-linux-amd64 \
//	    -arch amd64 -version 2.6.4
package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

type tgzEntry struct {
	name string // "./usr/bin/..." style, no leading slash
	mode int64
	dir  bool
	data []byte // regular file content (nil for dirs)
	from string // ...or a file to stream from disk
}

func buildTarGz(entries []tgzEntry) ([]byte, error) {
	buf := &strings.Builder{}
	zw := gzip.NewWriter(writerAdapter{buf})
	tw := tar.NewWriter(zw)
	stamp := time.Unix(0, 0).UTC()
	for _, e := range entries {
		h := &tar.Header{
			Name:    e.name,
			Mode:    e.mode,
			Uid:     0,
			Gid:     0,
			Uname:   "root",
			Gname:   "root",
			ModTime: stamp,
			Format:  tar.FormatGNU,
		}
		if e.dir {
			h.Typeflag = tar.TypeDir
			h.Name = strings.TrimSuffix(e.name, "/") + "/"
		} else {
			h.Typeflag = tar.TypeReg
			if e.from != "" {
				st, err := os.Stat(e.from)
				if err != nil {
					return nil, err
				}
				h.Size = st.Size()
			} else {
				h.Size = int64(len(e.data))
			}
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, err
		}
		if e.dir {
			continue
		}
		if e.from != "" {
			f, err := os.Open(e.from)
			if err != nil {
				return nil, err
			}
			n, err := io.Copy(tw, f)
			f.Close()
			if err != nil {
				return nil, err
			}
			_ = n
		} else {
			if _, err := tw.Write(e.data); err != nil {
				return nil, err
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// writerAdapter lets gzip write into a strings.Builder.
type writerAdapter struct{ b *strings.Builder }

func (w writerAdapter) Write(p []byte) (int, error) { return w.b.Write(p) }

type arMember struct {
	name string
	data []byte
}

func writeAr(out io.Writer, members []arMember) error {
	if _, err := out.Write([]byte("!<arch>\n")); err != nil {
		return err
	}
	for _, m := range members {
		// GNU ar pads every fixed-width header field with spaces (NUL
		// padding is only used for long-name string tables).
		hdr := fmt.Sprintf("%-16s%-12s%-6s%-6s%-8s%-10d`\n",
			m.name, "0", "0", "0", "100644", len(m.data))
		if len(hdr) != 60 {
			return fmt.Errorf("ar header length %d (name too long?)", len(hdr))
		}
		if _, err := io.WriteString(out, hdr); err != nil {
			return err
		}
		if _, err := out.Write(m.data); err != nil {
			return err
		}
		if len(m.data)%2 == 1 {
			if _, err := out.Write([]byte("\n")); err != nil { // ar pad to even
				return err
			}
		}
	}
	return nil
}

func controlFile(version, arch string, installedKB int64) []byte {
	return []byte(fmt.Sprintf(`Package: easytier-pro
Version: %s
Section: net
Priority: optional
Architecture: %s
Installed-Size: %d
Maintainer: EasyTier Pro <noreply@example.com>
Depends: systemd
Description: EasyTier Pro server — web management for the EasyTier mesh
 Self-contained headless server with embedded web UI and bundled
 easytier-core (amd64 builds). Manages networks, peers, tunnels and
 devices from any browser.
`, version, arch, installedKB))
}

func controlFileGUI(version, arch string, installedKB int64) []byte {
	return []byte(fmt.Sprintf(`Package: easytier-pro-gui
Version: %s
Section: net
Priority: optional
Architecture: %s
Installed-Size: %d
Maintainer: EasyTier Pro <noreply@example.com>
Depends: libgtk-3-0, libwebkit2gtk-4.0-37, libayatana-appindicator3-1, gvfs, polkitd | policykit-1
Description: EasyTier Pro — desktop GUI for the EasyTier mesh
 Graphical desktop application (GTK3 + WebKit2GTK) managing EasyTier
 networks: a local web view plus tray icon, auto-start, core version
 management and the bundled easytier-core (amd64 builds).
`, version, arch, installedKB))
}

const systemdUnit = `[Unit]
Description=EasyTier Pro server (EasyTier mesh web management)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/easytier-pro-server --data-dir /var/lib/easytier-pro
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`

const postinst = `#!/bin/sh
set -e
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
	systemctl enable easytier-pro.service >/dev/null 2>&1 || true
	systemctl restart easytier-pro.service >/dev/null 2>&1 || true
fi
#DEBHELPER#
exit 0
`

const prerm = `#!/bin/sh
set -e
if [ -d /run/systemd/system ]; then
	systemctl stop easytier-pro.service >/dev/null 2>&1 || true
fi
#DEBHELPER#
exit 0
`

const postrm = `#!/bin/sh
set -e
if [ "$1" = "purge" ]; then
	rm -rf /var/lib/easytier-pro
fi
if [ -d /run/systemd/system ]; then
	systemctl daemon-reload || true
	systemctl disable easytier-pro.service >/dev/null 2>&1 || true
fi
#DEBHELPER#
exit 0
`

func readme(arch string) []byte {
	coreNote := "The bundled easytier-core is included: no extra install steps."
	if arch != "amd64" {
		coreNote = `This architecture ships without a bundled easytier-core. Either
	install one from the web UI (Settings -> Core Version -> download) or
	provide a system-wide easytier-core on PATH.`
	}
	return []byte(fmt.Sprintf(`easytier-pro for Debian
=======================

The package installs /usr/bin/easytier-pro-server and a systemd unit
(easytier-pro.service) that starts it automatically on boot.

First run
---------
The web management address, bootstrap token and data directory are
printed to the service journal:

	journalctl -u easytier-pro

Open the URL in a browser and create the admin account (first visit
only). The listening address can be changed later in the web UI
(Settings); it persists in the settings file.

Layout
------
  /usr/bin/easytier-pro-server          server binary (static)
  /lib/systemd/system/easytier-pro.service
  /var/lib/easytier-pro                 data dir (configs, settings, logs)

%s

Uninstall
---------
  sudo dpkg -r easytier-pro        # keeps data in /var/lib/easytier-pro
  sudo dpkg -P easytier-pro        # purge: also deletes the data dir
`, coreNote))
}

const copyright = `Format: https://www.debian.org/doc/packaging-manuals/copyright-format/1.0/
Upstream-Name: easytier-pro
Source: https://github.com/EasyTier/EasyTier

License: refer to the upstream project repository for licensing terms.
`

// GUI variant extras: desktop launcher and postinst. Elevation needs no
// extra polkit files — pkexec's default policy already requires admin auth
// (the Linux UAC equivalent) for root-owned binaries.
const desktopEntry = `[Desktop Entry]
Type=Application
Name=EasyTier Pro
Comment=EasyTier mesh management (GUI)
Exec=easytier-pro-gui
Icon=easytier-pro
Terminal=false
Categories=Network;System;
Keywords=easytier;vpn;mesh;
`

const postinstGUI = `#!/bin/sh
set -e
# The GUI self-elevates via pkexec; the binary must be root-owned for
# pkexec to accept it (dpkg already sets that, this is belt-and-braces).
chown root:root /usr/bin/easytier-pro-gui || true
chmod 0755 /usr/bin/easytier-pro-gui || true
if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database >/dev/null 2>&1 || true
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
	gtk-update-icon-cache -f /usr/share/icons/hicolor >/dev/null 2>&1 || true
fi
#DEBHELPER#
exit 0
`

// GUI prerm/postrm: no service, no /var/lib data — user data lives in the
// invoking user's home and survives removal.
const prermGUI = `#!/bin/sh
set -e
pkill -f easytier-pro-gui >/dev/null 2>&1 || true
#DEBHELPER#
exit 0
`

const postrmGUI = `#!/bin/sh
set -e
#DEBHELPER#
exit 0
`

func main() {
	out := flag.String("out", "", "output .deb path")
	bin := flag.String("bin", "", "server binary to package")
	arch := flag.String("arch", "amd64", "deb architecture (amd64/arm64)")
	version := flag.String("version", "0.0", "package version")
	mode := flag.String("mode", "server", "package variant: server | gui")
	ico := flag.String("ico", "", "icon .png for the gui variant (installs to hicolor)")
	list := flag.String("list", "", "print members of an existing .deb and exit")
	flag.Parse()

	if *list != "" {
		listDeb(*list)
		return
	}
	if *out == "" || *bin == "" {
		flag.Usage()
		os.Exit(2)
	}

	st, err := os.Stat(*bin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var control []byte
	var data []byte
	if *mode == "gui" {
		pkgName := "easytier-pro-gui"
		control, err = buildTarGz([]tgzEntry{
			{name: "./", mode: 0o755, dir: true},
			{name: "./control", mode: 0o644, data: controlFileGUI(*version, *arch, st.Size()/1024)},
			{name: "./postinst", mode: 0o755, data: []byte(postinstGUI)},
			{name: "./prerm", mode: 0o755, data: []byte(prermGUI)},
			{name: "./postrm", mode: 0o755, data: []byte(postrmGUI)},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "control.tar.gz:", err)
			os.Exit(1)
		}
		entries := []tgzEntry{
			{name: "./", mode: 0o755, dir: true},
			{name: "./usr/", mode: 0o755, dir: true},
			{name: "./usr/bin/", mode: 0o755, dir: true},
			{name: "./usr/bin/" + pkgName, mode: 0o755, from: *bin},
			{name: "./usr/share/", mode: 0o755, dir: true},
			{name: "./usr/share/applications/", mode: 0o755, dir: true},
			{name: "./usr/share/applications/easytier-pro.desktop", mode: 0o644, data: []byte(desktopEntry)},
			{name: "./usr/share/doc/", mode: 0o755, dir: true},
			{name: "./usr/share/doc/easytier-pro-gui/", mode: 0o755, dir: true},
			{name: "./usr/share/doc/easytier-pro-gui/copyright", mode: 0o644, data: []byte(copyright)},
		}
		if *ico != "" {
			if _, err := os.Stat(*ico); err == nil {
				entries = append(entries,
					tgzEntry{name: "./usr/share/icons/", mode: 0o755, dir: true},
					tgzEntry{name: "./usr/share/icons/hicolor/", mode: 0o755, dir: true},
					tgzEntry{name: "./usr/share/icons/hicolor/512x512/", mode: 0o755, dir: true},
					tgzEntry{name: "./usr/share/icons/hicolor/512x512/apps/", mode: 0o755, dir: true},
					tgzEntry{name: "./usr/share/icons/hicolor/512x512/apps/easytier-pro.png", mode: 0o644, from: *ico},
				)
			}
		}
		data, err = buildTarGz(entries)
		if err != nil {
			fmt.Fprintln(os.Stderr, "data.tar.gz:", err)
			os.Exit(1)
		}
	} else {
		pkgName := "easytier-pro-server"
		control, err = buildTarGz([]tgzEntry{
			{name: "./", mode: 0o755, dir: true},
			{name: "./control", mode: 0o644, data: controlFile(*version, *arch, st.Size()/1024)},
			{name: "./postinst", mode: 0o755, data: []byte(postinst)},
			{name: "./prerm", mode: 0o755, data: []byte(prerm)},
			{name: "./postrm", mode: 0o755, data: []byte(postrm)},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "control.tar.gz:", err)
			os.Exit(1)
		}
		data, err = buildTarGz([]tgzEntry{
			{name: "./", mode: 0o755, dir: true},
			{name: "./usr/", mode: 0o755, dir: true},
			{name: "./usr/bin/", mode: 0o755, dir: true},
			{name: "./usr/bin/" + pkgName, mode: 0o755, from: *bin},
			{name: "./lib/", mode: 0o755, dir: true},
			{name: "./lib/systemd/", mode: 0o755, dir: true},
			{name: "./lib/systemd/system/", mode: 0o755, dir: true},
			{name: "./lib/systemd/system/easytier-pro.service", mode: 0o644, data: []byte(systemdUnit)},
			{name: "./usr/share/", mode: 0o755, dir: true},
			{name: "./usr/share/doc/", mode: 0o755, dir: true},
			{name: "./usr/share/doc/easytier-pro/", mode: 0o755, dir: true},
			{name: "./usr/share/doc/easytier-pro/copyright", mode: 0o644, data: []byte(copyright)},
			{name: "./usr/share/doc/easytier-pro/README.Debian", mode: 0o644, data: readme(*arch)},
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "data.tar.gz:", err)
			os.Exit(1)
		}
	}

	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	err = writeAr(f, []arMember{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.gz", control},
		{"data.tar.gz", data},
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%s/%s, v%s, %.1f MB)\n", *out, *mode, *arch, *version, float64(st.Size())/1024/1024)
}

// listDeb re-reads an assembled package and prints its members — a cheap
// structural self-check on the build machine (no dpkg available there).
func listDeb(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	magic := make([]byte, 8)
	if _, err := io.ReadFull(f, magic); err != nil || string(magic) != "!<arch>\n" {
		fmt.Fprintln(os.Stderr, "not an ar archive")
		os.Exit(1)
	}
	for {
		var hdr [60]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			break // EOF: done
		}
		name := strings.TrimSpace(string(hdr[0:16]))
		var size int64
		fmt.Sscanf(strings.TrimSpace(string(hdr[48:58])), "%d", &size)
		fmt.Printf("%-16s %10d bytes\n", name, size)
		if _, err := f.Seek(size+size%2, io.SeekCurrent); err != nil {
			break
		}
	}
}
