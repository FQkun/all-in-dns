<script setup>
import { ref, nextTick, watch } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'
import { renderMarkdown } from '../markdown'

const route = useRoute()
const open = ref(false)
const input = ref('')
const loading = ref(false)
const messages = ref([]) // { role: 'user'|'assistant', content, error? }
const bodyEl = ref(null)

const quickQuestions = [
  '我的解析为什么不生效？',
  '最近同步为什么失败？',
  '域名验证失败是什么意思？',
  '怎么把记录同步到另一个平台？'
]

function buildContext() {
  return {
    page: route.path,
    domain: route.query.domainName || ''
  }
}

function scrollToBottom() {
  nextTick(() => {
    if (bodyEl.value) bodyEl.value.scrollTop = bodyEl.value.scrollHeight
  })
}

watch(messages, scrollToBottom, { deep: true })

async function send(text) {
  const q = (text ?? input.value).trim()
  if (!q || loading.value) return
  messages.value.push({ role: 'user', content: q })
  input.value = ''
  loading.value = true
  scrollToBottom()
  try {
    const r = await api.post('/api/ai/chat', { message: q, ...buildContext() })
    messages.value.push({ role: 'assistant', content: r.answer || '（没有返回内容）' })
  } catch (e) {
    messages.value.push({ role: 'assistant', content: e.message, error: true })
  } finally {
    loading.value = false
  }
}

function clearChat() {
  messages.value = []
}

function toggle() {
  open.value = !open.value
  if (open.value) scrollToBottom()
}
</script>

<template>
  <div class="ai-root">
    <button class="ai-fab" :class="{ open }" type="button" title="AI 诊断助手" @click="toggle">
      <svg v-if="!open" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
           stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M12 3v2M12 19v2M5 12H3M21 12h-2"/>
        <rect x="6" y="6" width="12" height="12" rx="3.5"/>
        <circle cx="9.5" cy="11" r="1"/><circle cx="14.5" cy="11" r="1"/>
        <path d="M9.5 15h5"/>
      </svg>
      <svg v-else viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
           stroke-linecap="round" aria-hidden="true">
        <path d="M6 6l12 12M18 6L6 18"/>
      </svg>
    </button>

    <div v-if="open" class="ai-panel">
      <div class="ai-head">
        <div class="ai-head-main">
          <span class="ai-avatar">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
                 stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <rect x="4" y="7" width="16" height="12" rx="4"/>
              <circle cx="9" cy="13" r="1"/><circle cx="15" cy="13" r="1"/>
              <path d="M9 16h6M12 3v4"/>
            </svg>
          </span>
          <div>
            <strong>AI 诊断助手</strong>
            <span class="ai-sub">只读查询 · 不会修改你的配置</span>
          </div>
        </div>
        <div class="ai-head-ops">
          <button v-if="messages.length" class="ai-icon-btn" type="button" title="清空对话" @click="clearChat">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
              <path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6"/><path d="M10 11v5M14 11v5"/>
            </svg>
          </button>
          <button class="ai-icon-btn" type="button" title="收起" @click="open = false">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
              <path d="M6 6l12 12M18 6L6 18"/>
            </svg>
          </button>
        </div>
      </div>

      <div ref="bodyEl" class="ai-body">
        <div v-if="!messages.length" class="ai-welcome">
          <div class="ai-welcome-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"
                 stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <path d="M12 2a7 7 0 0 0-7 7c0 2.4 1.2 4 2.5 5.3.7.7 1 1.6 1 2.2V18h7v-1.5c0-.6.3-1.5 1-2.2C17.8 13 19 11.4 19 9a7 7 0 0 0-7-7Z"/>
              <path d="M9 21h6"/>
            </svg>
          </div>
          <p class="ai-welcome-title">遇到 DNS 问题？问我</p>
          <p class="ai-welcome-desc">我会查询你的域名、绑定关系、同步日志，用大白话告诉你原因和解决办法。</p>
          <div class="ai-quick">
            <button v-for="q in quickQuestions" :key="q" type="button" class="ai-chip" @click="send(q)">{{ q }}</button>
          </div>
        </div>

        <div v-for="(m, i) in messages" :key="i" class="ai-msg" :class="m.role">
          <div v-if="m.role === 'assistant'" class="ai-msg-avatar">AI</div>
          <div v-if="m.error" class="ai-bubble error">{{ m.content }}</div>
          <div v-else-if="m.role === 'assistant'" class="ai-bubble markdown" v-html="renderMarkdown(m.content)"></div>
          <div v-else class="ai-bubble">{{ m.content }}</div>
        </div>

        <div v-if="loading" class="ai-msg assistant">
          <div class="ai-msg-avatar">AI</div>
          <div class="ai-bubble ai-typing"><span></span><span></span><span></span></div>
        </div>
      </div>

      <form class="ai-input" @submit.prevent="send()">
        <input v-model="input" placeholder="描述你的问题…" :disabled="loading">
        <button class="ai-send" type="submit" :disabled="loading || !input.trim()" title="发送">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9"
               stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d="M4 12l16-8-6 16-2.5-6.5L4 12Z"/>
          </svg>
        </button>
      </form>
    </div>
  </div>
</template>
