import { parse as tomlParse } from 'smol-toml'
import type { ConfigFile } from '@/lib/backend'
import { emptyForm, formFromToml, formToToml, freeListenerPort, freeListeners } from '@/lib/network'

/**
 * Share-link helpers.
 *
 * Share format: easytier://join?network_name=...&network_secret=...&peer=...&peer=...
 * The same link can be pasted into the import box (or, for compatibility, raw
 * TOML config text is also accepted on import).
 */

const PREFIX = 'easytier://join?'

/** Build a join link from an existing config. */
export function buildShareLink(cfg: ConfigFile): string {
  let doc: any = {}
  try {
    doc = tomlParse(cfg.raw)
  } catch {
    doc = {}
  }

  const ident = doc.network_identity ?? {}
  const params = new URLSearchParams()
  params.set('network_name', String(ident.network_name ?? cfg.network ?? 'easytier-network'))
  if (ident.network_secret) params.set('network_secret', String(ident.network_secret))

  const peers = Array.isArray(doc.peer)
    ? doc.peer.map((p: any) => String(p?.uri ?? '')).filter((x: string) => x)
    : []
  // If the network has no explicit peer, the sharer's own listeners let
  // friends reach this node directly.
  if (peers.length) {
    for (const p of peers) params.append('peer', p)
  } else if (Array.isArray(doc.listeners)) {
    for (const l of doc.listeners) {
      const s = String(l)
      if (s.startsWith('tcp://') || s.startsWith('udp://')) {
        // 0.0.0.0/[::] are not reachable remotely — skip them.
        if (!/0\.0\.0\.0|\[::\]/.test(s)) params.append('peer', s)
      }
    }
  }
  return PREFIX + params.toString()
}

/** Parse an easytier:// join link into form fields. */
function parseShareUrl(url: string): { name: string; secret: string; peers: string[] } | null {
  try {
    const u = new URL(url)
    if (u.protocol !== 'easytier:') return null
    const name = u.searchParams.get('network_name') || 'easytier-network'
    const secret = u.searchParams.get('network_secret') || ''
    const peers = u.searchParams.getAll('peer')
    return { name, secret, peers }
  } catch {
    return null
  }
}

/**
 * Convert pasted text (share link or raw TOML) into a ready-to-save config.
 * Returns null when the content is not recognized.
 */
export function parseImport(text: string, existingRawConfigs: string[]): string | null {
  const input = text.trim()
  if (!input) return null

  if (input.startsWith('easytier://')) {
    const parsed = parseShareUrl(input)
    if (!parsed) return null
    const form = emptyForm()
    form.instance_name = parsed.name
    form.network_name = parsed.name
    form.network_secret = parsed.secret
    form.peers = parsed.peers
    const port = freeListenerPort(existingRawConfigs)
    form.listeners = freeListeners(port)
    const id = crypto.randomUUID()
    return formToToml(form, id)
  }

  // Raw TOML config text
  const form = formFromToml(input)
  if (!form.network_name && !form.instance_name) return null
  form.instance_name = form.instance_name || form.network_name
  const port = freeListenerPort(existingRawConfigs)
  form.listeners = freeListeners(port)
  const id = crypto.randomUUID()
  return formToToml(form, id)
}
