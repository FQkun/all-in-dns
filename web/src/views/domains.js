/* 域名管理：同步开关 / 绑定解绑 / 一致性校验 / 生效检测 */
import { $, $$, esc, icon, num, tshort, provChip, provLabel, PROV, DOMAIN_STATUS, toast, modal, isAdmin, canOperate } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub, go } from '../app.js';

export async function render(el, route, force) {
  const doms = await Api.get('/api/domains');
  const accounts = await Api.get('/api/accounts').catch(() => []);

  // 域名列表接口只给 "tencent:on,..." 汇总，开关需要 account_id → 逐个取绑定（数量可控时）
  const bindings = {};
  if (doms.length <= 40) {
    await Promise.all(doms.map(async d => {
      bindings[d.id] = await Api.get(`/api/domains/${d.id}/accounts`).catch(() => []);
    }));
  }

  setSub(`共 ${doms.length} 个域名 · ${accounts.length} 个云账号 · 开关即时生效`);

  const provCell = d => {
    const bs = bindings[d.id];
    if (bs && bs.length) {
      return bs.map(b => `<span class="pv-toggle" title="${esc(provLabel(b.provider))} · ${esc(b.account_name || '')}${b.verify_status === 'fail' ? ' · 验证失败' : ''}">
          ${provChip(b.provider)}
          <label class="switch"><input type="checkbox" data-act="toggle" data-domain="${d.id}" data-account="${b.account_id}" ${b.sync_enabled ? 'checked' : ''} ${canOperate() ? '' : 'disabled'}><span class="track"></span></label>
        </span>`).join('');
    }
    // 兜底：用汇总字符串渲染静态标签
    const parts = String(d.providers || '').split(',').filter(Boolean);
    if (!parts.length) return '<span class="chip tout">未绑定</span>';
    return parts.map(p => {
      const [prov, st] = p.split(':');
      return `<span class="chip ${PROV[prov] ? PROV[prov].cls : ''}" title="${esc(provLabel(prov))}${st === 'off' ? '（已暂停同步）' : ''}"><i class="dot"></i>${esc(provLabel(prov))}${st === 'off' ? ' 暂停' : ''}</span>`;
    }).join(' ');
  };

  const row = d => {
    const st = DOMAIN_STATUS[d.status] || ['未知', 'tout'];
    return `<tr class="clickable" data-id="${d.id}" data-name="${esc(d.name)}" data-search="${esc((d.name + ' ' + (d.remark || '')).toLowerCase())}">
      <td><b>${esc(d.name)}</b></td>
      <td>${provCell(d)}</td>
      <td><span class="chip ${st[1]}"><i class="dot"></i>${st[0]}</span></td>
      <td class="hint" style="max-width:200px;overflow:hidden;text-overflow:ellipsis">${esc(d.remark || '-')}</td>
      <td class="ops">
        <button class="btn ghost sm" data-act="bind" data-id="${d.id}" data-name="${esc(d.name)}">绑定管理</button>
        <button class="btn ghost sm" data-act="verify" data-id="${d.id}" data-name="${esc(d.name)}">校验</button>
        <button class="btn ghost sm" data-act="probe" data-id="${d.id}" data-name="${esc(d.name)}">生效检测</button>
        ${isAdmin() ? `<button class="btn ghost sm" data-act="del" data-id="${d.id}" data-name="${esc(d.name)}" style="color:var(--bad)">删除</button>` : ''}
      </td>
    </tr>`;
  };

  el.innerHTML = `
    <div class="toolbar">
      <div class="gsearch" style="width:240px;position:relative">
        ${icon('search')}
        <input id="dom-filter" placeholder="筛选域名 / 备注" style="padding-left:32px">
      </div>
      <span class="sp"></span>
      <span class="hint">共 ${doms.length} 个域名</span>
      ${canOperate() ? `<button class="btn primary" id="add-domain">${icon('plus')}添加域名</button>` : ''}
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>域名</th><th>同步平台</th><th>状态</th><th>备注</th><th style="text-align:right">操作</th></tr></thead>
        <tbody id="dom-body">${doms.map(row).join('') || '<tr><td colspan="5" class="empty">还没有域名，先添加一个</td></tr>'}</tbody>
      </table></div>
    </div>`;

  $('#dom-filter').oninput = e => {
    const kw = e.target.value.toLowerCase().trim();
    $$('#dom-body tr').forEach(tr => {
      tr.style.display = !kw || (tr.dataset.search || '').includes(kw) ? '' : 'none';
    });
  };

  const add = $('#add-domain');
  if (add) add.onclick = () => modal({
    title: '添加域名',
    body: `<div class="field"><label>域名</label><input class="input" id="f-name" placeholder="example.com">
        <p class="f-hint">添加后会立即向每个云账号校验域名归属，验证失败也会先建好绑定</p></div>
      <div class="field"><label>备注</label><input class="input" id="f-remark" placeholder="选填，例如：官网主域"></div>`,
    actions: [{ label: '取消', cls: 'ghost' }, {
      label: '添加并校验', cls: 'primary', onClick: async w => {
        const name = $('#f-name', w).value.trim();
        if (!name) throw new Error('请填写域名');
        await Api.post('/api/domains', { name, remark: $('#f-remark', w).value.trim() || null });
        toast('域名已添加', 'ok');
        rerender(true);
      }
    }]
  });

  el.onclick = async e => {
    const btn = e.target.closest('button[data-act]');
    if (btn) {
      e.stopPropagation();
      const { act, id, name } = btn.dataset;
      if (act === 'verify') return verifyDomain(id, name);
      if (act === 'probe') return probeDomain(id, name);
      if (act === 'bind') return bindManage(id, name, accounts, () => rerender(true));
      if (act === 'del') {
        if (!confirm(`删除 ${name} 及其本地解析记录？（不影响云平台）`)) return;
        btn.disabled = true;
        try { await Api.del('/api/domains/' + id); toast('已删除', 'ok'); rerender(true); }
        catch (err) { toast(err.message, 'err'); btn.disabled = false; }
        return;
      }
    }
    const tr = e.target.closest('tr[data-id]');
    if (tr && !e.target.closest('.switch')) go(`/domains/${tr.dataset.id}`);
  };

  el.onchange = async e => {
    const inp = e.target.closest('input[data-act="toggle"]');
    if (!inp) return;
    inp.disabled = true;
    try {
      await Api.post(`/api/domains/${inp.dataset.domain}/toggle`, { account_id: +inp.dataset.account, enabled: inp.checked });
      toast(inp.checked ? '已恢复该平台同步' : '已暂停该平台同步', 'ok');
    } catch (err) {
      toast(err.message, 'err');
      inp.checked = !inp.checked;
    } finally { inp.disabled = false; }
  };
}

/* ------------------------------ 一致性校验 ------------------------------ */
async function verifyDomain(id, name) {
  const m = modal({
    title: `一致性校验 · ${name || ''}`,
    wide: true,
    body: '<div class="empty">正在对比本地与各云平台的记录…</div>',
    actions: [{ label: '关闭', cls: 'ghost' }],
  });
  try {
    const rows = await Api.post(`/api/domains/${id}/verify`);
    const statusChip = s => s === 'consistent' ? '<span class="chip ok"><i class="dot"></i>一致</span>'
      : s === 'inconsistent' ? '<span class="chip warn"><i class="dot"></i>不一致</span>'
        : `<span class="chip bad"><i class="dot"></i>${esc(s || '错误')}</span>`;

    const body = m.wrap.querySelector('.modal-b');
    if (!rows || !rows.length) {
      body.innerHTML = '<div class="empty">该域名还没有绑定任何云账号</div>';
      return;
    }
    body.innerHTML = `<div class="table-wrap"><table>
      <thead><tr><th>平台</th><th>状态</th><th>本地</th><th>远端</th><th>缺失</th><th>多余</th><th></th></tr></thead>
      <tbody>${rows.map(r => {
        const missing = (r.detail && r.detail.missing) || [];
        return `<tr><td>${provChip(r.provider)}</td><td>${statusChip(r.status)}</td>
          <td class="num">${num(r.local_count)}</td><td class="num">${num(r.remote_count)}</td>
          <td class="num" style="color:${r.missing_local ? 'var(--bad)' : 'inherit'}">${num(r.missing_local)}</td>
          <td class="num">${num(r.extra_remote)}</td>
          <td class="ops">${missing.length ? `<button class="btn primary sm" data-repair="${r.account_id}" data-ids="${missing.map(x => x.id).join(',')}">补齐 ${missing.length} 条</button>` : ''}</td></tr>`;
      }).join('')}</tbody></table></div>`;

    body.onclick = async e => {
      const b = e.target.closest('button[data-repair]');
      if (!b) return;
      b.disabled = true;
      try {
        const res = await Api.post(`/api/domains/${id}/repair`, {
          account_id: +b.dataset.repair, record_ids: b.dataset.ids.split(',').map(Number),
        });
        const bad = (res.sync || []).filter(x => x.status !== 'success');
        toast(bad.length ? `已推送，${bad.length} 条失败（进入重试队列）` : '已全部补齐', bad.length ? 'err' : 'ok');
        b.textContent = bad.length ? '部分失败' : '已补齐';
      } catch (err) { toast(err.message, 'err'); b.disabled = false; }
    };
  } catch (e) {
    m.wrap.querySelector('.modal-b').innerHTML = `<div class="load-error">${esc(e.message)}</div>`;
  }
}

/* ------------------------------ 生效检测 ------------------------------ */
async function probeDomain(id, name) {
  const m = modal({
    title: `解析生效检测 · ${name || ''}`,
    wide: true,
    body: '<div class="empty">正在向阿里 / 腾讯 / Google 公共 DNS 发起真实查询…</div>',
    actions: [{ label: '关闭', cls: 'ghost' }],
  });
  try {
    const data = await Api.get(`/api/domains/${id}/probe`);
    const items = data.items || [];
    const body = m.wrap.querySelector('.modal-b');
    if (!items.length) {
      body.innerHTML = '<div class="empty">没有可检测的默认线路记录</div>';
      return;
    }
    const verdict = v => v === 'ok' ? '<span class="chip ok"><i class="dot"></i>已生效</span>'
      : v === 'mismatch' ? '<span class="chip warn"><i class="dot"></i>不一致</span>'
        : v === 'nxdomain' ? '<span class="chip bad"><i class="dot"></i>域名不存在</span>'
          : '<span class="chip bad"><i class="dot"></i>查询异常</span>';

    body.innerHTML = `<div class="detail-box" style="margin-bottom:10px">
        检测逻辑：用公共 DNS 实际查询这些记录，和本地值逐条对比。显示<a href="#" onclick="return false">已生效</a>说明解析链路正常，看不到变化多半是本地/运营商 TTL 缓存。
      </div>
      <div class="probe-grid">${items.map(it => `
        <div class="probe-row">
          <div class="fqdn" title="${esc(it.fqdn)}">${esc(it.fqdn)}</div>
          <div><span class="type-chip">${esc(it.type)}</span></div>
          <div style="display:flex;gap:14px;flex-wrap:wrap">
            ${(it.results || []).map(r => `<span class="probe-dot" title="${esc((r.records || []).join(' / '))}">
              <i style="background:${r.status === 'ok' ? 'var(--ok)' : r.status === 'nxdomain' ? 'var(--bad)' : 'var(--warn)'}"></i>
              ${esc(r.name || '')} ${r.latency_ms ? r.latency_ms + 'ms' : ''}
            </span>`).join('')}
          </div>
          <div style="text-align:right">${verdict(it.verdict)}</div>
        </div>`).join('')}</div>
      <div class="hint" style="margin-top:10px">本地值：${items.map(i => esc(`${i.sub} ${i.type} → ${(i.local || []).join(' / ')}`)).slice(0, 4).join('；')}${items.length > 4 ? ' …' : ''}</div>`;
  } catch (e) {
    m.wrap.querySelector('.modal-b').innerHTML = `<div class="load-error">${esc(e.message)}</div>`;
  }
}

/* ------------------------------ 绑定管理 ------------------------------ */
async function bindManage(id, name, accounts, refresh) {
  const bound = await Api.get(`/api/domains/${id}/accounts`).catch(() => []);
  const boundIds = new Set(bound.map(b => b.account_id));
  const free = accounts.filter(a => !boundIds.has(a.id));

  const renderBody = () => `
    <div class="detail-box" style="margin-bottom:12px">域名 <b>${esc(name)}</b> 已绑定 ${bound.length} / ${accounts.length} 个账号。开关控制该平台是否参与同步。</div>
    <div class="table-wrap" style="border:1px solid var(--line);border-radius:10px"><table>
      <thead><tr><th>已绑定账号</th><th>平台</th><th>验证</th><th style="text-align:right">同步 / 操作</th></tr></thead>
      <tbody>${bound.map(b => `<tr>
        <td><b>${esc(b.account_name || '账号#' + b.account_id)}</b></td>
        <td>${provChip(b.provider)}</td>
        <td>${b.verify_status === 'ok' ? '<span class="chip ok"><i class="dot"></i>正常</span>' : '<span class="chip bad"><i class="dot"></i>验证失败</span>'}</td>
        <td class="ops">
          <label class="switch"><input type="checkbox" data-bound-toggle="${b.account_id}" ${b.sync_enabled ? 'checked' : ''}><span class="track"></span></label>
          <button class="btn ghost sm" data-unbind="${b.account_id}" style="color:var(--bad)">解绑</button>
        </td></tr>`).join('') || '<tr><td colspan="4" class="empty">还没有绑定任何账号</td></tr>'}</tbody>
    </table></div>
    ${free.length ? `<div style="margin-top:14px" class="hint">未绑定的账号</div>
    <div class="table-wrap" style="border:1px solid var(--line);border-radius:10px;margin-top:6px"><table>
      <tbody>${free.map(a => `<tr>
        <td><b>${esc(a.name)}</b></td><td>${provChip(a.provider)}</td>
        <td class="ops"><button class="btn sm" data-bind="${a.id}">绑定</button></td></tr>`).join('')}</tbody>
    </table></div>` : ''}`;

  const m = modal({ title: `绑定管理 · ${name}`, wide: true, body: renderBody(), actions: [{ label: '完成', cls: 'primary' }] });
  const body = m.wrap.querySelector('.modal-b');

  body.onchange = async e => {
    const t = e.target.closest('input[data-bound-toggle]');
    if (!t) return;
    t.disabled = true;
    try {
      await Api.post(`/api/domains/${id}/toggle`, { account_id: +t.dataset.boundToggle, enabled: t.checked });
      toast(t.checked ? '已恢复同步' : '已暂停同步', 'ok');
      refresh();
    } catch (err) { toast(err.message, 'err'); t.checked = !t.checked; }
    finally { t.disabled = false; }
  };

  body.onclick = async e => {
    const unbindBtn = e.target.closest('button[data-unbind]');
    if (unbindBtn) {
      if (!confirm('解绑后该账号不再接收此域名的同步，本地映射会被清理，确认？')) return;
      unbindBtn.disabled = true;
      try {
        await Api.post(`/api/domains/${id}/unbind`, { account_id: +unbindBtn.dataset.unbind });
        toast('已解绑', 'ok');
        const idx = bound.findIndex(b => b.account_id === +unbindBtn.dataset.unbind);
        if (idx >= 0) { const [x] = bound.splice(idx, 1); free.push(accounts.find(a => a.id === x.account_id) || { id: x.account_id, name: x.account_name, provider: x.provider }); }
        body.innerHTML = renderBody();
        refresh();
      } catch (err) { toast(err.message, 'err'); unbindBtn.disabled = false; }
      return;
    }
    const bindBtn = e.target.closest('button[data-bind]');
    if (bindBtn) {
      bindBtn.disabled = true;
      try {
        const r = await Api.post(`/api/domains/${id}/bind`, { account_id: +bindBtn.dataset.bind });
        toast(r.verify_status === 'ok' ? '已绑定并验证通过' : '已绑定，但域名验证失败：' + (r.verify_message || ''), r.verify_status === 'ok' ? 'ok' : 'err');
        const acc = accounts.find(a => a.id === +bindBtn.dataset.bind);
        if (acc) { bound.push({ account_id: acc.id, account_name: acc.name, provider: acc.provider, verify_status: r.verify_status || 'ok', sync_enabled: 1 }); }
        body.innerHTML = renderBody();
        refresh();
      } catch (err) { toast(err.message, 'err'); bindBtn.disabled = false; }
    }
  };
}
