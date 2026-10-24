<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { toast, uiConfirm } from '../ui'
import { userState, isAdmin } from '../auth'
import Modal from '../components/Modal.vue'

const PROVIDER_LABEL = { tencent: '腾讯云', aliyun: '阿里云', volc: '火山引擎' }

const rows = ref([])
const loading = ref(false)
const showAdd = ref(false)
const form = ref({ provider: 'tencent', name: '', access_key_id: '', access_key_secret: '' })

async function load() {
  loading.value = true
  try {
    rows.value = await api.get('/api/accounts')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

async function save() {
  try {
    await api.post('/api/accounts', form.value)
    toast('账号已添加', 'ok')
    showAdd.value = false
    form.value = { provider: 'tencent', name: '', access_key_id: '', access_key_secret: '' }
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function testAccount(a) {
  try {
    toast('测试中…')
    const r = await api.post(`/api/accounts/${a.id}/test`)
    toast(`连通成功，该账号下有 ${r.domains} 个域名`, 'ok')
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function delAccount(a) {
  if (!(await uiConfirm({ title: '删除账号', message: '删除该云账号将同时解除它与所有域名的绑定，确认继续？', confirmText: '删除', danger: true }))) return
  try {
    await api.del(`/api/accounts/${a.id}`)
    toast('已删除', 'ok')
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
      <h2 class="page-title">云账号</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar">
      <span class="spacer"></span>
      <button class="btn primary" @click="showAdd = true">添加账号</button>
    </div>
    <div class="panel scroll">
      <table>
        <thead><tr><th>ID</th><th>厂商</th><th>名称</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="a in rows" :key="a.id">
            <td>{{ a.id }}</td>
            <td><span class="badge" :class="a.provider">{{ PROVIDER_LABEL[a.provider] || a.provider }}</span></td>
            <td>{{ a.name }}</td>
            <td><span class="badge" :class="a.status == 1 ? 'ok' : 'fail'">{{ a.status == 1 ? '启用' : '停用' }}</span></td>
            <td>{{ a.created_at }}</td>
            <td class="ops">
              <button class="btn ghost sm" @click="testAccount(a)">连通测试</button>
              <button v-if="isAdmin()" class="btn ghost sm danger" @click="delAccount(a)">删除</button>
            </td>
          </tr>
          <tr v-if="!rows.length"><td colspan="6" class="empty">暂无账号</td></tr>
        </tbody>
      </table>
    </div>

    <Modal v-if="showAdd" title="添加云账号" @close="showAdd = false">
      <div class="field"><label>厂商</label>
        <select v-model="form.provider">
          <option value="tencent">腾讯云</option>
          <option value="aliyun">阿里云</option>
          <option value="volc">火山引擎</option>
        </select>
      </div>
      <div class="field"><label>名称</label><input v-model="form.name" placeholder="如：主账号"></div>
      <div class="field"><label>Access Key ID</label><input v-model="form.access_key_id" autocomplete="off"></div>
      <div class="field"><label>Access Key Secret</label><input v-model="form.access_key_secret" type="password" autocomplete="off"></div>
      <template #actions>
        <button class="btn" @click="showAdd = false">取消</button>
        <button class="btn primary" @click="save">保存</button>
      </template>
    </Modal>
  </div>
</template>
