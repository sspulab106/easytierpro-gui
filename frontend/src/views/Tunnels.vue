<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { backend, auth, isNative } from '@/lib/backend'

const { t } = useI18n()

const tunnels = ref<any[]>([])
const cloudflared = ref<any>(null)
const sshStatus = ref<any>(null)
// composed target: provider + scheme select + host:port input (keeps typos out)
const provider = ref('cloudflared')
const scheme = ref('http://')
const hostPort = ref('')
const busy = ref(false)
const installing = ref(false)
const err = ref('')
const msg = ref('')
const copied = ref('')
// editing an existing tunnel record: id of the record being replaced
const editingId = ref('')
const editingKind = ref('cloudflared')

// QR modal for a running tunnel's public URL
const qrURL = ref('')
const qrData = ref('')

// remote (fleet) tunnels
const agents = ref<any[]>([])
const remoteAgent = ref('')
const remoteTarget = ref('')

// mesh devices (virtual IPs) for the target picker
const meshPeers = ref<any[]>([])
const fleetCommands = ref<any[]>([])

// history: stopped/failed records, collapsed by day (latest open)
const expandedDays = ref<Record<string, boolean>>({})

const isAdmin = computed(() => !isNative ? auth.role === 'admin' : true)
const sshOK = computed(() => sshStatus.value?.installed === 'true')
const cfOK = computed(() => cloudflared.value?.installed === 'true')
const providerOK = computed(() => (provider.value === 'ssh' ? sshOK : cfOK).value)

const activeTunnels = computed(() =>
  tunnels.value.filter((x) => x.status === 'running' || x.status === 'starting'))
const historyGroups = computed(() => {
  const hist = tunnels.value
    .filter((x) => x.status !== 'running' && x.status !== 'starting')
    .sort((a, b) => new Date(b.created).getTime() - new Date(a.created).getTime())
  const groups: { day: string; items: any[] }[] = []
  for (const item of hist) {
    const day = (item.created || '').slice(0, 10) || '—'
    const g = groups.find((x) => x.day === day)
    if (g) g.items.push(item)
    else groups.push({ day, items: [item] })
  }
  return groups
})
const historyCount = computed(() =>
  historyGroups.value.reduce((n, g) => n + g.items.length, 0))

function isOpen(day: string, idx: number): boolean {
  // newest group starts open unless the user toggled it
  if (expandedDays.value[day] === undefined) return idx === 0
  return expandedDays.value[day]
}
function toggleDay(day: string) {
  expandedDays.value[day] = !isOpen(day, 0)
}

/** Repair common CJK full-width characters before sending to the backend. */
function sanitizeTarget(raw: string): string {
  let s = raw.trim()
  const map: Record<string, string> = { '：': ':', '／': '/', '　': ' ' }
  s = s.replace(/[：／　]/g, (ch) => map[ch] ?? ch)
  s = s.replace(/[０-９]/g, (ch) => String.fromCharCode(ch.charCodeAt(0) - 0xFF10 + 0x30))
  return s.trim()
}

let timer: ReturnType<typeof setInterval> | null = null

function uptime(created: string): string {
  const ms = Date.now() - new Date(created).getTime()
  if (!(ms > 0)) return '—'
  const s = Math.floor(ms / 1000)
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60)
  if (h) return `${h}h ${m}m`
  if (m) return `${m}m ${s % 60}s`
  return `${s}s`
}

function fillTarget(ip: string) {
  scheme.value = 'http://'
  hostPort.value = `${ip}:`
}

function fillRemoteTarget(ip: string) {
  remoteTarget.value = `http://${ip}:`
}

async function refresh() {
  try {
    const data = await backend.tunnelsList()
    tunnels.value = data.tunnels ?? []
    cloudflared.value = data.cloudflared
    sshStatus.value = data.ssh
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
  try {
    const f = await backend.fleetList()
    agents.value = (f.agents ?? []).filter((a: any) => a.online)
    fleetCommands.value = (f.commands ?? []).filter((c: any) => c.action.includes('tunnel')).slice(0, 8)
  } catch { /* fleet unavailable */ }
  try {
    meshPeers.value = await backend.tunnelPeers()
  } catch { /* peers unavailable */ }
}

/** Split a pasted full target into scheme + host:port when possible. */
function absorbPasted(raw: string) {
  const s = sanitizeTarget(raw)
  const i = s.indexOf('://')
  if (i > 0) {
    const sch = s.slice(0, i + 3).toLowerCase()
    if (['http://', 'https://', 'tcp://', 'ssh://'].includes(sch)) {
      scheme.value = sch
      hostPort.value = s.slice(i + 3)
      return
    }
  }
  hostPort.value = s
}

function startEdit(tn: any) {
  editingId.value = tn.id
  editingKind.value = tn.kind || 'cloudflared'
  provider.value = tn.kind === 'ssh' ? 'ssh' : 'cloudflared'
  absorbPasted(tn.target || '')
  msg.value = t('tunnels.editing', { target: tn.target })
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function cancelEdit() {
  editingId.value = ''
  editingKind.value = 'cloudflared'
  scheme.value = 'http://'
  hostPort.value = ''
  msg.value = ''
}

async function openQR(tn: any) {
  if (!tn.public_url) return
  try {
    qrData.value = await QRCode.toDataURL(tn.public_url, { width: 240, margin: 1 })
    qrURL.value = tn.public_url
  } catch { /* qr generation failed */ }
}

async function start() {
  if (busy.value) return
  const composed = scheme.value + sanitizeTarget(hostPort.value)
  if (!sanitizeTarget(hostPort.value)) return
  busy.value = true
  err.value = ''
  try {
    if (editingId.value) {
      await backend.retargetTunnel(editingId.value, composed)
      msg.value = t('tunnels.retarged')
      cancelEdit()
    } else {
      await backend.createTunnel(composed, provider.value)
      msg.value = t('tunnels.started')
      scheme.value = 'http://'
      hostPort.value = ''
    }
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  } finally {
    busy.value = false
  }
}

async function stop(tn: any) {
  try {
    await backend.stopTunnel(tn.id)
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

async function clearHistory() {
  try {
    const n = await backend.clearTunnelHistory()
    msg.value = t('tunnels.history_cleared', { n })
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

/** Re-open a stopped/failed record with the same target: the old record is
 * replaced by the fresh tunnel and its new public URL. */
async function restart(tn: any) {
  err.value = ''
  try {
    await backend.retargetTunnel(tn.id, tn.target)
    msg.value = t('tunnels.retarged')
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

async function install() {
  installing.value = true
  err.value = ''
  try {
    await backend.installCloudflared()
    msg.value = t('tunnels.install_done')
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  } finally {
    installing.value = false
  }
}

async function startRemote() {
  if (!remoteAgent.value || !remoteTarget.value.trim()) return
  try {
    await backend.agentCommand(remoteAgent.value, 'start_tunnel', '', '', sanitizeTarget(remoteTarget.value))
    msg.value = t('tunnels.remote_sent')
    remoteTarget.value = ''
    setTimeout(refresh, 3000)
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

async function stopRemote(c: any) {
  const tgt = c.target || ''
  if (!tgt) return
  try {
    await backend.agentCommand(c.agent_id, 'stop_tunnel', '', '', tgt)
    msg.value = t('tunnels.remote_stop_sent')
    setTimeout(refresh, 3000)
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = text
    setTimeout(() => (copied.value = ''), 1500)
  } catch { /* clipboard unavailable */ }
}

onMounted(async () => {
  await refresh()
  timer = setInterval(refresh, 3000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="p-8 space-y-6 app-page">
    <div>
      <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('tunnels.title') }}</h1>
      <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">{{ t('tunnels.subtitle') }}</p>
    </div>

    <p v-if="err" class="text-sm text-danger">{{ err }}</p>
    <p v-if="msg" class="text-sm text-success font-bold">{{ msg }}</p>

    <!-- provider availability: cloudflared binary + system ssh -->
    <div v-if="cloudflared" class="card p-4 space-y-2">
      <div class="flex items-center gap-3 flex-wrap">
        <span class="badge text-[10px]" :class="cloudflared.installed === 'true' ? 'badge-green' : 'badge-yellow'">
          {{ cloudflared.installed === 'true' ? t('tunnels.bin_ok') : t('tunnels.bin_missing') }}
        </span>
        <button v-if="cloudflared.installed !== 'true'" class="btn btn-primary btn-sm" :disabled="installing" @click="install">
          {{ installing ? t('common.loading') : t('tunnels.install') }}
        </button>
        <span v-if="cloudflared.installed !== 'true'" class="text-xs text-surface-600 dark:text-surface-500">{{ t('tunnels.install_hint') }}</span>
      </div>
      <div v-if="cloudflared.path" class="flex items-center gap-2 text-xs flex-wrap">
        <span class="text-surface-600 dark:text-surface-500">{{ t('tunnels.storage') }}:</span>
        <code class="font-mono text-surface-700 dark:text-surface-300 break-all">{{ cloudflared.path }}</code>
        <button v-if="isNative" class="btn btn-ghost btn-sm" @click="copy(cloudflared.path)">{{ t('common.copy') }}</button>
      </div>
      <div class="flex items-center gap-3 flex-wrap border-t border-surface-100 pt-2 dark:border-white/5">
        <span class="badge text-[10px]" :class="sshOK ? 'badge-green' : 'badge-yellow'">
          {{ sshOK ? t('tunnels.ssh_ok') : t('tunnels.ssh_missing') }}
        </span>
        <span class="text-xs text-surface-600 dark:text-surface-500">{{ t('tunnels.ssh_hint') }}</span>
      </div>
    </div>

    <!-- local tunnel: create / edit -->
    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">
        {{ editingId ? t('tunnels.edit_title') : t('tunnels.create_title') }}
      </h2>
      <div class="flex gap-2 flex-wrap items-stretch">
        <select v-model="provider" class="input !w-auto text-xs" :disabled="!!editingId">
          <option value="cloudflared">{{ t('tunnels.provider_cf') }}</option>
          <option value="ssh">{{ t('tunnels.provider_ssh') }}</option>
        </select>
        <select v-model="scheme" class="input !w-auto text-xs font-mono" :disabled="false">
          <option value="http://">http://</option>
          <option value="https://">https://</option>
          <option v-if="provider === 'cloudflared'" value="tcp://">tcp://</option>
          <option v-if="provider === 'cloudflared'" value="ssh://">ssh://</option>
        </select>
        <input v-model="hostPort" class="input flex-1 min-w-48 font-mono text-xs"
               placeholder="127.0.0.1:8080  ·  10.106.106.128:9000"
               @input="absorbPasted(hostPort)" @keyup.enter="start" />
        <select v-if="meshPeers.length" class="input !w-auto text-xs" @change="fillTarget(($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).selectedIndex = 0">
          <option value="">{{ t('tunnels.peer_fill') }}</option>
          <option v-for="p in meshPeers" :key="p.ip" :value="p.ip">{{ p.hostname }} ({{ p.ip }})</option>
        </select>
        <button class="btn btn-primary btn-sm" :disabled="busy || !hostPort.trim() || !providerOK" @click="start">
          {{ busy ? t('common.loading') : editingId ? t('tunnels.save_edit') : t('tunnels.create') }}
        </button>
        <button v-if="editingId" class="btn btn-secondary btn-sm" @click="cancelEdit">{{ t('networks.cancel') }}</button>
      </div>
      <p v-if="editingId" class="text-xs mt-2 text-warning font-bold">↻ {{ t('tunnels.edit_note') }}</p>
      <p v-if="provider === 'ssh' && scheme !== 'http://' && scheme !== 'https://'" class="text-xs mt-2 text-danger font-bold">⚠ {{ t('tunnels.ssh_http_only') }}</p>
      <p v-if="!meshPeers.length" class="text-xs mt-2 text-surface-500">{{ t('tunnels.peer_none') }}</p>
      <p class="text-xs mt-2 text-warning font-bold">⚠ {{ t('tunnels.public_warn') }}</p>
      <p class="text-xs mt-1 text-surface-600 dark:text-surface-500">
        {{ provider === 'ssh' ? t('tunnels.ssh_provider_hint') : t('tunnels.proto_hint') }}
      </p>

      <div class="mt-4 overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">
              <th class="py-2 pr-4">{{ t('tunnels.col_target') }}</th>
              <th class="py-2 pr-4">{{ t('tunnels.col_url') }}</th>
              <th class="py-2 pr-4">{{ t('tunnels.col_status') }}</th>
              <th class="py-2 pr-4">{{ t('tunnels.col_uptime') }}</th>
              <th class="py-2"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="tn in activeTunnels" :key="tn.id" class="border-t border-surface-100 dark:border-white/5 align-top">
              <td class="py-2.5 pr-4 font-mono text-xs dark:text-surface-200 text-surface-800">{{ tn.target }}</td>
              <td class="py-2.5 pr-4">
                <a v-if="tn.public_url" :href="tn.public_url" target="_blank" rel="noreferrer"
                   class="font-mono text-xs text-accent-600 dark:text-accent-400 hover:underline">{{ tn.public_url }}</a>
                <span v-else class="text-xs text-surface-500">{{ tn.status === 'failed' ? tn.error || t('tunnels.url_failed') : t('tunnels.url_pending') }}</span>
                <button v-if="tn.public_url" class="btn btn-ghost btn-sm ml-2" :title="t('tunnels.copy_url')" @click="copy(tn.public_url)">
                  {{ copied === tn.public_url ? t('tunnels.copied') : t('common.copy') }}
                </button>
                <button v-if="tn.public_url" class="btn btn-ghost btn-sm" :title="t('tunnels.qr_btn')" @click="openQR(tn)">▣ QR</button>
                <p v-if="tn.status === 'failed' && tn.error && tn.public_url" class="text-xs text-danger mt-1 max-w-[20rem] break-all">{{ tn.error }}</p>
              </td>
              <td class="py-2.5 pr-4">
                <span class="badge text-[10px]" :class="tn.status === 'running' ? 'badge-green' : tn.status === 'starting' ? 'badge-yellow' : tn.status === 'failed' ? 'badge-red' : 'badge-gray'">{{ tn.status }}</span>
              </td>
              <td class="py-2.5 pr-4 text-xs text-surface-600 dark:text-surface-400 whitespace-nowrap">{{ tn.status === 'running' || tn.status === 'starting' ? uptime(tn.created) : '—' }}</td>
              <td class="py-2.5 text-right whitespace-nowrap">
                <button class="btn btn-ghost btn-sm" :title="t('tunnels.edit_btn')" @click="startEdit(tn)">{{ t('networks.edit') }}</button>
                <button class="btn btn-ghost btn-sm text-danger" @click="stop(tn)">{{ t('tunnels.stop') }}</button>
              </td>
            </tr>
            <tr v-if="!activeTunnels.length">
              <td colspan="5" class="py-6 text-center text-surface-500">{{ t('tunnels.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <!-- history: stopped/failed records, collapsed by day -->
      <div v-if="historyCount" class="mt-5 border-t border-surface-100 pt-3 dark:border-white/5">
        <div class="flex items-center gap-2 mb-2 flex-wrap">
          <h3 class="text-xs font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">
            {{ t('tunnels.history_title') }} ({{ historyCount }})
          </h3>
          <button class="btn btn-ghost btn-sm ml-auto text-danger" @click="clearHistory">{{ t('tunnels.history_clear') }}</button>
        </div>
        <div v-for="(g, gi) in historyGroups" :key="g.day" class="mb-1.5">
          <button class="w-full flex items-center gap-2 text-left text-xs py-1.5 px-2 bg-surface-100 hover:bg-surface-200 rounded dark:bg-white/5 dark:hover:bg-white/10 text-surface-700 dark:text-surface-300" @click="toggleDay(g.day)">
            <span class="font-mono">{{ g.day }}</span>
            <span class="text-surface-500">({{ g.items.length }})</span>
            <span class="ml-auto">{{ isOpen(g.day, gi) ? '▾' : '▸' }}</span>
          </button>
          <div v-if="isOpen(g.day, gi)" class="mt-1 space-y-1">
            <div v-for="hn in g.items" :key="hn.id" class="flex items-center gap-2 flex-wrap text-xs px-2 py-1 rounded border border-surface-100 dark:border-white/5">
              <span class="badge text-[10px] shrink-0" :class="hn.status === 'failed' ? 'badge-red' : 'badge-gray'">{{ hn.status }}</span>
              <span class="badge text-[10px] shrink-0 badge-gray">{{ hn.kind === 'ssh' ? 'SSH' : 'CF' }}</span>
              <code class="font-mono text-surface-700 dark:text-surface-300">{{ hn.target }}</code>
              <template v-if="hn.public_url">
                <a :href="hn.public_url" target="_blank" rel="noreferrer" class="font-mono text-accent-600 dark:text-accent-400 hover:underline truncate max-w-[14rem]">{{ hn.public_url }}</a>
                <button class="btn btn-ghost btn-sm" @click="copy(hn.public_url)">{{ copied === hn.public_url ? t('tunnels.copied') : t('common.copy') }}</button>
              </template>
              <span v-if="hn.error" class="text-danger truncate max-w-[14rem]" :title="hn.error">{{ hn.error }}</span>
              <span class="text-surface-500 ml-auto whitespace-nowrap">{{ (hn.created || '').replace('T', ' ').slice(0, 19) }}</span>
              <button class="btn btn-ghost btn-sm shrink-0" :title="t('tunnels.restart_hint')" @click="restart(hn)">{{ t('tunnels.restart') }}</button>
              <button class="btn btn-ghost btn-sm shrink-0" @click="startEdit(hn)">{{ t('networks.edit') }}</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- remote device tunnels via fleet -->
    <div v-if="isAdmin" class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">{{ t('tunnels.remote_title') }}</h2>
      <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('tunnels.remote_hint') }}</p>
      <div class="flex gap-2 flex-wrap">
        <select v-model="remoteAgent" class="input !w-auto text-xs">
          <option value="" disabled>{{ t('tunnels.select_device') }}</option>
          <option v-for="a in agents" :key="a.id" :value="a.id">{{ a.name }} ({{ a.ipv4 || a.os }})</option>
        </select>
        <input v-model="remoteTarget" class="input flex-1 min-w-56 font-mono text-xs"
               placeholder="http://127.0.0.1:3000" @keyup.enter="startRemote" />
        <select v-if="meshPeers.length" class="input !w-auto text-xs" @change="fillRemoteTarget(($event.target as HTMLSelectElement).value); ($event.target as HTMLSelectElement).selectedIndex = 0">
          <option value="">{{ t('tunnels.peer_fill') }}</option>
          <option v-for="p in meshPeers" :key="p.ip" :value="p.ip">{{ p.hostname }} ({{ p.ip }})</option>
        </select>
        <button class="btn btn-primary btn-sm" :disabled="!remoteAgent || !remoteTarget.trim()" @click="startRemote">
          {{ t('tunnels.create') }}
        </button>
      </div>
      <p v-if="!agents.length" class="text-xs mt-2 text-surface-500">{{ t('tunnels.remote_no_agents') }}</p>
      <p v-if="!meshPeers.length" class="text-xs mt-1 text-surface-500">{{ t('tunnels.peer_none') }}</p>
      <div v-if="fleetCommands.length" class="mt-4 border-t border-surface-100 pt-3 dark:border-white/5">
        <h3 class="text-xs font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400 mb-2">{{ t('tunnels.remote_recent') }}</h3>
        <div v-for="c in fleetCommands" :key="c.id" class="text-xs py-0.5 font-mono flex items-center gap-2 flex-wrap">
          <span :class="c.status === 'done' ? 'text-success' : c.status === 'failed' ? 'text-danger' : 'text-warning'">{{ c.status }}</span>
          <span class="text-surface-500">{{ c.action === 'start_tunnel' ? '▶' : '■' }} {{ c.target || c.result || '' }}</span>
          <span class="text-surface-500">{{ (c.created || '').replace('T', ' ').slice(0, 19) }}</span>
          <template v-if="c.result && c.result.startsWith('https://')">
            <a :href="c.result" target="_blank" rel="noreferrer" class="text-accent-600 dark:text-accent-400 hover:underline">{{ c.result }}</a>
            <button class="btn btn-ghost btn-sm" @click="copy(c.result)">{{ copied === c.result ? t('tunnels.copied') : t('common.copy') }}</button>
          </template>
          <template v-else-if="c.result && c.result !== 'ok'"> · {{ c.result }}</template>
          <button v-if="c.action === 'start_tunnel' && (c.status === 'done' || c.status === 'pending') && c.target"
                  class="btn btn-ghost btn-sm text-danger" @click="stopRemote(c)">{{ t('tunnels.remote_stop') }}</button>
        </div>
      </div>
    </div>

    <!-- QR modal: scan or copy the public URL -->
    <div v-if="qrURL" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" @click.self="qrURL = ''">
      <div class="card p-6 w-full max-w-xs text-center">
        <h2 class="text-sm font-bold uppercase tracking-wide mb-3 dark:text-white text-surface-900">{{ t('tunnels.qr_title') }}</h2>
        <img v-if="qrData" :src="qrData" alt="QR code" class="mx-auto rounded bg-white p-2 w-56 h-56" />
        <a :href="qrURL" target="_blank" rel="noreferrer" class="block mt-3 font-mono text-xs text-accent-600 dark:text-accent-400 hover:underline break-all">{{ qrURL }}</a>
        <div class="flex justify-center gap-2 mt-4">
          <button class="btn btn-secondary btn-sm" @click="copy(qrURL)">
            {{ copied === qrURL ? t('tunnels.copied') : t('common.copy') }}
          </button>
          <button class="btn btn-primary btn-sm" @click="qrURL = ''">{{ t('networks.cancel') }}</button>
        </div>
      </div>
    </div>
  </div>
</template>
