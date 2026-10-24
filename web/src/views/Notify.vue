<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { toast, uiConfirm } from '../ui'
import Modal from '../components/Modal.vue'

const TYPE_LABEL = { wecom: '企业微信', feishu: '飞书', dingtalk: '钉钉', webhook: '通用 Webhook' }
const TYPE_HINT = {
  wecom: '群机器人 Webhook，形如 https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx',
  feishu: '飞书群自定义机器人 Webhook，形如 https://open.feishu.cn/open-apis/bot/v2/hook/xxx',
  dingtalk: '钉钉群机器人 Webhook，形如 https://oapi.dingtalk.com/robot/send?access_token=xxx',
  webhook: '任意接收 JSON 的地址，会 POST 完整事件对象'
}

const rows = ref([])
const events = ref([])
const loading = ref(false)

const showForm = ref(false)
const editing = ref(null)
const form = ref({ name: '', type: 'wecom', url: '', events: [], enabled: true })

async function load() {
  loading.value = true
  try {
    const [channels, evs] = await Promise.all([
      api.get('/api/notify/channels'),
      api.get('/api/notify/events')
    ])
    rows.value = channels
    events.value = evs
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

function openForm(row = null) {
  editing.value = row
  form.value = row
    ? { name: row.name, type: row.type, url: '', events: [...(row.events || [])], enabled: row.enabled === 1 }
    : { name: '', type: 'wecom', url: '', events: ['all'], enabled: true }
  showForm.value = true
}

async function save() {
  const body = {
    name: form.value.name,
    type: form.value.type,
    events: form.value.events,
    enabled: form.value.enabled
  }
  // 编辑时 URL 留空表示不修改
  if (form.value.url) body.url = form.value.url
  try {
    if (editing.value) {
      await api.put(`/api/notify/channels/${editing.value.id}`, body)
    } else {
      await api.post('/api/notify/channels', body)
    }
    toast('已保存', 'ok')
    showForm.value = false
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function testChannel(row) {
  try {
    toast('发送中…')
    await api.post(`/api/notify/channels/${row.id}/test`)
    toast('测试消息已发送，请到群里查看', 'ok')
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function del(row) {
  if (!(await uiConfirm({ title: '删除通知渠道', message: `确认删除「${row.name}」？`, confirmText: '删除', danger: true }))) return
  try {
    await api.del(`/api/notify/channels/${row.id}`)
    toast('已删除', 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

function toggleEvent(t) {
  const list = form.value.events
  if (list.includes('all')) {
    form.value.events = [t]
    return
  }
  const i = list.indexOf(t)
  if (i >= 0) list.splice(i, 1)
  else list.push(t)
  if (list.length === 0) form.value.events = ['all']
}

function eventLabel(t) {
  if (t === 'all') return '全部事件'
  const e = events.value.find((x) => x.type === t)
  return e ? e.label : t
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-head">
      <h2 class="page-title">通知设置</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar">
      <button class="btn primary" @click="openForm()">添加渠道</button>
      <span class="hint">同步最终失败、域名验证失败、自动备份完成时可推送消息</span>
    </div>

    <div class="panel scroll">
      <table>
        <thead><tr><th>名称</th><th>类型</th><th>订阅事件</th><th>Webhook</th><th>状态</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="row in rows" :key="row.id">
            <td>{{ row.name }}</td>
            <td><span class="badge neutral">{{ TYPE_LABEL[row.type] || row.type }}</span></td>
            <td class="wrap">
              <span v-for="e in row.events" :key="e" class="badge neutral" style="margin-right:4px">{{ eventLabel(e) }}</span>
            </td>
            <td class="wrap mono" style="font-size:12px;color:var(--text3)">{{ row.url_masked }}</td>
            <td><span class="badge" :class="row.enabled === 1 ? 'ok' : 'neutral'">{{ row.enabled === 1 ? '启用' : '停用' }}</span></td>
            <td class="ops">
              <button class="btn ghost sm" @click="testChannel(row)">测试</button>
              <button class="btn ghost sm" @click="openForm(row)">编辑</button>
              <button class="btn ghost sm danger" @click="del(row)">删除</button>
            </td>
          </tr>
          <tr v-if="!rows.length"><td colspan="6" class="empty">暂无通知渠道</td></tr>
        </tbody>
      </table>
    </div>

    <Modal v-if="showForm" :title="editing ? '编辑通知渠道' : '添加通知渠道'" @close="showForm = false">
      <div class="field"><label>名称</label><input v-model="form.name" placeholder="如：运维群"></div>
      <div class="field">
        <label>类型</label>
        <select v-model="form.type" :disabled="!!editing">
          <option v-for="(label, key) in TYPE_LABEL" :key="key" :value="key">{{ label }}</option>
        </select>
        <p class="hint" style="margin-top:6px">{{ TYPE_HINT[form.type] }}</p>
      </div>
      <div class="field">
        <label>Webhook URL{{ editing ? '（留空表示不修改）' : '' }}</label>
        <input v-model="form.url" :placeholder="editing ? editing.url_masked : '粘贴机器人 Webhook 地址'">
      </div>
      <div class="field">
        <label>订阅事件</label>
        <div style="display:flex;flex-wrap:wrap;gap:14px;margin-top:4px">
          <label v-for="e in events" :key="e.type" style="display:flex;align-items:center;gap:6px;margin:0;cursor:pointer">
            <input type="checkbox" class="checkbox" :checked="form.events.includes(e.type)"
                   @change="toggleEvent(e.type)">
            <span style="font-size:13px">{{ e.label }}</span>
          </label>
        </div>
        <p class="hint" style="margin-top:6px">不选则默认订阅全部事件</p>
      </div>
      <div class="field" v-if="editing">
        <label style="display:flex;align-items:center;gap:8px;cursor:pointer">
          <input type="checkbox" class="checkbox" v-model="form.enabled">
          <span>启用该渠道</span>
        </label>
      </div>
      <template #actions>
        <button class="btn" @click="showForm = false">取消</button>
        <button class="btn primary" @click="save">保存</button>
      </template>
    </Modal>
  </div>
</template>
