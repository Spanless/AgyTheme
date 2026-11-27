package metrics

import (
	"regexp"
	"strings"
)

var cancelRe = regexp.MustCompile(`(?i)(\bcontext\s+canceled\b|\bcancell?ed\s+by\s+user\b|\baborted\s+by\s+user\b|\buser\s+cancell?ed\b)`)

func IsCancelText(msg string) bool {
	return cancelRe.MatchString(strings.TrimSpace(msg))
}

var ctrlRunsRe = regexp.MustCompile(`[\x00-\x1f\x7f]+`)

func oneLine(s string, n int) string {
	s = strings.ToValidUTF8(s, "�")
	return truncUTF16(strings.TrimSpace(ctrlRunsRe.ReplaceAllString(s, " ")), n)
}

func truncUTF16(s string, n int) string {
	count := 0
	for i, r := range s {
		w := 1
		if r >= 0x10000 {
			w = 2
		}
		if count+w > n {
			return s[:i]
		}
		count += w
	}
	return s
}
