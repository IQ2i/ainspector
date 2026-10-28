package publish

import (
	"encoding/json"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/findings"
)

const artifactVersion = 1

// Change identifies the PR/MR a report is about
type Change struct {
	Number      int    `json:"number"`
	CheckoutSHA string `json:"checkout_sha,omitempty"`
}

type artifactDoc struct {
	Version int            `json:"version"`
	Change  Change         `json:"change"`
	Modes   []artifactMode `json:"modes"`
}

type artifactMode struct {
	Mode            string              `json:"mode"`
	Scope           string              `json:"scope"`
	Version         string              `json:"version"`
	Model           string              `json:"model,omitempty"`
	Units           []artifactUnit      `json:"units"`
	AlreadyReviewed int                 `json:"already_reviewed"`
	Published       []findings.Finding  `json:"published"`
	Overflow        []findings.Finding  `json:"overflow"`
	Unjudged        []findings.Finding  `json:"unjudged"`
	Repeated        []findings.Finding  `json:"repeated"`
	Carried         []findings.Reported `json:"carried"`
	Discarded       int                 `json:"discarded"`
	FPModel         string              `json:"fp_model,omitempty"`
	Verdicts        []findings.Verdict  `json:"verdicts"`
	Skipped         []string            `json:"skipped"`
	Failed          []string            `json:"failed"`
	Usage           artifactUsage       `json:"usage"`
	Cost            float64             `json:"cost"`
	CostIncomplete  bool                `json:"cost_incomplete"`
	Notes           []string            `json:"notes"`
	Feedback        []Feedback          `json:"feedback,omitempty"`
}

type artifactUsage struct {
	Input      int `json:"input"`
	Output     int `json:"output"`
	CacheRead  int `json:"cache_read"`
	CacheWrite int `json:"cache_write"`
}

// artifactUnit describes a unit without its code, nor the content of its related files: the
// artifact is archived, and the repository is not ours to copy
type artifactUnit struct {
	Kind        collect.UnitKind  `json:"kind"`
	File        string            `json:"file"`
	Name        string            `json:"name"`
	StartLine   int               `json:"start_line"`
	EndLine     int               `json:"end_line"`
	ChangeType  string            `json:"change_type"`
	DeletedOnly bool              `json:"deleted_only,omitempty"`
	Hash        string            `json:"hash,omitempty"`
	Status      string            `json:"status,omitempty"`
	Enclosing   string            `json:"enclosing,omitempty"`
	Related     []artifactRelated `json:"related,omitempty"`
	Notes       []string          `json:"notes,omitempty"`
}

type artifactRelated struct {
	Path      string `json:"path"`
	Reason    string `json:"reason"`
	Truncated bool   `json:"truncated,omitempty"`
}

// WriteArtifact writes the JSON document that describes a whole run, for archiving: the units,
// the findings (published, left out by the cap, repeated, carried), the verdicts of the false
// positive filter, the tokens and the estimated cost, for every mode.
func WriteArtifact(dir, path string, change Change, runs []Run) error {
	doc := artifactDoc{Version: artifactVersion, Change: change, Modes: make([]artifactMode, 0, len(runs))}
	for _, run := range runs {
		doc.Modes = append(doc.Modes, newArtifactMode(run))
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(dir, path, append(data, '\n'))
}

func newArtifactMode(run Run) artifactMode {
	m := artifactMode{
		Mode:            run.Mode,
		Scope:           run.Scope,
		Version:         run.Version,
		Model:           run.Model,
		Units:           make([]artifactUnit, 0, len(run.Units)),
		AlreadyReviewed: run.AlreadyReviewed,
		Published:       withoutSuggestions(run.Published),
		Overflow:        withoutSuggestions(run.Overflow),
		Unjudged:        withoutSuggestions(run.Unjudged),
		Repeated:        withoutSuggestions(run.Repeated),
		Carried:         orEmpty(run.Carried),
		Discarded:       run.Discarded,
		FPModel:         run.FPModel,
		Verdicts:        orEmpty(run.Verdicts),
		Skipped:         orEmpty(run.Skipped),
		Failed:          orEmpty(run.Failed),
		Usage:           artifactUsage(run.Usage),
		Cost:            run.Cost,
		CostIncomplete:  run.CostIncomplete,
		Notes:           orEmpty(run.Notes),
		Feedback:        run.Feedback,
	}
	for i, u := range run.Units {
		au := artifactUnit{
			Kind: u.Kind, File: u.FilePath, Name: u.Name, StartLine: u.StartLine, EndLine: u.EndLine,
			ChangeType: u.ChangeType, DeletedOnly: u.DeletedOnly, Notes: u.Notes,
		}
		if i < len(run.Statuses) {
			au.Hash, au.Status = run.Statuses[i].Hash, run.Statuses[i].Status
		}
		if u.Enclosing != nil {
			au.Enclosing = u.Enclosing.Name
		}
		for _, r := range u.Related {
			au.Related = append(au.Related, artifactRelated{Path: r.Path, Reason: r.Reason, Truncated: r.Truncated})
		}
		m.Units = append(m.Units, au)
	}
	return m
}

// withoutSuggestions copies the findings without the code they suggest, which comes from the
// repository
func withoutSuggestions(fs []findings.Finding) []findings.Finding {
	out := make([]findings.Finding, len(fs))
	for i, f := range fs {
		f.Suggestion = ""
		out[i] = f
	}
	return out
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
