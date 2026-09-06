package main

// Peer data shared by the tray menu (desktop) and the MagicDNS hosts sync +
// watchdog (both desktop and headless server). Kept free of build tags.

import (
	"encoding/json"
	"sort"

	"easytier-pro-gui/internal/configmgr"
	"easytier-pro-gui/internal/easytier"
)

// trayPeer is one connected remote peer.
type trayPeer struct {
	Hostname string
	IP       string
}

// trayPeersByNetwork returns connected remote peers grouped by network name.
func (a *App) trayPeersByNetwork(configs []configmgr.ConfigFile) map[string][]trayPeer {
	out := map[string][]trayPeer{}
	raw, err := a.queryAll((*easytier.Client).QueryPeers)
	if err != nil {
		return out
	}
	byID := map[string]string{}
	for _, c := range configs {
		byID[c.InstanceID] = c.Network
	}
	var groups []struct {
		InstanceID   string          `json:"instance_id"`
		InstanceName string          `json:"instance_name"`
		Result       json.RawMessage `json:"result"`
	}
	add := func(network string, arr []rawPeer) {
		for _, p := range arr {
			if p.Cost == "Local" || p.Hostname == "" {
				continue
			}
			out[network] = append(out[network], trayPeer{Hostname: p.Hostname, IP: p.IPv4})
		}
	}
	if err := json.Unmarshal(raw, &groups); err == nil && len(groups) > 0 && len(groups[0].Result) > 0 {
		for _, g := range groups {
			name := byID[g.InstanceID]
			if name == "" {
				name = g.InstanceName
			}
			add(name, parseRawPeers(g.Result))
		}
	} else {
		add(a.primaryNetworkName(), parseRawPeers(raw))
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool { return out[k][i].Hostname < out[k][j].Hostname })
	}
	return out
}
