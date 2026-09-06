<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { backend, events, onEvent, isNative, auth, type Version } from '@/lib/backend'
import { useCore } from '@/composables/useCore'
import { useTheme } from '@/composables/useTheme'
import { setLocale } from '@/lib/i18n'

const { t, locale } = useI18n()
const { theme, apply } = useTheme()

const version = ref<Version | null>(null)
const appInfo = ref<Record<string, string> | null>(null)
const webInfo = ref<Record<string, string> | null>(null)
const logs = ref<string[]>([])
const { status } = useCore()

const settings = ref<any>({
  web_bind: '127.0.0.1',
  web_port: 0,
  web_https: false,
  log_dir: '',
  config_dir: '',
  custom_css: '',
  core_version: '',
  core_mirror: '',
  app_log_level: 'info',
  quota: { enabled: false, monthly_gb: 0, warn_percent: 80 },
  auto_start: false,
  magic_dns: false,
  fleet: { enabled: false, server_url: '', token: '', name: '', hub_user: '', hub_pass: '' },
  alerts: { enabled: false, offline_notify: true, high_latency_notify: false, high_latency_ms: 200, webhook_url: '' },
  webdav: { server_url: '', username: '', password: '', last_sync: '', sync_accounts: false, auto_sync: false },
})
const savedFlash = ref(false)
const settingsError = ref('')
const webdavMsg = ref('')
const webdavBusy = ref(false)
const notifyMsg = ref('')

const lang = computed({
  get: () => locale.value,
  set: (v: string) => setLocale(v as 'en' | 'zh'),
})

// Advanced / rarely-touched sections stay collapsed so the common settings
// are reachable without scrolling a wall of cards.
const showFleet = ref(false)
const showWebdav = ref(false)
const showCSS = ref(false)
const showSpec = ref(false)
const showCore = ref(false)
const showQuota = ref(false)
const showAppLog = ref(false)
const quotaUsed = ref<any>(null)
const appLogLines = ref<string[]>([])
const appLogLoading = ref(false)
const showMetrics = ref(false)

async function refreshInfo() {
  version.value = await backend.versions()
  appInfo.value = await backend.appInfo()
  webInfo.value = await backend.webInfo()
}

async function refreshLogs() {
  if (isNative) logs.value = (await backend.coreLog()).slice(-200)
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
  } catch (e) {
    console.error('clipboard copy failed', e)
  }
}

async function refreshSettings() {
  try {
    const s = await backend.getSettings()
    settings.value = {
      web_bind: s.web_bind ?? '127.0.0.1',
      web_port: s.web_port ?? 0,
      web_https: s.web_https ?? false,
      log_dir: s.log_dir ?? '',
      config_dir: s.config_dir ?? '',
      custom_css: s.custom_css ?? '',
      core_version: s.core_version ?? '',
      core_mirror: s.core_mirror ?? '',
      app_log_level: s.app_log_level ?? 'info',
      quota: {
        enabled: s.quota?.enabled ?? false,
        monthly_gb: s.quota?.monthly_gb ?? 0,
        warn_percent: s.quota?.warn_percent ?? 80,
      },
      auto_start: s.auto_start ?? false,
      magic_dns: s.magic_dns ?? false,
      fleet: {
        enabled: s.fleet?.enabled ?? false,
        server_url: s.fleet?.server_url ?? '',
        token: s.fleet?.token ?? '',
        name: s.fleet?.name ?? '',
        hub_user: s.fleet?.hub_user ?? '',
        hub_pass: s.fleet?.hub_pass ?? '',
        cert_pin: s.fleet?.cert_pin ?? '',
      },
      alerts: {
        enabled: s.alerts?.enabled ?? false,
        offline_notify: s.alerts?.offline_notify ?? true,
        high_latency_notify: s.alerts?.high_latency_notify ?? false,
        high_latency_ms: s.alerts?.high_latency_ms ?? 200,
        webhook_url: s.alerts?.webhook_url ?? '',
      },
      webdav: {
        server_url: s.webdav?.server_url ?? '',
        username: s.webdav?.username ?? '',
        password: s.webdav?.password ?? '',
        last_sync: s.webdav?.last_sync ?? '',
        sync_accounts: s.webdav?.sync_accounts ?? false,
        auto_sync: s.webdav?.auto_sync ?? false,
      },
    }
    applyCustomCSS()
  } catch (e) {
    settingsError.value = String(e)
  }
}

function applyCustomCSS() {
  let el = document.getElementById('et-custom-css') as HTMLStyleElement | null
  if (!el) {
    el = document.createElement('style')
    el.id = 'et-custom-css'
    document.head.appendChild(el)
  }
  el.textContent = settings.value.custom_css || ''
}

// Starter snippet for the Custom CSS field. Kept here so users can open the
// reference (below) and click "Fill example" to get a working baseline.
const cssExample = `/* EasyTier Pro — Custom CSS example / 自定义样式示例 */
/* Theme variables change with light/dark mode automatically.
   主题变量会随明暗模式自动切换。 */
:root {
  --accent: #FF4D00;      /* highlight color / 强调色 */
  --success: #00C853;     /* ok color / 成功色 */
  --danger: #FF2E2E;      /* error color / 危险色 */
  --yellow: #FFE600;      /* warning color / 警告色 */
}
html.dark {
  --accent: #FF6A3D;      /* brighter on dark / 暗色下更亮 */
}

/* Cards, buttons and badges are built from these variables,
   so overriding a variable restyles everything at once.
   卡片/按钮/徽章都基于这些变量，改一个变量即全局生效。 */
.card { border-width: 2px; }            /* thinner border / 细边框 */
.btn { text-transform: none; }          /* keep casing / 保留大小写 */
.input { border-color: var(--blue); }   /* input border / 输入框边框 */

/* Table row hover / 表格行悬停 */
tbody tr:hover { background: var(--yellow); color: #111; }

/* Topology view / 拓扑图 */
.topology-svg { max-height: 60vh; }            /* smaller graph / 图更矮 */
g.cursor-pointer rect { transition: stroke 0.2s; }  /* node hover easing */
.link-line { stroke: var(--blue); }            /* P2P links color / 连线颜色 */

/* Mobile / narrow window tweaks / 窄窗口微调 */
@media (max-width: 900px) {
  .app-page { padding: 0.5rem !important; }
  h1 { font-size: 0.95rem !important; }
}
`

function fillExample() {
  settings.value.custom_css = cssExample
  applyCustomCSS()
}

// ---- monthly traffic quota ----
function fmtTraffic(v: number): string {
  if (!v) return '0 B'
  if (v >= 1024 ** 3) return (v / 1024 ** 3).toFixed(2) + ' GB'
  if (v >= 1024 ** 2) return (v / 1024 ** 2).toFixed(1) + ' MB'
  if (v >= 1024) return (v / 1024).toFixed(1) + ' KB'
  return Math.round(v) + ' B'
}
async function refreshQuota() {
  try {
    quotaUsed.value = await backend.trafficHistory(31)
  } catch { /* history unavailable */ }
}

// ---- application log viewer ----
async function refreshAppLog() {
  appLogLoading.value = true
  try {
    appLogLines.value = (await backend.appLogTail(300)) ?? []
    if (!appLogLines.value.length) appLogLines.value = []
  } catch (e) {
    appLogLines.value = [String(e)]
  } finally {
    appLogLoading.value = false
  }
}

async function saveSettings() {
  settingsError.value = ''
  settings.value.web_port = Number(settings.value.web_port) || 0
  try {
    await backend.saveSettings(settings.value)
    savedFlash.value = true
    setTimeout(() => (savedFlash.value = false), 2000)
    applyCustomCSS()
    await refreshInfo() // web bind/port may have changed
  } catch (e) {
    settingsError.value = String(e)
  }
}

async function openDir(path: string) {
  if (path) await backend.openDir(path)
}

async function doNotifyTest() {
  notifyMsg.value = ''
  try {
    await backend.saveSettings(settings.value)
    await backend.sendTestNotify()
    notifyMsg.value = t('settings.notify_test_ok')
  } catch (e) {
    notifyMsg.value = String(e)
  }
}

const leases = ref<Record<string, { ip: string; prefix: number; hostname: string; updated: string }>>({})
const leaseForgetting = ref('')

// ---- web account ----
const hasAccount = ref(false)
const accUsername = ref('')
const accCurrent = ref('')
const accNew = ref('')
const accConfirm = ref('')
const accMsg = ref('')
const accErr = ref('')
const accBusy = ref(false)
const rotatedToken = ref('')

async function refreshAccount() {
  try {
    hasAccount.value = await backend.hasAccount()
  } catch { hasAccount.value = false }
  if (!isNative && auth.user) accUsername.value = auth.user
}

async function saveAccount() {
  accErr.value = ''
  accMsg.value = ''
  if (accNew.value !== accConfirm.value) {
    accErr.value = t('settings.acc_mismatch')
    return
  }
  accBusy.value = true
  try {
    await backend.changePassword(accCurrent.value, accNew.value, accUsername.value.trim())
    accMsg.value = t('settings.acc_saved')
    accCurrent.value = ''
    accNew.value = ''
    accConfirm.value = ''
    hasAccount.value = true
  } catch (e) {
    accErr.value = String(e).replace(/^Error: /, '')
  } finally {
    accBusy.value = false
  }
}

async function doRotateToken() {
  accErr.value = ''
  try {
    rotatedToken.value = await backend.rotateToken()
  } catch (e) {
    accErr.value = String(e).replace(/^Error: /, '')
  }
}

async function doLogout() {
  await backend.logout()
  location.reload()
}

async function refreshLeases() {
  try {
    leases.value = await backend.listLeases()
  } catch { /* backend unavailable */ }
}

async function forgetLease(network: string) {
  leaseForgetting.value = network
  try {
    await backend.forgetLease(network)
    await refreshLeases()
  } finally {
    leaseForgetting.value = ''
  }
}

async function doWebdavPush() {  if (webdavBusy.value) return
  webdavBusy.value = true
  webdavMsg.value = ''
  try {
    await backend.saveSettings(settings.value)
    const msg = await backend.webdavPush()
    webdavMsg.value = msg
    await refreshSettings()
  } catch (e) {
    webdavMsg.value = String(e)
  } finally {
    webdavBusy.value = false
  }
}

async function doWebdavPull() {
  if (webdavBusy.value) return
  if (!confirm(t('settings.webdav_pull_confirm'))) return
  webdavBusy.value = true
  webdavMsg.value = ''
  try {
    await backend.saveSettings(settings.value)
    const msg = await backend.webdavPull()
    webdavMsg.value = msg
    await refreshSettings()
    await refreshInfo()
  } catch (e) {
    webdavMsg.value = String(e)
  } finally {
    webdavBusy.value = false
  }
}

// ---- web HTTPS (self-signed certificate) ----
const tlsInfo = ref<any>(null)

async function refreshTLS() {
  try {
    tlsInfo.value = await backend.webTLSStatus()
  } catch { /* tls status unavailable */ }
}

async function rotateTLSCert() {
  try {
    tlsInfo.value = await backend.rotateWebTLSCert()
    webdavMsg.value = t('settings.tls_rotated')
  } catch (e) {
    settingsError.value = String(e).replace(/^Error: /, '')
  }
}

async function doClearFleetPin() {
  try {
    await backend.clearFleetPin()
    webdavMsg.value = t('settings.fleet_pin_cleared')
  } catch (e) {
    settingsError.value = String(e).replace(/^Error: /, '')
  }
}

// ---- metrics ----
const metricsText = ref('')
const metricsMsg = ref('')

async function refreshMetrics() {
  metricsMsg.value = ''
  try {
    metricsText.value = (await backend.metricsText()) || ''
  } catch (e) {
    metricsMsg.value = String(e).replace(/^Error: /, '')
  }
}

// ---- core version management ----
interface CoreRelease {
  tag: string
  date: string
  asset_name: string
  asset_size: number
  assets: { name: string; url: string; size: number }[]
}
interface CoreInstall {
  tag: string
  core: string
  cli: string
  core_path: string
  active: boolean
}
const coreState = ref<{ releases: CoreRelease[]; error: string; installed: CoreInstall[]; active: string; bundled: string; mirror: string } | null>(null)
const coreBusy = ref(false)
const coreMsg = ref('')
const coreProgress = ref<{ tag: string; loaded: number; total: number } | null>(null)

function fmtSize(n: number): string {
  if (!n) return ''
  if (n >= 1024 ** 2) return (n / 1024 ** 2).toFixed(1) + ' MB'
  return (n / 1024).toFixed(0) + ' KB'
}

async function refreshCore() {
  coreMsg.value = ''
  try {
    coreState.value = await backend.coreReleases()
  } catch (e) {
    coreMsg.value = String(e).replace(/^Error: /, '')
  }
}

async function coreInstall(tag: string) {
  if (!confirm(t('settings.core_confirm'))) return
  coreBusy.value = true
  coreMsg.value = ''
  try {
    await backend.coreInstall(tag)
    await refreshCore()
  await refreshQuota()
  } catch (e) {
    coreMsg.value = String(e).replace(/^Error: /, '')
  } finally {
    coreBusy.value = false
    coreProgress.value = null
  }
}

async function coreSwitch(tag: string) {
  coreBusy.value = true
  coreMsg.value = ''
  try {
    await backend.coreSetActive(tag)
    await refreshCore()
  await refreshQuota()
  } catch (e) {
    coreMsg.value = String(e).replace(/^Error: /, '')
  } finally {
    coreBusy.value = false
  }
}

async function coreDelete(tag: string) {
  coreBusy.value = true
  coreMsg.value = ''
  try {
    await backend.coreDelete(tag)
    await refreshCore()
  await refreshQuota()
  } catch (e) {
    coreMsg.value = String(e).replace(/^Error: /, '')
  } finally {
    coreBusy.value = false
  }
}

// Platform assets are picked server-side (AssetFor); asset_name/asset_size
// carry the matching zip for this machine ("" = no build for this platform).
function assetLabel(rel: CoreRelease): string {
  if (!rel.asset_name) return ''
  return `${rel.asset_name} · ${fmtSize(rel.asset_size)}`
}

let off: (() => void) | null = null
let offDownload: (() => void) | null = null
onMounted(async () => {
  await backend.init()
  await refreshInfo()
  await refreshLogs()
  await refreshSettings()
  await refreshLeases()
  await refreshAccount()
  await refreshMetrics()
  await refreshTLS()
  await refreshCore()
  await refreshQuota()
  off = onEvent<string>(events.coreLog, (line) => {
    logs.value.push(line)
    if (logs.value.length > 200) logs.value = logs.value.slice(-200)
  })
  // Desktop only: download progress events (web mode polls CoreReleases).
  offDownload = onEvent<any>(events.coreDownload, (d) => {
    if (d?.error) {
      coreMsg.value = d.error
      coreProgress.value = null
      return
    }
    coreProgress.value = { tag: d.tag, loaded: d.loaded, total: d.total }
  })
})
onUnmounted(() => {
  off?.()
  offDownload?.()
})
</script>

<template>
  <div class="p-8 space-y-6">
    <div class="flex items-start justify-between">
      <div>
        <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('settings.title') }}</h1>
        <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">{{ t('settings.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-3">
        <span v-if="savedFlash" class="text-xs text-success font-bold">{{ t('settings.save_success') }}</span>
        <button class="btn btn-primary btn-sm" @click="saveSettings">{{ t('settings.save') }}</button>
        <button class="btn btn-secondary btn-sm" @click="refreshLogs">{{ t('peers.refresh') }}</button>
      </div>
    </div>

    <p v-if="settingsError" class="text-sm text-danger">{{ settingsError }}</p>

    <!-- Language & Theme -->
    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">{{ t('app.name') }}</h2>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="label">{{ t('settings.language') }}</label>
          <select v-model="lang" class="input">
            <option value="en">English</option>
            <option value="zh">中文</option>
          </select>
        </div>
        <div>
          <label class="label">{{ t('settings.theme') }}</label>
          <div class="flex gap-2">
            <button class="btn btn-sm" :class="theme === 'light' ? 'btn-primary' : 'btn-secondary'" @click="apply('light')">
              ☀ {{ t('settings.theme_light') }}
            </button>
            <button class="btn btn-sm" :class="theme === 'dark' ? 'btn-primary' : 'btn-secondary'" @click="apply('dark')">
              ☾ {{ t('settings.theme_dark') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Web management: bind address + port + token -->
    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">{{ t('settings.web_management') }}</h2>
      <div class="grid md:grid-cols-2 gap-4">
        <div>
          <label class="label">{{ t('settings.web_bind') }}</label>
          <select v-model="settings.web_bind" class="input">
            <option value="127.0.0.1">127.0.0.1</option>
            <option value="0.0.0.0">0.0.0.0</option>
          </select>
          <p class="text-xs text-surface-600 mt-1 dark:text-surface-500">{{ t('settings.web_bind_hint') }}</p>
        </div>
        <div>
          <label class="label">{{ t('settings.web_port') }}</label>
          <input v-model.number="settings.web_port" type="number" min="0" max="65535" class="input" />
          <p class="text-xs text-surface-600 mt-1 dark:text-surface-500">{{ t('settings.web_port_hint') }}</p>
        </div>
      </div>
      <label class="flex items-start gap-3 cursor-pointer mt-4">
        <input v-model="settings.web_https" type="checkbox" class="accent-accent-500 mt-0.5" />
        <span>
          <span class="text-sm text-surface-700 dark:text-surface-300">{{ t('settings.web_https') }}</span>
          <span class="block text-xs text-surface-600 dark:text-surface-500">{{ t('settings.web_https_hint') }}</span>
        </span>
      </label>
      <div v-if="tlsInfo && tlsInfo.fingerprint" class="mt-3 text-xs space-y-1 border-t border-surface-100 pt-3 dark:border-white/5">
        <div class="flex items-center gap-2 flex-wrap">
          <span class="text-surface-600 dark:text-surface-500">{{ t('settings.tls_fingerprint') }}:</span>
          <code class="font-mono text-surface-700 dark:text-surface-300 break-all">{{ tlsInfo.fingerprint }}</code>
        </div>
        <div class="flex items-center gap-2 flex-wrap">
          <button class="btn btn-secondary btn-sm" @click="rotateTLSCert">{{ t('settings.tls_rotate') }}</button>
          <span class="text-surface-600 dark:text-surface-500">{{ t('settings.tls_rotate_hint') }}</span>
        </div>
      </div>
      <div v-if="webInfo && webInfo.running === 'true'" class="mt-4 space-y-2 text-sm border-t border-surface-100 pt-3 dark:border-white/5">
        <div class="flex items-center justify-between py-1">
          <span class="text-surface-600 dark:text-surface-500">{{ t('settings.address') }}</span>
          <code class="text-surface-700 dark:text-surface-300">http://{{ webInfo.addr }}</code>
        </div>
        <div class="flex items-center justify-between py-1">
          <span class="text-surface-600 dark:text-surface-500">{{ t('settings.auth_token') }}</span>
          <div class="flex items-center gap-2">
            <code class="text-surface-700 font-mono text-xs dark:text-surface-300">{{ webInfo.token }}</code>
            <button class="btn btn-secondary btn-sm" @click="copyText(webInfo.token)">{{ t('common.copy') }}</button>
          </div>
        </div>
        <p class="text-xs text-surface-600 dark:text-surface-500">{{ t('settings.web_hint') }}</p>
      </div>
    </div>

    <!-- General: autostart + peer watchdog alerts -->
    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">{{ t('settings.general_alerts') }}</h2>
      <div class="space-y-4">
        <label class="flex items-start gap-3 cursor-pointer">
          <input v-model="settings.auto_start" type="checkbox" class="accent-accent-500 mt-0.5" />
          <span>
            <span class="text-sm text-surface-700 dark:text-surface-300">{{ t('settings.autostart') }}</span>
            <span class="block text-xs text-surface-600 dark:text-surface-500">{{ t('settings.autostart_hint') }}</span>
          </span>
        </label>
        <label class="flex items-start gap-3 cursor-pointer border-t border-surface-100 pt-3 dark:border-white/5">
          <input v-model="settings.magic_dns" type="checkbox" class="accent-accent-500 mt-0.5" />
          <span>
            <span class="text-sm text-surface-700 dark:text-surface-300">{{ t('settings.magic_dns') }}</span>
            <span class="block text-xs text-surface-600 dark:text-surface-500">{{ t('settings.magic_dns_hint') }}</span>
          </span>
        </label>
        <div class="border-t border-surface-100 pt-3 dark:border-white/5">
          <label class="flex items-start gap-3 cursor-pointer">
            <input v-model="settings.alerts.enabled" type="checkbox" class="accent-accent-500 mt-0.5" />
            <span>
              <span class="text-sm text-surface-700 dark:text-surface-300">{{ t('settings.alerts_enable') }}</span>
              <span class="block text-xs text-surface-600 dark:text-surface-500">{{ t('settings.alerts_hint') }}</span>
            </span>
          </label>
          <div v-if="settings.alerts.enabled" class="mt-3 ml-7 space-y-3">
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300">
              <input v-model="settings.alerts.offline_notify" type="checkbox" class="accent-accent-500" />
              {{ t('settings.alerts_offline') }}
            </label>
            <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300">
              <input v-model="settings.alerts.high_latency_notify" type="checkbox" class="accent-accent-500" />
              {{ t('settings.alerts_latency') }}
              <input v-model.number="settings.alerts.high_latency_ms" type="number" min="1" max="60000" class="input w-28 py-1" />
              ms
            </label>
            <div>
              <label class="label">{{ t('settings.alerts_webhook') }}</label>
              <input v-model="settings.alerts.webhook_url" class="input font-mono text-xs" placeholder="https://hook.example.com/easytier" />
              <p class="text-xs text-surface-600 mt-1 dark:text-surface-500">{{ t('settings.alerts_webhook_hint') }}</p>
            </div>
          </div>
          <div class="flex items-center gap-3 mt-3">
            <button class="btn btn-secondary btn-sm" @click="doNotifyTest">{{ t('settings.notify_test') }}</button>
            <span v-if="notifyMsg" class="text-xs text-surface-600 dark:text-surface-400 truncate">{{ notifyMsg }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Fleet agent: connect to a management hub (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showFleet = !showFleet">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.fleet_title') }}</h2>
        <span class="text-xs text-surface-500">{{ showFleet ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showFleet" class="mt-3 space-y-3">
        <div v-if="!settings.auto_start" class="px-3 py-2 border-2 border-[var(--warning)] text-xs text-surface-800 dark:text-surface-200">
          ⚠ {{ t('settings.autostart_warn') }}
        </div>
        <label class="flex items-start gap-3 cursor-pointer">
          <input v-model="settings.fleet.enabled" type="checkbox" class="accent-accent-500 mt-0.5" />
          <span>
            <span class="text-sm text-surface-700 dark:text-surface-300">{{ t('settings.fleet_enable') }}</span>
            <span class="block text-xs text-surface-600 dark:text-surface-500">{{ t('settings.fleet_hint') }}</span>
          </span>
        </label>
        <div v-if="settings.fleet.enabled" class="grid md:grid-cols-2 gap-4 ml-7">
          <div>
            <label class="label">{{ t('settings.fleet_url') }}</label>
            <input v-model="settings.fleet.server_url" class="input font-mono text-xs" placeholder="http://hub:56000" />
          </div>
          <div>
            <label class="label">{{ t('settings.fleet_name') }}</label>
            <input v-model="settings.fleet.name" class="input" :placeholder="t('settings.fleet_name_hint')" />
          </div>
          <div class="md:col-span-2 border-t border-surface-100 pt-3 dark:border-white/5">
            <label class="label">{{ t('settings.fleet_hub_account') }}</label>
            <p class="text-xs text-surface-600 mb-2 dark:text-surface-500">{{ t('settings.fleet_hub_hint') }}</p>
            <div class="grid md:grid-cols-2 gap-4">
              <input v-model="settings.fleet.hub_user" class="input font-mono text-xs" :placeholder="t('login.username')" autocomplete="off" />
              <input v-model="settings.fleet.hub_pass" type="password" class="input font-mono text-xs" :placeholder="t('login.password')" autocomplete="new-password" />
            </div>
            <p class="text-xs text-surface-500 mt-2">{{ t('settings.fleet_token') }}: {{ settings.fleet.token || '—' }}</p>
            <p v-if="settings.fleet.cert_pin" class="text-xs text-surface-500 mt-1 flex items-center gap-2 flex-wrap">
              {{ t('settings.fleet_pinned') }}: <code class="font-mono">{{ settings.fleet.cert_pin.slice(0, 32) }}…</code>
              <button class="btn btn-ghost btn-sm" @click="doClearFleetPin">{{ t('settings.fleet_clear_pin') }}</button>
            </p>
          </div>
        </div>
      </div>
    </div>

    <!-- Web account security -->
    <div class="card p-5">
      <div class="flex items-center justify-between mb-1 flex-wrap gap-2">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.account') }}</h2>
        <span class="badge text-[10px]" :class="hasAccount ? 'badge-green' : 'badge-yellow'">
          {{ hasAccount ? t('settings.account_on') : t('settings.account_off') }}
        </span>
      </div>
      <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('settings.account_hint') }}</p>
      <div class="grid md:grid-cols-3 gap-4">
        <div>
          <label class="label">{{ t('login.username') }}</label>
          <input v-model="accUsername" class="input" :disabled="hasAccount && !isNative" />
        </div>
        <div v-if="hasAccount">
          <label class="label">{{ t('settings.account_current') }}</label>
          <input v-model="accCurrent" type="password" class="input" autocomplete="current-password" />
        </div>
        <div>
          <label class="label">{{ hasAccount ? t('settings.account_new') : t('login.password') }}</label>
          <input v-model="accNew" type="password" class="input" autocomplete="new-password" />
        </div>
        <div>
          <label class="label">{{ t('settings.account_confirm') }}</label>
          <input v-model="accConfirm" type="password" class="input" autocomplete="new-password" @keyup.enter="saveAccount" />
        </div>
      </div>
      <div class="flex items-center gap-3 mt-3 flex-wrap">
        <button class="btn btn-primary btn-sm" :disabled="accBusy || !accNew" @click="saveAccount">
          {{ hasAccount ? t('settings.account_change') : t('settings.account_create') }}
        </button>
        <button class="btn btn-secondary btn-sm" @click="doRotateToken">{{ t('settings.account_rotate') }}</button>
        <button v-if="!isNative && auth.user" class="btn btn-secondary btn-sm" @click="doLogout">{{ t('settings.account_logout') }}</button>
        <span v-if="accMsg" class="text-xs text-success font-bold">{{ accMsg }}</span>
        <span v-if="accErr" class="text-xs text-danger">{{ accErr }}</span>
      </div>
      <p v-if="rotatedToken" class="text-xs mt-2 font-mono break-all text-surface-700 dark:text-surface-300">
        {{ t('settings.account_new_token') }}: <code>{{ rotatedToken }}</code>
      </p>
    </div>

    <!-- Virtual IP memory (sticky DHCP) -->
    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-1 dark:text-surface-300">{{ t('settings.leases') }}</h2>
      <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('settings.leases_hint') }}</p>
      <div v-if="Object.keys(leases).length" class="space-y-1">
        <div v-for="(l, net) in leases" :key="net"
             class="flex items-center gap-3 text-sm py-1.5 border-b border-surface-100 last:border-0 dark:border-white/5">
          <span class="w-40 truncate font-medium text-surface-700 dark:text-surface-300">{{ net }}</span>
          <code class="font-mono text-xs text-accent-600 dark:text-accent-400">{{ l.ip }}/{{ l.prefix }}</code>
          <span class="text-xs text-surface-600 dark:text-surface-500 truncate">{{ l.hostname }}</span>
          <span class="ml-auto text-xs text-surface-500 dark:text-surface-500 shrink-0">{{ l.updated?.replace('T', ' ').slice(0, 16) }}</span>
          <button class="btn btn-secondary btn-sm shrink-0" :disabled="leaseForgetting === net" @click="forgetLease(net)">
            {{ t('settings.leases_forget') }}
          </button>
        </div>
      </div>
      <p v-else class="text-xs text-surface-600 dark:text-surface-500">{{ t('settings.leases_empty') }}</p>
    </div>

    <!-- Prometheus metrics (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showMetrics = !showMetrics">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.metrics') }}</h2>
        <span class="text-xs text-surface-500">{{ showMetrics ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showMetrics" class="mt-3">
        <div class="flex items-center justify-between mb-3">
          <p class="text-xs text-surface-600 dark:text-surface-500">{{ t('settings.metrics_hint') }}</p>
          <button class="btn btn-secondary btn-sm" @click="refreshMetrics">{{ t('settings.audit_refresh') }}</button>
        </div>
        <div class="bg-surface-100 border border-surface-200 rounded-lg p-3 dark:bg-surface-950/60 dark:border-white/5">
          <pre class="h-56 overflow-y-auto font-mono text-xs text-surface-800 whitespace-pre-wrap break-words dark:text-surface-400">{{ metricsText || t('settings.metrics_empty') }}</pre>
        </div>
      </div>
    </div>

    <!-- Storage paths -->
    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">{{ t('settings.paths') }}</h2>
      <div class="space-y-3">
        <div>
          <label class="label">{{ t('settings.env_labels.config_dir') }}</label>
          <div class="flex gap-2">
            <input v-model="settings.config_dir" class="input flex-1 font-mono text-xs" placeholder="%APPDATA%\easytier-pro-gui\configs" />
            <button class="btn btn-secondary btn-sm shrink-0" :disabled="!settings.config_dir" @click="openDir(settings.config_dir)">{{ t('settings.open_dir') }}</button>
          </div>
        </div>
        <div>
          <label class="label">{{ t('settings.env_labels.log_dir') }}</label>
          <div class="flex gap-2">
            <input v-model="settings.log_dir" class="input flex-1 font-mono text-xs" placeholder="%APPDATA%\easytier-pro-gui\logs" />
            <button class="btn btn-secondary btn-sm shrink-0" :disabled="!settings.log_dir" @click="openDir(settings.log_dir)">{{ t('settings.open_dir') }}</button>
          </div>
        </div>
        <div>
          <label class="label">{{ t('settings.env_labels.app_data') }}</label>
          <div class="flex gap-2">
            <input :value="appInfo?.app_data || ''" readonly class="input flex-1 font-mono text-xs" />
            <button class="btn btn-secondary btn-sm shrink-0" :disabled="!appInfo?.app_data" @click="openDir(appInfo!.app_data)">{{ t('settings.open_dir') }}</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Monthly traffic quota (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showQuota = !showQuota">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('quota_title') }}</h2>
        <span class="text-xs text-surface-500">{{ showQuota ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showQuota" class="mt-3">
        <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('quota_hint') }}</p>
        <div class="grid md:grid-cols-3 gap-4">
          <label class="flex items-center gap-2 text-sm">
            <input v-model="settings.quota.enabled" type="checkbox" class="accent-accent-500" />
            {{ t('quota_enabled') }}
          </label>
          <div>
            <label class="label">{{ t('quota_gb') }}</label>
            <input v-model.number="settings.quota.monthly_gb" type="number" min="0" step="1" class="input font-mono text-xs" />
          </div>
          <div>
            <label class="label">{{ t('quota_warn') }}</label>
            <input v-model.number="settings.quota.warn_percent" type="number" min="1" max="99" class="input font-mono text-xs" />
          </div>
        </div>
        <div v-if="quotaUsed" class="mt-3 text-xs text-surface-600 dark:text-surface-500">
          {{ t('traffic_today') }}: ↓ {{ fmtTraffic(quotaUsed.today_rx) }} ↑ {{ fmtTraffic(quotaUsed.today_tx) }}
          · {{ t('traffic_week') }}: ↓ {{ fmtTraffic(quotaUsed.week_rx) }} ↑ {{ fmtTraffic(quotaUsed.week_tx) }}
        </div>
      </div>
    </div>

    <!-- Application log (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showAppLog = !showAppLog">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('log_title') }}</h2>
        <span class="text-xs text-surface-500">{{ showAppLog ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showAppLog" class="mt-3">
        <div class="flex items-end gap-4 mb-3 flex-wrap">
          <div>
            <label class="label">{{ t('log_level') }}</label>
            <select v-model="settings.app_log_level" class="input !w-40 text-xs font-mono">
              <option value="trace">trace</option>
              <option value="debug">debug</option>
              <option value="info">info</option>
              <option value="warn">warn</option>
              <option value="error">error</option>
            </select>
          </div>
          <button class="btn btn-secondary btn-sm" :disabled="appLogLoading" @click="refreshAppLog">{{ t('log_view') }}</button>
          <p class="text-xs text-surface-600 flex-1 dark:text-surface-500">{{ t('log_hint') }}</p>
        </div>
        <pre v-if="appLogLines.length" class="border-2 border-[var(--ink)] bg-[var(--bg)] p-3 text-[10px] font-mono max-h-72 overflow-auto whitespace-pre-wrap">{{ appLogLines.join(String.fromCharCode(10)) }}</pre>
        <p v-else class="text-xs text-surface-500">{{ t('log_empty') }}</p>
      </div>
    </div>

    <!-- Custom CSS (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showCSS = !showCSS">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.custom_css') }}</h2>
        <span class="text-xs text-surface-500">{{ showCSS ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showCSS" class="mt-3">
        <div class="flex items-center gap-2 mb-3 flex-wrap">
          <p class="text-xs text-surface-600 dark:text-surface-500 flex-1">{{ t('settings.custom_css_hint') }}</p>
          <button class="btn btn-secondary btn-sm shrink-0" @click="fillExample">{{ t('settings.css_fill') }}</button>
          <button class="btn btn-secondary btn-sm shrink-0" @click="showSpec = !showSpec">
            {{ t('settings.css_spec') }} {{ showSpec ? '▾' : '▸' }}
          </button>
        </div>
        <textarea v-model="settings.custom_css" rows="6" class="input w-full font-mono text-xs" placeholder=".card { border-width: 1px; }" />
        <div v-if="showSpec" class="mt-3 border-2 border-[var(--ink)] bg-[var(--bg)] p-3 text-xs font-mono text-surface-700 dark:text-surface-300 whitespace-pre overflow-x-auto max-h-80 overflow-y-auto">
{{ cssExample }}
        </div>
      </div>
    </div>

    <!-- Core version management (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showCore = !showCore">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">
          {{ t('settings.core_title') }}
          <span class="ml-2 badge badge-gray text-[10px]">{{ coreState?.active ? coreState.active : t('settings.core_bundled') + ' ' + (coreState?.bundled || '…') }}</span>
        </h2>
        <span class="text-xs text-surface-500">{{ showCore ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showCore" class="mt-3">
        <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('settings.core_hint') }}</p>
        <p v-if="coreMsg" class="text-xs text-danger mb-3 font-mono">{{ coreMsg }}</p>

        <!-- Mirror prefix -->
        <div class="mb-4">
          <label class="label">{{ t('settings.core_mirror_label') }}</label>
          <input v-model="settings.core_mirror" class="input font-mono text-xs" placeholder="https://ghproxy.example.com/" />
          <p class="text-[10px] text-surface-500 mt-1">{{ t('settings.core_mirror_hint') }}</p>
        </div>

        <!-- Releases -->
        <div class="flex items-center justify-between mb-2">
          <span class="text-xs font-bold uppercase tracking-wide text-surface-600 dark:text-surface-400">{{ t('settings.core_refresh') }}</span>
          <button class="btn btn-secondary btn-sm" :disabled="coreBusy" @click="refreshCore">⟳ {{ t('settings.core_refresh') }}</button>
        </div>
        <p v-if="coreState && !coreState.releases.length && !coreState.error" class="text-xs text-surface-500 mb-2">{{ t('settings.core_none') }}</p>
        <p v-else-if="coreState?.error" class="text-xs text-danger mb-2 font-mono">{{ coreState.error }}</p>
        <div v-if="coreState?.releases.length" class="border-2 border-[var(--ink)] divide-y-2 divide-[var(--ink)]/20 max-h-72 overflow-y-auto">
          <div v-for="rel in coreState.releases" :key="rel.tag" class="flex items-center gap-2 px-3 py-2 flex-wrap">
            <span class="font-bold text-xs">{{ rel.tag }}</span>
            <span v-if="rel.tag === coreState.active" class="badge badge-green text-[10px]">{{ t('settings.core_active') }}</span>
            <span v-if="rel.tag === 'v' + (coreState?.bundled || '').split('-')[0]" class="badge badge-gray text-[10px]">{{ t('settings.core_bundled') }}</span>
            <span class="text-[10px] text-surface-500 font-mono truncate flex-1 min-w-40" :title="assetLabel(rel)">{{ assetLabel(rel) }}</span>
            <span v-if="coreProgress && coreProgress.tag === rel.tag && coreProgress.total" class="text-[10px] font-mono">
              {{ t('settings.core_downloading') }} {{ (coreProgress.loaded / 1048576).toFixed(1) }}/{{ (coreProgress.total / 1048576).toFixed(1) }} MB
            </span>
            <span v-else-if="coreBusy && coreProgress?.tag === rel.tag" class="text-[10px] font-mono">{{ t('settings.core_downloading') }}…</span>
            <button
              v-if="rel.tag !== coreState.active"
              class="btn btn-primary btn-sm shrink-0"
              :disabled="coreBusy || !rel.asset_name"
              @click="coreInstall(rel.tag)"
            >
              {{ coreBusy && coreProgress?.tag === rel.tag ? t('settings.core_installing') : t('settings.core_install') }}
            </button>
          </div>
        </div>

        <!-- Installed versions -->
        <div v-if="coreState?.installed.length" class="mt-4">
          <div class="text-xs font-bold uppercase tracking-wide text-surface-600 dark:text-surface-400 mb-2">{{ t('settings.core_installed_versions') }}</div>
          <div class="border-2 border-[var(--ink)] divide-y-2 divide-[var(--ink)]/20">
            <div v-for="inst in coreState.installed" :key="inst.tag" class="flex items-center gap-2 px-3 py-2 flex-wrap">
              <span class="font-bold text-xs">{{ inst.tag }}</span>
              <span v-if="inst.active" class="badge badge-green text-[10px]">{{ t('settings.core_active') }}</span>
              <span class="text-[10px] text-surface-500 font-mono truncate flex-1 min-w-40">{{ inst.core }}</span>
              <button v-if="!inst.active" class="btn btn-secondary btn-sm" :disabled="coreBusy" @click="coreSwitch(inst.tag)">{{ t('settings.core_switch') }}</button>
              <button v-if="!inst.active" class="btn btn-danger btn-sm" :disabled="coreBusy" @click="coreDelete(inst.tag)">{{ t('settings.core_delete') }}</button>
            </div>
          </div>
        </div>
        <p v-else class="mt-3 text-xs text-surface-500">{{ t('settings.core_no_installed') }}</p>

        <!-- Revert to bundled -->
        <button v-if="coreState?.active" class="btn btn-secondary btn-sm mt-4" :disabled="coreBusy" @click="coreSwitch('')">
          {{ t('settings.core_switch_bundled') }}
        </button>
      </div>
    </div>

    <!-- WebDAV cloud sync (collapsed by default) -->
    <div class="card p-5">
      <button class="w-full flex items-center justify-between" @click="showWebdav = !showWebdav">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.webdav') }}</h2>
        <span class="text-xs text-surface-500">{{ showWebdav ? '▾' : '▸' }}</span>
      </button>
      <div v-if="showWebdav" class="mt-3">
      <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('settings.webdav_note') }}</p>
      <div class="grid md:grid-cols-3 gap-4">
        <div class="md:col-span-3">
          <label class="label">{{ t('settings.webdav_url') }}</label>
          <input v-model="settings.webdav.server_url" class="input font-mono text-xs" placeholder="https://dav.example.com/remote.php/dav/files/user/easytier-pro" />
        </div>
        <div>
          <label class="label">{{ t('settings.webdav_user') }}</label>
          <input v-model="settings.webdav.username" class="input font-mono text-xs" />
        </div>
        <div>
          <label class="label">{{ t('settings.webdav_pass') }}</label>
          <input v-model="settings.webdav.password" type="password" class="input font-mono text-xs" />
        </div>
        <div>
          <label class="label">{{ t('settings.webdav_last_sync') }}</label>
          <div class="text-sm font-mono pt-2 text-surface-700 dark:text-surface-300 truncate">{{ settings.webdav.last_sync || '—' }}</div>
        </div>
      </div>
      <div class="mt-4 space-y-2">
        <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300">
          <input v-model="settings.webdav.sync_accounts" type="checkbox" class="accent-accent-500" />
          {{ t('settings.webdav_sync_accounts') }}
          <span class="text-xs text-surface-600 dark:text-surface-500">{{ t('settings.webdav_sync_accounts_hint') }}</span>
        </label>
        <label class="flex items-center gap-2 text-sm text-surface-700 dark:text-surface-300">
          <input v-model="settings.webdav.auto_sync" type="checkbox" class="accent-accent-500" />
          {{ t('settings.webdav_auto') }}
          <span class="text-xs text-surface-600 dark:text-surface-500">{{ t('settings.webdav_auto_hint') }}</span>
        </label>
      </div>
      <div class="flex items-center gap-3 mt-4">
        <button class="btn btn-primary btn-sm" :disabled="webdavBusy" @click="doWebdavPush">{{ t('settings.webdav_push') }}</button>
        <button class="btn btn-secondary btn-sm" :disabled="webdavBusy" @click="doWebdavPull">{{ t('settings.webdav_pull') }}</button>
        <span v-if="webdavMsg" class="text-xs text-surface-600 dark:text-surface-400 truncate">{{ webdavMsg }}</span>
      </div>
      </div>
    </div>

    <div class="card p-5">
      <h2 class="text-sm font-medium text-surface-700 text-surface-600 mb-3 dark:text-surface-300">{{ t('settings.relay') }}</h2>
      <div class="flex items-center gap-2 text-sm">
        <span class="text-surface-600 dark:text-surface-500">{{ t('settings.relay') }}:</span>
        <code class="text-surface-700 font-mono dark:text-surface-300">wss://ez.cloud.c01.kr</code>
        <button class="btn btn-secondary btn-sm ml-auto" @click="copyText('wss://ez.cloud.c01.kr')">{{ t('common.copy') }}</button>
      </div>
      <p class="text-xs text-surface-600 mt-2 dark:text-surface-500">{{ t('settings.relay_desc') }}</p>
    </div>

    <div v-if="appInfo" class="card p-5">
      <div class="flex items-center gap-3 mb-4">
        <div class="w-9 h-9 rounded-lg bg-accent-500/10 flex items-center justify-center text-lg">🖥️</div>
        <div>
          <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.environment') }}</h2>
          <p class="text-xs text-surface-600 dark:text-surface-500 mt-0.5">{{ version ? `EasyTier ${version.core}` : '' }}</p>
        </div>
      </div>
      <div class="grid md:grid-cols-2 gap-x-8 gap-y-0 text-sm">
        <div v-for="(v, k) in appInfo" :key="k"
             class="group flex items-center gap-2 py-2 border-b border-surface-100 dark:border-white/5">
          <span class="flex-1 min-w-0 text-surface-500 dark:text-surface-400">
            {{ t(`settings.env_labels.${k}`, k) }}
          </span>
          <code class="font-mono text-xs text-surface-800 dark:text-surface-200 truncate max-w-[16rem]">{{ v }}</code>
          <button class="opacity-0 group-hover:opacity-100 transition-opacity text-surface-400 hover:text-accent-500"
                  title="copy" @click="copyText(v)">⧉</button>
        </div>
      </div>
    </div>

    <div v-if="isNative" class="card p-5">
      <div class="flex items-center justify-between mb-3">
        <h2 class="text-sm font-medium text-surface-700 text-surface-600 dark:text-surface-300">{{ t('settings.core_log') }}</h2>
        <span class="text-xs text-surface-600 dark:text-surface-500">status: {{ status }}</span>
      </div>
      <pre class="h-72 overflow-y-auto font-mono text-xs text-surface-800 bg-surface-100 border border-surface-200 rounded-lg p-3 whitespace-pre-wrap break-words dark:text-surface-400 dark:bg-surface-950/60 dark:border-white/5">
{{ logs.join('\n') }}
      </pre>
    </div>
  </div>
</template>
