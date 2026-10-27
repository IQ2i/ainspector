package related

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRepo_ReadAndGlob(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/Controller/A.php":   "<?php a",
		"src/Controller/b/B.php": "<?php b",
		"src/Entity/C.php":       "<?php c",
	})
	r := NewRepo(root, nil)

	if got, err := r.Read("src/Entity/C.php"); err != nil || got != "<?php c" {
		t.Errorf("Read = %q, %v", got, err)
	}
	got, err := r.Glob("src/Controller/**/*.php")
	if err != nil || !reflect.DeepEqual(got, []string{"src/Controller/A.php", "src/Controller/b/B.php"}) {
		t.Errorf("Glob = %v, %v", got, err)
	}
	if _, err := r.Glob("src/[.php"); err == nil {
		t.Error("an invalid glob must fail")
	}
}

func TestRepo_Grep(t *testing.T) {
	root := writeTree(t, map[string]string{
		"src/A.php": "isGranted('VIEW')",
		"src/B.php": "nothing",
		"src/C.txt": "isGranted('VIEW')",
	})
	r := NewRepo(root, nil)

	got, err := r.Grep(`isGranted\('VIEW'\)`, "src/**/*.php")
	if err != nil || !reflect.DeepEqual(got, []string{"src/A.php"}) {
		t.Errorf("Grep = %v, %v", got, err)
	}
	if got, _ := r.Grep(`VIEW`); len(got) != 2 {
		t.Errorf("without globs every file is searched, got %v", got)
	}
	if _, err := r.Grep(`(`); err == nil {
		t.Error("an invalid expression must fail")
	}
}

func TestRepo_NeverExposesDependenciesNorSecrets(t *testing.T) {
	root := writeTree(t, map[string]string{
		"vendor/x/X.php":       "secret",
		"node_modules/y/y.js":  "secret",
		"var/cache/z.php":      "secret",
		".env":                 "SECRET=1",
		".env.local":           "SECRET=1",
		"config/.env.prod":     "SECRET=1",
		"src/Ignored/I.php":    "secret",
		"src/Kept/K.php":       "ok",
		"nested/vendor/V.php":  "secret",
		"nested/other/Own.php": "ok",
		"config/secrets/prod/prod.decrypt.private.php": "secret",
		"config/jwt/private.pem":                       "secret",
		"deploy/id_rsa":                                "secret",
		"auth.json":                                    "secret",
		"dist/app.js":                                  "secret",
		"public/build/app.min.js":                      "secret",
		"assets/app.js.map":                            "secret",
	})
	r := NewRepo(root, func(p string) bool { return strings.HasPrefix(p, "src/Ignored/") })

	for _, p := range []string{"vendor/x/X.php", "node_modules/y/y.js", "var/cache/z.php", ".env", ".env.local", "config/.env.prod", "src/Ignored/I.php", "nested/vendor/V.php",
		"config/secrets/prod/prod.decrypt.private.php", "config/jwt/private.pem", "deploy/id_rsa", "auth.json", "dist/app.js", "public/build/app.min.js", "assets/app.js.map"} {
		if _, err := r.Read(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s must not be readable, got %v", p, err)
		}
	}
	all, _ := r.Glob("**")
	if !reflect.DeepEqual(all, []string{"nested/other/Own.php", "src/Kept/K.php"}) {
		t.Errorf("unexpected listing %v", all)
	}
	if got, _ := r.Grep("secret|SECRET"); got != nil {
		t.Errorf("grep must not see excluded files, got %v", got)
	}
}

func TestRepo_RefusesPathsOutsideTheRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "outside.txt"), []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "outside.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skip("symbolic links unavailable")
	}
	r := NewRepo(root, nil)

	for _, p := range []string{"../outside.txt", "/etc/passwd", "link.txt", ".", ""} {
		if got, err := r.Read(p); err == nil {
			t.Errorf("Read(%q) = %q, expected an error", p, got)
		}
	}
	if all, _ := r.Glob("**"); len(all) != 0 {
		t.Errorf("symbolic links must not be listed, got %v", all)
	}
}

func TestRepo_LargeFilesAreCut(t *testing.T) {
	root := writeTree(t, map[string]string{"big.txt": strings.Repeat("a", maxFileBytes+10)})
	got, err := NewRepo(root, nil).Read("big.txt")
	if err != nil || len(got) != maxFileBytes {
		t.Errorf("len = %d, err = %v", len(got), err)
	}
}

func TestRepo_NeverFollowsLinksInsideTheRoot(t *testing.T) {
	root := writeTree(t, map[string]string{
		".env.local":             "SECRET=1",
		"config/secrets/k.php":   "secret",
		"config/ok.yaml":         "fine",
		"templates/real.twig":    "fine",
		"src/Security/Voter.php": "fine",
	})
	links := map[string]string{
		"templates/x.twig":   "../.env.local",
		"templates/y.twig":   "real.twig",
		"src/Linked":         "../config/secrets",
		"templates/cfg.yaml": "../config/ok.yaml",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skip("symbolic links unavailable")
		}
	}
	r := NewRepo(root, nil)

	for _, p := range []string{"templates/x.twig", "templates/y.twig", "src/Linked/k.php", "templates/cfg.yaml"} {
		if got, err := r.Read(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("Read(%q) = %q, %v: a link must not be followed", p, got, err)
		}
	}
	if got, err := r.Read("templates/real.twig"); err != nil || got != "fine" {
		t.Errorf("a regular file must stay readable, got %q, %v", got, err)
	}
}

func TestRepo_GrepIsRepeatable(t *testing.T) {
	r := NewRepo(writeTree(t, map[string]string{"src/A.php": "needle"}), nil)
	first, _ := r.Grep("needle", "src/**/*.php")
	second, _ := r.Grep("needle", "src/**/*.php")
	if !reflect.DeepEqual(first, second) || len(first) != 1 {
		t.Errorf("got %v then %v", first, second)
	}
}
