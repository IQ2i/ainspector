package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectContextFiles_IncludeExclude(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	createTestFile(t, tmpDir, "README.md", "readme content")
	createTestFile(t, tmpDir, "docs/guide.md", "guide content")
	createTestFile(t, tmpDir, "docs/internal.md", "internal content")

	tests := []struct {
		name         string
		include      []string
		exclude      []string
		wantFiles    []string
		wantNoFiles  []string
		wantWarnings int
	}{
		{
			name:        "include all, exclude one",
			include:     []string{"docs/**"},
			exclude:     []string{"docs/internal.md"},
			wantFiles:   []string{"docs/guide.md"},
			wantNoFiles: []string{"docs/internal.md"},
		},
		{
			name:      "exclude takes priority",
			include:   []string{"docs/**"},
			exclude:   []string{"**/*.md"},
			wantFiles: []string{},
		},
		{
			name:      "no exclusions",
			include:   []string{"README.md"},
			exclude:   []string{},
			wantFiles: []string{"README.md"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ContextConfig{
				Include: tt.include,
				Exclude: tt.exclude,
			}

			list, warnings, err := cfg.CollectContextFiles(tmpDir)
			files := byPath(list)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(warnings) != tt.wantWarnings {
				t.Errorf("got %d warnings, want %d", len(warnings), tt.wantWarnings)
			}

			for _, wantFile := range tt.wantFiles {
				if _, ok := files[wantFile]; !ok {
					t.Errorf("expected file %s not found in result", wantFile)
				}
			}

			for _, noFile := range tt.wantNoFiles {
				if _, ok := files[noFile]; ok {
					t.Errorf("file %s should be excluded but was found", noFile)
				}
			}
		})
	}
}

func TestCollectContextFiles_GlobPatterns(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test structure
	createTestFile(t, tmpDir, "README.md", "readme")
	createTestFile(t, tmpDir, "docs/guide.md", "guide")
	createTestFile(t, tmpDir, "docs/api/endpoints.md", "endpoints")
	createTestFile(t, tmpDir, "src/main.go", "main")

	tests := []struct {
		name      string
		pattern   string
		wantFiles []string
	}{
		{
			name:      "single star",
			pattern:   "*.md",
			wantFiles: []string{"README.md"},
		},
		{
			name:      "double star recursive",
			pattern:   "docs/**/*.md",
			wantFiles: []string{"docs/guide.md", "docs/api/endpoints.md"},
		},
		{
			name:      "double star all",
			pattern:   "**/*.go",
			wantFiles: []string{"src/main.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ContextConfig{
				Include: []string{tt.pattern},
			}

			list, _, err := cfg.CollectContextFiles(tmpDir)
			files := byPath(list)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(files) != len(tt.wantFiles) {
				t.Errorf("got %d files, want %d", len(files), len(tt.wantFiles))
			}

			for _, wantFile := range tt.wantFiles {
				if _, ok := files[wantFile]; !ok {
					t.Errorf("expected file %s not found", wantFile)
				}
			}
		})
	}
}

func TestCollectContextFiles_Directories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create directory structure
	createTestFile(t, tmpDir, "docs/a.md", "a")
	createTestFile(t, tmpDir, "docs/b.md", "b")
	createTestFile(t, tmpDir, "docs/sub/c.md", "c")

	cfg := &ContextConfig{
		Include: []string{"docs"},
	}

	list, _, err := cfg.CollectContextFiles(tmpDir)
	files := byPath(list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include all files in directory recursively
	expectedFiles := []string{"docs/a.md", "docs/b.md", "docs/sub/c.md"}
	if len(files) != len(expectedFiles) {
		t.Errorf("got %d files, want %d", len(files), len(expectedFiles))
	}

	for _, wantFile := range expectedFiles {
		if _, ok := files[wantFile]; !ok {
			t.Errorf("expected file %s not found", wantFile)
		}
	}
}

func TestCollectContextFiles_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &ContextConfig{
		Include: []string{"nonexistent.md", "missing/dir"},
	}

	list, warnings, err := cfg.CollectContextFiles(tmpDir)
	files := byPath(list)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("expected no files, got %d", len(files))
	}

	if len(warnings) != 2 {
		t.Errorf("expected 2 warnings for missing files, got %d", len(warnings))
	}
}

func TestCollectContextFiles_DeterministicOrder(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "z.md", "z")
	createTestFile(t, tmpDir, "a.md", "a")
	createTestFile(t, tmpDir, "docs/y.md", "y")
	createTestFile(t, tmpDir, "docs/b.md", "b")

	cfg := &ContextConfig{Include: []string{"docs", "z.md", "a.md"}}
	files, _, err := cfg.CollectContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The files named one by one come first, whatever the order of the patterns, then the
	// ones found in directories, each group in alphabetical order
	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	want := []string{"a.md", "z.md", "docs/b.md", "docs/y.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestShouldExclude(t *testing.T) {
	tests := []struct {
		name     string
		exclude  []string
		path     string
		expected bool
	}{
		{
			name:     "exact match",
			exclude:  []string{"docs/internal.md"},
			path:     "docs/internal.md",
			expected: true,
		},
		{
			name:     "glob match",
			exclude:  []string{"**/*_test.go"},
			path:     "internal/config/context_test.go",
			expected: true,
		},
		{
			name:     "no match",
			exclude:  []string{"*.test.go"},
			path:     "internal/config/context.go",
			expected: false,
		},
		{
			name:     "directory pattern",
			exclude:  []string{"vendor/**"},
			path:     "vendor/github.com/pkg/file.go",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &ContextConfig{
				Exclude: tt.exclude,
			}

			result := cfg.shouldExclude(tt.path)
			if result != tt.expected {
				t.Errorf("shouldExclude(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestCollectContextFiles_EmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "README.md", "readme")

	cfg := &ContextConfig{
		Include: []string{},
	}

	files, warnings, err := cfg.CollectContextFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if files != nil {
		t.Errorf("expected nil files for empty config, got %v", files)
	}

	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got %d", len(warnings))
	}
}

// Helper function to create test files
func createTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	dir := filepath.Dir(fullPath)

	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", fullPath, err)
	}
}

func TestCollectContextFiles_SymlinkOutsideTheProjectIsRefused(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("TOKEN=secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.md"), []byte("fine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "leak.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	cfg := &ContextConfig{Include: []string{"*.md", "../" + filepath.Base(outside) + "/secret"}}
	list, warnings, err := cfg.CollectContextFiles(dir)
	files := byPath(list)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 || files["ok.md"] != "fine" {
		t.Errorf("only the file inside the project may be read, got %v", files)
	}
	if len(warnings) == 0 {
		t.Error("expected a warning for the refused link")
	}
}

func TestReadProjectFile(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := ReadProjectFile(dir, "a.txt", 5)
	if err != nil || string(got) != "hello" {
		t.Errorf("expected the first 5 bytes, got %q / %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(base, "sibling.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadProjectFile(dir, "../sibling.txt", 5); !errors.Is(err, ErrOutsideRoot) {
		t.Errorf("a .. segment must be refused, got %v", err)
	}
	if _, err := ReadProjectFile(dir, "sub", 5); err == nil {
		t.Error("a directory is not a regular file")
	}
	if _, err := ReadProjectFile(dir, "missing", 5); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected a not-exist error, got %v", err)
	}
}

func TestCheckContextPattern(t *testing.T) {
	for _, p := range []string{"docs/*.md", "README.md", "src/**", "a/b/c.txt"} {
		if err := CheckContextPattern(p); err != nil {
			t.Errorf("%q must be accepted: %v", p, err)
		}
	}
	for _, p := range []string{"", "../secrets", "docs/../../x", "/etc/passwd", `\etc`, `..\x`, "a/../../b", "**/../x"} {
		if err := CheckContextPattern(p); err == nil {
			t.Errorf("%q must be refused", p)
		}
	}
}

func TestParse_RefusesContextPatternsLeavingTheProject(t *testing.T) {
	for name, yaml := range map[string]string{
		"parent include":   "context:\n  include: ['../outside/**']\n",
		"absolute include": "context:\n  include: ['/etc/passwd']\n",
		"parent exclude":   "context:\n  exclude: ['../x']\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse("ainspector.yaml", []byte(yaml)); err == nil {
				t.Errorf("expected an error for %q", yaml)
			}
		})
	}
}

func TestCollectContextFiles_SkipsSensitiveFiles(t *testing.T) {
	root := t.TempDir()
	createTestFile(t, root, "docs/guide.md", "guide")
	createTestFile(t, root, ".env", "SECRET=1")
	createTestFile(t, root, ".env.local", "SECRET=1")
	createTestFile(t, root, "deploy/server.pem", "KEY")
	createTestFile(t, root, ".npmrc", "//registry:_authToken=x")

	cfg := &ContextConfig{Include: []string{".env", ".env.local", "deploy", ".npmrc", "docs", "*.md", ".*"}}
	list, warnings, err := cfg.CollectContextFiles(root)
	files := byPath(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["docs/guide.md"] == "" {
		t.Errorf("only the guide may be read, got %v", files)
	}
	if len(warnings) == 0 {
		t.Error("the skipped secrets must be reported")
	}
}

func TestCollectContextFiles_DoesNotFollowDirectoryLinks(t *testing.T) {
	root := t.TempDir()
	createTestFile(t, root, "docs/real/guide.md", "guide")
	if err := os.Symlink(filepath.Join(root, "docs", "real"), filepath.Join(root, "docs", "link")); err != nil {
		t.Skip("symlinks unavailable")
	}

	list, _, err := (&ContextConfig{Include: []string{"docs/**/*.md"}}).CollectContextFiles(root)
	files := byPath(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["docs/real/guide.md"] == "" {
		t.Errorf("a linked directory must not be walked a second time, got %v", files)
	}
}

func TestCollectContextFiles_RefusesLinksToSecrets(t *testing.T) {
	root := t.TempDir()
	createTestFile(t, root, ".env", "SECRET=1")
	createTestFile(t, root, ".git/config", "url = https://token@host/x.git")
	createTestFile(t, root, "docs/guide.md", "guide")
	for name, target := range map[string]string{"notes.md": ".env", "docs/git.md": ".git/config"} {
		if err := os.Symlink(filepath.Join(root, target), filepath.Join(root, name)); err != nil {
			t.Skip("symlinks unavailable")
		}
	}

	list, warnings, err := (&ContextConfig{Include: []string{"notes.md", "docs"}}).CollectContextFiles(root)
	files := byPath(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files["docs/guide.md"] == "" || len(warnings) != 2 {
		t.Errorf("a link to a secret must not be read, got %v, warnings %v", files, warnings)
	}
	if _, err := ReadProjectFile(root, "notes.md", 100); !errors.Is(err, ErrSensitive) {
		t.Errorf("ReadProjectFile must refuse a link to a secret, got %v", err)
	}
}

func TestCollectFromPattern_StopsWalkingPastTheCap(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxContextFiles+50; i++ {
		createTestFile(t, root, filepath.Join("docs", fmt.Sprintf("f%03d.md", i)), "x")
	}
	for _, pattern := range []string{"docs", "docs/*.md"} {
		files, err := collectFromPattern(root, pattern)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != maxContextFiles+1 {
			t.Errorf("%s: the walk must stop one file past the cap, got %d", pattern, len(files))
		}
	}
}

func TestCollectContextFiles_CapsFilesAndBytes(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < maxContextFiles+30; i++ {
		createTestFile(t, root, filepath.Join("docs", fmt.Sprintf("f%03d.md", i)), "x")
	}
	list, warnings, err := (&ContextConfig{Include: []string{"docs"}}).CollectContextFiles(root)
	files := byPath(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != maxContextFiles || len(warnings) == 0 {
		t.Errorf("expected %d files and a warning, got %d files, warnings %v", maxContextFiles, len(files), warnings)
	}

	big := t.TempDir()
	for i := 0; i < 5; i++ {
		createTestFile(t, big, fmt.Sprintf("docs/f%d.md", i), strings.Repeat("y", 400))
	}
	list, _, err = (&ContextConfig{Include: []string{"docs"}, MaxBytes: 1000}).CollectContextFiles(big)
	files = byPath(list)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Errorf("reading must stop once max_bytes is reached, got %d files", len(files))
	}
}

func byPath(files []ContextFile) map[string]string {
	m := make(map[string]string, len(files))
	for _, f := range files {
		m[f.Path] = f.Content
	}
	return m
}
