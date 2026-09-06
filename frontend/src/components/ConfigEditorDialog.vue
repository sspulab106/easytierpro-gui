<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { backend } from '@/lib/backend'
import type { ConfigFile } from '@/lib/backend'

// show/hide toggles for secret inputs (network password, WireGuard key)
const showSecret = ref(false)
const showVpnKey = ref(false)
import { emptyForm, formFromToml, formToToml, freeListenerPort, freeListeners, listenerPorts, type NetworkForm, type VpnPortalClient } from '@/lib/network'

const { t } = useI18n()

const props = defineProps<{
  visible: boolean
  config: ConfigFile | null
  existing: ConfigFile[]
}>()
const emit = defineEmits<{
  (e: 'update:visible', v: boolean): void
  (e: 'save', cfg: ConfigFile): void
}>()

const form = ref<NetworkForm>(emptyForm())
const rawToml = ref('')
const useRaw = ref(false)

// list-entry inputs
const listenerInput = ref('')
const mappedListenerInput = ref('')
const peerInput = ref('')

// VPN portal client editor rows
const portalClientRows = ref<VpnPortalClient[]>([])

watch(
  () => [props.visible, props.config],
  () => {
    if (!props.visible) return
    const existing = props.config
    if (existing) {
      form.value = formFromToml(existing.raw)
      rawToml.value = existing.raw
    } else {
      const port = freeListenerPort(props.existing.map((c) => c.raw))
      form.value = emptyForm(freeListeners(port))
      rawToml.value = formToToml(form.value)
    }
    portalClientRows.value = form.value.vpn_portal_clients.map((c) => ({ ...c }))
    useRaw.value = false
  },
)

const generatedToml = computed(() => {
  if (useRaw.value) return rawToml.value
  const f = { ...form.value }
  f.vpn_portal_clients = portalClientRows.value
    .filter((c) => c.name.trim())
    .map((c) => ({ name: c.name.trim(), virtual_ip: c.virtual_ip.trim(), groups: c.groups }))
  return formToToml(f, props.config?.instance_id || '')
})

function addItem(list: string[], value: string) {
  const v = value.trim()
  if (v && !list.includes(v)) list.push(v)
}
function removeItem(list: string[], idx: number) {
  list.splice(idx, 1)
}

function textFromList(list: string[]): string {
  return list.join('\n')
}
function setTextList(list: string[], text: string) {
  list.length = 0
  list.push(...text.split('\n').map((x) => x.trim()).filter((x) => x))
}

function addPortalClient() {
  portalClientRows.value.push({ name: '', virtual_ip: '', groups: [] })
}
function removePortalClient(idx: number) {
  portalClientRows.value.splice(idx, 1)
}

// Quick-add the official public relay node.
function addRelay() {
  addItem(form.value.peers, 'wss://ez.cloud.c01.kr')
}

// Listener-port conflicts with other enabled configs (core binds 0.0.0.0:<port>).
const conflictingPorts = computed(() => {
  const mine = new Set<number>()
  for (const p of listenerPorts(generatedToml.value)) mine.add(p)
  const conflicts = new Set<number>()
  for (const other of props.existing) {
    if (other.instance_id === props.config?.instance_id) continue
    if (!other.enabled) continue
    for (const p of listenerPorts(other.raw)) {
      if (mine.has(p)) conflicts.add(p)
    }
  }
  return [...conflicts]
})

// Subnet-proxy CIDRs that overlap a local physical interface (would break
// routing) — checked asynchronously against the backend.
const subnetConflicts = ref<string[]>([])
let conflictTimer: ReturnType<typeof setTimeout> | null = null
watch(
  () => form.value.proxy_networks.join('\n'),
  () => {
    if (conflictTimer) clearTimeout(conflictTimer)
    conflictTimer = setTimeout(async () => {
      subnetConflicts.value = []
      for (const cidr of form.value.proxy_networks) {
        try {
          if (await backend.checkSubnetConflict(cidr)) subnetConflicts.value.push(cidr)
        } catch { /* backend unavailable */ }
      }
    }, 400)
  },
)


// ---- virtual IP subnet hint ----
// Classifies the entered address against RFC1918/CGNAT ranges, shows the
// subnet capacity, and warns about overlaps with other configured networks.
// EasyTier itself accepts /8–/32 (verified live: /12 and /8 both work).

interface SubnetInfo {
  valid: boolean
  ip: string
  prefix: number
  network: string
  broadcast: string
  usable: string
  range: string
  rangeOk: boolean
  overlaps: string[]
}

function ipv4ToInt(ip: string): number | null {
  const parts = ip.split('.').map((x) => parseInt(x, 10))
  if (parts.length !== 4 || parts.some((x) => isNaN(x) || x < 0 || x > 255)) return null
  return ((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0
}

function intToIpv4(n: number): string {
  return [(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255].join('.')
}

function parseSubnet(ip: string, prefix: number): SubnetInfo | null {
  const ipInt = ipv4ToInt(ip.trim())
  if (ipInt === null || prefix < 8 || prefix > 32) return null
  const mask = prefix === 0 ? 0 : (0xffffffff << (32 - prefix)) >>> 0
  const net = (ipInt & mask) >>> 0
  const size = Math.pow(2, 32 - prefix)
  const usable = prefix >= 31 ? size : size - 2
  // RFC1918 + CGNAT + loopback classification
  const inRange = (cidr: [string, number]) => {
    const base = ipv4ToInt(cidr[0])
    if (base === null) return false
    const m = (0xffffffff << (32 - cidr[1])) >>> 0
    return (ipInt & m) === (base & m)
  }
  let range = ''
  let rangeOk = true
  if (inRange(['10.0.0.0', 8])) range = '10.0.0.0/8 (RFC1918 私网)'
  else if (inRange(['172.16.0.0', 12])) range = '172.16.0.0/12 (RFC1918 私网)'
  else if (inRange(['192.168.0.0', 16])) range = '192.168.0.0/16 (RFC1918 私网)'
  else if (inRange(['100.64.0.0', 10])) range = '100.64.0.0/10 (CGNAT)'
  else if (inRange(['127.0.0.0', 8])) { range = '127.0.0.0/8 (回环，不可用)'; rangeOk = false }
  else if (inRange(['169.254.0.0', 16])) { range = '169.254.0.0/16 (链路本地，不可用)'; rangeOk = false }
  else { range = '公网地址段'; rangeOk = false }

  // overlaps with other configured networks
  const overlaps: string[] = []
  for (const e of props.existing) {
    if (props.config && e.instance_id === props.config.instance_id) continue
    const m = e.raw.match(/\bipv4\s*=\s*"(\d+\.\d+\.\d+\.\d+)\/(\d+)"/)
    if (!m) continue
    const eInt = ipv4ToInt(m[1])
    const eMask = (0xffffffff << (32 - parseInt(m[2], 10))) >>> 0
    if (eInt === null) continue
    if ((ipInt & eMask) === (eInt & eMask) || (net & eMask) === ((eInt & eMask) >>> 0) || (eInt & mask) === net) {
      overlaps.push((e.network || e.instance_name || e.instance_id) + ' (' + m[1] + '/' + m[2] + ')')
    }
  }
  return {
    valid: true, ip: ip.trim(), prefix,
    network: intToIpv4(net), broadcast: intToIpv4(net + size - 1),
    usable: usable.toLocaleString(),
    range, rangeOk, overlaps,
  }
}

const ipHint = computed<SubnetInfo | null>(() => {
  if (form.value.dhcp || !form.value.ipv4.trim()) return null
  return parseSubnet(form.value.ipv4, Number(form.value.network_length) || 24)
})

// MTU path probing against a remote peer of this network (binary-searched
// DF pings on the backend). Prefills the first connected peer's virtual IP.
const mtuProbeTarget = ref('')
const mtuProbeBusy = ref(false)
const mtuProbeMsg = ref('')

watch(
  () => props.visible,
  async (visible) => {
    if (!visible || mtuProbeTarget.value) return
    try {
      const groups: any[] = await backend.peers()
      // Flatten single-network arrays and per-instance groups alike, keeping
      // the network context of each row for matching against this config.
      const rows: Array<{ ipv4?: string; cost?: string; network: string }> = []
      for (const g of Array.isArray(groups) ? groups : []) {
        const grouped = g && typeof g === 'object' && 'result' in g
        const list: any[] = grouped ? (Array.isArray(g.result) ? g.result : []) : [g]
        const gname = String(g?.instance_name ?? g?.instance_id ?? '')
        for (const p of list) {
          if (!p) continue
          rows.push({ ...p, network: String(p.network_name ?? gname) })
        }
      }
      const net = form.value.network_name
      const remote = rows.filter((p) => p.ipv4 && p.cost !== 'Local')
      const candidate = remote.find((p) => !net || p.network === net) ?? remote[0]
      if (candidate) mtuProbeTarget.value = String(candidate.ipv4)
    } catch { /* core not running; user can type a target manually */ }
  },
)

async function runMtuProbe() {
  if (mtuProbeBusy.value) return
  const target = mtuProbeTarget.value.trim()
  if (!target) return
  mtuProbeBusy.value = true
  mtuProbeMsg.value = t('networks.mtu_probe_running')
  try {
    const mtu = await backend.probeMTU(target)
    form.value.mtu = String(mtu)
    mtuProbeMsg.value = t('networks.mtu_probe_ok', { n: mtu })
  } catch (e) {
    mtuProbeMsg.value = t('networks.mtu_probe_fail', { err: String(e).replace(/^Error: /, '') })
  } finally {
    mtuProbeBusy.value = false
  }
}

// Input validation: mtu and recv-limit must be plain integers (the core parses
// them as numbers; free-form values like "10mb" would corrupt the TOML).
const mtuValid = computed(() => {
  const v = form.value.mtu.trim()
  return v === '' || /^\d+$/.test(v)
})
const recvValid = computed(() => {
  const v = form.value.instance_recv_bps_limit.trim()
  return v === '' || /^\d+$/.test(v)
})

function switchMode(raw: boolean) {
  if (raw === useRaw.value) return
  if (raw) {
    rawToml.value = generatedToml.value
  } else {
    form.value = formFromToml(rawToml.value)
    portalClientRows.value = form.value.vpn_portal_clients.map((c) => ({ ...c }))
  }
  useRaw.value = raw
}

function isValidUuid(s: string): boolean {
  return /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/.test(s)
}

function onSave() {
  let raw = generatedToml.value
  let instanceId = props.config?.instance_id ?? ''
  if (!instanceId) {
    const m = raw.match(/instance_id\s*=\s*["']([^"']+)["']/)
    if (m) instanceId = m[1]
  }
  // easytier-core requires instance_id to be a valid UUID; regenerate if not.
  if (!isValidUuid(instanceId)) {
    instanceId = (crypto.randomUUID?.() ?? String(Date.now()) + Math.random().toString(16).slice(2)) as string
    // replace or inject a valid instance_id into the raw TOML
    if (raw.includes('instance_id')) {
      raw = raw.replace(/instance_id\s*=\s*["'][^"']*["']/, `instance_id = ${JSON.stringify(instanceId)}`)
    } else {
      raw = `instance_id = ${JSON.stringify(instanceId)}\n` + raw
    }
  }
  const cfg: ConfigFile = {
    instance_id: instanceId,
    instance_name: form.value.instance_name,
    network: form.value.network_name,
    ipv4: form.value.dhcp ? '' : form.value.ipv4,
    enabled: props.config?.enabled ?? true,
    path: props.config?.path ?? '',
    raw,
  }
  emit('save', cfg)
}
</script>

<template>
  <div v-if="visible" class="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-6" @click.self="emit('update:visible', false)">
    <div class="card w-full max-w-4xl max-h-[92vh] overflow-y-auto dark:bg-surface-900 bg-white dark:border-white/10 border-surface-200 p-6">
      <h2 class="text-base font-medium dark:text-white text-surface-900 mb-4">
        {{ config ? `${t('networks.edit_title')} · ${config.network || config.instance_id}` : t('networks.new_title') }}
      </h2>

      <!-- Mode switch -->
      <div class="flex items-center gap-2 mb-5">
        <button class="btn btn-sm" :class="!useRaw ? 'btn-primary' : 'btn-secondary'" @click="switchMode(false)">{{ t('networks.form') }}</button>
        <button class="btn btn-sm" :class="useRaw ? 'btn-primary' : 'btn-secondary'" @click="switchMode(true)">{{ t('networks.toml') }}</button>
      </div>

      <!-- ====== FORM MODE ====== -->
      <div v-if="!useRaw" class="space-y-6">
        <!-- Basic -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.basic') }}</h3>
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label class="label">{{ t('networks.instance_name') }}</label>
              <input v-model="form.instance_name" class="input" placeholder="my-node" />
            </div>
            <div>
              <label class="label">{{ t('networks.network_name') }}</label>
              <input v-model="form.network_name" class="input" placeholder="my-network" />
            </div>
            <div>
              <label class="label">{{ t('networks.hostname') }}</label>
              <input v-model="form.hostname" class="input" :placeholder="t('networks.hostname_hint')" />
            </div>
            <div>
              <label class="label">{{ t('networks.network_secret') }}</label>
              <div class="relative">
                <input v-model="form.network_secret" :type="showSecret ? 'text' : 'password'" class="input pr-10" :placeholder="t('networks.secret_hint')" />
                <button type="button" class="absolute right-2 top-1/2 -translate-y-1/2 text-surface-500 hover:text-accent-500"
                        :title="showSecret ? t('networks.hide_secret') : t('networks.show_secret')"
                        @click="showSecret = !showSecret">{{ showSecret ? '🙈' : '👁' }}</button>
              </div>
            </div>
          </div>
        </section>

        <!-- Addressing -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.addressing') }}</h3>
          <div class="grid grid-cols-2 gap-4">
            <!-- Official-GUI style: DHCP toggle lives ON the same row as the
                 static IP input; checking it just greys the input out. -->
            <div class="col-span-2">
              <label class="text-xs font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">{{ t('networks.ipv4') }}</label>
              <div class="flex items-center gap-3 mt-1">
                <input v-model="form.ipv4" class="input flex-1" :disabled="form.dhcp"
                       :class="form.dhcp ? 'opacity-40' : ''" placeholder="10.144.144.1" />
                <span class="text-surface-600 dark:text-surface-500">/</span>
                <input v-model.number="form.network_length" type="number" min="8" max="32" class="input w-20" :disabled="form.dhcp"
                       :class="form.dhcp ? 'opacity-40' : ''" />
                <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300 shrink-0 whitespace-nowrap">
                  <input v-model="form.dhcp" type="checkbox" class="accent-accent-500" />
                  {{ t('networks.dhcp') }}
                </label>
              </div>
              <div v-if="ipHint" class="mt-1.5 ml-1 space-y-0.5 text-xs">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="badge text-[10px]" :class="ipHint.rangeOk ? 'badge-green' : 'badge-yellow'">{{ ipHint.range }}</span>
                  <span class="text-surface-600 dark:text-surface-500">{{ t('networks.subnet_net') }} <code class="font-mono">{{ ipHint.network }}/{{ ipHint.prefix }}</code></span>
                  <span class="text-surface-600 dark:text-surface-500">{{ t('networks.subnet_broadcast') }} <code class="font-mono">{{ ipHint.broadcast }}</code></span>
                  <span class="text-surface-600 dark:text-surface-500">{{ t('networks.subnet_hosts') }} <code class="font-mono">{{ ipHint.usable }}</code></span>
                </div>
                <p class="text-surface-500 dark:text-surface-500">{{ t('networks.subnet_sizes') }}</p>
                <p v-if="ipHint.overlaps.length" class="text-warning font-bold">⚠ {{ t('networks.subnet_overlap') }} {{ ipHint.overlaps.join(', ') }}</p>
              </div>
            </div>
            <label class="flex items-center gap-2 text-sm text-surface-700 col-span-2 dark:text-surface-300">
              <input v-model="form.ipv6_public_addr_auto" type="checkbox" class="accent-accent-500" />
              {{ t('networks.auto_ipv6') }}
            </label>
            <label v-if="form.ipv6_public_addr_auto" class="flex items-center gap-2 text-sm text-surface-700 col-span-2 dark:text-surface-300">
              <input v-model="form.ipv6_public_addr_provider" type="checkbox" class="accent-accent-500" />
              {{ t('networks.ipv6_provider') }}
            </label>
            <div v-if="form.ipv6_public_addr_auto && form.ipv6_public_addr_provider" class="col-span-2">
              <label class="text-xs text-surface-600 dark:text-surface-400">{{ t('networks.ipv6_prefix') }}</label>
              <input v-model="form.ipv6_public_addr_prefix" class="input font-mono text-xs mt-1" placeholder="2001:db8::/64（留空自动检测本机公网前缀）" />
            </div>
            <p v-if="form.ipv6_public_addr_auto" class="col-span-2 text-xs text-surface-600 dark:text-surface-500">
              {{ form.ipv6_public_addr_provider ? t('networks.ipv6_provider_hint') : t('networks.ipv6_auto_hint') }}
            </p>
          </div>
        </section>

        <!-- Listeners -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.listeners') }}</h3>
          <div v-if="conflictingPorts.length" class="text-xs text-amber-600 dark:text-amber-400 mb-2">
            ⚠ {{ t('networks.listener_conflict') }}: {{ conflictingPorts.join(', ') }}
          </div>
          <div v-for="(l, i) in form.listeners" :key="i" class="flex gap-2 mb-2">
            <input v-model="form.listeners[i]" class="input font-mono text-xs" />
            <button class="btn btn-ghost btn-sm" @click="removeItem(form.listeners, i)">×</button>
          </div>
          <div class="flex gap-2">
            <input v-model="listenerInput" class="input font-mono text-xs flex-1" placeholder="tcp://0.0.0.0:11010" />
            <button class="btn btn-secondary btn-sm" @click="addItem(form.listeners, listenerInput); listenerInput = ''">{{ t('networks.add_listener') }}</button>
          </div>
        </section>

        <!-- Peers -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.peers_label') }}</h3>
          <div v-for="(p, i) in form.peers" :key="i" class="flex gap-2 mb-2">
            <input v-model="form.peers[i]" class="input font-mono text-xs" />
            <button class="btn btn-ghost btn-sm" @click="removeItem(form.peers, i)">×</button>
          </div>
          <div class="flex gap-2">
            <input v-model="peerInput" class="input font-mono text-xs flex-1" placeholder="tcp://peer.example.com:11010" />
            <button class="btn btn-secondary btn-sm" @click="addItem(form.peers, peerInput); peerInput = ''">{{ t('networks.add_peer') }}</button>
            <button class="btn btn-primary btn-sm" @click="addRelay">{{ t('networks.relay_quick') }}</button>
          </div>
          <p class="text-xs text-surface-600 mt-1 dark:text-surface-500">{{ t('networks.relay_hint') }} <code class="text-surface-700 dark:text-surface-300">wss://ez.cloud.c01.kr</code></p>
        </section>

        <!-- Mapped listeners -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.mapped_listeners') }}</h3>
          <div v-for="(l, i) in form.mapped_listeners" :key="i" class="flex gap-2 mb-2">
            <input v-model="form.mapped_listeners[i]" class="input font-mono text-xs" />
            <button class="btn btn-ghost btn-sm" @click="removeItem(form.mapped_listeners, i)">×</button>
          </div>
          <div class="flex gap-2">
            <input v-model="mappedListenerInput" class="input font-mono text-xs flex-1" placeholder="tcp://123.123.123.123:11223" />
            <button class="btn btn-secondary btn-sm" @click="addItem(form.mapped_listeners, mappedListenerInput); mappedListenerInput = ''">{{ t('networks.add_mapped') }}</button>
          </div>
        </section>

        <!-- Subnet proxy & routing -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.proxy_routing') }}</h3>
          <div class="grid grid-cols-2 gap-4">
            <div>
              <label class="label">{{ t('networks.proxy_cidr') }}</label>
              <textarea :value="textFromList(form.proxy_networks)" @input="setTextList(form.proxy_networks, ($event.target as HTMLTextAreaElement).value)" class="input font-mono text-xs" rows="3" placeholder="10.100.1.0/24" />
              <p v-if="subnetConflicts.length" class="text-xs text-amber-600 dark:text-amber-400 mt-1">
                ⚠ {{ t('networks.subnet_conflict', { cidrs: subnetConflicts.join(', ') }) }}
              </p>
            </div>
            <div>
              <label class="label">{{ t('networks.routes') }}</label>
              <textarea :value="textFromList(form.routes)" @input="setTextList(form.routes, ($event.target as HTMLTextAreaElement).value)" class="input font-mono text-xs" rows="3" placeholder="192.168.0.0/16" />
            </div>
            <div>
              <label class="label">{{ t('networks.exit_nodes') }}</label>
              <textarea :value="textFromList(form.exit_nodes)" @input="setTextList(form.exit_nodes, ($event.target as HTMLTextAreaElement).value)" class="input font-mono text-xs" rows="3" placeholder="192.168.8.8" />
            </div>
            <div>
              <label class="label">{{ t('networks.whitelist') }}</label>
              <textarea :value="textFromList(form.relay_network_whitelist)" @input="setTextList(form.relay_network_whitelist, ($event.target as HTMLTextAreaElement).value)" class="input font-mono text-xs" rows="3" placeholder="net1 net2 *" />
            </div>
            <div>
              <label class="label">{{ t('networks.socks5') }}</label>
              <input v-model="form.socks5_port" class="input" placeholder="1080" />
            </div>
          </div>
        </section>

        <!-- VPN Portal -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.vpn_portal') }}</h3>
          <div class="grid grid-cols-2 gap-4 mb-3">
            <div>
              <label class="label">{{ t('networks.vpn_listen') }}</label>
              <input v-model="form.vpn_portal_listen" class="input font-mono text-xs" placeholder="0.0.0.0:11015" />
            </div>
            <div>
              <label class="label">{{ t('networks.vpn_client_cidr') }}</label>
              <input v-model="form.vpn_portal_client_cidr" class="input font-mono text-xs" placeholder="10.14.14.0/24" />
            </div>
            <div>
              <label class="label">{{ t('networks.vpn_private_key') }}</label>
              <div class="relative">
                <input v-model="form.vpn_portal_private_key" :type="showVpnKey ? 'text' : 'password'" class="input font-mono text-xs pr-10" />
                <button type="button" class="absolute right-2 top-1/2 -translate-y-1/2 text-surface-500 hover:text-accent-500"
                        :title="showVpnKey ? t('networks.hide_secret') : t('networks.show_secret')"
                        @click="showVpnKey = !showVpnKey">{{ showVpnKey ? '🙈' : '👁' }}</button>
              </div>
            </div>
          </div>
          <div v-for="(c, i) in portalClientRows" :key="i" class="flex gap-2 items-center mb-2">
            <input v-model="c.name" class="input text-xs flex-1" :placeholder="t('networks.vpn_client_name')" />
            <input v-model="c.virtual_ip" class="input font-mono text-xs w-36" placeholder="10.144.144.3" />
            <input v-model="c.groups" class="input text-xs w-36" placeholder="groups" @input="(e: any) => c.groups = (e.target as HTMLInputElement).value.split(',').map((x: string) => x.trim()).filter(Boolean)" />
            <button class="btn btn-ghost btn-sm" @click="removePortalClient(i)">×</button>
          </div>
          <button class="btn btn-secondary btn-sm" @click="addPortalClient">{{ t('networks.vpn_add_client') }}</button>
        </section>

        <!-- TUN & performance -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.tun_perf') }}</h3>
          <div class="grid grid-cols-3 gap-4">
            <div>
              <label class="label">{{ t('networks.tun_name') }}</label>
              <input v-model="form.dev_name" class="input" placeholder="et_11_hwy4" />
            </div>
            <div>
              <label class="label">{{ t('networks.mtu') }}</label>
              <div class="flex gap-2">
                <input v-model="form.mtu" class="input flex-1 min-w-0" :class="mtuValid ? '' : 'border-danger'" placeholder="1380" />
                <button
                  type="button"
                  class="btn btn-secondary btn-sm shrink-0"
                  :disabled="mtuProbeBusy || !mtuProbeTarget.trim()"
                  :title="mtuProbeTarget"
                  @click="runMtuProbe"
                >
                  {{ mtuProbeBusy ? t('networks.mtu_probe_running') : t('networks.mtu_probe') }}
                </button>
              </div>
              <input
                v-model="mtuProbeTarget"
                class="input mt-1 font-mono text-xs py-1"
                :placeholder="t('networks.mtu_probe_target')"
              />
              <p v-if="mtuProbeMsg && !mtuProbeBusy" class="text-xs mt-1 text-surface-600 dark:text-surface-500">{{ mtuProbeMsg }}</p>
              <p v-if="!mtuValid" class="text-xs text-danger mt-1">{{ t('networks.mtu_invalid') }}</p>
            </div>
            <div>
              <label class="label">{{ t('networks.recv_limit') }}</label>
              <input v-model="form.instance_recv_bps_limit" class="input" :class="recvValid ? '' : 'border-danger'" :placeholder="t('networks.recv_hint')" />
              <p v-if="!recvValid" class="text-xs text-danger mt-1">{{ t('networks.recv_invalid') }}</p>
            </div>
          </div>
        </section>

        <!-- Port forwarding: local port -> virtual-network remote port -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.pf_title') }}</h3>
          <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('networks.pf_hint') }}</p>
          <div v-for="(pf, i) in form.port_forwards" :key="i" class="flex gap-2 mb-2 items-center flex-wrap">
            <select v-model="pf.proto" class="input !w-auto text-xs font-mono">
              <option value="tcp">tcp</option>
              <option value="udp">udp</option>
            </select>
            <input v-model="pf.bind_addr" class="input flex-1 min-w-44 font-mono text-xs" placeholder="0.0.0.0:13001" />
            <span class="text-surface-500">→</span>
            <input v-model="pf.dst_addr" class="input flex-1 min-w-44 font-mono text-xs" placeholder="10.126.126.1:80" />
            <button class="btn btn-ghost btn-sm text-danger" @click="form.port_forwards.splice(i, 1)">×</button>
          </div>
          <button class="btn btn-secondary btn-sm" @click="form.port_forwards.push({ proto: 'tcp', bind_addr: '', dst_addr: '' })">
            + {{ t('networks.pf_add') }}
          </button>
        </section>

        <!-- Access control: ACL chains (Inbound / Outbound / Forward) -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.acl_title') }}</h3>
          <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('networks.acl_hint') }}</p>
          <div v-for="(ch, ci) in form.acl_chains" :key="ci" class="border border-surface-200 rounded-lg p-3 mb-3 dark:border-white/10">
            <div class="flex gap-2 items-center flex-wrap mb-2">
              <select v-model="ch.chain_type" class="input !w-auto text-xs">
                <option value="Inbound">{{ t('networks.acl_inbound') }}</option>
                <option value="Outbound">{{ t('networks.acl_outbound') }}</option>
                <option value="Forward">{{ t('networks.acl_forward') }}</option>
              </select>
              <input v-model="ch.name" class="input !w-40 text-xs" :placeholder="t('networks.acl_chain_name')" />
              <select v-model="ch.default_action" class="input !w-auto text-xs">
                <option value="Allow">{{ t('networks.acl_allow') }}</option>
                <option value="Drop">{{ t('networks.acl_drop') }}</option>
              </select>
              <button class="btn btn-ghost btn-sm text-danger ml-auto" @click="form.acl_chains.splice(ci, 1)">×</button>
            </div>
            <div v-for="(r, ri) in ch.rules" :key="ri" class="flex gap-2 items-center flex-wrap mb-1.5 ml-4 text-xs">
              <select v-model="r.action" class="input !w-auto text-xs font-bold" :class="r.action === 'Drop' ? 'text-danger' : 'text-success'">
                <option value="Allow">{{ t('networks.acl_allow') }}</option>
                <option value="Drop">{{ t('networks.acl_drop') }}</option>
              </select>
              <select v-model="r.protocol" class="input !w-auto text-xs font-mono">
                <option value="Any">any</option>
                <option value="TCP">tcp</option>
                <option value="UDP">udp</option>
                <option value="ICMP">icmp</option>
              </select>
              <input v-model="r.ports[0]" class="input !w-24 font-mono text-xs" placeholder="80 / 4000-5000" />
              <span class="text-surface-500">{{ t('networks.acl_src') }}</span>
              <input v-model="r.source_ips[0]" class="input !w-40 font-mono text-xs" placeholder="10.62.0.0/24" />
              <span class="text-surface-500">{{ t('networks.acl_dst') }}</span>
              <input v-model="r.destination_ips[0]" class="input !w-40 font-mono text-xs" placeholder="0.0.0.0/0" />
              <button class="btn btn-ghost btn-sm text-danger" @click="ch.rules.splice(ri, 1)">×</button>
            </div>
            <button class="btn btn-ghost btn-sm ml-4" @click="ch.rules.push({ name: '', priority: 10, enabled: true, protocol: 'Any', ports: [], source_ips: [], destination_ips: [], action: 'Allow' })">
              + {{ t('networks.acl_add_rule') }}
            </button>
          </div>
          <button class="btn btn-secondary btn-sm" @click="form.acl_chains.push({ name: '', chain_type: 'Inbound', enabled: true, default_action: 'Allow', rules: [] })">
            + {{ t('networks.acl_add_chain') }}
          </button>
        </section>

        <!-- Advanced flags -->
        <section>
          <h3 class="text-xs font-medium uppercase tracking-wider text-surface-600 mb-2 border-b border-white/5 pb-1 dark:text-surface-500">{{ t('networks.advanced') }}</h3>
          <div class="grid grid-cols-2 lg:grid-cols-3 gap-x-6 gap-y-2">
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.latency_first" type="checkbox" class="accent-accent-500" /> 延迟优先</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.use_smoltcp" type="checkbox" class="accent-accent-500" /> 用户态协议栈</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_ipv6" type="checkbox" class="accent-accent-500" /> 禁用 IPv6</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.enable_kcp_proxy" type="checkbox" class="accent-accent-500" /> 启用 KCP 代理</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_kcp_input" type="checkbox" class="accent-accent-500" /> 禁用 KCP 输入</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.enable_quic_proxy" type="checkbox" class="accent-accent-500" /> 启用 QUIC 代理</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_quic_input" type="checkbox" class="accent-accent-500" /> 禁用 QUIC 输入</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_p2p" type="checkbox" class="accent-accent-500" /> 禁用 P2P</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.p2p_only" type="checkbox" class="accent-accent-500" /> 仅 P2P</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.lazy_p2p" type="checkbox" class="accent-accent-500" /> 延迟 P2P</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.bind_device" type="checkbox" class="accent-accent-500" /> 仅使用物理网卡</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.no_tun" type="checkbox" class="accent-accent-500" /> 无 TUN 模式</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.enable_exit_node" type="checkbox" class="accent-accent-500" /> 启用出口节点</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.relay_all_peer_rpc" type="checkbox" class="accent-accent-500" /> 转发 RPC 包</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.need_p2p" type="checkbox" class="accent-accent-500" /> 需要 P2P</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300" title="EasyTier 2.x 始终使用多线程运行时，此开关已废弃，无实际效果"><input v-model="form.multi_thread" type="checkbox" class="accent-accent-500" /> 启用多线程 <span class="text-[10px] text-surface-400">（已废弃）</span></label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300" title="将流量交给系统协议栈转发，需系统自行开启内核转发（如 Linux sysctl ip_forward=1）"><input v-model="form.proxy_forward_by_system" type="checkbox" class="accent-accent-500" /> 系统转发</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_encryption" type="checkbox" class="accent-accent-500" /> 禁用加密</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_tcp_hole_punching" type="checkbox" class="accent-accent-500" /> 禁用 TCP 打洞</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_udp_hole_punching" type="checkbox" class="accent-accent-500" /> 禁用 UDP 打洞</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.enable_udp_broadcast_relay" type="checkbox" class="accent-accent-500" /> UDP 广播中继</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_upnp" type="checkbox" class="accent-accent-500" /> 禁用 UPnP</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.disable_sym_hole_punching" type="checkbox" class="accent-accent-500" /> 禁用对称 NAT 打洞</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.enable_magic_dns" type="checkbox" class="accent-accent-500" /> 启用魔法 DNS</label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300"><input v-model="form.enable_private_mode" type="checkbox" class="accent-accent-500" /> 启用私有模式</label>
          </div>
        </section>
      </div>

      <!-- ====== RAW TOML MODE ====== -->
      <div v-else>
        <label class="label">{{ t('networks.toml') }}</label>
        <textarea v-model="rawToml" rows="22" class="input font-mono text-xs" spellcheck="false" />
      </div>

      <div class="flex justify-end gap-2 mt-5">
        <button class="btn btn-ghost btn-sm" @click="emit('update:visible', false)">{{ t('networks.cancel') }}</button>
        <button class="btn btn-primary btn-sm" @click="onSave">{{ t('networks.save') }}</button>
      </div>
    </div>
  </div>
</template>
