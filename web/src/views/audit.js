/* 审计日志：谁、什么时候、对什么做了什么 */
import { $, $$, esc, icon, num, tshort, toast, saveBlob, isAdmin } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

const ACTION_LABEL = {
  'login': '登录', 'login.failed': '登录失败', 'logout': '退出',
  'domain.create': '新建域名', 'domain.delete': '删除域名', 'domain.toggle': '平台同步开关',
  'domain.bind': '绑定账号', 'domain.unbind': '解绑账号', 'domain.verify': '一致性校验',
  'record.create': '新增记录', 'record.update': '修改记录', 'record.delete': '删除记录',
  'record.batch_delete': '批量删除', 'record.repair': '补齐记录', 'record.import': '导入记录',
  'record.import_remote': '云端拉取',
  'account.create': '新增云账号', 'account.delete': '删除云账号', 'account.test': '账号测试',
  'user.create': '新建子账号', 'user.update': '修改子账号', 'user.delete': '删除子账号',
  'backup.create': '创建备份', 'backup.delete': '删除备份', 'backup.restore': '恢复备份',
  'sync.retry': '手动重试', 'syncqueue.clear': '清空队列', 'synclog.clear': '清空日志',
  'notify.create': '新增通知渠道', 'notify.update': '修改通知渠道',
  'notify.delete': '删除通知渠道', 'notify.test': '测试通知',
  'oplog.clear': '清空审计日志',
};

const LEVEL = a => {
  if (a === 'login.failed') return 'bad';
  if (a.endsWith('.delete') || a.includes('clear')) return 'warn';
  if (a.startsWith('record.') || a.startsWith('domain.')) return 'brand';
  return 'tout';
};

let q = { page: 1, size: 50, username: '', action: '', q: '', from: '', to: '' };

export async function render(el, route, force) {
  if (!isAdmin()) {
    setSub('审计日志仅超级管理员可见');
    el.innerHTML = `<div class="card card-b" style="display:flex;gap:10px;align-items:center">
      ${icon('shield')}<span class="hint">只有管理员能查看全站操作留痕。</span></div>`;
    return;
  }
  const qs = new URLSearchParams({ page: String(q.page), size: String(q.size) });
  ['username', 'action', 'q', 'from', 'to'].forEach(k => { if (q[k]) qs.set(k, q[k]); });

  const [data, users] = await Promise.all([
    Api.get('/api/audit-logs?' + qs.toString()),
    Api.get('/api/users').catch(() => []),
  ]);
  const items = data.items || [];
  const total = data.total ?? items.length;
  const pages = Math.max(1, Math.ceil(total / q.size));
  setSub(`共 ${total} 条操作留痕 · 默认按时间倒序`);

  el.innerHTML = `
    <div class="toolbar">
      <div class="gsearch" style="width:210px;position:relative">
        ${icon('search')}
        <input id="f-q" placeholder="对象 / 详情关键字" value="${esc(q.q)}" style="padding-left:32px">
      </div>
      <select class="input" id="f-user">
        <option value="">全部账号</option>
        ${users.map(u => `<option value="${esc(u.username)}" ${q.username === u.username ? 'selected' : ''}>${esc(u.username)}</option>`).join('')}
      </select>
      <select class="input" id="f-action">
        <option value="">全部动作</option>
        ${(data.actions || []).map(a => `<option value="${esc(a)}" ${q.action === a ? 'selected' : ''}>${esc(ACTION_LABEL[a] || a)}</option>`).join('')}
      </select>
      <input class="input" id="f-from" type="date" value="${esc(q.from)}" title="开始日期">
      <input class="input" id="f-to" type="date" value="${esc(q.to)}" title="结束日期">
      <button class="btn" id="f-reset">重置</button>
      <span class="sp"></span>
      <button class="btn" id="f-export">导出本页 CSV</button>
    </div>

    <div class="card">
      <div class="table-wrap"><table class="table-dense">
        <thead><tr><th>时间</th><th>账号</th><th>动作</th><th>对象</th><th>详情</th><th>IP</th></tr></thead>
        <tbody>
          ${items.map(l => `<tr>
            <td class="hint">${tshort(l.created_at)}</td>
            <td><b>${esc(l.username || '-')}</b></td>
            <td><span class="chip ${LEVEL(l.action)}" title="${esc(l.action)}">${esc(ACTION_LABEL[l.action] || l.action)}</span></td>
            <td class="wrap">${esc(l.target || '-')}</td>
            <td class="wrap hint mono" style="font-size:11.5px">${esc(l.detail || '-')}</td>
            <td class="mono hint">${esc(l.ip || '-')}</td>
          </tr>`).join('') || '<tr><td colspan="6" class="empty">没有符合条件的记录</td></tr>'}
        </tbody>
      </table></div>
      <div class="pager">
        <span class="hint">第 ${q.page} / ${pages} 页 · 共 ${total} 条</span>
        <span class="sp"></span>
        <button class="btn ghost sm" id="pg-prev" ${q.page <= 1 ? 'disabled' : ''}>上一页</button>
        <button class="btn ghost sm" id="pg-next" ${q.page >= pages ? 'disabled' : ''}>下一页</button>
      </div>
    </div>

    <div class="card card-b">
      <div class="hint" style="line-height:1.9">
        <b style="color:var(--ink-2)">审计说明</b><br>
        · 记录登录、域名与解析记录的增删改、同步开关、云账号变更、子账号变更、备份与恢复等操作<br>
        · 每条包含操作人、动作、对象、来源 IP 与时间；敏感值（密钥、密码）不会进入日志<br>
        · 日志只增不删，清空操作本身也会留下记录
      </div>
    </div>`;

  const reload = () => rerender(true);
  const applyFilter = () => {
    q.username = $('#f-user').value;
    q.action = $('#f-action').value;
    q.q = $('#f-q').value.trim();
    q.from = $('#f-from').value;
    q.to = $('#f-to').value;
    q.page = 1;
    reload();
  };

  let timer = null;
  $('#f-q').oninput = () => { clearTimeout(timer); timer = setTimeout(applyFilter, 320); };
  $('#f-user').onchange = applyFilter;
  $('#f-action').onchange = applyFilter;
  $('#f-from').onchange = applyFilter;
  $('#f-to').onchange = applyFilter;
  $('#f-reset').onclick = () => {
    q = { page: 1, size: 50, username: '', action: '', q: '', from: '', to: '' };
    reload();
  };
  $('#pg-prev').onclick = () => { q.page = Math.max(1, q.page - 1); reload(); };
  $('#pg-next').onclick = () => { q.page = Math.min(pages, q.page + 1); reload(); };

  $('#f-export').onclick = () => {
    if (!items.length) return toast('当前没有可导出的记录', 'err');
    const head = ['时间', '账号', '动作', '对象', '详情', 'IP'];
    const rows = items.map(l => [l.created_at, l.username || '', ACTION_LABEL[l.action] || l.action, l.target || '', l.detail || '', l.ip || '']);
    const csv = [head, ...rows].map(r => r.map(v => `"${String(v).replace(/"/g, '""')}"`).join(',')).join('\n');
    saveBlob(new Blob(['\ufeff' + csv], { type: 'text/csv;charset=utf-8' }), `audit_logs_p${q.page}.csv`);
    toast(`已导出本页 ${items.length} 条`, 'ok');
  };
}
