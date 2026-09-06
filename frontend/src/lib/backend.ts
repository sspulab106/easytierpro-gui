import { reactive } from 'vue'
import { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
import * as App from '../../wailsjs/go/main/App'
import { configmgr, easytier } from '../../wailsjs/go/models'

export type ConfigFile = configmgr.ConfigFile
export type Version = easytier.Version

export type CoreStatus = 'stopped' | 'starting' | 'running' | 'error'

/** True when running inside the Wails WebView (native bindings available). */
export const isNative = typeof window !== 'undefined' && !!(window as any).go

/**
 * Web management auth state. The desktop app is always trusted (native);
 * browser sessions must log in when an admin account exists.
 */
export const auth = reactive({
  checked: false, // bootstrap finished
  required: false, // server has an admin account configured
  user: '', // logged-in username ('' = logged out)
  role: 'viewer', // 'admin' | 'operator' | 'viewer'
  networks: [] as string[], // operator grant: networks this user may operate
})

/**
 * Unified backend client. In the desktop WebView it proxies to Go bindings;
 * in a regular browser it talks to the embedded HTTP web-management API.
 */
class Backend {
  private token = ''
  private native = isNative
  private initPromise: Promise<void> | null = null
  private onUnauthorized: (() => void) | null = null

  /** Register a callback fired when a browser session expires. */
  onAuthRequired(cb: () => void) {
    this.onUnauthorized = cb
  }

  init(): Promise<void> {
    if (this.native) {
      auth.checked = true
      // The desktop app is locally trusted: it operates as the built-in admin,
      // so admin-only pages (tunnels, users) stay visible in the sidebar.
      auth.user = auth.user || 'local'
      auth.role = 'admin'
      return Promise.resolve()
    }
    if (!this.initPromise) {
      this.initPromise = (async () => {
        try {
          const res = await fetch('/webconfig.json')
          const cfg = await res.json()
          this.token = cfg.token || ''
          auth.required = !!cfg.auth_required
          if (auth.required) {
            // Resume an existing session cookie if present.
            try {
              const me = await fetch('/api/auth/me')
              if (me.ok) {
                const body = await me.json()
                auth.user = body.username || ''
                auth.role = body.role || 'viewer'
                auth.networks = Array.isArray(body.networks) ? body.networks : []
              }
            } catch { /* not logged in */ }
          }
        } catch (e) {
          console.error('web config bootstrap failed', e)
        }
        auth.checked = true
      })()
    }
    return this.initPromise
  }

  private async http<T>(path: string, opts: RequestInit = {}): Promise<T> {
    await this.init()
    const headers: Record<string, string> = { ...(opts.headers as Record<string, string> | undefined) }
    if (this.token) headers['X-Auth-Token'] = this.token
    const res = await fetch(path, { ...opts, headers })
    if (res.status === 401 && auth.required) {
      auth.user = ''
      this.onUnauthorized?.()
    }
    if (!res.ok) {
      const body = await res.text()
      let msg = body || `HTTP ${res.status}`
      try { msg = JSON.parse(body).error || msg } catch { /* keep raw */ }
      throw new Error(msg)
    }
    return res.json() as Promise<T>
  }

  private unwrapError(e: unknown): never {
    throw new Error(String(e))
  }

  // ---- web auth ----

  async login(username: string, password: string): Promise<void> {
    if (this.native) return
    await this.init()
    const res = await fetch('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
    const body = await res.json().catch(() => ({}))
    if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`)
    auth.user = body.username || username
    auth.role = body.role || 'viewer'
    auth.networks = Array.isArray(body.networks) ? body.networks : []
  }

  async logout(): Promise<void> {
    if (this.native) return
    try {
      await fetch('/api/auth/logout', { method: 'POST' })
    } finally {
      auth.user = ''
      auth.role = 'viewer'
      auth.networks = []
    }
  }

  async changePassword(current: string, next: string, username?: string): Promise<void> {
    if (this.native) {
      await App.SetWebAccountPassword(current, next, username || '')
      return
    }
    await this.http('/api/auth/password', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ current_password: current, new_password: next, username: username || '' }),
    })
  }

  async rotateToken(): Promise<string> {
    if (this.native) return App.RotateWebToken()
    const r = await this.http<{ token: string }>('/api/auth/rotate-token', { method: 'POST' })
    this.token = r.token
    return r.token
  }

  hasAccount(): Promise<boolean> {
    if (this.native) return App.HasWebAccount() as Promise<boolean>
    return this.init().then(() => auth.required)
  }

  // ---- multi-user & device sessions ----

  listUsers(): Promise<any[]> {
    if (this.native) return App.ListUsers()
    return this.http('/api/users')
  }

  saveUser(u: { username: string; password?: string; role: string; networks?: string[] }): Promise<void> {
    if (this.native) return App.SaveUser(u.username, u.password || '', u.role, u.networks || [])
    return this.http('/api/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(u),
    }).then(() => undefined)
  }

  deleteUser(username: string): Promise<void> {
    if (this.native) return App.DeleteUser(username)
    return this.http(`/api/users?name=${encodeURIComponent(username)}`, { method: 'DELETE' }).then(() => undefined)
  }

  listSessions(): Promise<any[]> {
    if (this.native) return App.ListSessions()
    return this.http('/api/auth/sessions')
  }

  revokeSession(id: string): Promise<void> {
    if (this.native) return App.RevokeSession(id)
    return this.http('/api/auth/sessions/revoke', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id }),
    }).then(() => undefined)
  }

  async revokeOtherSessions(): Promise<number> {
    if (this.native) return App.RevokeOtherSessions()
    const r = await this.http<{ revoked: number }>('/api/auth/sessions/revoke-others', { method: 'POST' })
    return r.revoked
  }

  // ---- fleet: managed devices ----

  fleetList(): Promise<any> {
    if (this.native) return Promise.all([App.ListAgents(), App.AgentCommands()]).then(([agents, commands]: any[]) => ({ agents: agents ?? [], commands: commands ?? [] }))
    return this.http('/api/fleet')
  }

  createAgent(name: string): Promise<{ agent: any; token: string }> {
    if (this.native) return App.CreateAgent(name).then((r: any) => ({ agent: r.agent, token: r.token }))
    return this.http('/api/fleet', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name }),
    })
  }

  deleteAgent(id: string): Promise<void> {
    if (this.native) return App.DeleteAgent(id)
    return this.http(`/api/fleet?id=${encodeURIComponent(id)}`, { method: 'DELETE' }).then(() => undefined)
  }

  agentCommand(agentId: string, action: string, networkId?: string, networkToml?: string, target?: string): Promise<any> {
    if (this.native) return App.AgentCommand(agentId, action, networkId || '', networkToml || '', target || '')
    return this.http('/api/fleet/command', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ agent_id: agentId, action, network_id: networkId || '', network_toml: networkToml || '', target: target || '' }),
    })
  }

  // ---- cloudflared quick tunnels (admin; every endpoint is auth-gated) ----

  tunnelsList(): Promise<any> {
    if (this.native) {
      return Promise.all([App.ListTunnels(), App.CloudflaredStatus(), App.SSHStatus()]).then(
        ([t, cf, ssh]: any[]) => ({ tunnels: t ?? [], cloudflared: cf, ssh }),
      )
    }
    return this.http('/api/tunnels')
  }

  tunnelPeers(): Promise<any[]> {
    if (this.native) return App.TunnelPeers()
    return this.http('/api/tunnel-peers')
  }

  createTunnel(target: string, provider = 'cloudflared'): Promise<any> {
    if (this.native) {
      return provider === 'ssh' ? App.StartSSHTunnel(target) : App.StartTunnel(target)
    }
    return this.http('/api/tunnels', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target, provider }),
    })
  }

  stopTunnel(id: string): Promise<void> {
    if (this.native) return App.StopTunnel(id)
    return this.http(`/api/tunnels?id=${encodeURIComponent(id)}`, { method: 'DELETE' }).then(() => undefined)
  }

  retargetTunnel(id: string, target: string): Promise<any> {
    if (this.native) return App.RetargetTunnel(id, target)
    return this.http('/api/tunnels', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, target }),
    })
  }

  clearTunnelHistory(): Promise<number> {
    if (this.native) return App.ClearTunnelHistory()
    return this.http('/api/tunnels', { method: 'PATCH' }).then((r: any) => r.cleared ?? 0)
  }

  installCloudflared(): Promise<any> {
    if (this.native) return App.InstallCloudflared()
    return this.http('/api/tunnels/install', { method: 'POST' })
  }

  coreStatus(): Promise<CoreStatus> {
    if (this.native) return App.CoreStatus() as Promise<CoreStatus>
    return this.http<CoreStatus>('/api/status').then((s: any) => s.status as CoreStatus)
  }

  startCore(): Promise<void> {
    if (this.native) return App.StartCore()
    return this.http('/api/start').then(() => undefined)
  }

  stopCore(): Promise<void> {
    if (this.native) return App.StopCore()
    return this.http('/api/stop').then(() => undefined)
  }

  restartCore(): Promise<void> {
    if (this.native) return App.RestartCore()
    return this.http('/api/restart').then(() => undefined)
  }

  nodeInfo(): Promise<any> {
    if (this.native) return App.NodeInfo()
    return this.http('/api/node').catch(this.unwrapError)
  }

  peers(): Promise<any> {
    if (this.native) return App.Peers()
    return this.http('/api/peers').catch(this.unwrapError)
  }

  routes(): Promise<any> {
    if (this.native) return App.Routes()
    return this.http('/api/routes').catch(this.unwrapError)
  }

  stats(): Promise<any> {
    if (this.native) return App.Stats()
    return this.http('/api/stats').catch(this.unwrapError)
  }

  vpnPortal(): Promise<any> {
    if (this.native) return App.VpnPortal()
    return this.http('/api/vpn-portal').catch(this.unwrapError)
  }

  versions(): Promise<Version> {
    if (this.native) return App.Versions()
    return this.http('/api/status').then((s: any) => s.versions)
  }

  appInfo(): Promise<Record<string, string>> {
    if (this.native) return App.AppInfo()
    return this.http('/api/status').then((s: any) => s.addr)
  }

  coreLog(): Promise<string[]> {
    if (this.native) return App.CoreLog()
    return Promise.resolve([])
  }

  webInfo(): Promise<Record<string, string>> {
    if (this.native) return App.WebInfo()
    return this.init().then(() => ({
      running: 'true',
      addr: window.location.host,
      token: this.token,
    }))
  }

  getSettings(): Promise<any> {
    if (this.native) return App.GetSettings().then((raw: string) => JSON.parse(raw))
    return this.http('/api/settings')
  }

  saveSettings(cfg: any): Promise<void> {
    if (this.native) return App.SaveSettings(JSON.stringify(cfg))
    return this.http('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ raw: JSON.stringify(cfg) }),
    }).then(() => undefined)
  }

  // ---- core version management (download / update / switch) ----

  coreReleases(): Promise<any> {
    if (this.native) return App.CoreReleases() as Promise<any>
    return this.http('/api/core/releases')
  }

  coreInstalled(): Promise<any[]> {
    if (this.native) return App.CoreInstalled() as Promise<any[]>
    return this.http('/api/core/installed')
  }

  coreInstall(tag: string): Promise<void> {
    if (this.native) return App.CoreInstall(tag) as Promise<void>
    return this.http('/api/core/install', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tag }),
    }).then(() => undefined)
  }

  coreSetActive(tag: string): Promise<void> {
    if (this.native) return App.CoreSetActive(tag) as Promise<void>
    return this.http('/api/core/active', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tag }),
    }).then(() => undefined)
  }

  coreDelete(tag: string): Promise<void> {
    if (this.native) return App.CoreDelete(tag) as Promise<void>
    return this.http('/api/core/delete', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ tag }),
    }).then(() => undefined)
  }

  devicesList(): Promise<any[]> {
    if (this.native) return App.DevicesList() as Promise<any[]>
    return this.http('/api/devices')
  }

  deviceAct(action: 'approve' | 'deny' | 'remove', peerID: string, extra: { days?: number; hostname?: string; network?: string; ipv4?: string } = {}): Promise<void> {
    if (this.native) {
      if (action === 'approve') return App.DeviceApprove(peerID, extra.days ?? 0) as Promise<void>
      if (action === 'deny') return App.DeviceDenyCurrent(peerID, extra.hostname ?? '', extra.network ?? '', extra.ipv4 ?? '') as Promise<void>
      return App.DeviceRemove(peerID) as Promise<void>
    }
    return this.http('/api/devices/act', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, peer_id: peerID, ...extra }),
    }).then(() => undefined)
  }

  appLogTail(limit = 200): Promise<string[]> {
    if (this.native) return App.AppLogTail(limit) as Promise<string[]>
    return this.http<string[]>(`/api/applog?limit=${limit}`)
  }

  openDir(path: string): Promise<void> {
    if (this.native) return App.OpenDir(path)
    return this.http(`/api/open-dir?path=${encodeURIComponent(path)}`).then(() => undefined)
  }

  webdavPush(): Promise<string> {
    if (this.native) return App.WebdavPush()
    return this.http('/api/webdav/push').then((r: any) => r.msg || '')
  }

  webdavPull(): Promise<string> {
    if (this.native) return App.WebdavPull()
    return this.http('/api/webdav/pull').then((r: any) => r.msg || '')
  }

  trafficHistory(days: number): Promise<any> {
    if (this.native) return App.TrafficHistory(days)
    return this.http(`/api/traffic?days=${days}`)
  }

  probeMTU(target: string): Promise<number> {
    if (this.native) return App.ProbeMTU(target)
    return this.http('/api/tools/mtu-probe', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ target }),
    }).then((r: any) => r.mtu)
  }

  sendTestNotify(): Promise<void> {
    if (this.native) return App.SendTestNotify()
    return this.http('/api/tools/notify-test', { method: 'POST' }).then(() => undefined)
  }

  webTLSStatus(): Promise<any> {
    if (this.native) return App.WebTLSStatus()
    return this.http('/api/web-tls')
  }

  rotateWebTLSCert(): Promise<any> {
    if (this.native) return App.RotateWebTLSCert()
    return this.http('/api/web-tls/rotate', { method: 'POST' })
  }

  clearFleetPin(): Promise<void> {
    if (this.native) return App.ClearFleetPin()
    return this.http('/api/fleet/clear-pin', { method: 'POST' }).then(() => undefined)
  }

  auditTail(limit: number): Promise<any[]> {
    if (this.native) return App.AuditTail(limit)
    return this.http(`/api/audit?limit=${limit}`)
  }

  metricsText(): Promise<string> {
    if (this.native) return App.Metrics(0)
    return this.init().then(async () => {
      const headers: Record<string, string> = {}
      if (this.token) headers['X-Auth-Token'] = this.token
      const res = await fetch('/api/metrics', { headers })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.text()
    })
  }

  listLeases(): Promise<Record<string, { ip: string; prefix: number; hostname: string; updated: string }>> {
    if (this.native) return App.ListLeases()
    return this.http('/api/leases')
  }

  forgetLease(network: string): Promise<void> {
    if (this.native) return App.ForgetLease(network)
    return this.http('/api/leases', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ network }),
    }).then(() => undefined)
  }

  isAdmin(): Promise<boolean> {
    if (this.native) return App.IsAdmin() as Promise<boolean>
    return Promise.resolve(true) // browser mode assumed elevated
  }

  relaunchAsAdmin(): Promise<void> {
    if (this.native) return App.RelaunchAsAdmin()
    return Promise.resolve() // no-op in browser
  }

  pingDiagnostic(host: string, count: number): Promise<any> {
    if (this.native) return App.PingDiagnostic(host, count)
    return Promise.resolve({ host, sent: 0, received: 0, rtts: [] })
  }

  launchService(host: string, port: number, proto: string): Promise<void> {
    if (this.native) return App.LaunchService(host, port, proto)
    return Promise.resolve()
  }

  localSubnets(): Promise<string[]> {
    if (this.native) return App.LocalSubnets()
    return Promise.resolve([])
  }

  checkSubnetConflict(cidr: string): Promise<boolean> {
    if (this.native) return App.CheckSubnetConflict(cidr)
    return Promise.resolve(false)
  }

  listConfigs(): Promise<ConfigFile[]> {
    if (this.native) return App.ListConfigs()
    return this.http<ConfigFile[]>('/api/configs').catch(this.unwrapError)
  }

  saveConfig(cfg: ConfigFile): Promise<void> {
    if (this.native) return App.SaveConfig(cfg)
    return this.http('/api/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: cfg.instance_id, raw: cfg.raw, enabled: cfg.enabled }),
    }).then(() => undefined)
  }

  getConfig(id: string): Promise<ConfigFile> {
    if (this.native) return App.GetConfig(id)
    return this.http<ConfigFile>(`/api/config?id=${encodeURIComponent(id)}`).catch(this.unwrapError)
  }

  deleteConfig(id: string): Promise<void> {
    if (this.native) return App.DeleteConfig(id)
    return this.http(`/api/config?id=${encodeURIComponent(id)}`, { method: 'DELETE' }).then(() => undefined)
  }

  setConfigEnabled(id: string, enabled: boolean): Promise<void> {
    if (this.native) return App.SetConfigEnabled(id, enabled)
    return this.http('/api/config', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, enabled, raw: '' }),
    }).then(() => undefined)
  }

  /** Start/stop a single network. Operators may toggle networks in their grant. */
  setNetworkRunning(id: string, running: boolean): Promise<void> {
    if (this.native) return App.SetNetworkRunning(id, running)
    return this.http('/api/network-running', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id, running }),
    }).then(() => undefined)
  }

  applyConfigs(configs: ConfigFile[], enabledIDs: string[]): Promise<void> {
    if (this.native) return App.ApplyConfigs(configs, enabledIDs)
    // Browser fallback: one POST to /api/apply. The server persists each
    // config and reconciles per instance — only changed networks restart,
    // never a global stop of every running network.
    return this.http('/api/apply', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ configs, enabled_ids: enabledIDs }),
    }).then(() => undefined)
  }
}

export const backend = new Backend()

export const events = {
  coreStatus: 'core-status',
  coreError: 'core-error',
  coreLog: 'core-log',
  coreDownload: 'core-download',
}

/** Subscribe to a Go-emitted event. No-op in browser mode (polling used). */
export function onEvent<T>(event: string, cb: (data: T) => void): () => void {
  if (!isNative) return () => {}
  EventsOn(event, cb)
  return () => EventsOff(event)
}
