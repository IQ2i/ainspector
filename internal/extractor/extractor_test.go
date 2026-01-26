package extractor

import (
	"context"
	"errors"
	"testing"

	"github.com/iq2i/ainspector/internal/provider"
)

// mockProvider is a test double for the Provider interface
type mockProvider struct {
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

func (m *mockProvider) PostComment(ctx context.Context, number int, body string) error {
	return nil
}

func (m *mockProvider) CreateReview(ctx context.Context, number int, comments []provider.ReviewComment) error {
	return nil
}

func (m *mockProvider) GetReviewComments(ctx context.Context, number int) ([]provider.ExistingComment, error) {
	return nil, nil
}

func TestExtractModifiedFunctions_SkipsDeletedFiles(t *testing.T) {
	mock := &mockProvider{files: map[string]string{}}
	e := New(mock, nil)
	defer e.Close()

	files := []provider.ModifiedFile{
		{Path: "deleted.go", Status: "deleted", Patch: "@@ -1,3 +0,0 @@\n-line1\n-line2\n-line3"},
	}

	ctx := context.Background()
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 functions for deleted file, got %d", len(result))
	}
}

func TestExtractModifiedFunctions_SkipsUnsupportedFiles(t *testing.T) {
	mock := &mockProvider{files: map[string]string{
		"data.json": `{"key": "value"}`,
	}}
	e := New(mock, nil)
	defer e.Close()

	files := []provider.ModifiedFile{
		{Path: "data.json", Status: "modified", Patch: "@@ -1 +1 @@\n-old\n+new"},
	}

	ctx := context.Background()
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 functions for unsupported file, got %d", len(result))
	}
}

func TestExtractModifiedFunctions_SingleModifiedFunction(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestExtractModifiedFunctions_AddedFile(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	if result[0].ChangeType != "added" {
		t.Errorf("expected change type 'added', got %s", result[0].ChangeType)
	}
}

func TestExtractModifiedFunctions_MultipleFunctions(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestExtractModifiedFunctions_MultipleFiles(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

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

func TestExtractModifiedFunctions_ContinuesOnFileError(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still extract from the good file
	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	if result[0].Name != "goodFunc" {
		t.Errorf("expected function 'goodFunc', got %s", result[0].Name)
	}
}

func TestExtractModifiedFunctions_NoModifiedFunctions(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 functions (none modified), got %d", len(result))
	}
}

func TestExtractModifiedFunctions_ExtractsDiff(t *testing.T) {
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
	result, err := e.ExtractModifiedFunctions(ctx, files)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result))
	}

	// Should have extracted the diff for the function
	if result[0].Diff == "" {
		t.Error("expected non-empty diff")
	}
}

func TestExtractModifiedFunctions_EmptyFileList(t *testing.T) {
	mock := &mockProvider{files: map[string]string{}}
	e := New(mock, nil)
	defer e.Close()

	ctx := context.Background()
	result, err := e.ExtractModifiedFunctions(ctx, []provider.ModifiedFile{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 functions for empty file list, got %d", len(result))
	}
}

func TestNew(t *testing.T) {
	mock := &mockProvider{}
	e := New(mock, nil)
	if e == nil {
		t.Fatal("expected non-nil extractor")
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
