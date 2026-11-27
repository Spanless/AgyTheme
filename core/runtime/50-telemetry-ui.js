  try {
    for (const staleId of [
      'ag-live-metrics-pill',
      'ag-side-metrics-pill',
      'ag-live-metrics-tooltip',
      'ag-turn-metrics-tooltip',
      'ag-telemetry-dock-btn',
      'ag-telemetry-dock',
      'ag-telemetry-sash',
      'ag-telemetry-hover'
    ]) {
      const staleEl = document.getElementById(staleId);
      if (staleEl) staleEl.remove();
    }
    document.querySelectorAll('.ag-live-metrics-pill').forEach((el) => el.remove());
    document.querySelectorAll('[data-ag-dock-host]').forEach((el) => el.removeAttribute('data-ag-dock-host'));
    if (document.documentElement) {
      document.documentElement.removeAttribute('data-ag-dock-open');
      document.documentElement.style.removeProperty('--ag-dock-w');
      document.documentElement.style.removeProperty('--ag-dock-top');
    }
    if (window.__ag_dock_chart_ro) {
      try { window.__ag_dock_chart_ro.disconnect(); } catch (e) {}
      window.__ag_dock_chart_ro = null;
    }
    if (window.__ag_dock_header_ro) {
      try { window.__ag_dock_header_ro.disconnect(); } catch (e) {}
      window.__ag_dock_header_ro = null;
    }
    if (window.__ag_pane_states && typeof window.__ag_pane_states === 'object') {
      for (const k of Object.keys(window.__ag_pane_states)) {
        const ps = window.__ag_pane_states[k];
        if (ps && ps.ro) {
          try { ps.ro.disconnect(); } catch (e) {}
        }
        if (ps && ps.roRaf && typeof cancelAnimationFrame === 'function') {
          try { cancelAnimationFrame(ps.roRaf); } catch (e) {}
        }
      }
    }
    if (window.__ag_turn_metrics_observer) {
      try { window.__ag_turn_metrics_observer.disconnect(); } catch (e) {}
      window.__ag_turn_metrics_observer = null;
    }
  } catch (e) {}

  const AG_UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  const metricsByCid = (window.__ag_metrics_by_cid =
    window.__ag_metrics_by_cid && typeof window.__ag_metrics_by_cid === 'object' ? window.__ag_metrics_by_cid : {});
  const paneStates = (window.__ag_pane_states = {});

  function getOrCreatePaneState(key) {
    if (!paneStates[key]) {
      paneStates[key] = {
        cid: '',
        waitTracker: null,
        rowMode: '',
        layoutSig: '',
        ro: null,
        roObserved: null,
        roObservedLeft: null,
        roObservedRight: null,
        roRaf: 0,
        lastSnap: null
      };
    }
    return paneStates[key];
  }

  function storeMetricsPayload(cid, payload) {
    if (!cid || !payload) return;
    if (Object.prototype.hasOwnProperty.call(metricsByCid, cid)) {
      delete metricsByCid[cid];
    }
    metricsByCid[cid] = payload;
    const keys = Object.keys(metricsByCid);
    if (keys.length > 12) {
      delete metricsByCid[keys[0]];
    }
  }

  function extractCascadeId() {
    try {
      const pathStr = String(window.location && window.location.pathname ? window.location.pathname : '');
      const mRoute = pathStr.match(/\/c\/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|[0-9a-zA-Z_-]{8,})/i);
      if (mRoute && mRoute[1]) return mRoute[1];
      const mainView = document.querySelector('[data-testid="conversation-view"][data-cascade-id]');
      const viewCid = mainView ? String(mainView.getAttribute('data-cascade-id') || '').trim() : '';
      if (AG_UUID_RE.test(viewCid)) return viewCid;
      if (!document.querySelector('[data-testid="home-greeting"], [data-testid="home-splash-content"]')) {
        const selRow = document.querySelector('[data-testid="conversation-row-sidebar"][data-selected="true"]');
        const selCid = selRow ? selRow.getAttribute('data-cascade-id') || '' : '';
        if (selCid) return selCid;
      }
    } catch (e) {}
    return '';
  }

  function extractSideCascadeId(sideContent) {
    try {
      if (!sideContent) return '';
      const convEl = sideContent.querySelector('[data-testid="side-question-tab-conversation"][data-cascade-id]');
      const attrCid = convEl ? String(convEl.getAttribute('data-cascade-id') || '').trim() : '';
      if (AG_UUID_RE.test(attrCid)) return attrCid;
    } catch (e) {}
    return '';
  }

  function discoverConversationPanes() {
    const panes = [];
    try {
      const boxes = document.querySelectorAll('[id="' + SELECTORS.inputBoxId + '"]');
      let mainFound = false;
      let sideFound = false;
      for (let i = 0; i < boxes.length; i++) {
        const box = boxes[i];
        const sideContent = box.closest ? box.closest('[data-testid="side-question-tab-content"]') : null;
        const inSide = Boolean(
          sideContent || (box.closest && box.closest('[data-testid="side-question-tab-composer"]'))
        );
        if (inSide) {
          if (sideFound) continue;
          sideFound = true;
          panes.push({
            key: 'side',
            pillId: 'ag-side-metrics-pill',
            inputBox: box,
            container: sideContent || box.parentElement || document,
            cid: extractSideCascadeId(sideContent)
          });
        } else {
          if (mainFound) continue;
          mainFound = true;
          panes.push({
            key: 'main',
            pillId: 'ag-live-metrics-pill',
            inputBox: box,
            container: (box.closest && box.closest('[data-testid="conversation-view"]')) || document,
            cid: extractCascadeId()
          });
        }
      }
    } catch (e) {}
    return panes;
  }

  function getPaneByKey(paneKey) {
    const panes = discoverConversationPanes();
    for (let i = 0; i < panes.length; i++) {
      if (panes[i].key === paneKey) return panes[i];
    }
    return null;
  }

  function getWatchedCascadeIds() {
    const out = [];
    const add = (id) => {
      const s = String(id || '').trim();
      if (AG_UUID_RE.test(s) && !out.includes(s)) out.push(s);
    };
    add(extractCascadeId());
    try {
      document.querySelectorAll('[data-testid="side-question-tab-content"]').forEach((sc) => {
        add(extractSideCascadeId(sc));
      });
    } catch (e) {}
    return out;
  }

  window.__ag_get_watched_cascade_ids = getWatchedCascadeIds;

  const TT_SHOW_DELAY_MS = 85;
  const TT_HIDE_DELAY_MS = 180;
  const PILL_FIT_LEVELS = ['full', 'short', 'units', 'min'];

  function measurePillFitWidths(pill) {
    if (!document.body) return null;
    const probe = pill.cloneNode(true);
    probe.removeAttribute('id');
    // Strip row-mode so bottom-mode's full-width CSS cannot poison intrinsic width measurement.
    probe.removeAttribute('data-ag-row-mode');
    probe.removeAttribute('data-ag-fit');
    probe.setAttribute('aria-hidden', 'true');
    probe.setAttribute('data-ag-probe', '1');
    probe.style.cssText =
      'position:fixed !important;left:-10000px !important;top:0 !important;display:inline-flex !important;' +
      'visibility:hidden !important;pointer-events:none !important;width:auto !important;' +
      'min-width:0 !important;max-width:none !important;flex:none !important;margin:0 !important;';
    document.body.appendChild(probe);
    const widths = {};
    for (const lv of PILL_FIT_LEVELS) {
      probe.setAttribute('data-ag-fit', lv);
      widths[lv] = Math.ceil(probe.getBoundingClientRect().width || 0);
    }
    probe.remove();
    return widths;
  }

  function getTelemetryLayout() {
    return boot && boot.telemetryLayout === 'detailed' ? 'detailed' : 'compact';
  }

  function payloadView(m) {
    return m && m.hasData && m.view && typeof m.view === 'object' ? m.view : null;
  }

  function metricsTooltipEl() {
    return document.getElementById('ag-live-metrics-tooltip');
  }

  function isMetricsTooltipOpen(tt) {
    return Boolean(tt && tt.getAttribute('data-visible') === '1');
  }

  function clearMetricsTooltipTimers() {
    if (window.__ag_tt_show_timer) {
      clearTimeout(window.__ag_tt_show_timer);
      window.__ag_tt_show_timer = null;
    }
    if (window.__ag_tt_hide_timer) {
      clearTimeout(window.__ag_tt_hide_timer);
      window.__ag_tt_hide_timer = null;
    }
  }

  function pillAnchorEl(pill, mode) {
    if (mode === 'ctx') {
      const ring = pill.querySelector('.ag-lm-ring-btn');
      if (ring && ring.getBoundingClientRect().width > 0) return ring;
    }
    return pill.querySelector('.ag-live-metrics-text') || pill;
  }

  function positionMetricsTooltip(pill, tt, inputBox) {
    if (!pill || !tt) return;
    try {
      const anchor = pillAnchorEl(pill, tt.getAttribute('data-mode'));
      const rect = anchor.getBoundingClientRect();
      const pillRect = pill.getBoundingClientRect();
      const box = inputBox || pill.closest('[id="' + SELECTORS.inputBoxId + '"]') || document.getElementById(SELECTORS.inputBoxId);
      const boxRect = box ? box.getBoundingClientRect() : null;
      const anchorTop = pillRect.height > 0 ? pillRect.top : boxRect ? boxRect.top : 0;
      const anchorX = rect.width > 0 ? rect.left + rect.width / 2 : boxRect ? boxRect.left + boxRect.width / 2 : 0;
      const ttW = tt.offsetWidth || 352;
      const vw = window.innerWidth || document.documentElement.clientWidth || 1280;
      const vh = window.innerHeight || document.documentElement.clientHeight || 800;
      const left = Math.max(12, Math.min(Math.round(anchorX - ttW / 2), vw - ttW - 12));
      tt.style.setProperty('--ag-tt-max-h', Math.max(180, Math.round(anchorTop - 24)) + 'px');
      tt.style.setProperty('--ag-tt-arrow-x', Math.max(16, Math.min(ttW - 16, Math.round(anchorX - left))) + 'px');
      tt.style.top = 'auto';
      tt.style.bottom = Math.max(12, Math.round(vh - anchorTop + 9)) + 'px';
      tt.style.left = left + 'px';
    } catch (e) {}
  }

  function syncTooltipLiveOverlay(tt, snap, ownerPill, ownerBox) {
    if (!tt || !snap) return;
    const chipEl = tt.querySelector('.ag-tt-live-chip');
    if (chipEl) {
      if (chipEl.textContent !== snap.chipText) chipEl.textContent = snap.chipText;
      const targetChipClass = 'ag-tt-live-chip' + (snap.chipClass ? ' ' + snap.chipClass : '');
      if (chipEl.className !== targetChipClass) chipEl.className = targetChipClass;
    }
    const noteEl = tt.querySelector('.ag-tt-wait-note');
    if (noteEl) {
      const targetNoteClass = 'ag-tt-wait-note' + (snap.noteClass ? ' ' + snap.noteClass : '');
      if (noteEl.className !== targetNoteClass) noteEl.className = targetNoteClass;
      if (noteEl.getAttribute('data-ag-note-html') !== snap.noteHtml) {
        noteEl.setAttribute('data-ag-note-html', snap.noteHtml);
        noteEl.innerHTML = snap.noteHtml;
        if (ownerPill && isMetricsTooltipOpen(tt)) {
          positionMetricsTooltip(ownerPill, tt, ownerBox);
        }
      }
    }
  }

  function ensureMetricsTooltipEl() {
    let tt = metricsTooltipEl();
    if (!tt && document.body) {
      tt = document.createElement('div');
      tt.id = 'ag-live-metrics-tooltip';
      tt.setAttribute('data-mode', 'detail');
      if (!SIDE_PANE) tt.setAttribute('data-ag-no-pane', '1');
      document.body.appendChild(tt);
      tt.addEventListener('mouseenter', () => {
        if (window.__ag_tt_hide_timer) {
          clearTimeout(window.__ag_tt_hide_timer);
          window.__ag_tt_hide_timer = null;
        }
      });
      tt.addEventListener('mouseleave', () => {
        scheduleHideMetricsTooltip();
      });
      tt.addEventListener('click', (e) => {
        const openBtn = e.target && e.target.closest ? e.target.closest('[data-ag-open-dock]') : null;
        if (!openBtn) return;
        e.stopPropagation();
        const sec = openBtn.getAttribute('data-ag-open-dock') || 'timeline';
        const ownerPane = tt.getAttribute('data-ag-owner-pane') || 'main';
        hideMetricsTooltip();
        showUsageInPane(ownerPane, sec);
      });
    }
    return tt;
  }

  function renderMetricsTooltipContent(tt, view, mode) {
    const html = mode === 'ctx' ? renderContextPanel(view) : renderDetailPanel(view);
    if (tt.__agRawMode === mode && tt.__agRaw === html) return false;
    tt.__agRaw = html;
    tt.__agRawMode = mode;
    tt.innerHTML = html;
    return true;
  }

  function hydrateTooltipForPane(paneKey, mode) {
    const tt = ensureMetricsTooltipEl();
    if (!tt) return null;
    const pane = getPaneByKey(paneKey);
    const cid = pane ? pane.cid : paneKey === 'main' ? extractCascadeId() : '';
    const view = payloadView(cid ? metricsByCid[cid] : null);
    if (!view) return null;
    const nextMode = mode === 'ctx' ? 'ctx' : 'detail';
    if (tt.getAttribute('data-mode') !== nextMode) {
      tt.setAttribute('data-mode', nextMode);
    }
    window.__ag_tt_owner_pane = paneKey;
    tt.setAttribute('data-ag-owner-pane', paneKey);
    tt.__agView = view;
    renderMetricsTooltipContent(tt, view, nextMode);
    const ps = paneStates[paneKey];
    if (ps && ps.lastSnap) syncTooltipLiveOverlay(tt, ps.lastSnap, null, null);
    const pill = document.getElementById(pane ? pane.pillId : 'ag-live-metrics-pill');
    return { tt, pane, pill };
  }

  function markOpenZone(paneKey, mode) {
    document.querySelectorAll('.ag-live-metrics-pill').forEach((p) => {
      const want = mode && p.getAttribute('data-ag-pane') === paneKey ? mode : '';
      if ((p.getAttribute('data-ag-open') || '') === want) return;
      if (want) p.setAttribute('data-ag-open', want);
      else p.removeAttribute('data-ag-open');
    });
  }

  function showMetricsTooltip(paneKey, mode, pin) {
    const res = hydrateTooltipForPane(paneKey, mode);
    if (!res) return false;
    window.__ag_tt_pinned = Boolean(pin);
    if (pin) res.tt.setAttribute('data-pinned', '1');
    else res.tt.removeAttribute('data-pinned');
    if (res.pill) positionMetricsTooltip(res.pill, res.tt, res.pane ? res.pane.inputBox : null);
    res.tt.setAttribute('data-visible', '1');
    markOpenZone(paneKey, res.tt.getAttribute('data-mode'));
    return true;
  }

  function hideMetricsTooltip() {
    clearMetricsTooltipTimers();
    window.__ag_tt_pinned = false;
    const tt = metricsTooltipEl();
    if (tt) {
      tt.removeAttribute('data-visible');
      tt.removeAttribute('data-pinned');
    }
    markOpenZone('', '');
  }

  function scheduleShowMetricsTooltip(paneKey, mode) {
    if (window.__ag_tt_hide_timer) {
      clearTimeout(window.__ag_tt_hide_timer);
      window.__ag_tt_hide_timer = null;
    }
    if (window.__ag_tt_pinned) return;
    if (window.__ag_tt_show_timer) {
      clearTimeout(window.__ag_tt_show_timer);
      window.__ag_tt_show_timer = null;
    }
    if (isMetricsTooltipOpen(metricsTooltipEl()) && (window.__ag_tt_owner_pane || 'main') === paneKey) {
      showMetricsTooltip(paneKey, mode, false);
      return;
    }
    window.__ag_tt_show_timer = setTimeout(() => {
      window.__ag_tt_show_timer = null;
      if (!window.__ag_tt_pinned) showMetricsTooltip(paneKey, mode, false);
    }, TT_SHOW_DELAY_MS);
  }

  function scheduleHideMetricsTooltip() {
    if (window.__ag_tt_show_timer) {
      clearTimeout(window.__ag_tt_show_timer);
      window.__ag_tt_show_timer = null;
    }
    if (window.__ag_tt_pinned) return;
    if (window.__ag_tt_hide_timer) clearTimeout(window.__ag_tt_hide_timer);
    window.__ag_tt_hide_timer = setTimeout(() => {
      window.__ag_tt_hide_timer = null;
      if (!window.__ag_tt_pinned) hideMetricsTooltip();
    }, TT_HIDE_DELAY_MS);
  }

  const SIDE_PANE = Boolean(boot && boot.sidePane === true);
  const PANE_SIDECAR_ID = 'agytheme/agytheme';
  const PANE_PATH = '/';
  const PANE_TITLE = '用量统计';
  const PANE_TAB_ID = 'sidecar__' + PANE_SIDECAR_ID + '__' + PANE_PATH;
  const PANE_FRAME_SELECTOR = 'iframe[src^="plugin://agytheme--agytheme-"]';
  const PANE_FOCUS_TTL_MS = 8000;
  const DOCK_KEY_MODE = 'ag_telemetry_dock_mode';
  const DOCK_KEY_SCOPE = 'ag_telemetry_dock_scope';

  const nativePane = {
    store: null,
    showCid: {},
    sent: new WeakMap(),
    ready: new WeakSet(),
    pendingFocus: null
  };

  function dockStorageGet(key) {
    try {
      return typeof localStorage !== 'undefined' ? localStorage.getItem(key) : null;
    } catch (e) {
      return null;
    }
  }

  function dockStorageSet(key, val) {
    try {
      if (typeof localStorage !== 'undefined') localStorage.setItem(key, String(val));
    } catch (e) {}
  }

  function panePrefs() {
    const mode = dockStorageGet(DOCK_KEY_MODE);
    return {
      barMode: mode === 'hit' || mode === 'out' ? mode : 'dur',
      distScope: dockStorageGet(DOCK_KEY_SCOPE) === 'turn' ? 'turn' : 'session'
    };
  }

  function dockButtonEl() {
    return document.getElementById('ag-telemetry-dock-btn');
  }

  function hostStore() {
    const cached = nativePane.store;
    if (cached) return cached;
    try {
      const root = document.getElementById('root') || (document.body && document.body.firstElementChild);
      const key = root ? Object.keys(root).find((k) => k.startsWith('__reactContainer$') || k.startsWith('__reactFiber$')) : '';
      const stack = key ? [root[key]] : [];
      for (let n = 0; stack.length > 0 && n < 6000; n++) {
        const f = stack.pop();
        if (!f) continue;
        const p = f.memoizedProps;
        if (p && p.store && typeof p.store.dispatch === 'function' && typeof p.store.getState === 'function') {
          const s = p.store.getState();
          if (s && s.sidePanelV2 && s.sidePanel) {
            nativePane.store = p.store;
            return p.store;
          }
        }
        if (f.sibling) stack.push(f.sibling);
        if (f.child) stack.push(f.child);
      }
    } catch (e) {}
    return null;
  }

  function panelScopeOf(cid) {
    return { kind: 'cascadeId', cascadeId: cid };
  }

  function nativePaneState(cid) {
    const store = cid ? hostStore() : null;
    if (!store) return { open: false, ours: false };
    try {
      const s = store.getState();
      const v1 = s.sidePanel.conversationPanes && s.sidePanel.conversationPanes[cid];
      const v2 = s.sidePanelV2.conversationPanes && s.sidePanelV2.conversationPanes[cid];
      return { open: Boolean(v1 && v1.isPaneOpen), ours: Boolean(v2 && v2.activeTabId === PANE_TAB_ID) };
    } catch (e) {
      return { open: false, ours: false };
    }
  }

  function dispatchToHost(action) {
    const store = hostStore();
    if (!store) return false;
    try {
      store.dispatch(action);
      return true;
    } catch (e) {
      return false;
    }
  }

  function hostThemeMode() {
    const attr = document.documentElement ? document.documentElement.getAttribute('data-ag-theme-mode') : '';
    if (attr === 'dark' || attr === 'light') return attr;
    return document.body && document.body.classList.contains('dark') ? 'dark' : 'light';
  }

  function paneFrames() {
    const out = [];
    try {
      document.querySelectorAll(PANE_FRAME_SELECTOR).forEach((f) => {
        let cid = '';
        try {
          cid = new URL(f.src).searchParams.get('conversationId') || '';
        } catch (e) {}
        if (f.contentWindow) out.push({ win: f.contentWindow, cid });
      });
    } catch (e) {}
    return out;
  }

  function snapForCid(cid) {
    for (const k of Object.keys(paneStates)) {
      const ps = paneStates[k];
      if (ps && ps.cid === cid && ps.lastSnap) {
        const s = ps.lastSnap;
        return {
          visualState: s.visualState,
          isExecuting: Boolean(s.isExecuting),
          chipText: s.chipText || '',
          noteHtml: s.noteHtml || '',
          noteClass: s.noteClass || ''
        };
      }
    }
    return null;
  }

  function postPaneState(frame) {
    const shown = (frame.cid && nativePane.showCid[frame.cid]) || frame.cid;
    const msg = { type: 'agytheme:state', cid: shown, snap: snapForCid(shown), theme: hostThemeMode(), prefs: panePrefs() };
    try {
      frame.win.postMessage(msg, '*');
    } catch (e) {}
  }

  function postPaneStates() {
    if (!SIDE_PANE) return;
    const frames = paneFrames();
    for (let i = 0; i < frames.length; i++) postPaneState(frames[i]);
  }

  function flushPendingFocus(frame) {
    const pf = nativePane.pendingFocus;
    if (!pf || pf.cid !== frame.cid) return;
    nativePane.pendingFocus = null;
    if (Date.now() - pf.at > PANE_FOCUS_TTL_MS) return;
    try {
      frame.win.postMessage({ type: 'agytheme:focus', sec: pf.sec }, '*');
    } catch (e) {}
  }

  function showUsageInPane(paneKey, focusSec) {
    const mainCid = extractCascadeId();
    if (!SIDE_PANE || !mainCid) return false;
    const side = paneKey === 'side' ? getPaneByKey('side') : null;
    if (side && side.cid && side.cid !== mainCid) nativePane.showCid[mainCid] = side.cid;
    else delete nativePane.showCid[mainCid];

    const panelScope = panelScopeOf(mainCid);
    const opened =
      dispatchToHost({
        type: 'sidePanelV2/setContent',
        payload: {
          panelScope,
          content: { type: 'sidecarView', sidecarView: { sidecarId: PANE_SIDECAR_ID, path: PANE_PATH, url: '', title: PANE_TITLE } },
          keepOpen: true
        }
      }) && dispatchToHost({ type: 'sidePanel/setSidePanelOpen', payload: { panelScope, isOpen: true } });
    if (!opened) return false;

    nativePane.pendingFocus = focusSec ? { cid: mainCid, sec: focusSec, at: Date.now() } : null;
    const frame = paneFrames().find((f) => f.cid === mainCid);
    if (frame) {
      postPaneState(frame, true);
      setTimeout(() => flushPendingFocus(frame), 60);
    }
    syncDockToggleButton();
    return true;
  }

  function toggleUsagePane() {
    const mainCid = extractCascadeId();
    if (!SIDE_PANE || !mainCid) return;
    const st = nativePaneState(mainCid);
    if (st.open && st.ours) {
      dispatchToHost({ type: 'sidePanel/setSidePanelOpen', payload: { panelScope: panelScopeOf(mainCid), isOpen: false } });
      syncDockToggleButton();
    } else {
      showUsageInPane('main', '');
    }
  }

  const _onPaneMessage = (e) => {
    const d = e && e.data;
    if (!d || typeof d !== 'object' || typeof d.type !== 'string' || d.type.indexOf('agytheme:') !== 0) return;
    const frame = paneFrames().find((f) => f.win === e.source);
    if (!frame) return;
    if (d.type === 'agytheme:ready') {
      nativePane.ready.add(frame.win);
      postPaneState(frame);
      flushPendingFocus(frame);
    } else if (d.type === 'agytheme:hb') {
      window.__ag_last_daemon_hb = Date.now();
    } else if (d.type === 'agytheme:prefs') {
      if (d.barMode === 'dur' || d.barMode === 'hit' || d.barMode === 'out') dockStorageSet(DOCK_KEY_MODE, d.barMode);
      if (d.distScope === 'turn' || d.distScope === 'session') dockStorageSet(DOCK_KEY_SCOPE, d.distScope);
    } else if (d.type === 'agytheme:toggle') {
      toggleUsagePane();
    }
  };

  function findTopBarContext() {
    try {
      const vw = window.innerWidth || document.documentElement.clientWidth || 1280;
      const pickRightBtnIn = (container, hRect) => {
        const btns = container.querySelectorAll('button');
        let firstRight = null;
        let firstRightLeft = Infinity;
        for (let i = 0; i < btns.length; i++) {
          const b = btns[i];
          if (b.id === 'ag-telemetry-dock-btn') continue;
          const r = b.getBoundingClientRect();
          if (r.width < 16 || r.width > 56 || r.height < 16 || r.height > 44) continue;
          if (r.left > hRect.left + hRect.width * 0.5 && r.left < firstRightLeft) {
            firstRightLeft = r.left;
            firstRight = b;
          }
        }
        return firstRight;
      };

      let header = null;
      const crumb = document.querySelector('[data-testid="breadcrumb-segment"]');
      if (crumb && crumb.closest) {
        header = crumb.closest('header');
        if (!header) {
          for (let cur = crumb.parentElement; cur && cur !== document.body; cur = cur.parentElement) {
            const r = cur.getBoundingClientRect();
            if (r.top >= -2 && r.top <= 64 && r.height >= 26 && r.height <= 64 && r.width >= 260) {
              if (pickRightBtnIn(cur, r)) {
                header = cur;
                break;
              }
            }
          }
        }
      }
      if (!header) {
        const headers = document.querySelectorAll('header');
        for (let i = 0; i < headers.length; i++) {
          const r = headers[i].getBoundingClientRect();
          if (r.top >= -2 && r.top <= 64 && r.height >= 26 && r.height <= 64 && r.width >= 260) {
            header = headers[i];
            break;
          }
        }
      }
      if (header) {
        const hRect = header.getBoundingClientRect();
        const firstRight = pickRightBtnIn(header, hRect);
        if (firstRight) return { header, headerRect: hRect, firstRight };
      }

      const allBtns = document.querySelectorAll('button');
      let firstRight = null;
      let firstRightLeft = Infinity;
      for (let i = 0; i < allBtns.length; i++) {
        const b = allBtns[i];
        if (b.id === 'ag-telemetry-dock-btn') continue;
        const r = b.getBoundingClientRect();
        if (r.width < 16 || r.width > 48 || r.height < 16 || r.height > 44) continue;
        if (r.top >= 0 && r.bottom <= 68 && r.right >= vw - 180 && r.left > vw * 0.55) {
          if (r.left < firstRightLeft) {
            firstRightLeft = r.left;
            firstRight = b;
          }
        }
      }
      if (firstRight) {
        let bar = firstRight.parentElement;
        for (let cur = bar; cur && cur !== document.body; cur = cur.parentElement) {
          const r = cur.getBoundingClientRect();
          if (r.height >= 26 && r.height <= 68 && r.width >= 260) {
            bar = cur;
            break;
          }
        }
        return { header: bar, headerRect: bar ? bar.getBoundingClientRect() : firstRight.getBoundingClientRect(), firstRight };
      }
      return header ? { header, headerRect: header.getBoundingClientRect(), firstRight: null } : null;
    } catch (e) {
      return null;
    }
  }

  function ensureDockToggleButton(visible) {
    let btn = dockButtonEl();
    if (!visible) {
      if (btn && btn.style.display !== 'none') btn.style.display = 'none';
      return null;
    }
    if (!btn && document.body) {
      btn = document.createElement('button');
      btn.id = 'ag-telemetry-dock-btn';
      btn.type = 'button';
      btn.setAttribute('aria-label', PANE_TITLE);
      btn.setAttribute('title', PANE_TITLE + ' (Alt+Shift+M)');
      btn.innerHTML =
        '<svg viewBox="0 0 16 16" width="15" height="15" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' +
        '<path d="M1.8 9.8h2.3l1.6-4.8 2.4 6.6 1.8-4.2h4.3"/>' +
        '</svg><span class="ag-dk-bdot"></span>';
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        toggleUsagePane();
      });
    }
    if (!btn) return null;

    const tb = findTopBarContext();
    if (tb && tb.firstRight && tb.firstRight.parentElement) {
      const parent = tb.firstRight.parentElement;
      if (btn.parentElement !== parent || btn.nextElementSibling !== tb.firstRight) {
        parent.insertBefore(btn, tb.firstRight);
      }
      if (btn.hasAttribute('data-ag-floating')) btn.removeAttribute('data-ag-floating');
    } else if (document.body) {
      if (btn.parentElement !== document.body) document.body.appendChild(btn);
      btn.setAttribute('data-ag-floating', '1');
    }
    if (btn.style.display !== 'inline-flex') btn.style.display = 'inline-flex';
    return btn;
  }

  function syncDockToggleButton() {
    const mainCid = extractCascadeId();
    const btn = ensureDockToggleButton(Boolean(SIDE_PANE && MODULES.metrics && mainCid));
    if (!btn) return;
    const st = nativePaneState(mainCid);
    btn.classList.toggle('is-active', st.open && st.ours);

    const ps = paneStates.main;
    const snap = ps && ps.cid === mainCid ? ps.lastSnap : null;
    const view = payloadView(metricsByCid[mainCid]);
    const lvl = view && view.ctx ? tmLevel(view.ctx) : 'ok';
    const running = Boolean(snap && (snap.isExecuting || snap.visualState === 'running'));
    const slow = Boolean(snap && snap.visualState === 'slow');
    const dot =
      snap && snap.visualState === 'error'
        ? 'crit'
        : slow || lvl === 'warn'
        ? 'warn'
        : lvl === 'crit'
        ? 'crit'
        : lvl === 'compacted'
        ? 'compacted'
        : running
        ? 'running'
        : '';
    if (dot) {
      if (btn.getAttribute('data-dot') !== dot) btn.setAttribute('data-dot', dot);
    } else if (btn.hasAttribute('data-dot')) {
      btn.removeAttribute('data-dot');
    }
  }

  function setPillContent(pill, bodyHtml, ringHtml, ringLabel, level) {
    const body = pill.querySelector('.ag-lm-body');
    if (body && body.__agRaw !== bodyHtml) {
      body.__agRaw = bodyHtml;
      body.innerHTML = bodyHtml;
    }
    const ring = pill.querySelector('.ag-lm-ring-btn');
    if (ring) {
      const disp = ringHtml ? '' : 'none';
      if (ring.style.display !== disp) ring.style.display = disp;
      if (ringHtml && ring.__agRaw !== ringHtml) {
        ring.__agRaw = ringHtml;
        ring.innerHTML = ringHtml;
      }
      if ((ring.getAttribute('aria-label') || '') !== ringLabel) ring.setAttribute('aria-label', ringLabel);
    }
    if (pill.getAttribute('data-level') !== level) pill.setAttribute('data-level', level);
  }

  function paintPill(pill, m, fallbackLabel) {
    const view = payloadView(m);
    if (view) {
      const ctx = view.ctx || {};
      setPillContent(pill, renderPillMetrics(view, getTelemetryLayout()), renderContextRing(ctx), contextRingLabel(ctx), tmLevel(ctx));
      if (!pill.hasAttribute('data-ag-has-data')) pill.setAttribute('data-ag-has-data', '1');
      return true;
    }
    const statusHtml = fallbackLabel ? '<span class="ag-lm-status">' + escapeStatusHtml(fallbackLabel) + '</span>' : '';
    setPillContent(pill, statusHtml, '', '', 'ok');
    if (pill.hasAttribute('data-ag-has-data')) pill.removeAttribute('data-ag-has-data');
    return false;
  }

  function findInputToolbarContext(inputBox) {
    if (!inputBox) return null;
    const modelTrigger = inputBox.querySelector('[data-testid="model-selector-trigger"]');
    if (!modelTrigger) return null;
    const isOurs = (el) => !!el && (el.classList.contains('ag-live-metrics-pill') || el.hasAttribute('data-ag-probe'));
    const groupsOf = (row) => {
      const kids = Array.prototype.filter.call(row.children, (el) => !isOurs(el));
      if (kids.length < 2) return null;
      const leftGroup = kids[0];
      const rightGroup = kids[kids.length - 1];
      if (!leftGroup.contains(modelTrigger)) return null;
      return { row, leftGroup, rightGroup };
    };

    const footer = modelTrigger.closest('.agy-composer-footer');
    if (footer && inputBox.contains(footer)) {
      const ctx = groupsOf(footer);
      if (ctx) return ctx;
    }
    let row = modelTrigger.parentElement;
    for (let i = 0; i < 8 && row && row !== inputBox; i++, row = row.parentElement) {
      const cs = getComputedStyle(row);
      if (!/flex/.test(cs.display) || /column/.test(cs.flexDirection)) continue;
      const ctx = groupsOf(row);
      if (ctx) return ctx;
    }
    return null;
  }

  function zoneOf(target) {
    const el = target && target.closest ? target.closest('[data-ag-zone]') : null;
    return el ? el.getAttribute('data-ag-zone') || '' : '';
  }

  function ensureMetricsPillForPane(pane, forceLayout = false, resetHysteresis = false) {
    if (!pane || !pane.inputBox) return null;
    const { key: paneKey, pillId, inputBox, cid } = pane;
    const ctx = findInputToolbarContext(inputBox);
    if (!ctx) return null;
    const { row, leftGroup, rightGroup } = ctx;
    const ps = getOrCreatePaneState(paneKey);

    let pill = document.getElementById(pillId);
    if (!pill) {
      pill = document.createElement('div');
      pill.id = pillId;
      pill.className = 'ag-live-metrics-pill';
      pill.setAttribute('data-ag-pane', paneKey);
      const textSpanId = paneKey === 'main' ? ' id="ag-live-metrics-text"' : '';
      pill.innerHTML =
        '<span' +
        textSpanId +
        ' class="ag-live-metrics-text" data-ag-zone="detail"><span class="ag-lm-state"></span><span class="ag-lm-body"></span></span>' +
        '<span class="ag-lm-ring-btn" data-ag-zone="ctx" role="img" style="display:none"></span>';
      paintPill(pill, cid ? metricsByCid[cid] : null, '');

      const onPointer = (e) => {
        const zone = zoneOf(e.target);
        if (!zone) return;
        const tt = metricsTooltipEl();
        const ownOpen = isMetricsTooltipOpen(tt) && (window.__ag_tt_owner_pane || 'main') === paneKey;
        if (ownOpen && tt.getAttribute('data-mode') === zone) {
          if (window.__ag_tt_hide_timer) {
            clearTimeout(window.__ag_tt_hide_timer);
            window.__ag_tt_hide_timer = null;
          }
          return;
        }
        if (!ownOpen && pill.__agZone === zone && window.__ag_tt_show_timer) return;
        pill.__agZone = zone;
        scheduleShowMetricsTooltip(paneKey, zone);
      };
      pill.addEventListener('mouseover', onPointer);
      pill.addEventListener('mousemove', onPointer);

      pill.addEventListener('mouseleave', () => {
        pill.__agZone = '';
        scheduleHideMetricsTooltip();
      });

      pill.addEventListener('click', (e) => {
        e.stopPropagation();
        clearMetricsTooltipTimers();
        const zone = zoneOf(e.target) || 'detail';
        if (SIDE_PANE) {
          hideMetricsTooltip();
          showUsageInPane(paneKey, zone === 'ctx' ? 'ctx' : 'timeline');
        } else {
          showMetricsTooltip(paneKey, zone, true);
        }
      });
    }

    const rowWidth = Math.round(row.getBoundingClientRect().width || 0);
    if (resetHysteresis) ps.rowMode = '';
    const prevMode = ps.rowMode || '';

    const measureIntrinsicWidth = (el, depth) => {
      if (!el || el === pill) return 0;
      let cs = null;
      try { cs = getComputedStyle(el); } catch (e) {}
      if (cs && cs.display === 'none') return 0;
      const kids = el.children;
      const grows = cs && (parseFloat(cs.flexGrow || '0') || 0) > 0;
      // Unwrap flex-growing containers so expanded empty space is not miscounted as occupied toolbar width.
      if (depth < 4 && (depth === 0 || grows) && kids && kids.length > 0) {
        let w = 0;
        let visible = 0;
        for (let i = 0; i < kids.length; i++) {
          const ch = kids[i];
          if (ch === pill) continue;
          const cw = measureIntrinsicWidth(ch, depth + 1);
          if (cw > 0) {
            w += cw;
            visible++;
          }
        }
        if (cs) {
          const gap = parseFloat(cs.columnGap || cs.gap || '0') || 0;
          const pad = (parseFloat(cs.paddingLeft || '0') || 0) + (parseFloat(cs.paddingRight || '0') || 0);
          if (visible > 1 && gap > 0) w += Math.ceil(gap * (visible - 1));
          if (pad > 0) w += Math.ceil(pad);
        }
        return w;
      }
      return Math.ceil((el.getBoundingClientRect && el.getBoundingClientRect().width) || 0);
    };
    const leftUsed = measureIntrinsicWidth(leftGroup, 0);
    const rightUsed = measureIntrinsicWidth(rightGroup, 0);

    const stateEl = pill.querySelector('.ag-lm-state');
    const body = pill.querySelector('.ag-lm-body');
    const ringEl = pill.querySelector('.ag-lm-ring-btn');
    const ringShown = ringEl && ringEl.style && ringEl.style.display !== 'none' ? '1' : '0';
    const bodyRaw = body ? body.__agRaw || body.textContent : '';
    const stateRaw = stateEl ? stateEl.textContent : '';
    const contentSig = bodyRaw + '|' + stateRaw + '|' + ringShown;
    if (ps.fitSig !== contentSig || !ps.fitWidths) {
      const measured = measurePillFitWidths(pill);
      if (measured) {
        ps.fitWidths = measured;
        const hasVisibleContent = Boolean(bodyRaw || stateRaw || ringShown === '1');
        const minW = measured[PILL_FIT_LEVELS[PILL_FIT_LEVELS.length - 1]] || 0;
        ps.fitSig = !hasVisibleContent || minW > 0 ? contentSig : '';
      }
    }
    const avail = rowWidth - leftUsed - rightUsed - 24;
    const curIdx = prevMode === 'bottom' ? PILL_FIT_LEVELS.length : PILL_FIT_LEVELS.indexOf(prevMode);
    let mode = 'bottom';
    if (rowWidth <= 0 || !ps.fitWidths || !row.parentElement) {
      mode = 'full';
    } else {
      for (let i = 0; i < PILL_FIT_LEVELS.length; i++) {
        const slack = curIdx >= 0 && i < curIdx ? 16 : 0;
        if (ps.fitWidths[PILL_FIT_LEVELS[i]] + slack <= avail) {
          mode = PILL_FIT_LEVELS[i];
          break;
        }
      }
    }
    const below = mode === 'bottom';
    let fit = mode;
    if (below) {
      fit = PILL_FIT_LEVELS[PILL_FIT_LEVELS.length - 1];
      for (const lv of PILL_FIT_LEVELS) {
        if (ps.fitWidths[lv] <= rowWidth - 16) {
          fit = lv;
          break;
        }
      }
    }
    const rowMode = below ? 'bottom' : 'middle';
    const sig = mode + ':' + fit + ':' + Math.round(rowWidth / 8) + ':' + Math.round(avail / 8);
    const rowAttrMismatch = !below && row.getAttribute('data-ag-row-mode') !== 'middle';
    const pillAttrMismatch = pill.getAttribute('data-ag-row-mode') !== rowMode || pill.getAttribute('data-ag-fit') !== fit;

    if (forceLayout || ps.layoutSig !== sig || rowAttrMismatch || pillAttrMismatch || !pill.isConnected || !inputBox.contains(pill)) {
      ps.rowMode = mode;
      ps.layoutSig = sig;
      inputBox.querySelectorAll('[data-ag-row-mode]').forEach((el) => {
        if (el !== pill && (below || el !== row)) el.removeAttribute('data-ag-row-mode');
      });
      if (!below) row.setAttribute('data-ag-row-mode', 'middle');
      pill.setAttribute('data-ag-row-mode', rowMode);
      pill.setAttribute('data-ag-fit', fit);
    }
    if (below) {
      if (pill.parentElement !== row.parentElement || row.nextElementSibling !== pill) {
        row.parentElement.insertBefore(pill, row.nextSibling);
      }
    } else if (pill.parentElement !== row || pill.nextElementSibling !== rightGroup) {
      row.insertBefore(pill, rightGroup);
    }

    const needReobserve =
      ps.roObserved !== row || ps.roObservedLeft !== leftGroup || ps.roObservedRight !== rightGroup;
    if (needReobserve && typeof ResizeObserver !== 'undefined') {
      if (ps.ro) {
        try { ps.ro.disconnect(); } catch (e) {}
      }
      if (ps.roRaf && typeof cancelAnimationFrame === 'function') {
        try { cancelAnimationFrame(ps.roRaf); } catch (e) {}
        ps.roRaf = 0;
      }
      ps.roObserved = row;
      ps.roObservedLeft = leftGroup;
      ps.roObservedRight = rightGroup;
      const runRelayout = () => {
        ps.roRaf = 0;
        const curPane = getPaneByKey(paneKey);
        if (!curPane) return;
        const p = document.getElementById(pillId);
        if (!p || p.style.display === 'none') return;
        ensureMetricsPillForPane(curPane, false, false);
        const tt = metricsTooltipEl();
        if (tt && isMetricsTooltipOpen(tt) && (window.__ag_tt_owner_pane || 'main') === paneKey) {
          positionMetricsTooltip(p, tt, curPane.inputBox);
        }
      };
      ps.ro = new ResizeObserver(() => {
        if (typeof requestAnimationFrame === 'function') {
          if (ps.roRaf) return;
          ps.roRaf = requestAnimationFrame(runRelayout);
        } else {
          runRelayout();
        }
      });
      try { ps.ro.observe(inputBox); } catch (e) {}
      try { ps.ro.observe(row); } catch (e) {}
      try { ps.ro.observe(leftGroup); } catch (e) {}
      try { ps.ro.observe(rightGroup); } catch (e) {}
    }

    return pill;
  }

  function pruneStalePills(activePanes) {
    const activeKeys = new Set(activePanes.map((p) => p.key));
    for (const [k, pid] of [['main', 'ag-live-metrics-pill'], ['side', 'ag-side-metrics-pill']]) {
      if (activeKeys.has(k)) continue;
      const stale = document.getElementById(pid);
      if (stale) stale.remove();
      const ps = paneStates[k];
      if (ps) {
        if (ps.ro) {
          try { ps.ro.disconnect(); } catch (e) {}
          ps.ro = null;
          ps.roObserved = null;
          ps.roObservedLeft = null;
          ps.roObservedRight = null;
        }
        if (ps.roRaf && typeof cancelAnimationFrame === 'function') {
          try { cancelAnimationFrame(ps.roRaf); } catch (e) {}
          ps.roRaf = 0;
        }
        ps.cid = '';
        ps.layoutSig = '';
        ps.rowMode = '';
      }
      if ((window.__ag_tt_owner_pane || '') === k) {
        hideMetricsTooltip();
      }
    }
  }

  function ensureMetricsPill(forceLayout = false, resetHysteresis = false) {
    const panes = discoverConversationPanes();
    pruneStalePills(panes);
    let mainPill = null;
    for (let i = 0; i < panes.length; i++) {
      const pill = ensureMetricsPillForPane(panes[i], forceLayout, resetHysteresis);
      if (panes[i].key === 'main') mainPill = pill;
    }
    return mainPill;
  }

  function applyPendingWaitVisualsForPane(pane, now) {
    const { key: paneKey, pillId, inputBox, container, cid: curCid } = pane;
    const ps = getOrCreatePaneState(paneKey);
    const m = curCid && metricsByCid[curCid] ? metricsByCid[curCid] : null;
    const st = m && m.status ? m.status : null;
    const live =
      typeof readLiveCascadeRuntimeState === 'function'
        ? readLiveCascadeRuntimeState(curCid, inputBox, container)
        : null;

    ps.cid = curCid;
    const activity = deriveActivity({ st, live });
    const liveMatched = Boolean(live && live.matched);
    ps.waitTracker = advanceWaitTracker(ps.waitTracker, {
      now,
      cid: curCid,
      activity,
      stepCounts: [
        st && typeof st.totalSteps === 'number' ? st.totalSteps : 0,
        st && typeof st.totalGenerations === 'number' ? st.totalGenerations : 0,
        liveMatched && typeof live.totalStepsLen === 'number' ? live.totalStepsLen : 0
      ],
      contentLen:
        (liveMatched ? Number(live.activeProgressLen) || 0 : 0) +
        (typeof readActiveStreamProgressLen === 'function' ? readActiveStreamProgressLen(container) : 0),
      unconnected: Boolean(
        curCid && liveMatched && live.hasProvider && Number(live.aspStatus) === 0 && !live.hasTrajectory
      )
    });
    const { waitSec, unconnSec, isStreaming } = ps.waitTracker;
    const snap = computeStatusSnapshot({
      now,
      st,
      live,
      waitSec,
      unconnSec,
      lastHb: window.__ag_last_daemon_hb || 0,
      visibleSince: window.__ag_visible_since || 0,
      documentHidden: Boolean(document.hidden),
      navigatorOnline: typeof navigator === 'undefined' ? true : navigator.onLine !== false,
      hasConversation: Boolean(curCid),
      curModel: typeof getSelectedModelLabel === 'function' ? getSelectedModelLabel(inputBox) : '',
      isStreaming
    });
    ps.lastSnap = snap;

    const hasData = Boolean(payloadView(m));
    const hasActiveStatus = Boolean(
      snap.visualState === 'error' || snap.visualState === 'slow' || snap.isExecuting
    );
    const showPill = Boolean(MODULES.metrics && (hasData || hasActiveStatus));

    let pill = document.getElementById(pillId);
    if (showPill && !pill) {
      pill = ensureMetricsPillForPane(pane, false, false);
    }

    if (pill) {
      const wasHidden = pill.style.display !== 'inline-flex';
      const disp = showPill ? 'inline-flex' : 'none';
      if (pill.style.display !== disp) pill.style.display = disp;

      let pillContentChanged = false;
      if (showPill) {
        const bodyEl = pill.querySelector('.ag-lm-body');
        const ringBtn = pill.querySelector('.ag-lm-ring-btn');
        const prevRaw = bodyEl ? bodyEl.__agRaw || '' : '';
        const prevRing = ringBtn && ringBtn.style ? ringBtn.style.display || '' : '';
        const fallbackLabel =
          snap.visualState === 'error'
            ? (snap.chipText || '连接异常')
            : snap.visualState === 'slow'
            ? (snap.chipText || '等待响应…')
            : snap.isExecuting
            ? (snap.chipText || '生成中…')
            : '';
        paintPill(pill, m, fallbackLabel);
        const nextRaw = bodyEl ? bodyEl.__agRaw || '' : '';
        const nextRing = ringBtn && ringBtn.style ? ringBtn.style.display || '' : '';
        pillContentChanged = prevRaw !== nextRaw || prevRing !== nextRing;
      } else {
        paintPill(pill, null, '');
        const ctx = findInputToolbarContext(inputBox);
        if (ctx && ctx.row && ctx.row.hasAttribute('data-ag-row-mode')) {
          ctx.row.removeAttribute('data-ag-row-mode');
        }
        ps.layoutSig = '';
        ps.rowMode = '';
      }

      const state =
        snap.visualState === 'error' || snap.visualState === 'slow' || snap.isExecuting
          ? snap.visualState
          : 'idle';
      if (pill.getAttribute('data-state') !== state) pill.setAttribute('data-state', state);
      const live = snap.isExecuting ? '1' : '0';
      if (pill.getAttribute('data-ag-live') !== live) pill.setAttribute('data-ag-live', live);
      const compacting = Boolean(
        (m && m.view && m.view.ctx && m.view.ctx.compacting) ||
          activity.kind === 'compact'
      );
      if (compacting) {
        if (pill.getAttribute('data-compacting') !== '1') pill.setAttribute('data-compacting', '1');
      } else if (pill.hasAttribute('data-compacting')) {
        pill.removeAttribute('data-compacting');
      }
      const stateEl = pill.querySelector('.ag-lm-state');
      let stateTextChanged = false;
      if (stateEl) {
        const stateText =
          hasData && (state === 'slow' || state === 'error')
            ? snap.chipText || (state === 'error' ? '连接异常' : '等待响应')
            : '';
        if (stateEl.textContent !== stateText) {
          stateEl.textContent = stateText;
          stateTextChanged = true;
        }
      }
      if (showPill && (wasHidden || !ps.layoutSig || stateTextChanged || pillContentChanged)) {
        pill = ensureMetricsPillForPane(pane, false, false) || pill;
      }
      const wantTitle = !showPill || hasData ? '' : snap.dotTitle || '';
      if ((pill.getAttribute('title') || '') !== wantTitle) {
        if (wantTitle) pill.setAttribute('title', wantTitle);
        else pill.removeAttribute('title');
      }
    }

    const tt = metricsTooltipEl();
    if (tt && (window.__ag_tt_owner_pane || 'main') === paneKey) {
      syncTooltipLiveOverlay(tt, snap, pill, inputBox);
    }

    return snap;
  }

  function applyPendingWaitVisuals() {
    if (!MODULES.metrics) return null;
    const now = Date.now();
    const panes = discoverConversationPanes();
    pruneStalePills(panes);
    let mainSnap = null;
    for (let i = 0; i < panes.length; i++) {
      const snap = applyPendingWaitVisualsForPane(panes[i], now);
      if (panes[i].key === 'main') mainSnap = snap;
    }
    syncDockToggleButton();
    postPaneStates();
    return mainSnap;
  }

  window.__ag_refresh_wait_ui = applyPendingWaitVisuals;

  function extractToolbarStepMeta(tb) {
    try {
      const domCidEl = tb.closest ? tb.closest('[data-cascade-id]') : null;
      const domCid = domCidEl ? String(domCidEl.getAttribute('data-cascade-id') || '').trim() : '';

      const fk = Object.keys(tb).find(
        (k) => k.startsWith('__reactFiber$') || k.startsWith('__reactInternalInstance$')
      );
      if (!fk) return null;
      let f = tb[fk];
      let nrcProps = null;
      let iEcProps = null;
      let sliceProps = null;
      for (let i = 0; i < 20 && f; i++) {
        const p = f.memoizedProps;
        if (p) {
          if (!nrcProps && Array.isArray(p.steps) && 'showThumbsUpDown' in p) nrcProps = p;
          if (!iEcProps && p.cascadeId && Array.isArray(p.trajectorySteps) && typeof p.trajectoryLength === 'number') {
            iEcProps = p;
          }
          if (!sliceProps && p.trajectorySlice && Array.isArray(p.trajectorySlice.stepsInSlice)) {
            sliceProps = p;
          }
        }
        if (nrcProps && (iEcProps || sliceProps)) break;
        f = f.return;
      }
      if (!nrcProps) return null;

      const tSteps = iEcProps
        ? iEcProps.trajectorySteps
        : sliceProps && sliceProps.trajectorySlice
        ? sliceProps.trajectorySlice.stepsInSlice
        : [];
      const base = iEcProps
        ? iEcProps.trajectoryLength - tSteps.length
        : sliceProps && sliceProps.trajectorySlice && typeof sliceProps.trajectorySlice.totalStepsLength === 'number'
        ? sliceProps.trajectorySlice.totalStepsLength - tSteps.length
        : 0;

      let maxStep = -1;
      let maxMeta = -1;
      for (let i = 0; i < nrcProps.steps.length; i++) {
        const s = nrcProps.steps[i];
        if (!s) continue;
        const srcInfo = s.metadata && s.metadata.sourceTrajectoryStepInfo;
        let si = srcInfo && typeof srcInfo.stepIndex === 'number' ? srcInfo.stepIndex : -1;
        if (si < 0 && tSteps.length > 0) {
          const pos = tSteps.indexOf(s);
          if (pos >= 0) si = base + pos;
        }
        if (si > maxStep) maxStep = si;
        if (srcInfo && typeof srcInfo.metadataIndex === 'number' && srcInfo.metadataIndex > maxMeta) {
          maxMeta = srcInfo.metadataIndex;
        }
      }
      const fiberCid = iEcProps && iEcProps.cascadeId ? String(iEcProps.cascadeId).trim() : '';
      const resolvedCid = AG_UUID_RE.test(domCid) ? domCid : fiberCid;
      if (!resolvedCid && maxStep < 0 && maxMeta < 0) return null;
      return {
        cascadeId: resolvedCid,
        maxStep,
        maxMeta
      };
    } catch {
      return null;
    }
  }

  function matchTurnForToolbar(turns, meta) {
    if (!turns || turns.length === 0 || !meta) return null;
    if (meta.maxStep >= 0) {
      for (let i = 0; i < turns.length; i++) {
        const t = turns[i];
        if (meta.maxStep >= t.startStepIdx && (t.endStepIdx < 0 || meta.maxStep <= t.endStepIdx)) {
          return t;
        }
      }
    }
    if (meta.maxMeta >= 0) {
      for (let i = 0; i < turns.length; i++) {
        const t = turns[i];
        if (meta.maxMeta >= t.minGenIdx && meta.maxMeta <= t.maxGenIdx) {
          return t;
        }
      }
    }
    return null;
  }


  let activeTurnBadgeEl = null;

  function clearTurnTooltipTimers() {
    if (window.__ag_turn_tt_show_timer) {
      clearTimeout(window.__ag_turn_tt_show_timer);
      window.__ag_turn_tt_show_timer = null;
    }
    if (window.__ag_turn_tt_hide_timer) {
      clearTimeout(window.__ag_turn_tt_hide_timer);
      window.__ag_turn_tt_hide_timer = null;
    }
  }

  function hideTurnMetricsTooltip(immediate = false) {
    if (window.__ag_turn_tt_show_timer) {
      clearTimeout(window.__ag_turn_tt_show_timer);
      window.__ag_turn_tt_show_timer = null;
    }
    const doHide = () => {
      window.__ag_turn_tt_hide_timer = null;
      activeTurnBadgeEl = null;
      const tt = document.getElementById('ag-turn-metrics-tooltip');
      if (tt) tt.removeAttribute('data-visible');
    };
    if (immediate) {
      if (window.__ag_turn_tt_hide_timer) {
        clearTimeout(window.__ag_turn_tt_hide_timer);
        window.__ag_turn_tt_hide_timer = null;
      }
      doHide();
      return;
    }
    if (window.__ag_turn_tt_hide_timer) clearTimeout(window.__ag_turn_tt_hide_timer);
    window.__ag_turn_tt_hide_timer = setTimeout(doHide, 110);
  }

  function ensureTurnMetricsTooltipEl() {
    let tt = document.getElementById('ag-turn-metrics-tooltip');
    if (!tt && document.body) {
      tt = document.createElement('div');
      tt.id = 'ag-turn-metrics-tooltip';
      document.body.appendChild(tt);
      tt.addEventListener('mouseenter', () => {
        if (window.__ag_turn_tt_hide_timer) {
          clearTimeout(window.__ag_turn_tt_hide_timer);
          window.__ag_turn_tt_hide_timer = null;
        }
      });
      tt.addEventListener('mouseleave', () => {
        hideTurnMetricsTooltip(false);
      });
    }
    return tt;
  }

  function positionTurnMetricsTooltip(badge, tt) {
    if (!badge || !tt) return;
    try {
      const rect = badge.getBoundingClientRect();
      const vw = window.innerWidth || document.documentElement.clientWidth || 1280;
      const vh = window.innerHeight || document.documentElement.clientHeight || 800;
      tt.style.left = '12px';
      const ttRect = tt.getBoundingClientRect();
      const ttW = tt.offsetWidth || ttRect.width || 320;
      const ttH = tt.offsetHeight || ttRect.height || 130;

      const preferAbove = rect.top >= ttH + 16 || rect.top >= vh - rect.bottom;
      const left = Math.max(12, Math.min(Math.round(rect.left), vw - ttW - 12));
      if (preferAbove) {
        const top = Math.max(12, Math.round(rect.top - ttH - 8));
        tt.setAttribute('data-placement', 'above');
        tt.style.top = top + 'px';
      } else {
        const top = Math.min(vh - ttH - 12, Math.round(rect.bottom + 8));
        tt.setAttribute('data-placement', 'below');
        tt.style.top = Math.max(12, top) + 'px';
      }
      tt.style.left = left + 'px';
    } catch (e) {}
  }

  function scheduleShowTurnMetricsTooltip(badge) {
    if (!badge || !badge.__agTurnTtHtml) return;
    if (window.__ag_turn_tt_hide_timer) {
      clearTimeout(window.__ag_turn_tt_hide_timer);
      window.__ag_turn_tt_hide_timer = null;
    }
    if (activeTurnBadgeEl === badge) {
      const existingTt = document.getElementById('ag-turn-metrics-tooltip');
      if (existingTt && existingTt.getAttribute('data-visible') === '1') return;
    }
    if (window.__ag_turn_tt_show_timer) clearTimeout(window.__ag_turn_tt_show_timer);
    window.__ag_turn_tt_show_timer = setTimeout(() => {
      window.__ag_turn_tt_show_timer = null;
      if (!badge.isConnected || !badge.__agTurnTtHtml) return;
      const tt = ensureTurnMetricsTooltipEl();
      if (!tt) return;
      activeTurnBadgeEl = badge;
      if (tt.getAttribute('data-ag-turn-tt-raw') !== badge.__agTurnTtHtml) {
        tt.setAttribute('data-ag-turn-tt-raw', badge.__agTurnTtHtml);
        tt.innerHTML = badge.__agTurnTtHtml;
      }
      positionTurnMetricsTooltip(badge, tt);
      tt.setAttribute('data-visible', '1');
    }, 80);
  }

  function renderTurnMetrics(optionalPayload, optionalCid) {
    try {
      if (optionalPayload && typeof optionalPayload === 'object') {
        const pCid = optionalCid || optionalPayload.cid || optionalPayload.cascadeId || '';
        if (pCid) storeMetricsPayload(pCid, optionalPayload);
      }
      const toolbars = document.querySelectorAll('[data-testid="cascade-system-message-toolbar"]');
      if (!toolbars || toolbars.length === 0) return;

      for (let i = 0; i < toolbars.length; i++) {
        const tb = toolbars[i];
        const row = tb.firstElementChild;
        if (!row) continue;
        let badge = row.querySelector(':scope > .ag-turn-metrics');
        if (!MODULES.metrics) {
          if (badge) badge.remove();
          continue;
        }
        const meta = extractToolbarStepMeta(tb);
        if (!meta) {
          if (badge) badge.remove();
          continue;
        }
        const tbCid = meta.cascadeId || '';
        const payload = tbCid ? metricsByCid[tbCid] : null;
        const turns =
          payload && payload.hasData && Array.isArray(payload.turns) && payload.turns.length > 0
            ? payload.turns
            : null;
        if (!turns) {
          if (badge) badge.remove();
          continue;
        }
        const turn = matchTurnForToolbar(turns, meta);
        if (!turn) {
          if (badge) badge.remove();
          continue;
        }
        const { html, tooltipHtml } = renderTurnBadge(turn);
        if (!html) {
          if (badge) badge.remove();
          continue;
        }
        if (!badge) {
          badge = document.createElement('span');
          badge.className = 'ag-turn-metrics';
          badge.setAttribute('data-ag-turn-metrics', '1');
          row.prepend(badge);
        } else if (row.firstElementChild !== badge) {
          row.prepend(badge);
        }
        if (badge.getAttribute('data-ag-turn-html') !== html) {
          badge.setAttribute('data-ag-turn-html', html);
          badge.innerHTML = html;
        }
        if (badge.hasAttribute('title')) badge.removeAttribute('title');
        badge.__agTurnTtHtml = tooltipHtml;
        if (activeTurnBadgeEl === badge) {
          const tt = document.getElementById('ag-turn-metrics-tooltip');
          if (tt && tt.getAttribute('data-visible') === '1' && tt.getAttribute('data-ag-turn-tt-raw') !== tooltipHtml) {
            tt.setAttribute('data-ag-turn-tt-raw', tooltipHtml);
            tt.innerHTML = tooltipHtml;
            positionTurnMetricsTooltip(badge, tt);
          }
        }
      }
    } catch (e) {}
  }

  function ensureTurnMetricsObserver() {
    if (!MODULES.metrics || !document.body || typeof MutationObserver === 'undefined') return;
    if (window.__ag_turn_metrics_observer) return;
    const scheduleSync = (withPaneCheck) => {
      if (withPaneCheck) window.__ag_turn_sync_needs_pane = true;
      if (window.__ag_turn_sync_raf) return;
      window.__ag_turn_sync_raf = requestAnimationFrame(() => {
        window.__ag_turn_sync_raf = null;
        const needPane = Boolean(window.__ag_turn_sync_needs_pane);
        window.__ag_turn_sync_needs_pane = false;
        if (needPane) {
          checkRouteAndMountMetrics();
        } else {
          renderTurnMetrics();
        }
      });
    };
    window.__ag_turn_metrics_observer = new MutationObserver((mutations) => {
      let hitToolbar = false;
      let hitPane = false;
      for (let i = 0; i < mutations.length; i++) {
        const m = mutations[i];
        if (m.type === 'attributes') {
          if (m.attributeName === 'data-cascade-id') {
            hitPane = true;
            break;
          }
          continue;
        }
        const checkList = (nodes) => {
          if (!nodes || nodes.length === 0) return;
          for (let j = 0; j < nodes.length; j++) {
            const n = nodes[j];
            if (n.nodeType !== 1) continue;
            const tid = n.getAttribute ? n.getAttribute('data-testid') || '' : '';
            if (
              n.id === SELECTORS.inputBoxId ||
              tid === 'side-question-tab-content' ||
              tid === 'side-question-tab-composer' ||
              tid === 'side-question-tab-conversation' ||
              (n.querySelector &&
                n.querySelector('[data-testid="side-question-tab-content"], [id="' + SELECTORS.inputBoxId + '"]'))
            ) {
              hitPane = true;
              return;
            }
            if (
              tid === 'cascade-system-message-toolbar' ||
              (n.querySelector && n.querySelector('[data-testid="cascade-system-message-toolbar"]'))
            ) {
              hitToolbar = true;
            }
          }
        };
        checkList(m.addedNodes);
        if (!hitPane) checkList(m.removedNodes);
        if (hitPane) break;
      }
      if (hitPane) {
        scheduleSync(true);
      } else if (hitToolbar) {
        scheduleSync(false);
      }
    });
    try {
      window.__ag_turn_metrics_observer.observe(document.body, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ['data-cascade-id']
      });
    } catch (e) {}
  }

  window.__ag_apply_prefs = function (prefs) {
    if (!prefs || typeof prefs !== 'object') return;
    if (prefs.telemetryLayout === 'compact' || prefs.telemetryLayout === 'detailed') {
      boot.telemetryLayout = prefs.telemetryLayout;
      if (window.__ag_runtime_config) window.__ag_runtime_config.telemetryLayout = prefs.telemetryLayout;
    }
    if (prefs.telemetryCacheMode === 'session' || prefs.telemetryCacheMode === 'step') {
      boot.telemetryCacheMode = prefs.telemetryCacheMode;
    }
    const panes = discoverConversationPanes();
    for (let i = 0; i < panes.length; i++) {
      const pane = panes[i];
      const pill = document.getElementById(pane.pillId);
      if (pill && pane.cid && metricsByCid[pane.cid]) {
        paintPill(pill, metricsByCid[pane.cid], '');
        ensureMetricsPillForPane(pane, true, true);
      }
    }
    applyPendingWaitVisuals();
  };

  window.__ag_render_metrics = function (payload) {
    window.__ag_last_daemon_hb = Date.now();
    if (!payload || typeof payload !== 'object') return;
    const payloadCid = String(payload.cid || payload.cascadeId || '').trim();
    const watched = getWatchedCascadeIds();
    if (!payloadCid || !watched.includes(payloadCid)) return;

    storeMetricsPayload(payloadCid, payload);
    const mainCid = extractCascadeId();
    if (payloadCid === mainCid) {
      window.__ag_last_metrics = payload;
    }
    if (!MODULES.metrics) return;

    const panes = discoverConversationPanes();
    pruneStalePills(panes);
    const tt = metricsTooltipEl();
    for (let i = 0; i < panes.length; i++) {
      const pane = panes[i];
      if (pane.cid !== payloadCid) continue;
      if (!payloadView(payload)) continue;
      let pill = ensureMetricsPillForPane(pane, false, false);
      if (pill && paintPill(pill, payload, '')) {
        pill = ensureMetricsPillForPane(pane, false, false) || pill;
      }
      if (isMetricsTooltipOpen(tt) && (window.__ag_tt_owner_pane || 'main') === pane.key) {
        const res = hydrateTooltipForPane(pane.key, tt.getAttribute('data-mode'));
        if (res && pill) positionMetricsTooltip(pill, res.tt, pane.inputBox);
      }
    }

    ensureTurnMetricsObserver();
    renderTurnMetrics();
    applyPendingWaitVisuals();
  };

  function checkRouteAndMountMetrics() {
    if (!MODULES.metrics) return;
    const mainCid = extractCascadeId();
    const panes = discoverConversationPanes();
    pruneStalePills(panes);

    const watchedIds = getWatchedCascadeIds();
    const watchedSig = watchedIds.join(',');
    window.__ag_watched_cascade_id = mainCid || '';
    if (window.__ag_last_watched_sig !== watchedSig) {
      window.__ag_last_watched_sig = watchedSig;
      if (typeof window.__agRequestMetrics === 'function') {
        try { window.__agRequestMetrics(watchedSig); } catch (e) {}
      }
    }

    for (let i = 0; i < panes.length; i++) {
      const pane = panes[i];
      const ps = getOrCreatePaneState(pane.key);
      if (ps.cid !== pane.cid) {
        ps.cid = pane.cid;
        ps.waitTracker = null;
        if (pane.key === 'main') {
          window.__ag_last_metrics = pane.cid && metricsByCid[pane.cid] ? metricsByCid[pane.cid] : null;
        }
        if ((window.__ag_tt_owner_pane || 'main') === pane.key) {
          hideMetricsTooltip();
          const tt = metricsTooltipEl();
          if (tt) {
            tt.__agRaw = '';
            tt.__agRawMode = '';
            tt.__agView = null;
            tt.innerHTML = '';
          }
        }
        hideTurnMetricsTooltip(true);
        try {
          const scopeRoot = pane.container && pane.container.querySelectorAll ? pane.container : document;
          scopeRoot.querySelectorAll('.ag-turn-metrics').forEach((el) => el.remove());
        } catch (e) {}
        const pill = document.getElementById(pane.pillId);
        if (pill) paintPill(pill, pane.cid ? metricsByCid[pane.cid] : null, '');
      }
      if (pane.cid && payloadView(metricsByCid[pane.cid])) {
        ensureMetricsPillForPane(pane, false, false);
      }
    }

    ensureTurnMetricsObserver();
    renderTurnMetrics();
    applyPendingWaitVisuals();
  }

  window.__ag_on_route_change = checkRouteAndMountMetrics;

  if (!window.__ag_history_hooked) {
    window.__ag_history_hooked = true;
    const origPush = history.pushState;
    const origReplace = history.replaceState;
    history.pushState = function () {
      const res = origPush.apply(this, arguments);
      setTimeout(() => window.__ag_on_route_change && window.__ag_on_route_change(), 40);
      return res;
    };
    history.replaceState = function () {
      const res = origReplace.apply(this, arguments);
      setTimeout(() => window.__ag_on_route_change && window.__ag_on_route_change(), 40);
      return res;
    };
  }

  const _onPopState = () => setTimeout(() => checkRouteAndMountMetrics(), 40);
  const _onVisibility = () => {
    if (!document.hidden) {
      window.__ag_visible_since = Date.now();
      applyPendingWaitVisuals();
    }
  };
  const _onNetworkChange = () => applyPendingWaitVisuals();
  const _closeMetricsTooltip = () => {
    hideTurnMetricsTooltip(true);
    hideMetricsTooltip();
  };
  const _onEscapeKey = (e) => {
    if (!e) return;
    if (e.altKey && e.shiftKey && String(e.key || '').toLowerCase() === 'm' && MODULES.metrics && SIDE_PANE) {
      e.preventDefault();
      toggleUsagePane();
      return;
    }
    if (e.key === 'Escape') {
      _closeMetricsTooltip();
    }
  };
  const _onDocClick = (e) => {
    const t = e && e.target;
    if (!t || !t.closest) return;
    if (
      t.closest('.ag-live-metrics-pill') ||
      t.closest('#ag-live-metrics-pill') ||
      t.closest('#ag-live-metrics-tooltip') ||
      t.closest('#ag-turn-metrics-tooltip') ||
      t.closest('#ag-telemetry-dock-btn')
    ) {
      return;
    }
    _closeMetricsTooltip();
  };
  const _onTurnMouseOver = (e) => {
    if (!MODULES.metrics) return;
    const t = e && e.target;
    if (!t || !t.closest) return;
    const badge = t.closest('.ag-turn-metrics');
    if (!badge) return;
    scheduleShowTurnMetricsTooltip(badge);
  };
  const _onTurnMouseOut = (e) => {
    const t = e && e.target;
    if (!t || !t.closest) return;
    const badge = t.closest('.ag-turn-metrics');
    if (!badge) return;
    const rel = e.relatedTarget;
    if (rel && rel.closest && (rel.closest('.ag-turn-metrics') === badge || rel.closest('#ag-turn-metrics-tooltip'))) {
      return;
    }
    hideTurnMetricsTooltip(false);
  };
  const _onTurnScroll = () => {
    if (activeTurnBadgeEl || window.__ag_turn_tt_show_timer) {
      hideTurnMetricsTooltip(true);
    }
  };

  window.__ag_tt_pinned = false;
  window.__ag_tt_owner_pane = 'main';
  window.__ag_last_watched_sig = '';

  window.addEventListener('popstate', _onPopState);
  document.addEventListener('visibilitychange', _onVisibility);
  window.addEventListener('online', _onNetworkChange);
  window.addEventListener('offline', _onNetworkChange);
  window.addEventListener('keydown', _onEscapeKey, true);
  window.addEventListener('message', _onPaneMessage);
  document.addEventListener('click', _onDocClick, true);
  document.addEventListener('mouseover', _onTurnMouseOver, true);
  document.addEventListener('mouseout', _onTurnMouseOut, true);
  window.addEventListener('scroll', _onTurnScroll, true);

  if (MODULES.metrics) {
    if (!window.__ag_visible_since) window.__ag_visible_since = Date.now();
    window.__ag_metrics_route_watcher = setInterval(() => {
      if (!document.hidden) checkRouteAndMountMetrics();
    }, 2000);
    window.__ag_live_probe_timer = setInterval(() => {
      if (!document.hidden) applyPendingWaitVisuals();
    }, 1000);
  } else {
    try {
      hideTurnMetricsTooltip(true);
      const btn = dockButtonEl();
      if (btn) btn.remove();
      document.querySelectorAll('.ag-turn-metrics, .ag-live-metrics-pill').forEach((el) => el.remove());
      for (const k of Object.keys(paneStates)) {
        const ps = paneStates[k];
        if (ps && ps.ro) {
          ps.ro.disconnect();
          ps.ro = null;
          ps.roObserved = null;
        }
      }
    } catch (e) {}
  }

  window.__ag_cleanup_fns.push(() => {
    window.removeEventListener('popstate', _onPopState);
    document.removeEventListener('visibilitychange', _onVisibility);
    window.removeEventListener('online', _onNetworkChange);
    window.removeEventListener('offline', _onNetworkChange);
    window.removeEventListener('keydown', _onEscapeKey, true);
    window.removeEventListener('message', _onPaneMessage);
    document.removeEventListener('click', _onDocClick, true);
    document.removeEventListener('mouseover', _onTurnMouseOver, true);
    document.removeEventListener('mouseout', _onTurnMouseOut, true);
    window.removeEventListener('scroll', _onTurnScroll, true);
    clearTurnTooltipTimers();
    activeTurnBadgeEl = null;
    window.__ag_tt_pinned = false;
    for (const k of Object.keys(paneStates)) {
      const ps = paneStates[k];
      if (ps && ps.ro) {
        try { ps.ro.disconnect(); } catch (e) {}
        ps.ro = null;
        ps.roObserved = null;
      }
    }
    if (window.__ag_turn_metrics_observer) {
      try { window.__ag_turn_metrics_observer.disconnect(); } catch (e) {}
      window.__ag_turn_metrics_observer = null;
    }
    if (window.__ag_metrics_route_watcher) {
      clearInterval(window.__ag_metrics_route_watcher);
      window.__ag_metrics_route_watcher = null;
    }
    if (window.__ag_live_probe_timer) {
      clearInterval(window.__ag_live_probe_timer);
      window.__ag_live_probe_timer = null;
    }
    if (window.__ag_tt_show_timer) {
      clearTimeout(window.__ag_tt_show_timer);
      window.__ag_tt_show_timer = null;
    }
    if (window.__ag_tt_hide_timer) {
      clearTimeout(window.__ag_tt_hide_timer);
      window.__ag_tt_hide_timer = null;
    }
  });
