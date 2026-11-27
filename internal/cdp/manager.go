package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Spanless/AgyTheme/internal/bundle"
	"github.com/Spanless/AgyTheme/internal/metrics"
	"github.com/Spanless/AgyTheme/internal/shared"
)

const targetDiscoveryInterval = 12500 * time.Millisecond

const hostProbeExpr = `(() => {
              const loc = window.location;
              const isHost = Boolean(
                (loc && loc.protocol === 'vscode-file:') ||
                (window.__APP_CONFIG__ && window.__APP_CONFIG__.productName === 'antigravity') ||
                (window.electronNative && (
                  typeof window.electronNative.setTitleBarOverlay === 'function' ||
                  typeof window.electronNative.getZoomLevel === 'function'
                )) ||
                (typeof document !== 'undefined' && document.querySelector &&
                  document.querySelector('link[href*="/jetbox.css"], body[data-agy-theme], body[data-newux-theme], [id="antigravity.agentSidePanelInputBox"]'))
              );
              return isHost;
            })()`

const watchedCascadeIdsExpr = `(function(){
                if (typeof window.__ag_get_watched_cascade_ids === 'function') {
                  const list = window.__ag_get_watched_cascade_ids();
                  if (Array.isArray(list)) return list;
                }
                const out = [];
                const add = (id) => {
                  const s = String(id || '').trim();
                  if (/^[0-9a-f-]{36}$/i.test(s) && !out.includes(s)) out.push(s);
                };
                if (window.__ag_watched_cascade_id) add(window.__ag_watched_cascade_id);
                const m = window.location.pathname.match(/\/c\/([0-9a-f-]{36})/i);
                if (m) {
                  add(m[1]);
                } else if (!document.querySelector('[data-testid="home-greeting"], [data-testid="home-splash-content"]')) {
                  const el = document.querySelector('[data-testid=conversation-row-sidebar][data-selected=true]');
                  if (el) add(el.getAttribute('data-cascade-id'));
                }
                document.querySelectorAll('[data-testid="side-question-tab-conversation"][data-cascade-id]').forEach((el) => {
                  if (!el.querySelector('[data-testid="side-question-tab-empty"]')) {
                    add(el.getAttribute('data-cascade-id'));
                  }
                });
                return out;
              })()`

const themeModeExpr = `document.documentElement ? (document.documentElement.getAttribute('data-ag-theme-mode') || '') : ''`

func jsonStr(v any) string {
	b, _ := shared.MarshalNoEscape(v)
	return string(b)
}

func TargetOriginKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return shared.TruncRunes(raw, 64)
	}
	return u.Scheme + "://" + u.Host
}

var cidSplitRe = regexp.MustCompile(`[,\s]+`)

func ParseCascadeIDList(raw any) []string {
	out := []string{}
	push := func(v any) {
		s := ""
		switch t := v.(type) {
		case string:
			s = t
		case nil:
		default:
			s = fmt.Sprint(t)
		}
		s = strings.TrimSpace(s)
		if !metrics.IsCascadeID(s) {
			return
		}
		for _, x := range out {
			if x == s {
				return
			}
		}
		out = append(out, s)
	}
	cap6 := func() []string {
		if len(out) > 6 {
			return out[:6]
		}
		return out
	}
	if arr, ok := raw.([]any); ok {
		for _, it := range arr {
			push(it)
		}
		return cap6()
	}
	str, _ := raw.(string)
	str = strings.TrimSpace(str)
	if str == "" {
		return out
	}
	if strings.HasPrefix(str, "[") {
		var arr []any
		if json.Unmarshal([]byte(str), &arr) == nil {
			for _, it := range arr {
				push(it)
			}
			return cap6()
		}
	}
	for _, part := range cidSplitRe.Split(str, -1) {
		push(part)
	}
	return cap6()
}

type targetMeta struct {
	typ      string
	parentID string
	channel  string
}

type pushedSession struct {
	sig          string
	retryUntilMs float64
}

type Manager struct {
	bundle   *bundle.Controller
	relaunch *RelaunchController
	runOnce  bool
	safeMode bool
	LogTrim  func()

	mu              sync.Mutex
	attached        map[string]*target
	fallbackBackoff map[int]time.Time
	skipped         map[string]string
	meta            map[string]targetMeta
	themeModes      map[string]string

	syncMu        sync.Mutex
	tick          int
	lastDiscovery time.Time

	metrics *metrics.Source
}

func (m *Manager) ConfigureRelaunch(spawn func(HostProc) error, pendingFile string, minColdStartWindowSec float64) {
	m.relaunch.Spawn = spawn
	m.relaunch.PendingFile = pendingFile
	m.relaunch.MinColdStartWindowSec = minColdStartWindowSec
}

func NewManager(b *bundle.Controller, ls *metrics.LSClient, runOnce, safeMode bool) *Manager {
	rc := NewRelaunchController()
	rc.SelfExecutable = shared.SelfExecutable()
	m := &Manager{
		bundle:          b,
		relaunch:        rc,
		runOnce:         runOnce,
		safeMode:        safeMode,
		attached:        map[string]*target{},
		fallbackBackoff: map[int]time.Time{},
		skipped:         map[string]string{},
		meta:            map[string]targetMeta{},
		themeModes:      map[string]string{},
	}
	if !runOnce && ls != nil {
		m.metrics = metrics.NewSource(ls, m.onConversationChanged)
		m.metrics.Logf = func(format string, args ...any) { fmt.Printf(format+"\n", args...) }
	}
	return m
}

func (m *Manager) watchConversations(id string, cids []string) {
	if m.metrics != nil {
		m.metrics.SetWatched(id, cids)
	}
}

func (m *Manager) onConversationChanged(cid string) {
	for _, t := range m.snapshot() {
		if t.isSwarm || !t.confirmed() {
			continue
		}
		go func(t *target) {
			t.opMu.Lock()
			defer t.opMu.Unlock()
			for _, a := range t.activeCids {
				if a == cid {
					t.syncMetrics(false)
					return
				}
			}
		}(t)
	}
}

func (m *Manager) resolveParentThemeMode(targetID, channel string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := targetID
	for depth := 0; depth < 6 && cur != ""; depth++ {
		pid := m.meta[cur].parentID
		if pid != "" {
			if mode, ok := m.themeModes[pid]; ok {
				return mode
			}
		}
		cur = pid
	}
	for id, mode := range m.themeModes {
		meta, ok := m.meta[id]
		if channel == "" || !ok || meta.channel == "" || meta.channel == channel {
			return mode
		}
	}
	return ""
}

func (m *Manager) relayThemeModeToChildIframes(parentID, channel, mode string) {
	if mode != "dark" && mode != "light" {
		return
	}
	m.mu.Lock()
	var list []*target
	for _, t := range m.attached {
		list = append(list, t)
	}
	m.mu.Unlock()
	for _, t := range list {
		t.mu.Lock()
		ok := t.isSwarm && t.hostConfirmed
		epCh := t.endpoint.Channel
		pid := t.parentID
		t.mu.Unlock()
		if !ok {
			continue
		}
		if channel != "" && epCh != "" && epCh != channel {
			continue
		}
		if parentID != "" && pid != "" && pid != parentID {
			if resolved := m.resolveParentThemeMode(t.id, epCh); resolved != "" && resolved != mode {
				continue
			}
		}
		t.opMu.Lock()
		t.send("Runtime.evaluate", map[string]any{
			"expression":    fmt.Sprintf("window.__ag_host_theme_mode = %s; if (typeof window.__ag_set_host_theme_mode === 'function') window.__ag_set_host_theme_mode(%s);", jsonStr(mode), jsonStr(mode)),
			"returnByValue": false,
		})
		t.updateNewDocScript()
		t.opMu.Unlock()
	}
}

type target struct {
	m       *Manager
	id      string
	title   string
	isSwarm bool
	conn    *Conn

	opMu sync.Mutex

	mu            sync.Mutex
	endpoint      bundle.Endpoint
	parentID      string
	hostConfirmed bool
	currentURL    string
	initialDone   bool
	navTimer      *time.Timer

	activeCids   []string
	pushed       map[string]pushedSession
	evalScriptID string
}

func (t *target) ep() bundle.Endpoint {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.endpoint
}

func (t *target) confirmed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hostConfirmed
}

func (t *target) getConn() *Conn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn
}

func (t *target) send(method string, params any) *Message {
	c := t.getConn()
	if c == nil {
		return nil
	}
	return c.Send(method, params)
}

func (t *target) buildEffectiveScript() string {
	_, script, _ := t.m.bundle.State()
	if !t.isSwarm {
		return script
	}
	ep := t.ep()
	if mode := t.m.resolveParentThemeMode(t.id, ep.Channel); mode != "" {
		return "window.__ag_host_theme_mode = " + jsonStr(mode) + ";\n" + script
	}
	return script
}

func (t *target) verifyHost() bool {
	if t.isSwarm {
		return true
	}
	res := t.send("Runtime.evaluate", map[string]any{"expression": hostProbeExpr, "returnByValue": true})
	v, _ := res.EvalValue().(bool)
	return v
}

func (t *target) updateNewDocScript() {
	if !t.confirmed() {
		return
	}
	cfg := t.m.bundle.Config()
	if t.evalScriptID != "" {
		t.send("Page.removeScriptToEvaluateOnNewDocument", map[string]any{"identifier": t.evalScriptID})
		t.evalScriptID = ""
	}
	if cfg.Enabled {
		res := t.send("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": t.buildEffectiveScript()})
		if res != nil && len(res.Result) > 0 {
			var r struct {
				Identifier string `json:"identifier"`
			}
			if json.Unmarshal(res.Result, &r) == nil && r.Identifier != "" {
				t.evalScriptID = r.Identifier
			}
		}
	}
}

func (t *target) inject() bool {
	if !t.confirmed() {
		return false
	}
	_, _, patchVersion := t.m.bundle.State()
	res := t.send("Runtime.evaluate", map[string]any{"expression": t.buildEffectiveScript(), "returnByValue": true})
	if res != nil && res.Error != nil {
		fmt.Fprintf(os.Stderr, "[Daemon] Injection error on %s: %s\n", t.id, res.Error.Message)
		return false
	}
	if text, ok := res.ExceptionText(); ok {
		fmt.Fprintf(os.Stderr, "[Daemon] Injection failed on %s: %s\n", t.id, text)
		return false
	}
	kind := "target"
	if t.isSwarm {
		kind = "swarm iframe"
	}
	fmt.Printf("[Daemon] Injected (%s) into %s %s [%s]\n", patchVersion, kind, t.id, t.ep().Channel)
	return true
}

func (t *target) pruneSessions() {
	for cid := range t.pushed {
		found := false
		for _, a := range t.activeCids {
			if a == cid {
				found = true
				break
			}
		}
		if !found {
			delete(t.pushed, cid)
		}
	}
}

func (t *target) pushPayload(p *metrics.Payload) {
	b, _ := shared.MarshalNoEscape(p)
	t.send("Runtime.evaluate", map[string]any{
		"expression":    "(window.__ag_last_daemon_hb = Date.now(), window.__ag_render_metrics && window.__ag_render_metrics(" + string(b) + "))",
		"returnByValue": false,
	})
}

func (t *target) syncMetrics(force bool) {
	if t.isSwarm || !t.confirmed() {
		return
	}
	cfg := t.m.bundle.Config()
	if !cfg.ModuleOn("metrics") || !cfg.Enabled || t.m.metrics == nil {
		t.m.watchConversations(t.id, nil)
		return
	}
	if len(t.activeCids) == 0 || force {
		res := t.send("Runtime.evaluate", map[string]any{"expression": watchedCascadeIdsExpr, "returnByValue": true})
		t.activeCids = ParseCascadeIDList(res.EvalValue())
		t.pruneSessions()
	}
	t.m.watchConversations(t.id, t.activeCids)
	rc := metrics.ReaderConfig{
		ManualContextLimit:   cfg.ManualContextLimit,
		FallbackContextLimit: cfg.FallbackContextLimit,
		TelemetryCacheMode:   cfg.TelemetryCacheMode,
	}
	nowMs := float64(time.Now().UnixMilli())
	for _, cid := range t.activeCids {
		p, ok := t.m.metrics.Payload(cid, rc)
		if !ok {
			continue
		}
		prev, seen := t.pushed[cid]
		retryExpired := prev.retryUntilMs > 0 && nowMs >= prev.retryUntilMs
		if seen && !force && !retryExpired && p.Sig == prev.sig {
			continue
		}
		retryUntil := 0.0
		if p.Status != nil {
			retryUntil = p.Status.RetryUntilMs
		}
		t.pushPayload(p)
		t.pushed[cid] = pushedSession{sig: p.Sig, retryUntilMs: retryUntil}
	}
}

func (t *target) checkAndInject() {
	if !t.confirmed() {
		return
	}
	cfg, _, patchVersion := t.m.bundle.State()
	expr := "((window.__ag_last_daemon_hb = Date.now()), window.__ag_refresh_wait_ui && window.__ag_refresh_wait_ui(), window.__ag_patch_version === " +
		jsonStr(patchVersion) + " && (" + jsonStr(!cfg.Enabled) +
		` || (window.__ag_body_ready === true && !!document.getElementById("agytheme-style"))))`
	res := t.send("Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true})
	if v, _ := res.EvalValue().(bool); !v {
		t.inject()
		t.syncMetrics(true)
	}
}

func (t *target) handshake() bool {
	t.opMu.Lock()
	defer t.opMu.Unlock()
	t.send("Page.enable", nil)
	t.send("Runtime.enable", nil)
	if !t.verifyHost() {
		t.m.mu.Lock()
		t.mu.Lock()
		t.m.skipped[t.id] = TargetOriginKey(t.currentURL)
		t.mu.Unlock()
		t.m.mu.Unlock()
		t.close()
		return false
	}
	t.mu.Lock()
	t.hostConfirmed = true
	ch := t.endpoint.Channel
	t.mu.Unlock()
	kind := "target"
	if t.isSwarm {
		kind = "swarm iframe"
	}
	title := t.title
	if title == "" {
		title = "Untitled"
	}
	fmt.Printf("[Daemon] Attached %s: %s (%s) [%s]\n", kind, t.id, title, ch)
	if !t.isSwarm {
		t.send("Runtime.addBinding", map[string]any{"name": "__agRequestMetrics"})
		t.send("Runtime.addBinding", map[string]any{"name": "__agSyncHostThemeMode"})
	}
	t.updateNewDocScript()
	ok := t.inject()
	if !t.isSwarm {
		res := t.send("Runtime.evaluate", map[string]any{"expression": themeModeExpr, "returnByValue": true})
		mode, _ := res.EvalValue().(string)
		mode = strings.TrimSpace(mode)
		if mode == "dark" || mode == "light" {
			t.m.mu.Lock()
			t.m.themeModes[t.id] = mode
			t.m.mu.Unlock()
		}
	}
	t.syncMetrics(true)
	t.mu.Lock()
	t.initialDone = true
	t.mu.Unlock()
	return ok
}

func (t *target) handleEvent(msg *Message) {
	if msg.Method == "Runtime.bindingCalled" {
		var p struct {
			Name    string `json:"name"`
			Payload string `json:"payload"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		switch p.Name {
		case "__agRequestMetrics":
			t.opMu.Lock()
			t.activeCids = ParseCascadeIDList(p.Payload)
			t.pruneSessions()
			if len(t.activeCids) > 0 {
				t.syncMetrics(true)
			}
			t.opMu.Unlock()
		case "__agSyncHostThemeMode":
			mode := strings.TrimSpace(p.Payload)
			if mode == "dark" || mode == "light" {
				t.m.mu.Lock()
				prev := t.m.themeModes[t.id]
				t.m.themeModes[t.id] = mode
				t.m.mu.Unlock()
				if prev != mode {
					go t.m.relayThemeModeToChildIframes(t.id, t.ep().Channel, mode)
				}
			}
		}
		return
	}
	t.mu.Lock()
	done := t.initialDone
	t.mu.Unlock()
	if !done {
		return
	}
	switch msg.Method {
	case "Page.frameNavigated", "Page.loadEventFired", "Page.domContentEventFired":
		var p struct {
			Frame *struct {
				ParentID string `json:"parentId"`
			} `json:"frame"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Frame != nil && p.Frame.ParentID != "" {
			return
		}
		t.mu.Lock()
		if t.navTimer != nil {
			t.navTimer.Stop()
		}
		t.navTimer = time.AfterFunc(120*time.Millisecond, func() {
			t.mu.Lock()
			t.navTimer = nil
			t.mu.Unlock()
			t.opMu.Lock()
			t.checkAndInject()
			t.opMu.Unlock()
		})
		t.mu.Unlock()
	}
}

func (t *target) onClose() {
	t.mu.Lock()
	if t.navTimer != nil {
		t.navTimer.Stop()
		t.navTimer = nil
	}
	t.mu.Unlock()
	t.m.mu.Lock()
	if cur, ok := t.m.attached[t.id]; ok && cur == t {
		delete(t.m.attached, t.id)
		delete(t.m.themeModes, t.id)
		t.m.watchConversations(t.id, nil)
	}
	t.m.mu.Unlock()
}

func (m *Manager) attach(ct shared.CdpTarget, ep bundle.Endpoint) bool {
	if ct.WebSocketDebuggerURL == "" {
		return false
	}
	t := &target{
		m:          m,
		id:         ct.ID,
		title:      ct.Title,
		isSwarm:    ct.Type == "iframe" && strings.HasPrefix(ct.URL, "plugin://swarm--"),
		endpoint:   ep,
		parentID:   ct.ParentID,
		currentURL: ct.URL,
		pushed:     map[string]pushedSession{},
	}
	t.hostConfirmed = t.isSwarm
	m.mu.Lock()
	m.attached[t.id] = t
	m.mu.Unlock()

	result := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		conn, err := Dial(ctx, ct.WebSocketDebuggerURL)
		cancel()
		if err != nil {
			t.onClose()
			result <- false
			return
		}
		t.mu.Lock()
		t.conn = conn
		t.mu.Unlock()
		m.mu.Lock()
		cur, still := m.attached[t.id]
		m.mu.Unlock()
		if !still || cur != t {
			conn.Close()
			result <- false
			return
		}
		go func() {
			for msg := range conn.Events() {
				t.handleEvent(msg)
			}
			t.onClose()
		}()
		result <- t.handshake()
	}()
	select {
	case ok := <-result:
		return ok
	case <-time.After(3 * time.Second):
		return false
	}
}

func (t *target) close() {
	if c := t.getConn(); c != nil {
		c.Close()
	}
}

func (m *Manager) snapshot() []*target {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*target, 0, len(m.attached))
	for _, t := range m.attached {
		out = append(out, t)
	}
	return out
}

func (m *Manager) AttachedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.attached)
}

func (m *Manager) Sync() {
	if !m.syncMu.TryLock() {
		return
	}
	defer m.syncMu.Unlock()
	m.tick = (m.tick + 1) % 6000
	if m.tick%24 == 1 && m.LogTrim != nil {
		m.LogTrim()
	}
	bundleChanged := m.bundle.RebuildIfNeeded()
	cfg := m.bundle.Config()
	if !cfg.Enabled {
		m.mu.Lock()
		m.themeModes = map[string]string{}
		m.mu.Unlock()
	}
	heartbeat := bundleChanged || m.tick%6 == 1
	now := time.Now()
	discover := m.runOnce || bundleChanged || m.AttachedCount() == 0 || now.Sub(m.lastDiscovery) >= targetDiscoveryInterval

	if !discover {
		for _, t := range m.snapshot() {
			if !t.confirmed() {
				continue
			}
			t.opMu.Lock()
			if heartbeat {
				t.checkAndInject()
			}
			t.syncMetrics(false)
			t.opMu.Unlock()
		}
		return
	}

	m.lastDiscovery = now
	activeIDs := map[string]bool{}
	seenRaw := map[string]bool{}
	type item struct {
		page shared.CdpTarget
		ep   bundle.Endpoint
	}
	var pendingPages, pendingIframes []item
	probed := map[int]bool{}

	for _, ep := range cfg.Endpoints {
		port := shared.ReadPortFromFile(ep.PortFile)
		var targets []shared.CdpTarget
		if port > 0 && !probed[port] {
			probed[port] = true
			targets = shared.FetchTargetsFromPort(port, 0)
		}
		if len(targets) == 0 && ep.ChannelInstalled && ep.FallbackPort > 0 && !probed[ep.FallbackPort] &&
			(m.runOnce || !now.Before(m.fallbackBackoff[ep.FallbackPort])) {
			probed[ep.FallbackPort] = true
			targets = shared.FetchTargetsFromPort(ep.FallbackPort, 0)
			if len(targets) == 0 {
				m.fallbackBackoff[ep.FallbackPort] = now.Add(10 * time.Second)
			} else {
				delete(m.fallbackBackoff, ep.FallbackPort)
			}
		}
		if len(targets) == 0 {
			continue
		}
		m.mu.Lock()
		for _, tg := range targets {
			if tg.ID == "" {
				continue
			}
			seenRaw[tg.ID] = true
			m.meta[tg.ID] = targetMeta{typ: tg.Type, parentID: tg.ParentID, channel: ep.Channel}
			if origin, ok := m.skipped[tg.ID]; ok && (!shared.IsAllowedOrigin(tg.URL, cfg.AllowedOrigins) || TargetOriginKey(tg.URL) != origin) {
				delete(m.skipped, tg.ID)
			}
		}
		for _, tg := range targets {
			isPage := tg.Type == "page" || (tg.Type == "iframe" && strings.HasPrefix(tg.URL, "plugin://swarm--"))
			if !isPage || tg.WebSocketDebuggerURL == "" || strings.HasPrefix(tg.URL, "devtools://") ||
				!shared.IsAllowedOrigin(tg.URL, cfg.AllowedOrigins) || strings.Contains(tg.Title, "Launchpad") {
				continue
			}
			if origin, ok := m.skipped[tg.ID]; ok && origin == TargetOriginKey(tg.URL) {
				continue
			}
			activeIDs[tg.ID] = true
			if tg.Type == "iframe" {
				pendingIframes = append(pendingIframes, item{tg, ep})
			} else {
				pendingPages = append(pendingPages, item{tg, ep})
			}
		}
		m.mu.Unlock()
	}

	syncBatch := func(items []item) {
		var wg sync.WaitGroup
		for _, it := range items {
			m.mu.Lock()
			t, exists := m.attached[it.page.ID]
			m.mu.Unlock()
			if !exists {
				wg.Add(1)
				go func(it item) {
					defer wg.Done()
					m.attach(it.page, it.ep)
				}(it)
				continue
			}
			t.mu.Lock()
			t.endpoint = it.ep
			if it.page.ParentID != "" {
				t.parentID = it.page.ParentID
			}
			if it.page.URL != "" {
				t.currentURL = it.page.URL
			}
			confirmed := t.hostConfirmed
			t.mu.Unlock()
			if confirmed {
				t.opMu.Lock()
				if bundleChanged {
					t.updateNewDocScript()
				}
				if heartbeat {
					t.checkAndInject()
				}
				t.syncMetrics(bundleChanged)
				t.opMu.Unlock()
			}
		}
		wg.Wait()
	}
	syncBatch(pendingPages)
	syncBatch(pendingIframes)

	if len(pendingPages) == 0 && m.AttachedCount() == 0 {
		m.relaunch.MaybeAutoRelaunch(cfg, m.safeMode, m.runOnce)
	} else if !m.runOnce {
		m.relaunch.ClearPending()
	}

	m.mu.Lock()
	for id, t := range m.attached {
		if !activeIDs[id] {
			delete(m.attached, id)
			delete(m.themeModes, id)
			m.watchConversations(id, nil)
			go t.close()
		}
	}
	if len(seenRaw) > 0 {
		for id := range m.skipped {
			if !seenRaw[id] {
				delete(m.skipped, id)
			}
		}
		for id := range m.meta {
			if !seenRaw[id] {
				delete(m.meta, id)
			}
		}
	} else if len(m.attached) == 0 {
		m.meta = map[string]targetMeta{}
	}
	m.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := m.attached
	m.attached = map[string]*target{}
	m.themeModes = map[string]string{}
	m.mu.Unlock()
	if m.metrics != nil {
		m.metrics.Close()
	}
	for _, t := range list {
		t.close()
	}
}
