<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useConfigs } from '@/composables/useConfigs'
import { useCore } from '@/composables/useCore'
import ConfigEditorDialog from '@/components/ConfigEditorDialog.vue'
import { buildShareLink, parseImport } from '@/lib/share'
import { auth, isNative, type ConfigFile } from '@/lib/backend'

const { t } = useI18n()
const { configs, loading, refresh, save: saveConfig, setEnabled, remove, applyAll } = useConfigs()
const { status: coreStatus, refreshStatus } = useCore()

const canManageAll = computed(() => isNative || auth.role === 'admin')
// operator: may operate only its granted networks (empty grant = all).
function mayOperate(cfg: ConfigFile): boolean {
  if (canManageAll.value) return true
  if (auth.role !== 'operator') return false
  if (!auth.networks.length) return true
  return auth.networks.includes(cfg.network)
}
// viewer (and ungranted operators) get a read-only list: no edit/toggle/delete/import.
const canCreate = computed(() => canManageAll.value || auth.role === 'operator')

const showEditor = ref(false)
const editing = ref<ConfigFile | null>(null)

const showImport = ref(false)
const importText = ref('')
const importError = ref('')
const importBusy = ref(false)

function openNew() {
  editing.value = null
  showEditor.value = true
}

function openEdit(cfg: ConfigFile) {
  editing.value = cfg
  showEditor.value = true
}

async function applyChanges() {
  // Persist state and restart core so enabled configs take effect.
  const enabled = configs.value.filter((c) => c.enabled).map((c) => c.instance_id)
  await applyAll(enabled)
  await refreshStatus()
}

async function onSave(cfg: ConfigFile) {
  await saveConfig(cfg)
  showEditor.value = false
  await refresh()
  await applyChanges()
}

async function toggle(cfg: ConfigFile) {
  await setEnabled(cfg.instance_id, !cfg.enabled)
  await refresh()
  await applyChanges()
}

async function del(cfg: ConfigFile) {
  if (confirm(t('networks.delete_confirm', { name: cfg.network || cfg.instance_id }))) {
    await remove(cfg.instance_id)
    await refresh()
    await applyChanges()
  }
}

async function copyShare(cfg: ConfigFile) {
  const link = buildShareLink(cfg)
  try {
    await navigator.clipboard.writeText(link)
  } catch (e) {
    console.error('clipboard copy failed', e)
  }
}

async function doImport() {
  if (importBusy.value) return
  importError.value = ''
  const raw = parseImport(importText.value, configs.value.map((c) => c.raw))
  if (!raw) {
    importError.value = t('networks.import_invalid')
    return
  }
  importBusy.value = true
  try {
    await saveConfig({ instance_id: '', instance_name: '', network: '', ipv4: '', enabled: true, path: '', raw })
    showImport.value = false
    importText.value = ''
    await applyChanges()
  } catch (e) {
    importError.value = String(e)
  } finally {
    importBusy.value = false
  }
}

let timer: ReturnType<typeof setInterval> | null = null
onMounted(() => {
  refresh()
  timer = setInterval(refresh, 5000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div class="p-8 space-y-6">
    <div class="flex items-start justify-between">
      <div>
        <h1 class="text-xl font-semibold dark:text-white text-surface-900">{{ t('networks.title') }}</h1>
        <p class="text-sm text-surface-600 mt-1 dark:text-surface-500">{{ t('networks.subtitle') }}</p>
      </div>
      <div class="flex items-center gap-2">
        <button v-if="canCreate" class="btn btn-secondary btn-sm" @click="showImport = true">{{ t('networks.import') }}</button>
        <button v-if="canCreate" class="btn btn-primary btn-sm" @click="openNew">{{ t('networks.new') }}</button>
      </div>
    </div>

    <div v-if="loading" class="text-surface-600 text-sm dark:text-surface-400">{{ t('common.loading') }}</div>

    <div v-else-if="configs.length === 0" class="card p-8 text-center text-surface-600 dark:text-surface-400">
      <p class="text-4xl mb-3">◈</p>
      <p class="text-sm">{{ t('networks.empty') }}</p>
    </div>

    <div v-else class="space-y-3">
      <div v-for="cfg in configs" :key="cfg.instance_id"
           class="card p-4 flex items-center gap-4 card-hover">
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <span class="badge" :class="cfg.enabled ? 'badge-green' : 'badge-gray'">
              {{ cfg.enabled ? t('networks.enabled') : t('networks.disabled') }}
            </span>
            <span class="font-medium dark:text-white text-surface-900 truncate">{{ cfg.network || cfg.instance_name || cfg.instance_id }}</span>
          </div>
          <div class="text-xs text-surface-600 mt-1 truncate dark:text-surface-500">
            {{ cfg.ipv4 ? `${cfg.ipv4} · ` : '' }}{{ cfg.instance_id }}
          </div>
        </div>
        <button class="btn btn-ghost btn-sm" :title="t('networks.share')" @click="copyShare(cfg)">{{ t('networks.share') }}</button>
        <button v-if="mayOperate(cfg)" class="btn btn-ghost btn-sm" @click="openEdit(cfg)">{{ t('networks.edit') }}</button>
        <button v-if="mayOperate(cfg)" class="btn btn-secondary btn-sm" @click="toggle(cfg)">
          {{ cfg.enabled ? t('networks.disable') : t('networks.enable') }}
        </button>
        <button v-if="mayOperate(cfg)" class="btn btn-ghost btn-sm text-danger" @click="del(cfg)">{{ t('networks.delete') }}</button>
      </div>
    </div>

    <ConfigEditorDialog
      :visible="showEditor"
      :config="editing"
      :existing="configs"
      @update:visible="showEditor = $event"
      @save="onSave"
    />

    <!-- Import dialog -->
    <div v-if="showImport" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4" @click.self="showImport = false">
      <div class="card p-6 w-full max-w-lg">
        <h2 class="text-sm font-bold uppercase tracking-wide mb-3 dark:text-white text-surface-900">{{ t('networks.import_title') }}</h2>
        <p class="text-xs text-surface-600 mb-3 dark:text-surface-500">{{ t('networks.import_hint') }}</p>
        <textarea
          v-model="importText"
          rows="8"
          class="input w-full font-mono text-xs"
          placeholder="easytier://join?network_name=...  /  TOML config…"
        />
        <p v-if="importError" class="text-xs text-danger mt-2">{{ importError }}</p>
        <div class="flex justify-end gap-2 mt-4">
          <button class="btn btn-secondary btn-sm" @click="showImport = false">{{ t('networks.cancel') }}</button>
          <button class="btn btn-primary btn-sm" :disabled="importBusy || !importText.trim()" @click="doImport">
            {{ t('networks.import_btn') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>



