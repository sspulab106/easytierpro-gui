<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { backend } from '@/lib/backend'
import { useConfigs } from '@/composables/useConfigs'
import { useCore } from '@/composables/useCore'
import TopologyView from '@/components/TopologyView.vue'
import { resolveRows, buildTopology, type TopologyData } from '@/lib/topology'

interface PeerRow {
  id: string
  network: string // display name of the network this peer belongs to
  instanceId: string
  hostname: string
  ipv4: string
  cidr: string
  cost: string
  lat_ms: string
  loss_rate: string
  tunnel_proto: string
  nat_type: string
  rx: string
  tx: string
  version: string
}

const { t } = useI18n()
const { configs, refresh: refreshConfigs } = useConfigs()
const { status } = useCore()

const peers = ref<PeerRow[]>([])
const loading = ref(false)
const lastError = ref<string | null>(null)
const filter = ref('') // '' = all networks

const networkName = (instanceId: string, fallback: string) =>
  configs.value.find((c) => c.instance_id === instanceId)?.network
  || configs.value.find((c) => c.instance_id === instanceId)?.instance_name
  || fallback

const filteredPeers = computed(() =>
  filter.value ? peers.value.filter((p) => p.instanceId === filter.value) : peers.value,
)

// ---- topology card: same rendering as the dashboard, synced with the table ----
const topology = ref<TopologyData>({ localHostname: '', localIp: '', localPeerId: '', nodes: [] })
// Instance shown in the topology card; '' = follow the table filter (or first).
const topoTarget = ref('')
// Peer id selected in either the table or the topology — drives both highlights.
const selectedPeer = ref('')

// Networks that actually have peers right now (options for filter + topology).
const peerNetworks = computed(() => {
  const seen = new Map<string, string>()
  for (const p of peers.value) {
    if (!seen.has(p.instanceId)) seen.set(p.instanceId, p.network)
  }
  return Array.from(seen, ([instanceId, network]) => ({ instanceId, network }))
})

function resolveTopoTarget(): string {
  if (topoTarget.value) return topoTarget.value
  if (filter.value) return filter.value
  return peerNetworks.value[0]?.instanceId || ''
}

function selectPeer(id: string, fromTopology = false) {
  selectedPeer.value = selectedPeer.value === id ? '' : id
  if (fromTopology && selectedPeer.value) {
    document
      .querySelector(`tr[data-peer-id="${CSS.escape(selectedPeer.value)}"]`)
      ?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  }
}

// easytier-cli peer returns a grouped array when multiple instances run:
// [{ instance_id, instance_name, result: [...] }] — or a flat array for one.
function parse(raw: any): PeerRow[] {
  const rows: PeerRow[] = []
  if (!Array.isArray(raw)) return rows

  const groups: Array<{ instance_id: string; instance_name?: string; result: any }> =
    raw.length && raw[0] && typeof raw[0] === 'object' && 'result' in raw[0] ? raw : []

  if (groups.length) {
    for (const g of groups) {
      const list = Array.isArray(g.result) ? g.result : []
      for (const p of list) {
        if (p.cost === 'Local') continue // skip the local node row
        rows.push(mapRow(p, g.instance_id, networkName(g.instance_id, String(g.instance_name ?? '-'))))
      }
    }
    return rows
  }

  // Single-instance flat array (peer objects directly).
  for (const p of raw) {
    if (p.cost === 'Local') continue
    rows.push(mapRow(p, '', networkName('', '-')))
  }
  return rows
}

function mapRow(p: any, instanceId: string, network: string): PeerRow {
  return {
    id: String(p.id ?? p.peer_id ?? Math.random()),
    network,
    instanceId,
    hostname: p.hostname ?? '-',
    ipv4: p.ipv4 ?? '-',
    cidr: p.cidr ?? p.ipv4_addr ?? '-',
    cost: p.cost ?? '-',
    lat_ms: p.lat_ms ?? '-',
    loss_rate: p.loss_rate ?? '-',
    tunnel_proto: p.tunnel_proto ?? p.tunnel ?? '-',
    nat_type: p.nat_type ?? '-',
    rx: p.rx_bytes ?? '-',
    tx: p.tx_bytes ?? '-',
    version: p.version ?? '-',
  }
}

async function refreshTopology() {
  const target = resolveTopoTarget()
  if (!target) {
    topology.value = { localHostname: '', localIp: '', localPeerId: '', nodes: [] }
    return
  }
  try {
    const [rawNodes, rawPeers, rawRoutes] = await Promise.all([
      backend.nodeInfo(),
      backend.peers(),
      backend.routes().catch(() => []),
    ])
    const node = resolveRows<any>(rawNodes, target).find((n) => n.instance_id === target)?.result ?? null
    const peerArr = resolveRows<any>(rawPeers, target).find((n) => n.instance_id === target)?.result ?? []
    const routeArr = resolveRows<any>(rawRoutes, target).find((n) => n.instance_id === target)?.result ?? []
    topology.value = buildTopology(node, Array.isArray(peerArr) ? peerArr : [], Array.isArray(routeArr) ? routeArr : [])
  } catch {
    topology.value = { localHostname: '', localIp: '', localPeerId: '', nodes: [] }
  }
}

async function refresh() {
  loading.value = true
  lastError.value = null
  try {
    const coreStatus = await backend.coreStatus()
    if (coreStatus === 'running') {
      peers.value = parse(await backend.peers())
    } else {
      peers.value = []
    }
  } catch (e) {
    lastError.value = String(e)
    peers.value = []
  } finally {
    loading.value = false
  }
  await refreshTopology()
}

// One-click launch of a node service with the OS-native client.
const launchErr = ref<string | null>(null)
const servicePorts: Record<string, number> = { rdp: 3389, ssh: 22, http: 80, https: 443 }

async function launch(peer: PeerRow, proto: string) {
  launchErr.value = null
  const host = (peer.ipv4 && peer.ipv4 !== '-' && peer.ipv4 !== '') ? peer.ipv4.split('/')[0] : peer.cidr.split('/')[0]
  if (!host || host === '-') return
  try {
    await backend.launchService(host, servicePorts[proto], proto)
  } catch (e) {
    launchErr.value = String(e)
  }
}

let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  refreshConfigs()
  refresh()
  timer = setInterval(refresh, 3000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="p-8 space-y-6">
    <div class="flex items-start justify-between">
      <div>
        <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('peers.title') }}</h1>
        <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">{{ t('peers.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <select v-model="filter" class="input !w-auto">
          <option value="">{{ t('peers.all_networks') }}</option>
          <option v-for="n in peerNetworks" :key="n.instanceId" :value="n.instanceId">
            {{ n.network }}
          </option>
        </select>
        <button class="btn btn-secondary btn-sm" @click="refresh" :disabled="loading">
          {{ loading ? t('common.loading') : t('peers.refresh') }}
        </button>
      </div>
    </div>

    <div v-if="lastError" class="card px-4 py-3 text-sm text-danger border-danger/20">
      {{ lastError }}
    </div>
    <div v-if="launchErr" class="card px-4 py-3 text-sm text-danger border-danger/20">
      {{ launchErr }}
    </div>

    <!-- Topology of the selected network, linked with the table below -->
    <div v-if="status === 'running' && peerNetworks.length" class="card p-4">
      <div class="flex items-center justify-between mb-2 flex-wrap gap-2">
        <h2 class="text-sm font-medium dark:text-surface-300 text-surface-700">{{ t('dashboard.topology') }}</h2>
        <div class="flex items-center gap-2">
          <span class="text-xs text-surface-600 dark:text-surface-500">{{ t('peers.link_hint') }}</span>
          <select
            :value="resolveTopoTarget()"
            class="input !w-auto text-xs"
            :title="t('networks.current_network')"
            @change="(e: any) => { topoTarget = (e.target as HTMLSelectElement).value }"
          >
            <option v-for="n in peerNetworks" :key="n.instanceId" :value="n.instanceId">
              {{ n.network }}
            </option>
          </select>
        </div>
      </div>
      <TopologyView
        :local-hostname="topology.localHostname"
        :local-ip="topology.localIp"
        :local-peer-id="topology.localPeerId"
        :peers="topology.nodes"
        :highlight-id="selectedPeer"
        @select="(id: string) => selectPeer(id, true)"
      />
    </div>

    <div class="card overflow-hidden">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left border-b-[3px] border-[var(--ink)]">
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.network') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase whitespace-nowrap">{{ t('peers.columns.ip') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.hostname') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.route') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.proto') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.latency') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase text-right">{{ t('peers.columns.upload') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase text-right">{{ t('peers.columns.download') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.loss') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.nat') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.version') }}</th>
              <th class="px-4 py-3 text-xs font-bold uppercase">{{ t('peers.columns.services') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in filteredPeers" :key="p.id + p.instanceId"
                :data-peer-id="p.id"
                class="border-b border-white/5 last:border-0 transition-colors cursor-pointer"
                :class="selectedPeer === p.id ? 'bg-[var(--accent)]/15' : 'hover:bg-white/5'"
                @click="selectPeer(p.id)">
              <td class="px-4 py-2.5">
                <span class="badge badge-gray whitespace-nowrap">{{ p.network }}</span>
              </td>
              <td class="px-4 py-2.5 font-mono text-surface-200 whitespace-nowrap">{{ p.cidr }}</td>
              <td class="px-4 py-2.5 font-medium dark:text-white text-surface-900">{{ p.hostname }}</td>
              <td class="px-4 py-2.5">
                <span class="badge" :class="p.cost === 'Local' ? 'badge-green' : (p.cost.startsWith('p2p') ? 'badge-yellow' : 'badge-gray')">
                  {{ p.cost }}
                </span>
              </td>
              <td class="px-4 py-2.5"><span class="badge badge-gray">{{ p.tunnel_proto }}</span></td>
              <td class="px-4 py-2.5 text-surface-200 whitespace-nowrap">{{ p.lat_ms }}</td>
              <td class="px-4 py-2.5 text-surface-700 text-right whitespace-nowrap dark:text-surface-300">{{ p.tx }}</td>
              <td class="px-4 py-2.5 text-surface-700 text-right whitespace-nowrap dark:text-surface-300">{{ p.rx }}</td>
              <td class="px-4 py-2.5 text-surface-700 whitespace-nowrap dark:text-surface-300">{{ p.loss_rate }}</td>
              <td class="px-4 py-2.5 text-surface-700 whitespace-nowrap dark:text-surface-300">{{ p.nat_type }}</td>
              <td class="px-4 py-2.5 text-surface-600 font-mono text-xs whitespace-nowrap dark:text-surface-500">{{ p.version }}</td>
              <td class="px-4 py-2.5" @click.stop>
                <div class="flex gap-1">
                  <button
                    v-for="s in ['rdp', 'ssh', 'http']"
                    :key="s"
                    class="px-2 py-0.5 text-[10px] font-bold uppercase border-2 border-surface-300 dark:border-white/10 text-surface-600 dark:text-surface-300 hover:bg-[var(--accent)] hover:text-white hover:border-[var(--accent)] transition-colors"
                    :title="`${s}://${p.ipv4 && p.ipv4 !== '-' ? p.ipv4.split('/')[0] : p.cidr.split('/')[0]}`"
                    @click="launch(p, s)"
                  >
                    {{ s }}
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!loading && filteredPeers.length === 0 && !lastError" class="p-8 text-center text-surface-600 text-sm dark:text-surface-400">
        {{ t('peers.empty') }}
      </div>
    </div>
  </div>
</template>
