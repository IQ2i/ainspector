package pipeline

import (
	"errors"
	"fmt"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/publish"
)

// reports gathers what every mode produced, for the files written once at the end of a run.
// A report whose path the configuration emptied is not written.
type reports struct {
	dir    string
	cfg    *config.Config
	change publish.Change
	runs   []publish.Run
}

func newReports(dir string, cfg *config.Config, change publish.Change) *reports {
	return &reports{dir: dir, cfg: cfg, change: change}
}

func (r *reports) add(run publish.Run) {
	r.runs = append(r.runs, run)
}

func (r *reports) write() error {
	var errs []error
	if path := r.cfg.CodeQualityPath(); path != "" {
		if err := publish.WriteCodeQuality(r.dir, path, r.runs); err != nil {
			errs = append(errs, fmt.Errorf("failed to write the Code Quality report: %w", err))
		}
	}
	if path := r.cfg.ArtifactPath(); path != "" {
		if err := publish.WriteArtifact(r.dir, path, r.change, r.runs); err != nil {
			errs = append(errs, fmt.Errorf("failed to write the artifact: %w", err))
		}
	}
	return errors.Join(errs...)
}
