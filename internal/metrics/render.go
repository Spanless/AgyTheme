package metrics

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

func FmtTok(n float64) string {
	if n <= 0 || math.IsNaN(n) {
		return "0"
	}
	if n >= 1000000 {
		return toFixed(n/1000000, 2) + "M"
	}
	if n >= 10000 {
		return jsNum(jsRound(n/1000)) + "k"
	}
	if n >= 1000 {
		return toFixed(n/1000, 1) + "k"
	}
	return jsNum(n)
}

var trailingZerosRe = regexp.MustCompile(`\.?0+$`)

func FormatContextLimitLabel(tokens float64) string {
	if tokens <= 0 || math.IsNaN(tokens) {
		return "0k"
	}
	if tokens >= 1000000 {
		if tokens == 1000000 || tokens == 1048576 {
			return "1M"
		}
		return trailingZerosRe.ReplaceAllString(toFixed(tokens/1000000, 2), "") + "M"
	}
	return jsNum(jsRound(tokens/1000)) + "k"
}

var (
	orchestratorRe       = regexp.MustCompile(`(?i)^Orchestrator\b`)
	orchestratorSuffixRe = regexp.MustCompile(`(?i)\s*\(Orchestrator\)\s*$`)
)

func FormatTurnModelName(gens []GenItem) string {
	var models []string
	seen := map[string]bool{}
	for _, g := range gens {
		if g.ModelName != "" && !seen[g.ModelName] {
			seen[g.ModelName] = true
			models = append(models, g.ModelName)
		}
	}
	if len(models) == 0 {
		return ""
	}
	if len(models) == 1 {
		return models[0]
	}
	first := models[0]
	last := models[len(models)-1]
	if orchestratorRe.MatchString(first) {
		last = strings.TrimSpace(orchestratorSuffixRe.ReplaceAllString(last, ""))
	}
	if last != "" && last != first {
		return first + " → " + last
	}
	return first
}

type TurnSummary struct {
	TurnIndex    int     `json:"turnIndex"`
	StartStepIdx int64   `json:"startStepIdx"`
	EndStepIdx   int64   `json:"endStepIdx"`
	StartedMs    float64 `json:"startedMs,omitempty"`
	MinGenIdx    int64   `json:"minGenIdx"`
	MaxGenIdx    int64   `json:"maxGenIdx"`
	Steps        int     `json:"steps"`
	Model        string  `json:"model"`
	LastCtx      float64 `json:"lastCtx"`
	CtxStr       string  `json:"ctxStr"`
	PromptTok    float64 `json:"promptTok"`
	PromptStr    string  `json:"promptStr"`
	OutTok       float64 `json:"outTok"`
	OutStr       string  `json:"outStr"`
	InTok        float64 `json:"inTok"`
	CacheWrite   float64 `json:"cacheWrite"`
	CacheRead    float64 `json:"cacheRead"`
	ThinkTok     float64 `json:"thinkTok"`
	RespTok      float64 `json:"respTok"`
	StreamDur    float64 `json:"streamDur"`
	TotDur       float64 `json:"totDur"`
	Tps          float64 `json:"tps"`
	E2ETps       float64 `json:"e2eTps"`
	ContextLimit float64 `json:"contextLimit"`
	LimitStr     string  `json:"limitStr"`
	CtxPct       float64 `json:"ctxPct"`
	CacheRate    string  `json:"cacheRate"`
}

const (
	minStreamDurSec    = 0.12
	maxSanityStreamTps = 3000
)

func BuildTurnSummaries(valid []GenItem, userStepIdxs []int64, manualLim, fallbackLim float64) []TurnSummary {
	turns := []TurnSummary{}
	hasStep := false
	for _, it := range valid {
		if it.MaxStepIdx >= 0 {
			hasStep = true
			break
		}
	}
	if len(valid) == 0 || !hasStep {
		return turns
	}
	buckets := map[int][]GenItem{}
	for _, item := range valid {
		si := item.MaxStepIdx
		if si < 0 {
			continue
		}
		lo, hi, found := 0, len(userStepIdxs)-1, -1
		for lo <= hi {
			mid := (lo + hi) / 2
			if float64(userStepIdxs[mid]) <= si {
				found = mid
				lo = mid + 1
			} else {
				hi = mid - 1
			}
		}
		buckets[found] = append(buckets[found], item)
	}
	keys := make([]int, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	for _, k := range keys {
		gens := buckets[k]
		if len(gens) == 0 {
			continue
		}
		startStep := int64(-1)
		if k >= 0 {
			startStep = userStepIdxs[k]
		}
		endStep := int64(-1)
		if k+1 < len(userStepIdxs) {
			endStep = userStepIdxs[k+1] - 1
		}
		var inSum, cwSum, crSum, outSum, thinkSum, respSum, streamSum, totSum, nativeMax float64
		for _, g := range gens {
			inSum += g.InTok
			cwSum += g.CacheWrite
			crSum += g.CacheRead
			outSum += g.OutTok
			thinkSum += g.ThinkTok
			respSum += g.RespTok
			streamSum += g.Stream
			totSum += g.TotDur
			if g.MaxCtx > 0 {
				nativeMax = g.MaxCtx
			}
		}
		firstG, lastG := gens[0], gens[len(gens)-1]
		lastCtx := lastG.Ctx
		contextLimit := fallbackLim
		if manualLim > 0 {
			contextLimit = manualLim
		} else if nativeMax > 0 {
			contextLimit = nativeMax
		}
		streamTps := 0.0
		if streamSum >= minStreamDurSec {
			streamTps = outSum / streamSum
		}
		e2e := 0.0
		if totSum > 0 {
			e2e = outSum / totSum
		}
		tps := jsRound(e2e)
		if streamTps > 0 && streamTps <= maxSanityStreamTps {
			tps = jsRound(streamTps)
		}
		prompt := inSum + cwSum + crSum
		model := FormatTurnModelName(gens)
		cacheRate := "0.0%"
		if prompt > 0 {
			cacheRate = toFixed(crSum/prompt*100, 1) + "%"
		}
		ctxStr := FmtTok(lastCtx)
		outStr := FmtTok(outSum)
		limitStr := FormatContextLimitLabel(contextLimit)
		ctxPct := jsRound(lastCtx / contextLimit * 100)

		turnIndex := 0
		if k >= 0 {
			turnIndex = k + 1
		}
		turns = append(turns, TurnSummary{
			TurnIndex:    turnIndex,
			StartStepIdx: startStep,
			EndStepIdx:   endStep,
			MinGenIdx:    firstG.Idx,
			MaxGenIdx:    lastG.Idx,
			Steps:        len(gens),
			Model:        model,
			LastCtx:      lastCtx,
			CtxStr:       ctxStr,
			PromptTok:    prompt,
			PromptStr:    FmtTok(prompt),
			OutTok:       outSum,
			OutStr:       outStr,
			InTok:        inSum,
			CacheWrite:   cwSum,
			CacheRead:    crSum,
			ThinkTok:     thinkSum,
			RespTok:      respSum,
			StreamDur:    toFixedNum(streamSum, 1),
			TotDur:       toFixedNum(totSum, 1),
			Tps:          tps,
			E2ETps:       toFixedNum(e2e, 1),
			ContextLimit: contextLimit,
			LimitStr:     limitStr,
			CtxPct:       ctxPct,
			CacheRate:    cacheRate,
		})
	}
	return turns
}
