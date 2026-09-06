<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { backend } from '@/lib/backend'

const { t, locale } = useI18n()

const username = ref('')
const password = ref('')
const busy = ref(false)
const error = ref('')

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    await backend.login(username.value.trim(), password.value)
  } catch (e) {
    error.value = String(e).replace(/^Error: /, '')
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center p-6 bg-surface-100 dark:bg-surface-950">
    <div class="card w-full max-w-sm p-8">
      <div class="flex items-center gap-3 mb-6">
        <div class="w-10 h-10 bg-accent-500 flex items-center justify-center text-white font-black text-lg">E</div>
        <div>
          <h1 class="text-lg font-black uppercase tracking-wide dark:text-white text-surface-900">EasyTier Pro</h1>
          <p class="text-xs text-surface-600 dark:text-surface-500">{{ t('login.subtitle') }}</p>
        </div>
      </div>

      <form class="space-y-4" @submit.prevent="submit">
        <div>
          <label class="label">{{ t('login.username') }}</label>
          <input v-model="username" class="input" autocomplete="username" autofocus />
        </div>
        <div>
          <label class="label">{{ t('login.password') }}</label>
          <input v-model="password" type="password" class="input" autocomplete="current-password"
                 @keyup.enter="submit" />
        </div>
        <p v-if="error" class="text-sm text-danger">{{ error }}</p>
        <button type="submit" class="btn btn-primary w-full" :disabled="busy || !username || !password">
          {{ busy ? t('common.loading') : t('login.submit') }}
        </button>
      </form>

      <div class="mt-4 text-right">
        <select :value="locale" class="input !w-auto text-xs py-1" @change="(e: any) => $i18n.locale = e.target.value">
          <option value="en">English</option>
          <option value="zh">中文</option>
        </select>
      </div>
    </div>
  </div>
</template>
