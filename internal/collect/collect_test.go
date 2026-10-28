package collect

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/iq2i/ainspector/internal/provider"
)

// mockProvider is a test double for the Provider interface
type mockProvider struct {
	provider.Provider
	files       map[string]string // path -> content
	getFileErr  error
	getFilesErr error
}

func (m *mockProvider) GetModifiedFiles(ctx context.Context, number int) ([]provider.ModifiedFile, error) {
	if m.getFilesErr != nil {
		return nil, m.getFilesErr
	}
	return nil, nil
}

func (m *mockProvider) GetFileContent(ctx context.Context, path string) (string, error) {
	if m.getFileErr != nil {
		return "", m.getFileErr
	}
	content, ok := m.files[path]
	if !ok {
		return "", errors.New("file not found")
	}
	return content, nil
}

func TestCollect_SkipsDeletedFiles(t *testing.T) {
	mock := &mockProvider{files: map[string]string{}}
	e := New(mock, nil)
	defer e.Close()

	files := []provider.ModifiedFile{
		{Path: "deleted.go", Status: "deleted", Patch: "@@ -1,3 +0,0 @@\n-line1\n-line2\n-line3"},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 0 {
		t.Errorf("expected 0 functions for deleted file, got %d", len(result))
	}
}

func TestCollect_SkipsUnsupportedFiles(t *testing.T) {
	mock := &mockProvider{files: map[string]string{
		"data.json": `{"key": "value"}`,
	}}
	e := New(mock, nil)
	defer e.Close()

	files := []provider.ModifiedFile{
		{Path: "data.json", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new"},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 0 {
		t.Errorf("expected 0 functions for unsupported file, got %d", len(result))
	}
}

func TestCollect_SingleModifiedFunction(t *testing.T) {
	goCode := `package main

func hello() {
	println("hello world")
}
`
	mock := &mockProvider{files: map[string]string{
		"main.go": goCode,
	}}
	e := New(mock, nil)
	defer e.Close()

	// Patch that modifies line 4 (inside the function)
	patch := `@@ -1,5 +1,5 @@
 package main

 func hello() {
-	println("hello")
+	println("hello world")
 }`

	files := []provider.ModifiedFile{
		{Path: "main.go", Status: "modified", Patch: patch},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	if result[0].Name != "hello" {
		t.Errorf("expected function name 'hello', got %s", result[0].Name)
	}
	if result[0].FilePath != "main.go" {
		t.Errorf("expected file path 'main.go', got %s", result[0].FilePath)
	}
	if result[0].Language != "go" {
		t.Errorf("expected language 'go', got %s", result[0].Language)
	}
	if result[0].ChangeType != "modified" {
		t.Errorf("expected change type 'modified', got %s", result[0].ChangeType)
	}
}

func TestCollect_AddedFile(t *testing.T) {
	goCode := `package main

func newFunc() {
	println("new")
}
`
	mock := &mockProvider{files: map[string]string{
		"new.go": goCode,
	}}
	e := New(mock, nil)
	defer e.Close()

	// Patch for a new file
	patch := `@@ -0,0 +1,5 @@
+package main
+
+func newFunc() {
+	println("new")
+}`

	files := []provider.ModifiedFile{
		{Path: "new.go", Status: "added", Patch: patch},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	if result[0].ChangeType != "added" {
		t.Errorf("expected change type 'added', got %s", result[0].ChangeType)
	}
}

func TestCollect_MultipleFunctions(t *testing.T) {
	// Note: The code must match the NEW state after the patch is applied
	goCode := `package main

func first() {
	println("first modified")
}

func second() {
	println("second")
}

func third() {
	println("third modified")
}
`
	mock := &mockProvider{files: map[string]string{
		"funcs.go": goCode,
	}}
	e := New(mock, nil)
	defer e.Close()

	// Patch that modifies line 4 (inside first function) and line 12 (inside third function)
	// first function: lines 3-5
	// second function: lines 7-9
	// third function: lines 11-13
	patch := `@@ -3,3 +3,3 @@
 func first() {
-	println("first")
+	println("first modified")
 }
@@ -11,3 +11,3 @@
 func third() {
-	println("third")
+	println("third modified")
 }`

	files := []provider.ModifiedFile{
		{Path: "funcs.go", Status: "modified", Patch: patch},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(result))
	}

	names := make(map[string]bool)
	for _, fn := range result {
		names[fn.Name] = true
	}

	if !names["first"] {
		t.Error("expected 'first' function to be extracted")
	}
	if !names["third"] {
		t.Error("expected 'third' function to be extracted")
	}
	if names["second"] {
		t.Error("'second' function should not be extracted (not modified)")
	}
}

func TestCollect_MultipleFiles(t *testing.T) {
	mock := &mockProvider{files: map[string]string{
		"a.go": `package main

func funcA() {
	println("a")
}
`,
		"b.go": `package main

func funcB() {
	println("b")
}
`,
	}}
	e := New(mock, nil)
	defer e.Close()

	files := []provider.ModifiedFile{
		{
			Path:   "a.go",
			Status: "modified",
			Patch: `@@ -1,5 +1,5 @@
 package main

 func funcA() {
-	println("old a")
+	println("a")
 }`,
		},
		{
			Path:   "b.go",
			Status: "modified",
			Patch: `@@ -1,5 +1,5 @@
 package main

 func funcB() {
-	println("old b")
+	println("b")
 }`,
		},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 2 {
		t.Fatalf("expected 2 functions, got %d", len(result))
	}

	paths := make(map[string]bool)
	for _, fn := range result {
		paths[fn.FilePath] = true
	}

	if !paths["a.go"] {
		t.Error("expected function from 'a.go'")
	}
	if !paths["b.go"] {
		t.Error("expected function from 'b.go'")
	}
}

func TestCollect_ContinuesOnFileError(t *testing.T) {
	mock := &mockProvider{files: map[string]string{
		"good.go": `package main

func goodFunc() {
	println("good")
}
`,
	}}
	// bad.go is not in the files map, so GetFileContent will fail
	e := New(mock, nil)
	defer e.Close()

	files := []provider.ModifiedFile{
		{
			Path:   "bad.go",
			Status: "modified",
			Patch:  "@@ -1 +1 @@\n-old\n+new",
		},
		{
			Path:   "good.go",
			Status: "modified",
			Patch: `@@ -1,5 +1,5 @@
 package main

 func goodFunc() {
-	println("old")
+	println("good")
 }`,
		},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	// Should still extract from the good file
	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	if result[0].Name != "goodFunc" {
		t.Errorf("expected function 'goodFunc', got %s", result[0].Name)
	}
}

func TestCollect_NoModifiedFunctions(t *testing.T) {
	goCode := `package main

import "fmt"

func unchanged() {
	fmt.Println("unchanged")
}
`
	mock := &mockProvider{files: map[string]string{
		"main.go": goCode,
	}}
	e := New(mock, nil)
	defer e.Close()

	// Patch that only modifies the import, not the function
	patch := `@@ -1,3 +1,4 @@
 package main

+import "fmt"
+`

	files := []provider.ModifiedFile{
		{Path: "main.go", Status: "modified", Patch: patch},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 0 {
		t.Errorf("expected 0 functions (none modified), got %d", len(result))
	}
}

func TestCollect_ExtractsDiff(t *testing.T) {
	goCode := `package main

func hello() {
	println("hello world")
}
`
	mock := &mockProvider{files: map[string]string{
		"main.go": goCode,
	}}
	e := New(mock, nil)
	defer e.Close()

	patch := `@@ -1,5 +1,5 @@
 package main

 func hello() {
-	println("hello")
+	println("hello world")
 }`

	files := []provider.ModifiedFile{
		{Path: "main.go", Status: "modified", Patch: patch},
	}

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: files})

	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	// Should have extracted the diff for the function
	if result[0].Diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestCollect_EmptyFileList(t *testing.T) {
	mock := &mockProvider{files: map[string]string{}}
	e := New(mock, nil)
	defer e.Close()

	ctx := context.Background()
	result := e.Collect(ctx, Request{Files: []provider.ModifiedFile{}})

	if len(result) != 0 {
		t.Errorf("expected 0 functions for empty file list, got %d", len(result))
	}
}

func TestNew(t *testing.T) {
	mock := &mockProvider{}
	e := New(mock, nil)
	if e == nil {
		t.Fatal("expected non-nil collector")
		return
	}
	if e.parser == nil {
		t.Error("expected parser to be initialized")
	}
	if e.provider == nil {
		t.Error("expected provider to be set")
	}
	e.Close()
}

func TestExtractor_Close(t *testing.T) {
	mock := &mockProvider{}
	e := New(mock, nil)

	// Should not panic
	e.Close()

	// Should handle nil parser gracefully
	e.parser = nil
	e.Close()
}

const symfonyController = `<?php

namespace App\Controller;

use Symfony\Component\Security\Http\Attribute\IsGranted;

#[IsGranted('ROLE_USER')]
class AccountController
{
    public function __construct(private AccountRepository $accounts)
    {
    }

    #[Route('/{id}')]
    public function show(int $id): Response
    {
        return new Response('x');
    }

    #[Route('/{id}/edit')]
    public function edit(int $id): Response
    {
        return new Response('y');
    }
}
`

func collectPHP(t *testing.T, content, patch string, req Request) []ReviewUnit {
	t.Helper()
	mock := &mockProvider{files: map[string]string{"src/Controller/AccountController.php": content}}
	e := New(mock, nil)
	defer e.Close()

	req.Files = []provider.ModifiedFile{{Path: "src/Controller/AccountController.php", Status: "modified", Patch: patch}}
	units := e.Collect(context.Background(), req)
	return units
}

func TestCollect_ClassHeaderAtFunctionClassScope(t *testing.T) {
	patch := "@@ -15,3 +15,3 @@\n     public function show(int $id): Response\n     {\n-        return new Response('a');\n+        return new Response('x');\n     }\n@@ -22,3 +22,3 @@\n     public function edit(int $id): Response\n     {\n-        return new Response('b');\n+        return new Response('y');\n     }"

	units := collectPHP(t, symfonyController, patch, Request{Scope: ScopeFunctionClass})
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2", len(units))
	}
	for _, u := range units {
		if u.Enclosing == nil {
			t.Fatalf("%s has no class header", u.Name)
		}
		if u.Enclosing != units[0].Enclosing {
			t.Errorf("the header of a class must be rendered once per file")
		}
	}
	h := units[0].Enclosing
	if h.Name != "AccountController" || h.StartLine != 7 {
		t.Errorf("unexpected header %+v", h)
	}
	for _, want := range []string{"#[IsGranted('ROLE_USER')]", "use Symfony\\Component\\Security\\Http\\Attribute\\IsGranted;", "public function __construct(private AccountRepository $accounts)"} {
		if !strings.Contains(h.Rendered, want) {
			t.Errorf("header misses %q:\n%s", want, h.Rendered)
		}
	}
}

func TestCollect_NoClassHeaderAtFunctionScope(t *testing.T) {
	patch := "@@ -17,1 +17,1 @@\n-        return new Response('a');\n+        return new Response('x');"
	units := collectPHP(t, symfonyController, patch, Request{Scope: ScopeFunction})
	if len(units) != 1 || units[0].Enclosing != nil || units[0].Kind != KindFunction || units[0].DeletedOnly {
		t.Errorf("unexpected units %+v", units)
	}
}

func TestCollect_RelatedScopeCollectsLikeClassScope(t *testing.T) {
	patch := "@@ -17,1 +17,1 @@\n-        return new Response('a');\n+        return new Response('x');"
	units := collectPHP(t, symfonyController, patch, Request{Scope: ScopeFunctionClassRelated})
	if len(units) != 1 || units[0].Enclosing == nil {
		t.Errorf("expected one unit with its class header, got %+v", units)
	}
	if units[0].Related != nil {
		t.Error("related files are attached by the pipeline, not by the collector")
	}
}

func TestCollect_DeletedLinesSelectTheFunction(t *testing.T) {
	// #[Route] removed above edit(): no added line anywhere
	content := strings.Replace(symfonyController, "    #[Route('/{id}/edit')]\n", "", 1)
	patch := "@@ -19,4 +19,3 @@\n     }\n \n-    #[Route('/{id}/edit')]\n     public function edit(int $id): Response\n"

	if units := collectPHP(t, content, patch, Request{Scope: ScopeFunctionClass}); len(units) != 0 {
		t.Fatalf("without MatchDeleted nothing is selected, got %+v", units)
	}

	units := collectPHP(t, content, patch, Request{Scope: ScopeFunctionClass, MatchDeleted: true})
	if len(units) != 1 || units[0].Name != "edit" || !units[0].DeletedOnly {
		t.Fatalf("expected edit selected for its deletion, got %+v", units)
	}
	if !strings.Contains(units[0].Diff, "-    #[Route('/{id}/edit')]") {
		t.Errorf("the diff must show the removed line:\n%s", units[0].Diff)
	}
}

func TestCollect_ChangeOutsideFunctionsBecomesAHunk(t *testing.T) {
	// the class-level attribute is removed
	content := strings.Replace(symfonyController, "#[IsGranted('ROLE_USER')]\n", "", 1)
	patch := "@@ -5,5 +5,4 @@\n use Symfony\\Component\\Security\\Http\\Attribute\\IsGranted;\n \n-#[IsGranted('ROLE_USER')]\n class AccountController\n {\n"

	req := Request{Scope: ScopeFunctionClass, MatchDeleted: true}
	if units := collectPHP(t, content, patch, req); len(units) != 0 {
		t.Fatalf("hunks are off by default, got %+v", units)
	}

	req.Hunks = HunkPolicy{Orphans: true, Before: 2, After: 1}
	units := collectPHP(t, content, patch, req)
	if len(units) != 1 {
		t.Fatalf("got %d units, want 1: %+v", len(units), units)
	}
	u := units[0]
	if u.Kind != KindHunk || u.Language != "php" || !strings.Contains(u.Diff, "-#[IsGranted('ROLE_USER')]") || !strings.Contains(u.Content, "class AccountController") {
		t.Errorf("unexpected hunk %+v", u)
	}
}

func TestCollect_ExtendedHunksForFilesWithoutFunctions(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n") + "\n"
	// lines 10 and 12 changed (one hunk once extended), line 25 elsewhere
	patch := "@@ -10,3 +10,3 @@\n-old 10\n+line 10\n line 11\n-old 12\n+line 12\n@@ -25,1 +25,1 @@\n-old 25\n+line 25"

	mock := &mockProvider{files: map[string]string{"templates/a.html.twig": content}}
	e := New(mock, nil)
	defer e.Close()

	req := Request{
		Scope:        ScopeFunctionClass,
		MatchDeleted: true,
		Hunks:        HunkPolicy{Extensions: []string{"twig"}, Before: 2, After: 1},
		Files:        []provider.ModifiedFile{{Path: "templates/a.html.twig", Status: "modified", Patch: patch}},
	}
	units := e.Collect(context.Background(), req)
	if len(units) != 2 {
		t.Fatalf("got %d units, want 2: %+v", len(units), units)
	}
	if u := units[0]; u.Kind != KindHunk || u.Language != "twig" || u.StartLine != 8 || u.EndLine != 13 || u.Name != "lines 8-13" {
		t.Errorf("first hunk %+v", u)
	}
	if u := units[1]; u.StartLine != 23 || u.EndLine != 26 {
		t.Errorf("second hunk %+v", u)
	}
	if !strings.HasPrefix(units[0].Content, "line 8\n") || !strings.HasSuffix(units[0].Content, "line 13") {
		t.Errorf("content %q", units[0].Content)
	}

	req.Hunks.Extensions = nil
	if units := e.Collect(context.Background(), req); len(units) != 0 {
		t.Errorf("a file whose extension is not listed is skipped, got %+v", units)
	}
}

func TestCollect_DeletionBeforeABlankLineBecomesAHunk(t *testing.T) {
	// the removed rule is now replaced, in the new file, by a blank line
	content := "security:\n    access_control:\n        - { path: ^/admin, roles: ROLE_ADMIN }\n\nother: x\n"
	patch := "@@ -1,6 +1,5 @@\n security:\n     access_control:\n         - { path: ^/admin, roles: ROLE_ADMIN }\n-        - { path: ^/api, roles: ROLE_USER }\n \n other: x"

	mock := &mockProvider{files: map[string]string{"config/packages/security.yaml": content}}
	e := New(mock, nil)
	defer e.Close()

	units := e.Collect(context.Background(), Request{
		MatchDeleted: true,
		Hunks:        HunkPolicy{Extensions: []string{"yaml"}, Before: 2, After: 1},
		Files:        []provider.ModifiedFile{{Path: "config/packages/security.yaml", Status: "modified", Patch: patch}},
	})
	if len(units) != 1 || !strings.Contains(units[0].Diff, "-        - { path: ^/api") {
		t.Fatalf("a removed rule must be reviewed, got %+v", units)
	}
}

func TestCollect_HunkUnitsAreTruncated(t *testing.T) {
	content := strings.Repeat(strings.Repeat("x", 99)+"\n", 400)
	mock := &mockProvider{files: map[string]string{"config/big.yaml": content}}
	e := New(mock, nil)
	defer e.Close()

	req := Request{
		Hunks: HunkPolicy{Extensions: []string{"yaml"}},
		Files: []provider.ModifiedFile{{Path: "config/big.yaml", Status: "added", Patch: "@@ -0,0 +1,400 @@\n+x"}},
	}
	// every line is reported as added so the whole file becomes one range
	var patch strings.Builder
	patch.WriteString("@@ -0,0 +1,400 @@\n")
	for i := 0; i < 400; i++ {
		patch.WriteString("+" + strings.Repeat("x", 99) + "\n")
	}
	req.Files[0].Patch = patch.String()

	units := e.Collect(context.Background(), req)
	if len(units) != 1 {
		t.Fatalf("units %d", len(units))
	}
	if len(units[0].Content) > maxHunkBytes || len(units[0].Notes) != 1 || units[0].ChangeType != "added" || units[0].Language != "yaml" {
		t.Errorf("unexpected unit: %d bytes, notes %v", len(units[0].Content), units[0].Notes)
	}
	if len(units[0].Diff) > maxHunkBytes {
		t.Errorf("the diff of a hunk is capped like its content, got %d bytes", len(units[0].Diff))
	}
}

func TestCollect_GeneratedFilesAreNotHunks(t *testing.T) {
	content := "a: 1\nb: 2\n"
	patch := "@@ -1,2 +1,2 @@\n-a: 0\n+a: 1\n b: 2"
	for _, path := range []string{"pnpm-lock.yaml", "web/package-lock.json", "composer.lock", "public/app.min.css"} {
		mock := &mockProvider{files: map[string]string{path: content}}
		e := New(mock, nil)
		units := e.Collect(context.Background(), Request{
			Hunks: HunkPolicy{Extensions: []string{"yaml", "json", "lock", "css"}, Before: 2, After: 1},
			Files: []provider.ModifiedFile{{Path: path, Status: "modified", Patch: patch}},
		})
		e.Close()
		if len(units) != 0 {
			t.Errorf("%s: a generated file must not be reviewed, got %d units", path, len(units))
		}
	}

	mock := &mockProvider{files: map[string]string{"config/services.yaml": content}}
	e := New(mock, nil)
	defer e.Close()
	units := e.Collect(context.Background(), Request{
		Hunks: HunkPolicy{Extensions: []string{"yaml"}, Before: 2, After: 1},
		Files: []provider.ModifiedFile{{Path: "config/services.yaml", Status: "modified", Patch: patch}},
	})
	if len(units) != 1 {
		t.Errorf("a regular yaml file is still reviewed, got %d units", len(units))
	}
}

func TestHasExtension(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"templates/a.html.twig", true},
		{"config/services.yaml", true},
		{"config/services.YML", true},
		{"front/App.vue", true},
		{".env", true},
		{".env.local", true},
		{"app/.env.prod", true},
		{"src/Env.php", false},
		{"src/environment.go", false},
		{"README.md", false},
	}
	for _, tt := range tests {
		if got := hasExtension(tt.path, []string{"twig", "yaml", "yml", ".vue", "env"}); got != tt.want {
			t.Errorf("hasExtension(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestExtensionLanguage(t *testing.T) {
	for path, want := range map[string]string{"a.yml": "yaml", "a.yaml": "yaml", "a.html.twig": "twig", ".env.local": "env", "App.vue": "vue", "Makefile": "text"} {
		if got := extensionLanguage(path); got != want {
			t.Errorf("extensionLanguage(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestCollect_LongClassHeaderIsTruncated(t *testing.T) {
	var props strings.Builder
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&props, "    private int $property%d = 0;\n", i)
	}
	content := "<?php\n\nclass Big\n{\n" + props.String() + "\n    public function run(): void\n    {\n        echo 1;\n    }\n}\n"
	line := strings.Count(content[:strings.Index(content, "echo 1;")], "\n") + 1
	patch := fmt.Sprintf("@@ -%d,1 +%d,1 @@\n-        echo 0;\n+        echo 1;", line, line)

	mock := &mockProvider{files: map[string]string{"Big.php": content}}
	e := New(mock, nil)
	defer e.Close()
	units := e.Collect(context.Background(), Request{
		Scope: ScopeFunctionClass,
		Files: []provider.ModifiedFile{{Path: "Big.php", Status: "modified", Patch: patch}},
	})
	if len(units) != 1 {
		t.Fatalf("units %d", len(units))
	}
	u := units[0]
	if len(u.Enclosing.Rendered) > maxHeaderBytes+len(headerTruncated) || !strings.HasSuffix(u.Enclosing.Rendered, headerTruncated) || len(u.Notes) != 1 {
		t.Errorf("header of %d bytes, notes %v", len(u.Enclosing.Rendered), u.Notes)
	}
}

func TestCollect_BlankLinesBetweenMethodsAreNotHunks(t *testing.T) {
	// a method is added: its blank separator line lies outside every function
	patch := "@@ -17,3 +17,9 @@\n     }\n+\n+    public function added(): Response\n+    {\n+        return new Response('z');\n+    }\n \n     #[Route('/{id}/edit')]"
	content := strings.Replace(symfonyController, "    #[Route('/{id}/edit')]", "    public function added(): Response\n    {\n        return new Response('z');\n    }\n\n    #[Route('/{id}/edit')]", 1)

	units := collectPHP(t, content, patch, Request{Scope: ScopeFunctionClass, MatchDeleted: true, Hunks: HunkPolicy{Orphans: true, Before: 2, After: 1}})
	for _, u := range units {
		if u.Kind == KindHunk {
			t.Errorf("a blank line must not become a hunk: %+v", u)
		}
	}
}

func TestCollect_VendoredAndGeneratedFilesAreSkippedWithoutConfig(t *testing.T) {
	src := "package x\n\nfunc F() int {\n\treturn 1\n}\n"
	mock := &mockProvider{files: map[string]string{
		"vendor/a.go": src, "src/vendor/b.go": src, "app.min.js": "function f(){return 1}", "ok.go": src,
	}}
	e := New(mock, nil)
	defer e.Close()

	patch := "@@ -3,3 +3,3 @@ package x\n func F() int {\n-\treturn 0\n+\treturn 1\n }"
	var files []provider.ModifiedFile
	for path := range mock.files {
		files = append(files, provider.ModifiedFile{Path: path, Status: "modified", Patch: patch})
	}

	result := e.Collect(context.Background(), Request{Files: files})
	if len(result) != 1 || result[0].FilePath != "ok.go" {
		t.Errorf("only ok.go should be collected, got %+v", result)
	}
}

func TestCollect_FunctionUnitsAreTruncated(t *testing.T) {
	var code, patch strings.Builder
	code.WriteString("package main\n\nfunc big() {\n")
	patch.WriteString("@@ -0,0 +1,2003 @@\n+package main\n+\n+func big() {\n")
	for i := 0; i < 2000; i++ {
		line := "\t_ = \"" + strings.Repeat("x", 80) + "\"\n"
		code.WriteString(line)
		patch.WriteString("+" + line)
	}
	code.WriteString("}\n")
	patch.WriteString("+}\n")

	mock := &mockProvider{files: map[string]string{"main.go": code.String()}}
	e := New(mock, nil)
	defer e.Close()

	units := e.Collect(context.Background(), Request{Files: []provider.ModifiedFile{{Path: "main.go", Status: "added", Patch: patch.String()}}})
	if len(units) != 1 {
		t.Fatalf("units %d", len(units))
	}
	if len(units[0].Content) > maxFunctionBytes || len(units[0].Diff) > maxFunctionBytes {
		t.Errorf("function capped at %d bytes, got content %d and diff %d", maxFunctionBytes, len(units[0].Content), len(units[0].Diff))
	}
	if len(units[0].Notes) != 1 || !strings.Contains(units[0].Notes[0], "truncated") {
		t.Errorf("the truncation must be reported, got notes %v", units[0].Notes)
	}
}
