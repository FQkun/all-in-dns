<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, download } from '../api'
import { toast } from '../ui'
import Modal from '../components/Modal.vue'

const PROVIDER_LABEL = { tencent: '腾讯云', aliyun: '阿里云', volc: '火山引擎' }
const TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA']

const route = useRoute()
const router = useRouter()
const domainId = computed(() => Number(route.query.domainId))
const domainName = computed(() => route.query.domainName || '')

const res = ref({ items: [], total: 0, page: 1, size: 100 })
const page = ref(1)
const loading = ref(false)
const checked = ref(new Set())

const showForm = ref(false)
const editing = ref(null)
const form = ref({ sub: '@', type: 'A', line: '默认', value: '', ttl: 600, priority: '' })
const showImportCsv = ref(false)
const csvText = ref('')

const pages = computed(() => Math.max(1, Math.ceil(res.value.total / res.value.size)))

async function load() {
  if (!domainId.value) return router.push('/domains')
  loading.value = true
  try {
    res.value = await api.get(`/api/domains/${domainId.value}/records?page=${page.value}&size=100`)
    checked.value = new Set()
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

function syncStates(r) {
  return (r.sync_states || '').split(',').filter(Boolean).map((s) => {
    const parts = s.split('|')
    const aid = parts.length > 2 ? parts[0] : ''
    const p = parts.length > 2 ? parts[1] : parts[0]
    const st = parts.length > 2 ? parts[2] : parts[1]
    return { aid, p, st }
  })
}

function openForm(r = null) {
  editing.value = r
  form.value = r
    ? { sub: r.sub_domain || '@', type: r.type, line: r.line || '默认', value: r.value, ttl: r.ttl, priority: r.priority ?? '' }
    : { sub: '@', type: 'A', line: '默认', value: '', ttl: 600, priority: '' }
  showForm.value = true
}

async function saveForm() {
  const body = {
    sub: form.value.sub, type: form.value.type, line: form.value.line,
    value: form.value.value, ttl: Number(form.value.ttl),
    priority: form.value.priority === '' ? null : Number(form.value.priority)
  }
  try {
    const r = editing.value
      ? await api.post(`/api/records/${editing.value.id}`, body)
      : await api.post(`/api/domains/${domainId.value}/records`, body)
    const fails = (r.sync || []).filter((s) => s.status === 'failed')
    if (fails.length) toast(`已保存，但 ${fails.length} 个平台同步失败（将自动重试）`, 'err')
    else toast('已保存并同步成功', 'ok')
    showForm.value = false
    page.value = 1
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function delRecord(r) {
  if (!confirm('删除该记录（并从各云平台删除）?')) return
  try {
    const res2 = await api.del(`/api/records/${r.id}`)
    const fails = (res2.sync || []).filter((s) => s.status === 'failed')
    fails.length ? toast('本地已删除，部分平台同步失败（将自动重试）', 'err') : toast('已删除', 'ok')
    page.value = 1
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function batchDelete() {
  const ids = [...checked.value]
  if (!ids.length) { toast('请先勾选要删除的记录', 'err'); return }
  if (!confirm(`确认删除选中的 ${ids.length} 条记录?`)) return
  try {
    const r = await api.post('/api/records/batch-delete', { ids })
    toast(r.pending?.length ? `已删除，${r.pending.length} 条记录等待云端重试` : '已删除', r.pending?.length ? 'err' : 'ok')
    page.value = 1
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function importRemote() {
  try {
    toast('拉取中…')
    const r = await api.post(`/api/domains/${domainId.value}/import-remote`)
    toast(`已拉取 ${r.imported} 条` + (r.merged ? `，合并重复 ${r.merged} 条` : ''), 'ok')
    if (r.errors?.length) toast('拉取失败: ' + r.errors.join('；'), 'err')
    page.value = 1
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function importCsv() {
  try {
    const r = await api.post(`/api/domains/${domainId.value}/import`, { csv: csvText.value })
    toast(`已导入 ${r.imported} 条` + (r.skipped ? `，跳过 ${r.skipped} 条` : ''), 'ok')
    showImportCsv.value = false
    csvText.value = ''
    page.value = 1
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function exportCsv() {
  try {
    const blob = await download(`/api/domains/${domainId.value}/export`)
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `records_${domainId.value}.csv`
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } catch (e) {
    toast(e.message, 'err')
  }
}

function toggleCheck(id) {
  const s = new Set(checked.value)
  s.has(id) ? s.delete(id) : s.add(id)
  checked.value = s
}

function toggleAll(e) {
  checked.value = e.target.checked ? new Set(res.value.items.map((r) => r.id)) : new Set()
}

function changePage(p) {
  page.value = p
  load()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-head">
      <h2 class="page-title">解析记录 - {{ domainName }}</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar">
      <button class="btn ghost" @click="router.push('/domains')">← 返回域名</button>
      <button class="btn primary" @click="openForm()">添加记录</button>
      <button class="btn" @click="importRemote">从云平台拉取</button>
      <button class="btn" @click="showImportCsv = true">导入 CSV</button>
      <button class="btn" @click="exportCsv">导出 CSV</button>
      <span class="spacer"></span>
      <button class="btn danger" @click="batchDelete">删除选中</button>
    </div>
    <div class="panel scroll">
      <table>
        <thead>
          <tr>
            <th><input type="checkbox" class="checkbox" :checked="checked.size === res.items.length && res.items.length > 0"
                       @change="toggleAll" aria-label="全选"></th>
            <th>主机</th><th>类型</th><th>线路</th><th>记录值</th><th>TTL</th><th>MX</th>
            <th>本地状态</th><th>同步状态</th><th>更新时间</th><th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in res.items" :key="r.id" :data-id="r.id">
            <td><input type="checkbox" class="checkbox" :checked="checked.has(r.id)" @change="toggleCheck(r.id)" :value="r.id"></td>
            <td>{{ r.sub_domain }}</td>
            <td><span class="badge neutral mono">{{ r.type }}</span></td>
            <td>{{ r.line }}</td>
            <td class="wrap">{{ r.value }}</td>
            <td>{{ r.ttl }}</td>
            <td>{{ r.priority ?? '-' }}</td>
            <td>
              <span v-if="r.status === 'disabled'" class="badge pending" title="云端删除未完成，等待定时任务重试">待清理</span>
              <span v-else class="badge ok">生效中</span>
            </td>
            <td>
              <span v-for="s in syncStates(r)" :key="s.aid + s.p" class="badge" :class="s.st"
                    :title="(PROVIDER_LABEL[s.p] || s.p) + (s.aid ? ' 账号#' + s.aid : '')">
                {{ PROVIDER_LABEL[s.p] || s.p }} {{ s.st === 'success' ? '成功' : s.st === 'failed' ? '失败' : s.st }}
              </span>
              <span v-if="!syncStates(r).length" class="badge neutral">未同步</span>
            </td>
            <td>{{ r.updated_at }}</td>
            <td class="ops">
              <button class="btn ghost sm" @click="openForm(r)">编辑</button>
              <button class="btn ghost sm danger" @click="delRecord(r)">删除</button>
            </td>
          </tr>
          <tr v-if="!res.items.length"><td colspan="11" class="empty">暂无记录</td></tr>
        </tbody>
      </table>
      <div class="pager">
        <button class="btn ghost sm" :disabled="page <= 1" @click="changePage(page - 1)">上一页</button>
        <span class="hint">第 {{ page }} / {{ pages }} 页 · 共 {{ res.total }} 条</span>
        <button class="btn ghost sm" :disabled="page >= pages" @click="changePage(page + 1)">下一页</button>
      </div>
    </div>

    <Modal v-if="showForm" :title="editing ? '编辑记录' : '添加记录'" @close="showForm = false">
      <div class="field"><label>主机记录（@ 表示根域名）</label><input v-model="form.sub"></div>
      <div class="field"><label>类型</label>
        <select v-model="form.type"><option v-for="t in TYPES" :key="t">{{ t }}</option></select>
      </div>
      <div class="field"><label>线路</label><input v-model="form.line"></div>
      <div class="field"><label>记录值</label><input v-model="form.value"></div>
      <div class="field"><label>TTL(秒)</label><input v-model.number="form.ttl" type="number"></div>
      <div class="field"><label>MX 优先级(仅 MX)</label><input v-model="form.priority" type="number"></div>
      <p class="hint">保存后将立即同步到所有已启用的云平台</p>
      <template #actions>
        <button class="btn" @click="showForm = false">取消</button>
        <button class="btn primary" @click="saveForm">保存</button>
      </template>
    </Modal>

    <Modal v-if="showImportCsv" title="导入 CSV" @close="showImportCsv = false">
      <div class="field">
        <label>格式: sub_domain,type,line,value,ttl,priority（首行为表头）</label>
        <textarea v-model="csvText" rows="10" placeholder="@,A,默认,1.2.3.4,600,"></textarea>
      </div>
      <template #actions>
        <button class="btn" @click="showImportCsv = false">取消</button>
        <button class="btn primary" @click="importCsv">导入</button>
      </template>
    </Modal>
  </div>
</template>
