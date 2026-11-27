  // Host postMessage contract (window.parent only):
  // in: agytheme:state, agytheme:focus, theme-change | out: agytheme:ready, agytheme:prefs, agytheme:toggle
  const TL_MIN_SLOT = 8;

  const st = {
    cid: '',
    payload: null,
    view: null,
    turns: null,
    snap: null,
    barMode: 'dur',
    distScope: 'session',
    ctxExpanded: '',
    prefsSeen: false,
    pinnedIdx: -1,
    hoverIdx: -1,
    chartMeta: null,
    chartRaf: 0,
    // Distance from right edge keeps scrolled-back views anchored while auto-following at the tail.
    tlFromRight: 0,
    chartRo: null,
    modalOpen: false,
    settings: null,
    doctor: null,
    pollTimer: 0,
    rollbackConfirm: false,
    rollbackTimer: 0,
    metricsEs: null,
    metricsStreamCid: ''
  };

  const urlParams = (() => {
    try {
      return new URLSearchParams(window.location.search || '');
    } catch (e) {
      return new URLSearchParams();
    }
  })();
  const urlToken = urlParams.get('token') || '';
  const initialCid = urlParams.get('conversationId') || '';

  function apiFetch(path, method, body) {
    const headers = { 'X-AgyTheme-UI': '1' };
    let target = path;
    if (urlToken) {
      headers['x-sidecar-token'] = urlToken;
      target += (path.includes('?') ? '&' : '?') + 'token=' + encodeURIComponent(urlToken);
    }
    const opts = { method: method || 'GET', credentials: 'same-origin', headers };
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    return fetch(target, opts).then(async (res) => {
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error((data && data.error) || '请求失败 (' + res.status + ')');
      return data;
    });
  }

  const byId = (id) => document.getElementById(id);

  function postToHost(msg) {
    try {
      if (window.parent && window.parent !== window) window.parent.postMessage(msg, '*');
    } catch (e) {}
  }

  function payloadView(m) {
    return m && m.hasData && m.view && typeof m.view === 'object' ? m.view : null;
  }

  function setTheme(mode) {
    const want = mode === 'dark' ? 'dark' : 'light';
    const root = document.documentElement;
    if (root.getAttribute('data-ag-theme-mode') !== want) root.setAttribute('data-ag-theme-mode', want);
    root.style.colorScheme = want;
  }

  function buildDom() {
    const dock = document.createElement('div');
    dock.id = 'ag-telemetry-dock';
    dock.setAttribute('role', 'main');
    dock.setAttribute('aria-label', '用量统计');
    dock.innerHTML =
      '<div class="ag-dk-header">' +
      '<div class="ag-dk-hrow">' +
      '<div class="ag-dk-model" id="ag-dock-model">用量统计</div>' +
      '<div class="ag-dk-hright">' +
      '<button type="button" id="ag-dock-settings-btn" title="AgyTheme 设置" aria-label="AgyTheme 设置">' +
      '<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
      '<circle cx="12" cy="12" r="3"></circle>' +
      '<path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9c.26.6.85 1 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path>' +
      '</svg>' +
      '<i id="ag-dock-upd-dot"></i>' +
      '</button>' +
      '</div>' +
      '</div>' +
      '<div class="ag-dk-alert ag-tt-wait-note" id="ag-dock-alert"></div>' +
      '</div>' +
      '<div class="ag-dk-body" id="ag-dock-body"></div>' +
      '<div class="ag-set-overlay" id="ag-settings-overlay" role="dialog" aria-modal="true" aria-label="AgyTheme 设置">' +
      '<div class="ag-set-card">' +
      '<div class="ag-set-hd">' +
      '<span class="ag-set-title">AgyTheme 设置</span>' +
      '<button type="button" class="ag-set-close" id="ag-set-close" aria-label="关闭">✕</button>' +
      '</div>' +
      '<div class="ag-set-body">' +
      '<div class="ag-set-box ag-set-upd" id="ag-set-upd-box">' +
      '<div class="ag-set-upd-row">' +
      '<div class="ag-set-upd-left">' +
      '<i class="ag-set-dot is-warn" id="ag-set-upd-dot" style="display:none"></i>' +
      '<span class="ag-set-ver" id="ag-set-ver">v--</span>' +
      '<span class="ag-set-hint" id="ag-set-upd-hint"></span>' +
      '</div>' +
      '<button type="button" class="ag-set-btn" id="ag-set-upd-btn">检查更新</button>' +
      '</div>' +
      '<div class="ag-set-note" id="ag-set-upd-note" style="display:none"></div>' +
      '<div class="ag-set-prog" id="ag-set-prog"><i id="ag-set-prog-bar"></i></div>' +
      '</div>' +
      '<div class="ag-set-box">' +
      '<div class="ag-set-row"><span class="ag-set-label">底部状态栏</span>' +
      '<div class="ag-set-seg" data-set-seg="telemetryLayout"><button type="button" data-val="compact">紧凑</button><button type="button" data-val="detailed">详细</button></div></div>' +
      '<div class="ag-set-row"><span class="ag-set-label">缓存统计口径</span>' +
      '<div class="ag-set-seg" data-set-seg="telemetryCacheMode"><button type="button" data-val="session">整场</button><button type="button" data-val="step">单步</button></div></div>' +
      '<div class="ag-set-row"><span class="ag-set-label">中文界面<span class="ag-set-tag" data-mod-tag="i18n" style="display:none">未安装</span></span>' +
      '<button type="button" class="ag-set-sw" data-set-mod="i18n" aria-label="中文界面"></button></div>' +
      '<div class="ag-set-row"><span class="ag-set-label">主题美化<span class="ag-set-tag" data-mod-tag="theme" style="display:none">未安装</span></span>' +
      '<button type="button" class="ag-set-sw" data-set-mod="theme" aria-label="主题美化"></button></div>' +
      '<div class="ag-set-row"><span class="ag-set-label">底部用量状态栏<span class="ag-set-tag" data-mod-tag="metrics" style="display:none">未安装</span></span>' +
      '<button type="button" class="ag-set-sw" data-set-mod="metrics" aria-label="底部用量状态栏"></button></div>' +
      '</div>' +
      '<div class="ag-set-ft">' +
      '<button type="button" class="ag-set-link" id="ag-set-doc-btn"><i class="ag-set-dot" id="ag-set-doc-dot"></i><span id="ag-set-doc-txt">运行正常</span></button>' +
      '<div class="ag-set-ft-right">' +
      '<button type="button" class="ag-set-link" id="ag-set-rb-btn" style="display:none">回滚</button>' +
      '<span class="ag-set-sep" id="ag-set-rb-sep" style="display:none">|</span>' +
      '<button type="button" class="ag-set-link" id="ag-set-dir-btn">配置目录</button>' +
      '</div>' +
      '</div>' +
      '</div>' +
      '</div>' +
      '</div>';
    document.body.appendChild(dock);

    const hc = document.createElement('div');
    hc.id = 'ag-telemetry-hover';
    hc.className = 'ag-dk-hover';
    document.body.appendChild(hc);

    const body = byId('ag-dock-body');
    body.addEventListener('click', onBodyClick);
    body.addEventListener('mousemove', onBodyMove);
    body.addEventListener('mouseleave', onBodyLeave);
    bindSettingsModal();
  }

  function showHover(html, anchor, compact) {
    const hc = byId('ag-telemetry-hover');
    if (!hc || !html) return;
    hc.className = 'ag-dk-hover is-visible' + (compact ? ' is-compact' : '');
    if (hc.__agRaw !== html) {
      hc.__agRaw = html;
      hc.innerHTML = html;
    }
    const vw = document.documentElement.clientWidth || window.innerWidth || 320;
    const vh = document.documentElement.clientHeight || window.innerHeight || 600;
    const cw = hc.offsetWidth || 244;
    const ch = hc.offsetHeight || 160;
    const x = typeof anchor.x === 'number' ? anchor.x : vw / 2;
    const left = Math.max(8, Math.min(Math.round(x - cw / 2), vw - cw - 8));
    const below = Math.round(anchor.bottom + 8);
    const above = Math.round(anchor.top - ch - 8);
    const top = below + ch <= vh - 8 ? below : above >= 8 ? above : Math.max(8, vh - ch - 8);
    hc.style.left = left + 'px';
    hc.style.top = top + 'px';
  }

  function chartAnchor(idx) {
    const box = byId('ag-dock-chart-box');
    const scroller = byId('ag-dock-tl-scroll');
    const meta = st.chartMeta;
    if (!box) return null;
    const r = (scroller || box).getBoundingClientRect();
    const br = box.getBoundingClientRect();
    let x = r.left + r.width / 2;
    if (meta && meta.slot > 0 && idx >= 0 && br.width > 0) {
      x = br.left + ((meta.x0 + idx * meta.slot + meta.bw / 2) / meta.W) * br.width;
      x = Math.max(r.left, Math.min(r.right, x));
    }
    return { top: r.top, bottom: r.bottom, x };
  }

  function hideHover(force) {
    const hc = byId('ag-telemetry-hover');
    if (!hc) return;
    if (!force && st.pinnedIdx >= 0 && st.view) {
      const html = renderDockStepHover(st.view, st.pinnedIdx, true);
      const anchor = chartAnchor(st.pinnedIdx);
      if (html && anchor) {
        showHover(html, anchor, false);
        return;
      }
    }
    hc.classList.remove('is-visible');
  }

  function paintChart() {
    const box = byId('ag-dock-chart-box');
    const view = st.view;
    if (!box || !view) return;
    const scroller = byId('ag-dock-tl-scroll');
    const viewW = Math.round(scroller ? scroller.clientWidth : box.getBoundingClientRect().width || box.clientWidth || 0);
    if (viewW < 40) return;
    const n = Array.isArray(view.history) ? view.history.length : 0;
    const width = Math.max(viewW, n * TL_MIN_SLOT);
    const taxis = byId('ag-dock-turn-axis');
    const wpx = width + 'px';
    if (box.style.width !== wpx) box.style.width = wpx;
    if (taxis && taxis.style.width !== wpx) taxis.style.width = wpx;
    const height = Math.round(box.getBoundingClientRect().height || box.clientHeight || 0);
    if (height < 24) return;
    const res = renderDockUnifiedChart(view, {
      width,
      height,
      barMode: st.barMode,
      hoverIdx: st.hoverIdx,
      pinnedIdx: st.pinnedIdx
    });
    st.chartMeta = res.meta;
    if (box.__agRawSvg !== res.html) {
      box.__agRawSvg = res.html;
      box.innerHTML = res.html;
    }
    if (taxis) {
      const axisHtml = renderDockTurnAxis(view, res.meta);
      if (taxis.__agRawAxis !== axisHtml) {
        taxis.__agRawAxis = axisHtml;
        taxis.innerHTML = axisHtml;
      }
    }
    if (scroller) {
      const maxLeft = Math.max(0, scroller.scrollWidth - scroller.clientWidth);
      scroller.classList.toggle('is-scrollable', maxLeft > 0);
      const target = Math.max(0, maxLeft - Math.max(0, st.tlFromRight || 0));
      if (Math.abs(scroller.scrollLeft - target) > 0.5) scroller.scrollLeft = target;
    }
    syncTimelineBar();
  }

  function syncTimelineBar() {
    const scroller = byId('ag-dock-tl-scroll');
    const bar = byId('ag-dock-tl-bar');
    if (!scroller) return;
    const taxisWrap = byId('ag-dock-taxis-wrap');
    if (taxisWrap && Math.abs(taxisWrap.scrollLeft - scroller.scrollLeft) > 0.5) {
      taxisWrap.scrollLeft = scroller.scrollLeft;
    }
    if (!bar) return;
    const sw = scroller.scrollWidth;
    const cw = scroller.clientWidth;
    const maxLeft = Math.max(0, sw - cw);
    const show = maxLeft > 0.5;
    if (bar.hidden === show) bar.hidden = !show;
    if (!show) return;
    const thumb = bar.querySelector('.ag-dk-tlthumb');
    const trackW = bar.clientWidth;
    if (!thumb || trackW <= 0) return;
    const thumbW = Math.max(24, Math.round((cw / sw) * trackW));
    const left = (scroller.scrollLeft / maxLeft) * (trackW - thumbW);
    const wpx = thumbW + 'px';
    if (thumb.style.width !== wpx) thumb.style.width = wpx;
    thumb.style.transform = 'translateX(' + left.toFixed(1) + 'px)';
  }

  function bindTimelineBar() {
    const bar = byId('ag-dock-tl-bar');
    if (!bar || bar.__agTlBound) return;
    bar.__agTlBound = true;
    bar.addEventListener('pointerdown', (e) => {
      if (e.button !== 0) return;
      const scroller = byId('ag-dock-tl-scroll');
      const thumb = bar.querySelector('.ag-dk-tlthumb');
      if (!scroller || !thumb) return;
      const maxLeft = Math.max(0, scroller.scrollWidth - scroller.clientWidth);
      if (maxLeft <= 0) return;
      e.preventDefault();
      const thumbW = thumb.offsetWidth;
      const travel = Math.max(1, bar.clientWidth - thumbW);
      const r = bar.getBoundingClientRect();
      const thumbL = (scroller.scrollLeft / maxLeft) * travel;
      const x = e.clientX - r.left;
      if (x < thumbL || x > thumbL + thumbW) {
        scroller.scrollLeft = ((x - thumbW / 2) / travel) * maxLeft;
      }
      const startX = e.clientX;
      const startLeft = scroller.scrollLeft;
      bar.classList.add('is-dragging');
      try { bar.setPointerCapture(e.pointerId); } catch (err) {}
      const move = (ev) => {
        scroller.scrollLeft = startLeft + ((ev.clientX - startX) / travel) * maxLeft;
      };
      const up = () => {
        bar.classList.remove('is-dragging');
        bar.removeEventListener('pointermove', move);
        bar.removeEventListener('pointerup', up);
        bar.removeEventListener('pointercancel', up);
      };
      bar.addEventListener('pointermove', move);
      bar.addEventListener('pointerup', up);
      bar.addEventListener('pointercancel', up);
    });
  }

  function revealTimelineIdx(idx) {
    const scroller = byId('ag-dock-tl-scroll');
    const meta = st.chartMeta;
    if (!scroller || !meta || !(meta.slot > 0) || idx < 0) return;
    const left = meta.x0 + idx * meta.slot;
    const right = left + meta.slot;
    const pad = meta.slot * 2;
    let next = scroller.scrollLeft;
    if (left - pad < next) next = left - pad;
    else if (right + pad > next + scroller.clientWidth) next = right + pad - scroller.clientWidth;
    const maxLeft = Math.max(0, scroller.scrollWidth - scroller.clientWidth);
    next = Math.max(0, Math.min(maxLeft, next));
    st.tlFromRight = maxLeft - next;
    if (Math.abs(scroller.scrollLeft - next) > 0.5) scroller.scrollLeft = next;
  }

  function scheduleChartPaint() {
    if (st.chartRaf) return;
    st.chartRaf = requestAnimationFrame(() => {
      st.chartRaf = 0;
      paintChart();
      if (st.pinnedIdx >= 0) hideHover(false);
    });
  }

  function attachChartObserver() {
    if (st.chartRo) {
      try { st.chartRo.disconnect(); } catch (e) {}
      st.chartRo = null;
    }
    const scroller = byId('ag-dock-tl-scroll');
    if (scroller && !scroller.__agTlBound) {
      scroller.__agTlBound = true;
      scroller.addEventListener('scroll', () => {
        st.tlFromRight = Math.max(0, scroller.scrollWidth - scroller.clientWidth - scroller.scrollLeft);
        syncTimelineBar();
        if (st.hoverIdx >= 0) {
          st.hoverIdx = -1;
          highlightBar(st.pinnedIdx);
          hideHover(false);
        }
      }, { passive: true });
      scroller.addEventListener('wheel', (e) => {
        if (scroller.scrollWidth <= scroller.clientWidth + 1) return;
        const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? scroller.clientWidth : 1;
        const d = (Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY) * unit;
        if (!d) return;
        e.preventDefault();
        scroller.scrollLeft += d;
      }, { passive: false });
    }
    const taxisWrap = byId('ag-dock-taxis-wrap');
    if (taxisWrap && scroller && !taxisWrap.__agTlBound) {
      taxisWrap.__agTlBound = true;
      taxisWrap.addEventListener('wheel', (e) => {
        if (scroller.scrollWidth <= scroller.clientWidth + 1) return;
        const unit = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? scroller.clientWidth : 1;
        const d = (Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY) * unit;
        if (!d) return;
        e.preventDefault();
        scroller.scrollLeft += d;
      }, { passive: false });
    }
    bindTimelineBar();
    const target = scroller || byId('ag-dock-chart-box');
    if (!target || typeof ResizeObserver === 'undefined') return;
    st.chartRo = new ResizeObserver(() => scheduleChartPaint());
    try { st.chartRo.observe(target); } catch (e) {}
  }

  function highlightBar(idx) {
    const box = byId('ag-dock-chart-box');
    const view = st.view;
    const meta = st.chartMeta;
    if (!box || !view || !meta) return;
    const svg = box.querySelector('svg[data-ag-unified]');
    if (!svg) return;
    const hist = Array.isArray(view.history) ? view.history : [];
    const active = idx >= 0 && idx < hist.length;
    svg.classList.toggle('is-active', active);
    svg.querySelectorAll('.ag-u-bar.is-hl').forEach((el) => el.classList.remove('is-hl'));
    if (!active) return;
    const bar = svg.querySelector('.ag-u-bar[data-i="' + idx + '"]');
    if (bar) bar.classList.add('is-hl');
    const cx = (meta.x0 + idx * meta.slot + meta.bw / 2).toFixed(1);
    const cy = (meta.top + (meta.H - meta.top - 2) * (1 - tmClamp(tmNum(hist[idx].ctx) / meta.limit, 0, 1))).toFixed(1);
    const guide = svg.querySelector('.ag-u-guide');
    const dot = svg.querySelector('.ag-u-dot');
    if (guide) {
      guide.setAttribute('x1', cx);
      guide.setAttribute('x2', cx);
    }
    if (dot) {
      dot.setAttribute('cx', cx);
      dot.setAttribute('cy', cy);
    }
  }

  function setFlowFocus(flow, key) {
    if (!flow) return;
    if (key) {
      if (flow.getAttribute('data-focus-key') !== key) flow.setAttribute('data-focus-key', key);
    } else if (flow.hasAttribute('data-focus-key')) {
      flow.removeAttribute('data-focus-key');
    }
  }

  function setCtxFocus(sec, key) {
    if (!sec) return;
    if (key) {
      if (sec.getAttribute('data-ctx-focus') !== key) sec.setAttribute('data-ctx-focus', key);
    } else if (sec.hasAttribute('data-ctx-focus')) {
      sec.removeAttribute('data-ctx-focus');
    }
  }

  function savePrefs() {
    postToHost({ type: 'agytheme:prefs', barMode: st.barMode, distScope: st.distScope });
  }

  function toggleCtxBreakdown(key) {
    if (!key) return;
    const list = st.view && st.view.ctx && Array.isArray(st.view.ctx.breakdown) ? st.view.ctx.breakdown : [];
    const grp = list.find((g) => g && g.key === key && g.expandable && Array.isArray(g.items) && g.items.length > 0);
    if (!grp) return;
    st.ctxExpanded = st.ctxExpanded === key ? '' : key;
    render(true);
    hideHover(true);
  }

  function onBodyClick(e) {
    const t = e.target && e.target.closest ? e.target : null;
    if (!t) return;
    const ctxBtn = t.closest('[data-ag-ctx-toggle]');
    if (ctxBtn) {
      toggleCtxBreakdown(ctxBtn.getAttribute('data-ag-ctx-toggle') || '');
      return;
    }
    const ctxSeg = t.closest('[data-ag-ctx-seg]');
    if (ctxSeg) {
      toggleCtxBreakdown(ctxSeg.getAttribute('data-ag-ctx-seg') || '');
      return;
    }
    const modeBtn = t.closest('[data-ag-barmode] button[data-mode]');
    if (modeBtn) {
      const next = modeBtn.getAttribute('data-mode');
      if (next === 'dur' || next === 'hit' || next === 'out') {
        st.barMode = next;
        savePrefs();
        render(true);
      }
      return;
    }
    const scopeBtn = t.closest('[data-ag-distscope] button[data-scope]');
    if (scopeBtn) {
      const next = scopeBtn.getAttribute('data-scope');
      if (next === 'turn' || next === 'session') {
        st.distScope = next;
        savePrefs();
        render(true);
        hideHover(true);
      }
      return;
    }
    if (t.closest('[data-ag-unpin]')) {
      st.pinnedIdx = -1;
      render(true);
      hideHover(true);
      return;
    }
    if (t.closest('svg[data-ag-unified]') && st.hoverIdx >= 0) {
      st.pinnedIdx = st.pinnedIdx === st.hoverIdx ? -1 : st.hoverIdx;
      render(true);
      if (st.pinnedIdx < 0) hideHover(true);
    }
  }

  function clearBarHover() {
    if (st.hoverIdx === -1) return;
    st.hoverIdx = -1;
    highlightBar(st.pinnedIdx);
  }

  function onBodyMove(e) {
    const view = st.view;
    const target = e.target && e.target.closest ? e.target : null;
    if (!view || !target) return;
    const body = byId('ag-dock-body');
    const flowEl = () => body.querySelector('.ag-dk-flow[data-ag-flow-scope]');
    const ctxSecEl = () => body.querySelector('.ag-dk-sec-ctx');

    const ctxKeyEl = target.closest('[data-ag-ctx-key], [data-ag-ctx-seg]');
    const ctxFocusKey = ctxKeyEl
      ? ctxKeyEl.getAttribute('data-ag-ctx-key') || ctxKeyEl.getAttribute('data-ag-ctx-seg') || ''
      : '';
    setCtxFocus(ctxSecEl(), ctxFocusKey);

    const chartSvg = target.closest('svg[data-ag-unified]');
    if (chartSvg && st.chartMeta) {
      const hist = Array.isArray(view.history) ? view.history : [];
      const rect = chartSvg.getBoundingClientRect();
      const meta = st.chartMeta;
      if (!rect.width || !meta.slot || hist.length === 0) return;
      const x = ((e.clientX - rect.left) / rect.width) * meta.W;
      if (x < meta.x0 - meta.slot / 2) {
        if (st.hoverIdx !== -1) {
          clearBarHover();
          hideHover(false);
        }
        return;
      }
      const idx = Math.max(0, Math.min(hist.length - 1, Math.floor((x - meta.x0) / meta.slot)));
      st.hoverIdx = idx;
      highlightBar(idx);
      const html = renderDockStepHover(view, idx, st.pinnedIdx === idx);
      const anchor = chartAnchor(idx);
      if (html && anchor) showHover(html, anchor, false);
      return;
    }
    clearBarHover();

    const tseg = target.closest('.ag-dk-tseg[data-ag-turn]');
    if (tseg) {
      const html = renderDockTurnHover(view, st.turns, Number(tseg.getAttribute('data-ag-turn')) || 0);
      if (html) {
        const r = tseg.getBoundingClientRect();
        showHover(html, { top: r.top, bottom: r.bottom, x: r.left + r.width / 2 }, false);
      }
      return;
    }

    const flow = target.closest('.ag-dk-flow[data-ag-flow-scope]');
    if (flow) {
      const scope = flow.getAttribute('data-ag-flow-scope') || 'session';
      const keyEl = target.closest('[data-ag-fkey]');
      const focusKey = keyEl ? keyEl.getAttribute('data-ag-fkey') || '' : '';
      setFlowFocus(flow, focusKey);
      const html = renderDockRingHover(view, scope, focusKey);
      if (html) {
        const r = flow.getBoundingClientRect();
        const y = Math.max(r.top, Math.min(r.bottom, e.clientY));
        showHover(html, { top: y - 14, bottom: y + 14 }, false);
      }
      return;
    }

    const tipEl = target.closest('[data-ag-tip]');
    if (tipEl) {
      setFlowFocus(flowEl(), '');
      const html = renderDockContextHover(view, tipEl.getAttribute('data-ag-tip'));
      if (html) {
        const r = tipEl.getBoundingClientRect();
        showHover(html, { top: r.top, bottom: r.bottom, x: r.left + r.width / 2 }, true);
      }
      return;
    }

    setFlowFocus(flowEl(), '');
    hideHover(false);
  }

  function onBodyLeave() {
    clearBarHover();
    const body = byId('ag-dock-body');
    if (body) {
      setFlowFocus(body.querySelector('.ag-dk-flow[data-ag-flow-scope]'), '');
      setCtxFocus(body.querySelector('.ag-dk-sec-ctx'), '');
    }
    hideHover(false);
  }

  function onKeyDown(e) {
    if (e.key === 'Escape' && st.modalOpen) {
      e.preventDefault();
      setModalOpen(false);
      return;
    }
    if (e.altKey && e.shiftKey && String(e.key || '').toLowerCase() === 'm') {
      e.preventDefault();
      postToHost({ type: 'agytheme:toggle' });
      return;
    }
    if (st.pinnedIdx < 0 || !st.view) return;
    const hist = Array.isArray(st.view.history) ? st.view.history : [];
    if (e.key === 'Escape') {
      st.pinnedIdx = -1;
      render(true);
      hideHover(true);
    } else if ((e.key === 'ArrowLeft' || e.key === 'ArrowRight') && hist.length > 0) {
      e.preventDefault();
      const step = e.key === 'ArrowLeft' ? -1 : 1;
      st.pinnedIdx = Math.max(0, Math.min(hist.length - 1, st.pinnedIdx + step));
      render(true);
      revealTimelineIdx(st.pinnedIdx);
    }
  }

  function flashSection(sec) {
    if (!sec) return;
    const el = document.querySelector('#ag-telemetry-dock [data-ag-sec="' + String(sec).replace(/[^\w-]/g, '') + '"]');
    if (!el) return;
    el.classList.add('is-flash');
    setTimeout(() => el.classList.remove('is-flash'), 650);
  }

  function syncHeader() {
    const snap = st.snap;
    const view = st.view;

    const modelEl = byId('ag-dock-model');
    const modelName = (view && view.last && view.last.model) || '用量统计';
    if (modelEl && modelEl.textContent !== modelName) modelEl.textContent = modelName;

    const alertEl = byId('ag-dock-alert');
    if (alertEl) {
      const noteHtml = snap && typeof snap.noteHtml === 'string' ? snap.noteHtml : '';
      const cls = 'ag-dk-alert ag-tt-wait-note' + (snap && snap.noteClass ? ' ' + snap.noteClass : '');
      if (alertEl.className !== cls) alertEl.className = cls;
      if (alertEl.getAttribute('data-ag-note-html') !== noteHtml) {
        alertEl.setAttribute('data-ag-note-html', noteHtml);
        alertEl.innerHTML = noteHtml;
        scheduleChartPaint();
      }
    }
  }

  function render(force) {
    syncHeader();
    const body = byId('ag-dock-body');
    if (!body) return;
    if (!st.view) {
      const msg = st.cid ? '等待首次模型调用数据…' : '打开一个对话后显示用量统计';
      const emptyHtml = '<div class="ag-dk-empty-state">' + msg + '</div>';
      if (body.__agRaw !== emptyHtml) {
        body.__agRaw = emptyHtml;
        body.innerHTML = emptyHtml;
        st.chartMeta = null;
      }
      hideHover(true);
      return;
    }
    const histLen = Array.isArray(st.view.history) ? st.view.history.length : 0;
    if (st.pinnedIdx >= histLen) st.pinnedIdx = -1;
    const html = renderDockBody(st.view, {
      barMode: st.barMode,
      distScope: st.distScope,
      pinnedIdx: st.pinnedIdx,
      ctxExpanded: st.ctxExpanded
    });
    if (force || body.__agRaw !== html) {
      const prevSub = body.querySelector('.ag-dk-csub');
      const prevSubKey = prevSub ? prevSub.getAttribute('data-ag-ctx-sub') : '';
      const prevSubTop = prevSub ? prevSub.scrollTop : 0;
      body.__agRaw = html;
      body.innerHTML = html;
      if (prevSubKey && prevSubTop > 0) {
        const nextSub = body.querySelector('.ag-dk-csub[data-ag-ctx-sub="' + prevSubKey + '"]');
        if (nextSub) nextSub.scrollTop = prevSubTop;
      }
      attachChartObserver();
      paintChart();
      if (st.pinnedIdx >= 0) hideHover(false);
    } else {
      scheduleChartPaint();
    }
  }

  function applyPayload(p) {
    st.payload = p && typeof p === 'object' ? p : null;
    st.view = payloadView(st.payload);
    st.turns = st.payload && Array.isArray(st.payload.turns) ? st.payload.turns : null;
    render(false);
  }

  function fetchMetricsOnce(cid) {
    if (!cid) return;
    apiFetch('/api/metrics?cid=' + encodeURIComponent(cid), 'GET')
      .then((res) => {
        if (st.cid !== cid) return;
        if (res && res.ready && res.payload) applyPayload(res.payload);
      })
      .catch(() => {});
  }

  function closeMetricsStream() {
    if (st.metricsEs) {
      try {
        st.metricsEs.close();
      } catch (e) {}
      st.metricsEs = null;
    }
    st.metricsStreamCid = '';
  }

  function connectMetricsStream(cid) {
    if (!cid) {
      closeMetricsStream();
      return;
    }
    if (st.metricsEs && st.metricsStreamCid === cid) return;
    closeMetricsStream();
    st.metricsStreamCid = cid;
    if (typeof EventSource !== 'function') {
      fetchMetricsOnce(cid);
      return;
    }
    let url = '/api/metrics/stream?cid=' + encodeURIComponent(cid);
    if (urlToken) url += '&token=' + encodeURIComponent(urlToken);
    try {
      const es = new EventSource(url);
      st.metricsEs = es;
      es.addEventListener('metrics', (ev) => {
        if (st.cid !== cid) return;
        try {
          applyPayload(JSON.parse(ev.data));
        } catch (e) {}
      });
      es.addEventListener('ping', () => {
        postToHost({ type: 'agytheme:hb' });
      });
      es.onerror = () => {
        fetchMetricsOnce(cid);
      };
    } catch (e) {
      fetchMetricsOnce(cid);
    }
  }

  function applyState(d) {
    const cid = typeof d.cid === 'string' ? d.cid : '';
    if (cid !== st.cid) {
      st.cid = cid;
      st.payload = null;
      st.view = null;
      st.turns = null;
      st.pinnedIdx = -1;
      st.hoverIdx = -1;
      st.ctxExpanded = '';
      st.tlFromRight = 0;
      hideHover(true);
      connectMetricsStream(cid);
      render(false);
    }
    if (!st.prefsSeen && d.prefs && typeof d.prefs === 'object') {
      st.prefsSeen = true;
      if (d.prefs.barMode === 'dur' || d.prefs.barMode === 'hit' || d.prefs.barMode === 'out') st.barMode = d.prefs.barMode;
      if (d.prefs.distScope === 'turn' || d.prefs.distScope === 'session') st.distScope = d.prefs.distScope;
    }
    if (d.theme) setTheme(d.theme);
    st.snap = d.snap && typeof d.snap === 'object' ? d.snap : null;
    if (Object.prototype.hasOwnProperty.call(d, 'payload') && d.payload) {
      applyPayload(d.payload);
      return;
    }
    syncHeader();
  }

  window.addEventListener('message', (e) => {
    if (e.source !== window.parent || e.source === window) return;
    const d = e.data;
    if (!d || typeof d !== 'object') return;
    if (d.type === 'agytheme:state') {
      applyState(d);
    } else if (d.type === 'agytheme:focus') {
      setTimeout(() => flashSection(d.sec), 45);
    } else if (d.type === 'theme-change') {
      const p = d.payload && typeof d.payload === 'object' ? d.payload : d;
      const name = String(p.themeType || p.theme || '');
      if (name) setTheme(/dark/i.test(name) ? 'dark' : 'light');
    }
  });
  window.addEventListener('keydown', onKeyDown);
  window.addEventListener('resize', () => scheduleChartPaint());

  function fmtVer(v) {
    const s = String(v || '').trim().replace(/^v/i, '');
    return s ? 'v' + s : 'v--';
  }

  function setModalOpen(open) {
    st.modalOpen = Boolean(open);
    const ov = byId('ag-settings-overlay');
    if (ov) ov.classList.toggle('is-open', st.modalOpen);
    if (st.modalOpen) {
      hideHover(true);
      resetRollbackConfirm();
      loadSettings();
      loadDoctor();
    }
  }

  function resetRollbackConfirm() {
    st.rollbackConfirm = false;
    if (st.rollbackTimer) {
      clearTimeout(st.rollbackTimer);
      st.rollbackTimer = 0;
    }
    renderSettingsModal();
  }

  function renderUpdateBox() {
    const s = st.settings;
    const u = (s && s.update) || {};
    const cur = fmtVer((u.currentVersion || (s && s.version) || ''));
    const latest = fmtVer(u.latestVersion || '');
    const phase = u.phase || 'idle';
    const busy = phase === 'downloading' || phase === 'verifying' || phase === 'installing' || phase === 'restarting';
    const avail = Boolean(u.updateAvailable && u.latestVersion);

    const headerDot = byId('ag-dock-upd-dot');
    if (headerDot) headerDot.classList.toggle('is-on', avail && phase !== 'done');

    const dot = byId('ag-set-upd-dot');
    const verEl = byId('ag-set-ver');
    const hintEl = byId('ag-set-upd-hint');
    const btn = byId('ag-set-upd-btn');
    const noteEl = byId('ag-set-upd-note');
    const progEl = byId('ag-set-prog');
    const barEl = byId('ag-set-prog-bar');
    if (!verEl || !btn) return;

    if (busy) {
      dot.style.display = 'inline-block';
      verEl.textContent = avail ? cur + ' → ' + latest : cur;
      hintEl.textContent = '';
      hintEl.className = 'ag-set-hint';
      btn.className = 'ag-set-btn is-primary';
      btn.disabled = true;
      btn.textContent = phase === 'downloading' ? '下载中…' : phase === 'restarting' ? '重启中…' : '安装中…';
      noteEl.style.display = 'block';
      noteEl.textContent = u.message || '正在更新…';
      progEl.classList.add('is-on');
      barEl.style.width = Math.max(6, Math.min(100, Number(u.percent) || 10)) + '%';
      return;
    }

    progEl.classList.remove('is-on');
    barEl.style.width = '0%';

    if (avail) {
      dot.style.display = 'inline-block';
      verEl.textContent = cur + ' → ' + latest;
      hintEl.textContent = '';
      hintEl.className = 'ag-set-hint';
      btn.className = 'ag-set-btn is-primary';
      btn.disabled = false;
      btn.textContent = '更新';
      if (u.releaseNote) {
        noteEl.style.display = 'block';
        noteEl.textContent = u.releaseNote;
      } else {
        noteEl.style.display = 'none';
      }
      return;
    }

    dot.style.display = 'none';
    verEl.textContent = cur;
    btn.className = 'ag-set-btn';
    btn.disabled = phase === 'checking';
    btn.textContent = phase === 'checking' ? '检查中…' : '检查更新';

    if (u.error) {
      hintEl.textContent = u.error;
      hintEl.className = 'ag-set-hint is-err';
    } else if (phase === 'done') {
      hintEl.textContent = '已更新，重启生效';
      hintEl.className = 'ag-set-hint';
    } else if (u.checkStatus === 'no_release') {
      hintEl.textContent = '暂无发布版本';
      hintEl.className = 'ag-set-hint';
    } else if (u.checkStatus === 'up_to_date') {
      hintEl.textContent = '已是最新';
      hintEl.className = 'ag-set-hint';
    } else {
      hintEl.textContent = '';
      hintEl.className = 'ag-set-hint';
    }
    noteEl.style.display = 'none';
  }

  function renderSettingsModal() {
    renderUpdateBox();
    const s = st.settings;
    if (!s) return;
    const cfg = s.config || {};
    const feats = s.installedFeatures || {};

    document.querySelectorAll('[data-set-seg]').forEach((seg) => {
      const key = seg.getAttribute('data-set-seg');
      const cur = cfg[key];
      seg.querySelectorAll('button[data-val]').forEach((b) => {
        b.classList.toggle('is-on', b.getAttribute('data-val') === cur);
      });
    });

    document.querySelectorAll('[data-set-mod]').forEach((sw) => {
      const key = sw.getAttribute('data-set-mod');
      const installed = feats[key] !== false;
      const on = installed && Boolean(cfg.modules && cfg.modules[key]);
      sw.classList.toggle('is-on', on);
      sw.disabled = !installed;
      const tag = document.querySelector('[data-mod-tag="' + key + '"]');
      if (tag) tag.style.display = installed ? 'none' : 'inline-block';
    });

    const rbBtn = byId('ag-set-rb-btn');
    const rbSep = byId('ag-set-rb-sep');
    if (rbBtn && rbSep) {
      const show = Boolean(s.rollbackAvailable);
      rbBtn.style.display = show ? 'inline-flex' : 'none';
      rbSep.style.display = show ? 'inline' : 'none';
      rbBtn.classList.toggle('is-confirm', st.rollbackConfirm);
      if (st.rollbackConfirm) {
        rbBtn.textContent = '确认回滚？';
      } else {
        rbBtn.textContent = s.prevVersion ? '回滚 ' + fmtVer(s.prevVersion) : '回滚';
      }
    }

    const docDot = byId('ag-set-doc-dot');
    const docTxt = byId('ag-set-doc-txt');
    if (st.doctor && docDot && docTxt) {
      docDot.className = 'ag-set-dot' + (st.doctor.ok ? '' : ' is-warn');
      docTxt.textContent = st.doctor.summary || (st.doctor.ok ? '运行正常' : '状态异常');
    }
  }

  function loadSettings() {
    return apiFetch('/api/settings', 'GET')
      .then((data) => {
        st.settings = data;
        renderSettingsModal();
        const phase = data && data.update && data.update.phase;
        if (phase === 'downloading' || phase === 'verifying' || phase === 'installing' || phase === 'restarting') {
          startUpdatePoll();
        }
      })
      .catch(() => {});
  }

  function loadDoctor() {
    const docTxt = byId('ag-set-doc-txt');
    if (docTxt) docTxt.textContent = '检查中…';
    return apiFetch('/api/doctor', 'GET')
      .then((data) => {
        st.doctor = data;
        renderSettingsModal();
      })
      .catch(() => {
        st.doctor = { ok: false, summary: '服务连接中断' };
        renderSettingsModal();
      });
  }

  function saveSettingsPatch(patch) {
    return apiFetch('/api/settings', 'POST', patch)
      .then((data) => {
        st.settings = data;
        renderSettingsModal();
      })
      .catch(() => {});
  }

  function startUpdatePoll() {
    if (st.pollTimer) return;
    st.pollTimer = setInterval(() => {
      apiFetch('/api/update/status', 'GET')
        .then((u) => {
          if (!st.settings) st.settings = {};
          st.settings.update = u;
          if (u.currentVersion) st.settings.version = u.currentVersion;
          renderUpdateBox();
          if (u.phase === 'done' || u.phase === 'error' || u.phase === 'idle') {
            clearInterval(st.pollTimer);
            st.pollTimer = 0;
            if (u.phase === 'done') setTimeout(() => loadSettings(), 2200);
          }
        })
        .catch(() => {
          // Sidecar may briefly disconnect while Antigravity restarts it on the new binary.
        });
    }, 300);
  }

  function bindSettingsModal() {
    const gearBtn = byId('ag-dock-settings-btn');
    const closeBtn = byId('ag-set-close');
    const overlay = byId('ag-settings-overlay');
    const updBtn = byId('ag-set-upd-btn');
    const docBtn = byId('ag-set-doc-btn');
    const rbBtn = byId('ag-set-rb-btn');
    const dirBtn = byId('ag-set-dir-btn');

    if (gearBtn) gearBtn.addEventListener('click', () => setModalOpen(!st.modalOpen));
    if (closeBtn) closeBtn.addEventListener('click', () => setModalOpen(false));
    if (overlay) {
      overlay.addEventListener('click', (e) => {
        if (e.target === overlay) setModalOpen(false);
      });
    }

    if (updBtn) {
      updBtn.addEventListener('click', () => {
        const u = (st.settings && st.settings.update) || {};
        if (u.updateAvailable && u.latestVersion) {
          apiFetch('/api/update/apply', 'POST')
            .then((next) => {
              if (!st.settings) st.settings = {};
              st.settings.update = next;
              renderUpdateBox();
              startUpdatePoll();
            })
            .catch((err) => {
              if (!st.settings) st.settings = {};
              st.settings.update = Object.assign({}, u, { phase: 'error', error: err.message });
              renderUpdateBox();
            });
          return;
        }
        if (!st.settings) st.settings = {};
        st.settings.update = Object.assign({}, u, { phase: 'checking', error: '' });
        renderUpdateBox();
        apiFetch('/api/update/check', 'POST')
          .then((next) => {
            st.settings.update = next;
            renderUpdateBox();
          })
          .catch((err) => {
            st.settings.update = Object.assign({}, u, { phase: 'idle', error: err.message });
            renderUpdateBox();
          });
      });
    }

    document.querySelectorAll('[data-set-seg]').forEach((seg) => {
      const key = seg.getAttribute('data-set-seg');
      seg.addEventListener('click', (e) => {
        const b = e.target && e.target.closest ? e.target.closest('button[data-val]') : null;
        if (!b) return;
        const val = b.getAttribute('data-val');
        const patch = {};
        patch[key] = val;
        saveSettingsPatch(patch);
      });
    });

    document.querySelectorAll('[data-set-mod]').forEach((sw) => {
      sw.addEventListener('click', () => {
        if (sw.disabled) return;
        const key = sw.getAttribute('data-set-mod');
        const cur = Boolean(st.settings && st.settings.config && st.settings.config.modules && st.settings.config.modules[key]);
        const modules = {};
        modules[key] = !cur;
        saveSettingsPatch({ modules });
      });
    });

    if (docBtn) docBtn.addEventListener('click', () => loadDoctor());

    if (rbBtn) {
      rbBtn.addEventListener('click', () => {
        if (!st.rollbackConfirm) {
          st.rollbackConfirm = true;
          renderSettingsModal();
          st.rollbackTimer = setTimeout(() => resetRollbackConfirm(), 3500);
          return;
        }
        resetRollbackConfirm();
        rbBtn.textContent = '回滚中…';
        apiFetch('/api/rollback', 'POST')
          .then(() => {
            if (st.settings) {
              st.settings.update = Object.assign({}, st.settings.update, { phase: 'done', updateAvailable: false });
            }
            renderSettingsModal();
            setTimeout(() => loadSettings(), 2000);
          })
          .catch((err) => {
            st.doctor = { ok: false, summary: err.message || '回滚失败' };
            renderSettingsModal();
          });
      });
    }

    if (dirBtn) {
      dirBtn.addEventListener('click', () => {
        apiFetch('/api/open-dir', 'POST').catch(() => {});
      });
    }
  }

  setTheme(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  buildDom();
  if (initialCid) {
    st.cid = initialCid;
    connectMetricsStream(initialCid);
  }
  render(true);
  postToHost({ type: 'agytheme:ready' });
  window.addEventListener('beforeunload', closeMetricsStream);
  setTimeout(() => loadSettings(), 1200);
  setTimeout(() => loadSettings(), 7000);
