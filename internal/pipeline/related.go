package pipeline

import (
	"context"
	"fmt"

	"github.com/iq2i/ainspector/internal/collect"
	"github.com/iq2i/ainspector/internal/collect/related"
	"github.com/iq2i/ainspector/internal/collect/related/symfony"
	"github.com/iq2i/ainspector/internal/collect/related/vue"
	"github.com/iq2i/ainspector/internal/provider"
)

// resolversFor builds the resolvers of the named presets
func resolversFor(presets []string) ([]related.Resolver, error) {
	var resolvers []related.Resolver
	for _, name := range presets {
		switch name {
		case "symfony":
			resolvers = append(resolvers, symfony.Resolvers()...)
		case "vue":
			resolvers = append(resolvers, vue.Resolvers()...)
		default:
			return nil, fmt.Errorf("unknown related files preset %q", name)
		}
	}
	return resolvers, nil
}

// attachRelated gives each unit the files related to it, read in the checkout of the job
func attachRelated(ctx context.Context, deps Deps, opts Options, units []collect.ReviewUnit, policy collect.RelatedPolicy, changeInfo func() (provider.ChangeInfo, error)) error {
	resolvers, err := resolversFor(policy.Presets)
	if err != nil {
		return err
	}

	if opts.CheckoutSHA != "" {
		if info, err := changeInfo(); err == nil && info.HeadSHA != "" && info.HeadSHA != opts.CheckoutSHA {
			fmt.Printf("Note: related files are read from the checkout of %s while the head of the change is %s (merge result pipeline or outdated checkout)\n", opts.CheckoutSHA, info.HeadSHA)
		}
	}

	fmt.Println("Looking for related files...")
	repo := related.NewRepo(deps.ProjectRoot, deps.Config.ShouldIgnore)
	related.Attach(ctx, units, repo, resolvers, policy)
	return nil
}

// mergeRelated widens a policy with another: the union of presets, the largest caps
func mergeRelated(a, b collect.RelatedPolicy) collect.RelatedPolicy {
	for _, preset := range b.Presets {
		found := false
		for _, existing := range a.Presets {
			found = found || existing == preset
		}
		if !found {
			a.Presets = append(a.Presets, preset)
		}
	}
	a.MaxFiles = max(a.MaxFiles, b.MaxFiles)
	a.MaxBytes = max(a.MaxBytes, b.MaxBytes)
	return a
}
