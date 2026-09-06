import { ref } from 'vue'

export type Theme = 'dark' | 'light'

const theme = ref<Theme>((localStorage.getItem('et-theme') as Theme) || 'dark')

export function useTheme() {
  function apply(t: Theme) {
    theme.value = t
    localStorage.setItem('et-theme', t)
    if (t === 'light') {
      document.documentElement.classList.remove('dark')
    } else {
      document.documentElement.classList.add('dark')
    }
  }

  function toggle() {
    apply(theme.value === 'dark' ? 'light' : 'dark')
  }

  return { theme, apply, toggle }
}
