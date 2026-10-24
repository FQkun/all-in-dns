<script setup>
import { onMounted, computed } from 'vue'
import { RouterView, useRouter } from 'vue-router'
import { userState, getToken } from './auth'
import { ui, resolveUiConfirm } from './ui'
import { navItems } from './router'
import { api } from './api'
import Login from './views/Login.vue'
import AiAssistant from './components/AiAssistant.vue'
import Modal from './components/Modal.vue'

const router = useRouter()
const whoami = computed(() => {
  const u = userState.user
  return u ? u.username + (u.role === 'admin' ? ' (管理员)' : '') : ''
})

onMounted(async () => {
  if (getToken()) {
    try {
      const me = await api.get('/api/auth/me')
      userState.user = me.user
    } catch (e) {
      userState.logout()
    }
  }
  userState.checked = true
})

async function logout() {
  try {
    await api.post('/api/auth/logout')
  } catch (e) { /* ignore */ }
  userState.logout()
  router.push('/dashboard')
}

const icons = {
  dashboard: '<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>',
  accounts: '<path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>',
  domains: '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>',
  synclogs: '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
  oplogs: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>',
  backups: '<rect x="2" y="3" width="20" height="5" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>'
}
</script>

<template>
  <div id="app" v-if="userState.checked">
    <template v-if="!userState.user">
      <main id="main">
        <Login />
      </main>
    </template>

    <template v-else>
      <aside id="sidebar">
        <div class="logo">DNS 管理平台</div>
        <nav id="nav">
          <router-link v-for="item in navItems" :key="item.path" :to="item.path">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
                 stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"
                 v-html="icons[item.icon]"></svg>
            <span>{{ item.title }}</span>
          </router-link>
        </nav>
        <div class="sidebar-footer">
          <div class="sf-user">
            <span id="whoami">{{ whoami }}</span>
            <button id="btn-logout" class="btn ghost sm" @click="logout">退出</button>
          </div>
          <span id="req-time"></span>
        </div>
      </aside>

      <main id="main">
        <div id="view">
          <router-view />
        </div>
        <footer id="app-footer">
          <span>DNS 统一管理平台</span>
          <span>腾讯云 · 阿里云 · 火山引擎 · 三端同步</span>
          <a class="author-link" href="https://github.com/FQkun/all-in-dns" target="_blank" rel="noopener">GitHub @FQkun</a>
        </footer>
      </main>

      <AiAssistant />
    </template>
  </div>

  <Modal v-if="ui.confirmState" :title="ui.confirmState.title" @close="resolveUiConfirm(false)">
    <p class="confirm-msg">{{ ui.confirmState.message }}</p>
    <template #actions>
      <button class="btn" @click="resolveUiConfirm(false)">取消</button>
      <button class="btn" :class="ui.confirmState.danger ? 'danger' : 'primary'" @click="resolveUiConfirm(true)">
        {{ ui.confirmState.confirmText }}
      </button>
    </template>
  </Modal>

  <div id="toast">
    <div v-for="t in ui.toasts" :key="t.id" class="toast-item" :class="t.type" @click="ui.toasts = ui.toasts.filter(x => x.id !== t.id)">
      {{ t.msg }}
    </div>
  </div>
</template>
