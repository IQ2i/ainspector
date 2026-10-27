package publish

import (
	"regexp"
	"strings"
)

const zeroWidthSpace = "\u200b"

var mentionRegex = regexp.MustCompile(`(^|[^\w])@(\w)`)

// sanitizeText neutralizes what the LLM must not be able to trigger from a comment body:
// GitLab quick actions (a line starting with /), HTML comments (which could forge
// ainspector markers) and user mentions.
func sanitizeText(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), "/") {
			lines[i] = `\` + strings.TrimLeft(l, " \t")
		}
	}
	s = strings.Join(lines, "\n")
	s = strings.ReplaceAll(s, "<!--", "&lt;!--")
	return mentionRegex.ReplaceAllString(s, "$1@"+zeroWidthSpace+"$2")
}
