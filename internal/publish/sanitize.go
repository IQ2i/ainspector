package publish

import (
	"regexp"
	"strings"
	"unicode"
)

const zeroWidthSpace = "\u200b"

var mentionRegex = regexp.MustCompile(`(^|[^\w])@(\w)`)

// isBlank matches what a platform may skip before a line is read as a quick action: whitespace
// of any script, control and invisible format characters
func isBlank(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// sanitizeText neutralizes what the LLM must not be able to trigger from a comment body:
// GitLab quick actions (a line starting with /), HTML comments (which could forge
// ainspector markers) and user mentions.
func sanitizeText(s string) string {
	// A lone carriage return ends a line for Markdown too
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if trimmed := strings.TrimLeftFunc(l, isBlank); strings.HasPrefix(trimmed, "/") {
			lines[i] = `\` + trimmed
		}
	}
	s = strings.Join(lines, "\n")
	s = strings.ReplaceAll(s, "<!--", "&lt;!--")
	// A numeric character reference such as &#64; would be decoded into an @ after the check below
	s = strings.ReplaceAll(s, "&#", "&amp;#")
	s = strings.ReplaceAll(s, "&commat;", "&amp;commat;")
	return mentionRegex.ReplaceAllString(s, "$1@"+zeroWidthSpace+"$2")
}

// LogText makes a text safe to print in the log of a job: control characters such as terminal
// escapes are dropped, and a line starting with :: or ## is moved aside as CI runners read these
// as workflow commands.
func LogText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r == '\r' {
			return '\n'
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if t := strings.TrimLeftFunc(l, unicode.IsSpace); strings.HasPrefix(t, "::") || strings.HasPrefix(t, "##") {
			lines[i] = "> " + t
		}
	}
	return strings.Join(lines, "\n")
}
