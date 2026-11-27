  const STEP_STATUS = Object.freeze({
    PENDING: 1,
    RUNNING: 2,
    DONE: 3,
    ERROR: 7,
    GENERATING: 8,
    WAITING: 9,
    HALTED: 10,
    QUEUED: 11,
    INTERRUPTED: 12
  });
  const STEP_TYPE = Object.freeze({ USER_INPUT: 14, PLANNER_RESPONSE: 15, ERROR_MESSAGE: 17, CHECKPOINT: 23 });
  const RUN_STATUS = Object.freeze({ UNSPECIFIED: 0, IDLE: 1, RUNNING: 2, CANCELING: 3, BUSY: 4 });
  const ASK_QUESTION_TOOLS = ['ask_question', 'default_api:ask_question'];

  function normalizeModelMatchKey(label) {
    return String(label || '')
      .toLowerCase()
      .replace(/\(extra\)|\bextra\b/g, 'xhigh')
      .replace(/[^a-z0-9]+/g, '');
  }

  function isSupersededStepError({ lastStepType = 0, stepsTail = [], totalStepsLen = 0, errorId = '' } = {}) {
    const tail = Array.isArray(stepsTail) ? stepsTail : [];
    const tailLast = tail.length > 0 ? tail[tail.length - 1] : null;
    const isTailErrorStep =
      Number(lastStepType) === STEP_TYPE.ERROR_MESSAGE ||
      (tailLast && (Number(tailLast.type) === STEP_TYPE.ERROR_MESSAGE || Number(tailLast.status) === STEP_STATUS.ERROR));
    if (isTailErrorStep) return false;
    const mErrIdx = errorId ? String(errorId).match(/-(\d+)$/) : null;
    const errStepIdx = mErrIdx ? Number(mErrIdx[1]) : -1;
    if (errStepIdx >= 0 && Number(totalStepsLen) > 0 && Number(totalStepsLen) - 1 - errStepIdx >= 2) {
      return true;
    }
    if (tailLast && Number(tailLast.status) === STEP_STATUS.DONE && Number(tailLast.type) !== STEP_TYPE.ERROR_MESSAGE) {
      return true;
    }
    return false;
  }

  function selectBestLoadingText(texts) {
    const list = (Array.isArray(texts) ? texts : []).map((t) => String(t || '').trim()).filter(Boolean);
    return (
      list.find((t) => /(?:retrying|Model unavailable|attempt\s*\d+\/\d+|重试|暂不可用|不可用)/i.test(t)) ||
      list[0] ||
      ''
    );
  }

  function isActiveStepStatus(status) {
    return (
      status === STEP_STATUS.PENDING ||
      status === STEP_STATUS.RUNNING ||
      status === STEP_STATUS.GENERATING ||
      status === STEP_STATUS.WAITING ||
      status === STEP_STATUS.QUEUED
    );
  }

  function isRunStatusBusy(status) {
    return status === RUN_STATUS.RUNNING || status === RUN_STATUS.CANCELING || status === RUN_STATUS.BUSY;
  }

  function stepKindOf(type, status) {
    if (status === STEP_STATUS.WAITING) return 'user';
    if (type === STEP_TYPE.CHECKPOINT) return 'compact';
    if (type === STEP_TYPE.PLANNER_RESPONSE) return 'llm';
    if (type === STEP_TYPE.ERROR_MESSAGE) return '';
    return 'tool';
  }

  function waitingForOf(step) {
    const toolName = step.metadata && step.metadata.toolCall ? String(step.metadata.toolCall.name || '') : '';
    return ASK_QUESTION_TOOLS.indexOf(toolName) >= 0 ? 'answer' : 'approval';
  }

  function strLen(v) {
    return typeof v === 'string' ? v.length : 0;
  }

  function stepPayloadLen(step) {
    const v = step.step && step.step.value;
    if (!v || typeof v !== 'object') return 0;
    let len = strLen(v.thinking) + strLen(v.rawThinking) + strLen(v.response);
    const calls = Array.isArray(v.toolCalls) ? v.toolCalls : [];
    for (let i = 0; i < calls.length; i++) {
      len += strLen(calls[i] && calls[i].argumentsJson);
    }
    return len;
  }

  function plannerPhaseOf(step) {
    const v = step.step && step.step.value;
    if (!v || typeof v !== 'object') return '';
    const hasCalls = Array.isArray(v.toolCalls) && v.toolCalls.length > 0;
    if (strLen(v.response) > 0 || hasCalls) return 'responding';
    if (strLen(v.thinking) > 0 || strLen(v.rawThinking) > 0) return 'thinking';
    return '';
  }

  function inspectLiveTailSteps(stepsInSlice) {
    const list = Array.isArray(stepsInSlice) ? stepsInSlice : [];
    let newest = null;
    let compactRunning = false;

    for (let i = list.length - 1; i >= 0; i--) {
      const step = list[i];
      if (!step || typeof step !== 'object') continue;
      const type = Number(step.type || 0);
      if (type === STEP_TYPE.USER_INPUT) break;
      const status = Number(step.status || 0);
      if (!isActiveStepStatus(status)) continue;
      if (type === STEP_TYPE.CHECKPOINT && status !== STEP_STATUS.WAITING) compactRunning = true;
      if (!newest) newest = step;
    }

    if (!newest) return { activeStepKind: '', waitingFor: '', llmPhase: '', activeProgressLen: 0 };
    const status = Number(newest.status || 0);
    const kind = compactRunning ? 'compact' : stepKindOf(Number(newest.type || 0), status);
    return {
      activeStepKind: kind,
      waitingFor: kind === 'user' ? waitingForOf(newest) : '',
      llmPhase: kind === 'llm' ? plannerPhaseOf(newest) : '',
      activeProgressLen: stepPayloadLen(newest)
    };
  }

  function getSelectedModelLabel(scopeEl) {
    try {
      const el =
        (scopeEl && scopeEl.querySelector ? scopeEl.querySelector('[data-testid="model-selector-trigger"]') : null) ||
        document.querySelector('[data-testid="model-selector-trigger"]');
      if (!el) return '';
      const fk = Object.keys(el).find(
        (k) => k.startsWith('__reactFiber$') || k.startsWith('__reactInternalInstance$')
      );
      if (fk && el[fk]) {
        let f = el[fk];
        for (let i = 0; i < 6 && f; i++, f = f.return) {
          const al = f.memoizedProps && f.memoizedProps['aria-label'];
          if (typeof al === 'string' && /Model selector|Select model/i.test(al)) {
            return al.replace(/^(?:Select model|Model(?:\s+selector)?)(?:,\s*current)?:\s*/i, '').trim();
          }
        }
      }
      const origAl = el.getAttribute('data-ag-orig-aria-label');
      if (origAl) {
        return origAl.replace(/^(?:Select model|Model(?:\s+selector)?)(?:,\s*current)?:\s*/i, '').trim();
      }
      return String(el.innerText || '').trim();
    } catch {
      return '';
    }
  }

  function readLiveCascadeRuntimeState(expectedCid, scopeBox, scopeContainer) {
    try {
      const box =
        scopeBox ||
        document.getElementById(SELECTORS.inputBoxId) ||
        document.querySelector('[data-testid="model-selector-trigger"]');
      const queryRoot = scopeContainer && scopeContainer.querySelectorAll ? scopeContainer : document;
      const isSideDraft = Boolean(
        !expectedCid &&
          scopeContainer &&
          scopeContainer.getAttribute &&
          scopeContainer.getAttribute('data-testid') === 'side-question-tab-content'
      );

      let aspState = null;
      if (box && !isSideDraft) {
        const fk = Object.keys(box).find(
          (k) => k.startsWith('__reactFiber$') || k.startsWith('__reactInternalInstance$')
        );
        let cur = fk ? box[fk] : null;
        for (let i = 0; i < 36 && cur; i++, cur = cur.return) {
          const val = cur.memoizedProps && cur.memoizedProps.value;
          const asp =
            val &&
            ((val.cascadeContext && val.cascadeContext.state && val.cascadeContext.state.agentStateProvider) ||
              val.agentStateProvider);
          if (asp && typeof asp.getState === 'function') {
            aspState = asp.getState();
            break;
          }
        }
      }

      const loadingNodes = Array.from(queryRoot.querySelectorAll('[data-testid="agent-loading"]')).filter((el) => {
        if (!el || el.hidden) return false;
        if (typeof el.getBoundingClientRect === 'function') {
          const r = el.getBoundingClientRect();
          if (r.width === 0 && r.height === 0) return false;
        }
        return true;
      });
      const loadingText = selectBestLoadingText(
        loadingNodes.map((el) => el.innerText || el.textContent || '')
      );
      const stopScope = box || document.getElementById(SELECTORS.inputBoxId);
      const stopBtnActive = Boolean(
        stopScope &&
          stopScope.querySelector(
            '[data-testid="input-cancel-button"], [data-tooltip-id="input-send-button-cancel-tooltip"], [aria-label^="Cancel ("], [aria-label^="取消生成"]'
          )
      );

      if (!aspState) {
        return {
          hasProvider: false,
          matched: !expectedCid,
          loadingText,
          stopBtnActive
        };
      }

      const providerCid = String(aspState.conversationId || '').trim();
      const matched = !expectedCid || !providerCid || providerCid === expectedCid;
      if (!matched) {
        return {
          hasProvider: true,
          matched: false,
          cid: providerCid,
          loadingText: '',
          stopBtnActive: false
        };
      }

      const execStatus = aspState.executionStatus && aspState.executionStatus.status;
      const execCase = execStatus && execStatus.case ? String(execStatus.case) : '';
      const execVal = execStatus && execStatus.value;
      const retryTs = execVal && execVal.retryAt;
      let retryAtMs = 0;
      if (retryTs && typeof retryTs.seconds !== 'undefined') {
        retryAtMs = Number(retryTs.seconds) * 1000 + Math.floor(Number(retryTs.nanos || 0) / 1e6);
      }

      const traj = aspState.trajectorySlice;
      const rawErr = traj && traj.lastStepError;
      const lastStepType = traj ? Number(traj.lastStepType || 0) : 0;
      const stepsInSlice = traj && Array.isArray(traj.stepsInSlice) ? traj.stepsInSlice : [];
      const totalStepsLen = traj && typeof traj.totalStepsLength === 'number' ? traj.totalStepsLength : 0;
      const tail = inspectLiveTailSteps(stepsInSlice);

      const errSuperseded = isSupersededStepError({
        lastStepType,
        stepsTail: stepsInSlice.slice(-4),
        totalStepsLen,
        errorId: rawErr && rawErr.errorId ? rawErr.errorId : ''
      });
      let lastStepErrorMsg = '';
      if (rawErr && !rawErr.isBenign && !errSuperseded) {
        lastStepErrorMsg = String(
          rawErr.shortError || rawErr.userErrorMessage || rawErr.modelErrorMessage || rawErr.fullError || ''
        ).trim();
      }

      const aspStatus = Number(aspState.status || 0);
      const execLoopStatus = Number(aspState.executorLoopStatus || 0);
      const effRunStatus = execLoopStatus !== RUN_STATUS.UNSPECIFIED ? execLoopStatus : aspStatus;

      return {
        hasProvider: true,
        matched: true,
        cid: providerCid,
        aspStatus,
        execLoopStatus,
        effRunStatus,
        fullyIdle: aspState.fullyIdle !== false,
        hasTrajectory: Boolean(traj),
        execCase,
        execAttempt: execVal && typeof execVal.attempt === 'number' ? execVal.attempt : 0,
        execMaxAttempts: execVal && typeof execVal.maxAttempts === 'number' ? execVal.maxAttempts : 0,
        execRetryAtMs: retryAtMs,
        execInitText: execCase === 'initialization' && typeof execVal === 'string' ? execVal : '',
        lastStepType,
        totalStepsLen,
        activeStepKind: tail.activeStepKind,
        waitingFor: tail.waitingFor,
        llmPhase: tail.llmPhase,
        activeProgressLen: tail.activeProgressLen,
        lastStepErrorMsg,
        loadingText,
        stopBtnActive
      };
    } catch {
      return { hasProvider: false, matched: false, loadingText: '', stopBtnActive: false };
    }
  }

  function readActiveStreamProgressLen(scopeContainer) {
    try {
      const root = scopeContainer && scopeContainer.querySelectorAll ? scopeContainer : document;
      const nodes = root.querySelectorAll('[data-markdown-root], .prose');
      if (!nodes.length) return 0;
      const lastNode = nodes[nodes.length - 1];
      const txtLen = lastNode && lastNode.textContent ? lastNode.textContent.length : 0;
      return nodes.length * 100000 + txtLen;
    } catch {
      return 0;
    }
  }
