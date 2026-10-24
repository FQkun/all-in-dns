<script setup>
import { ref } from 'vue'
import { api } from '../api'
import { userState } from '../auth'
import { toast } from '../ui'

const username = ref('')
const password = ref('')
const loading = ref(false)

async function submit() {
  if (loading.value) return
  loading.value = true
  try {
    const data = await api.post('/api/auth/login', {
      username: username.value,
      password: password.value
    })
    userState.setAuth(data.token, data.user)
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <form class="login-card" @submit.prevent="submit">
      <h2>欢迎回来</h2>
      <input v-model="username" name="username" placeholder="用户名" required autocomplete="username">
      <input v-model="password" name="password" type="password" placeholder="密码" required autocomplete="current-password">
      <button class="btn primary" type="submit" :disabled="loading">登录</button>
      <p class="author-line">开源项目 <a href="https://github.com/FQkun/all-in-dns" target="_blank" rel="noopener">FQkun/all-in-dns</a></p>
    </form>
  </div>
</template>
