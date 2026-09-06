import { ref } from 'vue'
import { backend, type ConfigFile } from '@/lib/backend'

// Module-level singleton so every component shares the same config list
// (Dashboard, Networks and Peers stay in sync without separate polling).
const configs = ref<ConfigFile[]>([])
const loading = ref(false)
const error = ref<string | null>(null)

export function useConfigs() {
  async function refresh() {
    loading.value = true
    error.value = null
    try {
      const list = await backend.listConfigs()
      configs.value = Array.isArray(list) ? list : []
    } catch (e) {
      error.value = String(e)
    } finally {
      loading.value = false
    }
  }

  async function save(cfg: ConfigFile) {
    await backend.saveConfig(cfg)
    await refresh()
  }

  async function remove(id: string) {
    await backend.deleteConfig(id)
    await refresh()
  }

  async function setEnabled(id: string, enabled: boolean) {
    await backend.setConfigEnabled(id, enabled)
    await refresh()
  }

  async function applyAll(enabledIDs: string[]) {
    await backend.applyConfigs(configs.value, enabledIDs)
    await refresh()
  }

  return { configs, loading, error, refresh, save, remove, setEnabled, applyAll }
}
