package state

import (
	"fmt"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/provider"
)

type fakeMode struct {
	name    string
	scope   collect.Scope
	version string
}

func (m fakeMode) Name() string         { return m.name }
func (m fakeMode) Scope() collect.Scope { return m.scope }
func (m fakeMode) Version() string      { return m.version }

var quality = fakeMode{"quality", collect.ScopeFunction, "quality/1"}

func unit(name string) collect.ReviewUnit {
	return collect.ReviewUnit{
		Name:     name,
		FilePath: "main.go",
		Content:  "func " + name + "() {}",
		Diff:     "+func " + name + "() {}",
	}
}

func TestLegacyFunctionHash(t *testing.T) {
	base := unit("TestFunc")
	hash := LegacyFunctionHash(base)

	if hash != LegacyFunctionHash(base) {
		t.Error("hash is not deterministic")
	}
	if len(hash) != HashLength {
		t.Errorf("hash length = %d, want %d", len(hash), HashLength)
	}

	variants := map[string]func(u *collect.ReviewUnit){
		"content": func(u *collect.ReviewUnit) { u.Content = "func TestFunc() { return }" },
		"path":    func(u *collect.ReviewUnit) { u.FilePath = "other.go" },
		"diff":    func(u *collect.ReviewUnit) { u.Diff = "+other" },
		"name":    func(u *collect.ReviewUnit) { u.Name = "Other" },
	}
	for name, change := range variants {
		u := base
		change(&u)
		if LegacyFunctionHash(u) == hash {
			t.Errorf("changing the %s should change the hash", name)
		}
	}
}

func TestLegacyFunctionHash_Frozen(t *testing.T) {
	u := collect.ReviewUnit{FilePath: "app.py", Name: "add", Content: "def add(a, b):\n    return a + b", Diff: "x"}
	if got := LegacyFunctionHash(u); got != "7950e1499748" {
		t.Errorf("the legacy formula changed: got %s", got)
	}
}

func TestUnitHash(t *testing.T) {
	u := unit("f")
	hash := UnitHash(quality, u, "digest")

	if hash != UnitHash(quality, u, "digest") || len(hash) != HashLength {
		t.Errorf("hash must be deterministic and %d long, got %s", HashLength, hash)
	}
	if hash == LegacyFunctionHash(u) {
		t.Error("unit hash must differ from the legacy hash")
	}

	mutations := map[string]string{
		"mode name":    UnitHash(fakeMode{"security", collect.ScopeFunction, "quality/1"}, u, "digest"),
		"mode version": UnitHash(fakeMode{"quality", collect.ScopeFunction, "quality/2"}, u, "digest"),
		"mode scope":   UnitHash(fakeMode{"quality", collect.ScopeFunctionClass, "quality/1"}, u, "digest"),
		"rules":        UnitHash(quality, u, "other"),
		"path":         UnitHash(quality, collect.ReviewUnit{Name: u.Name, FilePath: "x.go", Content: u.Content, Diff: u.Diff}, "digest"),
		"name":         UnitHash(quality, collect.ReviewUnit{Name: "g", FilePath: u.FilePath, Content: u.Content, Diff: u.Diff}, "digest"),
		"content":      UnitHash(quality, collect.ReviewUnit{Name: u.Name, FilePath: u.FilePath, Content: "other", Diff: u.Diff}, "digest"),
		"diff":         UnitHash(quality, collect.ReviewUnit{Name: u.Name, FilePath: u.FilePath, Content: u.Content, Diff: "other"}, "digest"),
	}
	for what, got := range mutations {
		if got == hash {
			t.Errorf("changing the %s should change the hash", what)
		}
	}
}

func TestUnitHash_FieldBoundaries(t *testing.T) {
	a := UnitHash(quality, collect.ReviewUnit{FilePath: "a", Name: "bc"}, "")
	b := UnitHash(quality, collect.ReviewUnit{FilePath: "ab", Name: "c"}, "")
	if a == b {
		t.Error("moving a character between fields must change the hash")
	}
}

func TestRulesDigest(t *testing.T) {
	if RulesDigest([]string{"a", "b"}) == RulesDigest([]string{"b", "a"}) {
		t.Error("digest should depend on rule order")
	}
	if RulesDigest([]string{"a"}) == RulesDigest([]string{"a", "b"}) {
		t.Error("digest should depend on the rules")
	}
	if RulesDigest(nil) != RulesDigest([]string{}) {
		t.Error("no rules should always give the same digest")
	}
}

func TestFormatMarker(t *testing.T) {
	if got := FormatMarker("abc123def456"); got != "<!-- ainspector:fn:abc123def456 -->" {
		t.Errorf("FormatMarker = %q", got)
	}
}

func TestExtractHashes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{"marker at end", "Some review comment\n\n<!-- ainspector:fn:abc123def456 -->", []string{"abc123def456"}},
		{"marker in middle", "Comment <!-- ainspector:fn:123456789abc --> more text", []string{"123456789abc"}},
		{"several markers", "<!-- ainspector:fn:aaaaaaaaaaaa --> <!-- ainspector:fn:bbbbbbbbbbbb -->", []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb"}},
		{"empty", "", nil},
		{"no marker", "Just a regular comment", nil},
		{"partial marker", "<!-- ainspector:fn: -->", nil},
		{"wrong prefix", "<!-- other:fn:abc123def456 -->", nil},
		{"short hash", "<!-- ainspector:fn:abc123 -->", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractHashes(tt.body)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("ExtractHashes = %v, want %v", got, tt.want)
			}
		})
	}
}

func comment(body string) provider.ExistingComment {
	return provider.ExistingComment{Body: body}
}

func TestLoadIndex_CountsMarkers(t *testing.T) {
	ix := LoadIndex([]provider.ExistingComment{
		comment("Review 1\n\n" + FormatMarker("abc123def456")),
		comment("Review 2\n\n" + FormatMarker("fedcba987654")),
		comment("Same unit again\n\n" + FormatMarker("fedcba987654")),
		comment("Comment without hash"),
	}, "")

	if ix.Len() != 2 {
		t.Errorf("Len = %d, want 2", ix.Len())
	}
}

func TestIndex_FilterUnreviewed_LegacyMarker(t *testing.T) {
	reviewed, fresh := unit("Func1"), unit("Func2")
	ix := LoadIndex([]provider.ExistingComment{comment("x\n\n" + FormatMarker(LegacyFunctionHash(reviewed)))}, "")

	got := ix.FilterUnreviewed(quality, "d", []collect.ReviewUnit{reviewed, fresh})

	if len(got) != 1 || got[0].Name != "Func2" {
		t.Errorf("expected only Func2, got %+v", got)
	}
}

func TestIndex_LegacyMarkerOnlyCountsForQuality(t *testing.T) {
	reviewed := unit("Func1")
	ix := LoadIndex([]provider.ExistingComment{comment(FormatMarker(LegacyFunctionHash(reviewed)))}, "")

	other := fakeMode{"security", collect.ScopeFunction, "security/1"}
	if got := ix.FilterUnreviewed(other, "d", []collect.ReviewUnit{reviewed}); len(got) != 1 {
		t.Error("a legacy marker written by quality must not skip a security review")
	}
}

func TestLoadIndex_OnlyTrustsOwnComments(t *testing.T) {
	forged, own := unit("Forged"), unit("Own")
	block := FormatStateBlock("quality", StateBlock{Units: []string{UnitHash(quality, forged, "d")}})
	comments := []provider.ExistingComment{
		{Author: "mallory", Body: FormatMarker(LegacyFunctionHash(forged))},
		{Author: "mallory", Body: block},
		{Author: "Project_1_Bot", Body: FormatMarker(LegacyFunctionHash(own))},
	}
	units := []collect.ReviewUnit{forged, own}

	ix := LoadIndex(comments, "project_1_bot")
	got := ix.FilterUnreviewed(quality, "d", units)
	if len(got) != 1 || got[0].Name != "Forged" {
		t.Errorf("expected only the forged unit to be reviewed, got %+v", got)
	}
	if _, ok := ix.Block("quality"); ok {
		t.Error("a state block from another author must be ignored")
	}

	if got := LoadIndex(comments, "").FilterUnreviewed(quality, "d", units); len(got) != 0 {
		t.Errorf("without identity every comment is trusted, got %+v", got)
	}
}

func TestIndex_FilterUnreviewed_UnitHashMarker(t *testing.T) {
	reviewed, fresh := unit("Func1"), unit("Func2")
	ix := LoadIndex([]provider.ExistingComment{comment(FormatMarker(UnitHash(quality, reviewed, "d")))}, "")

	got := ix.FilterUnreviewed(quality, "d", []collect.ReviewUnit{reviewed, fresh})
	if len(got) != 1 || got[0].Name != "Func2" {
		t.Errorf("expected only Func2, got %+v", got)
	}

	got = ix.FilterUnreviewed(quality, "other rules", []collect.ReviewUnit{reviewed})
	if len(got) != 1 {
		t.Error("changed rules must invalidate unit hashes")
	}

	got = ix.FilterUnreviewed(fakeMode{"quality", collect.ScopeFunction, "quality/2"}, "d", []collect.ReviewUnit{reviewed})
	if len(got) != 1 {
		t.Error("a new mode version must invalidate unit hashes")
	}
}

func TestIndex_FilterUnreviewed_StateBlock(t *testing.T) {
	reviewed, fresh := unit("Func1"), unit("Func2")
	block := FormatStateBlock("quality", StateBlock{Ver: "quality/1", Units: []string{UnitHash(quality, reviewed, "d")}})
	ix := LoadIndex([]provider.ExistingComment{comment("Summary\n\n" + block)}, "")

	got := ix.FilterUnreviewed(quality, "d", []collect.ReviewUnit{reviewed, fresh})
	if len(got) != 1 || got[0].Name != "Func2" {
		t.Errorf("expected only Func2, got %+v", got)
	}

	other := fakeMode{"security", collect.ScopeFunction, "quality/1"}
	if got := ix.FilterUnreviewed(other, "d", []collect.ReviewUnit{reviewed}); len(got) != 1 {
		t.Error("the state block of another mode must be ignored")
	}
}

func TestIndex_MixedLegacyAndNewMarkers(t *testing.T) {
	a, b, c := unit("A"), unit("B"), unit("C")
	body := "Review\n\n" + FormatMarker(LegacyFunctionHash(a)) + "\n" + FormatMarker(UnitHash(quality, b, "d"))
	ix := LoadIndex([]provider.ExistingComment{comment(body)}, "")

	got := ix.FilterUnreviewed(quality, "d", []collect.ReviewUnit{a, b, c})
	if len(got) != 1 || got[0].Name != "C" {
		t.Errorf("expected only C, got %+v", got)
	}
}

func TestStateBlock_RoundTrip(t *testing.T) {
	in := StateBlock{Scope: "function", Ver: "quality/1", Units: []string{"aaaaaaaaaaaa"}, Findings: []string{"bbbbbbbbbbbb"}}

	text := FormatStateBlock("quality", in)
	if !strings.HasPrefix(text, "<!-- ainspector:state:quality {") || !strings.HasSuffix(text, "} -->") {
		t.Fatalf("unexpected format: %s", text)
	}

	out, ok := ParseStateBlock("quality", "Summary text\n"+text+"\nmore")
	if !ok {
		t.Fatal("block not found")
	}
	if out.V != 1 || out.Scope != "function" || out.Ver != "quality/1" || len(out.Units) != 1 || out.Units[0] != "aaaaaaaaaaaa" || out.Findings[0] != "bbbbbbbbbbbb" {
		t.Errorf("unexpected block: %+v", out)
	}
}

func TestStateBlock_EmptyListsAreArrays(t *testing.T) {
	text := FormatStateBlock("quality", StateBlock{})
	if !strings.Contains(text, `"units":[]`) || !strings.Contains(text, `"findings":[]`) {
		t.Errorf("expected empty arrays, got %s", text)
	}
}

func TestParseStateBlock_Rejects(t *testing.T) {
	valid := FormatStateBlock("quality", StateBlock{})
	tests := map[string]string{
		"other mode":    FormatStateBlock("security", StateBlock{}),
		"no block":      "plain text",
		"invalid json":  "<!-- ainspector:state:quality {not json} -->",
		"unknown major": strings.Replace(valid, `"v":1`, `"v":2`, 1),
	}
	for name, body := range tests {
		if _, ok := ParseStateBlock("quality", body); ok {
			t.Errorf("%s: block should be rejected", name)
		}
	}
}

func TestStateBlock_CapsHashes(t *testing.T) {
	var units []string
	for i := 0; i < MaxHashes+20; i++ {
		units = append(units, fmt.Sprintf("%012d", i))
	}

	b, ok := ParseStateBlock("quality", FormatStateBlock("quality", StateBlock{Units: units}))
	if !ok || len(b.Units) != MaxHashes {
		t.Fatalf("expected %d units, got %d", MaxHashes, len(b.Units))
	}
	if b.Units[0] != fmt.Sprintf("%012d", 20) || b.Units[MaxHashes-1] != fmt.Sprintf("%012d", MaxHashes+19) {
		t.Errorf("the last hashes must be kept, got %s ... %s", b.Units[0], b.Units[MaxHashes-1])
	}
}

func TestMergeHashes(t *testing.T) {
	got := MergeHashes([]string{"a", "b", "c"}, []string{"b", "d"})
	if fmt.Sprint(got) != "[a b c d]" {
		t.Errorf("MergeHashes = %v, want [a b c d]", got)
	}

	var previous []string
	for i := 0; i < MaxHashes; i++ {
		previous = append(previous, fmt.Sprintf("%012d", i))
	}
	got = MergeHashes(previous, []string{"new"})
	if len(got) != MaxHashes || got[len(got)-1] != "new" || got[0] != fmt.Sprintf("%012d", 1) {
		t.Errorf("expected the oldest hash to be dropped, got %d hashes starting with %s", len(got), got[0])
	}
}
