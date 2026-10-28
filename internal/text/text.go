// Package text holds the small string helpers shared by the packages that write logs, prompts
// and comments.
package text

import (
	"strings"
	"unicode/utf8"
)

// MinPartialBytes is the room a file needs to be worth including truncated
const MinPartialBytes = 200

// TruncationMarker ends a text that was cut to fit its limit
const TruncationMarker = "\n... (truncated)"

// OneLine collapses every run of whitespace, line breaks included, into a single space
func OneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Plural returns word, with an s unless n is one
func Plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// Cut shortens s to at most n bytes, on a line boundary when there is one and never inside a rune
func Cut(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = CutAtRune(s, n)
	if i := strings.LastIndex(s, "\n"); i > 0 {
		return s[:i]
	}
	return s
}

// CutAtRune shortens s to at most n bytes without splitting a multi-byte character
func CutAtRune(s string, n int) string {
	if n >= len(s) {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
