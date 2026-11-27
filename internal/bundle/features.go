package bundle

import (
	"fmt"
	"regexp"
	"strings"
)

type FeatureGroup struct {
	ID         string
	Label      string
	ConfigKeys []string
	CoreFiles  []string
	Aliases    []string
}

var FeatureGroups = []FeatureGroup{
	{
		ID: "theme", Label: "浅色 / 深色主题 (light / dark)", ConfigKeys: []string{"theme"},
		CoreFiles: []string{"styles/00-base.css", "styles/10-light.css", "styles/20-dark.css", "runtime/20-theme-sync.js"},
		Aliases:   []string{"theme", "style", "styles", "css", "dark", "light"},
	},
	{
		ID: "i18n", Label: "中文界面本地化 (900+ 词条)", ConfigKeys: []string{"i18n"},
		CoreFiles: []string{"i18n.json", "runtime/60-i18n-engine.js"},
		Aliases:   []string{"i18n", "zh", "cn", "chinese", "locale", "l10n", "translate"},
	},
	{
		ID: "metrics", Label: "实时会话状态指示、一/二级统计面板与每轮尾部统计", ConfigKeys: []string{"metrics"},
		CoreFiles: []string{"styles/30-telemetry.css", "runtime/30-antigravity-probe.js", "runtime/40-status-machine.js", "runtime/45-telemetry-render.js", "runtime/50-telemetry-ui.js"},
		Aliases:   []string{"metrics", "status", "telemetry", "stats", "panel", "metricscapsule"},
	},
	{
		ID: "fixes", Label: "设置死锁修复与 Cmd/Ctrl + -/=/0 界面缩放快捷键", ConfigKeys: []string{"deadlockHealer", "zoomShortcuts"},
		CoreFiles: []string{"runtime/10-guards.js"},
		Aliases:   []string{"fixes", "fix", "guards", "deadlock", "deadlockhealer", "zoom", "zoomshortcuts"},
	},
}

func FeatureIDs() []string {
	out := make([]string, len(FeatureGroups))
	for i, g := range FeatureGroups {
		out[i] = g.ID
	}
	return out
}

func FeatureOfCoreFile(rel string) (string, bool) {
	for _, g := range FeatureGroups {
		for _, f := range g.CoreFiles {
			if f == rel {
				return g.ID, true
			}
		}
	}
	return "", false
}

var featureSplitRe = regexp.MustCompile(`[\s,]+`)

func parseFeatureTokenList(raw string) ([]string, error) {
	var tokens []string
	for _, t := range featureSplitRe.Split(raw, -1) {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tokens = append(tokens, t)
		}
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("Empty feature list. Expected one or more of: theme, i18n, metrics, fixes, all")
	}
	var ids []string
	for _, tok := range tokens {
		if tok == "all" || tok == "full" {
			return FeatureIDs(), nil
		}
		resolved := ""
		for _, g := range FeatureGroups {
			if tok == g.ID {
				resolved = g.ID
			}
			for _, a := range g.Aliases {
				if tok == a {
					resolved = g.ID
				}
			}
		}
		if resolved == "" {
			return nil, fmt.Errorf("Unknown feature %q. Valid options: theme, i18n, metrics, fixes, all", tok)
		}
		dup := false
		for _, id := range ids {
			dup = dup || id == resolved
		}
		if !dup {
			ids = append(ids, resolved)
		}
	}
	return ids, nil
}

type FeatureSelection struct {
	Explicit     bool
	Selected     map[string]bool
	ModulesPatch map[string]bool
}

func ParseFeatureSelection(only, without string) (FeatureSelection, error) {
	hasOnly := strings.TrimSpace(only) != ""
	hasWithout := strings.TrimSpace(without) != ""
	sel := map[string]bool{}
	for _, id := range FeatureIDs() {
		sel[id] = true
	}
	if hasOnly {
		ids, err := parseFeatureTokenList(only)
		if err != nil {
			return FeatureSelection{}, err
		}
		for k := range sel {
			sel[k] = false
		}
		for _, id := range ids {
			sel[id] = true
		}
	}
	if hasWithout {
		ids, err := parseFeatureTokenList(without)
		if err != nil {
			return FeatureSelection{}, err
		}
		for _, id := range ids {
			sel[id] = false
		}
	}
	patch := map[string]bool{}
	for _, g := range FeatureGroups {
		for _, k := range g.ConfigKeys {
			patch[k] = sel[g.ID]
		}
	}
	return FeatureSelection{Explicit: hasOnly || hasWithout, Selected: sel, ModulesPatch: patch}, nil
}
