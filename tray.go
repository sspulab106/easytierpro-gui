//go:build !headless

package main

import (
	_ "embed"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/getlantern/systray"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

// trayState is the package-level tray UI state. It lives outside App so the
// headless server build (which excludes this file) never links systray.
var trayState = struct {
	mu        sync.Mutex
	toggle    *systray.MenuItem
	netParent *systray.MenuItem
	nets      map[string]*systray.MenuItem
	peerRoots map[string]*systray.MenuItem
	peerItems map[string][]*systray.MenuItem
	sig       string
}{nets: map[string]*systray.MenuItem{}, peerRoots: map[string]*systray.MenuItem{}, peerItems: map[string][]*systray.MenuItem{}}

// StartTray runs the system tray in a background goroutine. It must be called
// from startup once the Wails context is available, because the menu actions
// interact with the main window.
func (a *App) StartTray() {
	go func() {
		// Win32 message loops are thread-affine: the tray window created
		// inside systray.Run must be pumped on the same OS thread that
		// created it. systray v1.2.2 does not lock the thread itself, and a
		// migrated goroutine leaves the icon visible but deaf — clicks and
		// right-clicks never reach the message queue.
		goruntime.LockOSThread()
		systray.Run(a.onTrayReady, nil)
	}()
}

// onTrayReady builds the static menu skeleton. The dynamic parts (network
// list, connected peers) are managed by syncTrayMenu from the background
// loop; the library has no menu reset, so items are added once and shown /
// hidden as state changes.
func (a *App) onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTitle("EasyTier Pro")
	systray.SetTooltip("EasyTier Pro VPN")

	mShow := systray.AddMenuItem("显示主窗口", "Show main window")
	systray.AddSeparator()
	mToggle := systray.AddMenuItem("启动核心", "Start / stop core")
	systray.AddSeparator()
	mNetworks := systray.AddMenuItem("网络", "Virtual networks — ● running / ○ stopped, click to toggle")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "Exit EasyTier Pro")

	trayState.mu.Lock()
	trayState.toggle = mToggle
	trayState.netParent = mNetworks
	trayState.nets = map[string]*systray.MenuItem{}
	trayState.peerRoots = map[string]*systray.MenuItem{}
	trayState.peerItems = map[string][]*systray.MenuItem{}
	trayState.mu.Unlock()
	a.updateTrayStatus()

	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				wailsruntime.Show(a.ctx)
			case <-mToggle.ClickedCh:
				if a.CoreStatus() == "running" {
					_ = a.StopCore()
				} else {
					_ = a.StartCore()
				}
			case <-mQuit.ClickedCh:
				_ = a.StopCore()
				systray.Quit()
				os.Exit(0)
			}
		}
	}()
}

// syncTrayMenu reconciles the dynamic tray menu with configs + running peers.
// Called from the background loop every 10 s and after network toggles; a
// content signature keeps no-op runs cheap (no menu rebuild while open).
func (a *App) syncTrayMenu() {
	trayState.mu.Lock()
	defer trayState.mu.Unlock()
	if trayState.netParent == nil || trayState.nets == nil {
		return // tray not ready yet
	}
	configs, err := a.config.List()
	if err != nil {
		return
	}
	peers := a.trayPeersByNetwork(configs)

	// Signature: config id/network/enabled + peer host/ip sets.
	var sig strings.Builder
	for _, c := range configs {
		fmt.Fprintf(&sig, "c:%s:%s:%v|", c.InstanceID, c.Network, c.Enabled)
		if c.Enabled {
			for _, p := range peers[c.Network] {
				fmt.Fprintf(&sig, "p:%s:%s|", p.Hostname, p.IP)
			}
		}
	}
	if sig.String() == trayState.sig {
		return
	}
	trayState.sig = sig.String()

	seen := map[string]bool{}
	for _, c := range configs {
		item := trayState.nets[c.InstanceID]
		if item == nil {
			item = trayState.netParent.AddSubMenuItem("", "")
			id := c.InstanceID
			go func() {
				for range item.ClickedCh {
					a.trayToggleNetwork(id)
				}
			}()
			trayState.nets[c.InstanceID] = item
		}
		if c.Enabled {
			item.SetTitle("● " + c.Network)
			item.SetTooltip("点击停止该网络")
		} else {
			item.SetTitle("○ " + c.Network)
			item.SetTooltip("点击启动该网络")
		}
		item.Show()
		seen[c.InstanceID] = true

		// Per-network connected-peers submenu (only for running networks).
		root := trayState.peerRoots[c.Network]
		if root == nil {
			root = trayState.netParent.AddSubMenuItem("", "")
			trayState.peerRoots[c.Network] = root
		}
		if c.Enabled {
			root.SetTitle("节点：" + c.Network)
			root.SetTooltip("在线节点 — 点击条目复制虚拟 IP")
		}
		// Peer rows change with connections: hide the old set, add fresh ones.
		for _, it := range trayState.peerItems[c.Network] {
			it.Hide()
		}
		var fresh []*systray.MenuItem
		if c.Enabled {
			for _, p := range peers[c.Network] {
				fresh = append(fresh, a.addTrayPeer(root, p)...)
			}
			root.Show()
		} else {
			root.Hide()
		}
		trayState.peerItems[c.Network] = fresh
	}
	for id, item := range trayState.nets {
		if !seen[id] {
			item.Hide() // config deleted
		}
	}
}

// addTrayPeer adds one peer row (click = copy IP) plus its actions submenu.
func (a *App) addTrayPeer(root *systray.MenuItem, p trayPeer) []*systray.MenuItem {
	label := p.Hostname
	if p.IP != "" {
		label += " (" + p.IP + ")"
	}
	items := []*systray.MenuItem{}

	mCopy := root.AddSubMenuItem(label, "点击复制虚拟 IP "+p.IP)
	go func(ip string) {
		for range mCopy.ClickedCh {
			copyToClipboard(ip)
			a.trayTip("已复制 " + ip)
		}
	}(p.IP)
	items = append(items, mCopy)

	if p.IP == "" {
		return items
	}

	mOps := root.AddSubMenuItem("操作："+p.Hostname, "复制命令或直接连接")
	items = append(items, mOps)

	mSSHCmd := mOps.AddSubMenuItem("复制 SSH 命令", "ssh root@"+p.IP)
	go func(ip string) {
		for range mSSHCmd.ClickedCh {
			copyToClipboard("ssh root@" + ip)
			a.trayTip("已复制 ssh root@" + ip)
		}
	}(p.IP)
	items = append(items, mSSHCmd)

	mSSH := mOps.AddSubMenuItem("SSH 连接", "Open an SSH session to "+p.IP)
	go func(ip string) {
		for range mSSH.ClickedCh {
			go func() { _ = a.LaunchService(ip, 22, "ssh") }()
		}
	}(p.IP)
	items = append(items, mSSH)

	mRDP := mOps.AddSubMenuItem("远程桌面连接", "Open RDP to "+p.IP)
	go func(ip string) {
		for range mRDP.ClickedCh {
			go func() { _ = a.LaunchService(ip, 3389, "rdp") }()
		}
	}(p.IP)
	items = append(items, mRDP)

	return items
}

// trayToggleNetwork flips one network's enabled state and applies it, with a
// completion toast so the action is acknowledged.
func (a *App) trayToggleNetwork(id string) {
	c, err := a.config.Get(id)
	if err != nil {
		return
	}
	name := c.Network
	running := !c.Enabled
	go func() {
		if err := a.SetNetworkRunning(id, running); err != nil {
			a.trayTip(name + " 操作失败: " + err.Error())
			return
		}
		if running {
			a.trayTip("网络 " + name + " 已启动")
		} else {
			a.trayTip("网络 " + name + " 已停止")
		}
		a.syncTrayMenu()
	}()
}

// // trayTip flashes the tooltip and raises a desktop toast so tray actions are
// acknowledged even when the menu closed instantly.
func (a *App) trayTip(msg string) {
	systray.SetTooltip("EasyTier Pro — " + msg)
	go func() {
		_ = desktopNotify("EasyTier Pro", msg)
		time.Sleep(2 * time.Second)
		systray.SetTooltip("EasyTier Pro VPN")
	}()
}

// updateTrayStatus reflects the core lifecycle in the tray menu label.
// Safe to call from any goroutine (core status callbacks).
func (a *App) updateTrayStatus() {
	trayState.mu.Lock()
	t := trayState.toggle
	trayState.mu.Unlock()
	if t == nil {
		return
	}
	status := a.CoreStatus()
	if status == "running" {
		t.SetTitle("停止核心")
	} else {
		t.SetTitle("启动核心")
	}
}
