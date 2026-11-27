package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	agytheme "github.com/Spanless/AgyTheme"
	"github.com/Spanless/AgyTheme/internal/shared"
)

var ModuleKeys = []string{"theme", "i18n", "metrics", "deadlockHealer", "zoomShortcuts", "cdpAutoRelaunch", "customCss", "customJs"}

type Endpoint struct {
	Channel          string `json:"channel"`
	PortFile         string `json:"portFile"`
	FallbackPort     int    `json:"fallbackPort"`
	ChannelInstalled bool   `json:"channelInstalled"`
	ConversationsDir string `json:"conversationsDir"`
}

type CliOpts struct {
	SafeMode         bool
	RunOnce          bool
	Channel          string
	PortFile         string
	ConversationsDir string
}

type ActiveConfig struct {
	Enabled              bool
	Channel              string
	Preset               string
	TelemetryLayout      string
	TelemetryCacheMode   string
	ContextLimit         any
	ManualContextLimit   float64
	FallbackContextLimit float64
	Modules              map[string]any
	ThemeVars            map[string]any
	Selectors            map[string]any
	AllowedOrigins       []string
	Endpoints            []Endpoint
}

func (c *ActiveConfig) ModuleOn(name string) bool {
	return Truthy(c.Modules[name])
}

func Truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case float64:
		return t != 0 && !math.IsNaN(t)
	case string:
		return t != ""
	default:
		return true
	}
}

func ToNumber(v any) float64 {
	switch t := v.(type) {
	case nil:
		return 0
	case bool:
		if t {
			return 1
		}
		return 0
	case float64:
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return math.NaN()
		}
		return f
	default:
		return math.NaN()
	}
}

func strOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return shared.FormatJSNumber(t)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

type InstallState struct {
	Version     string          `json:"version"`
	PrevVersion string          `json:"prevVersion,omitempty"`
	Features    map[string]bool `json:"features,omitempty"`
}

func ResolveChannelPaths(channel string, userCfg map[string]any, cli CliOpts) Endpoint {
	home, _ := os.UserHomeDir()
	isInsiders := channel == "insiders"
	appDirName := "Antigravity"
	geminiSubDir := filepath.Join(".gemini", "antigravity")
	dotName := ".antigravity"
	fallbackPort := 9222
	if isInsiders {
		appDirName = "Antigravity - Insiders"
		geminiSubDir = filepath.Join(".gemini", "antigravity-insiders")
		dotName = ".antigravity-insiders"
		fallbackPort = 9223
	}
	defaultPortFile := DefaultPortFile(appDirName)
	defaultConvDir := filepath.Join(home, geminiSubDir, "conversations")
	dotDir := filepath.Join(home, dotName)
	if raw, err := os.ReadFile(filepath.Join(dotDir, "argv.json")); err == nil {
		if p := shared.ExtractJsoncNumberProperty(string(raw), "remote-debugging-port"); p > 0 {
			fallbackPort = p
		}
	}
	portFile := cli.PortFile
	if portFile == "" {
		portFile = strOf(userCfg["portFile"])
	}
	if portFile == "" {
		portFile = defaultPortFile
	}
	installed := shared.FileExists(portFile) || shared.FileExists(filepath.Dir(defaultPortFile)) || shared.FileExists(dotDir)
	if fp := ToNumber(userCfg["fallbackPort"]); fp > 0 {
		fallbackPort = int(fp)
	}
	convDir := cli.ConversationsDir
	if convDir == "" {
		convDir = strOf(userCfg["conversationsDir"])
	}
	if convDir == "" {
		convDir = defaultConvDir
	}
	return Endpoint{
		Channel:          channel,
		PortFile:         portFile,
		FallbackPort:     fallbackPort,
		ChannelInstalled: installed,
		ConversationsDir: convDir,
	}
}

func DefaultPortFile(appDirName string) string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", appDirName, "DevToolsActivePort")
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appData, appDirName, "DevToolsActivePort")
	default:
		xdg := os.Getenv("XDG_CONFIG_HOME")
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		return filepath.Join(xdg, appDirName, "DevToolsActivePort")
	}
}

func ResolveActiveChannelEndpoints(mode string, userCfg map[string]any, cli CliOpts) []Endpoint {
	explicit := cli.PortFile != "" || strOf(userCfg["portFile"]) != ""
	if explicit {
		ch := "stable"
		if mode == "insiders" {
			ch = "insiders"
		}
		return []Endpoint{ResolveChannelPaths(ch, userCfg, cli)}
	}
	if mode == "stable" || mode == "insiders" {
		return []Endpoint{ResolveChannelPaths(mode, userCfg, cli)}
	}
	return []Endpoint{ResolveChannelPaths("stable", userCfg, cli), ResolveChannelPaths("insiders", userCfg, cli)}
}

func NormalizeOverrideDict(raw map[string]any) map[string]string {
	out := map[string]string{}
	if raw == nil {
		return out
	}
	src := raw
	if exact, ok := raw["exact"].(map[string]any); ok {
		src = exact
	}
	for k, v := range src {
		if strings.HasPrefix(k, "_") {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

type Source struct {
	FS  fs.FS
	Dir string
}

func EmbeddedSource() Source {
	sub, err := fs.Sub(agytheme.CoreFS, "core")
	if err != nil {
		panic(err)
	}
	return Source{FS: sub}
}

func DirSource(dir string) Source {
	return Source{FS: os.DirFS(dir), Dir: dir}
}

func (s Source) list(dir, ext string) []string {
	entries, err := fs.ReadDir(s.FS, dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			out = append(out, dir+"/"+e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func (s Source) read(name string) string {
	b, err := fs.ReadFile(s.FS, name)
	if err != nil {
		return ""
	}
	return string(b)
}

func (s Source) exists(name string) bool {
	_, err := fs.Stat(s.FS, name)
	return err == nil
}

func (s Source) sig(names []string) string {
	if s.Dir == "" {
		return "embedded"
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fileSig(filepath.Join(s.Dir, filepath.FromSlash(n))))
	}
	return strings.Join(parts, "|")
}

func fileSig(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return "0:0"
	}
	return fmt.Sprintf("%s:%d", shared.FormatJSNumber(shared.MtimeMs(fi)), fi.Size())
}

type Options struct {
	RootDir  string
	Source   Source
	Cli      CliOpts
	SidePane bool
}

type Controller struct {
	opts Options

	mu           sync.RWMutex
	sig          string
	script       string
	patchVersion string
	activeConfig ActiveConfig
}

const (
	PresetLight = "light"
	PresetDark  = "dark"
)

func NormalizePreset(raw string) string {
	if strings.TrimSpace(raw) == PresetDark {
		return PresetDark
	}
	return PresetLight
}

func NewController(opts Options) *Controller {
	c := &Controller{opts: opts}
	c.activeConfig = ActiveConfig{
		Enabled:              true,
		Channel:              "auto",
		Preset:               PresetLight,
		TelemetryLayout:      "compact",
		TelemetryCacheMode:   "step",
		ContextLimit:         "auto",
		FallbackContextLimit: 256000,
		Modules:              defaultModules(),
		ThemeVars:            map[string]any{},
		Selectors:            map[string]any{},
		AllowedOrigins:       shared.DefaultAllowedOrigins,
	}
	return c
}

func defaultModules() map[string]any {
	m := map[string]any{}
	for _, k := range ModuleKeys {
		m[k] = true
	}
	return m
}

func (c *Controller) UserDir() string { return filepath.Join(c.opts.RootDir, "user") }

func (c *Controller) State() (ActiveConfig, string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activeConfig, c.script, c.patchVersion
}

func (c *Controller) Config() ActiveConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.activeConfig
}

type BootPayload struct {
	Version            string            `json:"version"`
	Platform           string            `json:"platform"`
	Enabled            bool              `json:"enabled"`
	Preset             string            `json:"preset"`
	TelemetryLayout    string            `json:"telemetryLayout"`
	TelemetryCacheMode string            `json:"telemetryCacheMode"`
	CSSText            string            `json:"cssText"`
	CustomCSS          string            `json:"customCss"`
	ThemeVars          map[string]any    `json:"themeVars"`
	I18nDict           map[string]string `json:"i18nDict"`
	Modules            map[string]any    `json:"modules"`
	Selectors          map[string]any    `json:"selectors"`
	UserConfig         bootUserConfig    `json:"userConfig"`
	CustomJS           string            `json:"customJs"`
	SidePane           bool              `json:"sidePane"`
}

type bootUserConfig struct {
	Enabled            bool           `json:"enabled"`
	Channel            string         `json:"channel"`
	Preset             string         `json:"preset"`
	TelemetryLayout    string         `json:"telemetryLayout"`
	TelemetryCacheMode string         `json:"telemetryCacheMode"`
	ContextLimit       any            `json:"contextLimit"`
	Modules            map[string]any `json:"modules"`
	ThemeVars          map[string]any `json:"themeVars"`
}

const placeholder = "})(__AG_BOOT_PAYLOAD__);"

func (c *Controller) RebuildIfNeeded() bool {
	src := c.opts.Source
	styleFiles := src.list("styles", ".css")
	runtimeFiles := src.list("runtime", ".js")
	coreFiles := append(append(append([]string{}, styleFiles...), "i18n.json"), runtimeFiles...)
	safe := c.opts.Cli.SafeMode
	userDir := c.UserDir()
	userFiles := []string{}
	if !safe {
		userFiles = []string{
			filepath.Join(userDir, "config.json"),
			filepath.Join(userDir, "custom.css"),
			filepath.Join(userDir, "i18n.override.json"),
			filepath.Join(userDir, "custom.js"),
		}
	}
	statePath := filepath.Join(c.opts.RootDir, "state.json")
	userSigs := make([]string, 0, len(userFiles)+1)
	for _, f := range userFiles {
		userSigs = append(userSigs, fileSig(f))
	}
	userSigs = append(userSigs, fileSig(statePath))
	sig := src.sig(coreFiles) + "|" + strings.Join(userSigs, "|") +
		fmt.Sprintf("|safe:%v|cli:%s", safe, c.opts.Cli.Channel)

	c.mu.RLock()
	unchanged := sig == c.sig && c.script != ""
	c.mu.RUnlock()
	if unchanged {
		return false
	}

	userCfg := map[string]any{}
	if !safe {
		shared.ReadJSONFile(filepath.Join(userDir, "config.json"), &userCfg)
	}
	var state InstallState
	shared.ReadJSONFile(statePath, &state)

	enabled := true
	if !safe {
		if v, ok := userCfg["enabled"].(bool); ok && !v {
			enabled = false
		}
	}
	channel := c.opts.Cli.Channel
	if channel == "" {
		channel = strOf(userCfg["channel"])
	}
	if channel == "" {
		channel = "auto"
	}
	preset := NormalizePreset(strOf(userCfg["preset"]))
	telemetryLayout := "compact"
	if strOf(userCfg["telemetryLayout"]) == "detailed" {
		telemetryLayout = "detailed"
	}
	telemetryCacheMode := "step"
	if strOf(userCfg["telemetryCacheMode"]) == "session" {
		telemetryCacheMode = "session"
	}

	rawLimit, hasLimit := userCfg["contextLimit"]
	limitStr := strings.TrimSpace(strOf(rawLimit))
	isAuto := !hasLimit || rawLimit == nil || limitStr == "" || strings.ToLower(limitStr) == "auto"
	manual := 0.0
	if !isAuto {
		if n := ToNumber(rawLimit); n > 0 {
			manual = n
		}
	}
	fallback := 256000.0
	if n := ToNumber(userCfg["fallbackContextLimit"]); n > 0 {
		fallback = n
	}
	var contextLimit any = "auto"
	if manual > 0 {
		contextLimit = manual
	}

	modules := defaultModules()
	if feats, ok := userCfg["features"].(map[string]any); ok {
		for k, v := range feats {
			modules[k] = v
		}
	}
	rawMods, _ := userCfg["modules"].(map[string]any)
	for k, v := range rawMods {
		modules[k] = v
	}
	if mc, ok := modules["metricsCapsule"].(bool); ok {
		if _, isBool := rawModsBool(userCfg, "metrics"); !isBool {
			modules["metrics"] = mc
		}
	}

	installed := func(feature string) bool {
		if state.Features == nil {
			return true
		}
		v, ok := state.Features[feature]
		return !ok || v
	}
	hasTheme := installed("theme") && (src.exists("styles/10-light.css") || src.exists("styles/00-base.css"))
	hasI18n := installed("i18n") && src.exists("i18n.json") && src.exists("runtime/60-i18n-engine.js")
	hasMetrics := installed("metrics") && src.exists("runtime/50-telemetry-ui.js")
	hasGuards := installed("fixes") && src.exists("runtime/10-guards.js")
	if !hasTheme {
		modules["theme"] = false
	}
	if !hasI18n {
		modules["i18n"] = false
	}
	if !hasMetrics {
		modules["metrics"] = false
	}
	if !hasGuards {
		modules["deadlockHealer"] = false
		modules["zoomShortcuts"] = false
	}

	themeVars := map[string]any{}
	if !safe {
		if tv, ok := userCfg["themeVars"].(map[string]any); ok {
			themeVars = tv
		}
	}
	endpoints := ResolveActiveChannelEndpoints(channel, userCfg, c.opts.Cli)

	selectors := map[string]any{}
	if sel, ok := userCfg["selectors"].(map[string]any); ok {
		for k, v := range sel {
			selectors[k] = v
		}
	}
	if v, ok := selectors["branchPillInBox"]; ok && Truthy(v) && !strings.Contains(strOf(v), "project-branch-picker") {
		delete(selectors, "branchPillInBox")
	}
	if v, ok := selectors["homeGreeting"]; ok && Truthy(v) && !strings.Contains(strOf(v), "home-splash-content") {
		delete(selectors, "homeGreeting")
	}
	if s, ok := selectors["excludeI18n"].(string); ok && strings.TrimSpace(s) != "" && !strings.Contains(s, ".monaco-editor") {
		selectors["excludeI18n"] = s + `, .monaco-editor, .monaco-diff-editor, .xterm, [data-markdown-root], [data-testid="user-message-content"], .agy-user-bubble, [class*="onyx-key"]`
	}

	allowed := shared.DefaultAllowedOrigins
	if arr, ok := userCfg["allowedOrigins"].([]any); ok {
		allowed = make([]string, 0, len(arr))
		for _, a := range arr {
			allowed = append(allowed, strOf(a))
		}
	}

	cfg := ActiveConfig{
		Enabled:              enabled,
		Channel:              channel,
		Preset:               preset,
		TelemetryLayout:      telemetryLayout,
		TelemetryCacheMode:   telemetryCacheMode,
		ContextLimit:         contextLimit,
		ManualContextLimit:   manual,
		FallbackContextLimit: fallback,
		Modules:              modules,
		ThemeVars:            themeVars,
		Selectors:            selectors,
		AllowedOrigins:       allowed,
		Endpoints:            endpoints,
	}

	var cssParts []string
	for _, f := range styleFiles {
		if feat, ok := FeatureOfCoreFile(f); ok && !installed(feat) {
			continue
		}
		base := f[strings.LastIndex(f, "/")+1:]
		if base == "30-telemetry.css" {
			if !cfg.ModuleOn("metrics") {
				continue
			}
		} else if v, ok := modules["theme"].(bool); ok && !v {
			continue
		}
		cssParts = append(cssParts, src.read(f))
	}
	baseCSS := strings.Join(cssParts, "\n\n")
	customCSS := ""
	if !safe && cfg.ModuleOn("customCss") {
		customCSS = readText(filepath.Join(userDir, "custom.css"))
	}
	merged := map[string]string{}
	if cfg.ModuleOn("i18n") {
		var base map[string]any
		_ = json.Unmarshal([]byte(shared.StripUtf8Bom(src.read("i18n.json"))), &base)
		for k, v := range base {
			if s, ok := v.(string); ok {
				merged[k] = s
			}
		}
		if !safe {
			var override map[string]any
			shared.ReadJSONFile(filepath.Join(userDir, "i18n.override.json"), &override)
			for k, v := range NormalizeOverrideDict(override) {
				merged[k] = v
			}
		}
	}
	customJS := ""
	if !safe && cfg.ModuleOn("customJs") {
		customJS = readText(filepath.Join(userDir, "custom.js"))
	}
	runtimeParts := make([]string, 0, len(runtimeFiles))
	for _, f := range runtimeFiles {
		if feat, ok := FeatureOfCoreFile(f); ok && !installed(feat) {
			continue
		}
		runtimeParts = append(runtimeParts, src.read(f))
	}
	template := strings.Join(runtimeParts, "\n")

	dictJSON, _ := json.Marshal(merged)
	cfgJSON, _ := json.Marshal(map[string]any{
		"preset": preset, "telemetryLayout": telemetryLayout, "telemetryCacheMode": telemetryCacheMode,
		"modules": modules, "themeVars": themeVars, "selectors": selectors,
	})
	h := sha256.New()
	fmt.Fprint(h, enabled)
	h.Write([]byte(template))
	h.Write([]byte(baseCSS))
	h.Write([]byte(customCSS))
	h.Write(dictJSON)
	h.Write([]byte(customJS))
	h.Write(cfgJSON)
	hash := hex.EncodeToString(h.Sum(nil))[:8]

	channelTag := "stable"
	if channel == "insiders" {
		channelTag = "insiders"
	}
	patchVersion := fmt.Sprintf("%s-%s-%s", agytheme.Version, channelTag, hash)

	boot := BootPayload{
		Version:            patchVersion,
		Platform:           shared.Platform(),
		Enabled:            enabled,
		Preset:             preset,
		TelemetryLayout:    telemetryLayout,
		TelemetryCacheMode: telemetryCacheMode,
		CSSText:            baseCSS,
		CustomCSS:          customCSS,
		ThemeVars:          themeVars,
		I18nDict:           merged,
		Modules:            modules,
		Selectors:          selectors,
		UserConfig: bootUserConfig{
			Enabled: enabled, Channel: channel, Preset: preset,
			TelemetryLayout: telemetryLayout, TelemetryCacheMode: telemetryCacheMode,
			ContextLimit: contextLimit, Modules: modules, ThemeVars: themeVars,
		},
		CustomJS: customJS,
		SidePane: c.opts.SidePane,
	}
	bootJSON, err := shared.MarshalNoEscape(boot)
	if err != nil {
		bootJSON = []byte("{}")
	}
	script := strings.Replace(template, placeholder, "})("+string(bootJSON)+");", 1)

	c.mu.Lock()
	c.activeConfig = cfg
	c.script = script
	c.patchVersion = patchVersion
	c.sig = sig
	c.mu.Unlock()
	return true
}

func rawModsBool(userCfg map[string]any, key string) (bool, bool) {
	merged := map[string]any{}
	if feats, ok := userCfg["features"].(map[string]any); ok {
		for k, v := range feats {
			merged[k] = v
		}
	}
	if mods, ok := userCfg["modules"].(map[string]any); ok {
		for k, v := range mods {
			merged[k] = v
		}
	}
	b, ok := merged[key].(bool)
	return b, ok
}

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
