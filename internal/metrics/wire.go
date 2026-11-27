package metrics

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Protobuf JSON encodes int64 counters as strings.
type jnum float64

func (n *jnum) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	*n = jnum(f)
	return nil
}

func durationSec(s string) float64 {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "s") {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(s, "s"), 64)
	if err != nil {
		return 0
	}
	return f
}

func timestampMs(s string) float64 {
	if s == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return float64(t.UnixMilli())
}

type wireUsage struct {
	InputTokens          jnum `json:"inputTokens"`
	OutputTokens         jnum `json:"outputTokens"`
	ThinkingOutputTokens jnum `json:"thinkingOutputTokens"`
	ResponseOutputTokens jnum `json:"responseOutputTokens"`
	CacheWriteTokens     jnum `json:"cacheWriteTokens"`
	CacheReadTokens      jnum `json:"cacheReadTokens"`
}

type wireRetryInfo struct {
	Error       string `json:"error"`
	RetryReason string `json:"retryReason"`
}

type wireTokenItem struct {
	Name      string `json:"name"`
	NumTokens jnum   `json:"numTokens"`
}

type wireTokenGroup struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Source    string          `json:"source"`
	NumTokens jnum            `json:"numTokens"`
	Children  []wireTokenItem `json:"children"`
}

type rawTokenItem struct {
	Name   string
	Tokens float64
}

type rawTokenGroup struct {
	Name     string
	Type     string
	Source   string
	Tokens   float64
	Children []rawTokenItem
}

type rawTokenBreakdown struct {
	Groups []rawTokenGroup
	Total  float64
}

func toRawTokenGroups(wg []wireTokenGroup) []rawTokenGroup {
	if len(wg) == 0 {
		return nil
	}
	out := make([]rawTokenGroup, 0, len(wg))
	for _, g := range wg {
		rg := rawTokenGroup{
			Name:   strings.TrimSpace(g.Name),
			Type:   strings.TrimSpace(g.Type),
			Source: strings.TrimSpace(g.Source),
			Tokens: float64(g.NumTokens),
		}
		if len(g.Children) > 0 {
			rg.Children = make([]rawTokenItem, 0, len(g.Children))
			for _, ch := range g.Children {
				if tok := float64(ch.NumTokens); tok > 0 {
					rg.Children = append(rg.Children, rawTokenItem{
						Name:   strings.TrimSpace(ch.Name),
						Tokens: tok,
					})
				}
			}
		}
		out = append(out, rg)
	}
	return out
}

type wireGen struct {
	StepIndices []int64 `json:"stepIndices"`
	ChatModel   struct {
		Usage             wireUsage       `json:"usage"`
		TimeToFirstToken  string          `json:"timeToFirstToken"`
		StreamingDuration string          `json:"streamingDuration"`
		ModelDisplayName  string          `json:"modelDisplayName"`
		ResponseModel     string          `json:"responseModel"`
		RetryInfos        []wireRetryInfo `json:"retryInfos"`
		ChatStartMetadata struct {
			ContextWindowMetadata struct {
				EstimatedTokensUsed jnum `json:"estimatedTokensUsed"`
				MaxContextTokens    jnum `json:"maxContextTokens"`
				TokenBreakdown      *struct {
					Groups      []wireTokenGroup `json:"groups"`
					TotalTokens jnum             `json:"totalTokens"`
				} `json:"tokenBreakdown"`
			} `json:"contextWindowMetadata"`
		} `json:"chatStartMetadata"`
	} `json:"chatModel"`
}

const retryReasonAPI = "RETRY_REASON_API_RETRYABLE"

func (w *wireGen) genItem(idx int64) GenItem {
	cm := &w.ChatModel
	u := cm.Usage
	ttft := durationSec(cm.TimeToFirstToken)
	stream := durationSec(cm.StreamingDuration)
	maxStep := -1.0
	for _, s := range w.StepIndices {
		maxStep = max(maxStep, float64(s))
	}
	model := strings.TrimSpace(cm.ModelDisplayName)
	if model == "" {
		model = strings.TrimSpace(cm.ResponseModel)
	}
	g := GenItem{
		Idx:        idx,
		InTok:      float64(u.InputTokens),
		OutTok:     float64(u.OutputTokens),
		CacheWrite: float64(u.CacheWriteTokens),
		CacheRead:  float64(u.CacheReadTokens),
		MaxCtx:     float64(cm.ChatStartMetadata.ContextWindowMetadata.MaxContextTokens),
		MaxStepIdx: maxStep,
		ModelName:  model,
		ThinkTok:   float64(u.ThinkingOutputTokens),
		RespTok:    float64(u.ResponseOutputTokens),
		TTFT:       ttft,
		Stream:     stream,
		TotDur:     ttft + stream,
	}
	g.Ctx = g.InTok + g.CacheWrite + g.CacheRead
	if tb := cm.ChatStartMetadata.ContextWindowMetadata.TokenBreakdown; tb != nil && len(tb.Groups) > 0 {
		g.Breakdown = &rawTokenBreakdown{
			Groups: toRawTokenGroups(tb.Groups),
			Total:  float64(tb.TotalTokens),
		}
	}
	if n := len(cm.RetryInfos); n > 0 {
		last := cm.RetryInfos[n-1]
		g.ErrMsg = strings.TrimSpace(last.Error)
		g.APIError = last.RetryReason == retryReasonAPI
	}
	return g
}

type GenItem struct {
	Idx        int64
	InTok      float64
	OutTok     float64
	CacheWrite float64
	CacheRead  float64
	Ctx        float64
	MaxCtx     float64
	MaxStepIdx float64
	ModelName  string
	ThinkTok   float64
	RespTok    float64
	TTFT       float64
	Stream     float64
	TotDur     float64
	ErrMsg     string
	APIError   bool
	Breakdown  *rawTokenBreakdown
}

func (g *GenItem) apiFailure() bool { return g.APIError && g.ErrMsg != "" && !IsCancelText(g.ErrMsg) }

const (
	stepTypeUserInput       = "CORTEX_STEP_TYPE_USER_INPUT"
	stepTypePlannerResponse = "CORTEX_STEP_TYPE_PLANNER_RESPONSE"
	stepTypeErrorMessage    = "CORTEX_STEP_TYPE_ERROR_MESSAGE"
	stepTypeCheckpoint      = "CORTEX_STEP_TYPE_CHECKPOINT"

	stepStatusDone       = "CORTEX_STEP_STATUS_DONE"
	stepStatusPending    = "CORTEX_STEP_STATUS_PENDING"
	stepStatusRunning    = "CORTEX_STEP_STATUS_RUNNING"
	stepStatusGenerating = "CORTEX_STEP_STATUS_GENERATING"
	stepStatusWaiting    = "CORTEX_STEP_STATUS_WAITING"
	stepStatusQueued     = "CORTEX_STEP_STATUS_QUEUED"
	stepStatusError      = "CORTEX_STEP_STATUS_ERROR"
)

func isActiveStatus(s string) bool {
	switch s {
	case stepStatusPending, stepStatusRunning, stepStatusGenerating, stepStatusWaiting, stepStatusQueued:
		return true
	}
	return false
}

type wireErrorDetails struct {
	UserErrorMessage  string `json:"userErrorMessage"`
	ModelErrorMessage string `json:"modelErrorMessage"`
	ShortError        string `json:"shortError"`
	FullError         string `json:"fullError"`
}

func (e *wireErrorDetails) text() string {
	if e == nil {
		return ""
	}
	for _, s := range []string{e.ShortError, e.UserErrorMessage, e.ModelErrorMessage, e.FullError} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

type wireStep struct {
	Type     string            `json:"type"`
	Status   string            `json:"status"`
	Error    *wireErrorDetails `json:"error"`
	Metadata struct {
		CreatedAt                string          `json:"createdAt"`
		RetryInfos               []wireRetryInfo `json:"retryInfos"`
		SourceTrajectoryStepInfo struct {
			MetadataIndex *jnum `json:"metadataIndex"`
		} `json:"sourceTrajectoryStepInfo"`
		ToolCall *struct {
			Name string `json:"name"`
		} `json:"toolCall"`
		ToolSource string `json:"toolSource"`
	} `json:"metadata"`
	ErrorMessage *struct {
		Error *wireErrorDetails `json:"error"`
	} `json:"errorMessage"`
	Checkpoint *struct {
		SessionSummary *string `json:"sessionSummary"`
	} `json:"checkpoint"`
}

type stepInfo struct {
	Type         string
	Status       string
	StartMs      float64
	Bytes        int
	ErrText      string
	RetryErr     string
	CpMetaIdx    float64
	SummaryBytes int
	ToolName     string
	ToolSource   string
}

func (s *stepInfo) settled() bool { return !isActiveStatus(s.Status) }

func parseStep(raw json.RawMessage) stepInfo {
	var w wireStep
	_ = json.Unmarshal(raw, &w)
	s := stepInfo{
		Type:         w.Type,
		Status:       w.Status,
		StartMs:      timestampMs(w.Metadata.CreatedAt),
		Bytes:        len(raw),
		CpMetaIdx:    -1,
		SummaryBytes: -1,
		ToolSource:   strings.TrimSpace(w.Metadata.ToolSource),
	}
	if w.Metadata.ToolCall != nil {
		s.ToolName = strings.TrimSpace(w.Metadata.ToolCall.Name)
	}
	if mi := w.Metadata.SourceTrajectoryStepInfo.MetadataIndex; mi != nil {
		s.CpMetaIdx = float64(*mi)
	}
	for i := len(w.Metadata.RetryInfos) - 1; i >= 0; i-- {
		if e := strings.TrimSpace(w.Metadata.RetryInfos[i].Error); e != "" {
			s.RetryErr = e
			break
		}
	}
	switch {
	case w.Type == stepTypeErrorMessage && w.ErrorMessage != nil:
		s.ErrText = w.ErrorMessage.Error.text()
	case w.Status == stepStatusError:
		s.ErrText = w.Error.text()
	}
	if w.Checkpoint != nil && w.Checkpoint.SessionSummary != nil {
		s.SummaryBytes = len(*w.Checkpoint.SessionSummary)
	}
	return s
}

type agentStateUpdate struct {
	Update struct {
		Status               string `json:"status"`
		FullyIdle            *bool  `json:"fullyIdle"`
		MainTrajectoryUpdate *struct {
			StepsUpdate *struct {
				TotalLength *jnum `json:"totalLength"`
			} `json:"stepsUpdate"`
			GeneratorMetadatasUpdate *struct {
				TotalLength *jnum `json:"totalLength"`
			} `json:"generatorMetadatasUpdate"`
		} `json:"mainTrajectoryUpdate"`
		ExecutionStatus *struct {
			ModelRetry *struct {
				Attempt     jnum   `json:"attempt"`
				MaxAttempts jnum   `json:"maxAttempts"`
				RetryAt     string `json:"retryAt"`
			} `json:"modelRetry"`
		} `json:"executionStatus"`
		CompactionInfo *struct {
			CompactedAtStepIndices []int64 `json:"compactedAtStepIndices"`
		} `json:"compactionInfo"`
		TurnsUpdate *struct {
			Turns []struct {
				Index          int64  `json:"index"`
				StartStepIndex int64  `json:"startStepIndex"`
				StartTime      string `json:"startTime"`
			} `json:"turns"`
			TotalLength *jnum `json:"totalLength"`
		} `json:"turnsUpdate"`
		ContextWindowUsage *struct {
			GeneratorMetadataIndex *jnum            `json:"generatorMetadataIndex"`
			LastStepIndex          *jnum            `json:"lastStepIndex"`
			EstimatedTokensUsed    jnum             `json:"estimatedTokensUsed"`
			MaxContextTokens       jnum             `json:"maxContextTokens"`
			TokenGroups            []wireTokenGroup `json:"tokenGroups"`
		} `json:"contextWindowUsage"`
	} `json:"update"`
}
