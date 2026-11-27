package metrics

import (
	"fmt"
	"math"
	"regexp"
	"slices"
)

var cascadeIDRe = regexp.MustCompile(`(?i)^[0-9a-f-]{36}$`)

func IsCascadeID(s string) bool { return cascadeIDRe.MatchString(s) }

type ReaderConfig struct {
	ManualContextLimit   float64
	FallbackContextLimit float64
	TelemetryCacheMode   string
}

type Status struct {
	PendingKind      string  `json:"pendingKind"`
	PendingRetrying  bool    `json:"pendingRetrying"`
	RetryReason      string  `json:"retryReason"`
	RetryUntilMs     float64 `json:"retryUntilMs,omitempty"`
	LastErrorMsg     string  `json:"lastErrorMsg"`
	TotalSteps       int64   `json:"totalSteps"`
	TotalGenerations int     `json:"totalGenerations"`
	LastModelLabel   string  `json:"lastModelLabel"`
}

type Stats struct {
	GenIdx             int64   `json:"genIdx"`
	Tps                float64 `json:"tps"`
	StreamTps          float64 `json:"streamTps"`
	E2ETps             float64 `json:"e2eTps"`
	TTFT               float64 `json:"ttft"`
	StreamDur          float64 `json:"streamDur"`
	TotDur             float64 `json:"totDur"`
	Ctx                float64 `json:"ctx"`
	InTok              float64 `json:"inTok"`
	CacheWrite         float64 `json:"cacheWrite"`
	CacheRead          float64 `json:"cacheRead"`
	OutTok             float64 `json:"outTok"`
	ContextLimit       float64 `json:"contextLimit"`
	LimitStr           string  `json:"limitStr"`
	RemainCtx          float64 `json:"remainCtx"`
	CtxPct             float64 `json:"ctxPct"`
	CtxClass           string  `json:"ctxClass"`
	CompactedPending   bool    `json:"compactedPending"`
	CompactSummaryKb   float64 `json:"compactSummaryKb"`
	CompactingNow      bool    `json:"compactingNow"`
	GeneratingPending  bool    `json:"generatingPending"`
	LastModel          string  `json:"lastModel"`
	LastCacheRead      float64 `json:"lastCacheRead"`
	PendingKind        string  `json:"pendingKind"`
	PendingStepIdx     int64   `json:"pendingStepIdx"`
	PendingStartedAtMs float64 `json:"pendingStartedAtMs"`
	PendingPayloadLen  int64   `json:"pendingPayloadLen"`
	PendingRetrying    bool    `json:"pendingRetrying"`
	PendingRetryCount  int     `json:"pendingRetryCount"`
	PendingRetryReason string  `json:"pendingRetryReason"`
	LastErrorMsg       string  `json:"lastErrorMsg"`
	StatusLevel        string  `json:"statusLevel"`
	StepCacheRate      string  `json:"stepCacheRate"`
	SessionCacheRate   string  `json:"sessionCacheRate"`
}

type Payload struct {
	Cid       string        `json:"cid"`
	CascadeID string        `json:"cascadeId"`
	HasData   bool          `json:"hasData"`
	Sig       string        `json:"sig,omitempty"`
	View      *View         `json:"view,omitempty"`
	Turns     []TurnSummary `json:"turns,omitempty"`
	Status    *Status       `json:"status,omitempty"`
	Stats     *Stats        `json:"stats,omitempty"`
}

const (
	retryGraceMs     = 20000.0
	retryPostDelayMs = 15000.0
	retryMaxWindowMs = 180000.0
)

type modelRetry struct {
	Attempt   int
	RetryAtMs float64
	SeenMs    float64
}

func (r *modelRetry) untilMs() float64 {
	win := retryGraceMs
	if r.RetryAtMs > r.SeenMs {
		win = r.RetryAtMs - r.SeenMs + retryPostDelayMs
	}
	return r.SeenMs + min(win, retryMaxWindowMs)
}

type turnInfo struct {
	StartStep int64
	StartMs   float64
}

type convState struct {
	gens            []GenItem
	stepBase        int64
	steps           []stepInfo
	stepTotal       int64
	turns           []turnInfo
	compactedAt     []int64
	retry           *modelRetry
	streamBreakdown *rawTokenBreakdown
}

func (s *convState) step(idx int64) *stepInfo {
	if i := idx - s.stepBase; i >= 0 && i < int64(len(s.steps)) {
		return &s.steps[i]
	}
	return nil
}

type activeStep struct {
	idx int64
	*stepInfo
}

func buildPayload(cid, sig string, st *convState, cfg ReaderConfig, nowMs float64) *Payload {
	empty := &Payload{Cid: cid, CascadeID: cid, Sig: sig}
	manualLim := cfg.ManualContextLimit
	fallbackLim := cfg.FallbackContextLimit
	if fallbackLim <= 0 {
		fallbackLim = 256000
	}
	cacheMode := "session"
	if cfg.TelemetryCacheMode == "step" {
		cacheMode = "step"
	}

	var items, valid []GenItem
	var totalIn, totalCacheWrite, totalCache float64
	items = st.gens
	for _, p := range items {
		totalIn += p.InTok
		totalCacheWrite += p.CacheWrite
		totalCache += p.CacheRead
		if p.Ctx > 0 || p.OutTok > 0 {
			valid = append(valid, p)
		}
	}
	if len(valid) == 0 {
		return empty
	}

	userStepMs := map[int64]float64{}
	for _, t := range st.turns {
		userStepMs[t.StartStep] = t.StartMs
	}
	var checkpointIdxs []int64
	for _, c := range st.compactedAt {
		checkpointIdxs = append(checkpointIdxs, c-1)
	}
	type stepRef struct {
		idx int64
		*stepInfo
	}
	var lastCheckpoint, lastModelStep, latestNonUser *stepRef
	for i := range st.steps {
		s := stepRef{st.stepBase + int64(i), &st.steps[i]}
		switch s.Type {
		case stepTypeUserInput:
			if _, ok := userStepMs[s.idx]; !ok {
				userStepMs[s.idx] = s.StartMs
			}
		case stepTypeCheckpoint:
			if s.Status == stepStatusDone {
				lastCheckpoint = &s
				checkpointIdxs = append(checkpointIdxs, s.idx)
			}
		}
		switch s.Type {
		case stepTypePlannerResponse, stepTypeErrorMessage, stepTypeCheckpoint:
			lastModelStep = &s
		}
		if s.Type != stepTypeUserInput {
			latestNonUser = &s
		}
	}
	userStepIdxs := make([]int64, 0, len(userStepMs))
	for idx := range userStepMs {
		userStepIdxs = append(userStepIdxs, idx)
	}
	slices.Sort(userStepIdxs)
	slices.Sort(checkpointIdxs)
	checkpointIdxs = slices.Compact(checkpointIdxs)
	lastUserStepIdx := int64(-1)
	if n := len(userStepIdxs); n > 0 {
		lastUserStepIdx = userStepIdxs[n-1]
	}
	turnStartedMs := userStepMs[lastUserStepIdx]
	totalSteps := max(st.stepTotal, st.stepBase+int64(len(st.steps)))

	var activeRows []activeStep
	var rawTurnSteps, turnAllSteps int64
	for i := len(st.steps) - 1; i >= 0; i-- {
		s := &st.steps[i]
		idx := st.stepBase + int64(i)
		if !s.settled() && len(activeRows) < 10 {
			activeRows = append(activeRows, activeStep{idx, s})
		}
		if idx > lastUserStepIdx && s.Type != stepTypeUserInput {
			turnAllSteps++
			if s.Type == stepTypePlannerResponse {
				rawTurnSteps++
			}
		}
	}

	var turnSlice []GenItem
	anyStep := false
	for _, it := range valid {
		if it.MaxStepIdx >= 0 {
			anyStep = true
			break
		}
	}
	if lastUserStepIdx >= 0 && anyStep {
		for i := len(valid) - 1; i >= 0; i-- {
			si := valid[i].MaxStepIdx
			if si > float64(lastUserStepIdx) {
				turnSlice = append([]GenItem{valid[i]}, turnSlice...)
			} else if si >= 0 {
				break
			}
		}
	} else if rawTurnSteps > 0 {
		start := max(len(valid)-int(rawTurnSteps), 0)
		turnSlice = valid[start:]
	}

	last := valid[len(valid)-1]
	native := 0.0
	for i := len(valid) - 1; native == 0 && i >= 0; i-- {
		native = valid[i].MaxCtx
	}
	for i := len(items) - 1; native == 0 && i >= 0; i-- {
		native = items[i].MaxCtx
	}
	contextLimit := fallbackLim
	limitSrc := "default"
	if manualLim > 0 {
		contextLimit = manualLim
		limitSrc = "manual"
	} else if native > 0 {
		contextLimit = native
		limitSrc = "model"
	}
	streamTps := 0.0
	if last.Stream >= minStreamDurSec {
		streamTps = last.OutTok / last.Stream
	}
	e2eTps := 0.0
	if last.TotDur > 0 {
		e2eTps = last.OutTok / last.TotDur
	}
	displayTps := e2eTps
	if streamTps > 0 && streamTps <= maxSanityStreamTps {
		displayTps = streamTps
	}
	ctxPct := jsRound(last.Ctx / contextLimit * 100)
	ctxClass := "ag-ctx-ok"
	if ctxPct >= 90 {
		ctxClass = "ag-ctx-crit"
	} else if ctxPct >= 75 {
		ctxClass = "ag-ctx-warn"
	}
	lastCacheRate := "0.0"
	if last.Ctx > 0 {
		lastCacheRate = toFixed(last.CacheRead/last.Ctx*100, 1)
	}
	totalPrompt := totalIn + totalCacheWrite + totalCache
	sessionCacheRate := "0.0"
	if totalPrompt > 0 {
		sessionCacheRate = toFixed(totalCache/totalPrompt*100, 1)
	}
	remainCtx := math.Max(0, contextLimit-last.Ctx)
	compactedPending := false
	if lastCheckpoint != nil {
		if last.MaxStepIdx >= 0 {
			compactedPending = float64(lastCheckpoint.idx) > last.MaxStepIdx
		} else {
			compactedPending = lastCheckpoint.CpMetaIdx > float64(last.Idx)
		}
	}
	compactSummaryKb := 0.0
	if compactedPending && lastCheckpoint.SummaryBytes > 0 {
		compactSummaryKb = toFixedNum(float64(lastCheckpoint.SummaryBytes)/1024, 1)
	}
	var activeCompact *activeStep
	for i := range activeRows {
		if activeRows[i].Type == stepTypeCheckpoint && activeRows[i].Status != stepStatusWaiting {
			activeCompact = &activeRows[i]
			break
		}
	}
	compactingNow := activeCompact != nil
	var nonUserActive []activeStep
	for _, a := range activeRows {
		if a.Type == stepTypeUserInput || a.Type == stepTypeErrorMessage {
			continue
		}
		if lastUserStepIdx >= 0 && a.idx <= lastUserStepIdx {
			continue
		}
		afterLastGen := last.MaxStepIdx < 0 ||
			(a.Type == stepTypePlannerResponse && float64(a.idx) > last.MaxStepIdx) ||
			(a.Type != stepTypePlannerResponse && float64(a.idx) >= last.MaxStepIdx)
		if afterLastGen {
			nonUserActive = append(nonUserActive, a)
		}
	}
	latestActive := activeCompact
	if latestActive == nil && len(nonUserActive) > 0 {
		latestActive = &nonUserActive[0]
	}
	waitingForUser := latestActive != nil && !compactingNow && latestActive.Status == stepStatusWaiting
	generatingPending := compactingNow || (latestActive != nil && !waitingForUser)
	pendingKind := "idle"
	pendingStepIdx := int64(-1)
	pendingStartedAtMs := 0.0
	var pendingPayloadLen int64
	pendingRetryErr := ""
	if latestActive != nil {
		if waitingForUser {
			pendingKind = "user"
		} else {
			switch latestActive.Type {
			case stepTypeCheckpoint:
				pendingKind = "compact"
			case stepTypePlannerResponse:
				pendingKind = "llm"
			default:
				pendingKind = "tool"
			}
		}
		pendingStepIdx = latestActive.idx
		pendingStartedAtMs = latestActive.StartMs
		pendingPayloadLen = int64(latestActive.Bytes)
		pendingRetryErr = latestActive.RetryErr
	}

	var latestAnyGen *GenItem
	if len(items) > 0 {
		latestAnyGen = &items[len(items)-1]
	}
	tailRetryErrCount := 0
	tailRetryErrMsg := ""
	for i := len(items) - 1; i >= 0; i-- {
		it := &items[i]
		if it.apiFailure() &&
			(it.MaxStepIdx < 0 || it.MaxStepIdx >= float64(lastUserStepIdx)) &&
			(pendingStepIdx < 0 || it.MaxStepIdx < 0 || it.MaxStepIdx >= float64(pendingStepIdx)) {
			tailRetryErrCount++
			if tailRetryErrMsg == "" {
				tailRetryErrMsg = it.ErrMsg
			}
		} else {
			break
		}
	}

	step17InTail := latestNonUser != nil && latestNonUser.Type == stepTypeErrorMessage &&
		(lastUserStepIdx < 0 || latestNonUser.idx >= lastUserStepIdx) &&
		(last.MaxStepIdx < 0 || float64(latestNonUser.idx) > last.MaxStepIdx)
	step17Raw := ""
	if step17InTail {
		step17Raw = oneLine(latestNonUser.ErrText, 200)
	}
	step17TailActive := step17InTail && !IsCancelText(step17Raw)
	step17Msg := ""
	if step17TailActive {
		step17Msg = step17Raw
	}
	canRetryStep := generatingPending && pendingKind != "tool"
	var liveRetry *modelRetry
	if canRetryStep && st.retry != nil && st.retry.Attempt > 0 && nowMs < st.retry.untilMs() {
		liveRetry = st.retry
	}
	pendingRetrying := canRetryStep && (tailRetryErrCount > 0 || liveRetry != nil)
	retryUntilMs := 0.0
	if pendingRetrying && tailRetryErrCount == 0 && liveRetry != nil {
		retryUntilMs = liveRetry.untilMs()
	}
	pendingRetryCount := tailRetryErrCount
	if liveRetry != nil && liveRetry.Attempt > pendingRetryCount {
		pendingRetryCount = liveRetry.Attempt
	}
	pendingRetryReason := ""
	if pendingRetrying {
		switch {
		case pendingRetryErr != "":
			pendingRetryReason = oneLine(pendingRetryErr, 200)
		case tailRetryErrMsg != "":
			pendingRetryReason = tailRetryErrMsg
		default:
			pendingRetryReason = step17Msg
		}
	}

	stepErrMsg := ""
	if lastModelStep != nil && lastModelStep.idx >= lastUserStepIdx {
		if lastModelStep.Status == stepStatusError {
			raw := "步骤执行失败 (ERROR)"
			if t := oneLine(lastModelStep.ErrText, 180); t != "" {
				raw = t
			}
			if !IsCancelText(raw) {
				stepErrMsg = raw
			}
		} else if step17TailActive {
			stepErrMsg = step17Msg
			if stepErrMsg == "" {
				stepErrMsg = "API 调用失败 (ERROR_MESSAGE)"
			}
		}
	}
	latestGenErrored := !generatingPending && latestAnyGen != nil && latestAnyGen.apiFailure() &&
		(lastUserStepIdx < 0 || latestAnyGen.MaxStepIdx < 0 || latestAnyGen.MaxStepIdx >= float64(lastUserStepIdx))
	lastErrorMsg := ""
	if !generatingPending && !waitingForUser {
		lastErrorMsg = stepErrMsg
		if lastErrorMsg == "" && latestGenErrored {
			lastErrorMsg = latestAnyGen.ErrMsg
		}
	}
	if IsCancelText(lastErrorMsg) {
		lastErrorMsg = ""
	}

	statusLevel := "idle"
	switch {
	case lastErrorMsg != "":
		statusLevel = "error"
	case pendingRetrying:
		statusLevel = "retrying"
	case generatingPending:
		statusLevel = "running"
	}
	turns := BuildTurnSummaries(valid, userStepIdxs, manualLim, fallbackLim)
	for i := range turns {
		turns[i].StartedMs = userStepMs[turns[i].StartStepIdx]
	}
	breakdown, breakdownTotal := buildContextBreakdown(st)
	view := BuildView(viewInput{
		stepStartMs:     historyStepStartMs(st, valid),
		valid:           valid,
		all:             items,
		userStepIdxs:    userStepIdxs,
		checkpointIdxs:  checkpointIdxs,
		turnSlice:       turnSlice,
		turnSteps:       turnAllSteps,
		turnStartedMs:   turnStartedMs,
		contextLimit:    contextLimit,
		limitSrc:        limitSrc,
		compactedPend:   compactedPending,
		compactSummaryK: compactSummaryKb,
		compactingNow:   compactingNow,
		cacheMode:       cacheMode,
		breakdown:       breakdown,
		breakdownTotal:  breakdownTotal,
	})

	return &Payload{
		Cid:       cid,
		CascadeID: cid,
		HasData:   true,
		Sig:       sig,
		View:      view,
		Turns:     nonNilTurns(turns),
		Status: &Status{
			PendingKind:      pendingKind,
			PendingRetrying:  pendingRetrying,
			RetryReason:      pendingRetryReason,
			RetryUntilMs:     retryUntilMs,
			LastErrorMsg:     lastErrorMsg,
			TotalSteps:       totalSteps,
			TotalGenerations: len(valid),
			LastModelLabel:   last.ModelName,
		},
		Stats: &Stats{
			GenIdx:             last.Idx + 1,
			Tps:                jsRound(displayTps),
			StreamTps:          toFixedNum(streamTps, 1),
			E2ETps:             toFixedNum(e2eTps, 1),
			TTFT:               toFixedNum(last.TTFT, 2),
			StreamDur:          toFixedNum(last.Stream, 2),
			TotDur:             toFixedNum(last.TotDur, 2),
			Ctx:                last.Ctx,
			InTok:              last.InTok,
			CacheWrite:         last.CacheWrite,
			CacheRead:          last.CacheRead,
			OutTok:             last.OutTok,
			ContextLimit:       contextLimit,
			LimitStr:           FormatContextLimitLabel(contextLimit),
			RemainCtx:          remainCtx,
			CtxPct:             ctxPct,
			CtxClass:           ctxClass,
			CompactedPending:   compactedPending,
			CompactSummaryKb:   compactSummaryKb,
			CompactingNow:      compactingNow,
			GeneratingPending:  generatingPending,
			LastModel:          last.ModelName,
			LastCacheRead:      last.CacheRead,
			PendingKind:        pendingKind,
			PendingStepIdx:     pendingStepIdx,
			PendingStartedAtMs: pendingStartedAtMs,
			PendingPayloadLen:  pendingPayloadLen,
			PendingRetrying:    pendingRetrying,
			PendingRetryCount:  pendingRetryCount,
			PendingRetryReason: pendingRetryReason,
			LastErrorMsg:       lastErrorMsg,
			StatusLevel:        statusLevel,
			StepCacheRate:      lastCacheRate + "%",
			SessionCacheRate:   sessionCacheRate + "%",
		},
	}
}

func nonNilTurns(t []TurnSummary) []TurnSummary {
	if t == nil {
		return []TurnSummary{}
	}
	return t
}

func historyStepStartMs(st *convState, valid []GenItem) map[int64]float64 {
	out := map[int64]float64{}
	for _, g := range valid[max(len(valid)-HistoryLimit, 0):] {
		if s := st.step(int64(g.MaxStepIdx)); g.MaxStepIdx >= 0 && s != nil && s.StartMs > 0 {
			out[int64(g.MaxStepIdx)] = s.StartMs
		}
	}
	return out
}

func payloadSig(version uint64, cfg ReaderConfig) string {
	return fmt.Sprintf("v%d|lim:%s:%s|cm:%s", version, jsNum(cfg.ManualContextLimit), jsNum(cfg.FallbackContextLimit), cfg.TelemetryCacheMode)
}
