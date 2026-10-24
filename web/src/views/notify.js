/* 消息通知：渠道配置（企业微信 / 飞书 / 钉钉 / Webhook） */
import { $, $$, esc, icon, tshort, toast, modal, NOTIFY_TYPES, isAdmin } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

let events = [];
let channels = [];

export async function render(el) {
  if (!isAdmin()) {
    setSub('消息通知仅管理员可配置');
    el.innerHTML = `<div class="card card-b" style="display:flex;gap:10px;align-items:center">
        ${icon('bell')}<span class="hint">消息通知涉及 Webhook 地址，仅 admin 角色可见。</span></div>`;
    return;
  }

  [events, channels] = await Promise.all([
    Api.get('/api/notify/events').catch(() => []),
    Api.get('/api/notify/channels').catch(() => []),
  ]);
  setSub(`共 ${channels.length} 个通知渠道 · 同事件 5 分钟内自动去重，不会刷屏`);

  const eventLabel = t => (events.find(e => e.type === t) || { label: t }).label;

  el.innerHTML = `
    <div class="toolbar">
      <span class="hint">共 ${channels.length} 个渠道</span>
      <span class="sp"></span>
      <button class="btn primary" id="add-ch">${icon('plus')}添加渠道</button>
    </div>
    <div class="cards-grid">
      ${channels.map(c => `<div class="card acc-card">
        <div class="a-top">
          <span class="chip brand"><i class="dot"></i>${esc(NOTIFY_TYPES[c.type] || c.type)}</span>
          <span style="flex:1"></span>
          <span class="chip ${c.enabled ? 'ok' : 'tout'}"><i class="dot"></i>${c.enabled ? '启用' : '停用'}</span>
        </div>
        <div class="a-name">${esc(c.name)}</div>
        <div class="a-kv"><span>Webhook</span><b>${esc(c.url_masked || '-')}</b></div>
        <div style="display:flex;gap:5px;flex-wrap:wrap">
          ${(c.events || []).map(e => `<span class="chip tout">${esc(eventLabel(e))}</span>`).join('') || '<span class="hint">未订阅事件</span>'}
        </div>
        <div class="a-kv"><span>创建时间</span><b style="font-family:inherit">${tshort(c.created_at)}</b></div>
        <div class="a-ops">
          <button class="btn sm" data-act="test" data-id="${c.id}">发送测试</button>
          <button class="btn sm" data-act="edit" data-id="${c.id}">编辑</button>
          <button class="btn sm danger" data-act="del" data-id="${c.id}">删除</button>
        </div>
      </div>`).join('') || '<div class="card card-b empty">还没有通知渠道，添加一个后同步失败会自动通知</div>'}
    </div>`;

  $('#add-ch').onclick = () => channelForm(null);
  el.onclick = async e => {
    const b = e.target.closest('button[data-act]');
    if (!b) return;
    const id = +b.dataset.id;
    const ch = channels.find(c => c.id === id);
    if (b.dataset.act === 'edit') return channelForm(ch);
    if (b.dataset.act === 'test') {
      b.disabled = true;
      try {
        await Api.post(`/api/notify/channels/${id}/test`);
        toast('测试消息已发送，请到群里确认', 'ok');
      } catch (err) { toast(err.message, 'err'); }
      finally { b.disabled = false; }
    }
    if (b.dataset.act === 'del') {
      if (!confirm(`删除渠道「${ch?.name || id}」？`)) return;
      b.disabled = true;
      try { await Api.del(`/api/notify/channels/${id}`); toast('已删除', 'ok'); rerender(true); }
      catch (err) { toast(err.message, 'err'); b.disabled = false; }
    }
  };
}

function channelForm(ch) {
  const isEdit = !!ch;
  const picked = new Set(ch?.events || ['sync_failed']);
  modal({
    title: isEdit ? '编辑通知渠道' : '添加通知渠道',
    body: `
      <div class="field-row">
        <div class="field"><label>渠道名称</label><input class="input" id="f-name" value="${esc(ch?.name || '')}" placeholder="例如：运维群机器人"></div>
        <div class="field"><label>类型</label><select class="input" id="f-type">
          ${Object.entries(NOTIFY_TYPES).map(([k, v]) => `<option value="${k}" ${ch?.type === k ? 'selected' : ''}>${v}</option>`).join('')}
        </select></div>
      </div>
      <div class="field"><label>Webhook 地址</label><input class="input" id="f-url" placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...">
        <p class="f-hint">${isEdit ? '留空表示不修改；' : ''}地址加密存储，列表只显示掩码</p></div>
      <div class="field"><label>订阅事件</label>
        <div style="display:flex;gap:14px;flex-wrap:wrap;padding-top:2px">
          ${events.map(ev => `<label style="display:flex;align-items:center;gap:6px;font-size:13px;color:var(--ink-2)">
            <input type="checkbox" class="ev-chk" value="${ev.type}" ${picked.has(ev.type) ? 'checked' : ''}>${esc(ev.label)}</label>`).join('')}
        </div>
      </div>`,
    actions: [{ label: '取消', cls: 'ghost' }, {
      label: isEdit ? '保存' : '添加', cls: 'primary', onClick: async w => {
        const name = $('#f-name', w).value.trim();
        const type = $('#f-type', w).value;
        const url = $('#f-url', w).value.trim();
        const evs = $$('.ev-chk:checked', w).map(c => c.value);
        if (!name) throw new Error('请填写渠道名称');
        if (!isEdit && !url) throw new Error('请填写 Webhook 地址');
        if (isEdit) {
          await Api.put(`/api/notify/channels/${ch.id}`, { name, events: evs, ...(url ? { url } : {}) });
        } else {
          await Api.post('/api/notify/channels', { name, type, url, events: evs });
        }
        toast(isEdit ? '已保存' : '渠道已添加', 'ok');
        rerender(true);
      }
    }]
  });
}
