import { ref } from 'vue'
import { backend, events, onEvent, isNative, type CoreStatus } from '@/lib/backend'

// Module-level singleton so every component shares the same core status.
const status = ref<CoreStatus>('stopped')
const error = ref<string | null>(null)
const busy = ref(false)

let pollTimer: ReturnType<typeof setInterval> | null = null

export function useCore() {
  async function refreshStatus() {
    try {
      status.value = (await backend.coreStatus()) as CoreStatus
    } catch (e) {
      error.value = String(e)
    }
  }

  function clearError() {
    error.value = null
  }

  async function start() {
    busy.value = true
    error.value = null
    try {
      await backend.startCore()
      await refreshStatus()
    } catch (e) {
      error.value = String(e)
      await refreshStatus()
    } finally {
      busy.value = false
    }
  }

  async function stop() {
    busy.value = true
    error.value = null
    try {
      await backend.stopCore()
      await refreshStatus()
    } catch (e) {
      error.value = String(e)
    } finally {
      busy.value = false
    }
  }

  async function restart() {
    busy.value = true
    error.value = null
    try {
      await backend.restartCore()
      await refreshStatus()
    } catch (e) {
      error.value = String(e)
    } finally {
      busy.value = false
    }
  }

  function subscribe() {
    const offs = [
      onEvent<CoreStatus>(events.coreStatus, (s) => {
        status.value = s
      }),
      onEvent<string>(events.coreError, (msg) => {
        error.value = msg
      }),
    ]

    // Browser mode has no Go events; poll the status endpoint.
    if (!isNative && !pollTimer) {
      pollTimer = setInterval(refreshStatus, 2000)
    }

    return () => {
      offs.forEach((off) => off())
    }
  }

  return { status, busy, error, start, stop, restart, refreshStatus, clearError, subscribe }
}

export const statusLabel: Record<CoreStatus, string> = {
  stopped: 'Stopped',
  starting: 'Starting…',
  running: 'Running',
  error: 'Error',
}
