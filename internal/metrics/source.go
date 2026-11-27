package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type Source struct {
	client   *LSClient
	onChange func(cid string)
	now      func() time.Time
	Logf     func(format string, args ...any)

	mu     sync.Mutex
	owners map[string][]string
	convs  map[string]*conv
	closed bool
	subSeq atomic.Uint64
}

type conv struct {
	cid    string
	cancel context.CancelFunc

	state   *convState
	version uint64
	failed  bool

	cached      *Payload
	cachedSig   string
	cachedUntil float64
}

const maxFollowed = 8

func NewSource(client *LSClient, onChange func(cid string)) *Source {
	return &Source{client: client, onChange: onChange, now: time.Now, owners: map[string][]string{}, convs: map[string]*conv{}}
}

func (s *Source) SetWatched(owner string, cids []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if len(cids) == 0 {
		delete(s.owners, owner)
	} else {
		s.owners[owner] = slices.Clone(cids)
	}
	owners := make([]string, 0, len(s.owners))
	for o := range s.owners {
		owners = append(owners, o)
	}
	slices.Sort(owners)
	want := map[string]bool{}
	for _, o := range owners {
		for _, cid := range s.owners[o] {
			if len(want) < maxFollowed {
				want[cid] = true
			}
		}
	}
	for cid, c := range s.convs {
		if !want[cid] {
			c.cancel()
			delete(s.convs, cid)
		}
	}
	for cid := range want {
		if _, ok := s.convs[cid]; !ok {
			ctx, cancel := context.WithCancel(context.Background())
			c := &conv{cid: cid, cancel: cancel}
			s.convs[cid] = c
			go s.follow(ctx, c)
		}
	}
}

func (s *Source) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for cid, c := range s.convs {
		c.cancel()
		delete(s.convs, cid)
	}
}

// Returns ok=false while loading so the page keeps its previous display.
func (s *Source) Payload(cid string, cfg ReaderConfig) (p *Payload, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convs[cid]
	if c == nil {
		return nil, false
	}
	if c.state == nil {
		if c.failed {
			return &Payload{Cid: cid, CascadeID: cid}, true
		}
		return nil, false
	}
	sig := payloadSig(c.version, cfg)
	nowMs := float64(s.now().UnixMilli())
	if c.cached != nil && c.cachedSig == sig && (c.cachedUntil <= 0 || nowMs < c.cachedUntil) {
		return c.cached, true
	}
	p = buildPayload(cid, sig, c.state, cfg, nowMs)
	c.cached, c.cachedSig, c.cachedUntil = p, sig, 0
	if p.Status != nil {
		c.cachedUntil = p.Status.RetryUntilMs
	}
	return p, true
}

func (s *Source) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

func (s *Source) publish(c *conv, st *convState) {
	s.mu.Lock()
	c.version++
	if st != nil {
		c.state = st
	} else if c.state == nil {
		c.failed = true
	}
	s.mu.Unlock()
	if s.onChange != nil {
		s.onChange(c.cid)
	}
}

func (s *Source) edit(fn func()) {
	s.mu.Lock()
	fn()
	s.mu.Unlock()
}

const (
	followRetryMin  = time.Second
	followRetryMax  = 30 * time.Second
	refreshDebounce = 80 * time.Millisecond
)

func (s *Source) follow(ctx context.Context, c *conv) {
	wait := followRetryMin
	for {
		loaded, err := s.followOnce(ctx, c)
		if ctx.Err() != nil {
			return
		}
		if loaded {
			wait = followRetryMin
		}
		s.logf("[agytheme] metrics: conversation %s: %v (retrying in %s)", shortID(c.cid), err, wait)
		if !loaded {
			s.publish(c, nil)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, followRetryMax)
	}
}

func shortID(cid string) string {
	if len(cid) > 8 {
		return cid[:8]
	}
	return cid
}

type live struct {
	run         string
	runAtPull   string
	gensChanged bool
	streamSteps int64
	streamGens  int64
	rewound     bool
}

func (s *Source) followOnce(ctx context.Context, c *conv) (loaded bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := make(chan agentStateUpdate, 64)
	streamErr := make(chan error, 1)
	zero := map[string]int{"startIndex": 0, "endIndexExclusive": 0}
	req := map[string]any{
		"conversationId":                      c.cid,
		"subscriberId":                        fmt.Sprintf("agytheme-%d-%d", os.Getpid(), s.subSeq.Add(1)),
		"trajectoryVerbosity":                 verbosityTrimmed,
		"initialStepsPageBounds":              zero,
		"initialGeneratorMetadatasPageBounds": zero,
		"initialExecutorMetadatasPageBounds":  zero,
	}
	go func() {
		streamErr <- s.client.stream(ctx, "StreamAgentStateUpdates", req, func(raw json.RawMessage) error {
			var u agentStateUpdate
			if err := json.Unmarshal(raw, &u); err != nil {
				return err
			}
			select {
			case updates <- u:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	var lv live
	st := &convState{}
	select {
	case u := <-updates:
		s.apply(st, &lv, &u)
	case err := <-streamErr:
		return false, err
	case <-ctx.Done():
		return false, ctx.Err()
	}
	if err := s.load(ctx, c.cid, st, &lv); err != nil {
		return false, err
	}
	s.publish(c, st)

	var timer <-chan time.Time
	for {
		select {
		case u := <-updates:
			s.edit(func() { s.apply(st, &lv, &u) })
			if timer == nil {
				timer = time.After(refreshDebounce)
			}
		case <-timer:
			timer = nil
			if lv.rewound {
				lv.rewound = false
				next := &convState{stepTotal: st.stepTotal, turns: slices.Clone(st.turns), compactedAt: slices.Clone(st.compactedAt), retry: st.retry}
				if err := s.load(ctx, c.cid, next, &lv); err != nil {
					return true, err
				}
				st = next
			} else if err := s.refresh(ctx, c.cid, st, &lv); err != nil {
				return true, err
			}
			s.publish(c, st)
		case err := <-streamErr:
			return true, err
		case <-ctx.Done():
			return true, ctx.Err()
		}
	}
}

func (s *Source) apply(st *convState, lv *live, u *agentStateUpdate) {
	up := &u.Update
	if up.Status != "" {
		lv.run = up.Status
	}
	if m := up.MainTrajectoryUpdate; m != nil {
		if m.StepsUpdate != nil && m.StepsUpdate.TotalLength != nil {
			n := int64(*m.StepsUpdate.TotalLength)
			lv.rewound = lv.rewound || n < lv.streamSteps
			lv.streamSteps, st.stepTotal = n, n
		}
		if m.GeneratorMetadatasUpdate != nil && m.GeneratorMetadatasUpdate.TotalLength != nil {
			n := int64(*m.GeneratorMetadatasUpdate.TotalLength)
			lv.rewound = lv.rewound || n < lv.streamGens
			lv.streamGens = n
			lv.gensChanged = true
		}
	}
	if cw := up.ContextWindowUsage; cw != nil && cw.GeneratorMetadataIndex != nil {
		lv.gensChanged = true
	}
	if es := up.ExecutionStatus; es != nil {
		st.retry = nil
		if r := es.ModelRetry; r != nil {
			st.retry = &modelRetry{Attempt: int(r.Attempt), RetryAtMs: timestampMs(r.RetryAt), SeenMs: float64(s.now().UnixMilli())}
		}
	}
	if lv.run == runStatusIdle {
		st.retry = nil
	}
	if ci := up.CompactionInfo; ci != nil {
		st.compactedAt = slices.Clone(ci.CompactedAtStepIndices)
	}
	if tu := up.TurnsUpdate; tu != nil {
		for _, t := range tu.Turns {
			if t.Index < 0 || t.Index > 1<<20 {
				continue
			}
			for int64(len(st.turns)) <= t.Index {
				st.turns = append(st.turns, turnInfo{StartStep: -1})
			}
			st.turns[t.Index] = turnInfo{StartStep: t.StartStepIndex, StartMs: timestampMs(t.StartTime)}
		}
		if tu.TotalLength != nil && int64(*tu.TotalLength) < int64(len(st.turns)) {
			st.turns = st.turns[:int64(*tu.TotalLength)]
		}
	}
}

const (
	runStatusIdle    = "CASCADE_RUN_STATUS_IDLE"
	verbosityTrimmed = "CLIENT_TRAJECTORY_VERBOSITY_PROD_UI"
	maxStepWindow    = 600
)

func (s *Source) load(ctx context.Context, cid string, st *convState, lv *live) error {
	gens, err := s.pullGens(ctx, cid, 0)
	if err != nil {
		return err
	}
	st.gens = gens
	lv.runAtPull = lv.run
	lv.gensChanged = false
	base := st.stepTotal
	if n := len(st.turns); n > 0 && st.turns[n-1].StartStep >= 0 {
		base = min(base, st.turns[n-1].StartStep)
	}
	var valid []GenItem
	for _, g := range gens {
		if g.Ctx > 0 || g.OutTok > 0 {
			valid = append(valid, g)
		}
	}
	for _, g := range valid[max(len(valid)-HistoryLimit, 0):] {
		if g.MaxStepIdx >= 0 {
			base = min(base, int64(g.MaxStepIdx))
		}
	}
	st.stepBase = max(base, st.stepTotal-maxStepWindow, 0)
	steps, err := s.pullSteps(ctx, cid, st.stepBase, verbosityTrimmed)
	if err != nil {
		return err
	}
	st.steps = steps
	st.stepTotal = max(st.stepTotal, st.stepBase+int64(len(steps)))
	return s.readPendingSummary(ctx, cid, st)
}

func (s *Source) refresh(ctx context.Context, cid string, st *convState, lv *live) error {
	n := len(st.gens)
	from := n
	if n > 0 && (lv.gensChanged || lv.run != lv.runAtPull || incomplete(&st.gens[n-1])) {
		from = n - 1
	}
	gens, err := s.pullGens(ctx, cid, from)
	if err != nil {
		return err
	}
	lv.runAtPull = lv.run
	lv.gensChanged = false

	end := st.stepBase + int64(len(st.steps))
	stepFrom := min(end, st.stepTotal)
	for i := range st.steps {
		if idx := st.stepBase + int64(i); idx < stepFrom && !st.steps[i].settled() {
			stepFrom = idx
			break
		}
	}
	var steps []stepInfo
	pullSteps := stepFrom < st.stepTotal || stepFrom < end
	if stepFrom < st.stepBase {
		stepFrom = st.stepBase
	}
	if pullSteps {
		if steps, err = s.pullSteps(ctx, cid, stepFrom, verbosityTrimmed); err != nil {
			return err
		}
	}
	s.edit(func() {
		st.gens = append(st.gens[:from:from], gens...)
		if pullSteps {
			keep := stepFrom - st.stepBase
			st.steps = append(st.steps[:keep:keep], steps...)
			st.stepTotal = max(st.stepTotal, st.stepBase+int64(len(st.steps)))
		}
	})
	return s.readPendingSummary(ctx, cid, st)
}

func incomplete(g *GenItem) bool { return g.Ctx == 0 && g.OutTok == 0 && g.ErrMsg == "" }

func (s *Source) readPendingSummary(ctx context.Context, cid string, st *convState) error {
	lastStep := -1.0
	for i := len(st.gens) - 1; i >= 0; i-- {
		if g := &st.gens[i]; g.Ctx > 0 || g.OutTok > 0 {
			lastStep = g.MaxStepIdx
			break
		}
	}
	for i := len(st.steps) - 1; i >= 0; i-- {
		cp := &st.steps[i]
		idx := st.stepBase + int64(i)
		if float64(idx) <= lastStep {
			return nil
		}
		if cp.Type != stepTypeCheckpoint || cp.Status != stepStatusDone || cp.SummaryBytes >= 0 {
			continue
		}
		full, err := s.pullSteps(ctx, cid, idx, "")
		if err != nil {
			return err
		}
		size := 0
		if len(full) > 0 && full[0].SummaryBytes > 0 {
			size = full[0].SummaryBytes
		}
		s.edit(func() { cp.SummaryBytes = size })
		return nil
	}
	return nil
}

func (s *Source) pullGens(ctx context.Context, cid string, offset int) ([]GenItem, error) {
	var resp struct {
		GeneratorMetadata []wireGen `json:"generatorMetadata"`
	}
	req := map[string]any{"cascadeId": cid, "generatorMetadataOffset": offset}
	if err := s.client.call(ctx, "GetCascadeTrajectoryGeneratorMetadata", req, &resp); err != nil {
		return nil, err
	}
	out := make([]GenItem, len(resp.GeneratorMetadata))
	for i := range resp.GeneratorMetadata {
		out[i] = resp.GeneratorMetadata[i].genItem(int64(offset + i))
	}
	return out, nil
}

func (s *Source) pullSteps(ctx context.Context, cid string, offset int64, verbosity string) ([]stepInfo, error) {
	var resp struct {
		Steps []json.RawMessage `json:"steps"`
	}
	req := map[string]any{"cascadeId": cid, "stepOffset": offset}
	if verbosity != "" {
		req["trajectoryVerbosity"] = verbosity
	}
	if err := s.client.call(ctx, "GetCascadeTrajectorySteps", req, &resp); err != nil {
		return nil, err
	}
	out := make([]stepInfo, len(resp.Steps))
	for i, raw := range resp.Steps {
		out[i] = parseStep(raw)
	}
	return out, nil
}
