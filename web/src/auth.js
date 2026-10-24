// 登录态管理：JWT token（localStorage）+ 全局响应式 user 状态。
// 注意：本模块不依赖 api.js，避免循环 import。
import { reactive } from 'vue'

const TOKEN_KEY = 'dns_token'

export function getToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setToken(t) {
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export const userState = reactive({
  user: null,
  checked: false,

  setAuth(token, user) {
    setToken(token)
    this.user = user
  },
  logout() {
    setToken('')
    this.user = null
    this.checked = true
  }
})

export const isAdmin = () => userState.user && userState.user.role === 'admin'
export const canOperate = () =>
  userState.user && ['admin', 'operator'].includes(userState.user.role)
