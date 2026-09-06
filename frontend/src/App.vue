<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useCore, statusLabel } from '@/composables/useCore'
import { useTheme } from '@/composables/useTheme'
import { setLocale } from '@/lib/i18n'
import { backend, auth, isNative } from '@/lib/backend'
import LoginView from '@/views/Login.vue'

const router = useRouter()
const route = useRoute()
const { status, busy, error, start, stop, restart, clearError, subscribe } = useCore()
const { theme, toggle } = useTheme()
const { t, locale } = useI18n()

const lang = computed({
  get: () => locale.value,
  set: (v: string) => setLocale(v as 'en' | 'zh'),
})

// Desktop app is locally trusted (always admin); browser roles gate
// management pages. The header core controls hit admin-only endpoints.
const canManageAll = computed(() => !auth.required || isNative || auth.role === 'admin')

// Sidebar is grouped: observation / mesh / services / administration.
const navGroups = computed(() => [
  {
    label: t('app.nav_overview'),
    items: [
      { path: '/', label: t('app.dashboard'), icon: '⊞' },
    ],
  },
  {
    label: t('app.nav_mesh'),
    items: [
      { path: '/networks', label: t('app.networks'), icon: '◈' },
      { path: '/peers', label: t('app.peers'), icon: '◉' },
    ],
  },
  {
    label: t('app.nav_services'),
    items: [
      ...(canManageAll.value ? [{ path: '/tunnels', label: t('app.tunnels'), icon: '⇋' }] : []),
      ...(canManageAll.value ? [{ path: '/devices', label: t('app.devices'), icon: '⌂' }] : []),
    ],
  },
  {
    label: t('app.nav_manage'),
    items: [
      ...(canManageAll.value ? [{ path: '/users', label: t('app.users'), icon: '☰' }] : []),
      { path: '/settings', label: t('app.settings'), icon: '⚙' },
    ],
  },
])

const nav = computed(() => navGroups.value.flatMap((g) => g.items))

const coreVersion = ref('')
const admin = ref(true)
const relaunching = ref(false)
const statusDot = computed(() => ({
  'bg-success': status.value === 'running',
  'bg-warning': status.value === 'starting',
  'bg-danger': status.value === 'error',
  'bg-surface-500': status.value === 'stopped',
}))

async function relaunchAdmin() {
  relaunching.value = true
  try {
    await backend.relaunchAsAdmin()
  } catch {
    relaunching.value = false
  }
}

let unsub: (() => void) | null = null

// Load UI-facing data once a session (native trust or web login) exists.
async function bootstrapData() {
  try {
    const v = await backend.versions()
    coreVersion.value = v.core || ''
  } catch { /* versions unavailable */ }
  // Apply persisted custom CSS globally.
  try {
    const s = await backend.getSettings()
    if (s.custom_css) {
      let el = document.getElementById('et-custom-css') as HTMLStyleElement | null
      if (!el) {
        el = document.createElement('style')
        el.id = 'et-custom-css'
        document.head.appendChild(el)
      }
      el.textContent = s.custom_css
    }
  } catch { /* settings unavailable */ }
  // Warn when running without admin rights (TUN adapter cannot be created).
  try {
    admin.value = await backend.isAdmin()
  } catch { /* admin check unavailable */ }
}

const authed = computed(() => !auth.required || !!auth.user)

onMounted(async () => {
  await backend.init()
  unsub = subscribe()
  if (authed.value) await bootstrapData()
  watch(authed, (v) => { if (v) bootstrapData() })
})
onUnmounted(() => { unsub?.() })
</script>

<template>
  <!-- Web management login gate: nothing of the app renders until authed -->
  <LoginView v-if="auth.checked && auth.required && !auth.user" />

  <div v-else class="flex flex-col h-screen">
    <!-- Global header: status + core controls + theme/lang, visible on every page -->
    <header class="shrink-0 flex items-center gap-4 px-5 h-14 bg-[var(--panel)] text-[var(--panel-fg)] border-b-[3px] border-[var(--panel-fg)]/25">
      <div class="flex items-center gap-3 min-w-0">
        <div class="w-8 h-8 shrink-0 bg-[var(--accent)] flex items-center justify-center text-sm font-bold text-white border-2 border-[var(--panel-fg)]">E</div>
        <span class="text-sm font-bold uppercase tracking-wide whitespace-nowrap">{{ t('app.name') }}</span>
      </div>

      <div class="flex-1" />

      <!-- core status badge -->
      <div class="flex items-center gap-2 px-3 py-1.5 border-2 border-[var(--panel-fg)]/40 bg-[var(--panel-fg)]/10">
        <div class="w-2.5 h-2.5" :class="statusDot" />
        <span class="text-xs font-bold uppercase tracking-wider">{{ statusLabel[status] }}</span>
      </div>

      <!-- core controls (global; admin-only endpoints — hidden for other roles) -->
      <template v-if="canManageAll">
        <template v-if="status === 'running'">
          <button class="px-3 py-1.5 text-xs font-bold uppercase tracking-wide border-2 border-[var(--panel-fg)]/50 bg-transparent text-[var(--panel-fg)] hover:bg-[var(--panel-fg)] hover:text-[var(--panel)] transition-colors" :disabled="busy" @click="restart">
            {{ t('core.restart') }}
          </button>
          <button class="px-3 py-1.5 text-xs font-bold uppercase tracking-wide border-2 border-[var(--panel-fg)]/50 bg-[var(--danger)] text-white hover:bg-[var(--panel-fg)] hover:text-[var(--panel)] transition-colors" :disabled="busy" @click="stop">
            {{ t('core.stop') }}
          </button>
        </template>
        <button v-else class="px-4 py-1.5 text-xs font-bold uppercase tracking-wide border-2 border-[var(--panel-fg)]/50 bg-[var(--accent)] text-white hover:bg-[var(--panel-fg)] hover:text-[var(--panel)] transition-colors" :disabled="busy" @click="start">
          {{ t('core.start') }}
        </button>
      </template>

      <!-- theme + language quick toggles -->
      <button class="px-2.5 py-1.5 text-xs font-bold border-2 border-[var(--panel-fg)]/40 text-[var(--panel-fg)] hover:bg-[var(--panel-fg)] hover:text-[var(--panel)] transition-colors" :title="theme === 'dark' ? 'Light' : 'Dark'" @click="toggle">
        {{ theme === 'dark' ? '☀' : '☾' }}
      </button>
      <button class="px-2.5 py-1.5 text-xs font-bold border-2 border-[var(--panel-fg)]/40 text-[var(--panel-fg)] hover:bg-[var(--panel-fg)] hover:text-[var(--panel)] transition-colors" @click="lang = lang === 'en' ? 'zh' : 'en'">
        {{ lang === 'en' ? '中文' : 'EN' }}
      </button>
    </header>

    <!-- global error banner -->
    <div v-if="error" class="shrink-0 flex items-center gap-3 px-5 py-2 border-b-[3px] border-[var(--ink)] bg-[var(--danger)] text-white">
      <span class="flex-1 text-xs font-bold uppercase tracking-wide truncate">{{ error }}</span>
      <button class="px-2 text-sm font-bold border-2 border-white/70 hover:bg-white hover:text-[var(--danger)]" @click="clearError">×</button>
    </div>

    <!-- non-admin warning: TUN adapter cannot be created -->
    <div v-if="!admin" class="shrink-0 flex items-center gap-3 px-5 py-2 border-b-[3px] border-[var(--ink)] bg-[var(--warning)] text-black">
      <span class="flex-1 text-xs font-bold uppercase tracking-wide truncate">{{ t('admin.need_admin') }}</span>
      <button class="px-3 py-1 text-xs font-bold uppercase border-2 border-black hover:bg-black hover:text-[var(--warning)]" :disabled="relaunching" @click="relaunchAdmin">
        {{ relaunching ? '…' : t('admin.relaunch') }}
      </button>
    </div>

    <div class="flex flex-1 min-h-0 max-md:flex-col">
      <!-- Sidebar: navigation only. On phones it becomes a bottom bar. -->
      <nav class="w-48 shrink-0 flex flex-col bg-[var(--panel)] text-[var(--panel-fg)] border-r-[3px] border-[var(--panel-fg)]/25 max-md:w-full max-md:h-14 max-md:flex-row max-md:items-center max-md:border-r-0 max-md:border-t-[3px] max-md:fixed max-md:bottom-0 max-md:left-0 max-md:right-0 max-md:z-30">
        <div class="flex-1 px-3 mt-4 space-y-3 max-md:mt-0 max-md:flex max-md:items-center max-md:gap-1 max-md:px-2 max-md:space-y-0 max-md:overflow-x-auto">
          <div v-for="group in navGroups" :key="group.label" class="max-md:flex max-md:items-center max-md:gap-1">
            <p class="px-3 pb-1 text-[9px] font-bold uppercase tracking-widest opacity-50 max-md:hidden">{{ group.label }}</p>
            <div class="space-y-1 max-md:flex max-md:items-center max-md:gap-1 max-md:space-y-0">
              <button
                v-for="item in group.items"
                :key="item.path"
                @click="router.push(item.path)"
                class="w-full flex items-center gap-3 px-3 py-2 text-sm font-bold uppercase tracking-wide transition-colors text-left border-2 max-md:w-auto max-md:py-1.5 max-md:px-2.5 max-md:text-[10px] max-md:whitespace-nowrap"
                :class="route.path === item.path
                  ? 'bg-[var(--accent)] text-white border-[var(--panel-fg)]'
                  : 'bg-transparent text-[var(--panel-fg)]/80 border-transparent hover:bg-[var(--accent)] hover:text-white hover:border-[var(--panel-fg)]'"
              >
                <span class="w-5 text-center">{{ item.icon }}</span>
                <span class="max-md:hidden">{{ item.label }}</span>
              </button>
            </div>
          </div>
        </div>
        <div class="px-4 py-3 border-t-[3px] border-[var(--panel-fg)]/25 max-md:hidden">
          <p class="text-[10px] font-bold uppercase tracking-widest opacity-60 truncate">v{{ coreVersion || '…' }}</p>
        </div>
      </nav>

      <!-- Main -->
      <main class="flex-1 overflow-y-auto max-md:pb-16">
        <router-view />
      </main>
    </div>
  </div>
</template>
