import { expect, test } from 'bun:test';
import vm from 'node:vm';
import { readRuntimePart } from './helpers.ts';

type Snapshot = {
  visualState: 'idle' | 'running' | 'slow' | 'error';
  effKind: string;
  isExecuting: boolean;
  isWaitingUser: boolean;
  isRetrying: boolean;
  isStreamUnconnected: boolean;
  shouldStartWaitTimer: boolean;
  chipText: string;
  chipClass: string;
  dotTitle: string;
  noteHtml: string;
};

type TailInspection = { activeStepKind: string; waitingFor: string; llmPhase: string; activeProgressLen: number };

type Activity = { kind: string; waitingFor: string; llmPhase: string; canceling: boolean; isExecuting: boolean };

type Tracker = { waitSec: number; unconnSec: number; isStreaming: boolean } & Record<string, unknown>;

type Helpers = {
  normalizeModelMatchKey(label: string): string;
  isSupersededStepError(info: Record<string, unknown>): boolean;
  selectBestLoadingText(texts: string[]): string;
  inspectLiveTailSteps(steps: unknown[]): TailInspection;
  fmtWaitDuration(sec: number): string;
  isUserCancelError(msg: string): boolean;
  deriveActivity(input: Record<string, unknown>): Activity;
  advanceWaitTracker(prev: Tracker | null, input: Record<string, unknown>): Tracker;
  computeStatusSnapshot(input: Record<string, unknown>): Snapshot;
};

function loadPureRuntimeHelpers(): Helpers {
  const ctx: { __exports?: Helpers } = {};
  vm.createContext(ctx);
  vm.runInContext(
    readRuntimePart('30-antigravity-probe.js') +
      '\n' +
      readRuntimePart('40-status-machine.js') +
      '\nthis.__exports = { normalizeModelMatchKey, isSupersededStepError, selectBestLoadingText, inspectLiveTailSteps, fmtWaitDuration, isUserCancelError, deriveActivity, advanceWaitTracker, computeStatusSnapshot };',
    ctx
  );
  return ctx.__exports!;
}

const {
  normalizeModelMatchKey,
  isSupersededStepError,
  selectBestLoadingText,
  inspectLiveTailSteps,
  fmtWaitDuration,
  isUserCancelError,
  deriveActivity,
  advanceWaitTracker,
  computeStatusSnapshot
} = loadPureRuntimeHelpers();

const NOW = 1_700_000_000_000;

test('Part 30 helpers normalize model labels, detect superseded step errors, and prioritize retry text', () => {
  expect(normalizeModelMatchKey('Gemini 4 Hydrogen Extra')).toBe(normalizeModelMatchKey('Gemini 4 Hydrogen (XHigh)'));
  expect(
    isSupersededStepError({
      lastStepType: 15,
      stepsTail: [{ type: 15, status: 3 }],
      totalStepsLen: 20,
      errorId: 'conv-uuid-12'
    })
  ).toBe(true);
  expect(
    isSupersededStepError({
      lastStepType: 17,
      stepsTail: [{ type: 17, status: 7 }],
      totalStepsLen: 13,
      errorId: 'conv-uuid-12'
    })
  ).toBe(false);
  expect(selectBestLoadingText(['Thinking...', 'Model unavailable, retrying in 13s (attempt 6/9)'])).toBe(
    'Model unavailable, retrying in 13s (attempt 6/9)'
  );
});

test('Idle state stays steady blue even when fullyIdle is false after DONE turn', () => {
  const snap = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm', lastModelLabel: 'Gemini 4 Hydrogen (XHigh)' },
    live: {
      matched: true,
      hasProvider: true,
      aspStatus: 1,
      effRunStatus: 1,
      fullyIdle: false,
      hasTrajectory: true,
      execCase: '',
      loadingText: '',
      stopBtnActive: false
    },
    lastHb: NOW - 2000,
    visibleSince: NOW - 60000,
    curModel: 'Gemini 4 Hydrogen Extra'
  });
  expect(snap.visualState).toBe('idle');
  expect(snap.isExecuting).toBe(false);
  expect(snap.chipText).toBe('');
  expect(snap.dotTitle.startsWith('会话状态：就绪')).toBe(true);
});

test('Active execution pulses blue during normal generation and compacting', () => {
  const runSnap = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm' },
    live: { matched: true, hasProvider: true, hasTrajectory: true, effRunStatus: 2, stopBtnActive: true },
    waitSec: 12,
    lastHb: NOW - 1000
  });
  expect(runSnap.visualState).toBe('running');
  expect(runSnap.chipText).toBe('新步执行中...');
  expect(runSnap.dotTitle.startsWith('会话状态：执行中')).toBe(true);

  const compactSnap = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'compact' },
    live: { matched: true, hasProvider: true, hasTrajectory: true, effRunStatus: 2 },
    waitSec: 25,
    lastHb: NOW - 1000
  });
  expect(compactSnap.visualState).toBe('running');
  expect(compactSnap.chipText).toBe('正在压缩...');
});

test('Waiting >= 60s transitions to amber slow pulse and notes model switch cold prefill', () => {
  const snap = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'compact', lastModelLabel: 'Gemini 4 Hydrogen (XHigh)' },
    live: { matched: true, hasProvider: true, hasTrajectory: true, effRunStatus: 2 },
    waitSec: 75,
    lastHb: NOW - 1000,
    curModel: 'Gemini 4 Carbon (Medium)'
  });
  expect(snap.visualState).toBe('slow');
  expect(snap.chipClass).toBe('is-slow');
  expect(snap.chipText).toBe('压缩中 · 1m15s');
  expect(snap.noteHtml).toContain('无法复用原 KV 缓存');
  expect(snap.noteHtml).toContain('Gemini 4 Carbon (Medium)');
});

test('Model retry transitions to amber slow pulse with live countdown', () => {
  const snap = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm', pendingRetrying: true, retryReason: 'Model unavailable' },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 2,
      execCase: 'modelRequest',
      execAttempt: 6,
      execMaxAttempts: 9,
      execRetryAtMs: NOW + 13000,
      loadingText: 'Model unavailable, retrying in 13s (attempt 6/9)',
      stopBtnActive: true
    },
    lastHb: NOW - 1000
  });
  expect(snap.visualState).toBe('slow');
  expect(snap.isRetrying).toBe(true);
  expect(snap.chipText).toBe('重试连接中 · 13s');
  expect(snap.noteHtml).toContain('Model unavailable');
});

test('Retry state recovers cleanly to idle, running, or error when retry ends', () => {
  const finishedAfterRetry = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'idle', pendingRetrying: false },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 1,
      execCase: 'modelRequest',
      execAttempt: 3,
      execMaxAttempts: 9,
      execRetryAtMs: NOW - 5000,
      loadingText: '',
      stopBtnActive: false
    },
    lastHb: NOW - 1000
  });
  expect(finishedAfterRetry.visualState).toBe('idle');
  expect(finishedAfterRetry.isExecuting).toBe(false);
  expect(finishedAfterRetry.isRetrying).toBe(false);

  const canceledMidRetry = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'idle', lastErrorMsg: 'context canceled' },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 1,
      execCase: 'modelRequest',
      execAttempt: 2,
      execMaxAttempts: 9,
      execRetryAtMs: NOW + 11000,
      loadingText: 'Model unavailable, retrying in 11s (attempt 2/9)',
      stopBtnActive: false
    },
    lastHb: NOW - 1000
  });
  expect(canceledMidRetry.visualState).toBe('idle');
  expect(canceledMidRetry.isRetrying).toBe(false);

  const failedAfterRetry = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'idle', pendingRetrying: false, lastErrorMsg: '503 Service Unavailable' },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 1,
      execCase: 'modelRequest',
      execAttempt: 9,
      execMaxAttempts: 9,
      execRetryAtMs: NOW - 2000,
      lastStepErrorMsg: '503 Service Unavailable',
      stopBtnActive: false
    },
    lastHb: NOW - 1000
  });
  expect(failedAfterRetry.visualState).toBe('error');
  expect(failedAfterRetry.chipText).toBe('执行报错');
  expect(failedAfterRetry.isRetrying).toBe(false);

  const streamingAfterRetry = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm', pendingRetrying: true, retryUntilMs: NOW + 10000 },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 2,
      execCase: 'modelRequest',
      execAttempt: 2,
      execMaxAttempts: 9,
      execRetryAtMs: NOW + 5000,
      lastStepErrorMsg: 'Model unavailable',
      loadingText: '',
      stopBtnActive: true
    },
    isStreaming: true,
    lastHb: NOW - 1000
  });
  expect(streamingAfterRetry.visualState).toBe('running');
  expect(streamingAfterRetry.isRetrying).toBe(false);

  const thinkingAfterRetry = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm', pendingRetrying: true, retryUntilMs: NOW - 1000 },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 2,
      execCase: 'modelRequest',
      execAttempt: 2,
      execMaxAttempts: 9,
      execRetryAtMs: NOW - 3000,
      lastStepErrorMsg: 'Model unavailable',
      loadingText: 'Thinking...',
      stopBtnActive: true
    },
    lastHb: NOW - 1000
  });
  expect(thinkingAfterRetry.visualState).toBe('running');
  expect(thinkingAfterRetry.isRetrying).toBe(false);

  const toolAfterRetry = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'tool', pendingRetrying: true },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 2,
      execCase: 'modelRequest',
      execAttempt: 2,
      execMaxAttempts: 9,
      execRetryAtMs: NOW - 1000,
      stopBtnActive: true
    },
    lastHb: NOW - 1000
  });
  expect(toolAfterRetry.visualState).toBe('running');
  expect(toolAfterRetry.chipText).toBe('工具执行中...');
  expect(toolAfterRetry.isRetrying).toBe(false);

  const firstAttempt = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm' },
    live: {
      matched: true,
      hasProvider: true,
      hasTrajectory: true,
      effRunStatus: 2,
      execCase: 'modelRequest',
      execAttempt: 1,
      execMaxAttempts: 9,
      execRetryAtMs: 0,
      stopBtnActive: true
    },
    lastHb: NOW - 1000
  });
  expect(firstAttempt.visualState).toBe('running');
  expect(firstAttempt.isRetrying).toBe(false);
});

test('Unconnected stream escalates blue (<3s) -> amber (3s-11s) -> red (>=12s)', () => {
  const live = { matched: true, hasProvider: true, aspStatus: 0, hasTrajectory: false };
  const s2 = computeStatusSnapshot({ now: NOW, live, unconnSec: 2, lastHb: NOW });
  expect(s2.visualState).toBe('idle');
  expect(s2.shouldStartWaitTimer).toBe(true);

  const s5 = computeStatusSnapshot({ now: NOW, live, unconnSec: 5, lastHb: NOW });
  expect(s5.visualState).toBe('slow');
  expect(s5.chipText).toBe('重连中 · 5s');

  const s14 = computeStatusSnapshot({ now: NOW, live, unconnSec: 14, lastHb: NOW });
  expect(s14.visualState).toBe('error');
  expect(s14.chipClass).toBe('is-error');
  expect(s14.chipText).toBe('未连接 · 14s');

  const homeSplash = computeStatusSnapshot({ now: NOW, live, hasConversation: false, unconnSec: 14, lastHb: NOW });
  expect(homeSplash.visualState).toBe('idle');
  expect(homeSplash.isStreamUnconnected).toBe(false);
});

test('Offline, real terminal error, and daemon timeout turn red while user cancellation stays idle', () => {
  const offline = computeStatusSnapshot({ now: NOW, navigatorOnline: false, lastHb: NOW });
  expect(offline.visualState).toBe('error');
  expect(offline.chipText).toBe('网络离线');

  const canceled = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'idle', lastErrorMsg: 'context canceled' },
    lastHb: NOW
  });
  expect(canceled.visualState).toBe('idle');

  const xssErr = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'idle', lastErrorMsg: '<img src=x onerror=alert(1)>' },
    lastHb: NOW
  });
  expect(xssErr.visualState).toBe('error');
  expect(xssErr.noteHtml).not.toContain('<img');
  expect(xssErr.noteHtml).toContain('&lt;img');

  const wakeGrace = computeStatusSnapshot({
    now: NOW,
    lastHb: NOW - 120000,
    visibleSince: NOW - 5000,
    documentHidden: false
  });
  expect(wakeGrace.visualState).toBe('idle');

  const daemonDead = computeStatusSnapshot({
    now: NOW,
    lastHb: NOW - 120000,
    visibleSince: NOW - 30000,
    documentHidden: false
  });
  expect(daemonDead.visualState).toBe('error');
  expect(daemonDead.chipText).toBe('后台断连');
  expect(fmtWaitDuration(65)).toBe('1m05s');
  expect(isUserCancelError('Cancelled by user')).toBe(true);
});

const userInputStep = { type: 14, status: 3, step: { case: 'userInput', value: {} } };
const donePlannerStep = { type: 15, status: 3, step: { case: 'plannerResponse', value: { response: 'ok' } } };
const doneToolStep = { type: 132, status: 3, step: { case: 'generic', value: {} } };
const thinkingPlannerStep = {
  type: 15,
  status: 8,
  step: { case: 'plannerResponse', value: { thinking: 'Weighing the layout options', rawThinking: '', response: '', toolCalls: [] } }
};
const respondingPlannerStep = {
  type: 15,
  status: 8,
  step: { case: 'plannerResponse', value: { thinking: 'Plan ready', response: 'Here is the change', toolCalls: [] } }
};
const prefillPlannerStep = {
  type: 15,
  status: 8,
  step: { case: 'plannerResponse', value: { thinking: '', response: '', toolCalls: [] } }
};
const approvalStep = {
  type: 132,
  status: 9,
  metadata: { toolCall: { name: 'run_command' } },
  requestedInteraction: { interaction: { case: 'permission', value: {} } },
  step: { case: 'generic', value: {} }
};
const questionStep = {
  type: 132,
  status: 9,
  metadata: { toolCall: { name: 'ask_question' } },
  step: { case: 'generic', value: {} }
};

const busyLive = { matched: true, hasProvider: true, hasTrajectory: true, effRunStatus: 2, stopBtnActive: true };

test('A GENERATING planner step is the model thinking, never a wait for the user', () => {
  const tail = inspectLiveTailSteps([userInputStep, donePlannerStep, doneToolStep, thinkingPlannerStep]);
  expect(tail.activeStepKind).toBe('llm');
  expect(tail.waitingFor).toBe('');
  expect(tail.llmPhase).toBe('thinking');
  expect(tail.activeProgressLen).toBe(27);

  const snap = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'llm' },
    live: { ...busyLive, ...tail },
    waitSec: 5,
    lastHb: NOW - 1000
  });
  expect(snap.visualState).toBe('running');
  expect(snap.isWaitingUser).toBe(false);
  expect(snap.effKind).toBe('llm');
  expect(snap.chipText).toBe('思考中...');
});

test('Planner phase follows the streamed payload: prefill, thinking, then responding', () => {
  expect(inspectLiveTailSteps([userInputStep, prefillPlannerStep]).llmPhase).toBe('');
  expect(inspectLiveTailSteps([userInputStep, respondingPlannerStep]).llmPhase).toBe('responding');

  const responding = computeStatusSnapshot({
    now: NOW,
    live: { ...busyLive, ...inspectLiveTailSteps([userInputStep, respondingPlannerStep]) },
    lastHb: NOW - 1000
  });
  expect(responding.chipText).toBe('输出中...');
});

test('QUEUED and PENDING steps are work in progress; HALTED, INTERRUPTED and DONE are not', () => {
  expect(inspectLiveTailSteps([userInputStep, { type: 132, status: 11 }]).activeStepKind).toBe('tool');
  expect(inspectLiveTailSteps([userInputStep, { type: 15, status: 1 }]).activeStepKind).toBe('llm');
  expect(inspectLiveTailSteps([userInputStep, { type: 23, status: 2 }]).activeStepKind).toBe('compact');
  expect(inspectLiveTailSteps([userInputStep, { type: 132, status: 10 }]).activeStepKind).toBe('');
  expect(inspectLiveTailSteps([userInputStep, { type: 132, status: 12 }]).activeStepKind).toBe('');
  expect(inspectLiveTailSteps([userInputStep, donePlannerStep]).activeStepKind).toBe('');
});

test('Only steps after the latest user input count', () => {
  expect(inspectLiveTailSteps([approvalStep, userInputStep, { type: 15, status: 2 }])).toEqual({
    activeStepKind: 'llm',
    waitingFor: '',
    llmPhase: '',
    activeProgressLen: 0
  });
});

test('A WAITING step tells approval apart from a question', () => {
  const approvalTail = inspectLiveTailSteps([userInputStep, donePlannerStep, approvalStep]);
  expect(approvalTail.activeStepKind).toBe('user');
  expect(approvalTail.waitingFor).toBe('approval');

  const questionTail = inspectLiveTailSteps([userInputStep, donePlannerStep, questionStep]);
  expect(questionTail.activeStepKind).toBe('user');
  expect(questionTail.waitingFor).toBe('answer');

  const approval = computeStatusSnapshot({
    now: NOW,
    live: { ...busyLive, ...approvalTail },
    waitSec: 68,
    lastHb: NOW - 1000
  });
  expect(approval.visualState).toBe('idle');
  expect(approval.isExecuting).toBe(false);
  expect(approval.isWaitingUser).toBe(true);
  expect(approval.chipText).toBe('等待审批');
  expect(approval.shouldStartWaitTimer).toBe(false);

  const question = computeStatusSnapshot({
    now: NOW,
    live: { ...busyLive, ...questionTail },
    lastHb: NOW - 1000
  });
  expect(question.chipText).toBe('等待回答');
});

test('Live trajectory outranks a stale DB wait, and the DB is trusted only without live data', () => {
  const approvedLive = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'user' },
    live: { ...busyLive, activeStepKind: '' },
    lastHb: NOW - 1000
  });
  expect(approvedLive.isWaitingUser).toBe(false);
  expect(approvedLive.visualState).toBe('running');

  const toolLive = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'user' },
    live: { ...busyLive, activeStepKind: 'tool' },
    waitSec: 70,
    lastHb: NOW - 1000
  });
  expect(toolLive.visualState).toBe('slow');
  expect(toolLive.chipText).toBe('工具等待 · 1m10s');

  const dbOnly = computeStatusSnapshot({
    now: NOW,
    st: { pendingKind: 'user' },
    live: { matched: true, hasProvider: false, stopBtnActive: false },
    waitSec: 75,
    lastHb: NOW - 1000
  });
  expect(dbOnly.visualState).toBe('idle');
  expect(dbOnly.isWaitingUser).toBe(true);
  expect(dbOnly.chipText).toBe('等待确认');
});

test('Run status follows Antigravity: CANCELING and BUSY are still running', () => {
  const canceling = computeStatusSnapshot({
    now: NOW,
    live: { matched: true, hasProvider: true, hasTrajectory: true, effRunStatus: 3, stopBtnActive: false },
    lastHb: NOW - 1000
  });
  expect(canceling.visualState).toBe('running');
  expect(canceling.chipText).toBe('正在停止...');

  const busy = deriveActivity({
    live: { matched: true, hasProvider: true, hasTrajectory: true, effRunStatus: 4, stopBtnActive: false }
  });
  expect(busy.isExecuting).toBe(true);
  expect(busy.kind).toBe('llm');
});

test('Wait tracker keeps a long but streaming think out of 首字等待', () => {
  const activity = deriveActivity({ live: { ...busyLive, activeStepKind: 'llm', llmPhase: 'thinking' } });
  const t0 = advanceWaitTracker(null, { now: NOW, cid: 'c1', activity, stepCounts: [3, 1, 4], contentLen: 100 });
  const t1 = advanceWaitTracker(t0, { now: NOW + 90_000, cid: 'c1', activity, stepCounts: [3, 1, 4], contentLen: 4000 });
  expect(t1.waitSec).toBe(0);
  expect(t1.isStreaming).toBe(true);

  const t2 = advanceWaitTracker(t1, { now: NOW + 160_000, cid: 'c1', activity, stepCounts: [3, 1, 4], contentLen: 4000 });
  expect(t2.waitSec).toBe(70);
  expect(t2.isStreaming).toBe(false);

  const stalled = computeStatusSnapshot({
    now: NOW + 160_000,
    live: { ...busyLive, activeStepKind: 'llm', llmPhase: 'thinking' },
    waitSec: t2.waitSec,
    isStreaming: t2.isStreaming,
    lastHb: NOW + 159_000
  });
  expect(stalled.visualState).toBe('slow');
  expect(stalled.chipText).toBe('输出停滞 · 1m10s');
});

test('Wait tracker restarts on new steps, a new kind, or a new conversation, and clears when idle', () => {
  const llm = deriveActivity({ live: { ...busyLive, activeStepKind: 'llm' } });
  const tool = deriveActivity({ live: { ...busyLive, activeStepKind: 'tool' } });
  const idle = deriveActivity({ live: { ...busyLive, effRunStatus: 1, stopBtnActive: false } });

  const start = advanceWaitTracker(null, { now: NOW, cid: 'c1', activity: llm, stepCounts: [3, 1, 4], contentLen: 0 });
  const quiet = advanceWaitTracker(start, { now: NOW + 30_000, cid: 'c1', activity: llm, stepCounts: [3, 1, 4], contentLen: 0 });
  expect(quiet.waitSec).toBe(30);

  const newStep = advanceWaitTracker(quiet, { now: NOW + 31_000, cid: 'c1', activity: llm, stepCounts: [3, 1, 5], contentLen: 0 });
  expect(newStep.waitSec).toBe(0);

  const staleDbRow = advanceWaitTracker(newStep, { now: NOW + 40_000, cid: 'c1', activity: llm, stepCounts: [2, 1, 5], contentLen: 0 });
  expect(staleDbRow.waitSec).toBe(9);

  const kindSwitch = advanceWaitTracker(staleDbRow, { now: NOW + 41_000, cid: 'c1', activity: tool, stepCounts: [3, 1, 5], contentLen: 0 });
  expect(kindSwitch.waitSec).toBe(0);

  const otherConversation = advanceWaitTracker(kindSwitch, { now: NOW + 50_000, cid: 'c2', activity: tool, stepCounts: [9, 9, 9], contentLen: 50 });
  expect(otherConversation.waitSec).toBe(0);
  expect(otherConversation.isStreaming).toBe(false);

  const done = advanceWaitTracker(otherConversation, { now: NOW + 60_000, cid: 'c2', activity: idle, stepCounts: [9, 9, 9], contentLen: 50 });
  expect(done.waitSec).toBe(0);
  expect(done.isStreaming).toBe(false);
});

test('Wait tracker times an unconnected stream from its first sighting', () => {
  const idle = deriveActivity({});
  const first = advanceWaitTracker(null, { now: NOW, cid: 'c1', activity: idle, unconnected: true });
  const later = advanceWaitTracker(first, { now: NOW + 7_000, cid: 'c1', activity: idle, unconnected: true });
  expect(later.unconnSec).toBe(7);
  const back = advanceWaitTracker(later, { now: NOW + 8_000, cid: 'c1', activity: idle, unconnected: false });
  expect(back.unconnSec).toBe(0);
});
