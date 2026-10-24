// 全局 UI 工具：toast 通知 + 统一确认弹窗。
import { reactive } from 'vue'

export const ui = reactive({
  toasts: [],
  confirmState: null
})

let seq = 0
export function toast(msg, type = '') {
  const id = ++seq
  ui.toasts.push({ id, msg, type })
  setTimeout(() => {
    const i = ui.toasts.findIndex((t) => t.id === id)
    if (i >= 0) ui.toasts.splice(i, 1)
  }, 3000)
}

export function errToast(msg) {
  toast(msg, 'err')
}

// uiConfirm 统一的确认弹窗，返回 Promise<boolean>。
// 用法：if (!(await uiConfirm({ title, message, danger: true }))) return
export function uiConfirm({ title = '确认操作', message = '', confirmText = '确认', danger = false } = {}) {
  return new Promise((resolve) => {
    ui.confirmState = { title, message, confirmText, danger, resolve }
  })
}

// resolveUiConfirm 由确认弹窗组件调用。
export function resolveUiConfirm(ok) {
  const s = ui.confirmState
  if (!s) return
  ui.confirmState = null
  s.resolve(ok)
}
