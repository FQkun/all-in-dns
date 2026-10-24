<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { toast } from '../ui'
import { isAdmin } from '../auth'

const PROVIDER_LABEL = { tencent: '腾讯云', aliyun: '阿里云', volc: '火山引擎' }
const ACT_LABEL = { create: '新增', update: '修改', delete: '删除' }

const logs = ref([])
const queue = ref([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const [l, q] = await Promise.all([api.get('/api/sync/logs'), api.get('/api/sync/queue')])
    logs.value = l
    queue.value = q
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

async function retryNow() {
  try {
    const r = await api.post('/api/sync/retry-now')
    toast(`已重试 ${r.count ?? r.items?.length ?? 0} 个任务`, 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function clearQueue() {
  if (!confirm('确认删除全部待重试任务?')) return
  try {
    const r = await api.del('/api/sync/queue')
    toast(`已删除 ${r.count ?? 0} 个重试任务`, 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function clearLogs() {
  if (!confirm('确认清空全部同步日志?')) return
  try {
    await api.del('/api/sync/logs')
    toast('已清空', 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-head">
      <h2 class="page-title">同步日志</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar">
      <button class="btn" @click="retryNow">立即重试失败任务</button>
      <button class="btn danger" @click="clearQueue">删除全部重试任务</button>
      <button v-if="isAdmin()" class="btn danger" @click="clearLogs">清空日志</button>
      <span class="hint">失败任务由定时任务自动重试（指数退避，最多 5 次）</span>
    </div>

    <details v-if="queue.length" class="panel queue">
      <summary>待重试队列 ({{ queue.length }})<span v-if="queue.some(q => q.status === 'failed')" class="badge failed">有失败</span></summary>
      <table>
        <thead><tr><th>ID</th><th>平台</th><th>动作</th><th>重试次数</th><th>下次重试</th><th>错误</th></tr></thead>
        <tbody>
          <tr v-for="q in queue" :key="q.id">
            <td>{{ q.id }}</td>
            <td><span class="badge" :class="q.provider">{{ PROVIDER_LABEL[q.provider] || q.provider }}</span></td>
            <td>{{ ACT_LABEL[q.action] || q.action }}</td>
            <td>{{ q.retry_count }}</td>
            <td>{{ q.next_retry_at || '立即' }}</td>
            <td class="wrap">{{ q.last_error }}</td>
          </tr>
        </tbody>
      </table>
    </details>

    <div class="panel scroll">
      <table>
        <thead><tr><th>时间</th><th>平台</th><th>记录ID</th><th>动作</th><th>结果</th><th>消息</th><th>耗时</th></tr></thead>
        <tbody>
          <tr v-for="l in logs" :key="l.id">
            <td>{{ l.created_at }}</td>
            <td><span class="badge" :class="l.provider">{{ PROVIDER_LABEL[l.provider] || l.provider }}</span></td>
            <td>{{ l.record_id ?? '-' }}</td>
            <td>{{ ACT_LABEL[l.action] || l.action }}</td>
            <td><span class="badge" :class="l.status">{{ l.status === 'success' ? '成功' : '失败' }}</span></td>
            <td class="wrap">{{ l.message }}</td>
            <td>{{ l.request_cost_ms ?? '-' }}ms</td>
          </tr>
          <tr v-if="!logs.length"><td colspan="7" class="empty">暂无日志</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
