package metrics

import (
	"math"
	"sort"
)

type CallView struct {
	N     int     `json:"n"`
	Turn  int     `json:"turn"`
	Model string  `json:"model"`
	TTFT  float64 `json:"ttft"`
	Gen   float64 `json:"gen"`
	Dur   float64 `json:"dur"`
	Tps   float64 `json:"tps"`
	In    float64 `json:"in"`
	CW    float64 `json:"cw"`
	CR    float64 `json:"cr"`
	Ctx   float64 `json:"ctx"`
	Out   float64 `json:"out"`
	Think float64 `json:"think"`
	Resp  float64 `json:"resp"`
	Hit   float64 `json:"hit"`
	Cp    bool    `json:"cp,omitempty"`
	At    float64 `json:"at,omitempty"`
}

type ContextView struct {
	Used        float64 `json:"used"`
	Limit       float64 `json:"limit"`
	LimitSrc    string  `json:"limitSrc"`
	Pct         float64 `json:"pct"`
	Level       string  `json:"level"`
	Remain      float64 `json:"remain"`
	Cached      float64 `json:"cached"`
	Fresh       float64 `json:"fresh"`
	Compacted   bool    `json:"compacted"`
	SummaryKb   float64 `json:"summaryKb"`
	Compacting  bool    `json:"compacting"`
	Compactions int     `json:"compactions"`
	Peak        float64 `json:"peak"`
	Growth      float64 `json:"growth"`
	GrowthN     int     `json:"growthN"`
	CallsLeft   int     `json:"callsLeft"`
}

type TurnView struct {
	Index     int     `json:"index"`
	Calls     int     `json:"calls"`
	Steps     int64   `json:"steps"`
	StartedMs float64 `json:"startedMs"`
	In        float64 `json:"in"`
	CR        float64 `json:"cr"`
	Out       float64 `json:"out"`
	Think     float64 `json:"think"`
	Resp      float64 `json:"resp"`
	StreamDur float64 `json:"streamDur"`
	Dur       float64 `json:"dur"`
	Hit       float64 `json:"hit"`
}

type SessionView struct {
	Turns     int     `json:"turns"`
	Calls     int     `json:"calls"`
	Errors    int     `json:"errors"`
	In        float64 `json:"in"`
	CW        float64 `json:"cw"`
	CR        float64 `json:"cr"`
	Prompt    float64 `json:"prompt"`
	Out       float64 `json:"out"`
	Think     float64 `json:"think"`
	Resp      float64 `json:"resp"`
	StreamDur float64 `json:"streamDur"`
	Dur       float64 `json:"dur"`
	Hit       float64 `json:"hit"`
	TTFTMed   float64 `json:"ttftMed"`
	TpsMed    float64 `json:"tpsMed"`
}

type View struct {
	CacheMode string      `json:"cacheMode"`
	Last      CallView    `json:"last"`
	Ctx       ContextView `json:"ctx"`
	Turn      TurnView    `json:"turn"`
	Session   SessionView `json:"session"`
	History   []CallView  `json:"history"`
}

const HistoryLimit = 64

func round1(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return toFixedNum(v, 1)
}

func round2(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return toFixedNum(v, 2)
}

func pctOf(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return round1(part / whole * 100)
}

func callTps(g GenItem) float64 {
	if g.Stream >= minStreamDurSec && g.OutTok > 0 {
		if v := g.OutTok / g.Stream; v <= maxSanityStreamTps {
			return v
		}
	}
	if g.TotDur > 0 {
		return g.OutTok / g.TotDur
	}
	return 0
}

func turnOf(stepIdx float64, userStepIdxs []int64) int {
	if stepIdx < 0 || len(userStepIdxs) == 0 {
		return 0
	}
	i := sort.Search(len(userStepIdxs), func(i int) bool { return float64(userStepIdxs[i]) > stepIdx })
	return i
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func compactionDrop(prev, cur float64) bool {
	return prev > 0 && cur < prev*0.6
}

func makeCallView(n int, g GenItem, userStepIdxs []int64) CallView {
	return CallView{
		N:     n,
		Turn:  turnOf(g.MaxStepIdx, userStepIdxs),
		Model: g.ModelName,
		TTFT:  round2(g.TTFT),
		Gen:   round2(g.Stream),
		Dur:   round2(g.TotDur),
		Tps:   round1(callTps(g)),
		In:    g.InTok,
		CW:    g.CacheWrite,
		CR:    g.CacheRead,
		Ctx:   g.Ctx,
		Out:   g.OutTok,
		Think: g.ThinkTok,
		Resp:  g.RespTok,
		Hit:   pctOf(g.CacheRead, g.Ctx),
	}
}

type viewInput struct {
	valid           []GenItem
	all             []GenItem
	userStepIdxs    []int64
	checkpointIdxs  []int64
	turnSlice       []GenItem
	turnSteps       int64
	turnStartedMs   float64
	contextLimit    float64
	limitSrc        string
	compactedPend   bool
	compactSummaryK float64
	compactingNow   bool
	cacheMode       string
	stepStartMs     map[int64]float64
}

func BuildView(in viewInput) *View {
	valid := in.valid
	n := len(valid)
	calls := make([]CallView, n)
	cpPos := 0
	for i, g := range valid {
		calls[i] = makeCallView(i+1, g, in.userStepIdxs)
		calls[i].At = in.stepStartMs[int64(g.MaxStepIdx)]
		if i == 0 {
			continue
		}
		prevStep := valid[i-1].MaxStepIdx
		for cpPos < len(in.checkpointIdxs) && float64(in.checkpointIdxs[cpPos]) <= prevStep {
			cpPos++
		}
		if cpPos < len(in.checkpointIdxs) && g.MaxStepIdx >= 0 && float64(in.checkpointIdxs[cpPos]) <= g.MaxStepIdx {
			calls[i].Cp = true
		}
		if compactionDrop(valid[i-1].Ctx, g.Ctx) {
			calls[i].Cp = true
		}
	}
	last := calls[n-1]
	lastG := valid[n-1]

	limit := in.contextLimit
	used := lastG.Ctx
	pct := pctOf(used, limit)
	level := "ok"
	switch {
	case in.compactedPend:
		level = "compacted"
	case pct >= 90:
		level = "crit"
	case pct >= 75:
		level = "warn"
	}
	peak := 0.0
	for _, g := range valid {
		peak = math.Max(peak, g.Ctx)
	}
	segStart := 0
	for i := n - 1; i > 0; i-- {
		if calls[i].Cp {
			segStart = i
			break
		}
	}
	from := n - 11
	if from < segStart {
		from = segStart
	}
	growth, growthN, callsLeft := 0.0, 0, -1
	if deltas := n - 1 - from; deltas >= 3 {
		growth = (valid[n-1].Ctx - valid[from].Ctx) / float64(deltas)
		growthN = deltas
		if growth > 0 && !in.compactedPend {
			callsLeft = int(math.Max(0, limit-used) / growth)
		}
	}
	ctx := ContextView{
		Used:        used,
		Limit:       limit,
		LimitSrc:    in.limitSrc,
		Pct:         pct,
		Level:       level,
		Remain:      math.Max(0, limit-used),
		Cached:      lastG.CacheRead,
		Fresh:       lastG.InTok + lastG.CacheWrite,
		Compacted:   in.compactedPend,
		SummaryKb:   in.compactSummaryK,
		Compacting:  in.compactingNow,
		Compactions: len(in.checkpointIdxs),
		Peak:        peak,
		Growth:      round1(growth),
		GrowthN:     growthN,
		CallsLeft:   callsLeft,
	}

	turn := TurnView{Index: len(in.userStepIdxs), Calls: len(in.turnSlice), Steps: in.turnSteps, StartedMs: in.turnStartedMs}
	for _, g := range in.turnSlice {
		turn.In += g.InTok + g.CacheWrite
		turn.CR += g.CacheRead
		turn.Out += g.OutTok
		turn.Think += g.ThinkTok
		turn.Resp += g.RespTok
		turn.StreamDur += g.Stream
		turn.Dur += g.TotDur
	}
	turn.Hit = pctOf(turn.CR, turn.In+turn.CR)
	turn.StreamDur = round2(turn.StreamDur)
	turn.Dur = round2(turn.Dur)

	sess := SessionView{Turns: len(in.userStepIdxs), Calls: n}
	ttfts := make([]float64, 0, n)
	tpss := make([]float64, 0, n)
	for _, g := range valid {
		sess.In += g.InTok
		sess.CW += g.CacheWrite
		sess.CR += g.CacheRead
		sess.Out += g.OutTok
		sess.Think += g.ThinkTok
		sess.Resp += g.RespTok
		sess.StreamDur += g.Stream
		sess.Dur += g.TotDur
		if g.TTFT > 0 {
			ttfts = append(ttfts, g.TTFT)
		}
		if t := callTps(g); t > 0 {
			tpss = append(tpss, t)
		}
	}
	for _, g := range in.all {
		if g.apiFailure() {
			sess.Errors++
		}
	}
	sess.Prompt = sess.In + sess.CW + sess.CR
	sess.Hit = pctOf(sess.CR, sess.Prompt)
	sess.StreamDur = round2(sess.StreamDur)
	sess.Dur = round2(sess.Dur)
	sess.TTFTMed = round2(median(ttfts))
	sess.TpsMed = round1(median(tpss))

	hist := calls
	if len(hist) > HistoryLimit {
		hist = hist[len(hist)-HistoryLimit:]
	}
	cacheMode := "session"
	if in.cacheMode == "step" {
		cacheMode = "step"
	}
	return &View{
		CacheMode: cacheMode,
		Last:      last,
		Ctx:       ctx,
		Turn:      turn,
		Session:   sess,
		History:   append([]CallView(nil), hist...),
	}
}
