// API 客户端：统一处理 {code, msg, data} 响应、JWT 认证头与 401 跳转。
import { getToken, userState } from './auth'

async function request(method, url, body) {
  const opt = { method, headers: {} }
  const token = getToken()
  if (token) {
    opt.headers['Authorization'] = 'Bearer ' + token
  }
  if (body !== undefined) {
    opt.headers['Content-Type'] = 'application/json'
    opt.body = JSON.stringify(body)
  }
  const res = await fetch(url, opt)

  const ct = res.headers.get('content-type') || ''
  if (ct.includes('json')) {
    const j = await res.json()
    if (res.status === 401) {
      userState.logout()
      throw new Error(j.msg || '未登录')
    }
    if (j.code !== 0) throw new Error(j.msg || '请求失败')
    return j.data
  }
  // 非 JSON（如 CSV 下载）
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return res
}

export const api = {
  get: (url) => request('GET', url),
  post: (url, body) => request('POST', url, body ?? {}),
  put: (url, body) => request('PUT', url, body ?? {}),
  del: (url) => request('DELETE', url)
}

// download 带 JWT 认证头下载文件（如 CSV 导出），返回 Blob。
export async function download(url) {
  const token = getToken()
  const res = await fetch(url, {
    headers: token ? { 'Authorization': 'Bearer ' + token } : {}
  })
  if (!res.ok) {
    let msg = `下载失败 (HTTP ${res.status})`
    try {
      const j = await res.json()
      msg = j.msg || msg
    } catch (e) { /* ignore */ }
    throw new Error(msg)
  }
  return res.blob()
}
