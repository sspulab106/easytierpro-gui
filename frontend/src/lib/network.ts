import { parse as tomlParse } from 'smol-toml'
import type { ConfigFile } from '@/lib/backend'

/** VPN portal client descriptor. */
export interface VpnPortalClient {
  name: string
  virtual_ip: string
  groups: string[]
}

/** Structured form model covering the full easytier TOML surface. */
export interface NetworkForm {
  // Basic
  instance_name: string
  hostname: string
  network_name: string
  network_secret: string
  // Addressing
  dhcp: boolean
  ipv4: string
  network_length: number
  ipv6_public_addr_auto: boolean
  // Networking
  listeners: string[]
  peers: string[]
  mapped_listeners: string[]
  // Proxy & routes
  proxy_networks: string[]
  routes: string[]
  exit_nodes: string[]
  relay_network_whitelist: string[]
  // VPN Portal (WireGuard)
  vpn_portal_listen: string
  vpn_portal_client_cidr: string
  vpn_portal_private_key: string
  vpn_portal_clients: VpnPortalClient[]
  // TUN / performance
  dev_name: string
  mtu: string
  instance_recv_bps_limit: string
  socks5_port: string
  // Advanced flags
  latency_first: boolean
  use_smoltcp: boolean
  disable_ipv6: boolean
  enable_kcp_proxy: boolean
  disable_kcp_input: boolean
  enable_quic_proxy: boolean
  disable_quic_input: boolean
  disable_p2p: boolean
  p2p_only: boolean
  lazy_p2p: boolean
  bind_device: boolean
  no_tun: boolean
  enable_exit_node: boolean
  relay_all_peer_rpc: boolean
  need_p2p: boolean
  multi_thread: boolean
  proxy_forward_by_system: boolean
  disable_encryption: boolean
  disable_tcp_hole_punching: boolean
  disable_udp_hole_punching: boolean
  enable_udp_broadcast_relay: boolean
  disable_upnp: boolean
  disable_sym_hole_punching: boolean
  enable_magic_dns: boolean
  enable_private_mode: boolean
  ipv6_public_addr_provider: boolean
  ipv6_public_addr_prefix: string
  // Port forwarding: [[port_forward]] proto/bind_addr/dst_addr
  port_forwards: PortForwardEntry[]
  // Access control: acl.chains (Inbound/Outbound/Forward rule chains)
  acl_chains: AclChain[]
}

export interface PortForwardEntry {
  proto: string // tcp | udp
  bind_addr: string // 0.0.0.0:13001
  dst_addr: string // 10.126.126.1:80
}

export interface AclRule {
  name: string
  priority: number
  enabled: boolean
  protocol: string // TCP | UDP | ICMP | ICMPv6 | Any
  ports: string[] // e.g. ["80", "4000-5000"]
  source_ips: string[]
  destination_ips: string[]
  action: string // Allow | Drop | Noop
}

export interface AclChain {
  name: string
  chain_type: 'Inbound' | 'Outbound' | 'Forward'
  enabled: boolean
  default_action: string
  rules: AclRule[]
}

export function emptyForm(listeners?: string[]): NetworkForm {
  return {
    instance_name: '',
    hostname: '',
    network_name: 'my-network',
    network_secret: '',
    dhcp: true,
    ipv4: '',
    network_length: 24,
    ipv6_public_addr_auto: false,
    listeners: listeners ?? ['tcp://0.0.0.0:11010', 'udp://0.0.0.0:11010'],
    peers: [],
    mapped_listeners: [],
    proxy_networks: [],
    routes: [],
    exit_nodes: [],
    relay_network_whitelist: [],
    vpn_portal_listen: '',
    vpn_portal_client_cidr: '10.14.14.0/24',
    vpn_portal_private_key: '',
    vpn_portal_clients: [],
    dev_name: '',
    mtu: '',
    instance_recv_bps_limit: '',
    socks5_port: '',
    latency_first: false,
    use_smoltcp: false,
    disable_ipv6: false,
    enable_kcp_proxy: false,
    disable_kcp_input: false,
    enable_quic_proxy: false,
    disable_quic_input: false,
    disable_p2p: false,
    p2p_only: false,
    lazy_p2p: false,
    bind_device: false,
    no_tun: false,
    enable_exit_node: false,
    relay_all_peer_rpc: false,
    need_p2p: false,
    multi_thread: false,
    proxy_forward_by_system: false,
    disable_encryption: false,
    disable_tcp_hole_punching: false,
    disable_udp_hole_punching: false,
    enable_udp_broadcast_relay: false,
    disable_upnp: false,
    disable_sym_hole_punching: false,
    enable_magic_dns: false,
    enable_private_mode: false,
    ipv6_public_addr_provider: false,
    ipv6_public_addr_prefix: '',
    port_forwards: [],
    acl_chains: [],
  }
}

function asStringArray(v: unknown): string[] {
  if (!Array.isArray(v)) return []
  return v.map((x) => String(x))
}

function asBool(v: unknown, def: boolean): boolean {
  return typeof v === 'boolean' ? v : def
}

/** Parse a raw easytier TOML config into the form model. */
export function formFromToml(raw: string): NetworkForm {
  const f = emptyForm()
  try {
    const doc = tomlParse(raw) as any
    f.instance_name = String(doc.instance_name ?? f.instance_name)
    f.hostname = String(doc.hostname ?? '')
    f.dhcp = asBool(doc.dhcp, true)
    f.network_length = Number(doc.network_length ?? 24)
    f.ipv6_public_addr_auto = asBool(doc.ipv6_public_addr_auto, false)
    f.ipv6_public_addr_provider = asBool(doc.ipv6_public_addr_provider, false)
    f.ipv6_public_addr_prefix = String(doc.ipv6_public_addr_prefix ?? '')
    f.listeners = asStringArray(doc.listeners)
    f.peers = Array.isArray(doc.peer)
      ? doc.peer.map((p: any) => String(p?.uri ?? '')).filter((x: string) => x)
      : asStringArray(doc.peers)
    f.mapped_listeners = asStringArray(doc.mapped_listeners)
    f.exit_nodes = asStringArray(doc.exit_nodes)
    f.routes = asStringArray(doc.routes)

    if (typeof doc.ipv4 === 'string') {
      const m = doc.ipv4.match(/^([0-9.]+)\/(\d+)/)
      if (m) {
        f.ipv4 = m[1]
        f.network_length = Number(m[2])
      } else {
        f.ipv4 = doc.ipv4
      }
    }

    // proxy_network tables: [{ cidr, mapped_cidr?, allow? }]
    if (Array.isArray(doc.proxy_network)) {
      f.proxy_networks = doc.proxy_network
        .map((p: any) => (p?.cidr ? String(p.cidr) : ''))
        .filter((x: string) => x)
    }

    // socks5_proxy = "socks5://0.0.0.0:1080"
    if (typeof doc.socks5_proxy === 'string') {
      const m = doc.socks5_proxy.match(/:(\d+)\s*\/?$/)
      if (m) f.socks5_port = m[1]
    }

    // port_forward: [[port_forward]] proto/bind_addr/dst_addr
    if (Array.isArray(doc.port_forward)) {
      f.port_forwards = doc.port_forward
        .map((pf: any) => ({
          proto: String(pf?.proto ?? 'tcp'),
          bind_addr: String(pf?.bind_addr ?? ''),
          dst_addr: String(pf?.dst_addr ?? ''),
        }))
        .filter((pf: PortForwardEntry) => pf.bind_addr && pf.dst_addr)
    }

    // acl: { chains: [...] } (inline tables or expanded tables both land here)
    const acl = doc.acl
    if (acl && Array.isArray(acl.chains)) {
      f.acl_chains = acl.chains.map((ch: any) => ({
        name: String(ch?.name ?? ''),
        chain_type: (['Inbound', 'Outbound', 'Forward'].includes(ch?.chain_type) ? ch.chain_type : 'Inbound') as AclChain['chain_type'],
        enabled: ch?.enabled !== false,
        default_action: String(ch?.default_action ?? 'Allow'),
        rules: Array.isArray(ch?.rules)
          ? ch.rules.map((r: any) => ({
              name: String(r?.name ?? ''),
              priority: Number(r?.priority ?? 0),
              enabled: r?.enabled !== false,
              protocol: String(r?.protocol ?? 'Any'),
              ports: asStringArray(r?.ports),
              source_ips: asStringArray(r?.source_ips),
              destination_ips: asStringArray(r?.destination_ips),
              action: String(r?.action ?? 'Allow'),
            }))
          : [],
      }))
    }

    // VPN portal
    const portal = doc.vpn_portal_config
    if (portal) {
      if (portal.wireguard_listen) {
        const l = String(portal.wireguard_listen)
        f.vpn_portal_listen = l.includes(':') && !l.startsWith('[')
          ? l.substring(0, l.lastIndexOf(':')) + ':' + l.substring(l.lastIndexOf(':') + 1)
          : l
      }
      if (portal.client_cidr) f.vpn_portal_client_cidr = String(portal.client_cidr)
      f.vpn_portal_private_key = String(portal.wireguard_private_key ?? '')
      f.vpn_portal_clients = Array.isArray(portal.clients)
        ? portal.clients.map((c: any) => ({
            name: String(c.name ?? ''),
            virtual_ip: String(c.virtual_ip ?? ''),
            groups: asStringArray(c.groups),
          }))
        : []
    }

    const ident = doc.network_identity ?? {}
    f.network_name = String(ident.network_name ?? f.network_name)
    f.network_secret = String(ident.network_secret ?? '')

    const flags = doc.flags ?? {}
    f.latency_first = asBool(flags.latency_first, false)
    f.use_smoltcp = asBool(flags.use_smoltcp, false)
    f.disable_ipv6 = typeof flags.enable_ipv6 === 'boolean' ? !flags.enable_ipv6 : false
    f.enable_kcp_proxy = asBool(flags.enable_kcp_proxy, false)
    f.disable_kcp_input = asBool(flags.disable_kcp_input, false)
    f.enable_quic_proxy = asBool(flags.enable_quic_proxy, false)
    f.disable_quic_input = asBool(flags.disable_quic_input, false)
    f.disable_p2p = asBool(flags.disable_p2p, false)
    f.p2p_only = asBool(flags.p2p_only, false)
    f.lazy_p2p = asBool(flags.lazy_p2p, false)
    f.bind_device = asBool(flags.bind_device, false)
    f.no_tun = asBool(flags.no_tun, false)
    f.enable_exit_node = asBool(flags.enable_exit_node, false)
    f.relay_all_peer_rpc = asBool(flags.relay_all_peer_rpc, false)
    f.need_p2p = asBool(flags.need_p2p, false)
    f.multi_thread = asBool(flags.multi_thread, false)
    f.proxy_forward_by_system = asBool(flags.proxy_forward_by_system, false)
    f.disable_encryption =
      typeof flags.enable_encryption === 'boolean' ? !flags.enable_encryption : false
    f.disable_tcp_hole_punching = asBool(flags.disable_tcp_hole_punching, false)
    f.disable_udp_hole_punching = asBool(flags.disable_udp_hole_punching, false)
    f.enable_udp_broadcast_relay = asBool(flags.enable_udp_broadcast_relay, false)
    f.disable_upnp = asBool(flags.disable_upnp, false)
    f.disable_sym_hole_punching = asBool(flags.disable_sym_hole_punching, false)
    f.enable_magic_dns = asBool(flags.accept_dns, false)
    f.enable_private_mode = asBool(flags.private_mode, false)
    if (flags.dev_name !== undefined) f.dev_name = String(flags.dev_name)
    if (flags.mtu !== undefined) f.mtu = String(flags.mtu)
    if (flags.instance_recv_bps_limit !== undefined) {
      f.instance_recv_bps_limit = String(flags.instance_recv_bps_limit)
    }
    if (typeof flags.relay_network_whitelist === 'string') {
      f.relay_network_whitelist = flags.relay_network_whitelist
        .split(/\s+/)
        .map((x: string) => x.trim())
        .filter((x: string) => x)
    }
  } catch {
    // fall back to empty form; raw editing still available
  }
  return f
}

function quote(s: string): string {
  return JSON.stringify(s)
}

/** Serialise the form back into an easytier TOML config. */
export function formToToml(f: NetworkForm, instanceId?: string): string {
  const lines: string[] = []

  if (instanceId) lines.push(`instance_id = ${quote(instanceId)}`)
  if (f.instance_name) lines.push(`instance_name = ${quote(f.instance_name)}`)
  if (f.hostname) lines.push(`hostname = ${quote(f.hostname)}`)
  lines.push(`dhcp = ${f.dhcp}`)
  if (!f.dhcp) {
    lines.push(`ipv4 = ${quote(f.ipv4 ? `${f.ipv4}/${f.network_length}` : '0.0.0.0/24')}`)
  }
  if (f.ipv6_public_addr_auto) lines.push(`ipv6_public_addr_auto = true`)
  if (f.ipv6_public_addr_provider) lines.push(`ipv6_public_addr_provider = true`)
  if (f.ipv6_public_addr_prefix) lines.push(`ipv6_public_addr_prefix = ${quote(f.ipv6_public_addr_prefix)}`)
  if (f.listeners.length) lines.push(`listeners = [${f.listeners.map(quote).join(', ')}]`)
  if (f.mapped_listeners.length) {
    lines.push(`mapped_listeners = [${f.mapped_listeners.map(quote).join(', ')}]`)
  }
  if (f.routes.length) lines.push(`routes = [${f.routes.map(quote).join(', ')}]`)
  if (f.exit_nodes.length) lines.push(`exit_nodes = [${f.exit_nodes.map(quote).join(', ')}]`)
  if (f.socks5_port) lines.push(`socks5_proxy = ${quote(`socks5://0.0.0.0:${f.socks5_port}`)}`)

  if (f.peers.length) {
    lines.push('')
    for (const p of f.peers) {
      if (p.trim()) {
        lines.push('[[peer]]')
        lines.push(`uri = ${quote(p.trim())}`)
      }
    }
  }

  if (f.proxy_networks.length) {
    lines.push('')
    for (const cidr of f.proxy_networks) {
      if (cidr.trim()) {
        lines.push('[[proxy_network]]')
        lines.push(`cidr = ${quote(cidr.trim())}`)
      }
    }
  }

  lines.push('')
  lines.push('[network_identity]')
  lines.push(`network_name = ${quote(f.network_name)}`)
  if (f.network_secret) lines.push(`network_secret = ${quote(f.network_secret)}`)

  // Port forwarding
  if (f.port_forwards.length) {
    lines.push('')
    for (const pf of f.port_forwards) {
      if (!pf.bind_addr || !pf.dst_addr) continue
      lines.push('[[port_forward]]')
      lines.push(`proto = ${quote(pf.proto || 'tcp')}`)
      lines.push(`bind_addr = ${quote(pf.bind_addr)}`)
      lines.push(`dst_addr = ${quote(pf.dst_addr)}`)
    }
  }

  // Access control (acl): chains with rules — emitted as inline tables so the
  // whole section stays one TOML value (schema verified against easytier-core).
  if (f.acl_chains.length) {
    const chains = f.acl_chains
      .filter((ch) => ch.name)
      .map((ch) => {
        const rules = ch.rules
          .filter((r) => r.name || r.source_ips.length || r.destination_ips.length || r.ports.length)
          .map((r) => {
            const parts = [`name = ${quote(r.name || 'rule')}`, `priority = ${Number(r.priority) || 0}`, 'enabled = true', `protocol = ${quote(r.protocol || 'Any')}`]
            if (r.ports.length) parts.push(`ports = [${r.ports.map(quote).join(', ')}]`)
            if (r.source_ips.length) parts.push(`source_ips = [${r.source_ips.map(quote).join(', ')}]`)
            if (r.destination_ips.length) parts.push(`destination_ips = [${r.destination_ips.map(quote).join(', ')}]`)
            parts.push(`action = ${quote(r.action || 'Allow')}`)
            return `{ ${parts.join(', ')} }`
          })
        const parts = [
          `name = ${quote(ch.name)}`,
          `chain_type = ${quote(ch.chain_type)}`,
          'enabled = true',
          `default_action = ${quote(ch.default_action || 'Allow')}`,
        ]
        if (rules.length) parts.push(`rules = [${rules.join(', ')}]`)
        return `{ ${parts.join(', ')} }`
      })
    if (chains.length) {
      lines.push('')
      lines.push('[acl]')
      lines.push(`chains = [${chains.join(', ')}]`)
    }
  }

  // VPN Portal
  if (f.vpn_portal_listen || f.vpn_portal_clients.length) {
    lines.push('')
    lines.push('[vpn_portal_config]')
    lines.push(`wireguard_listen = ${quote(f.vpn_portal_listen || '0.0.0.0:11015')}`)
    // The bundled easytier-core release requires the client subnet CIDR.
    if (f.vpn_portal_client_cidr) {
      lines.push(`client_cidr = ${quote(f.vpn_portal_client_cidr)}`)
    }
    if (f.vpn_portal_private_key) {
      lines.push(`wireguard_private_key = ${quote(f.vpn_portal_private_key)}`)
    }
    for (const c of f.vpn_portal_clients) {
      if (!c.name) continue
      lines.push('[[vpn_portal_config.clients]]')
      lines.push(`name = ${quote(c.name)}`)
      if (c.virtual_ip) lines.push(`virtual_ip = ${quote(c.virtual_ip)}`)
      if (c.groups.length) lines.push(`groups = [${c.groups.map(quote).join(', ')}]`)
    }
  }

  // Flags
  const flags: string[] = []
  if (f.dev_name) flags.push(`dev_name = ${quote(f.dev_name)}`)
  if (f.mtu) flags.push(`mtu = ${Number(f.mtu)}`)
  if (f.instance_recv_bps_limit) flags.push(`instance_recv_bps_limit = ${f.instance_recv_bps_limit}`)
  if (f.latency_first) flags.push('latency_first = true')
  if (f.use_smoltcp) flags.push('use_smoltcp = true')
  if (f.disable_ipv6) flags.push('enable_ipv6 = false')
  if (f.enable_kcp_proxy) flags.push('enable_kcp_proxy = true')
  if (f.disable_kcp_input) flags.push('disable_kcp_input = true')
  if (f.enable_quic_proxy) flags.push('enable_quic_proxy = true')
  if (f.disable_quic_input) flags.push('disable_quic_input = true')
  if (f.disable_p2p) flags.push('disable_p2p = true')
  if (f.p2p_only) flags.push('p2p_only = true')
  if (f.lazy_p2p) flags.push('lazy_p2p = true')
  if (f.bind_device) flags.push('bind_device = true')
  if (f.no_tun) flags.push('no_tun = true')
  if (f.enable_exit_node) flags.push('enable_exit_node = true')
  if (f.relay_all_peer_rpc) flags.push('relay_all_peer_rpc = true')
  if (f.need_p2p) flags.push('need_p2p = true')
  if (f.multi_thread) flags.push('multi_thread = true')
  if (f.proxy_forward_by_system) flags.push('proxy_forward_by_system = true')
  if (f.disable_encryption) flags.push('enable_encryption = false')
  if (f.disable_tcp_hole_punching) flags.push('disable_tcp_hole_punching = true')
  if (f.disable_udp_hole_punching) flags.push('disable_udp_hole_punching = true')
  if (f.enable_udp_broadcast_relay) flags.push('enable_udp_broadcast_relay = true')
  if (f.disable_upnp) flags.push('disable_upnp = true')
  if (f.disable_sym_hole_punching) flags.push('disable_sym_hole_punching = true')
  if (f.enable_magic_dns) flags.push('accept_dns = true')
  if (f.enable_private_mode) flags.push('private_mode = true')
  if (f.relay_network_whitelist.length) {
    flags.push(`relay_network_whitelist = ${quote(f.relay_network_whitelist.join(' '))}`)
  }
  if (flags.length) {
    lines.push('')
    lines.push('[flags]')
    lines.push(...flags)
  }

  return lines.join('\n').trim() + '\n'
}

const DEFAULT_LISTENER_PORT = 11010

/** Extract listener ports used by a raw easytier TOML config (tcp://host:port / udp://...). */
export function listenerPorts(raw: string): number[] {
  const ports: number[] = []
  const m = raw.match(/^\s*listeners\s*=\s*\[([^\]]*)\]/m)
  if (!m) return ports
  for (const url of m[1].match(/"[^"]+"/g) ?? []) {
    const clean = url.replace(/^"|"$/g, '')
    const p = clean.match(/:(\d+)\s*\/?$/)
    if (p) ports.push(Number(p[1]))
  }
  return ports
}

/** Pick the next free listener port (default 11010) that no config uses. */
export function freeListenerPort(usedRawConfigs: string[]): number {
  const used = new Set<number>()
  for (const raw of usedRawConfigs) {
    for (const p of listenerPorts(raw)) used.add(p)
  }
  let port = DEFAULT_LISTENER_PORT
  while (used.has(port)) port++
  return port
}

/** Return the default tcp+udp listener URLs for a free port. */
export function freeListeners(port: number): string[] {
  return [`tcp://0.0.0.0:${port}`, `udp://0.0.0.0:${port}`]
}

/** Build a ConfigFile from the form + raw text, keeping the original id. */
export function configFromForm(raw: string, form: NetworkForm, existing?: ConfigFile): ConfigFile {
  return {
    instance_id: existing?.instance_id ?? '',
    instance_name: form.instance_name,
    network: form.network_name,
    ipv4: form.dhcp ? '' : form.ipv4,
    enabled: existing?.enabled ?? true,
    path: existing?.path ?? '',
    raw,
  }
}
