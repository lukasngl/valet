// Package crd provides CRD generation utilities.
package crd

import (
	"fmt"
	"os"

	"github.com/lukasngl/secret-manager/internal/adapter"
	"gopkg.in/yaml.v3"
)

// DefaultBaseCRDPath is the default path to the base CRD file.
const DefaultBaseCRDPath = "config/crd/secret-manager.ngl.cx_clientsecrets.yaml"

// Generate reads the base CRD from the given path, patches it with provider
// schemas from the registry, and returns the complete CRD as YAML bytes.
func Generate(baseCRDPath string, registry adapter.Registry) ([]byte, error) {
	baseBytes, err := os.ReadFile(baseCRDPath)
	if err != nil {
		return nil, fmt.Errorf("reading base CRD: %w", err)
	}

	return Patch(baseBytes, registry)
}

// Patch takes base CRD bytes, patches them with provider schemas from the
// registry, and returns the complete CRD as YAML bytes.
func Patch(baseCRD []byte, registry adapter.Registry) ([]byte, error) {
	var crd map[string]any
	if err := yaml.Unmarshal(baseCRD, &crd); err != nil {
		return nil, fmt.Errorf("unmarshaling base CRD: %w", err)
	}

	// Navigate to spec.versions[0].schema.openAPIV3Schema.properties.spec
	spec, ok := crd["spec"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing spec in CRD")
	}

	versions, ok := spec["versions"].([]any)
	if !ok || len(versions) == 0 {
		return nil, fmt.Errorf("missing versions in CRD spec")
	}

	v0, ok := versions[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid version[0] in CRD")
	}

	schema, ok := v0["schema"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing schema in version[0]")
	}

	openAPISchema, ok := schema["openAPIV3Schema"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing openAPIV3Schema")
	}

	properties, ok := openAPISchema["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing properties in openAPIV3Schema")
	}

	specProps, ok := properties["spec"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing spec in properties")
	}

	// Inject allOf with oneOf schema from registry
	specProps["allOf"] = []any{
		map[string]any{"oneOf": registry.ConfigSchema()},
	}

	return yaml.Marshal(crd)
}
