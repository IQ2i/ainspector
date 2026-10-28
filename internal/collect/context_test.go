package collect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/config"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func paths(files []contextFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.path
	}
	return out
}

func TestFindContextFiles_OrderAndSelection(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"package.json":                    `{"name": "test"}`,
		"README.md":                       "# Test",
		"go.mod":                          "module test",
		"go.sum":                          "checksums",
		"composer.json":                   `{"name": "test"}`,
		"CLAUDE.md":                       "Claude instructions",
		".github/copilot-instructions.md": "Copilot",
		"unrelated.txt":                   "ignored",
	})

	found, _, err := findContextFiles(dir, config.DefaultContextMaxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"CLAUDE.md", ".github/copilot-instructions.md", "README.md", "composer.json", "go.mod", "package.json"}
	if got := paths(found); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestFindContextFiles_GlobPattern(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"MyProject.csproj": `<Project Sdk="Microsoft.NET.Sdk"></Project>`})

	found, _, err := findContextFiles(dir, config.DefaultContextMaxBytes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := paths(found); len(got) != 1 || got[0] != "MyProject.csproj" {
		t.Errorf("expected the csproj file, got %v", got)
	}
}

func TestGenerateProjectContext_WellKnownFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"README.md": "# Test", "CLAUDE.md": "Keep it small", "go.mod": "module test"})

	pc, err := GenerateProjectContext(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "=== CLAUDE.md ===\nKeep it small\n\n=== README.md ===\n# Test\n\n=== go.mod ===\nmodule test\n\n"
	if pc.Description != want {
		t.Errorf("unexpected description:\n%q", pc.Description)
	}
	if len(pc.Notes) != 0 {
		t.Errorf("unexpected notes: %+v", pc)
	}
}

func TestGenerateProjectContext_NoFiles(t *testing.T) {
	pc, err := GenerateProjectContext(t.TempDir(), &config.ContextConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pc.Description != "" || len(pc.Notes) != 0 {
		t.Errorf("expected an empty context, got %+v", pc)
	}
}

func TestGenerateProjectContext_WithConfig(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"README.md": "# Test Project", "docs/guide.md": "# Guide", "CLAUDE.md": "not listed"})

	pc, err := GenerateProjectContext(dir, &config.ContextConfig{Include: []string{"README.md", "docs/guide.md"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"=== README.md ===\n# Test Project", "=== docs/guide.md ===\n# Guide"} {
		if !strings.Contains(pc.Description, want) {
			t.Errorf("expected description to contain %q, got %q", want, pc.Description)
		}
	}
	if strings.Contains(pc.Description, "not listed") {
		t.Error("files outside context.include must not be read")
	}
}

func TestGenerateProjectContext_MaxBytesCutsAndReports(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"CLAUDE.md":    "rules",
		"README.md":    strings.Repeat("r", 5000),
		"go.mod":       "module test",
		"package.json": "{}",
	})

	pc, err := GenerateProjectContext(dir, &config.ContextConfig{MaxBytes: 1000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pc.Description) > 1000 {
		t.Errorf("context exceeds the limit: %d bytes", len(pc.Description))
	}
	if !strings.HasPrefix(pc.Description, "=== CLAUDE.md ===\nrules\n\n=== README.md ===\n") || !strings.HasSuffix(pc.Description, "... (truncated)\n") {
		t.Errorf("unexpected description: %q", pc.Description)
	}
	if len(pc.Notes) != 1 || !strings.Contains(pc.Notes[0], "README.md truncated") || !strings.Contains(pc.Notes[0], "go.mod, package.json") {
		t.Errorf("unexpected notes: %v", pc.Notes)
	}
}

func TestGenerateProjectContext_TooLittleRoomLeavesTheFileOut(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"CLAUDE.md": strings.Repeat("a", 90), "README.md": strings.Repeat("r", 500)})

	pc, err := GenerateProjectContext(dir, &config.ContextConfig{MaxBytes: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(pc.Description, "README.md") {
		t.Errorf("a file that cannot be useful when cut must be left out: %q", pc.Description)
	}
	if len(pc.Notes) != 1 || !strings.Contains(pc.Notes[0], "left out: README.md") {
		t.Errorf("unexpected notes: %v", pc.Notes)
	}
}

func TestGenerateProjectContext_DefaultLimit(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"README.md": strings.Repeat("r", config.DefaultContextMaxBytes*2)})

	pc, err := GenerateProjectContext(dir, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pc.Description) > config.DefaultContextMaxBytes || len(pc.Notes) != 1 {
		t.Errorf("expected the default limit to apply, got %d bytes and %v", len(pc.Description), pc.Notes)
	}
}

func TestFindContextFiles_SymlinkOutsideTheProjectIsIgnored(t *testing.T) {
	outside := t.TempDir()
	writeFiles(t, outside, map[string]string{"secret": "GITLAB_TOKEN=glpat-secret"})
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"CLAUDE.md": "rules", "real.md": "inside"})
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "README.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink("real.md", filepath.Join(dir, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	found, warnings, err := findContextFiles(dir, config.DefaultContextMaxBytes)
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(paths(found), ","); got != "CLAUDE.md,AGENTS.md" {
		t.Errorf("expected the link inside the project to be kept and the other ignored, got %s", got)
	}
	for _, f := range found {
		if strings.Contains(f.content, "glpat") {
			t.Errorf("content of a file outside the project leaked: %q", f.content)
		}
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "README.md") {
		t.Errorf("expected one warning naming the link, got %v", warnings)
	}
}

func TestFindContextFiles_ReadsAtMostTheLimit(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{"README.md": strings.Repeat("x", 5000)})

	found, _, err := findContextFiles(dir, 100)
	if err != nil || len(found) != 1 || len(found[0].content) != 100 {
		t.Errorf("expected 100 bytes read, got %v / %v", found, err)
	}
}

func TestGenerateProjectContext_NamedFilesComeBeforeDirectoriesWhenTheLimitCuts(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"docs/a.md": strings.Repeat("a", 400),
		"ZZZ.md":    "named file",
	})

	pc, err := GenerateProjectContext(dir, &config.ContextConfig{Include: []string{"docs", "ZZZ.md"}, MaxBytes: 300})
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Index(pc.Description, "=== ZZZ.md ===")
	if first != 0 || !strings.Contains(pc.Description, "named file") {
		t.Errorf("the file named in include must come first and survive the cut, got %q", pc.Description)
	}
}
