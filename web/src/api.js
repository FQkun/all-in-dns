/* ==========================================================================
   API 客户端
   - 真实模式：Go 后端 /api/*，JWT（localStorage）认证，{code,msg,data} 信封
   - 演示模式：内置 Mock（同一契约），用于无后端时预览
   ========================================================================== */
import { state, saveBlob } from './core.js';
import { Mock } from './mock.js';

const TOKEN_KEY = 'dns_token';
const params = new URLSearchParams(location.search);

export function getToken() { return localStorage.getItem(TOKEN_KEY) || ''; }
export function setToken(t) {
  if (t) localStorage.setItem(TOKEN_KEY, t);
  else localStorage.removeItem(TOKEN_KEY);
}

function headers() {
  const h = { Accept: 'application/json' };
  const t = getToken();
  if (t) h.Authorization = 'Bearer ' + t;
  return h;
}

async function request(method, path, body) {
  if (state.mode === 'demo') {
    try {
      return await Mock.handle(method, path, body);
    } catch (e) {
      throw new Error(e.message || '演示模式请求失败');
    }
  }

  const opt = { method, headers: headers(), credentials: 'same-origin' };
  if (body !== undefined && body !== null) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(path, opt);
  } catch (e) {
    throw new Error('网络请求失败：' + e.message);
  }
  const ct = res.headers.get('content-type') || '';
  if (!ct.includes('json')) {
    const text = (await res.text()).replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();
    throw new Error(`HTTP ${res.status}：${text.slice(0, 140) || '服务端返回了非 JSON 内容'}`);
  }
  const j = await res.json();
  if (res.status === 401) {
    setToken('');
    state.user = null;
    window.dispatchEvent(new Event('dns:unauth'));
    throw new Error(j.msg || '登录已失效，请重新登录');
  }
  if (j.code !== 0) throw new Error(j.msg || `请求失败（${j.code}）`);
  return j.data;
}

/** 探活：能拿到 JSON 响应就是真实后端，否则进演示模式 */
export async function detectMode() {
  if (params.get('mode') === 'demo') return 'demo';
  if (params.get('mode') === 'api') return 'api';
  if (!/^https?:$/.test(location.protocol)) return 'demo';
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 2500);
    const res = await fetch('/api/auth/me', { headers: headers(), signal: ctrl.signal });
    clearTimeout(timer);
    if ((res.headers.get('content-type') || '').includes('json')) return 'api';
  } catch (e) { /* 静态预览 */ }
  return 'demo';
}

export const Api = {
  get: p => request('GET', p),
  post: (p, b) => request('POST', p, b ?? {}),
  put: (p, b) => request('PUT', p, b ?? {}),
  del: p => request('DELETE', p),

  async login(username, password) {
    const d = await request('POST', '/api/auth/login', { username, password });
    if (d?.token) setToken(d.token);
    state.user = d?.user ?? null;
    return d;
  },
  async logout() {
    try { await request('POST', '/api/auth/logout', {}); } catch (e) { /* ignore */ }
    setToken('');
    state.user = null;
  },
  async me() {
    const d = await request('GET', '/api/auth/me');
    state.user = d?.user ?? null;
    return state.user;
  },

  /** CSV 导出：真实模式带 JWT 拉 blob；演示模式由 Mock 生成 */
  async download(path, filename) {
    if (state.mode === 'demo') {
      const { csv, name } = await Mock.handle('GET', path);
      const blob = new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' });
      saveBlob(blob, name || filename);
      return;
    }
    const res = await fetch(path, { headers: headers() });
    if (!res.ok) {
      let msg = `下载失败 (HTTP ${res.status})`;
      try { msg = (await res.json()).msg || msg; } catch (e) { /* ignore */ }
      throw new Error(msg);
    }
    const blob = await res.blob();
    saveBlob(blob, filename);
  },

  resetDemo() { Mock.reset(); },
};
