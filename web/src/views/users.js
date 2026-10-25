/* 子账号与权限：面向中小企业多员工协作，只能操作被授权的域名 */
import { $, $$, esc, icon, tshort, num, toast, modal, isAdmin, state } from '../core.js';
import { Api } from '../api.js';
import { render as rerender, setSub } from '../app.js';

const ROLE_META = {
  admin: { label: '超级管理员', cls: 'brand', desc: '全部域名 + 账号管理 + 审计日志' },
  operator: { label: '员工账号', cls: 'ok', desc: '授权域名内可增删改解析记录' },
  viewer: { label: '只读账号', cls: 'tout', desc: '授权域名内只能查看，不能修改' },
};

export async function render(el) {
  if (!isAdmin()) {
    setSub('账号管理仅超级管理员可见');
    el.innerHTML = `<div class="card card-b" style="display:flex;gap:10px;align-items:center">
      ${icon('shield')}<span class="hint">当前账号是「${esc(state.user?.role || '-')}」，无法管理子账号。</span></div>`;
    return;
  }
  const [users, domains] = await Promise.all([
    Api.get('/api/users'),
    Api.get('/api/domains').catch(() => []),
  ]);
  const staff = users.filter(u => u.role !== 'admin');
  const admins = users.filter(u => u.role === 'admin');
  setSub(`共 ${users.length} 个账号（${admins.length} 个管理员 / ${staff.length} 个子账号）· 子账号只能操作被授权的域名`);

  const roleChip = r => {
    const m = ROLE_META[r] || { label: r, cls: 'tout' };
    return `<span class="chip ${m.cls}"><i class="dot"></i>${esc(m.label)}</span>`;
  };

  const scopeCell = u => {
    if (u.role === 'admin' || u.scope_all == 1) return '<span class="chip tout">全部域名</span>';
    const names = u.domain_names || [];
    if (!names.length) return '<span class="chip warn"><i class="dot"></i>未授权</span>';
    return names.map(n => `<span class="chip ok"><i class="dot"></i>${esc(n)}</span>`).join(' ');
  };

  el.innerHTML = `
    <div class="toolbar">
      <span class="hint">子账号登录后只会看到被授权的域名，其他域名既看不到也改不了</span>
      <span class="sp"></span>
      <button class="btn primary" id="add-user">${icon('plus')}新建子账号</button>
    </div>
    <div class="card">
      <div class="table-wrap"><table>
        <thead><tr><th>账号</th><th>角色</th><th>可访问域名</th><th>最近登录</th><th>创建时间</th><th style="text-align:right">操作</th></tr></thead>
        <tbody>
          ${users.map(u => `<tr>
            <td><b>${esc(u.username)}</b>${u.id === state.user?.id ? ' <span class="chip tout">当前</span>' : ''}</td>
            <td>${roleChip(u.role)}</td>
            <td class="wrap">${scopeCell(u)}</td>
            <td class="hint">${u.last_login_at ? tshort(u.last_login_at) : '从未登录'}</td>
            <td class="hint">${tshort(u.created_at)}</td>
            <td class="ops">
              <button class="btn ghost sm" data-act="edit" data-id="${u.id}">${u.role === 'admin' ? '改密码' : '编辑'}</button>
              ${u.id === state.user?.id ? '' : `<button class="btn ghost sm" data-act="del" data-id="${u.id}" data-name="${esc(u.username)}" style="color:var(--bad)">删除</button>`}
            </td></tr>`).join('')}
        </tbody>
      </table></div>
    </div>
    <div class="card card-b">
      <div class="hint" style="line-height:1.9">
        <b style="color:var(--ink-2)">权限说明</b><br>
        · <b>超级管理员</b>：全部域名，能管理云账号、子账号与审计日志<br>
        · <b>员工账号</b>：只能在被授权的域名里增删改解析记录、做校验与生效检测<br>
        · <b>只读账号</b>：同上范围，但只能查看<br>
        · 新建域名、删除域名、管理云账号都需要超级管理员
      </div>
    </div>`;

  $('#add-user').onclick = () => userForm(null, domains);

  el.onclick = async e => {
    const b = e.target.closest('button[data-act]');
    if (!b) return;
    const id = +b.dataset.id;
    const u = users.find(x => x.id === id);
    if (b.dataset.act === 'edit') return userForm(u, domains);
    if (b.dataset.act === 'del') {
      if (!confirm(`删除账号「${u.username}」？删除后该账号立即无法登录。`)) return;
      b.disabled = true;
      try {
        await Api.del('/api/users/' + id);
        toast('账号已删除', 'ok');
        rerender(true);
      } catch (err) { toast(err.message, 'err'); b.disabled = false; }
    }
  };
}

function userForm(u, domains) {
  const isEdit = !!u;
  const isSelf = u && u.id === state.user?.id;
  const picked = new Set(u?.domain_ids || []);
  const scopeAll = u ? (u.scope_all == 1 || u.role === 'admin') : false;

  const domainChecks = domains.map(d => `
    <label style="display:flex;align-items:center;gap:7px;padding:6px 8px;border-radius:8px;font-size:13px">
      <input type="checkbox" class="dom-chk" value="${d.id}" ${picked.has(d.id) ? 'checked' : ''}>
      <span>${esc(d.name)}</span>
      <span class="hint" style="margin-left:auto">${esc(d.remark || '')}</span>
    </label>`).join('') || '<div class="hint">还没有域名可授权</div>';

  const m = modal({
    title: isEdit ? `编辑账号 · ${u.username}` : '新建子账号',
    wide: true,
    body: `
      <div class="field-row">
        <div class="field"><label>用户名</label>
          <input class="input" id="f-username" value="${esc(u?.username || '')}" ${isEdit ? 'disabled' : ''} placeholder="3~32 位字母数字">
        </div>
        <div class="field"><label>角色</label>
          <select class="input" id="f-role" ${isSelf ? 'disabled' : ''}>
            ${['admin', 'operator', 'viewer'].map(r => `<option value="${r}" ${u?.role === r ? 'selected' : ''}>${ROLE_META[r].label} — ${ROLE_META[r].desc}</option>`).join('')}
          </select>
        </div>
      </div>
      <div class="field"><label>${isEdit ? '重置密码（留空则不修改）' : '初始密码'}</label>
        <input class="input" id="f-password" type="password" autocomplete="new-password" placeholder="至少 6 位">
      </div>
      <div class="field"><label>可访问范围</label>
        <div style="display:flex;gap:16px;font-size:13px;color:var(--ink-2);padding:2px 0 8px">
          <label style="display:flex;align-items:center;gap:6px"><input type="radio" name="f-scope" value="some" ${scopeAll ? '' : 'checked'}> 仅授权域名（推荐）</label>
          <label style="display:flex;align-items:center;gap:6px"><input type="radio" name="f-scope" value="all" ${scopeAll ? 'checked' : ''}> 全部域名</label>
        </div>
        <div id="f-domains" style="max-height:210px;overflow:auto;border:1px solid var(--line);border-radius:10px;padding:6px;${scopeAll ? 'opacity:.45;pointer-events:none' : ''}">
          ${domainChecks}
        </div>
        <p class="f-hint">未勾选任何域名时，该账号登录后看不到任何域名</p>
      </div>`,
    actions: [{ label: '取消', cls: 'ghost' }, {
      label: isEdit ? '保存' : '创建', cls: 'primary', onClick: async w => {
        const username = $('#f-username', w).value.trim();
        const password = $('#f-password', w).value;
        const role = $('#f-role', w).value;
        const scope = $('input[name=f-scope]:checked', w)?.value || 'some';
        const domainIds = $$('.dom-chk:checked', w).map(c => +c.value);

        if (isEdit) {
          const body = { role, scope_all: scope === 'all', domain_ids: scope === 'all' ? [] : domainIds };
          if (password) body.password = password;
          await Api.put('/api/users/' + u.id, body);
          toast('已保存', 'ok');
        } else {
          if (!username) throw new Error('请填写用户名');
          if (password.length < 6) throw new Error('密码至少 6 位');
          await Api.post('/api/users', { username, password, role, scope_all: scope === 'all', domain_ids: domainIds });
          toast('子账号已创建', 'ok');
        }
        rerender(true);
      }
    }]
  });

  // 切换"全部域名"时禁用域名勾选
  $$('input[name=f-scope]', m.wrap).forEach(r => {
    r.onchange = () => {
      const box = $('#f-domains', m.wrap);
      const all = $('input[name=f-scope]:checked', m.wrap)?.value === 'all';
      box.style.opacity = all ? '.45' : '1';
      box.style.pointerEvents = all ? 'none' : 'auto';
    };
  });
}
