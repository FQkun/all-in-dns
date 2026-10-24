import { createRouter, createWebHashHistory } from 'vue-router'

import Dashboard from './views/Dashboard.vue'
import Accounts from './views/Accounts.vue'
import Domains from './views/Domains.vue'
import Records from './views/Records.vue'
import SyncLogs from './views/SyncLogs.vue'
import OpLogs from './views/OpLogs.vue'
import Backups from './views/Backups.vue'
import Notify from './views/Notify.vue'

// 侧边栏导航项（records 从域名页进入，不在导航内）
export const navItems = [
  { path: '/dashboard', title: '监控概览', icon: 'dashboard' },
  { path: '/accounts', title: '云账号', icon: 'accounts' },
  { path: '/domains', title: '域名管理', icon: 'domains' },
  { path: '/synclogs', title: '同步日志', icon: 'synclogs' },
  { path: '/oplogs', title: '操作日志', icon: 'oplogs' },
  { path: '/backups', title: '备份与恢复', icon: 'backups' },
  { path: '/notify', title: '通知设置', icon: 'notify' }
]

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', redirect: '/dashboard' },
    { path: '/dashboard', component: Dashboard },
    { path: '/accounts', component: Accounts },
    { path: '/domains', component: Domains },
    { path: '/records', component: Records },
    { path: '/synclogs', component: SyncLogs },
    { path: '/oplogs', component: OpLogs },
    { path: '/backups', component: Backups },
    { path: '/notify', component: Notify }
  ]
})

export default router
