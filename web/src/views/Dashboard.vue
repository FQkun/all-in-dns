<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { toast } from '../ui'
import Modal from '../components/Modal.vue'

const PROVIDER_LABEL = { tencent: '腾讯云', aliyun: '阿里云', volc: '火山引擎' }
const ACT_LABEL = { create: '新增', update: '修改', delete: '删除' }

const s = ref({})
const loading = ref(false)
const showFails = ref(false)
const fails = ref([])

async function load() {
  loading.value = true
  try {
    s.value = await api.get('/api/monitor/summary')
    if (s.value.failed_queue > 0) {
      try {
        const q = await api.get('/api/sync/queue')
        fails.value = q.filter((x) => x.status === 'failed')
        if (fails.value.length) showFails.value = true
      } catch (e) { /* ignore */ }
    }
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

async function retryNow() {
  const r = await api.post('/api/sync/retry-now')
  toast(`已重新执行 ${r.count ?? r.items?.length ?? 0} 个任务`, 'ok')
  showFails.value = false
  load()
}

function recLabel(payload) {
  try {
    const p = JSON.parse(payload)
    return `${p.record.sub} ${p.record.type} ${p.record.value}`
  } catch (e) {
    return '-'
  }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-head">
      <h2 class="page-title">监控概览</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="cards">
      <div class="stat-card hero">
        <div class="lbl">域名总数</div>
        <div class="num">{{ s.domains ?? '-' }}</div>
        <div class="hero-sub">已收录 {{ s.records ?? '-' }} 条解析记录</div>
      </div>
      <div class="stat-card"><div class="num">{{ s.pending_queue ?? '-' }}</div><div class="lbl">待同步任务</div></div>
      <div class="stat-card"><div class="num">{{ s.failed_queue ?? '-' }}</div><div class="lbl">失败任务</div></div>
      <div class="stat-card"><div class="num">{{ s.success_sync_24h ?? '-' }}</div><div class="lbl">24h 同步成功</div></div>
      <div class="stat-card"><div class="num">{{ s.failed_sync_24h ?? '-' }}</div><div class="lbl">24h 同步失败</div></div>
    </div>

    <Modal v-if="showFails" :title="`有 ${fails.length} 个同步任务失败`" wide @close="showFails = false">
      <div class="panel scroll">
        <table>
          <thead><tr><th>平台</th><th>操作</th><th>记录</th><th>失败原因</th><th>已重试</th></tr></thead>
          <tbody>
            <tr v-for="x in fails" :key="x.id">
              <td>{{ PROVIDER_LABEL[x.provider] || x.provider || '-' }}</td>
              <td>{{ ACT_LABEL[x.action] || x.action }}</td>
              <td class="wrap">{{ recLabel(x.payload) }}</td>
              <td class="wrap err-text">{{ x.last_error || '-' }}</td>
              <td>{{ x.retry_count }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <template #actions>
        <button class="btn" @click="showFails = false">关闭</button>
        <button class="btn primary" @click="retryNow">立即重试</button>
      </template>
    </Modal>
  </div>
</template>
