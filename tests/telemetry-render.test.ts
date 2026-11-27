import { describe, expect, test } from 'bun:test';
import vm from 'node:vm';
import { readRuntimePart } from './helpers.ts';

type Call = Record<string, number | string | boolean>;
type View = Record<string, any>;

type Render = {
  tmFmtTok(n: number): string;
  tmFmtSec(s: number): string;
  tmFmtShortSec(s: number): string;
  tmFmtClock(s: number): string;
  tmFmtPct(p: number): string;
  tmFmtTps(t: number): string;
  renderPillMetrics(view: View, layout?: string): string;
  renderContextRing(ctx: Record<string, unknown>): string;
  contextRingLabel(ctx: Record<string, unknown>): string;
  renderDetailPanel(view: View): string;
  renderContextPanel(view: View): string;
  renderDockBody(view: View, state?: Record<string, unknown>): string;
  renderDockUnifiedChart(view: View, opts?: Record<string, unknown>): { html: string; meta: Record<string, number> };
  renderDockTurnAxis(view: View, meta?: Record<string, number>): string;
  renderDockStepHover(view: View, idx: number, isPinned?: boolean): string;
  renderDockTurnHover(view: View, turns: unknown[], turnIdx: number): string;
  renderDockRingHover(view: View, scope: string, focusLayer?: string): string;
  renderDockContextHover(view: View, tipKey: string): string;
  renderTurnBadge(t: Record<string, unknown>): { html: string; tooltipHtml: string };
};

function loadRender(): Render {
  const ctx: { __exports?: Render } = {};
  vm.createContext(ctx);
  vm.runInContext(
    readRuntimePart('40-status-machine.js') +
      '\n' +
      readRuntimePart('45-telemetry-render.js') +
      '\nthis.__exports = { tmFmtTok, tmFmtSec, tmFmtShortSec, tmFmtClock, tmFmtPct, tmFmtTps, renderPillMetrics, renderContextRing, contextRingLabel, renderDetailPanel, renderContextPanel, renderDockBody, renderDockUnifiedChart, renderDockTurnAxis, renderDockStepHover, renderDockTurnHover, renderDockRingHover, renderDockContextHover, renderTurnBadge };',
    ctx
  );
  return ctx.__exports!;
}

const R = loadRender();

function call(n: number, turn: number, ctx: number, extra: Call = {}): Call {
  const cr = Math.round(ctx * 0.9);
  return {
    n,
    turn,
    model: 'Gemini 3.1 Pro (High)',
    ttft: 1.2,
    gen: 6.5,
    dur: 7.7,
    tps: 85.3,
    in: ctx - cr,
    cw: 0,
    cr,
    ctx,
    out: 600,
    think: 240,
    resp: 360,
    hit: Math.round((cr / ctx) * 1000) / 10,
    ...extra
  };
}

function fixture(): View {
  const history = [
    call(1, 1, 20000),
    call(2, 1, 24000),
    call(3, 2, 30000, { ttft: 30, dur: 36.5 }),
    call(4, 2, 12000, { cp: true }),
    call(5, 3, 16000),
    call(6, 3, 20000, { ttft: 1.0, tps: 92.4, cr: 19200, in: 800, hit: 96 })
  ];
  return {
    cacheMode: 'step',
    last: history[history.length - 1],
    ctx: {
      used: 20000,
      limit: 200000,
      limitSrc: 'model',
      pct: 10,
      level: 'ok',
      remain: 180000,
      cached: 19200,
      fresh: 800,
      compacted: false,
      summaryKb: 0,
      compacting: false,
      compactions: 1,
      peak: 30000,
      growth: 4000,
      growthN: 3,
      callsLeft: 45
    },
    turn: {
      index: 3,
      calls: 2,
      steps: 5,
      startedMs: 1_700_000_000_000,
      in: 1600,
      cr: 34400,
      out: 1200,
      think: 480,
      resp: 720,
      streamDur: 13.0,
      dur: 15.4,
      hit: 95.6
    },
    session: {
      turns: 3,
      calls: 6,
      errors: 1,
      in: 12400,
      cw: 0,
      cr: 109600,
      prompt: 122000,
      out: 3600,
      think: 1440,
      resp: 2160,
      streamDur: 39.0,
      dur: 74.5,
      hit: 89.8,
      ttftMed: 1.2,
      tpsMed: 85.3
    },
    history
  };
}

const textOf = (html: string) => html.replace(/<[^>]+>/g, ' ').replace(/\s+/g, ' ').trim();

describe('formatters', () => {
  test('Chinese token units, durations, clock, percentages and throughput', () => {
    expect(R.tmFmtTok(0)).toBe('0');
    expect(R.tmFmtTok(980)).toBe('980');
    expect(R.tmFmtTok(1500)).toBe('1.5千');
    expect(R.tmFmtTok(16600)).toBe('1.7万');
    expect(R.tmFmtTok(230400)).toBe('23万');
    expect(R.tmFmtTok(999_700)).toBe('1百万');
    expect(R.tmFmtTok(1_250_000)).toBe('1.25百万');
    expect(R.tmFmtTok(125_000_000)).toBe('1.25亿');
    expect(R.tmFmtSec(0.84)).toBe('0.8s');
    expect(R.tmFmtSec(45.2)).toBe('45s');
    expect(R.tmFmtSec(124)).toBe('2分04秒');
    expect(R.tmFmtSec(120)).toBe('2分钟');
    expect(R.tmFmtSec(3900)).toBe('1小时05分');
    expect(R.tmFmtShortSec(124)).toBe('2m04s');
    expect(R.tmFmtClock(726)).toBe('12:06');
    expect(R.tmFmtPct(0)).toBe('0%');
    expect(R.tmFmtPct(4.5)).toBe('4.5%');
    expect(R.tmFmtPct(96.4)).toBe('96%');
    expect(R.tmFmtPct(99.7)).toBe('99%');
    expect(R.tmFmtPct(100)).toBe('100%');
    expect(R.tmFmtTps(85.3)).toBe('85');
    expect(R.tmFmtTps(4.25)).toBe('4.3');
  });
});

describe('inline bar', () => {
  test('shows 首字 · 吞吐 · 缓存读 of the latest call', () => {
    const v = fixture();
    const text = textOf(R.renderPillMetrics(v));
    expect(text).toBe('首字 1.0s 吞吐 92/s 缓存读 96%');
  });

  test('session cache mode switches the label and the figure', () => {
    const v = fixture();
    v.cacheMode = 'session';
    expect(textOf(R.renderPillMetrics(v))).toContain('累计缓存读 90%');
  });

  test('detailed layout adds the output size; slow first token is flagged', () => {
    const v = fixture();
    expect(textOf(R.renderPillMetrics(v, 'detailed'))).toContain('输出 600');
    v.last = { ...v.last, ttft: 30 };
    expect(R.renderPillMetrics(v)).toContain('ag-lm-item ag-lm-ttft is-slow');
  });

  test('context ring arc follows usage and level', () => {
    const half = R.renderContextRing({ pct: 50, level: 'ok' });
    const circumference = 2 * Math.PI * 6;
    const m = half.match(/stroke-dasharray="([\d.]+) ([\d.]+)"/)!;
    expect(Number(m[1])).toBeCloseTo(circumference / 2, 1);
    expect(Number(m[2])).toBeCloseTo(circumference, 1);
    expect(R.renderContextRing({ pct: 92, level: 'crit' })).toContain('data-level="crit"');
    expect(R.renderContextRing({ pct: 0, level: 'ok' })).toContain('stroke-dasharray="0.00 ');
    expect(R.renderContextRing({ pct: 140, level: 'crit' })).toContain('stroke-dasharray="' + circumference.toFixed(2));
    expect(R.renderContextRing({ pct: 30, compacting: true })).toContain('data-state="compacting"');
    expect(R.contextRingLabel({ pct: 10, used: 20000, limit: 200000 })).toBe('上下文 10%（2万 / 20万）');
  });
});

describe('compact popovers', () => {
  test('call detail popover is compact with live slots and dock link', () => {
    const html = R.renderDetailPanel(fixture());
    const text = textOf(html);
    expect(text).toContain('首字');
    expect(text).toContain('吞吐');
    expect(text).toContain('缓存读');
    expect(text).toContain('中位 1.2s');
    expect(text).toContain('端到端');
    expect(text).toContain('详情 ↗');
    expect(html).toContain('class="ag-tt-live-chip"');
    expect(html).toContain('class="ag-tt-wait-note"');
    expect(html).toContain('data-ag-open-dock="timeline"');
  });

  test('context popover shows hero, bar, outlook and dock link', () => {
    const html = R.renderContextPanel(fixture());
    const text = textOf(html);
    expect(html).toContain('<div class="ag-cp-big">10<u>%</u></div>');
    expect(text).toContain('2 万 / 20 万');
    expect(text).toContain('+4千/步 · 约 45 步');
    expect(html).toContain('data-ag-open-dock="ctx"');
  });

  test('compacted and compacting context states', () => {
    const v = fixture();
    v.ctx = { ...v.ctx, compacted: true, level: 'compacted', summaryKb: 12 };
    const html = R.renderContextPanel(v);
    expect(html).toContain('data-level="compacted"');
    expect(textOf(html)).toContain('已压缩');
    expect(textOf(html)).toContain('摘要 12 KB');
    v.ctx = { ...v.ctx, compacted: false, compacting: true };
    expect(textOf(R.renderContextPanel(v))).toContain('正在压缩上下文');
    expect(R.contextRingLabel(v.ctx)).toBe('上下文：正在压缩');
  });

  test('escapes model names and survives empty views', () => {
    expect(() => R.renderDetailPanel({})).not.toThrow();
    expect(() => R.renderContextPanel({})).not.toThrow();
    const v = fixture();
    v.last = { ...v.last, model: '<b>x</b>' };
    const html = R.renderDetailPanel(v);
    expect(html).toContain('&lt;b&gt;x&lt;/b&gt;');
    expect(html).not.toContain('<b>x</b>');
  });
});

describe('right inspector dock renderers', () => {
  test('renders context, timeline container and unified alluvial token flow', () => {
    const v = fixture();
    const html = R.renderDockBody(v, { barMode: 'dur', pinnedIdx: -1 });
    const text = textOf(html);
    expect(text).toContain('上下文');
    expect(text).not.toContain('上限');
    expect(text).toContain('峰值 3万 · 压 1');
    expect(text).toContain('步进时间线');
    expect(text).toContain('累计分布');
    expect(text).not.toContain('外环输入');
    expect(text).toContain('本轮');
    expect(text).toContain('整场');
    expect(text).toContain('缓存读');
    expect(text).toContain('读');
    expect(text).toContain('思考');
    expect(text).toContain('输出');
    expect(text).toContain('1败');
    expect(html).toContain('id="ag-dock-chart-box"');
    expect(html).toContain('data-ag-flow-scope="session"');
    expect(html).toContain('class="ag-dk-fsvg"');
    expect((html.match(/class="ag-dk-fband"/g) || []).length).toBe(4);

    const turnHtml = R.renderDockBody(v, { barMode: 'dur', distScope: 'turn', pinnedIdx: -1 });
    expect(turnHtml).toContain('data-ag-flow-scope="turn"');
    expect(textOf(turnHtml)).not.toContain('1败');

    const noThink = fixture();
    noThink.session = { ...noThink.session, think: 0 };
    const noThinkHtml = R.renderDockBody(noThink, { barMode: 'dur', distScope: 'session', pinnedIdx: -1 });
    expect((noThinkHtml.match(/class="ag-dk-fband"/g) || []).length).toBe(3);
    expect(noThinkHtml).toContain('class="ag-dk-frow is-zero" data-ag-fkey="think"');

    const flat = fixture();
    flat.ctx = { ...flat.ctx, used: 20000, peak: 20000, compactions: 0 };
    expect(textOf(R.renderDockBody(flat, { barMode: 'dur', pinnedIdx: -1 }))).not.toContain('峰值');
  });

  test('unified chart scales to exact pixel dimensions, uses full bar height and marks compactions', () => {
    const v = fixture();
    const { html, meta } = R.renderDockUnifiedChart(v, { width: 300, height: 140, barMode: 'dur', hoverIdx: 2, pinnedIdx: -1 });
    expect(meta.W).toBe(300);
    expect(meta.H).toBe(140);
    expect(html).toContain('viewBox="0 0 300 140"');
    expect((html.match(/class="ag-u-bar[ "]/g) || []).length).toBe(6);
    expect((html.match(/class="ag-u-cp"/g) || []).length).toBe(1);
    expect(html).toContain('is-active');

    v.history = [call(1, 1, 20000, { cr: 20000, in: 0, hit: 100 })];
    const hitChart = R.renderDockUnifiedChart(v, { width: 300, height: 140, barMode: 'hit', hoverIdx: -1, pinnedIdx: -1 });
    const barMatch = hitChart.html.match(/class="ag-f-cache"[^>]*y="([\d.]+)"[^>]*height="([\d.]+)"/)!;
    expect(Number(barMatch[1])).toBeCloseTo(hitChart.meta.top + 1, 0);
    expect(Number(barMatch[2])).toBeCloseTo(140 - hitChart.meta.top - 2, 0);
  });

  test('few steps keep bars at their maximum width, aligned to the latest edge', () => {
    const v = fixture();
    v.history = [call(1, 1, 20000)];
    const { html, meta } = R.renderDockUnifiedChart(v, { width: 300, height: 140, barMode: 'hit', hoverIdx: -1, pinnedIdx: -1 });
    expect(meta.slot).toBe(26);
    expect(meta.x0).toBe(274);
    const w = Number(html.match(/class="ag-f-cache"[^>]*width="([\d.]+)"/)![1]);
    expect(w).toBeLessThanOrEqual(26);
    expect(w).toBeGreaterThan(20);
  });

  test('many steps share the width once they no longer fit at the maximum', () => {
    const v = fixture();
    v.history = Array.from({ length: 20 }, (_, i) => call(i + 1, 1, 20000));
    const { meta } = R.renderDockUnifiedChart(v, { width: 300, height: 140, barMode: 'hit', hoverIdx: -1, pinnedIdx: -1 });
    expect(meta.slot).toBe(15);
    expect(meta.x0).toBe(0);
  });

  test('a full cache miss names its likely cause in the step hover', () => {
    const v = fixture();
    v.history = [
      call(1, 1, 20000),
      call(2, 1, 24000, { cr: 0, in: 24000, hit: 0 }),
      call(3, 2, 26000, { cr: 0, in: 26000, hit: 0, model: 'Gemini 3.1 Flash' })
    ];
    expect(textOf(R.renderDockStepHover(v, 0, false))).not.toContain('未命中');
    expect(textOf(R.renderDockStepHover(v, 1, false))).toContain('服务端没接上缓存');
    expect(textOf(R.renderDockStepHover(v, 2, false))).toContain('换了模型');
    v.history = [call(23, 5, 26000, { cr: 0, in: 26000, hit: 0 })];
    const trimmed = textOf(R.renderDockStepHover(v, 0, false));
    expect(trimmed).not.toContain('首次调用');
    expect(trimmed).toContain('服务端没接上缓存');
  });

  test('turn axis labels shrink from 第N轮 to N, then skip, while every turn stays hoverable', () => {
    const v = fixture();
    const axisFor = (steps: number, perTurn: number, width: number) => {
      v.history = Array.from({ length: steps }, (_, i) => call(i + 1, Math.floor(i / perTurn) + 1, 20000 + i * 5000));
      v.turn = { ...v.turn, index: Math.ceil(steps / perTurn) };
      const { meta } = R.renderDockUnifiedChart(v, { width, height: 140, barMode: 'hit', hoverIdx: -1, pinnedIdx: -1 });
      const html = R.renderDockTurnAxis(v, meta);
      const labels = [...html.matchAll(/class="ag-dk-tlbl"[^>]*>([^<]+)</g)].map((m) => m[1]);
      return { segs: (html.match(/class="ag-dk-tseg/g) || []).length, labels };
    };

    const roomy = axisFor(24, 6, 300);
    expect(roomy.segs).toBe(4);
    expect(roomy.labels).toEqual(['第1轮', '第2轮', '第3轮', '第4轮']);

    const narrow = axisFor(26, 2, 300);
    expect(narrow.segs).toBe(13);
    expect(narrow.labels.length).toBe(13);
    expect(narrow.labels[12]).toBe('13');

    const dense = axisFor(60, 1, 300);
    expect(dense.segs).toBe(60);
    expect(dense.labels.length).toBeLessThan(60);
    expect(dense.labels).toContain('60');
  });

  test('hover cards format step, turn, ring and context breakdowns in Chinese', () => {
    const v = fixture();
    const stepText = textOf(R.renderDockStepHover(v, 2, false));
    expect(stepText).toContain('#3 · 第2轮');
    expect(stepText).toContain('模型');
    expect(stepText).toContain('Gemini 3.1 Pro (High)');
    expect(stepText).toContain('缓存读');
    expect(stepText).toContain('思考 / 输出');

    const turnText = textOf(R.renderDockTurnHover(v, [], 2));
    expect(turnText).toContain('第 2 轮');
    expect(turnText).toContain('2 次调用');
    expect(turnText).not.toContain('时间');
  });

  test('step and turn hovers show when they happened', () => {
    const v = fixture();
    const now = Date.now();
    v.history[2].at = now - 600_000;
    v.history[3].at = now - 120_000;
    const step = textOf(R.renderDockStepHover(v, 2, false));
    expect(step).toMatch(/时间\s*(\d+月\d+日 )?\d{2}:\d{2}(:\d{2})?/);

    const turns = [{ turnIndex: 2, startedMs: now - 660_000, steps: 2, model: 'Gemini 3.1 Pro (High)' }];
    const turn = textOf(R.renderDockTurnHover(v, turns, 2));
    expect(turn).toMatch(/时间\s*(\d+月\d+日 )?\d{2}:\d{2}(:\d{2})? – (\d+月\d+日 )?\d{2}:\d{2}(:\d{2})?/);

    v.history[2].at = 1_700_000_060_000;
    expect(textOf(R.renderDockStepHover(v, 2, false))).toMatch(/时间\s*2023年11月1[45]日 \d{2}:\d{2}/);

    v.history[3].at = 1_700_000_300_000;
    const oldTurn = [{ turnIndex: 2, startedMs: 1_700_000_000_000, steps: 2, model: 'Gemini 3.1 Pro (High)' }];
    expect(textOf(R.renderDockTurnHover(v, oldTurn, 2))).toMatch(/时间\s*2023年11月1[45]日 \d{2}:\d{2} – \d{2}:\d{2}(?!:)/);
  });

  test('ring and context hovers format their breakdowns', () => {
    const v = fixture();

    const ringText = textOf(R.renderDockRingHover(v, 'session', 'outer'));
    expect(ringText).toContain('整场会话');
    expect(ringText).toContain('输入合计');
    expect(ringText).toContain('输出合计');
    expect(ringText).toContain('缓存读');
    expect(ringText).toContain('1 次 / 1 次');

    const thinkFocusHtml = R.renderDockRingHover(v, 'session', 'think');
    expect(thinkFocusHtml).toContain('is-dim');

    const ctxText = textOf(R.renderDockContextHover(v, 'ctx-bar'));
    expect(ctxText).toContain('缓存读');
    expect(ctxText).toContain('剩余可用');
  });

  test('copy stays Chinese apart from the model name', () => {
    const v = fixture();
    const combined =
      R.renderDetailPanel(v) +
      R.renderContextPanel(v) +
      R.renderDockBody(v, { barMode: 'dur', pinnedIdx: 1 }) +
      R.renderDockStepHover(v, 1, true) +
      R.renderDockRingHover(v, 'session', '');
    const text = textOf(combined).replaceAll(String(v.last.model), '');
    expect(text.match(/[A-Za-z]{2,}/g)).toBeNull();
  });
});

test('per-turn badge uses Chinese labels and units', () => {
  const { html, tooltipHtml } = R.renderTurnBadge({
    model: 'Claude <Opus>',
    steps: 3,
    lastCtx: 150000,
    ctxPct: 75,
    limitStr: '200k',
    contextLimit: 200000,
    inTok: 2000,
    cacheWrite: 0,
    cacheRead: 98000,
    outTok: 1200,
    thinkTok: 500,
    respTok: 700,
    streamDur: 14.2,
    totDur: 18.9,
    tps: 84.5
  });
  expect(textOf(html)).toBe('Claude &lt;Opus&gt; · 输入 10万 · 输出 1.2千 · 缓存读 98% · 吞吐 85/s');
  const tt = textOf(tooltipHtml);
  expect(tt).toContain('3 次调用');
  expect(tt).toContain('结束时上下文 15万 / 20万 （75%）');
  expect(tt).toContain('思考 500 · 输出 700');
  expect(tooltipHtml).toContain('ag-tmt-ctx-warn');
  expect(tt).not.toContain('tok/s');
});
