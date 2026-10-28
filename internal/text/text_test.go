package text

import "testing"

func TestOneLine(t *testing.T) {
	if got := OneLine("  a \n\t b\r\nc  "); got != "a b c" {
		t.Errorf("got %q", got)
	}
	if got := OneLine(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{0: "files", 1: "file", 2: "files"} {
		if got := Plural(n, "file"); got != want {
			t.Errorf("Plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestCut(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"short enough", "abc\ndef", 10, "abc\ndef"},
		{"on a line boundary", "abc\ndef\nghi", 9, "abc\ndef"},
		{"no line break", "abcdefgh", 4, "abcd"},
		{"never inside a rune", "ééé", 3, "é"},
		{"nothing fits", "abc", 0, ""},
		{"negative", "abc", -1, ""},
		{"a line break at the start is no boundary", "\nabcdef", 4, "\nabc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Cut(tt.in, tt.n); got != tt.want {
				t.Errorf("Cut(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
		})
	}
}

func TestCutAtRune(t *testing.T) {
	if got := CutAtRune("a\nbcd", 4); got != "a\nbc" {
		t.Errorf("a line boundary is not looked for, got %q", got)
	}
	if got := CutAtRune("éé", 3); got != "é" {
		t.Errorf("got %q", got)
	}
	if got := CutAtRune("abc", 5); got != "abc" {
		t.Errorf("got %q", got)
	}
}

func TestNumbered(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		first int
		want  string
	}{
		{"empty", "", 3, ""},
		{"single line", "x := 1", 7, "7| x := 1"},
		{"aligned across a width change", "a\n\tb\n\n\tc", 8, " 8| a\n 9| \tb\n10| \n11| \tc"},
	}
	for _, tt := range tests {
		if got := Numbered(tt.in, tt.first); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
