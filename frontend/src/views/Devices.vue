<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { backend, auth, isNative } from '@/lib/backend'

const { t } = useI18n()

const err = ref('')
const msg = ref('')

// fleet (managed devices)
const agents = ref<any[]>([])
const commands = ref<any[]>([])
const configs = ref<any[]>([])
const showEnroll = ref(false)
const enrollName = ref('')
const newToken = ref('')

// Fleet management is admin-only in the browser; the desktop app is local.
const canManage = isNative || auth.role === 'admin'

// device admission list
interface AdmDevice {
  peer_id: string
  name: string
  network: string
  ipv4: string
  status: string
  added: string
  expires: string
  note: string
}
const admDevices = ref<AdmDevice[]>([])
const admDays = ref(0)
const admBusy = ref(false)

async function refreshAdmission() {
  try {
    admDevices.value = (await backend.devicesList()) ?? []
  } catch { /* admission unavailable */ }
}

async function admAct(action: 'approve' | 'deny' | 'remove', d: AdmDevice) {
  if (action === 'deny' && !confirm(t('devices.confirm_deny'))) return
  if (action === 'remove' && !confirm(t('devices.confirm_remove'))) return
  admBusy.value = true
  try {
    await backend.deviceAct(action, d.peer_id, {
      days: action === 'approve' ? Number(admDays.value) || 0 : 0,
      hostname: d.name, network: d.network, ipv4: d.ipv4,
    })
    await refreshAdmission()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  } finally {
    admBusy.value = false
  }
}

function admLabel(d: AdmDevice): string {
  if (d.status === 'allow' && d.expires) {
    const left = Math.ceil((new Date(d.expires).getTime() - Date.now()) / 86400000)
    return `${t('devices_expires')} ${d.expires.slice(0, 10)} (${left}d)`
  }
  return d.note || ''
}

async function refreshFleet() {
  try {
    const data = await backend.fleetList()
    agents.value = data.agents ?? []
    commands.value = data.commands ?? []
  } catch { /* fleet unavailable */ }
}

async function refreshConfigs() {
  try {
    configs.value = await backend.listConfigs()
  } catch { /* configs unavailable */ }
}

async function enroll() {
  err.value = ''
  try {
    const r = await backend.createAgent(enrollName.value.trim())
    newToken.value = r.token
    enrollName.value = ''
    await refreshFleet()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

async function removeAgent(a: any) {
  if (!confirm(t('fleet.delete_confirm', { name: a.name || a.id.slice(0, 8) }))) return
  try {
    await backend.deleteAgent(a.id)
    await refreshFleet()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

// onJoinNetwork drives a remote agent to join a network: the hub sends the
// selected config TOML (sanitized server-side) and a start command.
async function onJoinNetwork(a: any, ev: Event) {
  const el = ev.target as HTMLSelectElement
  const instanceId = el.value
  el.value = ''
  if (!instanceId) return
  await pushNetwork(a, instanceId)
}

async function pushNetwork(a: any, instanceId: string) {
  if (!instanceId) return
  const cfg = configs.value.find((c) => c.instance_id === instanceId)
  if (!cfg) return
  try {
    await backend.agentCommand(a.id, 'start_network', instanceId, cfg.raw)
    msg.value = t('fleet.command_sent')
    setTimeout(refreshFleet, 1500)
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

// leaveNetwork stops every network the agent currently runs.
async function leaveNetwork(a: any) {
  if (!confirm(t('fleet.leave_confirm', { name: a.name }))) return
  for (const n of a.networks || []) {
    const cfg = configs.value.find((c) => c.network === n)
    try {
      await backend.agentCommand(a.id, 'stop_network', cfg?.instance_id || '')
    } catch (e) {
      err.value = String(e).replace(/^Error: /, '')
    }
  }
  msg.value = t('fleet.command_sent')
  setTimeout(refreshFleet, 1500)
}

async function sendCore(a: any, action: string) {
  try {
    await backend.agentCommand(a.id, action)
    msg.value = t('fleet.command_sent')
    setTimeout(refreshFleet, 1500)
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

onMounted(async () => {
  await refreshFleet()
  await refreshConfigs()
  await refreshAdmission()
})
</script>

<template>
  <div class="p-8 space-y-6 app-page">
    <div class="flex items-start justify-between flex-wrap gap-2">
      <div>
        <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('fleet.title') }}</h1>
        <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">{{ t('fleet.subtitle') }}</p>
      </div>
      <button v-if="canManage" class="btn btn-primary btn-sm" @click="showEnroll = true">+ {{ t('fleet.enroll') }}</button>
    </div>

    <p v-if="err" class="text-sm text-danger">{{ err }}</p>
    <p v-if="msg" class="text-sm text-success font-bold">{{ msg }}</p>

    <!-- Device admission list: allowlist / blacklist with credential expiry -->
    <div v-if="canManage" class="card p-5">
      <div class="flex items-center justify-between mb-2 flex-wrap gap-2">
        <h2 class="text-sm font-medium dark:text-surface-300 text-surface-700">{{ t('devices_title') }}</h2>
        <div class="flex items-center gap-2">
          <label class="text-xs text-surface-600 dark:text-surface-500">{{ t('devices_approve_days') }}</label>
          <input v-model.number="admDays" type="number" min="0" class="input !w-20 text-xs font-mono" placeholder="0" />
          <button class="btn btn-secondary btn-sm" :disabled="admBusy" @click="refreshAdmission">⟳</button>
        </div>
      </div>
      <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('devices_hint') }}</p>
      <p v-if="!admDevices.length" class="text-xs text-surface-500">{{ t('devices_none') }}</p>
      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">
              <th class="py-2 pr-4">{{ t('dashboard.peer_id') }}</th>
              <th class="py-2 pr-4">{{ t('dashboard.hostname') }}</th>
              <th class="py-2 pr-4">{{ t('networks.name') }}</th>
              <th class="py-2 pr-4">IPv4</th>
              <th class="py-2 pr-4">{{ t('users.col_role') }}</th>
              <th class="py-2 pr-4">{{ t('devices_expires') }}</th>
              <th class="py-2 pr-4 text-right">{{ t('settings.open_dir') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="d in admDevices" :key="d.peer_id" class="border-t border-surface-100 dark:border-white/5">
              <td class="py-2 pr-4 font-mono text-xs">{{ d.peer_id }}</td>
              <td class="py-2 pr-4">{{ d.name || '—' }}</td>
              <td class="py-2 pr-4">{{ d.network || '—' }}</td>
              <td class="py-2 pr-4 font-mono text-xs">{{ d.ipv4 || '—' }}</td>
              <td class="py-2 pr-4">
                <span class="badge text-[10px]" :class="d.status === 'allow' ? 'badge-green' : 'badge-red'">
                  {{ d.status === 'allow' ? t('devices_allow') : t('devices_deny') }}
                </span>
              </td>
              <td class="py-2 pr-4 text-xs text-surface-500">{{ admLabel(d) }}</td>
              <td class="py-2 pr-4 text-right whitespace-nowrap">
                <button v-if="d.status !== 'allow'" class="btn btn-secondary btn-sm mr-1" :disabled="admBusy" @click="admAct('approve', d)">{{ t('devices_approve') }}</button>
                <button v-if="d.status !== 'deny'" class="btn btn-danger btn-sm mr-1" :disabled="admBusy" @click="admAct('deny', d)">{{ t('devices_blacklist') }}</button>
                <button class="btn btn-ghost btn-sm" :disabled="admBusy" @click="admAct('remove', d)">{{ t('devices_remove') }}</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- Managed devices (fleet): enroll agents, drive them remotely -->
    <div class="card p-5">
      <div class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead>
            <tr class="text-left text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">
              <th class="py-2 pr-4">{{ t('fleet.col_name') }}</th>
              <th class="py-2 pr-4">{{ t('fleet.col_os') }}</th>
              <th class="py-2 pr-4">{{ t('fleet.col_ip') }}</th>
              <th class="py-2 pr-4">{{ t('fleet.col_networks') }}</th>
              <th class="py-2 pr-4">{{ t('fleet.col_autostart') }}</th>
              <th class="py-2 pr-4">{{ t('fleet.col_online') }}</th>
              <th class="py-2" v-if="canManage"></th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="a in agents" :key="a.id" class="border-t border-surface-100 dark:border-white/5">
              <td class="py-2.5 pr-4 font-medium text-surface-800 dark:text-surface-200">
                {{ a.name || a.id.slice(0, 8) }}
                <span v-if="!a.auto_start" class="badge badge-yellow text-[10px] ml-1" :title="t('fleet.autostart_warn')">{{ t('fleet.no_autostart') }}</span>
              </td>
              <td class="py-2.5 pr-4 text-xs text-surface-600 dark:text-surface-400">{{ a.os }} <span class="text-surface-400">{{ a.version }}</span></td>
              <td class="py-2.5 pr-4 font-mono text-xs dark:text-surface-300 text-surface-700">{{ a.ipv4 || '—' }}</td>
              <td class="py-2.5 pr-4 text-xs text-surface-600 dark:text-surface-400">{{ (a.networks || []).join(', ') || '—' }}</td>
              <td class="py-2.5 pr-4">
                <span class="badge text-[10px]" :class="a.auto_start ? 'badge-green' : 'badge-yellow'">{{ a.auto_start ? '✓' : '✗' }}</span>
              </td>
              <td class="py-2.5 pr-4">
                <span class="badge text-[10px]" :class="a.online ? 'badge-green' : 'badge-gray'">{{ a.online ? t('fleet.online') : t('fleet.offline') }}</span>
              </td>
              <td v-if="canManage" class="py-2.5 text-right whitespace-nowrap">
                <select v-if="configs.length" class="input !w-auto text-xs py-1 mr-2"
                        :value="''" @change="onJoinNetwork(a, $event)">
                  <option value="" disabled>{{ t('fleet.join_network') }}</option>
                  <option v-for="c in configs" :key="c.instance_id" :value="c.instance_id">{{ c.network || c.instance_name }}</option>
                </select>
                <button v-if="a.networks?.length" class="btn btn-secondary btn-sm mr-2" @click="leaveNetwork(a)">{{ t('fleet.leave_all') }}</button>
                <button v-if="a.online" class="btn btn-ghost btn-sm mr-2" @click="sendCore(a, 'stop_core')">{{ t('fleet.stop_core') }}</button>
                <button v-else class="btn btn-ghost btn-sm mr-2" @click="sendCore(a, 'start_core')">{{ t('fleet.start_core') }}</button>
                <button class="btn btn-ghost btn-sm text-danger" @click="removeAgent(a)">✕</button>
              </td>
            </tr>
            <tr v-if="!agents.length">
              <td colspan="7" class="py-6 text-center text-surface-500">{{ t('fleet.empty') }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-if="commands.length" class="mt-4 border-t border-surface-100 pt-3 dark:border-white/5">
        <h3 class="text-xs font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400 mb-2">{{ t('fleet.recent_commands') }}</h3>
        <div v-for="c in commands.slice(0, 6)" :key="c.id" class="text-xs text-surface-600 dark:text-surface-400 py-0.5 font-mono truncate">
          <span :class="c.status === 'done' ? 'text-success' : c.status === 'failed' ? 'text-danger' : 'text-warning'">{{ c.status }}</span>
          · {{ c.action }} {{ c.network_id?.slice(0, 8) }} {{ c.result ? '· ' + c.result : '' }}
        </div>
      </div>
    </div>

    <!-- enroll dialog: one-time token -->
    <div v-if="showEnroll" class="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-6" @click.self="showEnroll = false">
      <div class="card w-full max-w-md p-6 space-y-4">
        <h2 class="text-sm font-bold uppercase tracking-wide dark:text-white text-surface-900">{{ t('fleet.enroll_title') }}</h2>
        <template v-if="!newToken">
          <div>
            <label class="label">{{ t('fleet.agent_name') }}</label>
            <input v-model="enrollName" class="input" placeholder="office-nas" />
          </div>
          <button class="btn btn-primary w-full" :disabled="!enrollName.trim()" @click="enroll">{{ t('fleet.enroll') }}</button>
        </template>
        <template v-else>
          <p class="text-xs text-surface-600 dark:text-surface-500">{{ t('fleet.token_hint') }}</p>
          <code class="block font-mono text-xs p-3 bg-surface-100 dark:bg-surface-950/60 rounded break-all text-surface-800 dark:text-surface-300">{{ newToken }}</code>
          <p class="text-xs text-danger">{{ t('fleet.token_once') }}</p>
          <button class="btn btn-secondary w-full" @click="showEnroll = false">{{ t('networks.cancel') }}</button>
        </template>
      </div>
    </div>
  </div>
</template>
