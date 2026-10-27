package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/iq2i/ainspector/internal/config"
	"github.com/iq2i/ainspector/internal/publish"
)

// reports are the files written once at the end of a run, fed by every mode. A nil report is
// one the configuration disabled.
type reports struct {
	codeQuality, artifact publish.Report
}

func newReports(dir string, cfg *config.Config, change publish.Change) reports {
	var r reports
	if path := cfg.CodeQualityPath(); path != "" {
		r.codeQuality = publish.NewCodeQuality(dir, path)
	}
	if path := cfg.ArtifactPath(); path != "" {
		r.artifact = publish.NewArtifact(dir, path, change)
	}
	return r
}

// add hands what a mode produced to the reports its policy feeds
func (r reports) add(ctx context.Context, policy publish.Policy, run publish.Run) error {
	var errs []error
	if r.codeQuality != nil && policy.CodeQuality {
		errs = append(errs, r.codeQuality.Publish(ctx, run))
	}
	if r.artifact != nil && policy.Artifact {
		errs = append(errs, r.artifact.Publish(ctx, run))
	}
	return errors.Join(errs...)
}

func (r reports) write() error {
	var errs []error
	if r.codeQuality != nil {
		if err := r.codeQuality.Write(); err != nil {
			errs = append(errs, fmt.Errorf("failed to write the Code Quality report: %w", err))
		}
	}
	if r.artifact != nil {
		if err := r.artifact.Write(); err != nil {
			errs = append(errs, fmt.Errorf("failed to write the artifact: %w", err))
		}
	}
	return errors.Join(errs...)
}
