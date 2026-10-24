// 轻量 Markdown 渲染：先转义 HTML 防注入，再交给 marked 渲染。
import { marked } from 'marked'

marked.setOptions({ gfm: true, breaks: true })

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

// renderMarkdown 把 AI 输出的 markdown 转成安全 HTML。
export function renderMarkdown(text) {
  if (!text) return ''
  return marked.parse(escapeHtml(text))
}
