  // Must stay DOM/window-free so tests can evaluate this file directly via node:vm.
  const WAIT_DEFAULTS = Object.freeze({
    SLOW_WAIT_SEC: 60,
    UNCONN_WARN_SEC: 3,
    UNCONN_ERROR_SEC: 12,
    HEARTBEAT_STALE_MS: 90000,
    VISIBLE_GRACE_MS: 20000,
    STREAM_FRESH_MS: 4000
  });

  const ACTIVITY_KINDS = ['llm', 'compact', 'tool', 'user'];
  const RETRY_TEXT_RE = /(?:retrying|Model unavailable|attempt\s*\d+\/\d+|重试|暂不可用|不可用).*$/i;

  function fmtWaitDuration(sec) {
    const s = Math.max(0, Math.floor(Number(sec) || 0));
    if (s < 60) return s + 's';
    const m = Math.floor(s / 60);
    const r = s % 60;
    return m + 'm' + (r > 0 ? String(r).padStart(2, '0') + 's' : '');
  }

  function isUserCancelError(msg) {
    const s = String(msg || '').trim();
    if (!s) return false;
    return /(\bcontext\s+canceled\b|\bcancell?ed\s+by\s+user\b|\baborted\s+by\s+user\b|\buser\s+cancell?ed\b)/i.test(
      s
    );
  }

  function escapeStatusHtml(str) {
    return String(str == null ? '' : str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#39;');
  }

  function knownKind(raw) {
    const k = raw ? String(raw) : '';
    return ACTIVITY_KINDS.indexOf(k) >= 0 ? k : '';
  }

  function deriveActivity({ st = null, live = null } = {}) {
    const liveUsable = Boolean(live && live.matched);
    const hasProvider = Boolean(liveUsable && live.hasProvider);
    const liveAuthoritative = Boolean(hasProvider && live.hasTrajectory);
    const effRunStatus = hasProvider ? Number(live.effRunStatus || 0) : -1;
    const stopBtnActive = Boolean(liveUsable && live.stopBtnActive);
    const loadingText = liveUsable ? String(live.loadingText || '').trim() : '';
    const retryBanner = Boolean(loadingText && RETRY_TEXT_RE.test(loadingText));

    const stopped = Boolean(liveAuthoritative && effRunStatus === RUN_STATUS.IDLE && !stopBtnActive);
    const busy = Boolean(stopBtnActive || (hasProvider ? isRunStatusBusy(effRunStatus) : retryBanner));
    const canceling = Boolean(hasProvider && effRunStatus === RUN_STATUS.CANCELING);

    const liveKind = hasProvider ? knownKind(live.activeStepKind) : '';
    const dbKindRaw = knownKind(st && st.pendingKind);
    const dbKind = dbKindRaw === 'user' && liveAuthoritative ? '' : dbKindRaw;

    let kind = '';
    if (!stopped) kind = liveKind || dbKind || (busy ? 'llm' : '');
    if (!kind) kind = 'idle';

    const fromLive = Boolean(liveKind && kind === liveKind);
    return {
      kind,
      waitingFor: kind === 'user' && fromLive ? String(live.waitingFor || '') : '',
      llmPhase: kind === 'llm' && fromLive ? String(live.llmPhase || '') : '',
      canceling: Boolean(canceling && !stopped),
      isExecuting: kind === 'llm' || kind === 'compact' || kind === 'tool',
      isWaitingUser: kind === 'user',
      stopped
    };
  }

  function elapsedSec(now, sinceMs) {
    return sinceMs ? Math.max(0, Math.floor((now - sinceMs) / 1000)) : 0;
  }

  function advanceWaitTracker(
    prev,
    { now = Date.now(), cid = '', activity = null, stepCounts = [], contentLen = 0, unconnected = false, limits = WAIT_DEFAULTS } = {}
  ) {
    const cfg = Object.assign({}, WAIT_DEFAULTS, limits || {});
    const fresh = !prev || prev.cid !== cid;
    const base = fresh
      ? { cid, kind: 'idle', waitStartMs: 0, lastStreamMs: 0, unconnStartMs: 0, stepCounts: [], contentLen: 0 }
      : prev;
    const kind = activity && activity.kind ? activity.kind : 'idle';
    const executing = Boolean(activity && activity.isExecuting);

    const counts = Array.isArray(stepCounts) ? stepCounts.map((n) => Number(n) || 0) : [];
    const maxCounts = counts.map((n, i) => Math.max(n, fresh ? 0 : Number(base.stepCounts[i]) || 0));
    const stepped = !fresh && maxCounts.some((n, i) => n > (Number(base.stepCounts[i]) || 0));
    const len = Number(contentLen) || 0;
    const streamed = !fresh && executing && len > base.contentLen;

    let waitStartMs = 0;
    let lastStreamMs = 0;
    if (executing) {
      const restart = !base.waitStartMs || base.kind !== kind || stepped || streamed;
      waitStartMs = restart ? now : base.waitStartMs;
      lastStreamMs = streamed ? now : base.lastStreamMs;
    }
    const unconnStartMs = unconnected ? base.unconnStartMs || now : 0;

    return {
      cid,
      kind,
      waitStartMs,
      lastStreamMs,
      unconnStartMs,
      stepCounts: maxCounts,
      contentLen: len,
      waitSec: elapsedSec(now, waitStartMs),
      unconnSec: elapsedSec(now, unconnStartMs),
      isStreaming: Boolean(lastStreamMs && now - lastStreamMs <= cfg.STREAM_FRESH_MS)
    };
  }

  function computeStatusSnapshot({
    now = Date.now(),
    st = null,
    live = null,
    waitSec = 0,
    unconnSec = 0,
    lastHb = 0,
    visibleSince = 0,
    documentHidden = false,
    navigatorOnline = true,
    hasConversation = true,
    curModel = '',
    isStreaming = false,
    limits = WAIT_DEFAULTS
  } = {}) {
    const cfg = Object.assign({}, WAIT_DEFAULTS, limits || {});
    const activity = deriveActivity({ st, live });
    const effKind = activity.kind;
    const isExecuting = activity.isExecuting;
    const isWaitingUser = activity.isWaitingUser;

    const liveUsable = Boolean(live && live.matched);
    const hasProvider = Boolean(liveUsable && live.hasProvider);
    const aspStatus = hasProvider ? Number(live.aspStatus || 0) : -1;
    const hasTrajectory = Boolean(hasProvider && live.hasTrajectory);
    const execCase = hasProvider ? String(live.execCase || '') : '';
    const loadingText = liveUsable ? String(live.loadingText || '').trim() : '';
    const loadingRetryMatch = loadingText ? loadingText.match(RETRY_TEXT_RE) : null;
    const loadingNonRetry = Boolean(loadingText && !loadingRetryMatch);

    const liveErrRaw = hasProvider && live.lastStepErrorMsg ? String(live.lastStepErrorMsg).trim() : '';
    const isLiveNonCancelErr = Boolean(liveErrRaw && !isUserCancelError(liveErrRaw));

    const execRetryAtMs = hasProvider ? Number(live.execRetryAtMs || 0) : 0;
    const execAttempt = hasProvider ? Number(live.execAttempt || 0) : 0;
    const execRetryCountdownActive = Boolean(
      execCase === 'modelRequest' && execRetryAtMs > 0 && execRetryAtMs + 2000 > now
    );
    const execRetryAttemptActive = Boolean(
      execCase === 'modelRequest' && !execRetryAtMs && execAttempt > 1 && !loadingNonRetry
    );
    const execRetrying = Boolean(
      hasProvider &&
        effKind === 'llm' &&
        !isStreaming &&
        (execRetryCountdownActive || execRetryAttemptActive)
    );

    const stRetryUntilMs = st ? Number(st.retryUntilMs || 0) : 0;
    const stRetryExpired = Boolean(stRetryUntilMs > 0 && now >= stRetryUntilMs);
    const stRetrying = Boolean(
      st &&
        st.pendingRetrying &&
        !stRetryExpired &&
        effKind !== 'tool' &&
        !isStreaming &&
        !loadingNonRetry
    );
    const isRetrying = Boolean(isExecuting && (stRetrying || loadingRetryMatch || execRetrying));

    const isStreamUnconnected = Boolean(hasConversation && hasProvider && aspStatus === 0 && !hasTrajectory);
    const unconnDurSec = Math.max(0, Math.floor(Number(unconnSec) || 0));
    const waitDurSec = Math.max(0, Math.floor(Number(waitSec) || 0));

    const offlineProblem = !navigatorOnline ? '当前网络已断开（离线），无法连接到模型服务' : '';
    const unconnProblem =
      isStreamUnconnected && unconnDurSec >= cfg.UNCONN_ERROR_SEC
        ? '会话流未连接或重连超时（已等待 ' + fmtWaitDuration(unconnDurSec) + '）'
        : '';
    const unconnSlow = Boolean(
      isStreamUnconnected && unconnDurSec >= cfg.UNCONN_WARN_SEC && unconnDurSec < cfg.UNCONN_ERROR_SEC
    );

    const stErrRaw = st && st.lastErrorMsg ? String(st.lastErrorMsg).trim() : '';
    const stErrNonCancel = stErrRaw && !isUserCancelError(stErrRaw) ? stErrRaw : '';
    const apiErrMsg = !isExecuting && !isWaitingUser ? stErrNonCancel || (isLiveNonCancelErr ? liveErrRaw : '') : '';

    const hbMs = Number(lastHb) || 0;
    const visMs = Number(visibleSince) || 0;
    const daemonProblem =
      !documentHidden &&
      hbMs > 0 &&
      now - hbMs > cfg.HEARTBEAT_STALE_MS &&
      (visMs === 0 || now - visMs >= cfg.VISIBLE_GRACE_MS)
        ? '后台统计服务响应超时（超过 ' + fmtWaitDuration((now - hbMs) / 1000) + ' 未收到心跳）'
        : '';

    const curNorm = normalizeModelMatchKey(curModel);
    const lastNorm = st && st.lastModelLabel ? normalizeModelMatchKey(st.lastModelLabel) : '';
    const modelSwitched = Boolean(curNorm && lastNorm && curNorm !== lastNorm);

    const isModelWaiting = Boolean(isExecuting && waitDurSec >= cfg.SLOW_WAIT_SEC);
    const hardErrMsg = offlineProblem || unconnProblem || apiErrMsg;

    let visualState = 'idle';
    if (hardErrMsg) {
      visualState = 'error';
    } else if (unconnSlow || isRetrying || isModelWaiting) {
      visualState = 'slow';
    } else if (isExecuting) {
      visualState = 'running';
    } else if (daemonProblem) {
      visualState = 'error';
    }

    let chipText = '';
    let chipClass = '';
    let dotTitle = '会话状态：就绪';
    let noteHtml = '';
    let noteClass = '';

    if (visualState === 'error') {
      const errDetail = hardErrMsg || daemonProblem || '请求或连接出现异常';
      chipClass = 'is-error';
      if (offlineProblem) {
        chipText = '网络离线';
      } else if (unconnProblem) {
        chipText = '未连接 · ' + fmtWaitDuration(unconnDurSec);
      } else if (apiErrMsg) {
        chipText = '执行报错';
      } else {
        chipText = '后台断连';
      }
      dotTitle = '会话状态：异常 · ' + errDetail;
      noteClass = 'is-visible is-error';
      noteHtml = '<strong>连接 / 执行异常</strong><br/>' + escapeStatusHtml(errDetail);
    } else if (visualState === 'slow') {
      chipClass = 'is-slow';
      if (unconnSlow) {
        chipText = '重连中 · ' + fmtWaitDuration(unconnDurSec);
        dotTitle = '会话状态：等待或重试 · 正在建立会话流连接（已等待 ' + fmtWaitDuration(unconnDurSec) + '）';
        noteClass = 'is-visible';
        noteHtml =
          '<span class="ag-tt-note-row"><strong>会话连接中（' +
          escapeStatusHtml(fmtWaitDuration(unconnDurSec)) +
          '）</strong><span class="ag-tt-note-sub">同步数据流中</span></span>';
      } else if (isRetrying) {
        const retryBrief =
          (loadingRetryMatch && loadingRetryMatch[0]) ||
          (st && st.retryReason) ||
          (live && live.execAttempt && live.execMaxAttempts
            ? '正在重试连接 (attempt ' + live.execAttempt + '/' + live.execMaxAttempts + ')'
            : '正在自动重试模型连接');
        let countdownSuffix = '';
        if (hasProvider && live.execRetryAtMs && live.execRetryAtMs > now) {
          const leftSec = Math.max(1, Math.ceil((live.execRetryAtMs - now) / 1000));
          countdownSuffix = ' · ' + leftSec + 's';
        }
        chipText = '重试连接中' + countdownSuffix;
        dotTitle = '会话状态：等待或重试 · ' + retryBrief;
        noteClass = 'is-visible';
        noteHtml = '<strong>模型自动重试中</strong><br/>' + escapeStatusHtml(retryBrief);
      } else {
        const waitStr = fmtWaitDuration(waitDurSec);
        if (effKind === 'compact') {
          chipText = '压缩中 · ' + waitStr;
          dotTitle = '会话状态：等待或重试 · 上下文压缩中（已等待 ' + waitStr + '）';
          noteClass = 'is-visible';
          noteHtml = modelSwitched
            ? '<strong>上下文压缩中（' +
              escapeStatusHtml(waitStr) +
              '）</strong><br/>已切换为 <code>' +
              escapeStatusHtml(curModel) +
              '</code>，无法复用原 KV 缓存，全量重算耗时较长。'
            : '<span class="ag-tt-note-row"><strong>上下文压缩中（' +
              escapeStatusHtml(waitStr) +
              '）</strong><span class="ag-tt-note-sub">汇总对话历史中</span></span>';
        } else if (effKind === 'tool') {
          chipText = '工具等待 · ' + waitStr;
          dotTitle = '会话状态：等待或重试 · 工具执行等待中（已等待 ' + waitStr + '）';
          noteClass = 'is-visible';
          noteHtml =
            '<span class="ag-tt-note-row"><strong>工具执行中（' +
            escapeStatusHtml(waitStr) +
            '）</strong><span class="ag-tt-note-sub">等待结果返回</span></span>';
        } else if (activity.llmPhase) {
          chipText = '输出停滞 · ' + waitStr;
          dotTitle = '会话状态：等待或重试 · 模型输出停滞（已等待 ' + waitStr + '）';
          noteClass = 'is-visible';
          noteHtml =
            '<span class="ag-tt-note-row"><strong>' +
            (activity.llmPhase === 'thinking' ? '思考' : '输出') +
            '停滞（' +
            escapeStatusHtml(waitStr) +
            '）</strong><span class="ag-tt-note-sub">排队或网络抖动</span></span>';
        } else {
          chipText = '首字等待 · ' + waitStr;
          dotTitle = '会话状态：等待或重试 · 等待模型响应（已等待 ' + waitStr + '）';
          noteClass = 'is-visible';
          noteHtml = modelSwitched
            ? '<strong>等待首字输出（' +
              escapeStatusHtml(waitStr) +
              '）</strong><br/>已切换至 <code>' +
              escapeStatusHtml(curModel) +
              '</code>，冷启动预填充中。'
            : '<span class="ag-tt-note-row"><strong>等待首字输出（' +
              escapeStatusHtml(waitStr) +
              '）</strong><span class="ag-tt-note-sub">预填充 / 排队中</span></span>';
        }
      }
    } else if (visualState === 'running') {
      if (activity.canceling) {
        chipText = '正在停止...';
        dotTitle = '会话状态：执行中 · 正在停止当前任务';
      } else if (effKind === 'compact') {
        chipText = '正在压缩...';
        dotTitle = '会话状态：执行中 · 正在压缩上下文历史';
      } else if (effKind === 'tool') {
        chipText = '工具执行中...';
        dotTitle = '会话状态：执行中 · 正在执行工具调用';
      } else if (activity.llmPhase === 'thinking') {
        chipText = '思考中...';
        dotTitle = '会话状态：执行中 · 模型思考中';
      } else if (activity.llmPhase === 'responding') {
        chipText = '输出中...';
        dotTitle = '会话状态：执行中 · 模型输出中';
      } else {
        chipText = '新步执行中...';
        dotTitle = '会话状态：执行中 · 等待模型开始输出';
      }
    } else if (isWaitingUser) {
      if (activity.waitingFor === 'answer') {
        chipText = '等待回答';
        dotTitle = '会话状态：等待你回答智能体的提问';
      } else if (activity.waitingFor === 'approval') {
        chipText = '等待审批';
        dotTitle = '会话状态：等待你批准智能体的操作';
      } else {
        chipText = '等待确认';
        dotTitle = '会话状态：等待用户确认';
      }
    } else {
      chipText = '';
      dotTitle = '会话状态：就绪';
    }

    return {
      visualState,
      effKind,
      waitingFor: activity.waitingFor,
      llmPhase: activity.llmPhase,
      isExecuting,
      isWaitingUser,
      isModelWaiting,
      isRetrying,
      isStreamUnconnected,
      unconnSlow,
      dotTitle,
      chipText,
      chipClass,
      noteHtml,
      noteClass,
      shouldStartWaitTimer: Boolean(isExecuting || isStreamUnconnected)
    };
  }
