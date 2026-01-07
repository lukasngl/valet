package adapter

import (
	"fmt"
	"sort"

	"sigs.k8s.io/yaml"
)

// GenerateKustomizePatch creates a strategic merge patch for the CRD spec schema.
// It builds a oneOf schema from all registered providers, enabling provider-specific
// config validation in the OpenAPI schema.
func GenerateKustomizePatch() ([]byte, error) {
	// Build oneOf variants from all providers
	oneOf := make([]map[string]any, 0)
	providerTypes := make([]string, 0)

	for typ := range All() {
		providerTypes = append(providerTypes, typ)
	}
	// Sort for deterministic output
	sort.Strings(providerTypes)

	for _, typ := range providerTypes {
		p := Get(typ)
		if p == nil {
			continue
		}

		variant := map[string]any{
			"properties": map[string]any{
				"provider": map[string]any{"const": typ},
				"config":   p.ConfigSchema(),
			},
		}
		oneOf = append(oneOf, variant)
	}

	if len(oneOf) == 0 {
		return nil, fmt.Errorf("no providers registered")
	}

	// Generate strategic merge patch
	patch := map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata": map[string]any{
			"name": "clientsecrets.secret-manager.ngl.cx",
		},
		"spec": map[string]any{
			"versions": []map[string]any{{
				"name": "v1alpha1",
				"schema": map[string]any{
					"openAPIV3Schema": map[string]any{
						"properties": map[string]any{
							"spec": map[string]any{
								"allOf": []any{
									map[string]any{"oneOf": oneOf},
								},
							},
						},
					},
				},
			}},
		},
	}

	return yaml.Marshal(patch)
}
