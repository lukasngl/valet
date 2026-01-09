package adapter

import (
	"iter"
	"maps"
)

type Registry map[string]Provider

// Global registry
var def = Registry{}

func DefaultRegistry() Registry {
	return def
}

// Register adds a provider to the global registry.
// This is called from a provider's init() function.
func (r Registry) Register(p Provider) {
	r[p.Type()] = p
}

// Get returns a provider by type from the global registry.
func (r Registry) Get(providerType string) Provider {
	return r[providerType]
}

// All returns an iterator over all registered providers.
func (r Registry) All() iter.Seq2[string, Provider] {
	return maps.All(r)
}

// ConfigSchema builds the tagged union of all confgiures providers,
// as a oneOf schema for provider-specific config validation.
func (r *Registry) ConfigSchema() []map[string]any {
	// Build oneOf variants from all providers
	oneOf := make([]map[string]any, 0)

	for typ, provider := range r.All() {
		oneOf = append(oneOf, map[string]any{
			"properties": map[string]any{
				"provider": map[string]any{"const": typ},
				"config":   provider.ConfigSchema(),
			},
		})
	}

	return oneOf
}
