<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { backend, auth, isNative } from '@/lib/backend'

const { t } = useI18n()

const tab = ref<'accounts' | 'sessions' | 'audit'>('accounts')

// ---- accounts ----
const users = ref<any[]>([])
const busy = ref(false)
const err = ref('')
const msg = ref('')

// create / edit form
const showForm = ref(false)
const editName = ref('')
const username = ref('')
const password = ref('')
const role = ref<'admin' | 'operator' | 'viewer'>('viewer')
const networks = ref<string[]>([])
const configs = ref<any[]>([])

// ---- login sessions ----
const sessions = ref<any[]>([])
const busyId = ref('')

// ---- audit log (admin) ----
const auditRows = ref<any[]>([])
const auditMsg = ref('')
const auditBusy = ref(false)
const auditFilter = ref('')
const auditLoaded = ref(false)

const filteredAudit = computed(() => {
  const q = auditFilter.value.trim().toLowerCase()
  if (!q) return auditRows.value
  return auditRows.value.filter((e) =>
    [e.event, e.actor, e.ip, e.detail].some((v: string) => (v || '').toLowerCase().includes(q)),
  )
})

async function refreshAudit() {
  auditBusy.value = true
  auditMsg.value = ''
  try {
    auditRows.value = (await backend.auditTail(200)) ?? []
    if (!auditRows.value.length) auditMsg.value = t('settings.audit_empty')
    auditLoaded.value = true
  } catch (e) {
    auditMsg.value = String(e).replace(/^Error: /, '')
  } finally {
    auditBusy.value = false
  }
}

// Accounts + audit endpoints are admin-only; sessions any signed-in user.
const canManage = computed(() => isNative || auth.role === 'admin')

async function refresh() {
  if (!canManage.value) return
  try {
    users.value = await backend.listUsers()
  } catch (e) {
    err.value = String(e)
  }
}

async function refreshConfigs() {
  try {
    configs.value = await backend.listConfigs()
  } catch { /* configs unavailable */ }
}

async function refreshSessions() {
  try {
    sessions.value = await backend.listSessions()
  } catch { /* sessions unavailable (desktop mode) */ }
}

function openCreate() {
  editName.value = ''
  username.value = ''
  password.value = ''
  role.value = 'viewer'
  networks.value = []
  showForm.value = true
}

function openEdit(u: any) {
  editName.value = u.username
  username.value = u.username
  password.value = ''
  role.value = ['admin', 'operator', 'viewer'].includes(u.role) ? u.role : 'viewer'
  networks.value = Array.isArray(u.networks) ? [...u.networks] : []
  showForm.value = true
}

function toggleNetwork(name: string) {
  const i = networks.value.indexOf(name)
  if (i >= 0) networks.value.splice(i, 1)
  else networks.value.push(name)
}

async function submit() {
  if (busy.value) return
  busy.value = true
  err.value = ''
  msg.value = ''
  try {
    await backend.saveUser({
      username: username.value.trim(),
      password: password.value || undefined,
      role: role.value,
      networks: networks.value,
    })
    showForm.value = false
    msg.value = t('users.saved')
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  } finally {
    busy.value = false
  }
}

async function removeUser(u: any) {
  if (!confirm(t('users.delete_confirm', { name: u.username }))) return
  try {
    await backend.deleteUser(u.username)
    await refresh()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

async function revoke(s: any) {
  if (!confirm(t('devices.revoke_confirm', { ip: s.ip || s.id.slice(0, 8) }))) return
  busyId.value = s.id
  try {
    await backend.revokeSession(s.id)
    await refreshSessions()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  } finally {
    busyId.value = ''
  }
}

async function revokeOthers() {
  if (!confirm(t('devices.revoke_others_confirm'))) return
  try {
    const n = await backend.revokeOtherSessions()
    msg.value = t('devices.revoked_n', { n })
    await refreshSessions()
  } catch (e) {
    err.value = String(e).replace(/^Error: /, '')
  }
}

function uaShort(ua: string): string {
  if (!ua) return '—'
  if (ua.includes('Edg/')) return 'Edge'
  if (ua.includes('Chrome/')) return 'Chrome'
  if (ua.includes('Firefox/')) return 'Firefox'
  if (ua.includes('Safari/')) return 'Safari'
  return ua.slice(0, 40)
}

onMounted(async () => {
  await refresh()
  await refreshConfigs()
  await refreshSessions()
})
</script>

<template>
  <div class="p-8 space-y-6 app-page">
    <div class="flex items-start justify-between flex-wrap gap-2">
      <div>
        <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('users.title') }}</h1>
        <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">{{ t('users.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-2 flex-wrap">
        <button
          v-if="canManage"
          class="btn btn-sm" :class="tab === 'accounts' ? 'btn-primary' : 'btn-secondary'"
          @click="tab = 'accounts'"
        >
          {{ t('users.tab_accounts') }}
        </button>
        <button
          class="btn btn-sm" :class="tab === 'sessions' ? 'btn-primary' : 'btn-secondary'"
          @click="tab = 'sessions'; refreshSessions()"
        >
          {{ t('users.tab_sessions') }}
        </button>
        <button
          v-if="canManage"
          class="btn btn-sm" :class="tab === 'audit' ? 'btn-primary' : 'btn-secondary'"
          @click="tab = 'audit'; if (!auditLoaded) refreshAudit()"
        >
          {{ t('settings.audit') }}
        </button>
        <button v-if="tab === 'accounts' && canManage" class="btn btn-primary btn-sm" @click="openCreate">+ {{ t('users.create') }}</button>
        <button v-if="tab === 'sessions' && auth.user" class="btn btn-secondary btn-sm" @click="revokeOthers">
          {{ t('devices.revoke_others') }}
        </button>
      </div>
    </div>

    <p v-if="err" class="text-sm text-danger">{{ err }}</p>
    <p v-if="msg" class="text-sm text-success font-bold">{{ msg }}</p>

    <!-- Accounts tab -->
    <div v-if="tab === 'accounts' && canManage" class="card p-5 overflow-x-auto">
      <table class="w-full text-sm">
        <thead>
          <tr class="text-left text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">
            <th class="py-2 pr-4">{{ t('users.col_username') }}</th>
            <th class="py-2 pr-4">{{ t('users.col_role') }}</th>
            <th class="py-2 pr-4">{{ t('users.col_networks') }}</th>
            <th class="py-2 pr-4">{{ t('users.col_created') }}</th>
            <th class="py-2" v-if="auth.role === 'admin'"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="u in users" :key="u.username" class="border-t border-surface-100 dark:border-white/5">
            <td class="py-2.5 pr-4 font-mono text-xs dark:text-surface-200 text-surface-800">
              {{ u.username }}
              <span v-if="u.username === auth.user" class="badge badge-yellow text-[10px] ml-1">{{ t('users.you') }}</span>
            </td>
            <td class="py-2.5 pr-4">
              <span class="badge text-[10px]" :class="u.role === 'admin' ? 'badge-green' : u.role === 'operator' ? 'badge-yellow' : 'badge-gray'">{{ u.role }}</span>
            </td>
            <td class="py-2.5 pr-4 text-xs text-surface-600 dark:text-surface-400">
              {{ (u.networks?.length ? u.networks.join(', ') : t('users.all_networks')) }}
            </td>
            <td class="py-2.5 pr-4 text-xs text-surface-500 dark:text-surface-500">{{ (u.created || '').replace('T', ' ').slice(0, 16) }}</td>
            <td class="py-2.5 text-right whitespace-nowrap" v-if="auth.role === 'admin'">
              <button class="btn btn-secondary btn-sm mr-2" @click="openEdit(u)">{{ t('users.edit') }}</button>
              <button class="btn btn-ghost btn-sm text-danger" :disabled="u.username === auth.user" @click="removeUser(u)">
                {{ t('users.delete') }}
              </button>
            </td>
          </tr>
          <tr v-if="!users.length">
            <td colspan="5" class="py-6 text-center text-surface-500">{{ t('users.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Login sessions tab -->
    <div v-else-if="tab === 'sessions'" class="card p-5 overflow-x-auto">
      <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('devices.subtitle') }}</p>
      <table class="w-full text-sm">
        <thead>
          <tr class="text-left text-[10px] font-bold uppercase tracking-wider text-surface-600 dark:text-surface-400">
            <th class="py-2 pr-4">{{ t('devices.col_user') }}</th>
            <th class="py-2 pr-4">{{ t('devices.col_ip') }}</th>
            <th class="py-2 pr-4">{{ t('devices.col_browser') }}</th>
            <th class="py-2 pr-4">{{ t('devices.col_login') }}</th>
            <th class="py-2 pr-4">{{ t('devices.col_active') }}</th>
            <th class="py-2"></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="s in sessions" :key="s.id" class="border-t border-surface-100 dark:border-white/5">
            <td class="py-2.5 pr-4">
              <span class="font-mono text-xs dark:text-surface-200 text-surface-800">{{ s.user }}</span>
              <span class="badge text-[10px] ml-1" :class="s.role === 'admin' ? 'badge-green' : 'badge-gray'">{{ s.role }}</span>
              <span v-if="s.current" class="badge badge-yellow text-[10px] ml-1">{{ t('devices.current') }}</span>
            </td>
            <td class="py-2.5 pr-4 font-mono text-xs dark:text-surface-300 text-surface-700">{{ s.ip }}</td>
            <td class="py-2.5 pr-4 text-xs text-surface-600 dark:text-surface-400" :title="s.ua">{{ uaShort(s.ua) }}</td>
            <td class="py-2.5 pr-4 text-xs text-surface-500">{{ (s.created || '').replace('T', ' ').slice(0, 19) }}</td>
            <td class="py-2.5 pr-4 text-xs text-surface-500">{{ (s.last_seen || '').replace('T', ' ').slice(0, 19) }}</td>
            <td class="py-2.5 text-right">
              <button class="btn btn-ghost btn-sm text-danger"
                      :disabled="s.current || busyId === s.id"
                      :title="s.current ? t('devices.current_hint') : ''"
                      @click="revoke(s)">
                {{ t('devices.revoke') }}
              </button>
            </td>
          </tr>
          <tr v-if="!sessions.length">
            <td colspan="6" class="py-6 text-center text-surface-500">{{ t('devices.empty') }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Audit log tab (admin) -->
    <div v-else-if="tab === 'audit'" class="card p-5">
      <div class="flex items-center justify-between mb-3 flex-wrap gap-2">
        <p class="text-xs text-surface-600 dark:text-surface-500">{{ t('settings.audit_hint') }}</p>
        <button class="btn btn-secondary btn-sm" :disabled="auditBusy" @click="refreshAudit">{{ t('settings.audit_refresh') }}</button>
      </div>
      <input v-model="auditFilter" class="input mb-3 font-mono text-xs" :placeholder="t('settings.audit_filter')" />
      <div v-if="auditMsg" class="text-xs text-surface-600 mb-2 dark:text-surface-500">{{ auditMsg }}</div>
      <div v-if="filteredAudit.length" class="max-h-96 overflow-y-auto border border-surface-100 rounded-lg dark:border-white/5">
        <table class="w-full text-xs">
          <tbody>
            <tr v-for="(e, i) in filteredAudit" :key="i" class="border-b border-surface-100 last:border-0 dark:border-white/5">
              <td class="py-1.5 px-2 font-mono text-surface-500 whitespace-nowrap">{{ (e.time || '').replace('T', ' ').slice(0, 19) }}</td>
              <td class="py-1.5 px-2 font-mono font-bold text-accent-600 dark:text-accent-400 whitespace-nowrap">{{ e.event }}</td>
              <td class="py-1.5 px-2 text-surface-700 dark:text-surface-300 whitespace-nowrap">{{ e.actor }}</td>
              <td class="py-1.5 px-2 font-mono text-surface-500 whitespace-nowrap">{{ e.ip }}</td>
              <td class="py-1.5 px-2 text-surface-600 dark:text-surface-400 truncate max-w-[16rem]" :title="e.detail">{{ e.detail }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- create / edit dialog -->
    <div v-if="showForm" class="fixed inset-0 z-40 flex items-center justify-center bg-black/60 p-6" @click.self="showForm = false">
      <div class="card w-full max-w-md p-6 space-y-4">
        <h2 class="text-sm font-bold uppercase tracking-wide dark:text-white text-surface-900">
          {{ editName ? t('users.edit_title') : t('users.create_title') }}
        </h2>
        <div>
          <label class="label">{{ t('login.username') }}</label>
          <input v-model="username" class="input" :disabled="!!editName" />
        </div>
        <div>
          <label class="label">{{ editName ? t('settings.account_new') : t('login.password') }}</label>
          <input v-model="password" type="password" class="input" autocomplete="new-password"
                 :placeholder="editName ? t('users.password_keep') : ''" />
        </div>
        <div>
          <label class="label">{{ t('users.col_role') }}</label>
          <select v-model="role" class="input">
            <option value="admin">admin</option>
            <option value="operator">operator</option>
            <option value="viewer">viewer</option>
          </select>
          <p class="text-xs text-surface-600 mt-1 dark:text-surface-500">{{ t('users.role_hint') }}</p>
        </div>
        <div>
          <label class="label">{{ t('users.col_networks') }}</label>
          <div class="flex flex-wrap gap-2">
            <button v-for="c in configs" :key="c.instance_id" type="button"
                    class="btn btn-sm" :class="networks.includes(c.network) ? 'btn-primary' : 'btn-secondary'"
                    @click="toggleNetwork(c.network)">
              {{ c.network || c.instance_name }}
            </button>
          </div>
          <p class="text-xs text-surface-600 mt-1 dark:text-surface-500">{{ t('users.networks_hint') }}</p>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <button class="btn btn-secondary btn-sm" @click="showForm = false">{{ t('networks.cancel') }}</button>
          <button class="btn btn-primary btn-sm" :disabled="busy || !username.trim()" @click="submit">
            {{ t('networks.save') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
