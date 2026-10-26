/* ==========================================================================
   应用外壳：侧栏导航 / 顶栏 / 哈希路由 / 登录页 / 启动
   ========================================================================== */
import './styles/console.css';
import { $, $$, esc, icon, state, initTheme, applyTheme, toast } from './core.js';
import { Api, detectMode } from './api.js';
import { mountAssistant } from './ai.js';
import { currentRoute } from './router-state.js';

import * as dashboard from './views/dashboard.js';
import * as domains from './views/domains.js';
import * as records from './views/records.js';
import * as accounts from './views/accounts.js';
import * as sync from './views/sync.js';
import * as backups from './views/backups.js';
import * as notify from './views/notify.js';
import * as users from './views/users.js';
import * as audit from './views/audit.js';

const NAV = [
  { group: '总览', items: [{ key: 'dashboard', label: '监控概览', href: '#/dashboard', icon: 'grid' }] },
  {
    group: '资源', items: [
      { key: 'domains', label: '域名管理', href: '#/domains', icon: 'globe' },
      { key: 'accounts', label: '云账号', href: '#/accounts', icon: 'cloud' },
    ]
  },
  {
    group: '运维', items: [
      { key: 'sync', label: '同步中心', href: '#/sync', icon: 'refresh' },
      { key: 'backups', label: '备份恢复', href: '#/backups', icon: 'box' },
    ]
  },
  {
    group: '系统', items: [
      { key: 'users', label: '账号与权限', href: '#/users', icon: 'users', admin: true },
      { key: 'audit', label: '审计日志', href: '#/audit', icon: 'audit', admin: true },
      { key: 'notify', label: '消息通知', href: '#/notify', icon: 'bell' },
    ]
  },
];

const VIEWS = { dashboard, domains, records, accounts, sync, backups, notify, users, audit };
const TITLES = {
  dashboard: ['监控概览', '四端同步状态、失败任务与解析生效情况一览'],
  domains: ['域名管理', ''],
  records: ['解析记录', ''],
  accounts: ['云账号', ''],
  sync: ['同步中心', '失败任务按指数退避自动重试，最多 5 次'],
  backups: ['备份恢复', '快照包含域名、绑定关系与全部解析记录'],
  notify: ['消息通知', '同步失败 / 验证失败 / 备份完成时推送到群机器人'],
  users: ['账号与权限', ''],
  audit: ['审计日志', '全站操作留痕：谁、什么时候、对什么做了什么'],
};

let SELF_ROUTE = null;

/* ---------------------------------- 外壳 ---------------------------------- */
function renderShell() {
  const initial = (state.user?.username || 'A').slice(0, 1).toUpperCase();
  const roleText = state.user?.role === 'admin' ? '管理员' : state.user?.role === 'operator' ? '运维' : '只读';
  const navHtml = NAV.map(g => {
    const items = g.items.filter(it => !it.admin || state.user?.role === 'admin');
    if (!items.length) return '';
    return `<div class="nav-label">${g.group}</div>` + items.map(it =>
      `<a href="${it.href}" data-key="${it.key}">${icon(it.icon)}<span>${it.label}</span></a>`).join('');
  }).join('');

  $('#app').innerHTML = `
  <div class="shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark">${icon('bolt')}</div>
        <div><b>DNS 控制台</b><small>四云统一解析</small></div>
      </div>
      <nav class="nav" id="nav">${navHtml}</nav>
      <div class="sb-foot">
        <div class="mode-pill" id="mode-pill"></div>
        <div class="user-row">
          <div class="avatar">${esc(initial)}</div>
          <div class="uinfo"><b>${esc(state.user?.username || '-')}</b><small>${roleText}</small></div>
          <button class="btn ghost icon sm" id="btn-logout" title="退出登录">${icon('out')}</button>
        </div>
      </div>
    </aside>
    <div class="main">
      <header class="topbar">
        <div class="tb-title"><h1 id="pg-title">监控概览</h1><p id="pg-sub"></p></div>
        <div class="tb-spacer"></div>
        <button class="btn ghost icon" id="btn-ai" title="AI 诊断助手">${icon('spark')}</button>
        <button class="btn ghost icon" id="btn-theme" title="切换主题"></button>
        <button class="btn ghost icon" id="btn-refresh" title="刷新">${icon('refresh')}</button>
      </header>
      <main class="view" id="view"></main>
    </div>
  </div>`;

  $('#btn-logout').onclick = async () => { await Api.logout(); location.href = location.pathname; };
  $('#btn-theme').onclick = () => {
    const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
    applyTheme(next);
    render();
  };
  $('#btn-refresh').onclick = () => render(true);
  $('#btn-ai').onclick = () => window.__aiToggle?.();
  $('#nav').onclick = e => {
    const a = e.target.closest('a');
    if (!a) return;
    e.preventDefault();
    location.hash = a.getAttribute('href').slice(1);
  };
  applyTheme(document.documentElement.dataset.theme || 'light');
  renderModePill();
}

function renderModePill() {
  const el = $('#mode-pill');
  if (!el) return;
  if (state.mode === 'demo') {
    el.classList.add('show');
    el.innerHTML = `<b>演示模式</b>· 内置 Mock 数据 <a href="#" id="reset-demo" style="margin-left:auto">重置</a>`;
    $('#reset-demo').onclick = e => {
      e.preventDefault();
      Api.resetDemo();
      render(true);
    };
  } else {
    el.classList.remove('show');
  }
}

/* ---------------------------------- 路由 ---------------------------------- */
function parseHash() {
  const raw = (location.hash || '#/dashboard').replace(/^#/, '');
  const [path, qs] = raw.split('?');
  const query = {};
  new URLSearchParams(qs || '').forEach((v, k) => { query[k] = v; });
  let view = 'dashboard', id = null;
  if (/^\/domains\/(\d+)/.test(path)) { view = 'records'; id = +path.match(/^\/domains\/(\d+)/)[1]; }
  else if (path.startsWith('/domains')) view = 'domains';
  else if (path.startsWith('/accounts')) view = 'accounts';
  else if (path.startsWith('/sync')) view = 'sync';
  else if (path.startsWith('/oplogs')) view = 'oplogs';
  else if (path.startsWith('/backups')) view = 'backups';
  else if (path.startsWith('/oplogs')) view = 'audit'; // 旧入口并入审计日志
  else if (path.startsWith('/notify')) view = 'notify';
  else if (path.startsWith('/users')) view = 'users';
  else if (path.startsWith('/audit')) view = 'audit';
  return { view, id, query };
}

export async function render(force) {
  const r = parseHash();
  SELF_ROUTE = r;
  Object.assign(currentRoute, r);
  const el = $('#view');
  if (!el) return;

  const activeKey = r.view === 'records' ? 'domains' : r.view;
  $$('#nav a').forEach(a => a.classList.toggle('active', a.dataset.key === activeKey));
  $('#pg-title').textContent = TITLES[r.view][0];
  $('#pg-sub').textContent = TITLES[r.view][1];

  el.innerHTML = `<div class="card card-b"><div class="skeleton" style="width:40%;margin-bottom:10px"></div><div class="skeleton" style="width:70%"></div></div>`;
  try {
    await VIEWS[r.view].render(el, r, force);
  } catch (e) {
    el.innerHTML = `<div class="load-error">${esc(e.message)}</div>`;
  }
}

export function go(hash) { location.hash = hash; }
export function setSub(text) { const p = $('#pg-sub'); if (p) p.textContent = text; }
export function currentDomainId() { return currentRoute.view === 'records' ? currentRoute.id : null; }

/* --------------------------------- 登录页 --------------------------------- */
function renderLogin() {
  $('#app').innerHTML = `
    <div class="login-wrap">
      <form class="login-card" id="login-form">
        <div class="brand">
          <div class="brand-mark">${icon('bolt')}</div>
          <div><b>DNS 控制台</b><small>腾讯云 · 阿里云 · 火山引擎 · Spaceship</small></div>
        </div>
        <h2>登录控制台</h2>
        <p class="sub">一处修改，四端同步；失败自动重试，全程留痕</p>
        <div class="field"><label>用户名</label><input class="input" name="username" autocomplete="username" placeholder="admin"></div>
        <div class="field"><label>密码</label><input class="input" name="password" type="password" autocomplete="current-password" placeholder="••••••"></div>
        <button class="btn primary" type="submit" id="login-btn">登 录</button>
        <p class="login-foot">开源项目 FQkun/all-in-dns · 界面二次开发版</p>
      </form>
    </div>`;

  const form = $('#login-form');
  form.onsubmit = async e => {
    e.preventDefault();
    const fd = new FormData(form);
    const btn = $('#login-btn');
    btn.disabled = true;
    btn.textContent = '登录中…';
    try {
      await Api.login(String(fd.get('username') || ''), String(fd.get('password') || ''));
      await boot(true);
    } catch (err) {
      toast(err.message, 'err');
      btn.disabled = false;
      btn.textContent = '登 录';
    }
  };
  if (state.mode === 'demo') {
    const tip = document.createElement('div');
    tip.className = 'login-demo';
    tip.textContent = '演示模式：任意用户名 / 密码均可登录（建议 admin）';
    form.appendChild(tip);
    $('input[name=username]').value = 'admin';
    $('input[name=password]').value = '123456';
  }
}

/* --------------------------------- 启动 ---------------------------------- */
async function boot(skipMe) {
  state.mode = await detectMode();
  const forceLogin = new URLSearchParams(location.search).has('login');
  if (!skipMe && !forceLogin) {
    try { await Api.me(); } catch (e) { state.user = null; }
  }
  if (!state.user) { renderLogin(); return; }

  renderShell();
  mountAssistant();
  if (!location.hash) location.hash = '/dashboard';
  await render(true);
}

window.addEventListener('hashchange', () => { if (state.user) render(); });
window.addEventListener('dns:unauth', () => { state.user = null; renderLogin(); });
document.addEventListener('keydown', e => {
  if (e.key === 'Escape') {
    const masks = $$('#modal-root .modal-mask');
    if (masks.length) masks[masks.length - 1].remove();
  }
});

initTheme();
boot();
