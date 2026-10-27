package modes

import (
	"testing"

	"github.com/iq2i/ainspector/internal/config"
)

func TestEnabled(t *testing.T) {
	cfg := &config.Config{Rules: []string{"No globals"}}

	got, err := Enabled([]string{"quality", "quality"}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name() != "quality" {
		t.Fatalf("expected a single quality mode, got %v", got)
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
