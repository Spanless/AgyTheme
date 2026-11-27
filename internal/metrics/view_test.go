package metrics

import "testing"

func TestIsCancelText(t *testing.T) {
	for s, want := range map[string]bool{
		"context canceled": true,
		"rpc error: code = Canceled desc = context canceled": true,
		"Cancelled by user":                            true,
		"API request failed (503 Service Unavailable)": false,
	} {
		if IsCancelText(s) != want {
			t.Errorf("IsCancelText(%q) != %v", s, want)
		}
	}
}

func TestTurnSummaries(t *testing.T) {
	turns := BuildTurnSummaries([]GenItem{{
		Idx: 0, InTok: 1000, OutTok: 200, CacheRead: 9000, Ctx: 10000, MaxCtx: 512000, MaxStepIdx: 3,
		ModelName: "Gemini 4 Hydrogen (XHigh)", ThinkTok: 50, RespTok: 150, TTFT: 0.8, Stream: 1.0, TotDur: 1.8,
	}}, []int64{1}, 0, 256000)
	if len(turns) != 1 || turns[0].TurnIndex != 1 || turns[0].Tps != 200 || turns[0].CacheRate != "90.0%" {
		t.Errorf("turns = %+v", turns)
	}
	if FmtTok(1500) != "1.5k" || FmtTok(1500000) != "1.50M" {
		t.Errorf("FmtTok: %s %s", FmtTok(1500), FmtTok(1500000))
	}
}

func gen(step, ctx, cr, out float64) GenItem {
	return GenItem{InTok: ctx - cr, CacheRead: cr, Ctx: ctx, OutTok: out, ThinkTok: out / 2, RespTok: out / 2,
		MaxStepIdx: step, MaxCtx: 100000, ModelName: "M", TTFT: 2, Stream: 4, TotDur: 6}
}

func TestBuildView(t *testing.T) {
	valid := []GenItem{
		gen(1, 20000, 0, 400),
		gen(3, 24000, 20000, 400),
		gen(5, 28000, 24000, 400),
		gen(11, 50000, 28000, 400),
		gen(13, 9000, 0, 400),
		gen(14, 11000, 9000, 400),
		gen(15, 13000, 11000, 400),
		gen(16, 15000, 13000, 400),
	}
	errRow := GenItem{APIError: true, ErrMsg: "503 Service Unavailable"}
	cancelRow := GenItem{APIError: true, ErrMsg: "context canceled"}
	v := BuildView(viewInput{
		valid:          valid,
		all:            append(append([]GenItem{}, valid...), errRow, cancelRow),
		userStepIdxs:   []int64{0, 10},
		checkpointIdxs: []int64{12},
		turnSlice:      valid[3:],
		turnSteps:      9,
		turnStartedMs:  1_700_000_000_000,
		contextLimit:   100000,
		limitSrc:       "model",
		cacheMode:      "",
	})
	if v.CacheMode != "session" {
		t.Errorf("default cache mode = %q", v.CacheMode)
	}
	if v.Last.N != 8 || v.Last.Turn != 2 || v.Last.Hit != 86.7 || v.Last.Tps != 100 {
		t.Errorf("last = %+v", v.Last)
	}
	if v.History[0].Turn != 1 || v.History[3].Turn != 2 {
		t.Errorf("turn mapping: %+v", v.History)
	}
	if !v.History[4].Cp || v.History[3].Cp || v.History[5].Cp {
		t.Errorf("compaction marks: %+v", v.History)
	}
	c := v.Ctx
	if c.Used != 15000 || c.Pct != 15 || c.Level != "ok" || c.Remain != 85000 || c.Cached != 13000 || c.Fresh != 2000 {
		t.Errorf("ctx = %+v", c)
	}
	if c.Growth != 2000 || c.GrowthN != 3 || c.CallsLeft != 42 || c.Peak != 50000 || c.Compactions != 1 {
		t.Errorf("growth = %+v", c)
	}
	if v.Turn.Index != 2 || v.Turn.Calls != 5 || v.Turn.Out != 2000 || v.Turn.Dur != 30 || v.Turn.StreamDur != 20 || v.Turn.Steps != 9 {
		t.Errorf("turn = %+v", v.Turn)
	}
	s := v.Session
	if s.Turns != 2 || s.Calls != 8 || s.Errors != 1 || s.Out != 3200 || s.StreamDur != 32 || s.TTFTMed != 2 || s.TpsMed != 100 {
		t.Errorf("session = %+v", s)
	}
	if s.Hit != pctOf(s.CR, s.Prompt) || s.Prompt != s.In+s.CR {
		t.Errorf("session hit = %+v", s)
	}
}

func TestBuildViewEdgeCases(t *testing.T) {
	many := make([]GenItem, HistoryLimit+10)
	for i := range many {
		many[i] = gen(float64(i+1), float64(1000*(i+1)), 0, 10)
	}
	v := BuildView(viewInput{valid: many, all: many, userStepIdxs: []int64{0}, contextLimit: 1000000, cacheMode: "session"})
	if len(v.History) != HistoryLimit || v.History[len(v.History)-1].N != len(many) || v.CacheMode != "session" {
		t.Errorf("history = %d last=%+v", len(v.History), v.History[len(v.History)-1])
	}
	shrink := []GenItem{gen(1, 90000, 0, 1), gen(2, 92000, 0, 1), gen(3, 94000, 0, 1), gen(4, 30000, 0, 1)}
	v = BuildView(viewInput{valid: shrink, all: shrink, contextLimit: 100000, compactedPend: true})
	if !v.History[3].Cp || v.Ctx.Level != "compacted" || v.Ctx.CallsLeft != -1 || v.Ctx.GrowthN != 0 {
		t.Errorf("compacted view = %+v / %+v", v.History[3], v.Ctx)
	}
	if v.Last.Turn != 0 || v.Turn.Index != 0 || v.Turn.Calls != 0 {
		t.Errorf("no-user-step turn = %+v %+v", v.Last, v.Turn)
	}
}

func TestJSNumberHelpers(t *testing.T) {
	cases := []struct {
		x    float64
		d    int
		want string
	}{{1.005, 2, "1.00"}, {1.5, 0, "2"}, {2.5, 0, "3"}, {0.05, 1, "0.1"}, {98.95, 1, "99.0"}, {-1.25, 1, "-1.3"}, {0, 1, "0.0"}}
	for _, c := range cases {
		if got := toFixed(c.x, c.d); got != c.want {
			t.Errorf("toFixed(%v,%d) = %s, want %s", c.x, c.d, got, c.want)
		}
	}
	if jsRound(-2.5) != -2 || jsRound(2.5) != 3 {
		t.Errorf("jsRound mismatch")
	}
}
