// Package presets is the registry of the resolvers of related files that the configuration can
// name.
package presets

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/iq2i/ainspector/internal/collect/related"
	"github.com/iq2i/ainspector/internal/collect/related/symfony"
	"github.com/iq2i/ainspector/internal/collect/related/vue"
)

var registry = map[string]func() []related.Resolver{
	"symfony": symfony.Resolvers,
	"vue":     vue.Resolvers,
}

// Names lists the presets, in alphabetical order
func Names() []string {
	return slices.Sorted(maps.Keys(registry))
}

// Check refuses a name that is not a preset
func Check(names []string) error {
	for _, name := range names {
		if _, ok := registry[name]; !ok {
			return fmt.Errorf("unknown related files preset %q (expected one of %s)", name, strings.Join(Names(), ", "))
		}
	}
	return nil
}

// Resolvers builds the resolvers of the named presets, in the order of the names
func Resolvers(names []string) ([]related.Resolver, error) {
	if err := Check(names); err != nil {
		return nil, err
	}
	var resolvers []related.Resolver
	for _, name := range names {
		resolvers = append(resolvers, registry[name]()...)
	}
	return resolvers, nil
}
