/* 操作日志 */
import { $, esc, tshort, toast, isAdmin } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

export async function render(el) {
  const logs = await Api.get('/api/operation-logs').catch(() => []);
  setSub(`最近 ${logs.length} 条操作留痕`);

  el.innerHTML = `
    <div class="toolbar">
      <span class="hint">最近 ${logs.length} 条记录</span>
      <span class="sp"></span>
      ${isAdmin() ? '<button class="btn danger" id="clear">清空日志</button>' : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>时间</th><th>用户</th><th>动作</th><th>对象</th><th>详情</th><th>IP</th></tr></thead>
        <tbody>${logs.map(l => `<tr>
          <td class="hint">${tshort(l.created_at)}</td>
          <td><b>${esc(l.username || '-')}</b></td>
          <td><span class="type-chip">${esc(l.action)}</span></td>
          <td class="wrap">${esc(l.target || '-')}</td>
          <td class="wrap hint">${esc(l.detail || '-')}</td>
          <td class="mono hint">${esc(l.ip || '-')}</td>
        </tr>`).join('') || '<tr><td colspan="6" class="empty">暂无操作日志</td></tr>'}</tbody>
      </table></div>
    </div>`;

  const c = $('#clear');
  if (c) c.onclick = async () => {
    if (!confirm('清空全部操作日志？')) return;
    c.disabled = true;
    try { await Api.del('/api/operation-logs'); toast('已清空', 'ok'); rerender(true); }
    catch (e) { toast(e.message, 'err'); c.disabled = false; }
  };
}
