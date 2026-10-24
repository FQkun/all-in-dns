/* 监控概览 */
import { $, esc, icon, num, tshort, provChip, ACT_LABEL, PROVIDERS, toast } from '../core.js';
import { Charts } from '../charts.js';
import { Api } from '../api.js';
import { render as rerender, setSub, go } from '../app.js';

const parse = s => (s ? new Date(String(s).replace(' ', 'T')) : null);

function hourly(logs) {
  const now = new Date();
  const buckets = [];
  for (let i = 23; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3600 * 1000);
    buckets.push({
      key: `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}-${d.getHours()}`,
      hour: String(d.getHours()).padStart(2, '0') + ':00',
      success: 0, failed: 0,
    });
  }
  const map = new Map(buckets.map(b => [b.key, b]));
  for (const l of logs) {
    const d = parse(l.created_at);
    if (!d) continue;
    const b = map.get(`${d.getFullYear()}-${d.getMonth()}-${d.getDate()}-${d.getHours()}`);
    if (!b) continue;
    if (l.status === 'success') b.success++;
    else b.failed++;
  }
  return buckets;
}

export async function render(el, route, force) {
  const [summary, logs, queue, domains, accounts] = await Promise.all([
    Api.get('/api/monitor/summary'),
    Api.get('/api/sync/logs').catch(() => []),
    Api.get('/api/sync/queue').catch(() => []),
    Api.get('/api/domains').catch(() => []),
    Api.get('/api/accounts').catch(() => []),
  ]);

  const fails = queue.filter(q => q.status === 'failed');
  const recent = logs.filter(l => {
    const d = parse(l.created_at);
    return d && Date.now() - d.getTime() < 24 * 3600 * 1000;
  });
  const okN = recent.filter(l => l.status === 'success').length;
  const badN = recent.filter(l => l.status === 'failed').length;
  const rate = okN + badN ? Math.round(okN * 1000 / (okN + badN)) / 10 : 100;

  setSub(`共 ${domains.length} 个域名 · ${accounts.length} 个云账号 · ${num(summary.records)} 条解析记录`);

  const health = PROVIDERS.map(p => {
    const list = recent.filter(l => l.provider === p);
    const ok = list.filter(l => l.status === 'success').length;
    const bad = list.filter(l => l.status === 'failed').length;
    return {
      provider: p,
      accounts: accounts.filter(a => a.provider === p).length,
      ok, bad,
      rate: ok + bad ? Math.round(ok * 1000 / (ok + bad)) / 10 : null,
      last: list[0]?.created_at || null,
    };
  }).filter(h => h.accounts > 0 || h.ok + h.bad > 0);

  const kpi = o => `<div class="card kpi ${o.accent ? 'accent' : ''}">
      <div class="k-icon">${icon(o.icon)}</div>
      <div class="k-label">${o.label}</div>
      <div class="k-val">${o.value}</div>
      <div class="k-sub">${o.sub}</div>
    </div>`;

  el.innerHTML = `
    <div class="kpis">
      ${kpi({ label: '域名总数', value: num(summary.domains), icon: 'globe', accent: true, sub: `已收录 <b>${num(summary.records)}</b> 条解析记录` })}
      ${kpi({ label: '云账号', value: num(accounts.length), icon: 'cloud', sub: `覆盖 <b>${new Set(accounts.map(a => a.provider)).size}</b> 个云平台` })}
      ${kpi({ label: '待同步任务', value: num(summary.pending_queue + summary.failed_queue), icon: 'refresh', sub: `其中失败 <b>${num(summary.failed_queue)}</b> 个，自动退避重试` })}
      ${kpi({ label: '24h 同步成功率', value: rate + '%', icon: 'shield', sub: `成功 <b>${okN}</b> · 失败 <b>${badN}</b>` })}
    </div>

    <div class="grid-2">
      <div class="card">
        <div class="card-h"><h3>24 小时同步趋势</h3><span class="sp"></span>
          <div class="legend"><span><i style="background:var(--brand)"></i>成功</span><span><i style="background:var(--bad)"></i>失败</span></div>
        </div>
        <div class="card-b" style="padding:12px 14px 6px"><div id="trend-host"></div></div>
      </div>
      <div class="card">
        <div class="card-h"><h3>厂商健康度</h3><span class="sp"></span><span class="hint">近 24h</span></div>
        <div class="prov-list">
          ${health.length ? health.map(h => {
            const r = h.rate === null ? 0 : h.rate;
            return `<div class="prov-row">
              <div class="p-name">${provChip(h.provider)}<span class="chip tout">${h.accounts} 账号</span></div>
              <div class="p-rate" style="color:${r >= 98 ? 'var(--ok)' : r >= 90 ? 'var(--brand)' : 'var(--bad)'}">${h.rate === null ? '—' : r + '%'}</div>
              <div class="p-meta">${h.ok} 成功${h.bad ? ` · <span style="color:var(--bad)">${h.bad} 失败</span>` : ''} · 最近 ${tshort(h.last)}</div>
              <div class="bar"><i style="width:${Math.max(2, r)}%;${r < 90 ? 'background:linear-gradient(90deg,var(--warn),var(--bad))' : ''}"></i></div>
            </div>`;
          }).join('') : '<div class="empty">暂无调用记录</div>'}
        </div>
      </div>
    </div>

    <div class="card">
      <div class="card-h"><h3 style="${fails.length ? 'color:var(--bad)' : ''}">${fails.length ? `失败任务 ${fails.length} 个` : '同步队列'}</h3>
        <span class="sp"></span>
        <button class="btn ghost sm" id="go-sync">查看同步中心</button>
        ${fails.length ? '<button class="btn primary sm" id="btn-retry">立即重试</button>' : ''}
      </div>
      ${fails.length ? `<div class="table-wrap"><table>
        <thead><tr><th>平台</th><th>动作</th><th>记录</th><th>失败原因</th><th>重试</th></tr></thead>
        <tbody>${fails.map(f => {
          let rec = '-';
          try {
            const p = JSON.parse(f.payload || '{}');
            if (p.record) rec = `${p.domain || ''} ${p.record.sub ?? p.record.sub_domain ?? ''} ${p.record.type || ''} ${p.record.value || ''}`.trim();
          } catch (e) { /* ignore */ }
          return `<tr><td>${provChip(f.provider)}</td><td>${ACT_LABEL[f.action] || esc(f.action)}</td>
            <td class="wrap mono">${esc(rec)}</td><td class="wrap" style="color:var(--bad)">${esc(f.last_error || '-')}</td>
            <td class="num">${f.retry_count} 次</td></tr>`;
        }).join('')}</tbody></table></div>`
        : `<div class="card-b" style="display:flex;align-items:center;gap:10px;color:var(--ok)">${icon('shield')}
            <span style="font-size:13px">当前没有失败任务，各平台记录保持一致。</span></div>`}
    </div>`;

  Charts.stackedBars($('#trend-host'), hourly(logs));
  $('#go-sync').onclick = () => go('/sync');
  const retry = $('#btn-retry');
  if (retry) retry.onclick = async () => {
    retry.disabled = true;
    try {
      const r = await Api.post('/api/sync/retry-now');
      toast(`已重新执行 ${r.count ?? 0} 个任务`, 'ok');
      rerender(true);
    } catch (e) { toast(e.message, 'err'); retry.disabled = false; }
  };
}
