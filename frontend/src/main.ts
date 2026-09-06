import { createApp } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import App from './App.vue'
import Dashboard from './views/Dashboard.vue'
import Networks from './views/Networks.vue'
import Peers from './views/Peers.vue'
import Settings from './views/Settings.vue'
import Users from './views/Users.vue'
import Devices from './views/Devices.vue'
import Tunnels from './views/Tunnels.vue'
import { i18n } from '@/lib/i18n'
import { useTheme } from '@/composables/useTheme'
import './style.css'

const routes = [
  { path: '/', name: 'dashboard', component: Dashboard },
  { path: '/networks', name: 'networks', component: Networks },
  { path: '/peers', name: 'peers', component: Peers },
  { path: '/users', name: 'users', component: Users },
  { path: '/devices', name: 'devices', component: Devices },
  { path: '/tunnels', name: 'tunnels', component: Tunnels },
  { path: '/settings', name: 'settings', component: Settings },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

// Web/browser vs desktop: the web UI compacts itself (smaller scale,
// responsive layout) while the desktop window keeps its proportions.
if (typeof window !== 'undefined' && !(window as any).go) {
  document.documentElement.classList.add('web')
}

// Apply saved theme before mount
useTheme().apply((localStorage.getItem('et-theme') as 'dark' | 'light') || 'dark')

const app = createApp(App)
app.use(router)
app.use(i18n)
app.mount('#app')
