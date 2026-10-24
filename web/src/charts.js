/* 轻量 SVG 图表：无依赖，跟随主题色（ESM 版） */
const NS = 'http://www.w3.org/2000/svg';

function niceMax(v) {
  if (v <= 0) return 4;
  const steps = [4, 5, 8, 10, 16, 20, 25, 32, 40, 50, 80, 100, 160, 200, 320, 400, 800, 1000];
  for (const s of steps) if (v <= s) return s;
  return Math.ceil(v / 1000) * 1000;
}

function tooltip(text) {
  const t = document.createElementNS(NS, 'title');
  t.textContent = text;
  return t;
}

/** 24 小时同步趋势：成功 / 失败 堆叠柱 */
function stackedBars(host, data) {
  if (!host) return;
  host.innerHTML = '';
  if (!data || !data.length) {
    host.innerHTML = '<div class="empty">暂无同步数据</div>';
    return;
  }
  const W = 900, H = 214;
  const pad = { l: 40, r: 8, t: 12, b: 26 };
  const plotW = W - pad.l - pad.r;
  const plotH = H - pad.t - pad.b;
  const peak = Math.max(4, ...data.map(d => d.success + d.failed));
  const maxY = niceMax(peak);
  const slot = plotW / data.length;
  const bw = Math.max(3, Math.min(22, slot * 0.58));

  const svg = document.createElementNS(NS, 'svg');
  svg.setAttribute('class', 'chart');
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
  svg.setAttribute('preserveAspectRatio', 'none');

  const y = v => pad.t + plotH - (v / maxY) * plotH;

  for (let i = 0; i <= 4; i++) {
    const val = (maxY / 4) * i;
    const gy = y(val);
    const line = document.createElementNS(NS, 'line');
    line.setAttribute('class', 'grid-line');
    line.setAttribute('x1', pad.l);
    line.setAttribute('x2', W - pad.r);
    line.setAttribute('y1', gy);
    line.setAttribute('y2', gy);
    svg.appendChild(line);
    const t = document.createElementNS(NS, 'text');
    t.setAttribute('x', pad.l - 8);
    t.setAttribute('y', gy + 3.5);
    t.setAttribute('text-anchor', 'end');
    t.textContent = Math.round(val);
    svg.appendChild(t);
  }

  const labelEvery = Math.ceil(data.length / 8);
  data.forEach((d, i) => {
    const cx = pad.l + slot * i + slot / 2;
    const total = d.success + d.failed;
    if (total > 0) {
      const hOk = (d.success / maxY) * plotH;
      const hBad = (d.failed / maxY) * plotH;
      if (hOk > 0) {
        const r1 = document.createElementNS(NS, 'rect');
        r1.setAttribute('class', 'bar-ok');
        r1.setAttribute('x', cx - bw / 2);
        r1.setAttribute('y', y(d.success));
        r1.setAttribute('width', bw);
        r1.setAttribute('height', Math.max(1.5, hOk));
        r1.setAttribute('rx', Math.min(3, bw / 2));
        r1.appendChild(tooltip(`${d.hour} · 成功 ${d.success} / 失败 ${d.failed}`));
        svg.appendChild(r1);
      }
      if (hBad > 0) {
        const r2 = document.createElementNS(NS, 'rect');
        r2.setAttribute('class', 'bar-bad');
        r2.setAttribute('x', cx - bw / 2);
        r2.setAttribute('y', y(total));
        r2.setAttribute('width', bw);
        r2.setAttribute('height', Math.max(2, hBad));
        r2.setAttribute('rx', Math.min(3, bw / 2));
        r2.appendChild(tooltip(`${d.hour} · 失败 ${d.failed}`));
        svg.appendChild(r2);
      }
    } else {
      const base = document.createElementNS(NS, 'rect');
      base.setAttribute('x', cx - bw / 2);
      base.setAttribute('y', y(0) - 2);
      base.setAttribute('width', bw);
      base.setAttribute('height', 2);
      base.setAttribute('rx', 1);
      base.setAttribute('fill', 'var(--line-2)');
      base.setAttribute('opacity', '.7');
      svg.appendChild(base);
    }
    if (i % labelEvery === 0 || i === data.length - 1) {
      const t = document.createElementNS(NS, 'text');
      t.setAttribute('x', cx);
      t.setAttribute('y', H - 8);
      t.setAttribute('text-anchor', 'middle');
      t.textContent = String(d.hour || '').replace(':00', '');
      svg.appendChild(t);
    }
  });

  host.appendChild(svg);
}

/** 迷你走势线（KPI 卡片用） */
function spark(values, color = 'var(--brand)') {
  if (!values || values.length < 2) return '';
  const w = 92, h = 26, p = 2;
  const max = Math.max(...values, 1);
  const min = Math.min(...values, 0);
  const span = max - min || 1;
  const pts = values.map((v, i) => {
    const x = p + (i / (values.length - 1)) * (w - p * 2);
    const yy = h - p - ((v - min) / span) * (h - p * 2);
    return `${x.toFixed(1)},${yy.toFixed(1)}`;
  });
  return `<svg viewBox="0 0 ${w} ${h}" width="${w}" height="${h}" fill="none" style="overflow:visible">
    <polyline points="${pts.join(' ')}" stroke="${color}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" opacity=".9"/>
  </svg>`;
}

export const Charts = { stackedBars, spark };
export default Charts;
