<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useCore } from '@/composables/useCore'
import { useConfigs } from '@/composables/useConfigs'
import { backend, events, onEvent, type Version } from '@/lib/backend'
import { resolveRows } from '@/lib/topology'
import BandwidthChart from '@/components/BandwidthChart.vue'
import type { BandSample } from '@/components/BandwidthChart.vue'
import HistoryChart from '@/components/HistoryChart.vue'
import type { HistoryPoint } from '@/components/HistoryChart.vue'

const { t } = useI18n()
const { status, refreshStatus } = useCore()
const { configs, refresh: refreshConfigs } = useConfigs()

const version = ref<Version | null>(null)

// The dashboard is a pure observation surface: one info card per running
// network, ping diagnostic, traffic history. Topology lives on the Peers
// page; start/stop management lives on the Networks page.

// ---- stats ----

// NAT type names and severity mapping (easytier-proto NatType enum).
const NAT_LABELS: Record<number, string> = {
  0: 'Unknown', 1: 'Open Internet', 2: 'No PAT', 3: 'Full Cone',
  4: 'Restricted', 5: 'Port Restricted', 6: 'Symmetric', 7: 'Sym UDP Firewall',
  8: 'Symmetric Easy Inc', 9: 'Symmetric Easy Dec',
}
const NAT_BADGE: Record<number, string> = {
  0: 'badge-gray', 1: 'badge-green', 2: 'badge-green', 3: 'badge-green',
  4: 'badge-yellow', 5: 'badge-yellow', 6: 'badge-yellow', 7: 'badge-gray',
  8: 'badge-yellow', 9: 'badge-yellow',
}
function natType(v: any): number {
  if (v === undefined || v === null || v === '') return 0
  const n = typeof v === 'number' ? v : Number(v)
  return Number.isFinite(n) && n >= 0 && n <= 9 ? n : 0
}

interface InstanceCard {
  instance_id: string
  instance_name: string
  network: string
  peer_id: string
  hostname: string
  version: string
  ipv4: string
  peersCount: number
  natUdp: string
  natTcp: string
  natBadge: string
}

const instances = ref<InstanceCard[]>([])

// ---- bandwidth history (polled from stats, deltas per 3s interval) ----
const MAX_BW_SAMPLES = 30
const bwSamples = ref<Map<string, BandSample[]>>(new Map())
let lastStats = new Map<string, { rx: number; tx: number }>()

// ---- traffic history (persisted samples aggregated by the backend) ----
const trafficHist = ref<any>(null)
let trafficTimer: ReturnType<typeof setInterval> | null = null

function fmtBytes(v: number): string {
  if (!v) return '0 B'
  if (v >= 1024 ** 3) return (v / 1024 ** 3).toFixed(2) + ' GB'
  if (v >= 1024 ** 2) return (v / 1024 ** 2).toFixed(1) + ' MB'
  if (v >= 1024) return (v / 1024).toFixed(1) + ' KB'
  return Math.round(v) + ' B'
}

async function refreshTraffic() {
  try {
    trafficHist.value = await backend.trafficHistory(7)
  } catch (e) {
    console.error('traffic history failed:', e)
  }
}

const pingHost = ref('')
const pingBusy = ref(false)
const pingResult = ref<any>(null)

async function runPing() {
  const host = pingHost.value.trim()
  if (!host || pingBusy.value) return
  pingBusy.value = true
  pingResult.value = null
  try {
    pingResult.value = await backend.pingDiagnostic(host, 4)
  } catch (e) {
    pingResult.value = { error: String(e) }
  } finally {
    pingBusy.value = false
  }
}

// parse stats metrics into per-network cumulative bytes, then derive
// per-interval bandwidth deltas for the chart.
function absorbStats(raw: any) {
  if (!Array.isArray(raw)) return
  const cur = new Map<string, { rx: number; tx: number }>()
  for (const m of raw) {
    if (!m || typeof m.value !== 'number') continue
    const net = m.labels?.network_name || ''
    if (!net) continue
    if (m.name === 'traffic_bytes_rx') {
      const v = cur.get(net) ?? { rx: 0, tx: 0 }
      v.rx = m.value
      cur.set(net, v)
    } else if (m.name === 'traffic_bytes_tx') {
      const v = cur.get(net) ?? { rx: 0, tx: 0 }
      v.tx = m.value
      cur.set(net, v)
    }
  }
  const now = Date.now()
  for (const [net, v] of cur) {
    const prev = lastStats.get(net)
    const list = bwSamples.value.get(net) ?? []
    list.push({ t: now, rx: prev ? Math.max(0, v.rx - prev.rx) / 3 : 0, tx: prev ? Math.max(0, v.tx - prev.tx) / 3 : 0 })
    if (list.length > MAX_BW_SAMPLES) list.shift()
    bwSamples.value.set(net, list)
  }
  lastStats = cur
}

// Build one info card per running network instance.
function buildInstances(
  nodeRows: Array<{ instance_id: string; instance_name?: string; result: any }>,
  peerRows: Array<{ instance_id: string; result: any }>,
) {
  const cards: InstanceCard[] = []
  for (const n of nodeRows) {
    const node = n.result
    if (!node) continue
    const cfg = configs.value.find((c) => c.instance_id === n.instance_id)
    const p = peerRows.find((x) => x.instance_id === n.instance_id)?.result ?? []
    const peerArr = Array.isArray(p) ? p : []
    const stun = node.stun_info || {}
    const udp = natType(stun.udp_nat_type)
    cards.push({
      instance_id: n.instance_id,
      instance_name: String(node.instance_name ?? n.instance_name ?? cfg?.instance_name ?? n.instance_id),
      network: cfg?.network || String(node.network_name ?? ''),
      peer_id: String(node.peer_id ?? '-'),
      hostname: node.hostname || '-',
      version: node.version || '-',
      ipv4: node.ipv4_addr || 'DHCP',
      peersCount: peerArr.filter((x: any) => x.cost !== 'Local').length,
      natUdp: NAT_LABELS[udp] || 'Unknown',
      natTcp: NAT_LABELS[natType(stun.tcp_nat_type)] || 'Unknown',
      natBadge: NAT_BADGE[udp] || 'badge-gray',
    })
  }
  instances.value = cards
}

async function refresh() {
  await refreshStatus()
  try {
    if (status.value === 'running') {
      const rawNodes = await backend.nodeInfo()
      const rawPeers = await backend.peers()

      const nodes = resolveRows(rawNodes, '')
      const pRows = resolveRows(rawPeers, '')
      buildInstances(nodes, pRows)

      // bandwidth history
      try {
        absorbStats(await backend.stats())
      } catch { /* stats unavailable */ }
    } else {
      instances.value = []
    }
  } catch (e) {
    instances.value = []
  }
}

let logOff: (() => void) | null = null
let statsTimer: ReturnType<typeof setInterval> | null = null
onMounted(async () => {
  try {
    version.value = await backend.versions()
  } catch (e) {
    console.error('dashboard init failed:', e)
  }
  await refreshConfigs()
  await refresh()
  refreshTraffic()
  logOff = onEvent(events.coreStatus, refresh)
  statsTimer = setInterval(refresh, 3000)
  trafficTimer = setInterval(refreshTraffic, 60000)
})
onUnmounted(() => {
  logOff?.()
  if (statsTimer) clearInterval(statsTimer)
  if (trafficTimer) clearInterval(trafficTimer)
})
</script>

<template>
  <div class="p-8 space-y-6">
    <div class="flex items-start justify-between flex-wrap gap-2">
      <div>
        <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('dashboard.title') }}</h1>
        <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">
          {{ t('core.version') }} {{ version?.core || '…' }}
        </p>
      </div>
    </div>

    <div v-if="instances.length" class="space-y-3">
      <div class="flex items-center justify-between">
        <h2 class="text-sm font-medium dark:text-surface-300 text-surface-700">{{ t('dashboard.instances') }}</h2>
      </div>
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div
          v-for="inst in instances"
          :key="inst.instance_id"
          class="card p-4"
        >
          <div class="flex items-center justify-between mb-3 gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span class="font-bold text-sm dark:text-white text-surface-900 truncate">
                {{ inst.network || inst.instance_name || inst.instance_id }}
              </span>
              <span class="badge badge-green text-[10px]">{{ t('networks.running') }}</span>
            </div>
            <span class="text-xs text-surface-600 dark:text-surface-500 shrink-0">{{ inst.peersCount }} {{ t('dashboard.instance_peers') }}</span>
          </div>
          <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-sm">
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">{{ t('dashboard.peer_id') }}</div>
              <div class="font-mono text-xs dark:text-surface-200 text-surface-800 truncate">{{ inst.peer_id }}</div>
            </div>
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">{{ t('dashboard.hostname') }}</div>
              <div class="font-mono text-xs dark:text-surface-200 text-surface-800 truncate">{{ inst.hostname }}</div>
            </div>
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">{{ t('dashboard.version') }}</div>
              <div class="font-mono text-xs dark:text-surface-200 text-surface-800 truncate">{{ inst.version }}</div>
            </div>
            <div>
              <div class="text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">{{ t('dashboard.ipv4') }}</div>
              <div class="font-mono text-xs dark:text-surface-200 text-surface-800 truncate">{{ inst.ipv4 }}</div>
            </div>
          </div>
          <div class="flex items-center gap-2 mt-2">
            <span class="badge text-[10px]" :class="inst.natBadge" :title="'UDP ' + inst.natUdp">
              NAT: {{ inst.natUdp }}
            </span>
            <span class="text-[10px] text-surface-500 dark:text-surface-500">TCP {{ inst.natTcp }}</span>
          </div>
          <div class="mt-2 border-t border-surface-100 pt-2 dark:border-white/5">
            <BandwidthChart v-if="(bwSamples.get(inst.network)?.length ?? 0) > 1" :samples="bwSamples.get(inst.network) ?? []" :height="56" />
          </div>
        </div>
      </div>
    </div>

    <div v-else-if="status !== 'running'" class="card p-8 text-center text-surface-600 dark:text-surface-400">
      <p class="text-4xl mb-3">⇪</p>
      <p class="text-sm">{{ t('core.not_running') }}</p>
    </div>

    <div v-else class="card p-8 text-center text-surface-600 dark:text-surface-400">
      <p class="text-sm">{{ t('dashboard.instance_no_networks') }}</p>
    </div>

    <!-- Ping / Jitter diagnostic -->
    <div class="card p-4">
      <div class="flex items-center justify-between mb-2 flex-wrap gap-2">
        <h2 class="text-sm font-medium dark:text-surface-300 text-surface-700">{{ t('dashboard.ping_title') }}</h2>
        <div class="flex items-center gap-2">
          <input
            v-model="pingHost"
            class="input !w-56 font-mono text-xs"
            :placeholder="t('dashboard.ping_placeholder')"
            @keyup.enter="runPing"
          />
          <button class="btn btn-primary btn-sm" :disabled="pingBusy || !pingHost.trim()" @click="runPing">
            {{ pingBusy ? t('common.loading') : t('dashboard.ping_run') }}
          </button>
        </div>
      </div>
      <div v-if="pingResult" class="text-sm font-mono">
        <template v-if="pingResult.error">
          <span class="text-danger">{{ pingResult.error }}</span>
        </template>
        <template v-else>
          <span class="text-surface-700 dark:text-surface-300">{{ pingResult.host }} — </span>
          <span class="text-surface-700 dark:text-surface-300">{{ t('dashboard.ping_loss') }}: {{ pingResult.loss_percent }}%</span>
          <span class="text-surface-600 dark:text-surface-500"> · </span>
          <span class="text-surface-700 dark:text-surface-300">{{ t('dashboard.ping_rtt') }}: {{ pingResult.min_ms }}/{{ pingResult.avg_ms }}/{{ pingResult.max_ms }} ms</span>
          <span class="text-surface-600 dark:text-surface-500"> · </span>
          <span class="text-surface-700 dark:text-surface-300">{{ t('dashboard.ping_jitter') }}: {{ pingResult.jitter_ms.toFixed(2) }} ms</span>
        </template>
      </div>
    </div>

    <!-- Traffic history (persisted, sampled by the backend) -->
    <div class="card p-4">
      <div class="flex items-center justify-between mb-2 flex-wrap gap-2">
        <h2 class="text-sm font-medium dark:text-surface-300 text-surface-700">{{ t('dashboard.traffic_title') }}</h2>
        <div class="flex items-center gap-4 text-xs text-surface-600 dark:text-surface-500">
          <span>{{ t('dashboard.traffic_today') }}:
            <span class="font-mono text-surface-700 dark:text-surface-300">↓ {{ fmtBytes(trafficHist?.today_rx ?? 0) }}</span>
            <span class="font-mono text-surface-700 dark:text-surface-300">↑ {{ fmtBytes(trafficHist?.today_tx ?? 0) }}</span>
          </span>
          <span>{{ t('dashboard.traffic_week') }}:
            <span class="font-mono text-surface-700 dark:text-surface-300">↓ {{ fmtBytes(trafficHist?.week_rx ?? 0) }}</span>
            <span class="font-mono text-surface-700 dark:text-surface-300">↑ {{ fmtBytes(trafficHist?.week_tx ?? 0) }}</span>
          </span>
        </div>
      </div>
      <HistoryChart
        v-if="trafficHist && (trafficHist.hours_24 ?? []).some((p: HistoryPoint) => p.rx > 0 || p.tx > 0)"
        :points="trafficHist.hours_24"
        :height="110"
      />
      <p v-else class="text-xs text-surface-600 dark:text-surface-500">{{ t('dashboard.traffic_empty') }}</p>
    </div>
  </div>
</template>
