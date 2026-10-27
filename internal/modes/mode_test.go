package modes

import (
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/config"
)

func TestEnabled(t *testing.T) {
	cfg := &config.Config{
		Rules: []string{"No globals"},
		Modes: config.ModesConfig{Quality: config.QualityConfig{Model: "some-model", Rules: []string{"Small functions"}}},
	}

	got, err := Enabled([]string{"quality", "quality"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "quality" {
		t.Fatalf("expected a single quality mode, got %v", got)
	}
	if got[0].Model() != "some-model" {
		t.Errorf("expected the configured model, got %q", got[0].Model())
	}
	if r := got[0].Rules(); len(r) != 2 || r[0] != "No globals" || r[1] != "Small functions" {
		t.Errorf("expected top-level then mode rules, got %v", r)
	}
}

func TestEnabled_UnknownMode(t *testing.T) {
	if _, err := Enabled([]string{"quality", "nope"}, &config.Config{}); err == nil {
		t.Error("expected an error for an unknown mode")
	}
}

func TestEnabled_NoNames(t *testing.T) {
	got, err := Enabled(nil, &config.Config{})
	if err != nil || len(got) != 0 {
		t.Errorf("expected no modes and no error, got %v / %v", got, err)
	}
}

func TestEnabled_Security(t *testing.T) {
	cfg := &config.Config{Modes: config.ModesConfig{Security: config.SecurityConfig{
		Model:         "some-model",
		MaxFindings:   2,
		FalsePositive: config.FalsePositiveConfig{Model: "big-model", Instructions: "Ignore legacy/"},
	}}}

	got, err := Enabled([]string{"security", "quality"}, cfg)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	m := got[0]
	if m.Name() != "security" || m.Model() != "some-model" || m.Scope() != collect.ScopeFunctionClass {
		t.Errorf("unexpected mode %s %s %v", m.Name(), m.Model(), m.Scope())
	}
	p := m.FilterPolicy()
	if p.MaxPublished != 2 || p.FalsePositive.Model != "big-model" || p.FalsePositive.Instructions != "Ignore legacy/" {
		t.Errorf("unexpected policy %+v", p)
	}
	if h := m.Needs().Hunks; h.Before != 5 || h.After != 1 || len(h.Extensions) != 5 {
		t.Errorf("unexpected hunk defaults %+v", h)
	}
	if !got[1].Accepts(collect.ReviewUnit{Kind: collect.KindFunction}) || got[1].Accepts(collect.ReviewUnit{Kind: collect.KindHunk}) {
		t.Error("quality only reviews functions")
	}
}

func TestEnabled_SecurityScope(t *testing.T) {
	for scope, want := range map[string]collect.Scope{
		"":                       collect.ScopeFunctionClass,
		"function":               collect.ScopeFunction,
		"function+class":         collect.ScopeFunctionClass,
		"function+class+related": collect.ScopeFunctionClass,
	} {
		cfg := &config.Config{Modes: config.ModesConfig{Security: config.SecurityConfig{Scope: scope}}}
		got, err := Enabled([]string{"security"}, cfg)
		if err != nil || got[0].Scope() != want {
			t.Errorf("scope %q: got %v, %v, want %v", scope, got, err, want)
		}
	}
}
