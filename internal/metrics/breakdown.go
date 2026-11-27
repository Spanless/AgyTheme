package metrics

import (
	"sort"
	"strconv"
	"strings"
)

type ContextBreakdownItem struct {
	Name   string  `json:"name"`
	Tokens float64 `json:"tokens"`
	Count  int     `json:"count,omitempty"`
}

type ContextBreakdownGroup struct {
	Key        string                 `json:"key"`
	Label      string                 `json:"label"`
	Tokens     float64                `json:"tokens"`
	Expandable bool                   `json:"expandable,omitempty"`
	Count      int                    `json:"count,omitempty"`
	Items      []ContextBreakdownItem `json:"items,omitempty"`
}

func isMCPName(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	if low == "" {
		return false
	}
	return strings.HasPrefix(low, "mcp_") ||
		strings.HasPrefix(low, "mcp:") ||
		strings.HasPrefix(low, "mcp-") ||
		strings.HasPrefix(low, "mcp/") ||
		strings.Contains(low, "__") ||
		strings.Contains(low, "/")
}

func isMCPSource(src string) bool {
	return strings.Contains(strings.ToUpper(src), "MCP")
}

func formatChatToolLabel(toolName, toolSource string, mcpNames map[string]bool) string {
	if isMCPSource(toolSource) || mcpNames[toolName] || isMCPName(toolName) {
		return "MCP · " + toolName
	}
	switch toolName {
	case "read_file", "view_file", "view_file_outline", "view_code_item":
		return toolName + " · 读取文件"
	case "run_command", "command_status", "send_command_input":
		return toolName + " · 终端输出"
	case "write_file", "edit_file", "write_to_file", "replace_file_content", "multi_replace_file_content":
		return toolName + " · 编辑文件"
	case "find_by_name", "grep_search", "list_dir":
		return toolName + " · 搜索代码"
	case "search_web", "read_url_content":
		return toolName + " · 联网检索"
	default:
		return toolName
	}
}

func formatChatStepType(stepType string) string {
	clean := strings.TrimPrefix(strings.TrimSpace(stepType), "CORTEX_STEP_TYPE_")
	switch clean {
	case "PLANNER_RESPONSE":
		return "模型思考与回复"
	case "USER_INPUT":
		return "用户输入"
	case "SYSTEM_MESSAGE":
		return "系统消息"
	case "CHECKPOINT":
		return "压缩摘要"
	case "ERROR_MESSAGE":
		return "错误信息"
	case "":
		return "其他消息"
	default:
		return clean
	}
}

func parseChatChildLabel(rawName string, st *convState, mcpNames map[string]bool) string {
	idxPart, typePart, hasColon := strings.Cut(rawName, ":")
	if hasColon {
		if idx, err := strconv.ParseInt(strings.TrimSpace(idxPart), 10, 64); err == nil {
			if s := st.step(idx); s != nil {
				if s.ToolName != "" {
					return formatChatToolLabel(s.ToolName, s.ToolSource, mcpNames)
				}
				if s.Type != "" {
					return formatChatStepType(s.Type)
				}
			}
		}
		return formatChatStepType(typePart)
	}
	return formatChatStepType(rawName)
}

func cfgGroupLabel(g rawTokenGroup) string {
	switch g.Type {
	case "TOKEN_TYPE_SKILLS":
		return "Skills"
	case "TOKEN_TYPE_SUBAGENTS":
		return "Subagents"
	case "TOKEN_TYPE_PLUGINS":
		return "Plugins"
	case "TOKEN_TYPE_RULES":
		return "规则"
	case "TOKEN_TYPE_MEMORY":
		return "记忆文件"
	default:
		if g.Name != "" {
			return g.Name
		}
		return strings.TrimPrefix(g.Type, "TOKEN_TYPE_")
	}
}

func sortBreakdownItems(items []ContextBreakdownItem) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Tokens != items[j].Tokens {
			return items[i].Tokens > items[j].Tokens
		}
		return items[i].Name < items[j].Name
	})
}

func buildContextBreakdown(st *convState) ([]ContextBreakdownGroup, float64) {
	if st == nil {
		return nil, 0
	}
	var raw *rawTokenBreakdown
	for i := len(st.gens) - 1; i >= 0; i-- {
		if b := st.gens[i].Breakdown; b != nil && len(b.Groups) > 0 {
			raw = b
			break
		}
	}
	if raw == nil && st.streamBreakdown != nil && len(st.streamBreakdown.Groups) > 0 {
		raw = st.streamBreakdown
	}
	if raw == nil || len(raw.Groups) == 0 {
		return nil, 0
	}

	mcpNames := map[string]bool{}
	for i := range st.steps {
		if st.steps[i].ToolName != "" && isMCPSource(st.steps[i].ToolSource) {
			mcpNames[st.steps[i].ToolName] = true
		}
	}

	var msgTok, mcpTok, toolsTok, sysTok, cfgTok float64
	msgAgg := map[string]*ContextBreakdownItem{}
	var mcpItems, toolsItems, cfgItems []ContextBreakdownItem
	msgSteps := 0

	for _, g := range raw.Groups {
		if g.Tokens <= 0 {
			continue
		}
		upperType := strings.ToUpper(g.Type)
		upperName := strings.ToUpper(g.Name)

		switch {
		case upperType == "TOKEN_TYPE_CHAT_MESSAGES":
			msgTok += g.Tokens
			for _, ch := range g.Children {
				if ch.Tokens <= 0 {
					continue
				}
				msgSteps++
				lbl := parseChatChildLabel(ch.Name, st, mcpNames)
				if cur, ok := msgAgg[lbl]; ok {
					cur.Tokens += ch.Tokens
					cur.Count++
				} else {
					msgAgg[lbl] = &ContextBreakdownItem{Name: lbl, Tokens: ch.Tokens, Count: 1}
				}
			}

		case strings.Contains(upperType, "MCP") || strings.Contains(upperName, "MCP"):
			mcpTok += g.Tokens
			if len(g.Children) > 0 {
				for _, ch := range g.Children {
					if ch.Tokens > 0 {
						mcpItems = append(mcpItems, ContextBreakdownItem{Name: ch.Name, Tokens: ch.Tokens})
					}
				}
			} else if g.Name != "" {
				mcpItems = append(mcpItems, ContextBreakdownItem{Name: g.Name, Tokens: g.Tokens})
			}

		case upperType == "TOKEN_TYPE_TOOLS":
			if len(g.Children) > 0 {
				var childSum float64
				for _, ch := range g.Children {
					if ch.Tokens <= 0 {
						continue
					}
					childSum += ch.Tokens
					if mcpNames[ch.Name] || isMCPName(ch.Name) {
						mcpTok += ch.Tokens
						mcpItems = append(mcpItems, ContextBreakdownItem{Name: ch.Name, Tokens: ch.Tokens})
					} else {
						toolsTok += ch.Tokens
						toolsItems = append(toolsItems, ContextBreakdownItem{Name: ch.Name, Tokens: ch.Tokens})
					}
				}
				if diff := g.Tokens - childSum; diff > 0 {
					toolsTok += diff
				}
			} else {
				toolsTok += g.Tokens
			}

		case upperType == "TOKEN_TYPE_SYSTEM_PROMPT":
			sysTok += g.Tokens

		default:
			cfgTok += g.Tokens
			if len(g.Children) > 1 {
				for _, ch := range g.Children {
					if ch.Tokens > 0 {
						cfgItems = append(cfgItems, ContextBreakdownItem{Name: ch.Name, Tokens: ch.Tokens})
					}
				}
			} else {
				cfgItems = append(cfgItems, ContextBreakdownItem{Name: cfgGroupLabel(g), Tokens: g.Tokens})
			}
		}
	}

	msgItems := make([]ContextBreakdownItem, 0, len(msgAgg))
	for _, it := range msgAgg {
		msgItems = append(msgItems, *it)
	}
	sortBreakdownItems(msgItems)
	sortBreakdownItems(mcpItems)
	sortBreakdownItems(toolsItems)
	sortBreakdownItems(cfgItems)

	var out []ContextBreakdownGroup
	if msgTok > 0 {
		out = append(out, ContextBreakdownGroup{
			Key:        "msg",
			Label:      "消息",
			Tokens:     msgTok,
			Expandable: len(msgItems) > 0,
			Count:      msgSteps,
			Items:      msgItems,
		})
	}
	if mcpTok > 0 {
		out = append(out, ContextBreakdownGroup{
			Key:        "mcp",
			Label:      "MCP 工具",
			Tokens:     mcpTok,
			Expandable: len(mcpItems) > 0,
			Count:      len(mcpItems),
			Items:      mcpItems,
		})
	}
	if toolsTok > 0 {
		out = append(out, ContextBreakdownGroup{
			Key:        "tools",
			Label:      "系统工具",
			Tokens:     toolsTok,
			Expandable: len(toolsItems) > 0,
			Count:      len(toolsItems),
			Items:      toolsItems,
		})
	}
	if sysTok > 0 {
		out = append(out, ContextBreakdownGroup{
			Key:    "sys",
			Label:  "系统提示词",
			Tokens: sysTok,
		})
	}
	if cfgTok > 0 {
		out = append(out, ContextBreakdownGroup{
			Key:        "cfg",
			Label:      "技能与规则",
			Tokens:     cfgTok,
			Expandable: len(cfgItems) > 0,
			Count:      len(cfgItems),
			Items:      cfgItems,
		})
	}

	total := raw.Total
	var sum float64
	for _, g := range out {
		sum += g.Tokens
	}
	if sum > 0 {
		total = sum
	}
	return out, total
}
