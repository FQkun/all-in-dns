<script setup>
import { ref, computed, onMounted } from 'vue'
import { api } from '../api'
import { toast, uiConfirm } from '../ui'

const rows = ref([])
const loading = ref(false)
const checked = ref(new Set())

const hasSel = computed(() => checked.value.size > 0)

async function load() {
  loading.value = true
  try {
    rows.value = await api.get('/api/backups')
    checked.value = new Set()
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

async function create() {
  try {
    await api.post('/api/backups')
    toast('备份已创建', 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function restore(b) {
  if (!(await uiConfirm({ title: '恢复备份', message: '恢复将覆盖当前全部域名与解析记录数据，此操作不可撤销。', confirmText: '确认恢复', danger: true }))) return
  try {
    await api.post(`/api/backups/${b.id}/restore`)
    toast('恢复完成', 'ok')
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function del(b) {
  if (!(await uiConfirm({ title: '删除备份', message: '确认删除该备份？删除后不可恢复。', confirmText: '删除', danger: true }))) return
  try {
    await api.post('/api/backups/delete', { ids: [b.id] })
    toast('已删除', 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

async function delSelected() {
  const ids = [...checked.value]
  if (!ids.length) return
  if (!(await uiConfirm({ title: '删除备份', message: `确认删除所选 ${ids.length} 个备份？`, confirmText: '删除', danger: true }))) return
  try {
    await api.post('/api/backups/delete', { ids })
    toast('已删除', 'ok')
    load()
  } catch (e) {
    toast(e.message, 'err')
  }
}

function toggle(id) {
  const s = new Set(checked.value)
  s.has(id) ? s.delete(id) : s.add(id)
  checked.value = s
}

function toggleAll(e) {
  checked.value = e.target.checked ? new Set(rows.value.map((b) => b.id)) : new Set()
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-head">
      <h2 class="page-title">备份与恢复</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar">
      <button class="btn primary" @click="create">创建备份</button>
      <button class="btn ghost sm danger" :disabled="!hasSel" @click="delSelected">删除所选</button>
      <label class="hint" style="cursor:pointer"><input type="checkbox" class="checkbox" :checked="checked.size === rows.length && rows.length > 0" @change="toggleAll"> 全选</label>
      <span class="hint">备份保存域名、绑定关系与全部解析记录的 JSON 快照</span>
    </div>
    <div class="panel scroll">
      <table>
        <thead><tr><th style="width:36px"></th><th>ID</th><th>名称</th><th>记录数</th><th>创建人</th><th>时间</th><th>操作</th></tr></thead>
        <tbody>
          <tr v-for="b in rows" :key="b.id">
            <td><input type="checkbox" class="checkbox" :checked="checked.has(b.id)" @change="toggle(b.id)"></td>
            <td>{{ b.id }}</td>
            <td>{{ b.name }}</td>
            <td>{{ b.record_count }}</td>
            <td>{{ b.created_by }}</td>
            <td>{{ b.created_at }}</td>
            <td class="ops">
              <button class="btn ghost sm danger" @click="restore(b)">恢复</button>
              <button class="btn ghost sm danger" @click="del(b)">删除</button>
            </td>
          </tr>
          <tr v-if="!rows.length"><td colspan="7" class="empty">暂无备份</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
