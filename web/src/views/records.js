/* 解析记录：筛选 / 增删改 / 批量 / CSV / 从云端拉取 */
import { $, $$, esc, icon, num, tshort, provShort, provLabel, TYPES, LINES, toast, modal, toastSync, canOperate } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub, go } from '../app.js';

let ctx = { domainId: 0, page: 1, q: '', type: 'ALL', status: 'all' };

export async function render(el, route, force) {
  const domainId = route.id;
  if (ctx.domainId !== domainId) ctx = { domainId, page: 1, q: '', type: 'ALL', status: 'all' };

  const doms = await Api.get('/api/domains').catch(() => []);
  const domain = doms.find(d => d.id === domainId);
  const size = 100;

  const res = await Api.get(`/api/domains/${domainId}/records?page=${ctx.page}&size=${size}`);
  const items = res.items || [];
  const total = res.total ?? items.length;
  const pages = Math.max(1, Math.ceil(total / size));

  const view = items.filter(r => {
    if (ctx.type !== 'ALL' && r.type !== ctx.type) return false;
    if (ctx.status !== 'all' && r.status !== ctx.status) return false;
    if (ctx.q) {
      const kw = ctx.q.toLowerCase();
      if (!(r.sub_domain || '').toLowerCase().includes(kw) && !(r.value || '').toLowerCase().includes(kw) && !(r.line || '').includes(ctx.q)) return false;
    }
    return true;
  });

  setSub(`共 ${total} 条记录 · 修改后自动下发到各云平台`);

  const syncCell = r => {
    const parts = String(r.sync_states || '').split(',').filter(Boolean);
    if (!parts.length) return '<span class="chip tout">未同步</span>';
    return parts.map(p => {
      const arr = p.split('|');
      const [aid, prov, st] = arr.length > 2 ? arr : ['', arr[0], arr[1]];
      const cls = st === 'success' ? 'sync-success' : st === 'failed' ? 'sync-failed' : 'sync-pending';
      const txt = st === 'success' ? '成功' : st === 'failed' ? '失败' : '待同步';
      return `<span class="chip ${cls}" title="${esc(provLabel(prov))}${aid ? ' · 账号#' + aid : ''} · ${txt}">${esc(provShort(prov))}</span>`;
    }).join(' ');
  };

  el.innerHTML = `
    <div class="toolbar">
      <button class="btn ghost" id="back">← 返回域名</button>
      <span class="sp"></span>
      ${canOperate() ? `<button class="btn" id="pull">从云平台拉取</button>
      <button class="btn" id="import">导入 CSV</button>
      <button class="btn primary" id="add-rec">${icon('plus')}添加记录</button>` : ''}
      <button class="btn" id="export">导出 CSV</button>
    </div>

    <div class="toolbar">
      <div class="gsearch" style="width:240px;position:relative">
        ${icon('search')}
        <input id="rec-q" placeholder="本页搜索：主机 / 记录值" value="${esc(ctx.q)}" style="padding-left:32px">
      </div>
      <select class="input" id="rec-type">
        <option value="ALL">全部类型</option>
        ${TYPES.map(t => `<option ${ctx.type === t ? 'selected' : ''}>${t}</option>`).join('')}
      </select>
      <select class="input" id="rec-status">
        <option value="all" ${ctx.status === 'all' ? 'selected' : ''}>全部状态</option>
        <option value="active" ${ctx.status === 'active' ? 'selected' : ''}>生效中</option>
        <option value="disabled" ${ctx.status === 'disabled' ? 'selected' : ''}>待清理</option>
      </select>
      <span class="sp"></span>
      ${canOperate() ? '<button class="btn danger" id="batch-del">删除选中</button>' : ''}
    </div>

    <div class="card">
      <div class="table-wrap"><table class="table-dense">
        <thead><tr>
          <th style="width:34px"><input type="checkbox" id="chk-all"></th>
          <th>主机</th><th>类型</th><th>线路</th><th>记录值</th><th>TTL</th><th>优先级</th>
          <th title="绿=成功 红=失败 黄=待同步">多端同步</th><th>状态</th><th>更新时间</th><th style="text-align:right">操作</th>
        </tr></thead>
        <tbody id="rec-body">
          ${view.map(r => `<tr data-id="${r.id}">
            <td><input type="checkbox" class="row-chk" value="${r.id}"></td>
            <td><b>${esc(r.sub_domain)}</b></td>
            <td><span class="type-chip">${esc(r.type)}</span></td>
            <td class="hint">${esc(r.line)}</td>
            <td class="wrap mono">${esc(r.value)}</td>
            <td class="num">${r.ttl}</td>
            <td class="num">${num(r.priority)}</td>
            <td>${syncCell(r)}</td>
            <td>${r.status === 'disabled' ? '<span class="chip warn"><i class="dot"></i>待清理</span>' : '<span class="chip ok"><i class="dot"></i>生效中</span>'}</td>
            <td class="hint">${tshort(r.updated_at)}</td>
            <td class="ops">
              ${canOperate() ? `<button class="btn ghost sm" data-act="edit" data-id="${r.id}">编辑</button>
              <button class="btn ghost sm" data-act="del" data-id="${r.id}" style="color:var(--bad)">删除</button>` : '<span class="hint">只读</span>'}
            </td>
          </tr>`).join('') || '<tr><td colspan="11" class="empty">没有符合条件的记录</td></tr>'}
        </tbody>
      </table></div>
      <div class="pager">
        <span class="hint">第 ${ctx.page} / ${pages} 页 · 共 ${total} 条${ctx.q || ctx.type !== 'ALL' || ctx.status !== 'all' ? `（本页筛选出 ${view.length} 条）` : ''}</span>
        <span class="sp"></span>
        <button class="btn ghost sm" id="pg-prev" ${ctx.page <= 1 ? 'disabled' : ''}>上一页</button>
        <button class="btn ghost sm" id="pg-next" ${ctx.page >= pages ? 'disabled' : ''}>下一页</button>
      </div>
    </div>`;

  const reload = () => rerender(true);
  $('#back').onclick = () => go('/domains');
  $('#pg-prev').onclick = () => { ctx.page = Math.max(1, ctx.page - 1); reload(); };
  $('#pg-next').onclick = () => { ctx.page = Math.min(pages, ctx.page + 1); reload(); };

  let timer = null;
  $('#rec-q').oninput = e => {
    clearTimeout(timer);
    timer = setTimeout(() => { ctx.q = e.target.value.trim(); reload(); }, 320);
  };
  $('#rec-type').onchange = e => { ctx.type = e.target.value; reload(); };
  $('#rec-status').onchange = e => { ctx.status = e.target.value; reload(); };
  $('#chk-all').onclick = e => $$('.row-chk', el).forEach(c => { c.checked = e.target.checked; });

  const addRec = $('#add-rec');
  if (addRec) addRec.onclick = () => recordForm(domainId, null);

  $('#export').onclick = () => Api.download(`/api/domains/${domainId}/export`, `records_${domain ? domain.name : domainId}.csv`)
    .catch(e => toast(e.message, 'err'));

  const pull = $('#pull');
  if (pull) pull.onclick = async e => {
    const b = e.currentTarget;
    b.disabled = true;
    try {
      const r = await Api.post(`/api/domains/${domainId}/import-remote`);
      toast(`已拉取 ${r.imported} 条${r.merged ? `，合并 ${r.merged} 条` : ''}`, 'ok');
      if (r.errors && r.errors.length) toast('部分平台失败：' + r.errors.join('；'), 'err');
      reload();
    } catch (err) { toast(err.message, 'err'); b.disabled = false; }
  };

  const imp = $('#import');
  if (imp) imp.onclick = () => modal({
    title: '导入解析记录',
    body: `<div class="field"><label>CSV 内容</label>
        <textarea class="input" id="f-csv" rows="9" placeholder="sub_domain,type,line,value,ttl,priority
@,A,默认,1.2.3.4,600,
www,CNAME,默认,example.com,600,"></textarea>
        <p class="f-hint">首行必须是表头；重复或冲突的记录会自动跳过</p></div>`,
    actions: [{ label: '取消', cls: 'ghost' }, {
      label: '开始导入', cls: 'primary', onClick: async w => {
        const csv = $('#f-csv', w).value;
        if (!csv.trim()) throw new Error('请粘贴 CSV 内容');
        const r = await Api.post(`/api/domains/${domainId}/import`, { csv });
        toast(`导入 ${r.imported} 条，跳过 ${r.skipped} 条${r.failed ? `，失败 ${r.failed} 条` : ''}`, 'ok');
        reload();
      }
    }]
  });

  const batch = $('#batch-del');
  if (batch) batch.onclick = async e => {
    const ids = $$('.row-chk:checked', el).map(c => +c.value);
    if (!ids.length) return toast('请先勾选记录', 'err');
    if (!confirm(`确认删除选中的 ${ids.length} 条记录？会同步从云平台删除`)) return;
    const b = e.currentTarget;
    b.disabled = true;
    try {
      const r = await Api.post('/api/records/batch-delete', { ids });
      const pending = r.pending || [];
      toast(pending.length ? `已删除，${pending.length} 条等待云端重试` : '已删除', pending.length ? 'err' : 'ok');
      reload();
    } catch (err) { toast(err.message, 'err'); b.disabled = false; }
  };

  el.onclick = async e => {
    const b = e.target.closest('button[data-act]');
    if (!b) return;
    const id = +b.dataset.id;
    const rec = items.find(x => x.id === id);
    if (b.dataset.act === 'edit') return recordForm(domainId, rec);
    if (b.dataset.act === 'del') {
      if (!confirm('删除该记录并从各云平台同步删除？')) return;
      b.disabled = true;
      try {
        const r = await Api.del('/api/records/' + id);
        const bad = (r.sync || []).filter(x => x.status !== 'success');
        toast(bad.length ? '已删除，部分平台失败（自动重试中）' : '已删除', bad.length ? 'err' : 'ok');
        reload();
      } catch (err) { toast(err.message, 'err'); b.disabled = false; }
    }
  };
}

/* -------------------------------- 记录表单 -------------------------------- */
function recordForm(domainId, rec) {
  const isEdit = !!rec;
  const m = modal({
    title: isEdit ? '编辑解析记录' : '添加解析记录',
    body: `
      <div class="field-row">
        <div class="field"><label>主机记录</label><input class="input" id="f-sub" value="${esc(rec?.sub_domain || '@')}" placeholder="@ 表示根域名"></div>
        <div class="field"><label>记录类型</label><select class="input" id="f-type">${TYPES.map(t => `<option ${rec?.type === t ? 'selected' : ''}>${t}</option>`).join('')}</select></div>
      </div>
      <div class="field"><label>记录值</label><input class="input" id="f-value" value="${esc(rec?.value || '')}" placeholder="1.2.3.4 / example.com / v=spf1 ...">
        <p class="f-hint" id="f-tip"></p></div>
      <div class="field-row">
        <div class="field"><label>线路</label><select class="input" id="f-line">${LINES.map(l => `<option ${rec?.line === l ? 'selected' : ''}>${l}</option>`).join('')}</select></div>
        <div class="field"><label>TTL（秒）</label><input class="input" id="f-ttl" type="number" value="${rec?.ttl || 600}"></div>
      </div>
      <div class="field-row">
        <div class="field"><label>MX 优先级</label><input class="input" id="f-pri" type="number" value="${rec?.priority ?? ''}" placeholder="仅 MX 需要"></div>
        <div class="field"><label>备注</label><input class="input" id="f-remark" value="${esc(rec?.remark || '')}" placeholder="选填"></div>
      </div>
      <p class="f-hint">保存后会立即下发到该域名所有已启用的云平台</p>`,
    actions: [{ label: '取消', cls: 'ghost' }, {
      label: isEdit ? '保存修改' : '创建并同步', cls: 'primary', onClick: async w => {
        const body = {
          sub: $('#f-sub', w).value.trim() || '@',
          type: $('#f-type', w).value,
          line: $('#f-line', w).value,
          value: $('#f-value', w).value.trim(),
          ttl: +$('#f-ttl', w).value || 600,
          priority: $('#f-pri', w).value === '' ? null : +$('#f-pri', w).value,
          remark: $('#f-remark', w).value.trim() || null,
        };
        if (!body.value) throw new Error('记录值不能为空');
        const res = isEdit
          ? await Api.post('/api/records/' + rec.id, body)
          : await Api.post(`/api/domains/${domainId}/records`, body);
        toastSync(res.sync);
        ctx.page = 1;
        rerender(true);
      }
    }]
  });

  const tips = {
    A: 'IPv4 地址，例如 1.2.3.4', AAAA: 'IPv6 地址，例如 2408:4003::1',
    CNAME: '目标域名，例如 example.com（不可与 A/AAAA 共存）',
    MX: '邮件服务器域名，需填写优先级', TXT: '文本内容，常用于 SPF / 验证',
    NS: '名称服务器域名', SRV: '格式：优先级 权重 端口 目标', CAA: '格式：0 issue "ca.example.com"',
  };
  const syncTip = () => { $('#f-tip', m.wrap).textContent = tips[$('#f-type', m.wrap).value] || ''; };
  $('#f-type', m.wrap).onchange = syncTip;
  syncTip();
}
