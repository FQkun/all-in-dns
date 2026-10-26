/* ==========================================================================
   演示模式 Mock 引擎
   严格对齐 Go 后端 /api/* 契约（{code,msg,data} + JWT 语义省略），
   让前端在没有 Go/MySQL/Redis 的环境里也能完整点起来。
   ========================================================================== */

const KEY = 'dns_go_demo_v1';
const pad = n => String(n).padStart(2, '0');
const fmt = d => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
const ago = min => fmt(new Date(Date.now() - min * 60000));
const now = () => fmt(new Date());

let S = null;

class ApiError extends Error {
  constructor(msg, status = 400) { super(msg); this.status = status; }
}
const fail = (msg, status) => { throw new ApiError(msg, status); };
const nextId = arr => (arr && arr.length ? Math.max(...arr.map(x => x.id)) + 1 : 1);

/* ------------------------------- 种子数据 ------------------------------- */
function seed() {
  const s = {
    user: { id: 1, username: 'admin', role: 'admin', last_login_at: now() },
    users: [
      { id: 1, username: 'admin', role: 'admin', scope_all: 1, created_at: ago(60 * 24 * 200), last_login_at: now(), domain_ids: [] },
      { id: 2, username: 'zhang.wei', role: 'operator', scope_all: 0, created_at: ago(60 * 24 * 40), last_login_at: ago(180), domain_ids: [1] },
      { id: 3, username: 'li.na', role: 'operator', scope_all: 0, created_at: ago(60 * 24 * 26), last_login_at: ago(1500), domain_ids: [2] },
      { id: 4, username: 'guest.audit', role: 'viewer', scope_all: 0, created_at: ago(60 * 24 * 9), last_login_at: ago(60 * 24 * 3), domain_ids: [3, 4] },
    ],
    accounts: [
      { id: 1, provider: 'tencent', name: '腾讯云 · 主账号', status: 1, created_at: ago(60 * 24 * 120), access_key_id_masked: 'AKID****4821' },
      { id: 2, provider: 'aliyun', name: '阿里云 · 生产账号', status: 1, created_at: ago(60 * 24 * 96), access_key_id_masked: 'LTAI****9102' },
      { id: 3, provider: 'volc', name: '火山引擎 · 备用', status: 1, created_at: ago(60 * 24 * 41), access_key_id_masked: 'AKLT****7745' },
      { id: 4, provider: 'spaceship', name: 'Spaceship · 新域名', status: 1, created_at: ago(60 * 24 * 9), access_key_id_masked: 'SPSK****3310' },
    ],
    domains: [
      { id: 1, name: 'example.com', status: 'active', remark: '官网主域名', created_at: ago(60 * 24 * 200) },
      { id: 2, name: 'shop-api.cn', status: 'active', remark: '电商接口', created_at: ago(60 * 24 * 120) },
      { id: 3, name: 'static-cdn.net', status: 'active', remark: '静态资源 CDN', created_at: ago(60 * 24 * 60) },
      { id: 4, name: 'mail-crop.com', status: 'pending', remark: '邮件服务 · 待校验', created_at: ago(300) },
      { id: 5, name: 'newbrand.io', status: 'active', remark: 'Spaceship 测试', created_at: ago(60 * 24 * 8) },
    ],
    bindings: [],
    records: [],
    syncLogs: [],
    queue: [],
    opLogs: [],
    backups: [],
    channels: [
      { id: 1, name: '运维群 · 企业微信', type: 'wecom', url_masked: 'https://qyapi.weixin.qq.com/**', events: ['sync_failed', 'verify_failed'], enabled: 1, created_at: ago(60 * 24 * 30) },
      { id: 2, name: '值班机器人 · 飞书', type: 'feishu', url_masked: 'https://open.feishu.cn/**', events: ['sync_failed'], enabled: 1, created_at: ago(60 * 24 * 12) },
    ],
    trendVals: [],
  };

  const bind = (domain_id, account_id, provider, name, on, verify) => s.bindings.push({
    domain_id, account_id, provider, account_name: name,
    sync_enabled: on ? 1 : 0, verify_status: verify || 'ok',
  });
  for (const d of s.domains) {
    const trio = [[1, 'tencent', '腾讯云 · 主账号'], [2, 'aliyun', '阿里云 · 生产账号'], [3, 'volc', '火山引擎 · 备用'], [4, 'spaceship', 'Spaceship · 新域名']];
    trio.forEach(([aid, prov, name]) => {
      if (d.id === 5 && prov !== 'spaceship' && prov !== 'aliyun') return;
      const on = !((d.id === 2 && prov === 'spaceship') || (d.id === 3 && prov === 'tencent'));
      bind(d.id, aid, prov, name, on, d.id === 4 && prov === 'volc' ? 'fail' : 'ok');
    });
  }

  let rid = 0;
  const add = (domain_id, sub, type, value, o = {}) => {
    const id = ++rid;
    s.records.push({
      id, domain_id, sub_domain: sub, type, line: o.line || '默认', value,
      ttl: o.ttl || 600, priority: o.priority ?? null, remark: o.remark || '',
      status: o.status || 'active', updated_at: ago(o.mins || 120), created_at: ago(o.mins || 120),
    });
    return id;
  };
  // example.com
  add(1, '@', 'A', '1.2.3.4', { ttl: 600, mins: 43 });
  add(1, 'www', 'CNAME', 'example.com', { ttl: 600, mins: 43 });
  add(1, '@', 'MX', 'mx1.mail-example.com', { priority: 10, mins: 300 });
  add(1, 'mail', 'A', '1.2.3.9', { mins: 300 });
  add(1, '@', 'TXT', 'v=spf1 include:_spf.example.com -all', { mins: 1500 });
  add(1, 'api', 'A', '1.2.3.7', { ttl: 120, mins: 88 });
  add(1, '_dmarc', 'TXT', 'v=DMARC1; p=none; rua=mailto:dmarc@example.com', { ttl: 3600, mins: 1500 });
  add(1, 'shop', 'CNAME', 'shop.cdn-provider.net', { mins: 260 });
  // shop-api.cn
  add(2, '@', 'A', '47.101.22.8', { ttl: 60, mins: 15 });
  add(2, 'www', 'A', '47.101.22.9', { ttl: 60, mins: 15 });
  add(2, 'api', 'AAAA', '2408:4003:1a::8', { ttl: 120, mins: 31 });
  add(2, 'cdn', 'CNAME', 'shop-api.cn.cdn.volces.com', { ttl: 300, mins: 210 });
  add(2, '_verify', 'TXT', 'verify-8f2a91c3d4', { mins: 210 });
  // static-cdn.net
  add(3, 'img', 'CNAME', 'img.static-cdn.net', { ttl: 300, mins: 26 });
  add(3, 'dl', 'A', '39.98.11.20', { ttl: 300, mins: 26 });
  add(3, '@', 'NS', 'ns1.alidns.com', { ttl: 3600, mins: 5000 });
  add(3, 'oss', 'CNAME', 'static-cdn.oss.aliyuncs.com', { status: 'disabled', mins: 47 });
  // mail-crop.com
  add(4, '@', 'MX', 'mx.mail-crop.com', { priority: 10, mins: 12 });
  add(4, 'mail', 'A', '122.51.8.6', { mins: 12 });
  add(4, 'smtp', 'CNAME', 'smtp.mail-crop.com', { mins: 12 });
  add(4, '@', 'TXT', 'v=spf1 mx -all', { mins: 12 });
  // newbrand.io
  add(5, '@', 'A', '18.166.22.41', { mins: 90 });
  add(5, 'www', 'CNAME', 'newbrand.io', { mins: 90 });
  add(5, 'blog', 'CNAME', 'blog.newbrand.io', { mins: 240 });

  // 同步日志
  const msgs = ['OK', '记录下发成功', '远端已存在，自动登记', 'RequestLimitExceeded: 请求频率超限', 'DomainRecordDuplicate: 记录重复'];
  let lid = 0;
  for (let i = 0; i < 52; i++) {
    const fail2 = i % 11 === 4;
    const acc = s.accounts[i % s.accounts.length];
    s.syncLogs.push({
      id: ++lid, record_id: s.records[i % s.records.length].id, account_id: acc.id, provider: acc.provider,
      action: ['create', 'update', 'delete'][i % 3], status: fail2 ? 'failed' : 'success',
      message: fail2 ? msgs[3 + (i % 2)] : msgs[i % 3],
      request_cost_ms: 120 + (i * 47) % 520, created_at: ago(4 + i * 26),
      p: acc.provider, account_name: acc.name,
    });
  }

  // 重试队列
  s.queue = [
    { id: 3, record_id: 15, account_id: 3, provider: 'volc', action: 'create', status: 'failed', retry_count: 2, last_error: 'RequestLimitExceeded: 请求频率超限，已退避 120s', created_at: ago(40), executed_at: null, next_retry_at: ago(-3), payload: JSON.stringify({ domain: 'static-cdn.net', record: { sub: 'dl', type: 'A', value: '39.98.11.20' } }) },
    { id: 2, record_id: 10, account_id: 2, provider: 'aliyun', action: 'update', status: 'failed', retry_count: 1, last_error: 'DomainRecordDuplicate: 记录重复', created_at: ago(75), executed_at: null, next_retry_at: ago(-1), payload: JSON.stringify({ domain: 'shop-api.cn', record: { sub: 'www', type: 'A', value: '47.101.22.9' } }) },
    { id: 1, record_id: 17, account_id: 2, provider: 'aliyun', action: 'delete', status: 'pending', retry_count: 1, last_error: 'DomainRecordNotBelongToUser', created_at: ago(120), executed_at: null, next_retry_at: null, payload: JSON.stringify({ domain: 'static-cdn.net', record: { sub: 'oss', type: 'CNAME', value: 'static-cdn.oss.aliyuncs.com' } }) },
  ];

  // 操作日志
  const ops = [
    ['record.create', 'example.com / api A', '新增解析记录，4 端同步成功', 12],
    ['domain.verify', 'example.com', '一致性校验通过', 35],
    ['domain.bind', 'newbrand.io', '绑定账号 spaceship', 60],
    ['record.update', 'shop-api.cn / www A', '修改记录值', 88],
    ['account.test', 'aliyun#2', '连通测试成功', 120],
    ['record.batch_delete', 'static-cdn.net', '批量删除 2 条记录', 200],
    ['backup.create', '手动备份', '共 23 条记录', 300],
    ['login', '', '登录成功 IP 116.24.88.12', 320],
    ['domain.toggle', 'static-cdn.net tencent', '暂停腾讯云同步', 420],
    ['record.import', 'shop-api.cn', 'CSV 导入 5 条，跳过 1 条', 1500],
    ['backup.restore', '#1', '恢复前已自动快照', 2600],
    ['notify.create', '值班机器人 · 飞书', 'type=feishu', 3000],
  ];
  s.opLogs = ops.map((o, i) => ({
    id: ops.length - i, user_id: 1, username: 'admin', action: o[0], target: o[1],
    detail: o[2], ip: '116.24.88.12', created_at: ago(o[3]),
  }));
  // 子账号相关留痕
  s.opLogs.unshift({ id: ops.length + 2, user_id: 1, username: 'admin', action: 'user.create', target: 'zhang.wei', detail: '{"role":"operator","domains":[1]}', ip: '116.24.88.12', created_at: ago(60 * 24 * 40) });
  s.opLogs.unshift({ id: ops.length + 3, user_id: 1, username: 'admin', action: 'user.create', target: 'li.na', detail: '{"role":"operator","domains":[2]}', ip: '116.24.88.12', created_at: ago(60 * 24 * 26) });
  s.opLogs.unshift({ id: ops.length + 4, user_id: 2, username: 'zhang.wei', action: 'record.create', target: 'example.com / api A', detail: '新增解析记录', ip: '116.24.88.31', created_at: ago(120) });

  s.backups = [
    { id: 3, name: '每日自动备份 ' + ago(720).slice(0, 10), record_count: s.records.length, created_by: 'system', created_at: ago(720) },
    { id: 2, name: '恢复前快照 ' + ago(2600).slice(0, 10), record_count: s.records.length - 3, created_by: 'admin', created_at: ago(2600) },
    { id: 1, name: '手动备份 ' + ago(4300).slice(0, 10), record_count: s.records.length - 6, created_by: 'admin', created_at: ago(4300) },
  ];

  const shape = [2, 1, 1, 3, 6, 14, 26, 31, 34, 29, 33, 36, 30, 27, 33, 38, 31, 24, 19, 15, 11, 8, 5, 3];
  s.trendVals = shape.map((v, i) => ({
    success: v + ((i * 7) % 5),
    failed: [0, 0, 1, 0, 2, 1, 0, 4, 1, 0, 0, 2, 1, 0, 3, 0, 1, 0, 0, 1, 0, 0, 0, 0][i],
  }));

  return s;
}

function load() {
  try {
    const raw = localStorage.getItem(KEY);
    if (raw) {
      const s = JSON.parse(raw);
      if (s && s.records && s.trendVals) return s;
    }
  } catch (e) { /* ignore */ }
  return null;
}
function save() {
  try { localStorage.setItem(KEY, JSON.stringify(S)); } catch (e) { /* ignore */ }
}
const touch = () => {
  if (!S) S = load() || seed();
  if (!S.users) S.users = seed().users; // 兼容旧版 localStorage 缓存
  return S;
};

/* ------------------------------- 工具 ------------------------------- */
const bindingsOf = id => touch().bindings.filter(b => b.domain_id === +id);
const domainOf = id => touch().domains.find(d => d.id === +id);
const recordsOf = id => touch().records.filter(r => r.domain_id === +id);

function syncStates(record) {
  const bs = bindingsOf(record.domain_id).filter(b => b.sync_enabled);
  if (!bs.length) return '';
  const failed = record.status === 'disabled';
  return bs.map((b, i) => {
    const st = failed && i === 0 ? 'pending' : (record._fail && record._fail.includes(b.provider) ? 'failed' : 'success');
    return `${b.account_id}|${b.provider}|${st}`;
  }).join(',');
}

function providersStr(domainId) {
  const bs = bindingsOf(domainId);
  return bs.map(b => `${b.provider}:${b.sync_enabled ? 'on' : 'off'}`).join(',');
}

function op(action, target, detail) {
  touch().opLogs.unshift({
    id: nextId(S.opLogs), user_id: 1, username: S.user.username, action, target, detail,
    ip: '116.24.88.12', created_at: now(),
  });
}

function syncLog(rec, account, action, status, message) {
  touch().syncLogs.unshift({
    id: nextId(S.syncLogs), record_id: rec ? rec.id : null, account_id: account.id, provider: account.provider,
    action, status, message, request_cost_ms: 150 + Math.floor(Math.random() * 400),
    created_at: now(), p: account.provider, account_name: account.name,
  });
  if (S.syncLogs.length > 300) S.syncLogs.length = 300;
}

function simulateSync(rec, action) {
  const enabledIds = new Set(bindingsOf(rec.domain_id).filter(b => b.sync_enabled).map(b => b.account_id));
  const results = [];
  rec._fail = [];
  for (const acc of touch().accounts) {
    if (!enabledIds.has(acc.id) || acc.status !== 1) continue;
    const bad = action !== 'delete' && Math.random() < 0.1;
    if (bad) rec._fail.push(acc.provider);
    syncLog(rec, acc, action, bad ? 'failed' : 'success',
      bad ? 'RequestLimitExceeded: 请求频率超限' : (bad ? 'error' : 'OK'));
    if (bad) {
      S.queue.push({
        id: nextId(S.queue), record_id: rec.id, account_id: acc.id, provider: acc.provider, action,
        status: 'failed', retry_count: 0, last_error: 'RequestLimitExceeded: 请求频率超限',
        created_at: now(), executed_at: null, next_retry_at: null,
        payload: JSON.stringify({ domain: domainOf(rec.domain_id)?.name, record: { sub: rec.sub_domain, type: rec.type, value: rec.value } }),
      });
    }
    results.push({ account_id: acc.id, provider: acc.provider, status: bad ? 'failed' : 'success', queue_id: results.length + 1 });
  }
  return results;
}

const TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA'];
function validateRecord(inp) {
  const type = String(inp.type || '').toUpperCase().trim();
  const sub = String(inp.sub ?? '@').trim() || '@';
  const line = String(inp.line ?? '默认').trim() || '默认';
  const value = String(inp.value ?? '').trim();
  const ttl = +(inp.ttl ?? 600);
  if (!TYPES.includes(type)) fail('不支持的记录类型');
  if (!value) fail('记录值不能为空');
  if (ttl < 60 || ttl > 86400) fail('TTL 需在 60~86400 秒之间');
  if (type === 'A' && !/^(\d{1,3}\.){3}\d{1,3}$/.test(value)) fail('A 记录值需为 IPv4 地址');
  if (type === 'AAAA' && !/^[0-9a-fA-F:]{2,39}$/.test(value)) fail('AAAA 记录值需为 IPv6 地址');
  if (type === 'CNAME' && !/^([a-z0-9_*.-]+\.)+[a-z]{2,}\.?$/i.test(value)) fail('CNAME 记录值需为域名');
  let priority = inp.priority === null || inp.priority === '' || inp.priority === undefined ? null : +inp.priority;
  if (type === 'MX' && (priority === null || priority < 0 || priority > 65535)) fail('MX 优先级不合法');
  if (type !== 'MX') priority = null;
  return { sub_domain: sub, type, line, value, ttl, priority, remark: inp.remark || '' };
}

function checkConflict(domainId, r, excludeId) {
  const dup = touch().records.find(x => x.domain_id === domainId && x.id !== excludeId &&
    x.sub_domain.toLowerCase() === r.sub_domain.toLowerCase() && x.type === r.type &&
    x.line === r.line && x.value === r.value);
  if (dup) fail(dup.status === 'disabled' ? '记录已存在（待云端清理）' : '记录已存在：同一主机 / 线路 / 类型下已有相同记录值', 409);
  if (['A', 'AAAA', 'CNAME'].includes(r.type)) {
    const others = ['A', 'AAAA'].includes(r.type) ? ['CNAME'] : ['A', 'AAAA'];
    const hit = touch().records.find(x => x.domain_id === domainId && x.id !== excludeId &&
      x.sub_domain.toLowerCase() === r.sub_domain.toLowerCase() && x.line === r.line &&
      x.status === 'active' && others.includes(x.type));
    if (hit) fail(`记录冲突：${r.sub_domain} / ${r.line} 已存在 ${hit.type} 记录，不能与 ${r.type} 共存`, 409);
  }
}

/* ------------------------------- 路由 ------------------------------- */
const routes = [];
function route(method, path, fn) {
  const keys = [];
  const re = new RegExp('^' + path.replace(/\{(\w+)\}/g, (_, k) => { keys.push(k); return '([^/]+)'; }) + '$');
  routes.push({ method, re, keys, fn });
}

route('GET', '/api/auth/me', () => ({ user: touch().user }));
route('POST', '/api/auth/login', (p, q, b) => {
  if (!b.username || !b.password) fail('用户名或密码错误', 401);
  S.user.last_login_at = now();
  save();
  return { token: 'demo-token', user: S.user };
});
route('POST', '/api/auth/logout', () => null);

route('GET', '/api/accounts', () => touch().accounts.slice());
route('POST', '/api/accounts', (p, q, b) => {
  if (!['tencent', 'aliyun', 'volc', 'spaceship'].includes(b.provider)) fail('无效的云厂商');
  if (!b.access_key_id || !b.access_key_secret) fail('名称和密钥不能为空');
  S.accounts.push({
    id: nextId(S.accounts), provider: b.provider, name: b.name || b.provider,
    status: 1, created_at: now(), access_key_id_masked: '****' + String(1000 + nextId(S.accounts) * 37).slice(-4),
  });
  op('account.create', b.name || b.provider, b.provider);
  save();
  return null;
});
route('DELETE', '/api/accounts/{id}', p => {
  const id = +p.id;
  S.accounts = S.accounts.filter(a => a.id !== id);
  S.bindings = S.bindings.filter(b => b.account_id !== id);
  op('account.delete', String(id), '');
  save();
  return null;
});
route('POST', '/api/accounts/{id}/test', p => {
  const acc = S.accounts.find(a => a.id === +p.id);
  if (!acc) fail('账号不存在', 404);
  op('account.test', `${acc.provider}#${acc.id}`, '连通测试成功');
  save();
  return { ok: true, domains: 3 + (acc.id % 4) };
});

route('GET', '/api/domains', () => touch().domains.map(d => ({ ...d, providers: providersStr(d.id) })));
route('POST', '/api/domains', (p, q, b) => {
  const name = String(b.name || '').toLowerCase().trim();
  if (!/^([a-z0-9-]+\.)+[a-z]{2,}$/.test(name)) fail('域名格式不正确');
  if (S.domains.some(d => d.name === name)) fail('域名已存在');
  const id = nextId(S.domains);
  S.domains.push({ id, name, status: 'active', remark: b.remark || '', created_at: now() });
  for (const acc of S.accounts) {
    S.bindings.push({ domain_id: id, account_id: acc.id, provider: acc.provider, account_name: acc.name, sync_enabled: 1, verify_status: 'ok' });
  }
  op('domain.create', name, '');
  save();
  return { domain_id: id };
});
route('GET', '/api/domains/{id}/accounts', p => bindingsOf(p.id).map(b => ({
  account_id: b.account_id, account_name: b.account_name, provider: b.provider,
  verify_status: b.verify_status, sync_enabled: b.sync_enabled,
})));
route('POST', '/api/domains/{id}/bind', (p, q, b) => {
  const acc = S.accounts.find(a => a.id === +b.account_id);
  if (!acc) fail('账号不存在', 404);
  if (bindingsOf(p.id).some(x => x.account_id === acc.id)) fail('该账号已绑定到此域名');
  S.bindings.push({ domain_id: +p.id, account_id: acc.id, provider: acc.provider, account_name: acc.name, sync_enabled: 1, verify_status: 'ok' });
  op('domain.bind', domainOf(p.id)?.name || p.id, acc.provider);
  save();
  return { verify_status: 'ok', verify_message: '' };
});
route('POST', '/api/domains/{id}/unbind', (p, q, b) => {
  const aid = +b.account_id;
  if (!bindingsOf(p.id).some(x => x.account_id === aid)) fail('该账号未绑定到此域名');
  S.bindings = S.bindings.filter(x => !(x.domain_id === +p.id && x.account_id === aid));
  op('domain.unbind', String(p.id), 'account=' + aid);
  save();
  return null;
});
route('POST', '/api/domains/{id}/toggle', (p, q, b) => {
  const bd = S.bindings.find(x => x.domain_id === +p.id && x.account_id === +b.account_id);
  if (bd) bd.sync_enabled = b.enabled ? 1 : 0;
  op('domain.toggle', `${domainOf(p.id)?.name} #${b.account_id}`, b.enabled ? '恢复同步' : '暂停同步');
  save();
  return null;
});
route('POST', '/api/domains/{id}/verify', p => {
  const local = recordsOf(p.id).filter(r => r.status === 'active');
  return bindingsOf(p.id).map((b, i) => {
    const broken = i === 1 && local.length > 5;
    const missing = broken ? local.slice(-2).map(r => ({ id: r.id, key: `${r.sub_domain}|${r.type}|${r.value}|${r.line}` })) : [];
    return {
      account_id: b.account_id, provider: b.provider,
      status: broken ? 'inconsistent' : 'consistent',
      local_count: local.length, remote_count: local.length - missing.length,
      missing_local: missing.length, extra_remote: 0,
      detail: { missing, extra: [] },
    };
  });
});
route('POST', '/api/domains/{id}/repair', (p, q, b) => {
  const ids = (b.record_ids || []).map(Number);
  const acc = S.accounts.find(a => a.id === +b.account_id);
  const sync = ids.map((id, i) => {
    const rec = S.records.find(r => r.id === id);
    if (rec && acc) syncLog(rec, acc, 'create', 'success', '一致性修复推送成功');
    return { queue_id: i + 1, provider: acc?.provider, status: 'success' };
  });
  op('record.repair', String(p.id), `推送 ${ids.length} 条`);
  save();
  return { sync };
});
route('GET', '/api/domains/{id}/probe', p => {
  const d = domainOf(p.id);
  const recs = recordsOf(p.id).filter(r => r.status === 'active' && r.line === '默认').slice(0, 8);
  const endpoints = [
    { name: 'aliyun DoH 223.5.5.5', latency: 21 },
    { name: 'tencent DoH 119.29.29.29', latency: 28 },
    { name: 'google DoH 8.8.8.8', latency: 63 },
  ];
  const items = recs.map((r, idx) => {
    const fqdn = r.sub_domain === '@' ? d.name : `${r.sub_domain}.${d.name}`;
    const mismatch = idx === 2;
    const results = endpoints.map((e, i) => ({
      name: e.name,
      status: 'ok',
      records: mismatch && i === 2 ? ['198.51.100.7'] : [r.value],
      latency_ms: e.latency + i * 3,
    }));
    return {
      sub: r.sub_domain, type: r.type, fqdn, local: [r.value], results,
      verdict: mismatch ? 'mismatch' : 'ok',
    };
  });
  return { domain: d.name, items };
});
route('DELETE', '/api/domains/{id}', p => {
  const id = +p.id;
  S.records = S.records.filter(r => r.domain_id !== id);
  S.bindings = S.bindings.filter(b => b.domain_id !== id);
  S.domains = S.domains.filter(d => d.id !== id);
  op('domain.delete', String(id), '');
  save();
  return null;
});

route('GET', '/api/domains/{id}/records', (p, q) => {
  const size = Math.min(200, Math.max(20, +(q.size || 100)));
  const page = Math.max(1, +(q.page || 1));
  const list = recordsOf(p.id).slice().sort((a, b) => a.type.localeCompare(b.type) || a.sub_domain.localeCompare(b.sub_domain) || a.id - b.id);
  return {
    items: list.slice((page - 1) * size, page * size).map(r => ({ ...r, sync_states: syncStates(r) })),
    total: list.length, page, size,
  };
});
route('POST', '/api/domains/{id}/records', (p, q, b) => {
  const d = domainOf(p.id);
  if (!d) fail('域名不存在', 404);
  const r = validateRecord(b);
  checkConflict(d.id, r);
  const rec = { id: nextId(S.records), domain_id: d.id, ...r, status: 'active', updated_at: now(), created_at: now() };
  S.records.push(rec);
  const sync = simulateSync(rec, 'create');
  op('record.create', `${d.name} / ${r.sub_domain} ${r.type}`, `${sync.length} 端下发`);
  save();
  return { record_id: rec.id, sync };
});
route('POST', '/api/records/{id}', (p, q, b) => {
  const rec = S.records.find(r => r.id === +p.id);
  if (!rec) fail('记录不存在', 404);
  const r = validateRecord(b);
  checkConflict(rec.domain_id, r, rec.id);
  Object.assign(rec, r, { status: 'active', updated_at: now() });
  const sync = simulateSync(rec, 'update');
  op('record.update', `${domainOf(rec.domain_id)?.name} / ${r.sub_domain} ${r.type}`, '');
  save();
  return { sync };
});
route('DELETE', '/api/records/{id}', p => {
  const rec = S.records.find(r => r.id === +p.id);
  if (!rec) fail('记录不存在', 404);
  const sync = simulateSync(rec, 'delete');
  const failed = sync.some(s => s.status !== 'success');
  if (failed) rec.status = 'disabled';
  else S.records = S.records.filter(r => r.id !== rec.id);
  op('record.delete', String(p.id), failed ? '部分平台失败，等待重试' : '');
  save();
  return { sync, pending: failed };
});
route('POST', '/api/records/batch-delete', (p, q, b) => {
  const ids = (b.ids || []).map(Number);
  if (!ids.length) fail('ids 不能为空');
  const pending = [];
  for (const id of ids) {
    const rec = S.records.find(r => r.id === id);
    if (!rec) continue;
    const sync = simulateSync(rec, 'delete');
    if (sync.some(s => s.status !== 'success')) { rec.status = 'disabled'; pending.push(id); }
    else S.records = S.records.filter(r => r.id !== id);
  }
  op('record.batch_delete', '-', `删除 ${ids.length} 条，${pending.length} 条待重试`);
  save();
  return { pending };
});
route('POST', '/api/domains/{id}/import-remote', p => {
  const d = domainOf(p.id);
  if (!d) fail('域名不存在', 404);
  const extra = [
    { sub: 'blog', type: 'CNAME', value: `blog.${d.name}` },
    { sub: 'test', type: 'A', value: '10.0.0.21' },
  ];
  let imported = 0;
  for (const e of extra) {
    if (S.records.some(r => r.domain_id === d.id && r.sub_domain === e.sub && r.type === e.type)) continue;
    S.records.push({ id: nextId(S.records), domain_id: d.id, sub_domain: e.sub, type: e.type, line: '默认', value: e.value, ttl: 600, priority: null, remark: '', status: 'active', updated_at: now(), created_at: now() });
    imported++;
  }
  op('record.import_remote', d.name, `拉取 ${imported} 条`);
  save();
  return { imported, merged: 0, errors: [] };
});
route('POST', '/api/domains/{id}/import', (p, q, b) => {
  const d = domainOf(p.id);
  const rows = String(b.csv || '').split(/\r?\n/).filter(Boolean).slice(1);
  if (!rows.length) fail('csv 内容为空');
  let imported = 0, skipped = 0;
  for (const line of rows) {
    const c = line.split(',');
    if (c.length < 4) { skipped++; continue; }
    try {
      const r = validateRecord({ sub: c[0] || '@', type: c[1], line: c[2] || '默认', value: c[3], ttl: c[4] || 600, priority: c[5] || null });
      if (S.records.some(x => x.domain_id === d.id && x.sub_domain === r.sub_domain && x.type === r.type && x.value === r.value)) { skipped++; continue; }
      S.records.push({ id: nextId(S.records), domain_id: d.id, ...r, status: 'active', updated_at: now(), created_at: now() });
      imported++;
    } catch (e) { skipped++; }
  }
  op('record.import', d.name, `导入 ${imported} 条，跳过 ${skipped} 条`);
  save();
  return { imported, skipped, failed: 0, sync: [] };
});
route('GET', '/api/domains/{id}/export', p => {
  const d = domainOf(p.id);
  const rows = [['sub_domain', 'type', 'line', 'value', 'ttl', 'priority']];
  for (const r of recordsOf(p.id)) {
    rows.push([r.sub_domain, r.type, r.line, `"${String(r.value).replace(/"/g, '""')}"`, r.ttl, r.priority ?? '']);
  }
  return { csv: rows.map(r => r.join(',')).join('\n'), name: `records_${d ? d.name : p.id}.csv` };
});

route('GET', '/api/sync/logs', () => touch().syncLogs.slice(0, 200));
route('DELETE', '/api/sync/logs', () => { S.syncLogs = []; op('synclog.clear', '', ''); save(); return null; });
route('GET', '/api/sync/queue', () => touch().queue.slice());
route('POST', '/api/sync/retry-now', () => {
  const items = S.queue.slice();
  let count = 0;
  for (const q of items) {
    if (Math.random() < 0.8) {
      q.status = 'success';
      count++;
      const rec = S.records.find(r => r.id === q.record_id);
      const acc = S.accounts.find(a => a.id === q.account_id);
      if (rec && acc) syncLog(rec, acc, q.action, 'success', '重试成功');
    } else {
      q.retry_count++;
      q.status = 'failed';
    }
  }
  S.queue = S.queue.filter(q => q.status !== 'success');
  op('sync.retry', '', `重试 ${items.length} 个`);
  save();
  return { count, items };
});
route('DELETE', '/api/sync/queue', () => {
  const n = S.queue.length;
  S.queue = [];
  op('syncqueue.clear', '', `清空 ${n} 个`);
  save();
  return { count: n };
});

route('GET', '/api/operation-logs', () => touch().opLogs.slice(0, 200));
route('DELETE', '/api/operation-logs', () => { S.opLogs = []; save(); return null; });

route('GET', '/api/backups', () => touch().backups.slice().sort((a, b) => b.id - a.id));
route('POST', '/api/backups', () => {
  S.backups.push({ id: nextId(S.backups), name: '手动备份 ' + now().slice(0, 16), record_count: S.records.length, created_by: S.user.username, created_at: now() });
  op('backup.create', '', '');
  save();
  return null;
});
route('POST', '/api/backups/delete', (p, q, b) => {
  const ids = (b.ids || []).map(Number);
  S.backups = S.backups.filter(x => !ids.includes(x.id));
  op('backup.delete', ids.join(','), '');
  save();
  return { deleted: ids.length };
});
route('POST', '/api/backups/{id}/restore', p => {
  if (!S.backups.some(b => b.id === +p.id)) fail('备份不存在', 404);
  op('backup.restore', '#' + p.id, '演示模式不覆盖数据');
  save();
  return { need_verify: true };
});

route('GET', '/api/monitor/summary', () => ({
  domains: S.domains.length,
  records: S.records.length,
  pending_queue: S.queue.filter(q => q.status === 'pending').length,
  failed_queue: S.queue.filter(q => q.status === 'failed').length,
  failed_sync_24h: S.syncLogs.filter(l => l.status === 'failed').length,
  success_sync_24h: S.syncLogs.filter(l => l.status === 'success').length,
}));

route('GET', '/api/notify/events', () => [
  { type: 'sync_failed', label: '同步失败' },
  { type: 'verify_failed', label: '域名验证失败' },
  { type: 'backup_done', label: '自动备份完成' },
]);
route('GET', '/api/notify/channels', () => touch().channels.slice());
route('POST', '/api/notify/channels', (p, q, b) => {
  if (!b.name || !b.url) fail('名称和 Webhook URL 不能为空');
  if (!['wecom', 'feishu', 'dingtalk', 'webhook'].includes(b.type)) fail('无效的渠道类型');
  S.channels.push({
    id: nextId(S.channels), name: b.name, type: b.type,
    url_masked: String(b.url).replace(/^(https?:\/\/[^/]+).*/, '$1/**'),
    events: Array.isArray(b.events) ? b.events : [b.events].filter(Boolean),
    enabled: 1, created_at: now(),
  });
  op('notify.create', b.name, 'type=' + b.type);
  save();
  return null;
});
route('PUT', '/api/notify/channels/{id}', (p, q, b) => {
  const ch = S.channels.find(c => c.id === +p.id);
  if (!ch) fail('渠道不存在', 404);
  if (b.name) ch.name = b.name;
  if (b.url) ch.url_masked = b.url;
  if (b.events) ch.events = b.events;
  if (b.enabled !== undefined) ch.enabled = b.enabled ? 1 : 0;
  op('notify.update', ch.name, '');
  save();
  return null;
});
route('DELETE', '/api/notify/channels/{id}', p => {
  S.channels = S.channels.filter(c => c.id !== +p.id);
  op('notify.delete', String(p.id), '');
  save();
  return null;
});
route('POST', '/api/notify/channels/{id}/test', p => {
  const ch = S.channels.find(c => c.id === +p.id);
  if (!ch) fail('渠道不存在', 404);
  op('notify.test', ch.name, '发送测试消息');
  save();
  return { ok: true };
});

/* ---------- 子账号与权限（演示实现） ---------- */
route('GET', '/api/users', () => touch().users.map(u => ({
  ...u,
  domain_names: (u.domain_ids || [])
    .map(id => (S.domains.find(d => d.id === id) || {}).name)
    .filter(Boolean),
})));

route('POST', '/api/users', (p, q, b) => {
  const username = String(b.username || '').trim();
  if (!/^[A-Za-z0-9_.-]{3,32}$/.test(username)) fail('用户名需为 3~32 位字母、数字、下划线、点或短横线');
  if (String(b.password || '').length < 6) fail('密码至少 6 位');
  if (!['admin', 'operator', 'viewer'].includes(b.role)) fail('无效的角色');
  if (S.users.some(u => u.username === username)) fail('用户名已被占用');
  const id = nextId(S.users);
  const scopeAll = b.scope_all ? 1 : 0;
  S.users.push({
    id, username, role: b.role, scope_all: scopeAll,
    created_at: now(), last_login_at: null,
    domain_ids: scopeAll ? [] : (Array.isArray(b.domain_ids) ? b.domain_ids.map(Number) : []),
  });
  op('user.create', username, JSON.stringify({ role: b.role, scope_all: scopeAll, domains: b.domain_ids || [] }));
  save();
  return { user_id: id };
});

route('PUT', '/api/users/{id}', (p, q, b) => {
  const u = S.users.find(x => x.id === +p.id);
  if (!u) fail('账号不存在', 404);
  if (b.role && !['admin', 'operator', 'viewer'].includes(b.role)) fail('无效的角色');
  if (b.password && String(b.password).length < 6) fail('密码至少 6 位');
  if (b.role) u.role = b.role;
  if (typeof b.scope_all === 'boolean') u.scope_all = b.scope_all ? 1 : 0;
  if (Array.isArray(b.domain_ids)) u.domain_ids = b.domain_ids.map(Number);
  op('user.update', u.username, JSON.stringify({ role: u.role, scope_all: u.scope_all, domains: u.domain_ids }));
  save();
  return null;
});

route('DELETE', '/api/users/{id}', p => {
  const id = +p.id;
  const u = S.users.find(x => x.id === id);
  if (!u) fail('账号不存在', 404);
  if (id === (S.user?.id || 1)) fail('不能删除当前登录的账号');
  if (u.role === 'admin' && S.users.filter(x => x.role === 'admin').length <= 1) fail('至少保留一个管理员账号');
  S.users = S.users.filter(x => x.id !== id);
  op('user.delete', u.username, '');
  save();
  return null;
});

/* ---------- 审计日志（演示实现） ---------- */
route('GET', '/api/audit-logs', (p, q) => {
  const page = Math.max(1, +(q.page || 1));
  const size = Math.min(200, Math.max(10, +(q.size || 50)));
  let items = touch().opLogs.slice().sort((a, b) => b.id - a.id);
  if (q.username) items = items.filter(l => l.username === q.username);
  if (q.action) items = items.filter(l => String(l.action).startsWith(q.action));
  if (q.q) {
    const kw = String(q.q).toLowerCase();
    items = items.filter(l => String(l.target || '').toLowerCase().includes(kw) || String(l.detail || '').toLowerCase().includes(kw));
  }
  if (q.from) items = items.filter(l => String(l.created_at).slice(0, 10) >= q.from);
  if (q.to) items = items.filter(l => String(l.created_at).slice(0, 10) <= q.to);
  const actions = [...new Set(S.opLogs.map(l => l.action))].sort();
  return {
    items: items.slice((page - 1) * size, page * size),
    total: items.length, page, size, actions,
  };
});

route('POST', '/api/ai/chat', (p, q, b) => {
  const msg = String(b.message || '');
  if (!msg) fail('问题不能为空');
  const answer = /duplicate|重复|已存在/i.test(msg)
    ? '**结论**：云平台上已经存在一条「同主机 + 同线路 + 同类型」的记录，所以新写入被拒绝。\n\n**为什么会这样**：这个平台是"覆盖式"写入，服务端发现重复就直接报 `DomainRecordDuplicate`，而不是覆盖旧值。\n\n**怎么修**：\n1. 登录对应云平台控制台，找到该域名的解析列表\n2. 删掉那条旧的同主机记录\n3. 回到这里点「同步中心 → 立即重试」\n\n如果旧记录是历史遗留、本地列表里看不到，可以先做一次「一致性校验」，用「从云平台拉取」把它登记进本地再删除。'
    : /不生效|没生效|生效|缓存|ttl/i.test(msg)
      ? '**结论**：很可能还在 TTL 缓存期内，或者 NS 没有指向这家云平台。\n\n**三步确认**：\n1. 打开域名详情 → 点「生效检测」，会看到阿里 / 腾讯 / Google 三个公共 DNS 的实际查询结果\n2. 如果三个点都显示 `ok` 但本地看不到变化 → 是你的电脑或运营商缓存，等 TTL 到期（当前 600 秒）\n3. 如果显示 `nxdomain` → 这个域名的 NS 还没指向该平台，去域名注册商改 NS\n\n想立刻绕过缓存，可以临时把 TTL 改小（比如 60）再重新下发。'
      : '我看到你在看「' + (b.page || '控制台') + '」页面。\n\n**能帮你的方向**：\n1. 同步失败报错看不懂 —— 把错误原文贴给我\n2. 改了记录不生效 —— 我可以带你看生效检测结果\n3. 不知道选哪个平台线路/记录类型\n\n我只做只读查询，不会改你的任何配置。';
  return { answer };
});

/* ------------------------------- 分发 ------------------------------- */
async function handle(method, fullPath, body) {
  touch();
  const [path, qs] = String(fullPath).split('?');
  const query = {};
  new URLSearchParams(qs || '').forEach((v, k) => { query[k] = v; });
  const hit = routes.find(r => r.method === method && r.re.test(path));
  await new Promise(res => setTimeout(res, 70 + Math.random() * 160));
  if (!hit) fail(`演示模式未实现该接口：${method} ${path}`, 404);
  const m = path.match(hit.re);
  const params = {};
  hit.keys.forEach((k, i) => { params[k] = decodeURIComponent(m[i + 1]); });
  const data = hit.fn(params, query, body || {});
  if (!S.trendVals.length) S.trendVals = seed().trendVals;
  return data;
}

export const Mock = {
  handle,
  reset() {
    localStorage.removeItem(KEY);
    S = seed();
    save();
  },
};
