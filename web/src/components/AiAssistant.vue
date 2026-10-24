<script setup>
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import { api } from '../api'

const route = useRoute()
const open = ref(false)
const input = ref('')
const loading = ref(false)
const messages = ref([]) // { role: 'user'|'assistant', content, error? }

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

async function send(text) {
  const q = (text ?? input.value).trim()
  if (!q || loading.value) return
  messages.value.push({ role: 'user', content: q })
  input.value = ''
  loading.value = true
  try {
    const r = await api.post('/api/ai/chat', { message: q, ...buildContext() })
    messages.value.push({ role: 'assistant', content: r.answer || '（没有返回内容）' })
  } catch (e) {
    messages.value.push({ role: 'assistant', content: e.message, error: true })
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="ai-root">
    <button class="ai-fab" :class="{ open }" type="button" title="AI 诊断助手" @click="open = !open">
      <svg v-if="!open" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"
           stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M12 3v2M12 19v2M5 12H3M21 12h-2"/>
        <rect x="6" y="6" width="12" height="12" rx="3"/>
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
        <div>
          <strong>AI 诊断助手</strong>
          <span class="ai-sub">只读查询，不会修改你的配置</span>
        </div>
      </div>

      <div class="ai-body">
        <div v-if="!messages.length" class="ai-welcome">
          <p>遇到问题可以问我，我会查询你的域名、同步日志来帮你定位原因。</p>
          <div class="ai-quick">
            <button v-for="q in quickQuestions" :key="q" type="button" class="ai-chip" @click="send(q)">{{ q }}</button>
          </div>
        </div>

        <div v-for="(m, i) in messages" :key="i" class="ai-msg" :class="m.role">
          <div class="ai-bubble" :class="{ error: m.error }">{{ m.content }}</div>
        </div>

        <div v-if="loading" class="ai-msg assistant">
          <div class="ai-bubble ai-typing"><span></span><span></span><span></span></div>
        </div>
      </div>

      <form class="ai-input" @submit.prevent="send()">
        <input v-model="input" placeholder="描述你的问题…" :disabled="loading">
        <button class="btn primary sm" type="submit" :disabled="loading || !input.trim()">发送</button>
      </form>
    </div>
  </div>
</template>
