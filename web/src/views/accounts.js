/* 云账号 */
import { $, esc, icon, tshort, provChip, PROVIDERS, provLabel, toast, modal, isAdmin } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

export async function render(el) {
  const rows = await Api.get('/api/accounts');
  setSub(`共 ${rows.length} 个账号 · AccessKey 使用 AES-256-GCM 加密存储，列表仅显示掩码`);

  el.innerHTML = `
    <div class="toolbar">
      <span class="hint">共 ${rows.length} 个云账号</span>
      <span class="sp"></span>
      ${isAdmin() ? `<button class="btn primary" id="add-acc">${icon('plus')}添加账号</button>` : ''}
    </div>
    <div class="cards-grid">
      ${rows.map(a => `<div class="card acc-card">
        <div class="a-top">${provChip(a.provider)}<span style="flex:1"></span>
          <span class="chip ${a.status == 1 ? 'ok' : 'tout'}"><i class="dot"></i>${a.status == 1 ? '启用' : '停用'}</span></div>
        <div class="a-name">${esc(a.name)}</div>
        <div class="a-kv"><span>AccessKey</span><b>${esc(a.access_key_id_masked || '****')}</b></div>
        <div class="a-kv"><span>创建时间</span><b style="font-family:inherit">${tshort(a.created_at)}</b></div>
        <div class="a-ops">
          <button class="btn sm" data-act="test" data-id="${a.id}">连通测试</button>
          ${isAdmin() ? `<button class="btn sm danger" data-act="del" data-id="${a.id}">删除</button>` : ''}
        </div>
      </div>`).join('') || '<div class="card card-b empty">还没有云账号，先添加一个再绑定域名</div>'}
    </div>`;

  const add = $('#add-acc');
  if (add) add.onclick = () => modal({
    title: '添加云账号',
    body: `
      <div class="field"><label>云厂商</label><select class="input" id="f-provider">
        ${PROVIDERS.map(p => `<option value="${p}">${provLabel(p)}</option>`).join('')}
      </select></div>
      <div class="field"><label>账号名称</label><input class="input" id="f-name" placeholder="例如：生产账号"></div>
      <div class="field"><label>AccessKey ID</label><input class="input" id="f-ak" autocomplete="off"></div>
      <div class="field"><label>AccessKey Secret</label><input class="input" id="f-sk" type="password" autocomplete="off">
        <p class="f-hint">密钥加密后入库，任何接口都不会返回明文</p></div>`,
    actions: [{ label: '取消', cls: 'ghost' }, {
      label: '保存', cls: 'primary', onClick: async w => {
        const provider = $('#f-provider', w).value;
        const name = $('#f-name', w).value.trim() || provLabel(provider);
        const ak = $('#f-ak', w).value.trim();
        const sk = $('#f-sk', w).value.trim();
        if (!ak || !sk) throw new Error('AccessKey ID / Secret 不能为空');
        await Api.post('/api/accounts', { provider, name, access_key_id: ak, access_key_secret: sk });
        toast('账号已添加', 'ok');
        rerender(true);
      }
    }]
  });

  el.onclick = async e => {
    const b = e.target.closest('button[data-act]');
    if (!b) return;
    if (b.dataset.act === 'test') {
      b.disabled = true;
      try {
        const r = await Api.post(`/api/accounts/${b.dataset.id}/test`);
        toast(`连通正常，该账号下可见 ${r.domains ?? '-'} 个域名`, 'ok');
      } catch (err) { toast(err.message, 'err'); }
      finally { b.disabled = false; }
    }
    if (b.dataset.act === 'del') {
      if (!confirm('删除该账号？该账号的域名绑定、同步队列与记录映射会一并清理')) return;
      b.disabled = true;
      try { await Api.del('/api/accounts/' + b.dataset.id); toast('已删除', 'ok'); rerender(true); }
      catch (err) { toast(err.message, 'err'); b.disabled = false; }
    }
  };
}
