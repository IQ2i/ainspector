package presets

import (
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	if got := strings.Join(Names(), ","); got != "symfony,vue" {
		t.Errorf("Names = %q", got)
	}
}

func TestCheck(t *testing.T) {
	if err := Check([]string{"symfony", "vue"}); err != nil {
		t.Errorf("known presets must pass: %v", err)
	}
	err := Check([]string{"symfony", "laravel"})
	if err == nil || !strings.Contains(err.Error(), `"laravel"`) || !strings.Contains(err.Error(), "symfony, vue") {
		t.Errorf("an unknown preset must be named with the expected ones, got %v", err)
	}
}

func TestResolvers(t *testing.T) {
	all, err := Resolvers(Names())
	if err != nil || len(all) == 0 {
		t.Fatalf("resolvers %d, err %v", len(all), err)
	}
	one, err := Resolvers([]string{"vue"})
	if err != nil || len(one) == 0 || len(one) >= len(all) {
		t.Errorf("a single preset gives its own resolvers: %d of %d, %v", len(one), len(all), err)
	}
	if _, err := Resolvers([]string{"laravel"}); err == nil {
		t.Error("an unknown preset must be refused")
	}
	if none, err := Resolvers(nil); err != nil || len(none) != 0 {
		t.Errorf("no preset, no resolver: %d, %v", len(none), err)
	}
}
