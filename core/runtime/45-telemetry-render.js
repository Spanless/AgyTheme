  const TM_RING_R = 6;
  const TM_RING_C = 2 * Math.PI * TM_RING_R;
  const DOCK_MAX_SLOT = 26;

  function tmNum(v) {
    return typeof v === 'number' && isFinite(v) ? v : 0;
  }

  function tmEsc(s) {
    if (typeof escapeStatusHtml === 'function') return escapeStatusHtml(s);
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function tmTrim(fixed) {
    return fixed.indexOf('.') >= 0 ? fixed.replace(/\.?0+$/, '') : fixed;
  }

  function tmFmtInt(n) {
    return Math.round(tmNum(n)).toLocaleString('en-US');
  }

  function tmFmtTok(n) {
    const v = tmNum(n);
    if (v <= 0) return '0';
    if (v < 1000) return String(Math.round(v));
    if (v < 9950) return tmTrim((v / 1000).toFixed(1)) + '千';
    if (v < 999500) {
      const w = v / 10000;
      return tmTrim(w >= 100 ? String(Math.round(w)) : w.toFixed(1)) + '万';
    }
    if (v < 99950000) {
      const m = v / 1e6;
      return tmTrim(m >= 10 ? m.toFixed(1) : m.toFixed(2)) + '百万';
    }
    const y = v / 1e8;
    return tmTrim(y >= 10 ? y.toFixed(1) : y.toFixed(2)) + '亿';
  }

  function tmFmtSec(s) {
    const v = Math.max(0, tmNum(s));
    if (v < 10) return v.toFixed(1) + 's';
    if (v < 59.5) return Math.round(v) + 's';
    const total = Math.round(v);
    if (total < 3600) {
      const m = Math.floor(total / 60);
      const r = total % 60;
      return m + '分' + (r ? String(r).padStart(2, '0') + '秒' : '钟');
    }
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    return h + '小时' + (m ? String(m).padStart(2, '0') + '分' : '');
  }

  function tmFmtShortSec(s) {
    const v = Math.max(0, tmNum(s));
    if (v < 10) return v.toFixed(1) + 's';
    if (v < 59.5) return Math.round(v) + 's';
    const total = Math.round(v);
    if (total < 3600) {
      const m = Math.floor(total / 60);
      const r = total % 60;
      return m + 'm' + (r ? String(r).padStart(2, '0') + 's' : '');
    }
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    return h + 'h' + (m ? String(m).padStart(2, '0') + 'm' : '');
  }

  function tmFmtClock(s) {
    const total = Math.max(0, Math.floor(tmNum(s)));
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const r = total % 60;
    const mm = String(m).padStart(2, '0');
    const ss = String(r).padStart(2, '0');
    return h > 0 ? h + ':' + mm + ':' + ss : mm + ':' + ss;
  }

  function tmFmtTime(ms, sameDayAs) {
    const t = tmNum(ms);
    if (t <= 0) return '';
    const d = new Date(t);
    const today = new Date().toDateString();
    const ref = tmNum(sameDayAs) > 0 ? new Date(sameDayAs).toDateString() : today;
    const pad = (x) => String(x).padStart(2, '0');
    const hm = pad(d.getHours()) + ':' + pad(d.getMinutes());
    if (d.toDateString() === ref) return ref === today ? hm + ':' + pad(d.getSeconds()) : hm;
    const md = d.getMonth() + 1 + '月' + d.getDate() + '日 ' + hm;
    return d.getFullYear() === new Date().getFullYear() ? md : d.getFullYear() + '年' + md;
  }

  function tmFmtPct(p) {
    const v = tmNum(p);
    if (v <= 0) return '0%';
    if (v < 10) return tmTrim(v.toFixed(1)) + '%';
    const r = Math.round(v);
    return (r >= 100 && v < 100 ? 99 : r) + '%';
  }

  function tmFmtTps(t) {
    const v = tmNum(t);
    if (v <= 0) return '0';
    return v < 10 ? tmTrim(v.toFixed(1)) : String(Math.round(v));
  }

  function tmUnitHtml(str) {
    return tmEsc(str).replace(/([^\d.,\s]+)/g, '<u>$1</u>');
  }

  function tmClamp(v, lo, hi) {
    return Math.max(lo, Math.min(hi, v));
  }

  function tmLevel(ctx) {
    const lv = ctx && ctx.level;
    return lv === 'warn' || lv === 'crit' || lv === 'compacted' ? lv : 'ok';
  }

  function tmLimitSrcText(src) {
    if (src === 'manual') return '手动设置';
    if (src === 'model') return '模型声明';
    return '默认值';
  }

  function tmIsSlowTtft(ttft, median) {
    const v = tmNum(ttft);
    const m = tmNum(median);
    return v >= 10 && m > 0 && v >= m * 3;
  }

  function tmCallE2eTps(c) {
    const dur = tmNum(c && c.dur);
    const out = tmNum(c && c.out);
    return dur > 0 && out > 0 ? out / dur : 0;
  }

  function tmCallFresh(c) {
    return Math.max(0, tmNum(c && c.in) + tmNum(c && c.cw));
  }

  function renderContextRing(ctx) {
    const c = ctx || {};
    const pct = tmClamp(tmNum(c.pct), 0, 100);
    const arc = pct > 0 ? Math.max((TM_RING_C * pct) / 100, 1.4) : 0;
    const state = c.compacting ? 'compacting' : '';
    return (
      '<svg class="ag-lm-ring" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" data-level="' +
      tmLevel(c) +
      '"' +
      (state ? ' data-state="' + state + '"' : '') +
      '><circle class="ag-lm-ring-trk" cx="8" cy="8" r="' +
      TM_RING_R +
      '"/><circle class="ag-lm-ring-arc" cx="8" cy="8" r="' +
      TM_RING_R +
      '" stroke-dasharray="' +
      arc.toFixed(2) +
      ' ' +
      TM_RING_C.toFixed(2) +
      '" transform="rotate(-90 8 8)"/></svg>'
    );
  }

  function tmPillItem(key, value, cls, shortKey) {
    return (
      '<span class="ag-lm-item' +
      (cls ? ' ' + cls : '') +
      '"><span class="ag-lm-k"' +
      (shortKey ? ' data-short="' + tmEsc(shortKey) + '"' : '') +
      '>' +
      key +
      '</span><span class="ag-lm-v">' +
      tmEsc(value) +
      '</span></span>'
    );
  }

  function renderPillMetrics(view, layout) {
    const v = view || {};
    const last = v.last || {};
    const sess = v.session || {};
    const sessionHit = v.cacheMode === 'session';
    const items = [
      tmPillItem('首字', tmFmtSec(last.ttft), 'ag-lm-ttft' + (tmIsSlowTtft(last.ttft, sess.ttftMed) ? ' is-slow' : '')),
      tmPillItem('吞吐', tmFmtTps(last.tps) + '/s', ''),
      tmPillItem(sessionHit ? '累计缓存读' : '缓存读', tmFmtPct(sessionHit ? sess.hit : last.hit), 'ag-lm-cache', '缓存')
    ];
    if (layout === 'detailed') items.push(tmPillItem('输出', tmFmtTok(last.out), 'ag-lm-opt'));
    return items.join('<span class="ag-lm-sep" aria-hidden="true"></span>');
  }

  function contextRingLabel(ctx) {
    const c = ctx || {};
    if (c.compacting) return '上下文：正在压缩';
    if (c.compacted) return '上下文：已压缩，下一次调用后刷新';
    return '上下文 ' + tmFmtPct(c.pct) + '（' + tmFmtTok(c.used) + ' / ' + tmFmtTok(c.limit) + '）';
  }

  function tmStackBar(parts, total, extraCls) {
    const sum = parts.reduce((a, p) => a + Math.max(0, tmNum(p[1])), 0);
    const denom = Math.max(sum, tmNum(total));
    let html = '<div class="ag-mp-bar' + (extraCls ? ' ' + extraCls : '') + '">';
    if (denom > 0) {
      for (let i = 0; i < parts.length; i++) {
        const val = Math.max(0, tmNum(parts[i][1]));
        if (val <= 0) continue;
        html += '<i class="' + parts[i][0] + '" style="width:' + ((val / denom) * 100).toFixed(2) + '%"></i>';
      }
    }
    return html + '</div>';
  }

  function tmWaterfallBar(last, cls) {
    const ttft = Math.max(0, tmNum(last && last.ttft));
    const gen = Math.max(0, tmNum(last && last.gen));
    const out = tmNum(last && last.out);
    const think = tmNum(last && last.think);
    const thinkShare = out > 0 ? tmClamp(think / out, 0, 1) : 0;
    const segs = [
      ['ag-c-wait', ttft],
      ['ag-c-think', gen * thinkShare],
      ['ag-c-resp', gen * (1 - thinkShare)]
    ];
    let html = '<div class="' + (cls || 'ag-mp-wf') + '">';
    for (let i = 0; i < segs.length; i++) {
      if (segs[i][1] <= 0) continue;
      html += '<i class="' + segs[i][0] + '" style="flex-grow:' + segs[i][1].toFixed(3) + '"></i>';
    }
    return html + '</div>';
  }

  function renderDetailPanel(view) {
    const v = view || {};
    const last = v.last || {};
    const sess = v.session || {};
    const slowTtft = tmIsSlowTtft(last.ttft, sess.ttftMed);
    const e2e = tmCallE2eTps(last);
    const footLeft =
      (sess.calls > 1 && sess.ttftMed > 0 ? '中位 ' + tmFmtSec(sess.ttftMed) : '首轮调用') +
      (e2e > 0 ? ' · 端到端 ' + tmFmtTps(e2e) + '/s' : '');

    return (
      '<div class="ag-mp ag-mp-mini" data-mode="detail" data-level="' +
      tmLevel(v.ctx) +
      '">' +
      '<div class="ag-mp-mini-hd"><span class="ag-mp-title">' +
      tmEsc(last.model || '模型调用') +
      '</span><span class="ag-tt-live-chip"></span></div>' +
      '<div class="ag-tt-wait-note"></div>' +
      '<div class="ag-mp-grid3">' +
      '<div class="ag-mp-cell' +
      (slowTtft ? ' is-slow' : '') +
      '"><div class="ag-mp-ck">首字</div><div class="ag-mp-cv">' +
      tmUnitHtml(tmFmtSec(last.ttft)) +
      '</div></div>' +
      '<div class="ag-mp-cell"><div class="ag-mp-ck">吞吐</div><div class="ag-mp-cv">' +
      tmUnitHtml(tmFmtTps(last.tps) + '/s') +
      '</div></div>' +
      '<div class="ag-mp-cell"><div class="ag-mp-ck">缓存读</div><div class="ag-mp-cv">' +
      tmUnitHtml(tmFmtPct(last.hit)) +
      '</div></div>' +
      '</div>' +
      tmWaterfallBar(last, 'ag-mp-wf') +
      '<div class="ag-mp-mini-foot"><span>' +
      tmEsc(footLeft) +
      '</span><button type="button" class="ag-mp-open" data-ag-open-dock="timeline">详情 ↗</button></div>' +
      '</div>'
    );
  }

  function tmCompactOutlook(ctx) {
    const c = ctx || {};
    if (c.compacting) return '正在压缩上下文…';
    if (c.compacted) return c.summaryKb > 0 ? '摘要 ' + c.summaryKb + ' KB · 下步刷新' : '已压缩 · 下步刷新';
    if (c.growthN >= 3 && c.growth > 0) {
      if (c.callsLeft > 999 || c.callsLeft < 0) return '↗ +' + tmFmtTok(c.growth) + '/步';
      return '↗ +' + tmFmtTok(c.growth) + '/步 · 约 ' + tmFmtInt(c.callsLeft) + ' 步';
    }
    return '余 ' + tmFmtTok(c.remain);
  }

  function renderContextPanel(view) {
    const v = view || {};
    const ctx = v.ctx || {};
    const level = tmLevel(ctx);
    const limit = tmNum(ctx.limit);
    const heroLeft = ctx.compacted
      ? '<div class="ag-cp-big is-compacted">已压缩</div>'
      : '<div class="ag-cp-big">' + tmUnitHtml(tmFmtPct(ctx.pct)) + '</div>';
    const heroRight =
      '<div class="ag-cp-used"><b>' +
      tmUnitHtml(tmFmtTok(ctx.used)) +
      '</b> / ' +
      tmUnitHtml(tmFmtTok(limit)) +
      '</div>';
    const marks =
      limit > 0
        ? '<i class="ag-cp-mark" style="left:75%"></i><i class="ag-cp-mark" style="left:90%"></i>'
        : '';

    return (
      '<div class="ag-mp ag-mp-mini ag-cp" data-mode="ctx" data-level="' +
      level +
      '">' +
      '<div class="ag-tt-wait-note"></div>' +
      '<div class="ag-cp-hero">' +
      heroLeft +
      heroRight +
      '</div>' +
      '<div class="ag-cp-barwrap">' +
      tmStackBar(
        ctx.compacted ? [['ag-c-compacted', ctx.used]] : [['ag-c-cache', ctx.cached], ['ag-c-fresh', ctx.fresh]],
        limit,
        'ag-cp-bar'
      ) +
      marks +
      '</div>' +
      '<div class="ag-mp-mini-foot"><span>' +
      tmEsc(tmCompactOutlook(ctx)) +
      '</span><button type="button" class="ag-mp-open" data-ag-open-dock="ctx">详情 ↗</button></div>' +
      '</div>'
    );
  }

  function tmFlowSpineLayout(items, padY, spineH, gap, minH) {
    const pos = items.filter((it) => it.val > 0);
    if (pos.length === 0) return {};
    const total = pos.reduce((acc, it) => acc + it.val, 0);
    const usable = spineH - gap * (pos.length - 1);
    const heights = pos.map((it) => Math.max(minH, (it.val / total) * usable));
    const excess = heights.reduce((acc, h) => acc + h, 0) - usable;
    if (excess > 0) {
      const flexPool = heights.reduce((acc, h) => acc + Math.max(0, h - minH), 0);
      if (flexPool > 0) {
        for (let i = 0; i < heights.length; i++) {
          if (heights[i] > minH) {
            heights[i] -= excess * ((heights[i] - minH) / flexPool);
          }
        }
      }
    }
    const map = {};
    let curY = padY;
    for (let i = 0; i < pos.length; i++) {
      const h = heights[i];
      map[pos[i].key] = { y1: curY, y2: curY + h, h };
      curY += h + gap;
    }
    return map;
  }

  function tmTurnSegmentsFromHistory(hist, curTurnIdx) {
    const out = [];
    for (let i = 0; i < hist.length; i++) {
      const t = tmNum(hist[i].turn);
      const prev = out.length > 0 ? out[out.length - 1] : null;
      if (prev && prev.turn === t) {
        prev.count++;
        prev.endIdx = i;
      } else {
        out.push({ turn: t, count: 1, startIdx: i, endIdx: i, isCur: curTurnIdx > 0 && t === curTurnIdx });
      }
    }
    return out;
  }

  function tmEstLabelWidth(text) {
    let w = 6;
    for (let i = 0; i < text.length; i++) {
      w += text.charCodeAt(i) > 127 ? 9.6 : 5.8;
    }
    return w;
  }

  function renderDockTurnAxis(view, meta) {
    const v = view || {};
    const m = meta || {};
    const hist = Array.isArray(v.history) ? v.history : [];
    const W = Math.max(120, tmNum(m.W) || 296);
    const x0 = tmNum(m.x0);
    const slot = tmNum(m.slot);
    const gap = tmNum(m.gap);
    if (hist.length === 0 || slot <= 0) return '';

    const curTurnIdx = tmNum(v.turn && v.turn.index);
    const segs = tmTurnSegmentsFromHistory(hist, curTurnIdx);
    if (segs.length === 0 || !segs.some((s) => s.turn > 0)) return '';

    const n = segs.length;
    const boxes = segs.map((s) => {
      const left = x0 + s.startIdx * slot;
      const right = x0 + (s.endIdx + 1) * slot - gap;
      const segW = Math.max(2, right - left);
      const full = s.turn > 0 ? '第' + s.turn + '轮' : '前置';
      const short = s.turn > 0 ? String(s.turn) : '';
      const fullW = tmEstLabelWidth(full);
      const label = fullW <= segW + 2 ? full : short;
      const lw = label ? tmEstLabelWidth(label) - 4 : 0;
      const cx = left + segW / 2;
      const lblL = Math.max(0, Math.min(W - lw, cx - lw / 2));
      return { s, left, segW, label, lw, lblL, lblR: lblL + lw, show: false };
    });

    const minGap = 5;
    const placed = [];
    const order = [n - 1];
    for (let i = n - 2; i >= 0; i--) order.push(i);
    for (const i of order) {
      const b = boxes[i];
      if (!b.label) continue;
      const clash = placed.some((o) => b.lblL < o.lblR + minGap && b.lblR > o.lblL - minGap);
      if (clash) continue;
      b.show = true;
      placed.push(b);
    }

    let html = '';
    for (let i = 0; i < n; i++) {
      const b = boxes[i];
      html +=
        '<div class="ag-dk-tseg' +
        (b.s.isCur ? ' is-cur' : '') +
        (b.show ? ' has-lbl' : '') +
        '" data-ag-turn="' +
        b.s.turn +
        '" style="left:' +
        b.left.toFixed(1) +
        'px;width:' +
        b.segW.toFixed(1) +
        'px">' +
        (b.show
          ? '<span class="ag-dk-tlbl" style="left:' + (b.lblL + b.lw / 2 - b.left).toFixed(1) + 'px">' + b.label + '</span>'
          : '') +
        '</div>';
    }
    return html;
  }

  function tmFlowDistHtml(scope, stats, errors, subLabel) {
    const prompt = tmNum(stats.prompt);
    const cr = tmNum(stats.cr);
    const fresh = tmNum(stats.fresh);
    const out = tmNum(stats.out);
    const think = tmNum(stats.think);
    const resp = tmNum(stats.resp);
    const total = cr + fresh + think + resp;

    const items = [
      { key: 'cache', label: '缓存读', val: cr, color: 'var(--ag-c-cache)', dotCls: 'ag-c-cache' },
      { key: 'fresh', label: '读', val: fresh, color: 'var(--ag-c-fresh)', dotCls: 'ag-c-fresh' },
      { key: 'think', label: '思考', val: think, color: 'var(--ag-c-think)', dotCls: 'ag-c-think' },
      { key: 'resp', label: '输出', val: resp, color: 'var(--ag-c-resp)', dotCls: 'ag-c-resp' }
    ];

    const svgW = 64;
    const svgH = 124;
    const rowCenters = [14, 46, 78, 110];
    const spineX = 1;
    const spineW = 9;
    const startX = spineX + spineW + 2;
    const endX = svgW - 3;
    const c1x = startX + (endX - startX) * 0.46;
    const c2x = startX + (endX - startX) * 0.54;
    const segMap = tmFlowSpineLayout(items, 2, 120, 2.5, 4);

    let svgBody = '';
    if (total <= 0) {
      svgBody = '<rect class="ag-dk-fempty" x="1" y="2" width="9" height="120" rx="2.5"/>';
    } else {
      for (let i = 0; i < items.length; i++) {
        const it = items[i];
        const seg = segMap[it.key];
        if (!seg) continue;
        const rc = rowCenters[i];
        const rHalf = tmClamp(seg.h * 0.2, 1.3, 5.2);
        const ry1 = rc - rHalf;
        const ry2 = rc + rHalf;
        const path =
          'M ' +
          startX +
          ' ' +
          seg.y1.toFixed(1) +
          ' C ' +
          c1x.toFixed(1) +
          ' ' +
          seg.y1.toFixed(1) +
          ', ' +
          c2x.toFixed(1) +
          ' ' +
          ry1.toFixed(1) +
          ', ' +
          endX +
          ' ' +
          ry1.toFixed(1) +
          ' L ' +
          endX +
          ' ' +
          ry2.toFixed(1) +
          ' C ' +
          c2x.toFixed(1) +
          ' ' +
          ry2.toFixed(1) +
          ', ' +
          c1x.toFixed(1) +
          ' ' +
          seg.y2.toFixed(1) +
          ', ' +
          startX +
          ' ' +
          seg.y2.toFixed(1) +
          ' Z';
        svgBody +=
          '<g class="ag-dk-fband" data-ag-fkey="' +
          it.key +
          '">' +
          '<rect class="ag-dk-fspine" x="' +
          spineX +
          '" y="' +
          seg.y1.toFixed(1) +
          '" width="' +
          spineW +
          '" height="' +
          seg.h.toFixed(1) +
          '" rx="2.5" fill="' +
          it.color +
          '"/>' +
          '<path class="ag-dk-frib" d="' +
          path +
          '" fill="' +
          it.color +
          '"/>' +
          '<circle class="ag-dk-fnode" cx="' +
          endX +
          '" cy="' +
          rc +
          '" r="2" fill="' +
          it.color +
          '"/>' +
          '</g>';
      }
    }

    const rowsHtml = items
      .map((it) => {
        const share = total > 0 ? (it.val / total) * 100 : 0;
        const shareText = it.val > 0 && share < 0.1 ? '<0.1%' : tmFmtPct(share);
        return (
          '<div class="ag-dk-frow' +
          (it.val <= 0 ? ' is-zero' : '') +
          '" data-ag-fkey="' +
          it.key +
          '">' +
          '<span class="ag-dk-fname"><i class="' +
          it.dotCls +
          '"></i>' +
          it.label +
          '</span>' +
          '<span class="ag-dk-fval"><b>' +
          tmUnitHtml(tmFmtTok(it.val)) +
          '</b><span class="ag-dk-fpct">' +
          shareText +
          '</span></span>' +
          '</div>'
        );
      })
      .join('');

    return (
      '<div class="ag-dk-flow" data-ag-flow-scope="' +
      scope +
      '">' +
      '<div class="ag-dk-fstage">' +
      '<svg class="ag-dk-fsvg" width="' +
      svgW +
      '" height="' +
      svgH +
      '" viewBox="0 0 ' +
      svgW +
      ' ' +
      svgH +
      '" aria-hidden="true">' +
      svgBody +
      '</svg>' +
      '<div class="ag-dk-frows">' +
      rowsHtml +
      '</div>' +
      '</div>' +
      '<div class="ag-dk-ffoot">' +
      '<div class="ag-dk-rio">' +
      '<span class="ag-dk-rv" title="输入"><i class="ag-dk-arr is-in" aria-hidden="true">↓</i><b>' +
      tmUnitHtml(tmFmtTok(prompt)) +
      '</b></span>' +
      '<span class="ag-dk-rv" title="输出"><i class="ag-dk-arr is-out" aria-hidden="true">↑</i><b>' +
      tmUnitHtml(tmFmtTok(out)) +
      '</b></span>' +
      '</div>' +
      '<div class="ag-dk-rmeta">' +
      (subLabel ? tmEsc(subLabel) + ' · ' : '') +
      tmFmtInt(stats.calls) +
      '次 · ' +
      tmFmtShortSec(stats.dur) +
      (errors > 0 ? ' · <span class="ag-dk-err">' + tmFmtInt(errors) + '败</span>' : '') +
      '</div>' +
      '</div>' +
      '</div>'
    );
  }

  const CTX_BREAKDOWN_CLS = {
    msg: 'ag-c-ctx-msg',
    mcp: 'ag-c-ctx-mcp',
    tools: 'ag-c-ctx-tools',
    sys: 'ag-c-ctx-sys',
    cfg: 'ag-c-ctx-cfg'
  };

  function tmCtxBreakdownBar(groups, bTotal, used, limit) {
    let html = '<div class="ag-mp-bar ag-dk-cbar">';
    if (bTotal > 0 && limit > 0) {
      const fillPct = tmClamp((used > 0 ? used : bTotal) / limit, 0, 1) * 100;
      for (let i = 0; i < groups.length; i++) {
        const g = groups[i];
        const tok = Math.max(0, tmNum(g.tokens));
        if (tok <= 0) continue;
        const w = Math.max(0.6, (tok / bTotal) * fillPct);
        const cls = CTX_BREAKDOWN_CLS[g.key] || 'ag-c-ctx-cfg';
        html +=
          '<i class="' +
          cls +
          '" data-ag-ctx-seg="' +
          tmEsc(g.key) +
          '" style="width:' +
          w.toFixed(2) +
          '%"></i>';
      }
    }
    return html + '</div>';
  }

  function tmCtxBreakdownList(groups, bTotal, used, limit, expandedKey) {
    if (!groups.length || bTotal <= 0) return '';
    const scale = limit > 0 ? ((used > 0 ? used : bTotal) / limit) * 100 : 100;
    const rows = groups
      .map((g) => {
        const tok = Math.max(0, tmNum(g.tokens));
        if (tok <= 0) return '';
        const key = String(g.key || '');
        const cls = CTX_BREAKDOWN_CLS[key] || 'ag-c-ctx-cfg';
        const items = Array.isArray(g.items) ? g.items : [];
        const canExpand = Boolean(g.expandable && items.length > 0);
        const isOpen = canExpand && expandedKey === key;
        const pct = (tok / bTotal) * scale;
        const pctText = pct > 0 && pct < 0.1 ? '<0.1%' : tmFmtPct(pct);
        const tag = canExpand ? 'button' : 'div';
        const openAttrs = canExpand
          ? ' type="button" data-ag-ctx-toggle="' + tmEsc(key) + '" aria-expanded="' + (isOpen ? 'true' : 'false') + '"'
          : '';
        const countHtml =
          isOpen && tmNum(g.count) > 0
            ? '<span class="ag-dk-ccnt">' + tmFmtInt(g.count) + '</span>'
            : '';

        let subHtml = '';
        if (isOpen) {
          subHtml =
            '<div class="ag-dk-csub" data-ag-ctx-sub="' +
            tmEsc(key) +
            '">' +
            items
              .map((it) => {
                const cnt = tmNum(it.count) > 1 ? '<span class="ag-dk-cscnt"> (' + tmFmtInt(it.count) + ')</span>' : '';
                return (
                  '<div class="ag-dk-csrow">' +
                  '<span class="ag-dk-csname" title="' +
                  tmEsc(it.name) +
                  '">' +
                  tmEsc(it.name) +
                  cnt +
                  '</span>' +
                  '<span class="ag-dk-csval">' +
                  tmUnitHtml(tmFmtTok(it.tokens)) +
                  '</span>' +
                  '</div>'
                );
              })
              .join('') +
            '</div>';
        }

        return (
          '<div class="ag-dk-cgrp">' +
          '<' +
          tag +
          openAttrs +
          ' class="ag-dk-crow' +
          (canExpand ? ' is-exp' : '') +
          (isOpen ? ' is-open' : '') +
          '" data-ag-ctx-key="' +
          tmEsc(key) +
          '">' +
          '<span class="ag-dk-cleft"><i class="ag-dk-csq ' +
          cls +
          '"></i><span class="ag-dk-cname">' +
          tmEsc(g.label) +
          '</span>' +
          countHtml +
          '</span>' +
          '<span class="ag-dk-cright"><span class="ag-dk-ctok">' +
          tmUnitHtml(tmFmtTok(tok)) +
          '</span><span class="ag-dk-cshare">' +
          pctText +
          '</span></span>' +
          '</' +
          tag +
          '>' +
          subHtml +
          '</div>'
        );
      })
      .join('');
    return '<div class="ag-dk-cbreak">' + rows + '</div>';
  }

  function renderDockBody(view, state) {
    const v = view || {};
    const st = state || {};
    const ctx = v.ctx || {};
    const turn = v.turn || {};
    const sess = v.session || {};
    const hist = Array.isArray(v.history) ? v.history : [];
    const level = tmLevel(ctx);
    const limit = tmNum(ctx.limit);
    const barMode = st.barMode === 'hit' || st.barMode === 'out' ? st.barMode : 'dur';
    const distScope = st.distScope === 'turn' ? 'turn' : 'session';
    const pinnedIdx = typeof st.pinnedIdx === 'number' && st.pinnedIdx >= 0 && st.pinnedIdx < hist.length ? st.pinnedIdx : -1;

    const marks =
      limit > 0
        ? '<i class="ag-dk-tick" style="left:75%"></i><i class="ag-dk-tick" style="left:90%"></i>'
        : '';

    const pinChip =
      pinnedIdx >= 0
        ? '<button type="button" class="ag-dk-pin" data-ag-unpin="1" title="取消锁定">#' + tmFmtInt(hist[pinnedIdx].n) + ' ×</button>'
        : '';

    const turnSegs = tmTurnSegmentsFromHistory(hist, tmNum(turn.index));
    const hasTurnAxis = turnSegs.length > 0 && turnSegs.some((s) => s.turn > 0);
    const turnAxisHtml = hasTurnAxis
      ? '<div class="ag-dk-taxis-wrap" id="ag-dock-taxis-wrap"><div class="ag-dk-taxis" id="ag-dock-turn-axis" data-ag-taxis="1"></div></div>'
      : '';

    const turnFresh = tmNum(turn.in);
    const turnCr = tmNum(turn.cr);
    const turnStats = {
      prompt: turnFresh + turnCr,
      cr: turnCr,
      fresh: turnFresh,
      hit: tmNum(turn.hit),
      out: tmNum(turn.out),
      think: tmNum(turn.think),
      resp: tmNum(turn.resp),
      calls: tmNum(turn.calls),
      dur: tmNum(turn.dur)
    };
    const sessFresh = tmNum(sess.in) + tmNum(sess.cw);
    const sessStats = {
      prompt: tmNum(sess.prompt) || sessFresh + tmNum(sess.cr),
      cr: tmNum(sess.cr),
      fresh: sessFresh,
      hit: tmNum(sess.hit),
      out: tmNum(sess.out),
      think: tmNum(sess.think),
      resp: tmNum(sess.resp),
      calls: tmNum(sess.calls),
      dur: tmNum(sess.dur)
    };

    const turnSub = turn.index > 0 ? '第' + turn.index + '轮' : '';
    const sessSub = sess.turns > 0 ? sess.turns + '轮' : '';
    const activeStats = distScope === 'turn' ? turnStats : sessStats;
    const activeSub = distScope === 'turn' ? turnSub : sessSub;
    const activeErrors = distScope === 'turn' ? 0 : tmNum(sess.errors);
    const activeTotal = tmNum(activeStats.cr) + tmNum(activeStats.fresh) + tmNum(activeStats.think) + tmNum(activeStats.resp);
    const activeHit = tmNum(activeStats.prompt) > 0 ? (tmNum(activeStats.cr) / tmNum(activeStats.prompt)) * 100 : tmNum(activeStats.hit);
    const activeBadgeCls = activeHit >= 85 ? ' is-good' : tmNum(activeStats.prompt) > 0 && activeHit < 30 ? ' is-warn' : '';

    const breakdown = !ctx.compacted && Array.isArray(ctx.breakdown) ? ctx.breakdown.filter((g) => g && tmNum(g.tokens) > 0) : [];
    const breakdownTotal = tmNum(ctx.breakdownTotal) || breakdown.reduce((a, g) => a + Math.max(0, tmNum(g.tokens)), 0);
    const ctxExpanded = typeof st.ctxExpanded === 'string' ? st.ctxExpanded : '';
    const ctxBarHtml =
      breakdown.length > 0
        ? tmCtxBreakdownBar(breakdown, breakdownTotal, tmNum(ctx.used), limit)
        : tmStackBar(
            ctx.compacted ? [['ag-c-compacted', ctx.used]] : [['ag-c-cache', ctx.cached], ['ag-c-fresh', ctx.fresh]],
            limit,
            'ag-dk-cbar'
          );
    const ctxNoteHtml =
      ctx.compacting || ctx.compacted
        ? '<div class="ag-dk-cquiet"><span class="is-cp">' +
          tmEsc(ctx.compacting ? '正在压缩上下文…' : '已压缩，下一次调用后刷新') +
          '</span></div>'
        : '';

    return (
      '<section class="ag-dk-sec ag-dk-sec-ctx" data-ag-sec="ctx" data-level="' +
      level +
      '">' +
      '<div class="ag-dk-chero">' +
      '<div class="ag-dk-chead">' +
      '<span class="ag-dk-t">' +
      (ctx.compacted ? '上下文' : '上下文已用') +
      '</span>' +
      (ctx.compacted
        ? '<div class="ag-dk-cpct is-cp">已压缩</div>'
        : '<div class="ag-dk-cpct">' + tmUnitHtml(tmFmtPct(ctx.pct)) + '</div>') +
      '</div>' +
      '<div class="ag-dk-cratio" data-ag-tip="ctx-limit"><b>' +
      tmUnitHtml(tmFmtTok(ctx.used)) +
      '</b> / ' +
      tmUnitHtml(tmFmtTok(limit)) +
      '</div></div>' +
      '<div class="ag-dk-bwrap" data-ag-tip="ctx-bar">' +
      ctxBarHtml +
      marks +
      '</div>' +
      ctxNoteHtml +
      tmCtxBreakdownList(breakdown, breakdownTotal, tmNum(ctx.used), limit, ctxExpanded) +
      '</section>' +
      '<section class="ag-dk-sec ag-dk-sec-tl" data-ag-sec="timeline">' +
      '<div class="ag-dk-hd"><span class="ag-dk-twrap"><span class="ag-dk-t">步进时间线</span>' +
      pinChip +
      '</span><div class="ag-dk-pills" data-ag-barmode="1">' +
      '<button type="button" data-mode="dur"' +
      (barMode === 'dur' ? ' class="is-active"' : '') +
      '>耗时</button>' +
      '<button type="button" data-mode="hit"' +
      (barMode === 'hit' ? ' class="is-active"' : '') +
      '>缓存读</button>' +
      '<button type="button" data-mode="out"' +
      (barMode === 'out' ? ' class="is-active"' : '') +
      '>输出</button>' +
      '</div></div>' +
      '<div class="ag-dk-tlscroll" id="ag-dock-tl-scroll">' +
      '<div class="ag-dk-cbox" id="ag-dock-chart-box"></div>' +
      '</div>' +
      '<div class="ag-dk-tlbar" id="ag-dock-tl-bar" hidden><i class="ag-dk-tlthumb"></i></div>' +
      turnAxisHtml +
      '</section>' +
      '<section class="ag-dk-sec ag-dk-sec-dist" data-ag-sec="dist">' +
      '<div class="ag-dk-hd ag-dk-dist-hd"><span class="ag-dk-dist-sum">' +
      '<span class="ag-dk-t">累计</span>' +
      '<b class="ag-dk-dtot">' +
      tmUnitHtml(tmFmtTok(activeTotal)) +
      '</b>' +
      '<span class="ag-dk-fbadge' +
      activeBadgeCls +
      '">缓存 <b>' +
      tmUnitHtml(tmFmtPct(activeHit)) +
      '</b></span>' +
      '</span><div class="ag-dk-pills" data-ag-distscope="1">' +
      '<button type="button" data-scope="turn"' +
      (distScope === 'turn' ? ' class="is-active"' : '') +
      '>本轮</button>' +
      '<button type="button" data-scope="session"' +
      (distScope === 'session' ? ' class="is-active"' : '') +
      '>整场</button>' +
      '</div></div>' +
      tmFlowDistHtml(distScope, activeStats, activeErrors, activeSub) +
      '</section>'
    );
  }

  function renderDockUnifiedChart(view, opts) {
    const v = view || {};
    const o = opts || {};
    const hist = Array.isArray(v.history) ? v.history : [];
    const n = hist.length;
    const W = Math.max(120, Math.round(tmNum(o.width) || 296));
    const H = Math.max(52, Math.round(tmNum(o.height) || 140));
    const top = 6;
    const mode = o.barMode === 'hit' || o.barMode === 'out' ? o.barMode : 'dur';
    if (n === 0) {
      return {
        html: '<div class="ag-dk-empty">暂无调用记录</div>',
        meta: { W, H, top, x0: 0, slot: 0, bw: 0, limit: 1 }
      };
    }

    const ctx = v.ctx || {};
    const sess = v.session || {};
    const limit = Math.max(tmNum(ctx.limit), ...hist.map((c) => tmNum(c.ctx)), 1);
    const slot = Math.min(DOCK_MAX_SLOT, W / n);
    const gap = slot >= 24 ? Math.min(8, slot * 0.14) : slot >= 8 ? 2.2 : 1.2;
    const bw = Math.max(2, slot - gap);
    const x0 = W - slot * n;
    const cxOf = (i) => x0 + i * slot + bw / 2;
    const ctxY = (val) => top + (H - top - 2) * (1 - tmClamp(tmNum(val) / limit, 0, 1));

    const pts = hist.map((c, i) => [cxOf(i), ctxY(c.ctx)]);
    const line = pts.map((p, i) => (i ? 'L' : 'M') + p[0].toFixed(1) + ' ' + p[1].toFixed(1)).join('');
    const area = line + 'L' + pts[n - 1][0].toFixed(1) + ' ' + (H - 1) + 'L' + pts[0][0].toFixed(1) + ' ' + (H - 1) + 'Z';
    const warnY = ctxY(limit * 0.75);
    const level = tmLevel(ctx);

    let turnSeps = '';
    for (let i = 1; i < n; i++) {
      if (hist[i].turn !== hist[i - 1].turn) {
        const sx = (x0 + i * slot - gap / 2).toFixed(1);
        turnSeps += '<line class="ag-u-tsep" x1="' + sx + '" y1="' + top + '" x2="' + sx + '" y2="' + (H - 1) + '"/>';
      }
    }

    const maxBarH = H - top - 2;
    let cap = 100;
    if (mode === 'dur') {
      const vals = hist.map((c) => Math.max(0, tmNum(c.ttft)) + Math.max(0, tmNum(c.gen))).sort((a, b) => a - b);
      const p90 = vals[Math.min(n - 1, Math.floor(n * 0.9))] || 1;
      const max = vals[n - 1] || 1;
      cap = Math.max(0.001, max > p90 * 2.1 && p90 > 0 ? p90 * 2.1 : max);
    } else if (mode === 'out') {
      cap = Math.max(100, ...hist.map((c) => tmNum(c.out)));
    }

    const curTurn = tmNum(v.turn && v.turn.index);
    const hoverIdx = typeof o.hoverIdx === 'number' ? o.hoverIdx : -1;
    const pinnedIdx = typeof o.pinnedIdx === 'number' ? o.pinnedIdx : -1;
    const activeIdx = hoverIdx >= 0 && hoverIdx < n ? hoverIdx : pinnedIdx >= 0 && pinnedIdx < n ? pinnedIdx : -1;

    let bars = '';
    let cpDots = '';
    for (let i = 0; i < n; i++) {
      const c = hist[i];
      const x = x0 + i * slot;
      const old = curTurn > 0 && c.turn !== curTurn;
      const hl = activeIdx === i;
      let inner = '<rect class="ag-u-hit" x="' + x.toFixed(1) + '" y="0" width="' + slot.toFixed(1) + '" height="' + H + '"/>';
      if (mode === 'dur') {
        const wait = Math.max(0, tmNum(c.ttft));
        const gen = Math.max(0, tmNum(c.gen));
        const tot = wait + gen;
        const h = Math.max(2.5, (Math.min(tot, cap) / cap) * maxBarH);
        const genH = tot > 0 ? h * (gen / tot) : 0;
        const waitH = h - genH;
        if (genH > 0.4) {
          inner += '<rect class="ag-f-resp" x="' + x.toFixed(1) + '" y="' + (H - 1 - genH).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + genH.toFixed(1) + '" rx="1.2"/>';
        }
        if (waitH > 0.4) {
          const wCls = tmIsSlowTtft(wait, sess.ttftMed) ? 'ag-f-warn' : 'ag-f-wait';
          inner += '<rect class="' + wCls + '" x="' + x.toFixed(1) + '" y="' + (H - 1 - genH - waitH).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + Math.max(1, waitH - 0.9).toFixed(1) + '" rx="1.2"/>';
        }
      } else if (mode === 'hit') {
        const hit = tmClamp(tmNum(c.hit), 0, 100);
        const h = Math.max(2.5, (hit / 100) * maxBarH);
        const hCls = hit < 20 ? 'ag-f-warn' : 'ag-f-cache';
        inner += '<rect class="' + hCls + '" x="' + x.toFixed(1) + '" y="' + (H - 1 - h).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + h.toFixed(1) + '" rx="1.2"/>';
      } else {
        const out = Math.max(0, tmNum(c.out));
        const resp = Math.max(0, tmNum(c.resp));
        const h = Math.max(2.5, (out / cap) * maxBarH);
        const respH = out > 0 ? h * tmClamp(resp / out, 0, 1) : 0;
        const thinkH = h - respH;
        if (respH > 0.4) {
          inner += '<rect class="ag-f-resp" x="' + x.toFixed(1) + '" y="' + (H - 1 - respH).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + respH.toFixed(1) + '" rx="1.2"/>';
        }
        if (thinkH > 0.4) {
          inner += '<rect class="ag-f-think" x="' + x.toFixed(1) + '" y="' + (H - 1 - respH - thinkH).toFixed(1) + '" width="' + bw.toFixed(1) + '" height="' + Math.max(1, thinkH - 0.9).toFixed(1) + '" rx="1.2"/>';
        }
      }
      if (c.cp) {
        cpDots += '<circle class="ag-u-cp" cx="' + pts[i][0].toFixed(1) + '" cy="' + pts[i][1].toFixed(1) + '" r="2.8"/>';
      }
      bars += '<g class="ag-u-bar' + (old ? ' is-old' : '') + (hl ? ' is-hl' : '') + '" data-i="' + i + '">' + inner + '</g>';
    }

    const activePt = activeIdx >= 0 ? pts[activeIdx] : [0, 0];
    const html =
      '<svg class="ag-u-svg' +
      (activeIdx >= 0 ? ' is-active' : '') +
      '" data-ag-unified="1" data-level="' +
      level +
      '" viewBox="0 0 ' +
      W +
      ' ' +
      H +
      '" aria-hidden="true">' +
      '<line class="ag-u-grid" x1="0" y1="' + top + '" x2="' + W + '" y2="' + top + '"/>' +
      turnSeps +
      (n >= 2 ? '<path class="ag-u-area" d="' + area + '"/>' : '') +
      bars +
      '<line class="ag-u-warn" x1="0" y1="' + warnY.toFixed(1) + '" x2="' + W + '" y2="' + warnY.toFixed(1) + '"/>' +
      (H >= 72 ? '<text class="ag-u-lbl" x="2" y="' + (warnY - 3).toFixed(1) + '">75%</text>' : '') +
      (n >= 2 ? '<path class="ag-u-line" d="' + line + '"/>' : '') +
      cpDots +
      '<line class="ag-u-guide" x1="' + activePt[0].toFixed(1) + '" y1="' + top + '" x2="' + activePt[0].toFixed(1) + '" y2="' + (H - 1) + '"/>' +
      '<circle class="ag-u-dot" cx="' + activePt[0].toFixed(1) + '" cy="' + activePt[1].toFixed(1) + '" r="3.2"/>' +
      '<line class="ag-u-grid" x1="0" y1="' + (H - 0.5) + '" x2="' + W + '" y2="' + (H - 0.5) + '"/>' +
      '</svg>';

    return { html, meta: { W, H, top, x0, slot, bw, gap, limit } };
  }

  function tmHoverRow(labelHtml, valHtml, extraCls) {
    return (
      '<div class="ag-hc-row' +
      (extraCls ? ' ' + extraCls : '') +
      '"><span>' +
      labelHtml +
      '</span><b>' +
      valHtml +
      '</b></div>'
    );
  }

  function tmHoverDot(cls) {
    return '<i class="ag-hc-dot ' + cls + '"></i>';
  }

  function renderDockStepHover(view, idx, isPinned) {
    const v = view || {};
    const hist = Array.isArray(v.history) ? v.history : [];
    const c = idx >= 0 && idx < hist.length ? hist[idx] : null;
    if (!c) return '';
    const sess = v.session || {};
    const limit = tmNum(v.ctx && v.ctx.limit);
    const slow = tmIsSlowTtft(c.ttft, sess.ttftMed);
    const ctxPct = limit > 0 ? (tmNum(c.ctx) / limit) * 100 : 0;
    const e2e = tmCallE2eTps(c);
    const fresh = tmCallFresh(c);
    const hitCls = c.hit >= 85 ? 'is-good' : c.hit < 20 ? 'is-warn' : '';
    const prev = idx > 0 ? hist[idx - 1] : null;
    let missWhy = '';
    if (tmNum(c.ctx) > 0 && tmNum(c.cr) <= 0) {
      if (tmNum(c.n) <= 1) missWhy = '首次调用，还没有可复用的缓存';
      else if (prev && prev.model && c.model && prev.model !== c.model) missWhy = '换了模型，缓存不能跨模型复用';
      else missWhy = '服务端没接上缓存（空闲较久、缓存被回收等），整段重新读入';
    }

    return (
      '<div class="ag-hc-hd"><span class="ag-hc-title">#' +
      tmFmtInt(c.n) +
      (c.turn > 0 ? ' · 第' + c.turn + '轮' : '') +
      (slow ? '<span class="ag-hc-tag is-warn">慢</span>' : '') +
      (c.cp ? '<span class="ag-hc-tag is-cp">压缩后</span>' : '') +
      '</span><span class="ag-hc-sub">' +
      tmFmtSec(c.dur) +
      '</span></div>' +
      tmWaterfallBar(c, 'ag-hc-wf') +
      '<div class="ag-hc-grid">' +
      (tmNum(c.at) > 0 ? tmHoverRow('时间', tmFmtTime(c.at)) : '') +
      (c.model ? tmHoverRow('模型', '<span class="ag-hc-trunc" title="' + tmEsc(c.model) + '">' + tmEsc(c.model) + '</span>') : '') +
      tmHoverRow(tmHoverDot('ag-c-wait') + '首字 / 生成', '<span class="' + (slow ? 'is-warn' : '') + '">' + tmFmtSec(c.ttft) + '</span><u>/ ' + tmFmtSec(c.gen) + '</u>') +
      tmHoverRow('流式 / 端到端', tmFmtTps(c.tps) + '/s<u>/ ' + tmFmtTps(e2e) + '/s</u>') +
      tmHoverRow('上下文', tmFmtTok(c.ctx) + (limit > 0 ? '<u>' + tmFmtPct(ctxPct) + '</u>' : '')) +
      '<div class="ag-hc-div"></div>' +
      tmHoverRow(tmHoverDot('ag-c-cache') + '缓存读', '<span class="' + hitCls + '">' + tmFmtTok(c.cr) + '</span><u>' + tmFmtPct(c.hit) + '</u>') +
      tmHoverRow(tmHoverDot('ag-c-fresh') + '读', tmFmtTok(fresh)) +
      tmHoverRow(tmHoverDot('ag-c-think') + '思考 / ' + tmHoverDot('ag-c-resp') + '输出', tmFmtTok(c.think) + '<u>/ ' + tmFmtTok(c.resp) + '</u>') +
      (missWhy ? '<div class="ag-hc-note is-warn">未命中：' + tmEsc(missWhy) + '</div>' : '') +
      '</div>' +
      '<div class="ag-hc-hint">' +
      (isPinned ? '已锁定 · 方向键切换 · 再次点击解除' : '点击锁定 · 方向键逐帧切换') +
      '</div>'
    );
  }

  function tmTurnCalls(view, turnIdx) {
    const hist = Array.isArray(view && view.history) ? view.history : [];
    return hist.filter((c) => tmNum(c.turn) === turnIdx && tmNum(c.at) > 0);
  }

  function tmTurnFirstAt(view, turnIdx) {
    const gens = tmTurnCalls(view, turnIdx);
    return gens.length > 0 ? tmNum(gens[0].at) : 0;
  }

  function tmTurnEndAt(view, turnIdx) {
    const gens = tmTurnCalls(view, turnIdx);
    if (gens.length === 0) return 0;
    const last = gens[gens.length - 1];
    return tmNum(last.at) + Math.max(0, tmNum(last.dur)) * 1000;
  }

  function tmResolveTurnSummary(view, turns, turnIdx) {
    const list = Array.isArray(turns) ? turns : [];
    for (let i = 0; i < list.length; i++) {
      if (tmNum(list[i].turnIndex) === turnIdx) {
        const t = list[i];
        const fresh = tmNum(t.inTok) + tmNum(t.cacheWrite);
        const cr = tmNum(t.cacheRead);
        const prompt = tmNum(t.promptTok) || fresh + cr;
        return {
          turnIndex: turnIdx,
          startedMs: tmNum(t.startedMs) || tmTurnFirstAt(view, turnIdx),
          endedMs: tmTurnEndAt(view, turnIdx),
          calls: tmNum(t.steps),
          model: t.model || '',
          prompt,
          cr,
          fresh,
          hit: prompt > 0 ? (cr / prompt) * 100 : 0,
          out: tmNum(t.outTok),
          think: tmNum(t.thinkTok),
          resp: tmNum(t.respTok),
          streamDur: tmNum(t.streamDur),
          totDur: tmNum(t.totDur),
          tps: tmNum(t.tps),
          e2eTps: tmNum(t.e2eTps),
          lastCtx: tmNum(t.lastCtx),
          ctxPct: tmNum(t.ctxPct)
        };
      }
    }
    const hist = Array.isArray(view && view.history) ? view.history : [];
    const gens = hist.filter((c) => tmNum(c.turn) === turnIdx);
    if (gens.length === 0) return null;
    let cr = 0, fresh = 0, out = 0, think = 0, resp = 0, streamDur = 0, totDur = 0;
    for (let i = 0; i < gens.length; i++) {
      cr += tmNum(gens[i].cr);
      fresh += tmCallFresh(gens[i]);
      out += tmNum(gens[i].out);
      think += tmNum(gens[i].think);
      resp += tmNum(gens[i].resp);
      streamDur += tmNum(gens[i].gen);
      totDur += tmNum(gens[i].dur);
    }
    const prompt = cr + fresh;
    const lastCtx = tmNum(gens[gens.length - 1].ctx);
    const limit = tmNum(view && view.ctx && view.ctx.limit);
    return {
      turnIndex: turnIdx,
      startedMs: tmTurnFirstAt(view, turnIdx),
      endedMs: tmTurnEndAt(view, turnIdx),
      calls: gens.length,
      model: gens[gens.length - 1].model || '',
      prompt,
      cr,
      fresh,
      hit: prompt > 0 ? (cr / prompt) * 100 : 0,
      out,
      think,
      resp,
      streamDur,
      totDur,
      tps: streamDur > 0 ? out / streamDur : 0,
      e2eTps: totDur > 0 ? out / totDur : 0,
      lastCtx,
      ctxPct: limit > 0 ? (lastCtx / limit) * 100 : 0
    };
  }

  function renderDockTurnHover(view, turns, turnIdx) {
    const t = tmResolveTurnSummary(view, turns, turnIdx);
    if (!t) return '';
    const thinkPct = t.out > 0 ? (t.think / t.out) * 100 : 0;
    return (
      '<div class="ag-hc-hd"><span class="ag-hc-title">' +
      (t.turnIndex > 0 ? '第 ' + t.turnIndex + ' 轮' : '前置调用') +
      '</span><span class="ag-hc-sub">' +
      tmFmtInt(t.calls) +
      ' 次调用</span></div>' +
      '<div class="ag-hc-grid">' +
      (t.startedMs > 0
        ? tmHoverRow('时间', tmFmtTime(t.startedMs) + (t.endedMs > t.startedMs ? ' – ' + tmFmtTime(t.endedMs, t.startedMs) : ''))
        : '') +
      (t.model ? tmHoverRow('模型', '<span class="ag-hc-trunc">' + tmEsc(t.model) + '</span>') : '') +
      '<div class="ag-hc-div"></div>' +
      tmHoverRow('输入合计', tmFmtTok(t.prompt)) +
      tmHoverRow(tmHoverDot('ag-c-cache') + '缓存读', '<span class="is-good">' + tmFmtTok(t.cr) + '</span><u>' + tmFmtPct(t.hit) + '</u>') +
      tmHoverRow(tmHoverDot('ag-c-fresh') + '读', tmFmtTok(t.fresh) + '<u>' + tmFmtPct(100 - t.hit) + '</u>') +
      '<div class="ag-hc-div"></div>' +
      tmHoverRow('输出合计', tmFmtTok(t.out)) +
      tmHoverRow(tmHoverDot('ag-c-think') + '思考', tmFmtTok(t.think) + '<u>' + tmFmtPct(thinkPct) + '</u>') +
      tmHoverRow(tmHoverDot('ag-c-resp') + '输出', tmFmtTok(t.resp) + '<u>' + tmFmtPct(100 - thinkPct) + '</u>') +
      '<div class="ag-hc-div"></div>' +
      tmHoverRow('流式 / 端到端', tmFmtTps(t.tps) + '/s<u>/ ' + tmFmtTps(t.e2eTps) + '/s</u>') +
      tmHoverRow('耗时（流式）', tmFmtSec(t.totDur) + '<u>(' + tmFmtSec(t.streamDur) + ')</u>') +
      (t.lastCtx > 0 ? tmHoverRow('结束上下文', tmFmtTok(t.lastCtx) + '<u>' + tmFmtPct(t.ctxPct) + '</u>') : '') +
      '</div>'
    );
  }

  function renderDockRingHover(view, scope, focusKey) {
    const v = view || {};
    const turn = v.turn || {};
    const sess = v.session || {};
    const ctx = v.ctx || {};
    const isTurn = scope === 'turn';
    const isSpecific = focusKey === 'cache' || focusKey === 'fresh' || focusKey === 'think' || focusKey === 'resp';
    const dimIn = focusKey === 'inner' || focusKey === 'think' || focusKey === 'resp' ? 'is-dim' : '';
    const dimOut = focusKey === 'outer' || focusKey === 'cache' || focusKey === 'fresh' ? 'is-dim' : '';
    const dimCache = isSpecific ? (focusKey === 'cache' ? '' : 'is-dim') : dimIn;
    const dimFresh = isSpecific ? (focusKey === 'fresh' ? '' : 'is-dim') : dimIn;
    const dimThink = isSpecific ? (focusKey === 'think' ? '' : 'is-dim') : dimOut;
    const dimResp = isSpecific ? (focusKey === 'resp' ? '' : 'is-dim') : dimOut;

    const cr = isTurn ? tmNum(turn.cr) : tmNum(sess.cr);
    const fresh = isTurn ? tmNum(turn.in) : tmNum(sess.in) + tmNum(sess.cw);
    const prompt = isTurn ? cr + fresh : tmNum(sess.prompt) || cr + fresh;
    const hit = prompt > 0 ? (cr / prompt) * 100 : isTurn ? tmNum(turn.hit) : tmNum(sess.hit);
    const out = isTurn ? tmNum(turn.out) : tmNum(sess.out);
    const think = isTurn ? tmNum(turn.think) : tmNum(sess.think);
    const resp = isTurn ? tmNum(turn.resp) : tmNum(sess.resp);
    const thinkPct = out > 0 ? (think / out) * 100 : 0;
    const dur = isTurn ? tmNum(turn.dur) : tmNum(sess.dur);
    const streamDur = isTurn ? tmNum(turn.streamDur) : tmNum(sess.streamDur);
    const tps = streamDur > 0 && out > 0 ? out / streamDur : 0;
    const e2e = dur > 0 && out > 0 ? out / dur : 0;

    return (
      '<div class="ag-hc-hd"><span class="ag-hc-title">' +
      (isTurn ? '本轮' + (turn.index > 0 ? ' · 第 ' + turn.index + ' 轮' : '') : '整场会话') +
      '</span><span class="ag-hc-sub">' +
      (isTurn
        ? tmFmtInt(turn.calls) + ' 次 · ' + tmFmtInt(turn.steps) + ' 步'
        : tmFmtInt(sess.turns) + ' 轮 · ' + tmFmtInt(sess.calls) + ' 次') +
      '</span></div>' +
      '<div class="ag-hc-grid">' +
      tmHoverRow('输入合计', tmFmtTok(prompt), dimIn) +
      tmHoverRow(tmHoverDot('ag-c-cache') + '缓存读', '<span class="is-good">' + tmFmtTok(cr) + '</span><u>' + tmFmtPct(hit) + '</u>', dimCache) +
      tmHoverRow(tmHoverDot('ag-c-fresh') + '读', tmFmtTok(fresh) + '<u>' + tmFmtPct(100 - hit) + '</u>', dimFresh) +
      '<div class="ag-hc-div"></div>' +
      tmHoverRow('输出合计', tmFmtTok(out), dimOut) +
      tmHoverRow(tmHoverDot('ag-c-think') + '思考', tmFmtTok(think) + '<u>' + tmFmtPct(thinkPct) + '</u>', dimThink) +
      tmHoverRow(tmHoverDot('ag-c-resp') + '输出', tmFmtTok(resp) + '<u>' + tmFmtPct(100 - thinkPct) + '</u>', dimResp) +
      '<div class="ag-hc-div"></div>' +
      tmHoverRow('耗时' + (streamDur > 0 ? '（流式）' : ''), tmFmtSec(dur) + (streamDur > 0 ? '<u>(' + tmFmtSec(streamDur) + ')</u>' : '')) +
      (isTurn
        ? tps > 0
          ? tmHoverRow('流式 / 端到端', tmFmtTps(tps) + '/s<u>/ ' + tmFmtTps(e2e) + '/s</u>')
          : ''
        : tmHoverRow('首字 / 吞吐中位', tmFmtSec(sess.ttftMed) + '<u>/ ' + tmFmtTps(sess.tpsMed) + '/s</u>') +
          tmHoverRow(
            '失败 / 压缩',
            '<span class="' + (sess.errors > 0 ? 'is-err' : '') + '">' + tmFmtInt(sess.errors) + ' 次</span><u>/ ' + tmFmtInt(ctx.compactions) + ' 次</u>'
          )) +
      '</div>'
    );
  }

  function renderDockContextHover(view, tipKey) {
    const ctx = (view && view.ctx) || {};
    const limit = tmNum(ctx.limit);
    if (tipKey === 'ctx-bar') {
      if (ctx.compacted) {
        return (
          '<div class="ag-hc-grid">' +
          tmHoverRow('压缩前窗口', tmFmtTok(ctx.used) + '<u>(' + tmFmtInt(ctx.used) + ')</u>') +
          (ctx.summaryKb > 0 ? tmHoverRow('摘要大小', ctx.summaryKb + ' KB') : '') +
          '</div>'
        );
      }
      return (
        '<div class="ag-hc-grid">' +
        tmHoverRow(tmHoverDot('ag-c-cache') + '缓存读', '<span class="is-good">' + tmFmtTok(ctx.cached) + '</span><u>(' + tmFmtInt(ctx.cached) + ')</u>') +
        tmHoverRow(tmHoverDot('ag-c-fresh') + '读', tmFmtTok(ctx.fresh) + '<u>(' + tmFmtInt(ctx.fresh) + ')</u>') +
        tmHoverRow('剩余可用', tmFmtTok(ctx.remain)) +
        '</div>'
      );
    }
    if (tipKey === 'ctx-limit') {
      return (
        '<div class="ag-hc-grid">' +
        tmHoverRow('窗口上限', tmFmtTok(limit) + '<u>' + tmLimitSrcText(ctx.limitSrc) + '</u>') +
        tmHoverRow('历史峰值', tmFmtTok(ctx.peak) + (limit > 0 ? '<u>' + tmFmtPct((tmNum(ctx.peak) / limit) * 100) + '</u>' : '')) +
        tmHoverRow('已压缩次数', tmFmtInt(ctx.compactions) + ' 次') +
        '</div>'
      );
    }
    if (tipKey === 'ctx-growth') {
      const pace =
        ctx.compacting
          ? '正在压缩对话历史'
          : ctx.compacted
          ? '下一次调用后刷新'
          : ctx.growthN >= 3 && ctx.growth > 0
          ? ctx.callsLeft > 999 || ctx.callsLeft < 0
            ? '离上限还很远'
            : '约 ' + tmFmtInt(ctx.callsLeft) + ' 步触顶'
          : '用量平稳';
      return (
        '<div class="ag-hc-grid">' +
        (ctx.growthN >= 3 ? tmHoverRow('近 ' + ctx.growthN + ' 步均增', (ctx.growth > 0 ? '+' : '') + tmFmtTok(ctx.growth) + ' / 步') : '') +
        tmHoverRow('预计趋势', pace) +
        '</div>'
      );
    }
    return '';
  }

  function renderTurnBadge(t) {
    const parts = [];
    const sep = '<span class="ag-sep"> · </span>';
    if (t.model) parts.push('<span class="ag-turn-model">' + tmEsc(t.model) + '</span>');
    const fresh = tmNum(t.inTok) + tmNum(t.cacheWrite);
    const prompt = fresh + tmNum(t.cacheRead);
    if (prompt > 0) parts.push('<span>输入 ' + tmFmtTok(prompt) + '</span>');
    if (t.outTok > 0) parts.push('<span>输出 ' + tmFmtTok(t.outTok) + '</span>');
    if (prompt > 0) parts.push('<span>缓存读 ' + tmFmtPct((tmNum(t.cacheRead) / prompt) * 100) + '</span>');
    if (t.tps > 0) parts.push('<span>吞吐 ' + tmFmtTps(t.tps) + '/s</span>');
    const html = parts.join(sep);

    const rows = [];
    const row = (k, val) => '<div class="ag-tmt-row"><span class="ag-tmt-lbl">' + k + '</span><span class="ag-tmt-val">' + val + '</span></div>';
    if (t.lastCtx > 0) {
      const cls = t.ctxPct >= 90 ? 'ag-tmt-ctx-crit' : t.ctxPct >= 75 ? 'ag-tmt-ctx-warn' : '';
      const limitLabel = tmNum(t.contextLimit) > 0 ? tmFmtTok(t.contextLimit) : tmEsc(t.limitStr || '');
      rows.push(row('结束时上下文', tmFmtTok(t.lastCtx) + ' / ' + limitLabel + ' <span class="' + cls + '">（' + tmFmtPct(t.ctxPct) + '）</span>'));
    }
    if (t.outTok > 0) {
      rows.push(row('输出', tmFmtTok(t.outTok) + ' <span class="ag-tmt-sub">思考 ' + tmFmtTok(t.thinkTok) + ' · 输出 ' + tmFmtTok(t.respTok) + '</span>'));
    }
    if (prompt > 0) {
      rows.push(
        row(
          '输入',
          tmFmtTok(prompt) +
            ' <span class="ag-tmt-sub">缓存读 ' +
            tmFmtTok(t.cacheRead) +
            '（' +
            tmFmtPct((tmNum(t.cacheRead) / prompt) * 100) +
            '） · 读 ' +
            tmFmtTok(fresh) +
            '</span>'
        )
      );
    }
    if (t.tps > 0 || t.totDur > 0) {
      rows.push(row('速度与耗时', (t.tps > 0 ? '吞吐 ' + tmFmtTps(t.tps) + '/s · ' : '') + '生成 ' + tmFmtSec(t.streamDur) + ' / 共 ' + tmFmtSec(t.totDur)));
    }
    const hdr = t.model
      ? '<div class="ag-tmt-hdr"><span class="ag-tmt-model">' + tmEsc(t.model) + '</span><span class="ag-tmt-chip">' + tmFmtInt(t.steps || 1) + ' 次调用</span></div>'
      : '';
    const tooltipHtml = hdr || rows.length > 0 ? hdr + '<div class="ag-tmt-grid">' + rows.join('') + '</div>' : '';
    return { html, tooltipHtml };
  }
