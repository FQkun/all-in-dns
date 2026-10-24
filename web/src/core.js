/* ==========================================================================
   基础工具层：DOM / 转义 / 厂商元数据 / Toast / Modal / 主题
   ========================================================================== */

export const $ = (s, r = document) => r.querySelector(s);
export const $$ = (s, r = document) => [...r.querySelectorAll(s)];
export const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
export const tshort = s => (s ? String(s).replace('T', ' ').slice(5, 16) : '-');
export const num = v => (v === null || v === undefined || v === '' ? '-' : v);

export const PROV = {
  tencent: { label: '腾讯云', cls: 'p-t' },
  aliyun: { label: '阿里云', cls: 'p-a' },
  volc: { label: '火山引擎', cls: 'p-v' },
  spaceship: { label: 'Spaceship', cls: 'p-s' },
};
export const PROV_SHORT = { tencent: '腾讯', aliyun: '阿里', volc: '火山', spaceship: 'Ship' };
export const PROVIDERS = ['tencent', 'aliyun', 'volc', 'spaceship'];
export const provLabel = p => (PROV[p] ? PROV[p].label : p || '-');
export const provShort = p => PROV_SHORT[p] || provLabel(p);
export const provChip = p => `<span class="chip ${PROV[p] ? PROV[p].cls : ''}"><i class="dot"></i>${esc(provLabel(p))}</span>`;

export const ACT_LABEL = { create: '新增', update: '修改', delete: '删除' };
export const DOMAIN_STATUS = {
  active: ['生效中', 'ok'], pending: ['待校验', 'warn'], error: ['异常', 'bad'], disabled: ['停用', 'tout'],
};
export const TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA'];
export const LINES = ['默认', '电信', '联通', '移动', '境外'];
export const NOTIFY_TYPES = { wecom: '企业微信', feishu: '飞书', dingtalk: '钉钉', webhook: '通用 Webhook' };

export const state = { user: null, mode: 'api', token: '' };
export const isAdmin = () => state.user?.role === 'admin';
export const canOperate = () => ['admin', 'operator'].includes(state.user?.role);

const ICONS = {
  grid: '<rect x="3" y="3" width="7" height="7" rx="1.6"/><rect x="14" y="3" width="7" height="7" rx="1.6"/><rect x="3" y="14" width="7" height="7" rx="1.6"/><rect x="14" y="14" width="7" height="7" rx="1.6"/>',
  globe: '<circle cx="12" cy="12" r="9.5"/><path d="M2.5 12h19"/><path d="M12 2.5a15 15 0 0 1 0 19a15 15 0 0 1 0-19"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13"/><circle cx="3.5" cy="6" r="1.2"/><circle cx="3.5" cy="12" r="1.2"/><circle cx="3.5" cy="18" r="1.2"/>',
  cloud: '<path d="M17.5 19H9a7 7 0 1 1 6.7-9h1.8a4.5 4.5 0 1 1 0 9Z"/>',
  refresh: '<path d="M3 12a9 9 0 0 1 9-9 9.7 9.7 0 0 1 6.7 2.7L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.7 9.7 0 0 1-6.7-2.7L3 16"/><path d="M8 16H3v5"/>',
  doc: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M9 15h6"/><path d="M9 11h2"/>',
  box: '<rect x="2.5" y="4" width="19" height="4.5" rx="1.4"/><path d="M4.5 8.5V19a2 2 0 0 0 2 2h11a2 2 0 0 0 2-2V8.5"/><path d="M10 13h4"/>',
  bell: '<path d="M18 8a6 6 0 1 0-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.7 21a2 2 0 0 1-3.4 0"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M21 12.8A8.5 8.5 0 1 1 11.2 3a6.6 6.6 0 0 0 9.8 9.8Z"/>',
  out: '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><path d="m16 17 5-5-5-5"/><path d="M21 12H9"/>',
  bolt: '<path d="M13 2 4.5 13.5H11l-1 8.5 8.5-11.5H12l1-8.5Z"/>',
  shield: '<path d="M12 2.5 20 6v6c0 5-3.4 8.4-8 9.5-4.6-1.1-8-4.5-8-9.5V6l8-3.5Z"/><path d="m9 12 2 2 4-4"/>',
  spark: '<path d="M12 3v3M12 18v3M4.5 12h3M16.5 12h3M6.8 6.8l2.1 2.1M15.1 15.1l2.1 2.1M6.8 17.2l2.1-2.1M15.1 8.9l2.1-2.1"/><circle cx="12" cy="12" r="3"/>',
  radar: '<circle cx="12" cy="12" r="9.5"/><circle cx="12" cy="12" r="5"/><circle cx="12" cy="12" r="1.2"/><path d="M12 12 18 6"/>',
  link: '<path d="M9.5 14.5 14.5 9.5"/><rect x="3" y="10" width="8" height="8" rx="2.5" transform="rotate(-45 7 14)"/><rect x="13" y="6" width="8" height="8" rx="2.5" transform="rotate(-45 17 10)"/>',
  send: '<path d="M21 3 10.5 13.5"/><path d="M21 3l-6.5 18-4-8-8-4L21 3Z"/>',
  mic: '<rect x="9" y="2.5" width="6" height="11" rx="3"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0"/><path d="M12 17.5V21"/><path d="M8.5 21h7"/>',
  stop: '<rect x="7" y="7" width="10" height="10" rx="2"/>',
  close: '<path d="M6 6l12 12M18 6 6 18"/>',
};

export function icon(name, cls = '') {
  return `<svg class="${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name] || ''}</svg>`;
}

/* ---------------------------------- Toast --------------------------------- */
export function toast(msg, type = '') {
  const box = $('#toasts');
  if (!box) return;
  const el = document.createElement('div');
  el.className = 'toast ' + type;
  el.textContent = msg;
  box.appendChild(el);
  setTimeout(() => el.remove(), 3400);
}

export function toastSync(sync) {
  if (!Array.isArray(sync) || !sync.length) return;
  const bad = sync.filter(s => s.status !== 'success');
  if (bad.length) toast(`已保存，${bad.length} 个平台同步失败（进入重试队列）`, 'err');
  else toast('已保存并同步全部成功', 'ok');
}

/* ---------------------------------- Modal --------------------------------- */
export function modal(opts) {
  const root = $('#modal-root');
  const wrap = document.createElement('div');
  wrap.className = 'modal-mask';
  wrap.innerHTML = `<div class="modal ${opts.wide ? 'wide' : ''}">
      <div class="modal-h"><h3>${esc(opts.title || '')}</h3>
        <button class="btn ghost icon sm" data-close aria-label="关闭">✕</button></div>
      <div class="modal-b">${opts.body || ''}</div>
      <div class="modal-f"></div>
    </div>`;
  root.appendChild(wrap);

  let closed = false;
  const close = () => { if (!closed) { closed = true; wrap.remove(); opts.onClose?.(); } };
  wrap.addEventListener('mousedown', e => { if (e.target === wrap) close(); });
  wrap.querySelector('[data-close]').onclick = close;

  const foot = $('.modal-f', wrap);
  (opts.actions || [{ label: '关闭' }]).forEach(a => {
    const b = document.createElement('button');
    b.className = 'btn ' + (a.cls || '');
    b.textContent = a.label;
    b.onclick = async () => {
      if (b.disabled) return;
      b.disabled = true;
      try {
        const res = a.onClick ? await a.onClick(wrap, close) : undefined;
        if (res !== false) close();
        else b.disabled = false;
      } catch (e) {
        toast(e.message, 'err');
        b.disabled = false;
      }
    };
    foot.appendChild(b);
  });
  setTimeout(() => {
    const first = wrap.querySelector('input:not([type=checkbox]):not([type=hidden]),select,textarea');
    if (first && opts.focus !== false) first.focus();
  }, 40);
  return { wrap, close };
}

/* ---------------------------------- 主题 ---------------------------------- */
const THEME_KEY = 'dns_console_theme';
export function applyTheme(mode) {
  document.documentElement.dataset.theme = mode;
  localStorage.setItem(THEME_KEY, mode);
  const btn = $('#btn-theme');
  if (btn) btn.innerHTML = icon(mode === 'dark' ? 'sun' : 'moon');
}
export function initTheme() {
  const q = new URLSearchParams(location.search).get('theme');
  applyTheme(q === 'dark' || q === 'light' ? q : (localStorage.getItem(THEME_KEY) || 'light'));
}

/* -------------------------------- CSV 下载 -------------------------------- */
export function saveBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename || 'download.csv';
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 4000);
}
