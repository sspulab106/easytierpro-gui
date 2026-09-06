// Shared topology helpers: normalize easytier-cli rows and build the
// node list consumed by TopologyView. Used by the Dashboard and the Peers
// page so both render identical topology data.

export interface TopologyNode {
  id: string
  hostname: string
  ipv4: string
  tunnel: string
  latency: string
  loss: string
  nat: string
  cost: string
  isSelf: boolean
  isRelay: boolean
  proxyCidrs: string[]
}

export interface TopologyData {
  localHostname: string
  localIp: string
  localPeerId: string
  nodes: TopologyNode[]
}

// easytier-cli returns different shapes depending on instance count:
// - multiple instances: [{ instance_id, instance_name, result }]
// - single instance:    node info -> plain object (with inst_id),
//                       peer/route -> plain array
// Normalize everything into the grouped form.
export function resolveRows<T>(raw: any, fallbackId: string): Array<{ instance_id: string; result: T }> {
  if (!raw) return []
  if (Array.isArray(raw) && raw.length && raw[0] && typeof raw[0] === 'object' && 'result' in raw[0]) {
    return raw as Array<{ instance_id: string; result: T }>
  }
  const id = raw && typeof raw === 'object' && raw.inst_id ? String(raw.inst_id) : fallbackId
  return [{ instance_id: id, result: raw as T }]
}

// Build the topology node list for one instance: local node plus peers,
// with relay detection (PublicServer hostnames) and per-host proxy CIDRs
// aggregated from route rows.
export function buildTopology(node: any, peerRows: any[], routeRows: any[] = []): TopologyData {
  const localId = String(node?.peer_id ?? '')
  const localHostname = node?.hostname || 'local'
  const localProxyCidrs = Array.isArray(node?.proxy_cidrs) ? node.proxy_cidrs : []

  const proxyByHost = new Map<string, string[]>()
  if (Array.isArray(routeRows)) {
    for (const r of routeRows) {
      const cidrs = String(r.proxy_cidrs ?? '').split(/\s+/).filter(Boolean)
      if (cidrs.length && r.hostname) {
        const existing = proxyByHost.get(r.hostname) ?? []
        proxyByHost.set(r.hostname, [...new Set([...existing, ...cidrs])])
      }
    }
  }
  const proxyFor = (hostname: string) => proxyByHost.get(hostname) ?? []

  const filtered = (Array.isArray(peerRows) ? peerRows : [])
    .filter((p) => String(p.id ?? p.peer_id ?? '') !== localId && p.cost !== 'Local')

  const nodes: TopologyNode[] = [{
    id: localId,
    hostname: localHostname,
    ipv4: node?.ipv4_addr || '',
    tunnel: '-',
    latency: '-',
    loss: '-',
    nat: '-',
    cost: 'Local',
    isSelf: true,
    isRelay: false,
    proxyCidrs: localProxyCidrs.length ? localProxyCidrs : proxyFor(localHostname),
  }]

  for (const p of filtered) {
    const isRelay = !!(p.hostname && p.hostname.startsWith('PublicServer'))
    nodes.push({
      id: String(p.id ?? p.peer_id ?? ''),
      hostname: p.hostname ?? '-',
      ipv4: p.ipv4 ?? p.cidr ?? '',
      tunnel: p.tunnel_proto ?? p.tunnel ?? '-',
      latency: p.lat_ms ?? '-',
      loss: p.loss_rate ?? '-',
      nat: p.nat_type ?? '-',
      cost: p.cost ?? '',
      isSelf: false,
      isRelay,
      proxyCidrs: proxyFor(p.hostname ?? ''),
    })
  }

  return { localHostname, localIp: node?.ipv4_addr || '', localPeerId: localId, nodes }
}
