package metrics

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const testCid = "0b9f6c1e-2c64-4d1b-9a3e-0123456789ab"

func TestSourceLoadsConversation(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.gens = []map[string]any{
		call{in: 1000, out: 200, cacheRead: 9000, think: 50, resp: 150, maxCtx: 512000, steps: []int{1}, model: "Gemini 4 Hydrogen (XHigh)", ttft: "0.8s", stream: "1s"}.json(),
		call{in: 1200, out: 300, cacheWrite: 100, cacheRead: 10000, think: 60, resp: 240, maxCtx: 512000, steps: []int{3}, model: "Gemini 4 Hydrogen (XHigh)", ttft: "0.5s", stream: "1.5s"}.json(),
	}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"), step(2, "USER_INPUT", "DONE"), step(3, "PLANNER_RESPONSE", "DONE")}
	src, _ := follow(t, f)

	p := payloadWhen(t, src, testCid, hasData)
	s := p.Stats
	if s.GenIdx != 2 || s.OutTok != 300 || s.Ctx != 11300 || s.Tps != 200 || s.ContextLimit != 512000 || s.StatusLevel != "idle" {
		t.Errorf("stats = %+v", s)
	}
	if len(p.Turns) != 2 || p.Status.TotalGenerations != 2 || p.Status.TotalSteps != 4 || p.Status.LastModelLabel != "Gemini 4 Hydrogen (XHigh)" {
		t.Errorf("turns = %d, status = %+v", len(p.Turns), p.Status)
	}
	v := p.View
	if v.CacheMode != "session" || v.Last.N != 2 || v.Last.Turn != 2 || v.Ctx.Limit != 512000 || v.Ctx.LimitSrc != "model" {
		t.Errorf("view = %+v", v)
	}
	if v.Turn.Index != 2 || v.Turn.Calls != 1 || v.Turn.Steps != 1 || v.Turn.StartedMs != stepMs(2) || v.Session.Calls != 2 || len(v.History) != 2 {
		t.Errorf("turn = %+v, session = %+v", v.Turn, v.Session)
	}
	if p.Turns[0].StartedMs != stepMs(0) || p.Turns[1].StartedMs != stepMs(2) || v.History[0].At != stepMs(1) || v.History[1].At != stepMs(3) {
		t.Errorf("times: turns %v/%v, calls %v/%v", p.Turns[0].StartedMs, p.Turns[1].StartedMs, v.History[0].At, v.History[1].At)
	}

	again, _ := src.Payload(testCid, defaultCfg)
	if again != p {
		t.Errorf("an unchanged conversation should return the same payload")
	}
	stepMode, _ := src.Payload(testCid, ReaderConfig{FallbackContextLimit: 256000, TelemetryCacheMode: "step"})
	if stepMode.View.CacheMode != "step" || stepMode.Sig == p.Sig {
		t.Errorf("a config change should rebuild the payload: %q vs %q", stepMode.Sig, p.Sig)
	}
}

func TestSourceReportsUnreadableConversationAsEmpty(t *testing.T) {
	f := newFakeLS(t, testCid)
	other := "11111111-2222-3333-4444-555555555555"
	src := NewSource(f.client(), nil)
	t.Cleanup(src.Close)
	wrongToken := NewSource(NewLSClient(strings.TrimPrefix(f.srv.URL, "http://"), "stale"), nil)
	t.Cleanup(wrongToken.Close)

	src.SetWatched("window-1", []string{other})
	wrongToken.SetWatched("window-1", []string{testCid})

	missing := payloadWhen(t, src, other, func(p *Payload) bool { return !p.HasData })
	rejected := payloadWhen(t, wrongToken, testCid, func(p *Payload) bool { return !p.HasData })
	if missing.Cid != other || rejected.Cid != testCid {
		t.Errorf("empty payloads should name their conversation: %q %q", missing.Cid, rejected.Cid)
	}
}

func TestSourceStopsFollowingClosedConversations(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.gens = []map[string]any{call{in: 10, out: 5, steps: []int{1}}.json()}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE")}
	src, _ := follow(t, f)
	payloadWhen(t, src, testCid, hasData)

	src.SetWatched("window-1", nil)

	if p, ok := src.Payload(testCid, defaultCfg); ok {
		t.Errorf("a conversation no window shows should not be served: %s", describe(p))
	}
}

func TestSourcePullsOnlyWhatChanged(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.status = "CASCADE_RUN_STATUS_RUNNING"
	f.gens = []map[string]any{
		call{in: 1000, out: 200, cacheRead: 9000, maxCtx: 512000, steps: []int{1}, ttft: "0.8s", stream: "1s"}.json(),
		call{in: 1500, out: 400, cacheRead: 9500, maxCtx: 512000, steps: []int{3}, ttft: "0.5s", stream: "2s"}.json(),
	}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"), step(2, "USER_INPUT", "DONE"), step(3, "PLANNER_RESPONSE", "DONE"), step(4, "PLANNER_RESPONSE", "GENERATING")}
	src, _ := follow(t, f)
	running := payloadWhen(t, src, testCid, hasData)
	if running.Status.PendingKind != "llm" || running.Stats.StatusLevel != "running" || running.Stats.PendingStepIdx != 4 || running.Stats.PendingStartedAtMs != stepMs(4) {
		t.Fatalf("a generating model step should be a running call: %s", describe(running))
	}
	f.resetPullLog()

	f.change(func(f *fakeLS) {
		f.status = runStatusIdle
		f.gens = append(f.gens, call{in: 1600, out: 500, cacheRead: 10000, maxCtx: 512000, steps: []int{4}, ttft: "0.5s", stream: "1s"}.json())
		f.steps[4] = step(4, "PLANNER_RESPONSE", "DONE")
	}, nil)

	idle := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Stats.GenIdx == 3 })
	if idle.Stats.GeneratingPending || idle.Stats.OutTok != 500 || idle.Status.TotalGenerations != 3 || idle.Status.PendingKind != "idle" {
		t.Errorf("finished call = %s", describe(idle))
	}
	if got := f.pullLog(); !slices.Equal(got, []string{"gens@1", "steps@4"}) {
		t.Errorf("pulls = %v", got)
	}
}

func TestSourceApprovalAndToolSteps(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.gens = []map[string]any{call{in: 1000, out: 200, cacheRead: 9000, maxCtx: 512000, steps: []int{1, 2}, ttft: "0.8s", stream: "1s"}.json()}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"), step(2, "RUN_COMMAND", "WAITING")}
	src, _ := follow(t, f)

	waiting := payloadWhen(t, src, testCid, hasData)
	if waiting.Status.PendingKind != "user" || waiting.Stats.GeneratingPending || waiting.Stats.StatusLevel != "idle" {
		t.Fatalf("a step waiting for approval should wait on the user: %s", describe(waiting))
	}

	f.change(func(f *fakeLS) { f.steps[2] = step(2, "RUN_COMMAND", "RUNNING") }, nil)
	approved := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Status.PendingKind != "user" })
	if approved.Status.PendingKind != "tool" || !approved.Stats.GeneratingPending || approved.Stats.StatusLevel != "running" {
		t.Errorf("an approved command should run: %s", describe(approved))
	}

	f.change(func(f *fakeLS) {
		f.steps[2] = step(2, "RUN_COMMAND", "DONE")
		f.steps = append(f.steps, step(3, "VIEW_FILE", "QUEUED"))
	}, nil)
	queued := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Stats.PendingStepIdx == 3 })
	if queued.Status.PendingKind != "tool" || queued.Stats.StatusLevel != "running" {
		t.Errorf("a queued tool step is work in progress: %s", describe(queued))
	}

	f.change(func(f *fakeLS) { f.steps[3] = step(3, "VIEW_FILE", "CANCELED") }, nil)
	canceled := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Stats.PendingStepIdx == -1 })
	if canceled.Status.PendingKind != "idle" || canceled.Stats.StatusLevel != "idle" {
		t.Errorf("a canceled step ends the work: %s", describe(canceled))
	}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func TestSourceRetryLifecycle(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.status = "CASCADE_RUN_STATUS_RUNNING"
	f.gens = []map[string]any{call{in: 1000, out: 200, cacheRead: 9000, maxCtx: 512000, steps: []int{1}, ttft: "0.8s", stream: "1s"}.json()}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"), step(2, "USER_INPUT", "DONE"), step(3, "PLANNER_RESPONSE", "PENDING")}
	t0 := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: t0}
	src := NewSource(f.client(), nil)
	src.now = clock.Now
	t.Cleanup(src.Close)
	src.SetWatched("window-1", []string{testCid})
	payloadWhen(t, src, testCid, hasData)

	f.change(func(f *fakeLS) {
		f.steps[3] = with(step(3, "PLANNER_RESPONSE", "PENDING"), "metadata", map[string]any{
			"createdAt":  stepAt(3).Format(time.RFC3339Nano),
			"retryInfos": []any{map[string]any{"error": "Model unavailable", "retryReason": "RETRY_REASON_API_RETRYABLE"}},
		})
	}, map[string]any{"executionStatus": map[string]any{"modelRetry": map[string]any{"attempt": 2, "maxAttempts": 5, "retryAt": t0.Add(2 * time.Second).Format(time.RFC3339Nano)}}})
	retrying := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Status.PendingRetrying })
	if retrying.Stats.StatusLevel != "retrying" || retrying.Status.RetryReason != "Model unavailable" || retrying.Stats.PendingRetryCount != 2 ||
		retrying.Status.RetryUntilMs != float64(t0.Add(17*time.Second).UnixMilli()) {
		t.Fatalf("retry = %s", describe(retrying))
	}

	clock.Set(t0.Add(30 * time.Second))
	expired, _ := src.Payload(testCid, defaultCfg)
	if expired.Status.PendingRetrying || expired.Stats.StatusLevel != "running" {
		t.Errorf("a retry that should have happened long ago no longer counts: %s", describe(expired))
	}

	f.change(func(f *fakeLS) {
		f.status = runStatusIdle
		f.steps[3] = step(3, "PLANNER_RESPONSE", "DONE")
		f.steps = append(f.steps, with(step(4, "ERROR_MESSAGE", "DONE"), "errorMessage", map[string]any{"error": map[string]any{
			"userErrorMessage": "The model is overloaded.", "shortError": "UNAVAILABLE (code 503): Model unavailable",
		}}))
	}, map[string]any{"executionStatus": map[string]any{}})
	failed := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Stats.StatusLevel == "error" })
	if failed.Status.LastErrorMsg != "UNAVAILABLE (code 503): Model unavailable" || failed.Status.PendingRetrying {
		t.Errorf("a final error message ends the turn in error: %s", describe(failed))
	}

	f.change(func(f *fakeLS) {
		f.status = "CASCADE_RUN_STATUS_RUNNING"
		f.gens = append(f.gens, call{maxCtx: 512000, steps: []int{3}, err: "Model unavailable", reason: "RETRY_REASON_API_RETRYABLE"}.json())
		f.steps = append(f.steps, step(5, "RUN_COMMAND", "RUNNING"))
	}, nil)
	tool := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Status.PendingKind == "tool" })
	if tool.Status.PendingRetrying || tool.Stats.StatusLevel != "running" || tool.View.Session.Errors != 1 {
		t.Errorf("a running tool after a failed call is not a retry: %s", describe(tool))
	}
}

func TestSourceStepErrorAndCancellation(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.gens = []map[string]any{call{in: 1000, out: 200, maxCtx: 512000, steps: []int{1}, ttft: "0.8s", stream: "1s"}.json()}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"), with(step(2, "PLANNER_RESPONSE", "ERROR"), "error", map[string]any{"shortError": "invalid tool call\nretry later", "fullError": "long trace"})}
	src, _ := follow(t, f)

	failed := payloadWhen(t, src, testCid, hasData)
	if failed.Stats.StatusLevel != "error" || failed.Status.LastErrorMsg != "invalid tool call retry later" {
		t.Errorf("a failed model step shows its short error on one line: %s", describe(failed))
	}

	f.change(func(f *fakeLS) {
		f.steps = append(f.steps, with(step(3, "PLANNER_RESPONSE", "ERROR"), "error", map[string]any{"shortError": "context canceled"}))
	}, nil)
	canceled := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Stats.StatusLevel != "error" })
	if canceled.Status.LastErrorMsg != "" || canceled.Stats.StatusLevel != "idle" {
		t.Errorf("a cancellation is not an error: %s", describe(canceled))
	}
}

func TestSourceCompaction(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.status = "CASCADE_RUN_STATUS_RUNNING"
	f.gens = []map[string]any{call{in: 90000, out: 200, maxCtx: 100000, steps: []int{1}, ttft: "0.8s", stream: "1s"}.json()}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"), step(2, "CHECKPOINT", "RUNNING")}
	src, _ := follow(t, f)

	compacting := payloadWhen(t, src, testCid, hasData)
	if !compacting.Stats.CompactingNow || compacting.Status.PendingKind != "compact" || !compacting.View.Ctx.Compacting {
		t.Fatalf("a running checkpoint is a compaction in progress: %s", describe(compacting))
	}

	f.change(func(f *fakeLS) {
		f.status = runStatusIdle
		f.compact = []int{3}
		f.steps[2] = with(step(2, "CHECKPOINT", "DONE"), "checkpoint", map[string]any{"sessionSummary": strings.Repeat("s", 2048)})
	}, nil)
	compacted := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Stats.CompactedPending })
	c := compacted.View.Ctx
	if compacted.Stats.CompactingNow || c.Level != "compacted" || c.SummaryKb != 2 || c.Compactions != 1 || compacted.Stats.CompactSummaryKb != 2 {
		t.Errorf("a finished compaction waits for the next call with its summary size: %+v", c)
	}
}

func TestSourceRewind(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.gens = []map[string]any{
		call{in: 1000, out: 100, maxCtx: 512000, steps: []int{1}, ttft: "1s", stream: "1s"}.json(),
		call{in: 1100, out: 100, maxCtx: 512000, steps: []int{3}, ttft: "1s", stream: "1s"}.json(),
		call{in: 1200, out: 100, maxCtx: 512000, steps: []int{5}, ttft: "1s", stream: "1s"}.json(),
	}
	f.steps = []map[string]any{
		step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE"),
		step(2, "USER_INPUT", "DONE"), step(3, "PLANNER_RESPONSE", "DONE"),
		step(4, "USER_INPUT", "DONE"), step(5, "PLANNER_RESPONSE", "DONE"),
	}
	src, _ := follow(t, f)
	full := payloadWhen(t, src, testCid, hasData)
	if full.Status.TotalGenerations != 3 || full.View.Session.Turns != 3 {
		t.Fatalf("before rewind = %s", describe(full))
	}

	f.change(func(f *fakeLS) {
		f.gens = f.gens[:1]
		f.steps = f.steps[:3]
	}, nil)

	rewound := payloadWhen(t, src, testCid, func(p *Payload) bool { return p.Status.TotalGenerations == 1 })
	if rewound.Status.TotalSteps != 3 || rewound.View.Session.Turns != 2 || rewound.View.Turn.Calls != 0 {
		t.Errorf("after rewind = %s, turn %+v", describe(rewound), rewound.View.Turn)
	}
}

func TestSourceContextBreakdownFromGeneratorMetadata(t *testing.T) {
	f := newFakeLS(t, testCid)
	g := call{in: 2000, cacheRead: 28000, out: 300, maxCtx: 512000, steps: []int{3}, ttft: "0.8s", stream: "1s"}.json()
	cwm := g["chatModel"].(map[string]any)["chatStartMetadata"].(map[string]any)["contextWindowMetadata"].(map[string]any)
	cwm["tokenBreakdown"] = map[string]any{
		"totalTokens": 30000,
		"groups": []any{
			map[string]any{"name": "System Prompt", "type": "TOKEN_TYPE_SYSTEM_PROMPT", "numTokens": 4000},
			map[string]any{"name": "Skills", "type": "TOKEN_TYPE_SKILLS", "numTokens": 2500},
			map[string]any{"name": "Rules", "type": "TOKEN_TYPE_RULES", "numTokens": 1000},
			map[string]any{
				"name": "Tools", "type": "TOKEN_TYPE_TOOLS", "numTokens": 6500,
				"children": []any{
					map[string]any{"name": "swarm", "numTokens": 1800},
					map[string]any{"name": "read_file", "numTokens": 700},
					map[string]any{"name": "mcp_chrome_snapshot", "numTokens": 4000},
				},
			},
			map[string]any{
				"name": "Chat Messages", "type": "TOKEN_TYPE_CHAT_MESSAGES", "numTokens": 16000,
				"children": []any{
					map[string]any{"name": "0: USER_INPUT", "numTokens": 200},
					map[string]any{"name": "1: GENERIC", "numTokens": 9800},
					map[string]any{"name": "2: GENERIC", "numTokens": 4500},
					map[string]any{"name": "3: PLANNER_RESPONSE", "numTokens": 1500},
				},
			},
		},
	}
	f.gens = []map[string]any{g}

	s1 := step(1, "VIEW_FILE", "DONE")
	s1Meta := s1["metadata"].(map[string]any)
	s1Meta["toolCall"] = map[string]any{"name": "read_file"}
	s1Meta["toolSource"] = "CORTEX_COMPONENT_SOURCE_BUILTIN"

	s2 := step(2, "GENERIC", "DONE")
	s2Meta := s2["metadata"].(map[string]any)
	s2Meta["toolCall"] = map[string]any{"name": "mcp_chrome_snapshot"}
	s2Meta["toolSource"] = "CORTEX_COMPONENT_SOURCE_MCP"

	f.steps = []map[string]any{
		step(0, "USER_INPUT", "DONE"),
		s1,
		s2,
		step(3, "PLANNER_RESPONSE", "DONE"),
	}

	src, _ := follow(t, f)
	p := payloadWhen(t, src, testCid, hasData)
	bd := p.View.Ctx.Breakdown
	if len(bd) != 5 {
		t.Fatalf("expected 5 groups (msg/mcp/tools/sys/cfg), got %+v", bd)
	}
	if bd[0].Key != "msg" || bd[0].Tokens != 16000 || !bd[0].Expandable || len(bd[0].Items) != 4 {
		t.Fatalf("unexpected msg group: %+v", bd[0])
	}
	if bd[0].Items[0].Name != "read_file · 读取文件" || bd[0].Items[0].Tokens != 9800 {
		t.Errorf("expected GENERIC step resolved via toolCall to read_file first: %+v", bd[0].Items)
	}
	if bd[0].Items[1].Name != "MCP · mcp_chrome_snapshot" || bd[0].Items[1].Tokens != 4500 {
		t.Errorf("expected MCP chat tool resolved second: %+v", bd[0].Items)
	}
	if bd[1].Key != "mcp" || bd[1].Tokens != 4000 || !bd[1].Expandable || len(bd[1].Items) != 1 {
		t.Errorf("expected MCP tool split out of Tools: %+v", bd[1])
	}
	if bd[2].Key != "tools" || bd[2].Tokens != 2500 || !bd[2].Expandable || len(bd[2].Items) != 2 || bd[2].Items[0].Name != "swarm" {
		t.Errorf("expected builtin tools kept sorted without truncation: %+v", bd[2])
	}
	if bd[3].Key != "sys" || bd[3].Tokens != 4000 || bd[3].Expandable {
		t.Errorf("expected non-expandable system prompt group: %+v", bd[3])
	}
	if bd[4].Key != "cfg" || bd[4].Tokens != 3500 || !bd[4].Expandable || len(bd[4].Items) != 2 || bd[4].Items[0].Name != "Skills" {
		t.Errorf("expected Skills + Rules merged into cfg group: %+v", bd[4])
	}
}

func TestSourceContextBreakdownFallsBackToStream(t *testing.T) {
	f := newFakeLS(t, testCid)
	f.gens = []map[string]any{call{in: 1000, cacheRead: 19000, out: 100, maxCtx: 512000, steps: []int{1}, ttft: "0.5s", stream: "1s"}.json()}
	f.steps = []map[string]any{step(0, "USER_INPUT", "DONE"), step(1, "PLANNER_RESPONSE", "DONE")}
	src, _ := follow(t, f)
	_ = payloadWhen(t, src, testCid, hasData)

	f.change(func(*fakeLS) {}, map[string]any{
		"contextWindowUsage": map[string]any{
			"estimatedTokensUsed": 20000,
			"tokenGroups": []any{
				map[string]any{"name": "Chat Messages", "type": "TOKEN_TYPE_CHAT_MESSAGES", "numTokens": 12000},
				map[string]any{"name": "Tools", "type": "TOKEN_TYPE_TOOLS", "numTokens": 5000},
				map[string]any{"name": "System Prompt", "type": "TOKEN_TYPE_SYSTEM_PROMPT", "numTokens": 3000},
			},
		},
	})

	p := payloadWhen(t, src, testCid, func(p *Payload) bool { return len(p.View.Ctx.Breakdown) == 3 })
	bd := p.View.Ctx.Breakdown
	if bd[0].Key != "msg" || bd[0].Tokens != 12000 || bd[0].Expandable {
		t.Errorf("stream fallback should be non-expandable: %+v", bd[0])
	}
	if bd[1].Key != "tools" || bd[1].Tokens != 5000 || bd[2].Key != "sys" || bd[2].Tokens != 3000 {
		t.Errorf("unexpected stream fallback groups: %+v", bd)
	}
}
