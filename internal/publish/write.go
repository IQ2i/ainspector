package publish

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iq2i/ainspector/internal/config"
)

// writeFile writes a report below dir. The path comes from the configuration of the project,
// which the author of the PR/MR controls: it cannot leave dir, and neither a link nor a
// directory that is a link may take the data elsewhere. The file is replaced atomically.
func writeFile(dir, rel string, data []byte) error {
	if err := config.CheckOutputPath(rel); err != nil {
		return err
	}
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })

	current := dir
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		switch {
		case os.IsNotExist(err):
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case info.Mode()&os.ModeSymlink != 0 || !info.IsDir():
			return fmt.Errorf("%s: %s is not a plain directory", rel, part)
		}
	}

	target := filepath.Join(current, parts[len(parts)-1])
	if info, err := os.Lstat(target); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("%s: not a regular file", rel)
	}

	tmp, err := os.CreateTemp(current, ".ainspector-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
