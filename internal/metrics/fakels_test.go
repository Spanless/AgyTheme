package metrics

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeLS struct {
	t       *testing.T
	cid     string
	srv     *httptest.Server
	mu      sync.Mutex
	gens    []map[string]any
	steps   []map[string]any
	compact []int
	status  string
	streams []chan map[string]any
	pulls   []string
}

const fakeToken = "csrf-test"

func newFakeLS(t *testing.T, cid string) *fakeLS {
	f := &fakeLS{t: t, cid: cid, status: runStatusIdle}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLS) client() *LSClient {
	return NewLSClient(strings.TrimPrefix(f.srv.URL, "http://"), fakeToken)
}

func (f *fakeLS) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("x-codeium-csrf-token") != fakeToken {
		http.Error(w, `{"code":"unauthenticated","message":"bad csrf token"}`, http.StatusUnauthorized)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, lsService)
	body, _ := io.ReadAll(r.Body)
	if method == "StreamAgentStateUpdates" {
		f.serveStream(w, r, body[5:])
		return
	}
	var req struct {
		CascadeID               string `json:"cascadeId"`
		GeneratorMetadataOffset int    `json:"generatorMetadataOffset"`
		StepOffset              int    `json:"stepOffset"`
		TrajectoryVerbosity     string `json:"trajectoryVerbosity"`
	}
	_ = json.Unmarshal(body, &req)
	if req.CascadeID != f.cid {
		http.Error(w, `{"code":"not_found","message":"trajectory not found"}`, http.StatusNotFound)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var resp map[string]any
	switch method {
	case "GetCascadeTrajectoryGeneratorMetadata":
		f.pulls = append(f.pulls, fmt.Sprintf("gens@%d", req.GeneratorMetadataOffset))
		resp = map[string]any{"generatorMetadata": tail(f.gens, req.GeneratorMetadataOffset)}
	case "GetCascadeTrajectorySteps":
		f.pulls = append(f.pulls, fmt.Sprintf("steps@%d", req.StepOffset))
		steps := tail(f.steps, req.StepOffset)
		if req.TrajectoryVerbosity != "" {
			steps = trimmed(steps)
		}
		resp = map[string]any{"steps": steps}
	default:
		http.Error(w, `{"code":"unimplemented"}`, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func tail(xs []map[string]any, off int) []map[string]any {
	if off >= len(xs) {
		return []map[string]any{}
	}
	return xs[off:]
}

func trimmed(steps []map[string]any) []map[string]any {
	out := make([]map[string]any, len(steps))
	for i, s := range steps {
		c := map[string]any{}
		for k, v := range s {
			c[k] = v
		}
		if _, ok := c["checkpoint"]; ok {
			c["checkpoint"] = map[string]any{}
		}
		out[i] = c
	}
	return out
}

func (f *fakeLS) serveStream(w http.ResponseWriter, r *http.Request, body []byte) {
	var req struct {
		ConversationID string `json:"conversationId"`
	}
	_ = json.Unmarshal(body, &req)
	w.Header().Set("Content-Type", "application/connect+json")
	w.WriteHeader(http.StatusOK)
	if req.ConversationID != f.cid {
		writeEnvelope(w, envelopeEndStream, map[string]any{"error": map[string]any{"code": "not_found", "message": "conversation not found"}})
		return
	}
	ch := make(chan map[string]any, 16)
	f.mu.Lock()
	f.streams = append(f.streams, ch)
	first := f.snapshotLocked()
	f.mu.Unlock()
	writeEnvelope(w, 0, first)
	for {
		select {
		case u := <-ch:
			writeEnvelope(w, 0, u)
		case <-r.Context().Done():
			return
		}
	}
}

func writeEnvelope(w http.ResponseWriter, flags byte, msg any) {
	b, _ := json.Marshal(msg)
	hdr := make([]byte, 5)
	hdr[0] = flags
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(b)))
	_, _ = w.Write(append(hdr, b...))
	w.(http.Flusher).Flush()
}

func (f *fakeLS) snapshotLocked() map[string]any {
	var turns []map[string]any
	for i, s := range f.steps {
		if s["type"] == stepTypeUserInput {
			turns = append(turns, map[string]any{"index": len(turns), "startStepIndex": i, "startTime": s["metadata"].(map[string]any)["createdAt"]})
		}
	}
	return map[string]any{"update": map[string]any{
		"conversationId": f.cid,
		"status":         f.status,
		"mainTrajectoryUpdate": map[string]any{
			"stepsUpdate":              map[string]any{"totalLength": len(f.steps)},
			"generatorMetadatasUpdate": map[string]any{"totalLength": len(f.gens)},
		},
		"turnsUpdate":    map[string]any{"turns": turns, "totalLength": len(turns)},
		"compactionInfo": map[string]any{"compactedAtStepIndices": f.compact},
	}}
}

func (f *fakeLS) change(edit func(f *fakeLS), extra map[string]any) {
	f.mu.Lock()
	edit(f)
	msg := f.snapshotLocked()
	for k, v := range extra {
		msg["update"].(map[string]any)[k] = v
	}
	streams := f.streams
	f.mu.Unlock()
	for _, ch := range streams {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (f *fakeLS) pullLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.pulls...)
}

func (f *fakeLS) resetPullLog() {
	f.mu.Lock()
	f.pulls = nil
	f.mu.Unlock()
}

var fixtureStart = time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)

func stepAt(k int) time.Time { return fixtureStart.Add(time.Duration(k) * time.Minute) }

func stepMs(k int) float64 { return float64(stepAt(k).UnixMilli()) }

func step(k int, kind, status string) map[string]any {
	return map[string]any{
		"type":     "CORTEX_STEP_TYPE_" + kind,
		"status":   "CORTEX_STEP_STATUS_" + status,
		"metadata": map[string]any{"createdAt": stepAt(k).Format(time.RFC3339Nano)},
	}
}

func with(s map[string]any, k string, v any) map[string]any {
	s[k] = v
	return s
}

type call struct {
	in, out, cacheWrite, cacheRead, think, resp, maxCtx int
	steps                                               []int
	model, ttft, stream                                 string
	err, reason                                         string
}

func (c call) json() map[string]any {
	n := func(v int) string { return fmt.Sprint(v) }
	cm := map[string]any{
		"usage": map[string]any{
			"inputTokens": n(c.in), "outputTokens": n(c.out), "cacheWriteTokens": n(c.cacheWrite),
			"cacheReadTokens": n(c.cacheRead), "thinkingOutputTokens": n(c.think), "responseOutputTokens": n(c.resp),
		},
		"timeToFirstToken":  c.ttft,
		"streamingDuration": c.stream,
		"modelDisplayName":  c.model,
		"chatStartMetadata": map[string]any{"contextWindowMetadata": map[string]any{"maxContextTokens": c.maxCtx}},
	}
	if c.err != "" {
		cm["retryInfos"] = []any{map[string]any{"error": c.err, "retryReason": c.reason}}
	}
	return map[string]any{"stepIndices": c.steps, "chatModel": cm}
}

func follow(t *testing.T, f *fakeLS) (*Source, <-chan string) {
	t.Helper()
	changed := make(chan string, 64)
	src := NewSource(f.client(), func(cid string) { changed <- cid })
	t.Cleanup(src.Close)
	src.SetWatched("window-1", []string{f.cid})
	return src, changed
}

var defaultCfg = ReaderConfig{FallbackContextLimit: 256000, TelemetryCacheMode: "session"}

func payloadWhen(t *testing.T, src *Source, cid string, cond func(*Payload) bool) *Payload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last *Payload
	for time.Now().Before(deadline) {
		if p, ok := src.Payload(cid, defaultCfg); ok {
			last = p
			if cond(p) {
				return p
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("payload never matched; last = %s", describe(last))
	return nil
}

func describe(p *Payload) string {
	if p == nil {
		return "<none>"
	}
	b, _ := json.Marshal(struct {
		HasData bool
		Status  *Status
		Stats   *Stats
	}{p.HasData, p.Status, p.Stats})
	return string(b)
}

func hasData(p *Payload) bool { return p.HasData }
