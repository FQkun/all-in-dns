/* 同步中心：重试队列 + 同步日志 */
import { $, $$, esc, tshort, num, provChip, ACT_LABEL, toast, isAdmin, canOperate } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

let tab = 'queue';

export async function render(el) {
  const [queue, logs] = await Promise.all([
    Api.get('/api/sync/queue').catch(() => []),
    Api.get('/api/sync/logs').catch(() => []),
  ]);
  const failed = queue.filter(q => q.status === 'failed').length;
  setSub(`队列 ${queue.length} 个任务${failed ? ` · ${failed} 个失败` : ''} · 失败任务按指数退避重试，最多 5 次`);

  const queueHtml = queue.length ? `<div class="table-wrap"><table>
      <thead><tr><th>ID</th><th>平台</th><th>动作</th><th>记录</th><th>状态</th><th>已重试</th><th>下次重试</th><th>错误</th></tr></thead>
      <tbody>${queue.map(q => {
        let rec = '-';
        try {
          const p = JSON.parse(q.payload || '{}');
          if (p.record) rec = `${p.domain || ''} ${p.record.sub ?? p.record.sub_domain ?? ''} ${p.record.type || ''} ${p.record.value || ''}`.trim();
        } catch (e) { /* ignore */ }
        return `<tr><td class="num">${q.id}</td><td>${provChip(q.provider)}</td>
          <td>${ACT_LABEL[q.action] || esc(q.action)}</td><td class="wrap mono">${esc(rec)}</td>
          <td><span class="chip ${q.status === 'failed' ? 'bad' : 'warn'}"><i class="dot"></i>${q.status === 'failed' ? '失败' : '待执行'}</span></td>
          <td class="num">${q.retry_count}</td><td class="hint">${tshort(q.next_retry_at) || '立即'}</td>
          <td class="wrap" style="color:var(--ink-2)">${esc(q.last_error || '-')}</td></tr>`;
      }).join('')}</tbody></table></div>` : '<div class="empty">队列为空，所有同步任务都已执行完毕</div>';

  const logsHtml = logs.length ? `<div class="table-wrap"><table>
      <thead><tr><th>时间</th><th>平台</th><th>账号</th><th>记录</th><th>动作</th><th>结果</th><th>消息</th><th style="text-align:right">耗时</th></tr></thead>
      <tbody>${logs.map(l => `<tr>
        <td class="hint">${tshort(l.created_at)}</td><td>${provChip(l.provider)}</td>
        <td class="hint">${esc(l.account_name || '-' + (l.account_id ?? ''))}</td>
        <td class="num">#${num(l.record_id)}</td><td>${ACT_LABEL[l.action] || esc(l.action)}</td>
        <td><span class="chip ${l.status === 'success' ? 'ok' : 'bad'}"><i class="dot"></i>${l.status === 'success' ? '成功' : '失败'}</span></td>
        <td class="wrap" style="color:${l.status === 'failed' ? 'var(--bad)' : 'var(--ink-2)'}">${esc(l.message || '-')}</td>
        <td class="num" style="text-align:right">${num(l.request_cost_ms)}ms</td>
      </tr>`).join('')}</tbody></table></div>` : '<div class="empty">暂无同步日志</div>';

  el.innerHTML = `
    <div class="toolbar">
      <span class="hint">队列 ${queue.length} 个任务${failed ? ` · <b style="color:var(--bad)">${failed} 个失败</b>` : ''}</span>
      <span class="sp"></span>
      ${canOperate() ? `<button class="btn primary" id="retry">立即重试${failed ? `（${failed} 个失败）` : ''}</button>
      <button class="btn danger" id="clear-queue">清空队列</button>` : ''}
      ${isAdmin() ? '<button class="btn danger" id="clear-logs">清空日志</button>' : ''}
    </div>
    <div class="card">
      <div class="tabs">
        <button data-tab="queue" class="${tab === 'queue' ? 'active' : ''}">重试队列<span class="cnt">${queue.length}</span></button>
        <button data-tab="logs" class="${tab === 'logs' ? 'active' : ''}">同步日志<span class="cnt">${logs.length}</span></button>
      </div>
      ${tab === 'queue' ? queueHtml : logsHtml}
    </div>`;

  $$('.tabs button', el).forEach(b => {
    b.onclick = () => { tab = b.dataset.tab; rerender(); };
  });

  const retry = $('#retry');
  if (retry) retry.onclick = async e => {
    const b = e.currentTarget;
    b.disabled = true;
    try {
      const r = await Api.post('/api/sync/retry-now');
      toast(`已重新执行 ${r.count ?? 0} 个任务`, 'ok');
      rerender(true);
    } catch (err) { toast(err.message, 'err'); b.disabled = false; }
  };

  const cq = $('#clear-queue');
  if (cq) cq.onclick = async e => {
    if (!confirm('清空待重试队列？失败任务将不再自动重试')) return;
    const b = e.currentTarget;
    b.disabled = true;
    try { const r = await Api.del('/api/sync/queue'); toast(`已清空 ${r.count ?? 0} 个任务`, 'ok'); rerender(true); }
    catch (err) { toast(err.message, 'err'); b.disabled = false; }
  };

  const cl = $('#clear-logs');
  if (cl) cl.onclick = async e => {
    if (!confirm('清空全部同步日志？')) return;
    const b = e.currentTarget;
    b.disabled = true;
    try { await Api.del('/api/sync/logs'); toast('已清空', 'ok'); rerender(true); }
    catch (err) { toast(err.message, 'err'); b.disabled = false; }
  };
}
