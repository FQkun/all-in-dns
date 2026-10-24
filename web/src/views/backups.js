/* 备份恢复 */
import { $, $$, esc, icon, num, tshort, toast, isAdmin } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

export async function render(el) {
  const rows = await Api.get('/api/backups').catch(() => []);
  setSub(`共 ${rows.length} 份快照 · 恢复前会自动再存一份快照`);

  el.innerHTML = `
    <div class="toolbar">
      <span class="hint">共 ${rows.length} 份快照</span>
      <span class="sp"></span>
      ${isAdmin() ? `<button class="btn danger" id="del-sel" disabled>删除所选</button>
      <button class="btn primary" id="create">${icon('plus')}创建备份</button>` : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th style="width:34px"><input type="checkbox" id="sel-all"></th><th>ID</th><th>名称</th><th style="text-align:right">记录数</th><th>创建人</th><th>时间</th><th style="text-align:right">操作</th></tr></thead>
        <tbody>${rows.map(b => `<tr>
          <td><input type="checkbox" class="bk-sel" data-id="${b.id}"></td>
          <td class="num">${b.id}</td>
          <td><b>${esc(b.name)}</b></td>
          <td class="num" style="text-align:right">${num(b.record_count)}</td>
          <td>${esc(b.created_by || '-')}</td>
          <td class="hint">${tshort(b.created_at)}</td>
          <td class="ops">
            ${isAdmin() ? `<button class="btn ghost sm" data-act="restore" data-id="${b.id}">恢复</button>
            <button class="btn ghost sm" data-act="del" data-id="${b.id}" style="color:var(--bad)">删除</button>` : ''}
          </td></tr>`).join('') || '<tr><td colspan="7" class="empty">还没有备份</td></tr>'}
        </tbody>
      </table></div>
    </div>`;

  const selIds = () => $$('.bk-sel:checked', el).map(c => +c.dataset.id);
  const syncBtn = () => { const b = $('#del-sel'); if (b) b.disabled = !selIds().length; };
  const sa = $('#sel-all');
  if (sa) sa.onclick = e => { $$('.bk-sel', el).forEach(c => { c.checked = e.target.checked; }); syncBtn(); };
  el.onchange = e => { if (e.target.classList.contains('bk-sel')) syncBtn(); };

  const create = $('#create');
  if (create) create.onclick = async e => {
    const b = e.currentTarget;
    b.disabled = true;
    try { await Api.post('/api/backups'); toast('备份已创建', 'ok'); rerender(true); }
    catch (err) { toast(err.message, 'err'); b.disabled = false; }
  };

  const delSel = $('#del-sel');
  if (delSel) delSel.onclick = async () => {
    const ids = selIds();
    if (!ids.length) return;
    if (!confirm(`删除所选 ${ids.length} 个备份？`)) return;
    try { await Api.post('/api/backups/delete', { ids }); toast('已删除', 'ok'); rerender(true); }
    catch (err) { toast(err.message, 'err'); }
  };

  el.onclick = async e => {
    const b = e.target.closest('button[data-act]');
    if (!b) return;
    if (b.dataset.act === 'restore') {
      if (!confirm('恢复会覆盖当前全部域名与解析记录，确认继续？')) return;
      b.disabled = true;
      try { await Api.post(`/api/backups/${b.dataset.id}/restore`, {}); toast('恢复完成，建议做一次一致性校验', 'ok'); rerender(true); }
      catch (err) { toast(err.message, 'err'); b.disabled = false; }
    }
    if (b.dataset.act === 'del') {
      if (!confirm('删除该备份？')) return;
      b.disabled = true;
      try { await Api.post('/api/backups/delete', { ids: [+b.dataset.id] }); toast('已删除', 'ok'); rerender(true); }
      catch (err) { toast(err.message, 'err'); b.disabled = false; }
    }
  };
}
