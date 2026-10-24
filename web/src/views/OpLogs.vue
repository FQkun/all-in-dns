<script setup>
import { ref, onMounted } from 'vue'
import { api } from '../api'
import { toast } from '../ui'

const logs = ref([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    logs.value = await api.get('/api/operation-logs')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}

async function clearLogs() {
  if (!confirm('确认清空全部操作日志?')) return
  try {
    await api.del('/api/operation-logs')
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
      <h2 class="page-title">操作日志</h2>
      <button class="btn ghost sm" :disabled="loading" @click="load">刷新</button>
    </div>
    <div class="toolbar"><button class="btn danger" @click="clearLogs">清空日志</button></div>
    <div class="panel scroll">
      <table>
        <thead><tr><th>时间</th><th>用户</th><th>动作</th><th>对象</th><th>详情</th><th>IP</th></tr></thead>
        <tbody>
          <tr v-for="l in logs" :key="l.id">
            <td>{{ l.created_at }}</td>
            <td>{{ l.username }}</td>
            <td>{{ l.action }}</td>
            <td>{{ l.target }}</td>
            <td class="wrap">{{ l.detail }}</td>
            <td>{{ l.ip }}</td>
          </tr>
          <tr v-if="!logs.length"><td colspan="6" class="empty">暂无日志</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
