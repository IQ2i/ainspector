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
		return nil, fmt.Errorf("%s: %w", rel, ErrOutsideRoot)
	}

	// Before opening: opening a named pipe would block until something writes to it
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", rel)
	}

	f, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return io.ReadAll(io.LimitReader(f, int64(limit)))
}
