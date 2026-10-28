package modes

import (
	"strings"
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
	if got[0].BuildRequest(collect.ReviewUnit{}, nil).Model != "some-model" {
		t.Errorf("expected the configured model, got %q", got[0].BuildRequest(collect.ReviewUnit{}, nil).Model)
	}
	if r := got[0].Rules(); len(r) != 2 || r[0] != "No globals" || r[1] != "Small functions" {
		t.Errorf("expected top-level then mode rules, got %v", r)
	}
}

func TestEnabled_QualityLeavesVulnerabilitiesToSecurity(t *testing.T) {
	system := func(names ...string) string {
		got, err := Enabled(names, &config.Config{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, m := range got {
			if m.Name() == "quality" {
				return m.BuildRequest(collect.ReviewUnit{}, nil).System[0].Text
			}
		}
		t.Fatalf("no quality mode in %v", got)
		return ""
	}

	if strings.Contains(system("quality"), "dedicated security review") {
		t.Error("alone, quality reviews vulnerabilities too")
	}
	if !strings.Contains(system("quality", "security"), "dedicated security review") {
		t.Error("with the security mode, quality leaves vulnerabilities to it")
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
	if m.Name() != "security" || m.BuildRequest(collect.ReviewUnit{}, nil).Model != "some-model" || m.Scope() != collect.ScopeFunctionClassRelated {
		t.Errorf("unexpected mode %s %s %v", m.Name(), m.BuildRequest(collect.ReviewUnit{}, nil).Model, m.Scope())
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
		"":                       collect.ScopeFunctionClassRelated,
		"function":               collect.ScopeFunction,
		"function+class":         collect.ScopeFunctionClass,
		"function+class+related": collect.ScopeFunctionClassRelated,
	} {
		cfg := &config.Config{Modes: config.ModesConfig{Security: config.SecurityConfig{Scope: scope}}}
		got, err := Enabled([]string{"security"}, cfg)
		if err != nil || got[0].Scope() != want {
			t.Errorf("scope %q: got %v, %v, want %v", scope, got, err, want)
		}
	}
}

func TestEnabled_SecurityRelatedNeeds(t *testing.T) {
	cfg := &config.Config{Modes: config.ModesConfig{Security: config.SecurityConfig{
		Related: config.RelatedConfig{Presets: []string{"vue"}, MaxFiles: 2},
	}}}
	got, err := Enabled([]string{"security"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := got[0].Needs().Related
	if len(r.Presets) != 1 || r.Presets[0] != "vue" || r.MaxFiles != 2 || r.MaxBytes != config.DefaultRelatedMaxBytes {
		t.Errorf("unexpected policy %+v", r)
	}

	cfg.Modes.Security.Scope = "function+class"
	got, _ = Enabled([]string{"security"}, cfg)
	if r := got[0].Needs().Related; len(r.Presets) != 0 {
		t.Errorf("a scope without related files must not ask for them, got %+v", r)
	}
}

func TestEnabled_SecurityDefaults(t *testing.T) {
	got, err := Enabled([]string{"security"}, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	r := got[0].Needs().Related
	if len(r.Presets) != 2 || r.MaxFiles != 6 || r.MaxBytes != 40000 {
		t.Errorf("unexpected defaults %+v", r)
	}
}

func TestEnabled_SecurityUnknownPreset(t *testing.T) {
	cfg := &config.Config{Modes: config.ModesConfig{Security: config.SecurityConfig{
		Related: config.RelatedConfig{Presets: []string{"symfony", "laravel"}},
	}}}
	_, err := Enabled([]string{"security"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "laravel") {
		t.Errorf("an unknown preset must be refused and named, got %v", err)
	}
	if _, err := Enabled([]string{"quality"}, cfg); err != nil {
		t.Errorf("the quality mode does not use presets, got %v", err)
	}
}
