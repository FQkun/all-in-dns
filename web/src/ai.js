/* ==========================================================================
   AI 诊断助手：右下悬浮球 + 抽屉对话（只读查询，后端 /api/ai/chat）
   附：麦克风语音输入（浏览器 Web Speech API，纯前端，不改后端）
   ========================================================================== */
import { $, esc, icon, toast } from './core.js';
import { Api } from './api.js';
import { currentRoute } from './router-state.js';

let els = null;
let busy = false;
const history = [];

/* 语音识别（Chrome / Edge 支持；Firefox、Safari 15 以下不支持） */
const SR = window.SpeechRecognition || window.webkitSpeechRecognition || null;
let recog = null;
let listening = false;
let committed = '';
const EXPAND_KEY = 'dns_ai_expanded';

/** 极简 Markdown 渲染：标题 / 粗体 / 行内代码 / 代码块 / 列表 */
function md(text) {
  const lines = String(text || '').split('\n');
  const out = [];
  let inCode = false;
  let inList = false;
  const inline = s => esc(s)
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/`([^`]+)`/g, '<code>$1</code>');

  for (const raw of lines) {
    const line = raw.trimEnd();
    if (/^```/.test(line)) {
      if (inList) { out.push('</ul>'); inList = false; }
      out.push(inCode ? '</code></pre>' : '<pre><code>');
      inCode = !inCode;
      continue;
    }
    if (inCode) { out.push(esc(raw)); continue; }
    const li = line.match(/^\s*(?:\d+\.|[-*])\s+(.*)$/);
    if (li) {
      if (!inList) { out.push('<ul>'); inList = true; }
      out.push('<li>' + inline(li[1]) + '</li>');
      continue;
    }
    if (inList) { out.push('</ul>'); inList = false; }
    if (!line.trim()) continue;
    const h = line.match(/^(#{1,4})\s+(.*)$/);
    if (h) { out.push(`<p><strong>${inline(h[2])}</strong></p>`); continue; }
    out.push('<p>' + inline(line) + '</p>');
  }
  if (inList) out.push('</ul>');
  if (inCode) out.push('</code></pre>');
  return out.join('');
}

export function mountAssistant() {
  const root = $('#ai-root');
  if (!root || els) return;
  root.innerHTML = `
    <div class="ai-drawer" id="ai-drawer">
      <div class="ai-head">
        ${icon('spark')}
        <h3>AI 诊断助手</h3>
        <span class="chip ok"><i class="dot"></i>只读</span>
        <button class="btn ghost icon sm" id="ai-expand" title="放大窗口">${icon('expand')}</button>
        <button class="btn ghost icon sm" id="ai-close">✕</button>
      </div>
      <div class="ai-body" id="ai-body"></div>
      <div class="ai-voice-bar" id="ai-voice-bar"></div>
      <div class="ai-foot">
        <button class="btn ghost icon" id="ai-mic" title="语音输入">${icon(SR ? 'mic' : 'mic')}</button>
        <textarea class="input" id="ai-input" rows="1" placeholder="例如：为什么这条记录同步失败？"></textarea>
        <button class="btn primary icon" id="ai-send" title="发送">${icon('send')}</button>
      </div>
    </div>
    <button class="ai-fab" id="ai-fab" title="AI 诊断助手">${icon('spark')}</button>`;

  els = {
    drawer: $('#ai-drawer'),
    body: $('#ai-body'),
    input: $('#ai-input'),
    send: $('#ai-send'),
    mic: $('#ai-mic'),
    voiceBar: $('#ai-voice-bar'),
  };

  els.body.innerHTML = `<div class="ai-msg bot">${md('你好，我是 DNS 诊断助手。\n\n报错看不懂、改了不生效、不知道该选哪个平台——直接问我。\n\n不方便打字？点输入框左边的 **麦克风** 说话就行。')}</div>`;

  $('#ai-fab').onclick = () => window.__aiToggle();
  $('#ai-close').onclick = () => toggle(false);
  $('#ai-expand').onclick = () => setExpanded(!els.drawer.classList.contains('expanded'));
  if (localStorage.getItem(EXPAND_KEY) === '1') setExpanded(true);
  els.send.onclick = send;
  els.input.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); }
  });

  setupVoice();

  window.__aiToggle = () => toggle(!els.drawer.classList.contains('show'));
  // 便于直接预览：?ai=1 打开抽屉
  if (new URLSearchParams(location.search).has('ai')) toggle(true);
}

/* ------------------------------- 语音输入 ------------------------------- */
function setupVoice() {
  if (!SR) {
    els.mic.title = '当前浏览器不支持语音输入，建议用 Edge / Chrome';
    els.mic.onclick = () => toast('当前浏览器不支持语音输入，请换 Edge 或 Chrome', 'err');
    return;
  }
  if (!window.isSecureContext) {
    els.mic.title = '语音输入需要 HTTPS 或 localhost 环境';
    els.mic.onclick = () => toast('语音输入需要在 HTTPS 或 localhost 下使用（浏览器安全限制）', 'err');
    return;
  }

  recog = new SR();
  recog.lang = 'zh-CN';
  recog.continuous = true;
  recog.interimResults = true;
  recog.maxAlternatives = 1;

  recog.onstart = () => setListening(true);

  recog.onresult = e => {
    let finalText = '';
    let interimText = '';
    for (let i = e.resultIndex; i < e.results.length; i++) {
      const r = e.results[i];
      const t = r[0]?.transcript || '';
      if (r.isFinal) finalText += t;
      else interimText += t;
    }
    if (finalText) committed += finalText;
    const merged = (committed + interimText).replace(/\s+$/g, '');
    els.input.value = merged;
    els.input.scrollTop = els.input.scrollHeight;
  };

  recog.onerror = e => {
    const map = {
      'not-allowed': '麦克风权限被拒绝，请点地址栏左侧图标允许麦克风后再试',
      'service-not-allowed': '浏览器禁止了语音识别服务，请在设置里开启',
      'audio-capture': '没有检测到麦克风设备',
      'no-speech': '没听到声音，靠近麦克风再说一次',
      network: '语音识别服务连不上网（Chrome 走在线识别，国内建议用 Edge）',
      aborted: '',
    };
    const msg = map[e.error] ?? `语音识别出错：${e.error}`;
    if (msg) toast(msg, 'err');
    setListening(false);
  };

  recog.onend = () => {
    setListening(false);
    if (els.input.value.trim()) els.input.focus();
  };

  els.mic.onclick = () => {
    if (listening) { stopVoice(); return; }
    startVoice();
  };
}

function startVoice() {
  if (!recog) return;
  committed = els.input.value.replace(/\s+$/g, '');
  if (committed && !/\s$/.test(committed)) committed += ' ';
  try {
    recog.start();
    setListening(true);
  } catch (e) {
    // InvalidStateError：已经在识别中
    if (String(e?.name) !== 'InvalidStateError') toast('无法启动语音输入：' + e.message, 'err');
  }
}

function stopVoice() {
  try { recog?.stop(); } catch (e) { /* ignore */ }
  setListening(false);
}

function setListening(on) {
  listening = on;
  if (!els) return;
  els.mic.innerHTML = icon(on ? 'stop' : 'mic');
  els.mic.classList.toggle('recording', on);
  els.mic.title = on ? '停止语音输入' : '语音输入';
  els.input.placeholder = on ? '正在聆听…说完点麦克风停止，可再编辑后发送' : '例如：为什么这条记录同步失败？';
  els.voiceBar.classList.toggle('show', on);
  els.voiceBar.innerHTML = on ? '<span class="live-dot"></span>正在聆听，请说话…' : '';
}

function setExpanded(on) {
  if (!els) return;
  els.drawer.classList.toggle('expanded', on);
  const b = $('#ai-expand');
  if (b) {
    b.innerHTML = icon(on ? 'shrink' : 'expand');
    b.title = on ? '还原窗口' : '放大窗口';
  }
  try { localStorage.setItem(EXPAND_KEY, on ? '1' : '0'); } catch (e) { /* ignore */ }
}

/* --------------------------------- 对话 --------------------------------- */
function toggle(show) {
  if (!els) return;
  els.drawer.classList.toggle('show', show);
  if (show) els.input.focus();
  if (!show && listening) stopVoice();
}

async function send() {
  if (busy || !els) return;
  if (listening) stopVoice();
  const text = els.input.value.trim();
  if (!text) return;
  els.input.value = '';
  committed = '';
  history.push(text);
  push('user', esc(text));
  busy = true;
  els.send.disabled = true;
  const typing = document.createElement('div');
  typing.className = 'ai-msg bot ai-typing';
  typing.innerHTML = '<span></span><span></span><span></span>';
  els.body.appendChild(typing);
  els.body.scrollTop = els.body.scrollHeight;

  try {
    const r = await Api.post('/api/ai/chat', {
      message: text,
      domain: '',
      page: currentRoute.view,
    });
    typing.remove();
    push('bot', md(r.answer || '（没有返回内容）'));
  } catch (e) {
    typing.remove();
    push('bot', md('调用失败：' + e.message + '\n\n如果后端未配置 `DNS_AI_API_KEY`，AI 助手会不可用，其他功能不受影响。'));
  } finally {
    busy = false;
    els.send.disabled = false;
    els.input.focus();
  }
}

function push(who, html) {
  const el = document.createElement('div');
  el.className = 'ai-msg ' + who;
  el.innerHTML = html;
  els.body.appendChild(el);
  els.body.scrollTop = els.body.scrollHeight;
}
