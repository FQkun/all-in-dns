'use strict';
const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

let user = null;

const cache = new Map();
const CACHE_TTL = 8000;

async function api(method, url, body, headers = {}) {
  if (url.startsWith('/api/')) {
    const qi = url.indexOf('?');
    const path = qi === -1 ? url : url.slice(0, qi);
    const qs = qi === -1 ? '' : url.slice(qi + 1);
    url = '/index.php?r=' + encodeURIComponent(path) + (qs ? '&' + qs : '');
  }
  const isGet = method === 'GET' && body === undefined;
  const noCache = headers['Cache-Control'] === 'no-cache';
  if (isGet && !noCache) {
    const hit = cache.get(url);
    if (hit && Date.now() - hit.t < CACHE_TTL) return hit.d;
  } else if (!isGet) {
    cache.clear();
  }
  const opt = { method, headers: { ...headers } };
  if (body !== undefined) {
    if (body instanceof Object) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
    else opt.body = body;
  }
  let res;
  const t0 = performance.now();
  try {
    res = await fetch(url, opt);
  } catch (e) {
    throw new Error('网络请求失败: ' + e.message);
  }
  const srvTime = res.headers.get('x-time');
  const timeEl = $('#req-time');
  if (timeEl && srvTime !== null) timeEl.textContent = `服务端 ${srvTime}ms / 总 ${Math.round(performance.now() - t0)}ms`;
  const ct = res.headers.get('content-type') || '';
  let data;
  if (ct.includes('json')) {
    const j = await res.json();
    if (res.status === 401) { showLogin(); throw new Error(j.msg || '未登录'); }
    if (j.code !== 0) throw new Error(j.msg || '请求失败');
    data = j.data;
  } else {
    if (!res.ok) {
      const text = (await res.text()).replace(/<[^>]*>/g, ' ').trim();
      throw new Error(`HTTP ${res.status}: ${text.slice(0, 200) || '服务器错误'}`);
    }
    return res;
  }
  if (isGet) cache.set(url, { t: Date.now(), d: data });
  return data;
}

function toast(msg, type = '') {
  if (type === 'err') {
    const m = modal('错误详情', `
      <div class="err-text" style="margin-bottom:12px;word-break:break-all">${esc(msg)}</div>
      <textarea readonly rows="4" onclick="this.select()">${esc(msg)}</textarea>`,
      [{ label: '复制', cls: 'primary', onclick: () => {
          const ta = $('textarea', $('#modal-root'));
          ta.select();
          (navigator.clipboard?.writeText(msg) || Promise.reject()).then(
            () => toast('已复制', 'ok'),
            () => { document.execCommand('copy'); toast('已复制', 'ok'); }
          );
          return false;
        }}, { label: '关闭' }]);
    $('textarea', m).select();
    return;
  }
  const el = document.createElement('div');
  el.className = 'toast-item ' + type;
  el.textContent = msg;
  el.onclick = () => el.remove();
  $('#toast').appendChild(el);
  setTimeout(() => el.remove(), 3000);
}

function modal(title, bodyHtml, actions, wide = false) {
  const root = $('#modal-root');
  root.innerHTML = `<div class="modal${wide ? ' wide' : ''}"><h3>${esc(title)}</h3>${bodyHtml}<div class="actions"></div></div>`;
  const acts = $('.actions', root);
  for (const a of actions) {
    const b = document.createElement('button');
    b.className = 'btn ' + (a.cls || '');
    b.textContent = a.label;
    b.onclick = async () => {
      if (b.disabled) return;
      b.disabled = true;
      try {
        const keep = await a.onclick?.(root);
        if (keep !== false) root.innerHTML = '';
      } catch (e) {
        toast(e.message, 'err');
        b.disabled = false;
      }
    };
    acts.appendChild(b);
  }
  return root;
}

async function busy(btn, fn) {
  if (!btn || btn.disabled) return;
  btn.disabled = true;
  try { await fn(); } catch (e) { toast(e.message, 'err'); }
  finally { btn.disabled = false; }
}

const PROVIDER_LABEL = { tencent: '腾讯云', aliyun: '阿里云', volc: '火山引擎' };
const ACT_LABEL = { create: '新增', update: '修改', delete: '删除' };
const prov = p => PROVIDER_LABEL[p] || esc(p || '-');

// ---------- 视图注册 ----------
const views = {};

views.dashboard = {
  title: '监控概览',
  async render(el) {
    const s = await api('GET', '/api/monitor/summary');
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">监控概览</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="cards">
        <div class="stat-card hero"><div class="lbl">域名总数</div><div class="num">${s.domains ?? '-'}</div><div class="hero-sub">已收录 ${s.records ?? '-'} 条解析记录</div></div>
        <div class="stat-card"><div class="num">${s.pending_queue ?? '-'}</div><div class="lbl">待同步任务</div></div>
        <div class="stat-card"><div class="num">${s.failed_queue ?? '-'}</div><div class="lbl">失败任务</div></div>
        <div class="stat-card"><div class="num">${s.success_sync_24h ?? '-'}</div><div class="lbl">24h 同步成功</div></div>
        <div class="stat-card"><div class="num">${s.failed_sync_24h ?? '-'}</div><div class="lbl">24h 同步失败</div></div>
      </div>`;
    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.dashboard.render(el));
    if (s.failed_queue > 0) {
      try {
        const q = await api('GET', '/api/sync/queue');
        const fails = q.filter(x => x.status === 'failed');
        if (!fails.length) return;
        const rows = fails.map(x => {
          let rec = '-';
          try { const p = JSON.parse(x.payload); rec = `${p.record.sub} ${p.record.type} ${p.record.value}`; } catch (e) {}
          return `<tr><td>${prov(x.provider)}</td>
            <td>${ACT_LABEL[x.action] || esc(x.action)}</td>
            <td class="wrap">${esc(rec)}</td>
            <td class="wrap err-text">${esc(x.last_error || '-')}</td>
            <td>${x.retry_count}</td></tr>`;
        }).join('');
        modal(`有 ${fails.length} 个同步任务失败`, `
          <div class="panel scroll"><table>
          <thead><tr><th>平台</th><th>操作</th><th>记录</th><th>失败原因</th><th>已重试</th></tr></thead>
          <tbody>${rows}</tbody></table></div>`,
          [{ label: '关闭' }, { label: '立即重试', cls: 'primary', onclick: async () => {
            const r = await api('POST', '/api/sync/retry-now');
            toast(`已重新执行 ${r.length} 个任务`, 'ok');
            views.dashboard.render(el);
          }}], true);
      } catch (e) { toast(`有 ${s.failed_queue} 个同步任务失败，可到同步日志页重试`, 'err'); }
    }
  }
};

views.accounts = {
  title: '云账号',
  async render(el) {
    const rows = await api('GET', '/api/accounts');
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">云账号</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="toolbar"><span class="spacer"></span><button class="btn primary" id="add-acc">添加账号</button></div>
      <div class="panel scroll"><table><thead><tr><th>ID</th><th>厂商</th><th>名称</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead>
      <tbody>${rows.map(a => `<tr>
        <td>${a.id}</td>
        <td><span class="badge ${a.provider}">${prov(a.provider)}</span></td>
        <td>${esc(a.name)}</td>
        <td><span class="badge ${a.status == 1 ? 'ok' : 'fail'}">${a.status == 1 ? '启用' : '停用'}</span></td>
        <td>${esc(a.created_at)}</td>
        <td class="ops"><button class="btn ghost sm test-acc" data-id="${a.id}">连通测试</button>
            ${user?.role === 'admin' ? `<button class="btn ghost sm danger del-acc" data-id="${a.id}">删除</button>` : ''}</td>
      </tr>`).join('') || '<tr><td colspan="6" class="empty">暂无账号</td></tr>'}</tbody></table></div>`;

    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.accounts.render(el));
    $('#add-acc').onclick = () => {
      const m = modal('添加云账号', `
        <div class="field"><label>厂商</label><select id="m-provider">
          <option value="tencent">腾讯云</option><option value="aliyun">阿里云</option><option value="volc">火山引擎</option></select></div>
        <div class="field"><label>名称</label><input id="m-name" placeholder="如：主账号"></div>
        <div class="field"><label>Access Key ID</label><input id="m-ak" autocomplete="off"></div>
        <div class="field"><label>Access Key Secret</label><input id="m-sk" type="password" autocomplete="off"></div>`,
        [{ label: '取消' }, { label: '保存', cls: 'primary', onclick: async () => {
          try {
            await api('POST', '/api/accounts', { provider: $('#m-provider').value, name: $('#m-name').value,
              access_key_id: $('#m-ak').value, access_key_secret: $('#m-sk').value });
            toast('账号已添加', 'ok'); views.accounts.render(el);
          } catch (e) { toast(e.message, 'err'); return false; }
        }}]);
      $('#m-name', m).focus();
    };
    el.onclick = async e => {
      const t = e.target;
      if (t.classList.contains('test-acc')) {
        busy(t, async () => {
          toast('测试中…');
          const r = await api('POST', `/api/accounts/${t.dataset.id}/test`);
          toast(`连通成功，该账号下有 ${r.domains} 个域名`, 'ok');
        });
      }
      if (t.classList.contains('del-acc') && confirm('确认删除该账号?')) {
        busy(t, async () => {
          await api('DELETE', `/api/accounts/${t.dataset.id}`);
          toast('已删除', 'ok');
          views.accounts.render(el);
        });
      }
    };
  }
};

views.domains = {
  title: '域名管理',
  async render(el) {
    const rows = await api('GET', '/api/domains');
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">域名管理</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="toolbar">
        <button class="btn primary" id="add-domain">添加域名</button>
        <span class="spacer"></span>
        <span class="hint">点击行进入解析记录管理</span>
      </div>
      <div class="panel scroll"><table><thead><tr><th>ID</th><th>域名</th><th>同步平台</th><th>备注</th><th>操作</th></tr></thead>
      <tbody>${rows.map(d => `<tr data-id="${d.id}">
        <td>${d.id}</td><td>${esc(d.name)}</td>
        <td>${(d.providers || '').split(',').filter(Boolean).map(p => {
          const [p2, on] = p.split(':');
          const lbl = prov(p2);
          return `<span class="badge ${p2}${on === 'off' ? ' dim' : ''}" title="${lbl}${on === 'off' ? '（已暂停同步）' : ''}">${lbl}${on === 'off' ? ' 暂停' : ''}</span>`;
        }).join(' ') || '<span class="badge neutral">未绑定</span>'}</td>
        <td>${esc(d.remark)}</td>
        <td class="ops">
          <button class="btn ghost sm verify-dom" data-id="${d.id}">一致性校验</button>
          ${user?.role === 'admin' ? `<button class="btn ghost sm danger del-dom" data-id="${d.id}">删除</button>` : ''}
        </td>
      </tr>`).join('') || '<tr><td colspan="5" class="empty">暂无域名</td></tr>'}</tbody></table></div>`;

    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.domains.render(el));
    $('#add-domain').onclick = () => {
      modal('添加域名（自动在各云厂商验证）', `
        <div class="field"><label>域名</label><input id="m-domain" placeholder="example.com"></div>
        <div class="field"><label>备注</label><input id="m-remark"></div>`,
        [{ label: '取消' }, { label: '添加', cls: 'primary', onclick: async () => {
          try {
            await api('POST', '/api/domains', { name: $('#m-domain').value, remark: $('#m-remark').value });
            toast('域名已添加', 'ok'); views.domains.render(el);
          } catch (e) { toast(e.message, 'err'); return false; }
        }}]);
    };
    el.onclick = async e => {
      const tr = e.target.closest('tr[data-id]');
      if (e.target.classList.contains('verify-dom')) {
        e.stopPropagation();
        busy(e.target, async () => {
          toast('校验中…');
          const domId = e.target.dataset.id;
          const rep = await api('POST', `/api/domains/${domId}/verify`);
          const rows = rep.map(r => {
            let html = `<tr><td>${prov(r.provider)}</td><td><span class="badge ${r.status}">${
              r.status === 'consistent' ? '一致' : r.status === 'inconsistent' ? '不一致' : '错误: ' + esc(r.message || '')
            }</span></td><td>${r.local_count ?? '-'}</td><td>${r.remote_count ?? '-'}</td><td>${r.missing_local ?? '-'}</td><td>${r.extra_remote ?? '-'}</td></tr>`;
            if (r.detail?.missing?.length) {
              html += `<tr><td colspan="6" class="detail-cell">
                ${prov(r.provider)} 缺少的记录：<br>
                ${r.detail.missing.map(m => '· ' + esc(m.key)).join('<br>')}
                <br><button class="btn primary sm repair-btn" data-aid="${r.account_id}" data-ids="${r.detail.missing.map(m => m.id).join(',')}">推送这 ${r.detail.missing.length} 条到该平台</button>
              </td></tr>`;
            }
            if (r.detail?.extra?.length) {
              html += `<tr><td colspan="6" class="detail-cell">该平台多出的记录（本地没有，可点“从云平台拉取”导入）：${r.detail.extra.map(esc).join('、')}</td></tr>`;
            }
            return html;
          }).join('');
          modal('一致性校验结果', `<div class="panel scroll"><table><thead><tr><th>平台</th><th>状态</th><th>本地</th><th>远端</th><th>本地缺失</th><th>远端多余</th></tr></thead><tbody>
            ${rows}
          </tbody></table></div>`, [{ label: '关闭', cls: 'primary' }], true);
          document.querySelectorAll('#modal-root .repair-btn').forEach(btn => btn.onclick = async () => {
            if (btn.disabled) return;
            btn.disabled = true;
            try {
              const res = await api('POST', `/api/domains/${domId}/repair`, {
                account_id: +btn.dataset.aid,
                record_ids: btn.dataset.ids.split(',').map(Number),
              });
              const fails = (res.sync || []).filter(s => s.status === 'failed');
              if (fails.length) { toast(`推送完成，${fails.length} 条失败（将自动重试）: ${fails[0].message || ''}`, 'err'); btn.disabled = false; }
              else { toast('已全部推送成功', 'ok'); btn.textContent = '已推送'; }
            } catch (e2) { toast(e2.message, 'err'); btn.disabled = false; }
          });
        });
        return;
      }
      if (e.target.classList.contains('del-dom')) {
        e.stopPropagation();
        if (confirm('删除域名及其全部本地解析记录（不影响云平台）?')) {
          busy(e.target, async () => {
            await api('DELETE', `/api/domains/${e.target.dataset.id}`);
            views.domains.render(el);
          });
        }
        return;
      }
      if (tr && !e.target.closest('button')) nav('records', { domainId: +tr.dataset.id, domainName: tr.children[1].textContent });
    };
  }
};

views.records = {
  title: '解析记录',
  async render(el, ctx = {}) {
    if (!ctx.domainId) return nav('domains');
    const page = ctx.page || 1, size = 100;
    const res = await api('GET', `/api/domains/${ctx.domainId}/records?page=${page}&size=${size}`, undefined, { 'Cache-Control': 'no-cache' });
    const recs = res.items || [];
    const total = res.total ?? recs.length;
    const pages = Math.max(1, Math.ceil(total / size));
    const TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA'];
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">解析记录 - ${esc(ctx.domainName || '')}</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="toolbar">
        <button class="btn ghost" id="back-domains">← 返回域名</button>
        <button class="btn primary" id="add-rec">添加记录</button>
        <button class="btn" id="import-remote">从云平台拉取</button>
        <button class="btn" id="import-csv">导入 CSV</button>
        <a class="btn" href="/index.php?r=/api/domains/${ctx.domainId}/export" download>导出 CSV</a>
        <span class="spacer"></span>
        <button class="btn danger" id="batch-del">删除选中</button>
      </div>
      <div class="panel scroll"><table><thead><tr><th><input type="checkbox" class="checkbox" id="chk-all" aria-label="全选"></th>
        <th>主机</th><th>类型</th><th>线路</th><th>记录值</th><th>TTL</th><th>MX</th><th>本地状态</th><th>同步状态</th><th>更新时间</th><th>操作</th></tr></thead>
      <tbody>${recs.map(r => `<tr data-id="${r.id}">
        <td><input type="checkbox" class="checkbox row-chk" value="${r.id}" aria-label="选择"></td>
        <td>${esc(r.sub_domain)}</td>
        <td><span class="badge neutral mono">${esc(r.type)}</span></td>
        <td>${esc(r.line)}</td>
        <td class="wrap">${esc(r.value)}</td>
        <td>${r.ttl}</td><td>${r.priority ?? '-'}</td>
        <td>${r.status === 'disabled'
          ? '<span class="badge pending" title="云端删除未完成，等待定时任务重试">待清理</span>'
          : '<span class="badge ok">生效中</span>'}</td>
        <td>${(r.sync_states || '').split(',').filter(Boolean).map(s => {
          const parts = s.split('|');
          const aid = parts.length > 2 ? parts[0] : '';
          const p = parts.length > 2 ? parts[1] : parts[0];
          const st = parts.length > 2 ? parts[2] : parts[1];
          const lbl = prov(p);
          return `<span class="badge ${st}" title="${lbl}${aid ? ' 账号#' + aid : ''}">${lbl} ${st === 'success' ? '成功' : st === 'failed' ? '失败' : esc(st)}</span>`;
        }).join(' ') || '<span class="badge neutral">未同步</span>'}</td>
        <td>${esc(r.updated_at)}</td>
        <td class="ops"><button class="btn ghost sm edit-rec" data-r='${esc(JSON.stringify(r))}'>编辑</button>
            <button class="btn ghost sm danger del-rec" data-id="${r.id}">删除</button></td>
      </tr>`).join('') || '<tr><td colspan="11" class="empty">暂无记录</td></tr>'}</tbody></table>
      <div class="pager"><button class="btn ghost sm" id="pg-prev" ${page <= 1 ? 'disabled' : ''}>上一页</button>
        <span class="hint">第 ${page} / ${pages} 页 · 共 ${total} 条</span>
        <button class="btn ghost sm" id="pg-next" ${page >= pages ? 'disabled' : ''}>下一页</button></div></div>`;

    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.records.render(el, ctx));
    $('#back-domains').onclick = () => nav('domains');
    $('#pg-prev').onclick = () => { ctx.page = page - 1; views.records.render(el, ctx); };
    $('#pg-next').onclick = () => { ctx.page = page + 1; views.records.render(el, ctx); };

    const syncChkAll = () => {
      const all = $('#chk-all');
      const boxes = $$('.row-chk');
      all.checked = boxes.length > 0 && boxes.every(c => c.checked);
      all.indeterminate = !all.checked && boxes.some(c => c.checked);
    };

    const recForm = (r = {}) => modal(r.id ? '编辑记录' : '添加记录', `
      <div class="field"><label>主机记录（@ 表示根域名）</label><input id="f-sub" value="${esc(r.sub_domain || '@')}"></div>
      <div class="field"><label>类型</label><select id="f-type">${TYPES.map(t => `<option ${r.type === t ? 'selected' : ''}>${t}</option>`).join('')}</select></div>
      <div class="field"><label>线路</label><input id="f-line" value="${esc(r.line || '默认')}"></div>
      <div class="field"><label>记录值</label><input id="f-value" value="${esc(r.value || '')}"></div>
      <div class="field"><label>TTL(秒)</label><input id="f-ttl" type="number" value="${r.ttl || 600}"></div>
      <div class="field"><label>MX 优先级(仅 MX)</label><input id="f-pri" type="number" value="${r.priority ?? ''}"></div>
      <p class="hint">保存后将立即同步到所有已启用的云平台</p>`,
      [{ label: '取消' }, { label: '保存', cls: 'primary', onclick: async () => {
        const body = { sub: $('#f-sub').value, type: $('#f-type').value, line: $('#f-line').value,
          value: $('#f-value').value, ttl: +$('#f-ttl').value, priority: $('#f-pri').value || null };
        try {
          const res = r.id ? await api('POST', `/api/records/${r.id}`, body)
                           : await api('POST', `/api/domains/${ctx.domainId}/records`, body);
          const fails = (res.sync || []).filter(s => s.status === 'failed');
          if (fails.length) toast(`已保存，但 ${fails.length} 个平台同步失败（将自动重试）`, 'err');
          else toast('已保存并同步成功', 'ok');
          ctx.page = 1;
          views.records.render(el, ctx);
        } catch (e) { toast(e.message, 'err'); return false; }
      }}]);

    $('#add-rec').onclick = () => recForm();
    el.onclick = async e => {
      const t = e.target;
      if (t.id === 'chk-all') { $$('.row-chk').forEach(c => c.checked = t.checked); t.indeterminate = false; }
      if (t.classList.contains('row-chk')) syncChkAll();
      if (t.classList.contains('edit-rec')) recForm(JSON.parse(t.dataset.r));
      if (t.classList.contains('del-rec') && confirm('删除该记录（并从各云平台删除）?')) {
        busy(t, async () => {
          const res = await api('DELETE', `/api/records/${t.dataset.id}`);
          const fails = (res.sync || []).filter(s => s.status === 'failed');
          t.closest('tr')?.remove();
          fails.length ? toast('本地已删除，部分平台同步失败（将自动重试）', 'err') : toast('已删除', 'ok');
          ctx.page = 1;
          await views.records.render(el, ctx);
        });
      }
      if (t.id === 'batch-del') {
        const ids = $$('.row-chk:checked').map(c => +c.value);
        if (!ids.length) { toast('请先勾选要删除的记录', 'err'); return; }
        if (!confirm(`确认删除选中的 ${ids.length} 条记录?`)) return;
        busy(t, async () => {
          const result = await api('POST', '/api/records/batch-delete', { ids });
          toast(result.pending?.length ? `已删除，${result.pending.length} 条记录等待云端重试` : '已删除', result.pending?.length ? 'err' : 'ok');
          ctx.page = 1;
          await views.records.render(el, ctx);
        });
      }
      if (t.id === 'import-remote') {
        busy(t, async () => {
          toast('拉取中…');
          const r2 = await api('POST', `/api/domains/${ctx.domainId}/import-remote`);
          toast(`已拉取 ${r2.imported} 条` + (r2.merged ? `，合并重复 ${r2.merged} 条` : ''), 'ok');
          if (r2.errors?.length) toast('拉取失败: ' + r2.errors.join('；'), 'err');
          ctx.page = 1;
          views.records.render(el, ctx);
        });
      }
      if (t.id === 'import-csv') {
        modal('导入 CSV', `<div class="field"><label>格式: sub_domain,type,line,value,ttl,priority（首行为表头）</label>
          <textarea id="f-csv" rows="10" placeholder="@,A,默认,1.2.3.4,600,"></textarea></div>`,
          [{ label: '取消' }, { label: '导入', cls: 'primary', onclick: async () => {
            try { const r2 = await api('POST', `/api/domains/${ctx.domainId}/import`, { csv: $('#f-csv').value });
              toast(`已导入 ${r2.imported} 条` + (r2.skipped ? `，跳过 ${r2.skipped} 条` : ''), 'ok');
              ctx.page = 1; views.records.render(el, ctx);
            } catch (e2) { toast(e2.message, 'err'); return false; }
          }}]);
      }
    };
  }
};

views.synclogs = {
  title: '同步日志',
  async render(el) {
    const [logs, queue] = await Promise.all([
      api('GET', '/api/sync/logs'), api('GET', '/api/sync/queue')]);
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">同步日志</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="toolbar">
        <button class="btn" id="retry-now">立即重试失败任务</button>
        <button class="btn danger" id="clear-sync-queue">删除全部重试任务</button>
        ${user?.role === 'admin' ? '<button class="btn danger" id="clear-synclogs">清空日志</button>' : ''}
        <span class="hint">失败任务由定时任务自动重试（指数退避，最多 5 次）</span>
      </div>
      ${queue.length ? `<details class="panel queue"><summary>待重试队列 (${queue.length})${queue.some(q => q.status === 'failed') ? ' <span class="badge failed">有失败</span>' : ''}</summary><table><thead>
        <tr><th>ID</th><th>平台</th><th>动作</th><th>重试次数</th><th>下次重试</th><th>错误</th></tr></thead><tbody>
        ${queue.map(q => `<tr><td>${q.id}</td><td><span class="badge ${q.provider}">${prov(q.provider)}</span></td>
          <td>${ACT_LABEL[q.action] || esc(q.action)}</td><td>${q.retry_count}</td><td>${esc(q.next_retry_at || '立即')}</td>
          <td class="wrap">${esc(q.last_error)}</td></tr>`).join('')}</tbody></table></details>` : ''}
      <div class="panel scroll"><table><thead><tr><th>时间</th><th>平台</th><th>记录ID</th><th>动作</th><th>结果</th><th>消息</th><th>耗时</th></tr></thead>
      <tbody>${logs.map(l => `<tr>
        <td>${esc(l.created_at)}</td><td><span class="badge ${l.provider}">${prov(l.provider)}</span></td>
        <td>${l.record_id ?? '-'}</td><td>${ACT_LABEL[l.action] || esc(l.action)}</td>
        <td><span class="badge ${l.status}">${l.status === 'success' ? '成功' : '失败'}</span></td>
        <td class="wrap">${esc(l.message)}</td><td>${l.request_cost_ms ?? '-'}ms</td>
      </tr>`).join('') || '<tr><td colspan="7" class="empty">暂无日志</td></tr>'}</tbody></table></div>`;
    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.synclogs.render(el));
    $('#retry-now').onclick = () => busy($('#retry-now'), async () => {
      const r = await api('POST', '/api/sync/retry-now');
      toast(`已重试 ${r.count ?? r.items?.length ?? 0} 个任务`, 'ok');
      await views.synclogs.render(el);
    });
    $('#clear-sync-queue').onclick = () => busy($('#clear-sync-queue'), async () => {
      if (!confirm('确认删除全部待重试任务?')) return;
      const r = await api('DELETE', '/api/sync/queue');
      toast(`已删除 ${r.count ?? 0} 个重试任务`, 'ok');
      await views.synclogs.render(el);
    });
    const clr = $('#clear-synclogs');
    if (clr) clr.onclick = () => busy(clr, async () => {
      if (!confirm('确认清空全部同步日志?')) return;
      await api('DELETE', '/api/sync/logs');
      toast('已清空', 'ok');
      views.synclogs.render(el);
    });
  }
};

views.oplogs = {
  title: '操作日志',
  async render(el) {
    const logs = await api('GET', '/api/operation-logs');
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">操作日志</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="toolbar"><button class="btn danger" id="clear-oplogs">清空日志</button></div>
      <div class="panel scroll"><table><thead><tr><th>时间</th><th>用户</th><th>动作</th><th>对象</th><th>详情</th><th>IP</th></tr></thead>
      <tbody>${logs.map(l => `<tr>
        <td>${esc(l.created_at)}</td><td>${esc(l.username)}</td><td>${esc(l.action)}</td>
        <td>${esc(l.target)}</td><td class="wrap">${esc(l.detail)}</td><td>${esc(l.ip)}</td></tr>`).join('') || '<tr><td colspan="6" class="empty">暂无日志</td></tr>'}</tbody></table></div>`;
    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.oplogs.render(el));
    $('#clear-oplogs').onclick = () => busy($('#clear-oplogs'), async () => {
      if (!confirm('确认清空全部操作日志?')) return;
      await api('DELETE', '/api/operation-logs');
      toast('已清空', 'ok');
      views.oplogs.render(el);
    });
  }
};

views.backups = {
  title: '备份与恢复',
  async render(el) {
    const rows = await api('GET', '/api/backups');
    el.innerHTML = `
      <div class="page-head"><h2 class="page-title">备份与恢复</h2>
        <button class="btn ghost sm" id="btn-refresh">刷新</button></div>
      <div class="toolbar"><button class="btn primary" id="mk-bk">创建备份</button>
        <button class="btn ghost sm danger" id="del-sel">删除所选</button>
        <label class="hint" style="cursor:pointer"><input type="checkbox" id="sel-all"> 全选</label>
        <span class="hint">备份保存域名、绑定关系与全部解析记录的 JSON 快照</span></div>
      <div class="panel scroll"><table><thead><tr><th style="width:36px"></th><th>ID</th><th>名称</th><th>记录数</th><th>创建人</th><th>时间</th><th>操作</th></tr></thead>
      <tbody>${rows.map(b => `<tr><td><input type="checkbox" class="bk-sel" data-id="${b.id}"></td><td>${b.id}</td><td>${esc(b.name)}</td><td>${b.record_count}</td>
        <td>${esc(b.created_by)}</td><td>${esc(b.created_at)}</td>
        <td class="ops"><button class="btn ghost sm danger restore-bk" data-id="${b.id}">恢复</button>
        <button class="btn ghost sm danger del-bk" data-id="${b.id}">删除</button></td></tr>`).join('') || '<tr><td colspan="7" class="empty">暂无备份</td></tr>'}</tbody></table></div>`;
    $('#btn-refresh').onclick = () => busy($('#btn-refresh'), () => views.backups.render(el));
    $('#mk-bk').onclick = () => busy($('#mk-bk'), async () => {
      await api('POST', '/api/backups');
      toast('备份已创建', 'ok');
      views.backups.render(el);
    });
    $('#sel-all').onclick = e => {
      $$('.bk-sel', el).forEach(cb => { cb.checked = e.target.checked; });
      updateDelBtn();
    };
    const selIds = () => $$('.bk-sel', el).filter(cb => cb.checked).map(cb => +cb.dataset.id);
    const updateDelBtn = () => { $('#del-sel').disabled = !selIds().length; };
    $('#del-sel').disabled = true;
    el.onchange = e => {
      if (e.target.classList.contains('bk-sel')) {
        if ($$('.bk-sel', el).every(cb => cb.checked)) $('#sel-all').checked = true;
        else $('#sel-all').checked = false;
        updateDelBtn();
      }
    };
    el.onclick = async e => {
      if (e.target.classList.contains('restore-bk') &&
          confirm('恢复将覆盖当前全部域名/解析记录数据，确认?')) {
        busy(e.target, async () => {
          await api('POST', `/api/backups/${e.target.dataset.id}/restore`);
          toast('恢复完成', 'ok');
        });
      }
      if (e.target.classList.contains('del-bk') &&
          confirm('确认删除该备份?')) {
        busy(e.target, async () => {
          await api('POST', '/api/backups/delete', { ids: [+e.target.dataset.id] });
          toast('已删除', 'ok');
          views.backups.render(el);
        });
      }
    };
    $('#del-sel').onclick = () => {
      const ids = selIds();
      if (!ids.length) return;
      if (!confirm(`确认删除所选 ${ids.length} 个备份?`)) return;
      busy($('#del-sel'), async () => {
        await api('POST', '/api/backups/delete', { ids });
        toast('已删除', 'ok');
        views.backups.render(el);
      });
    };
  }
};

// ---------- 导航 ----------
let navCtx = {};
let curView = '';
function nav(view, ctx = {}) {
  if (!views[view]) view = 'dashboard';
  if (Object.keys(ctx).length) navCtx[view] = ctx;
  // 换新容器节点，清掉上个视图累积的事件监听
  const old = $('#view');
  const el = document.createElement('div');
  el.id = 'view';
  el.className = old.className;
  old.replaceWith(el);
  el.innerHTML = '<p class="loading">加载中…</p>';
  curView = view;
  views[view].render(el, navCtx[view] || {}).catch(e => { el.innerHTML = `<p class="load-error">${esc(e.message)}</p>`; });
  document.querySelectorAll('#nav a').forEach(a => a.classList.toggle('active', a.dataset.view === view));
  if (location.hash.slice(1) !== view) location.hash = view;
}

window.addEventListener('hashchange', () => {
  const v = location.hash.slice(1);
  if (views[v] && v !== curView) nav(v);
});

const NAV_ICONS = {
  dashboard: '<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>',
  accounts: '<path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>',
  domains: '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>',
  records: '<line x1="8" x2="21" y1="6" y2="6"/><line x1="8" x2="21" y1="12" y2="12"/><line x1="8" x2="21" y1="18" y2="18"/><line x1="3" x2="3.01" y1="6" y2="6"/><line x1="3" x2="3.01" y1="12" y2="12"/><line x1="3" x2="3.01" y1="18" y2="18"/>',
  synclogs: '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
  oplogs: '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M10 9H8"/><path d="M16 13H8"/><path d="M16 17H8"/>',
  backups: '<rect x="2" y="3" width="20" height="5" rx="1"/><path d="M4 8v11a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8"/><path d="M10 12h4"/>'
};

function initNav() {
  $('#nav').innerHTML = Object.entries(views)
    .map(([k, v]) => `<a href="#${k}" data-view="${k}"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${NAV_ICONS[k] || ''}</svg><span>${v.title}</span></a>`).join('');
  $('#nav').onclick = e => {
    const a = e.target.closest('a');
    if (a) { e.preventDefault(); nav(a.dataset.view); }
  };
}

function showLogin() {
  user = null;
  $('#login-view').classList.remove('hidden');
  $('#view').classList.add('hidden');
  $('#app-footer').classList.add('hidden');
  $('#sidebar').style.visibility = 'hidden';
}

async function showApp() {
  const me = await api('GET', '/api/auth/me').catch(() => null);
  if (!me) return showLogin();
  user = me.user;
  $('#whoami').textContent = user.username + (user.role === 'admin' ? ' (管理员)' : '');
  $('#login-view').classList.add('hidden');
  $('#view').classList.remove('hidden');
  $('#app-footer').classList.remove('hidden');
  $('#sidebar').style.visibility = 'visible';
  initNav();
  const hash = location.hash.slice(1);
  nav(views[hash] ? hash : 'dashboard');
}

const loginBtn = $('#login-btn');
const captchaCfg = window.AliyunCaptchaConfig || {};
const captchaEnabled = !!(captchaCfg.prefix && captchaCfg.scene);
let captchaParam = null, captchaInst = null;

function resetCaptcha() {
  captchaParam = null;
  loginBtn.disabled = captchaEnabled;
  if (captchaInst && captchaInst.refresh) captchaInst.refresh();
}

$('#login-form').onsubmit = async e => {
  e.preventDefault();
  if (loginBtn.disabled) return;
  if (captchaEnabled && !captchaParam) { toast('请先完成安全验证', 'err'); return; }
  loginBtn.disabled = true;
  try {
    const fd = new FormData($('#login-form'));
    const body = { username: fd.get('username'), password: fd.get('password') };
    const headers = {};
    if (captchaParam) { body.captcha = captchaParam; headers['captcha-verify-param'] = captchaParam; }
    await api('POST', '/api/auth/login', body, headers);
    await showApp();
  } catch (err) {
    toast(err.message, 'err');
    resetCaptcha();
  }
};

loginBtn.onclick = () => $('#login-form').requestSubmit();

if (captchaEnabled) {
  if (typeof window.initAliyunCaptcha === 'function') {
    try {
      loginBtn.disabled = true;
      window.initAliyunCaptcha({
        SceneId: captchaCfg.scene,
        mode: 'embed',
        element: '#captcha-element',
        success: p => { captchaParam = p; loginBtn.disabled = false; },
        fail: r => console.error(r),
        getInstance: i => { captchaInst = i; },
        server: ['captcha-esa-open.aliyuncs.com', 'captcha-esa-open-b.aliyuncs.com'],
        slideStyle: { width: 320, height: 40 },
        language: 'cn',
        rem: 0.9
      });
    } catch (e) {
      console.error('captcha init failed', e);
      loginBtn.disabled = false;
      loginBtn.onclick = () => toast('验证码组件加载失败，请检查网络后刷新', 'err');
    }
  } else {
    loginBtn.disabled = false;
    loginBtn.onclick = () => toast('验证码组件加载失败，请检查网络后刷新', 'err');
  }
}

$('#btn-logout').onclick = async () => { await api('POST', '/api/auth/logout'); showLogin(); };

const blurTargets = () => [$('#view'), $('#sidebar'), $('#app-footer')].filter(Boolean);
window.addEventListener('focus', () => blurTargets().forEach(el => el.classList.remove('blurred')));
window.addEventListener('blur', () => {
  if ($('#modal-root')?.children.length) return;
  if (user && user.role === 'admin') return;
  blurTargets().forEach(el => el.classList.add('blurred'));
});

(async function boot() {
  showApp();
})();
