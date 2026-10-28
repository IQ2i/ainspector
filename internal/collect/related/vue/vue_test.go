package vue

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/collect/related"
)

func resolve(t *testing.T, files map[string]string, u collect.ReviewUnit) []string {
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
	repo := related.NewRepo(root, nil)

	var paths []string
	for _, r := range Resolvers() {
		found, err := r.Resolve(context.Background(), &u, repo)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range found {
			paths = append(paths, f.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestComponent_JoinsParentsStoresAndComposables(t *testing.T) {
	files := map[string]string{
		"src/components/Card.vue": `<script setup>
import { useAuth } from '@/composables/useAuth'
import { useCart } from '../stores/cart'
import Child from './Child.vue'
import { ref } from 'vue'
import helper from '../utils/helper'
</script>`,
		"src/pages/Home.vue":         `import Card from '../components/Card.vue'`,
		"src/pages/Other.vue":        `import Else from '../components/Else.vue'`,
		"src/router.ts":              `import Card from './components/Card.vue'`,
		"src/composables/useAuth.ts": "export const useAuth = () => {}",
		"src/stores/cart/index.ts":   "export const useCart = () => {}",
		"src/utils/helper.ts":        "export default {}",
		"src/components/Child.vue":   "<template></template>",
	}
	u := collect.ReviewUnit{FilePath: "src/components/Card.vue", Language: "vue"}

	want := []string{"src/composables/useAuth.ts", "src/pages/Home.vue", "src/router.ts", "src/stores/cart/index.ts"}
	if got := resolve(t, files, u); !equal(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestComponent_NothingFound(t *testing.T) {
	files := map[string]string{"src/Alone.vue": "<template></template>"}
	if got := resolve(t, files, collect.ReviewUnit{FilePath: "src/Alone.vue"}); len(got) != 0 {
		t.Errorf("unexpected %v", got)
	}
}

func TestComponent_IgnoresOtherFiles(t *testing.T) {
	if got := resolve(t, map[string]string{"a.ts": "x"}, collect.ReviewUnit{FilePath: "a.ts"}); len(got) != 0 {
		t.Errorf("unexpected %v", got)
	}
}

func TestResolveImport(t *testing.T) {
	cases := map[string]string{
		"./a":                 "src/c/a",
		"../s/b":              "src/s/b",
		"@/stores/x":          "src/stores/x",
		"~/stores/x":          "src/stores/x",
		"vue":                 "",
		"../../../etc":        "",
		"@/stores/../../x":    "",
		"@/stores/../../../x": "",
		"./../../../etc":      "",
		"@/stores/a/../b":     "src/stores/b",
	}
	for spec, want := range cases {
		if got := resolveImport("src/c", spec); got != want {
			t.Errorf("resolveImport(%q) = %q, want %q", spec, got, want)
		}
	}
}
