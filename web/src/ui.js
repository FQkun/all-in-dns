// 全局 UI 工具：toast 通知。
import { reactive } from 'vue'

export const ui = reactive({
  toasts: []
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

// 错误 toast：弹模态显示详情（对齐原 app.js toast(msg,'err') 行为）。
export function errToast(msg) {
  toast(msg, 'err')
}
