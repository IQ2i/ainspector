package related

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/iq2i/ainspector/internal/collect"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

type fixedResolver struct {
	files []collect.RelatedFile
	err   error
}

func (fixedResolver) Name() string { return "fixed" }

func (r fixedResolver) Resolve(context.Context, *collect.ReviewUnit, Repo) ([]collect.RelatedFile, error) {
	return r.files, r.err
}

func file(path string, size int) collect.RelatedFile {
	return collect.RelatedFile{Path: path, Content: strings.Repeat("x", size)}
}

func TestAttach_KeepsOrderAndDropsSelfAndDuplicates(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "src/A.php", Name: "show"}}
	r := fixedResolver{files: []collect.RelatedFile{file("b", 1), file("src/A.php", 1), file("c", 1), file("b", 1)}}

	Attach(context.Background(), units, nil, []Resolver{r}, collect.RelatedPolicy{MaxFiles: 5, MaxBytes: 1000})

	got := units[0].Related
	if len(got) != 2 || got[0].Path != "b" || got[1].Path != "c" {
		t.Errorf("unexpected related files %+v", got)
	}
	if len(units[0].Notes) != 0 {
		t.Errorf("nothing was cut, got notes %v", units[0].Notes)
	}
}

func TestAttach_CapsFiles(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "a", Name: "f"}}
	r := fixedResolver{files: []collect.RelatedFile{file("1", 1), file("2", 1), file("3", 1)}}

	Attach(context.Background(), units, nil, []Resolver{r}, collect.RelatedPolicy{MaxFiles: 2, MaxBytes: 1000})

	if len(units[0].Related) != 2 {
		t.Errorf("expected 2 files, got %d", len(units[0].Related))
	}
	if len(units[0].Notes) != 1 || !strings.Contains(units[0].Notes[0], "1 left out") {
		t.Errorf("the omission must be reported, got %v", units[0].Notes)
	}
}

func TestAttach_TruncatesTheFileThatCrossesTheByteCap(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "a", Name: "f"}}
	big := collect.RelatedFile{Path: "big", Content: strings.Repeat("line of code\n", 100)}
	r := fixedResolver{files: []collect.RelatedFile{file("small", 300), big, file("after", 10)}}

	Attach(context.Background(), units, nil, []Resolver{r}, collect.RelatedPolicy{MaxFiles: 5, MaxBytes: 800})

	got := units[0].Related
	if len(got) != 2 || got[0].Truncated || !got[1].Truncated || got[1].Path != "big" {
		t.Fatalf("unexpected related files %+v", got)
	}
	total := 0
	for _, f := range got {
		total += len(f.Content)
	}
	if total > 800 {
		t.Errorf("the byte cap is exceeded: %d", total)
	}
	if !strings.HasSuffix(got[1].Content, truncationMarker) {
		t.Error("a truncated file must say so")
	}
	if !strings.Contains(units[0].Notes[0], "1 left out and 1 truncated") {
		t.Errorf("unexpected note %v", units[0].Notes)
	}
}

func TestAttach_OmitsWhenNoRoomIsLeft(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "a", Name: "f"}}
	r := fixedResolver{files: []collect.RelatedFile{file("1", 900), file("2", 500)}}

	Attach(context.Background(), units, nil, []Resolver{r}, collect.RelatedPolicy{MaxFiles: 5, MaxBytes: 1000})

	if len(units[0].Related) != 1 {
		t.Errorf("expected only the first file, got %+v", units[0].Related)
	}
}

func TestAttach_ResolverErrorIsNotFatal(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "a", Name: "f"}}
	failing := fixedResolver{err: errors.New("boom")}
	working := fixedResolver{files: []collect.RelatedFile{file("ok", 1)}}

	Attach(context.Background(), units, nil, []Resolver{failing, working}, collect.RelatedPolicy{MaxFiles: 5, MaxBytes: 100})

	if len(units[0].Related) != 1 {
		t.Errorf("the other resolvers must still run, got %+v", units[0].Related)
	}
}

func TestAttach_NoResolverResultLeavesTheUnitUntouched(t *testing.T) {
	units := []collect.ReviewUnit{{FilePath: "a", Name: "f"}}
	Attach(context.Background(), units, nil, []Resolver{fixedResolver{}}, collect.RelatedPolicy{MaxFiles: 5, MaxBytes: 100})
	if units[0].Related != nil || units[0].Notes != nil {
		t.Errorf("unexpected %+v", units[0])
	}
}

func TestAttach_StopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	units := []collect.ReviewUnit{{FilePath: "a"}}
	Attach(ctx, units, nil, []Resolver{fixedResolver{files: []collect.RelatedFile{file("x", 1)}}}, collect.RelatedPolicy{MaxFiles: 5})
	if units[0].Related != nil {
		t.Error("a cancelled run must not resolve")
	}
}

func TestCut_NeverSplitsARune(t *testing.T) {
	s := strings.Repeat("é", 50)
	for n := 1; n < 40; n++ {
		if got := cut(s, n); !utf8.ValidString(got) || len(got) > n {
			t.Fatalf("cut(%d) = %q", n, got)
		}
	}
	if cut("abc\ndef", 5) != "abc" {
		t.Error("expected a cut on a line boundary")
	}
	if cut("abc", 0) != "" || cut("abc", 10) != "abc" {
		t.Error("unexpected bounds")
	}
}
