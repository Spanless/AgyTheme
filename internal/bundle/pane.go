package bundle

import (
	"strings"
	"sync"
)

var (
	paneStyles  = []string{"styles/30-telemetry.css", "pane/pane.css"}
	paneScripts = []string{"runtime/45-telemetry-render.js", "pane/pane.js"}
)

type PanePage struct {
	src  Source
	mu   sync.Mutex
	sig  string
	page []byte
}

func NewPanePage(src Source) *PanePage { return &PanePage{src: src} }

func (p *PanePage) Bytes() []byte {
	sig := p.src.sig(append(append([]string{}, paneStyles...), paneScripts...))
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.page == nil || sig != p.sig {
		p.page = p.build()
		p.sig = sig
	}
	return p.page
}

func (p *PanePage) build() []byte {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"zh-CN\">\n<head>\n<meta charset=\"utf-8\">\n" +
		"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>用量统计</title>\n<style>\n")
	for _, name := range paneStyles {
		b.WriteString(inertIn(p.src.read(name), "</style"))
		b.WriteString("\n")
	}
	b.WriteString("</style>\n</head>\n<body>\n<script>\n(function () {\n")
	for _, name := range paneScripts {
		b.WriteString(inertIn(p.src.read(name), "</script"))
		b.WriteString("\n")
	}
	b.WriteString("})();\n</script>\n</body>\n</html>\n")
	return []byte(b.String())
}

// Escape closing tags so inlined JS/CSS cannot terminate the enclosing element.
func inertIn(s, closeTag string) string {
	return strings.ReplaceAll(s, closeTag, "<\\/"+closeTag[2:])
}
