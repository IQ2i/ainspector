package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideRoot is returned for a file that resolves outside the project, through a symbolic link
// or a ".." segment
var ErrOutsideRoot = errors.New("path resolves outside the project")

// ErrSensitive is returned for a file that holds secrets, whatever link led to it
var ErrSensitive = errors.New("path resolves to a file that may hold secrets")

// ReadProjectFile reads at most limit bytes of a regular file of the project. The repository is
// the content of the PR/MR under review: a link to a file outside of it, such as the environment
// of the job, would otherwise end up in the prompt.
func ReadProjectFile(root, rel string, limit int) ([]byte, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, rel))
	if err != nil {
		return nil, err
	}

	inside, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%q: %w", rel, ErrOutsideRoot)
	}

	// A link inside the project may lead to .env or .git/config: the name asked for is not enough
	if IsSensitivePath(filepath.ToSlash(inside)) {
		return nil, fmt.Errorf("%q: %w", rel, ErrSensitive)
	}

	// Before opening: opening a named pipe would block until something writes to it
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q: not a regular file", rel)
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return io.ReadAll(io.LimitReader(f, int64(limit)))
}

// CheckOutputPath tells whether a path is fit for a file the tool writes: relative and staying
// inside the directory it is resolved against. The path comes from the configuration of the
// project, which the author of the PR/MR controls.
func CheckOutputPath(p string) error {
	if p == "" {
		return errors.New("empty path")
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || filepath.VolumeName(p) != "" {
		return fmt.Errorf("%s: must be relative", p)
	}
	for _, segment := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("%s: must not contain a parent directory segment", p)
		}
		if strings.EqualFold(segment, ".git") {
			return fmt.Errorf("%s: must not point into the .git folder", p)
		}
	}
	return nil
}
