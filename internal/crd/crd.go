// Package crd provides CRD generation utilities.
package crd

import (
	"fmt"
	"os"

	"github.com/go-openapi/jsonpointer"
	"github.com/lukasngl/secret-manager/internal/adapter"
	"gopkg.in/yaml.v3"
)

// DefaultBaseCRDPath is the default path to the base CRD file.
const DefaultBaseCRDPath = "config/crd/secret-manager.ngl.cx_clientsecrets.yaml"

// JSON Pointer to the spec properties in the CRD schema.
const specPointer = "/spec/versions/0/schema/openAPIV3Schema/properties/spec"

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
	var crd any
	if err := yaml.Unmarshal(baseCRD, &crd); err != nil {
		return nil, fmt.Errorf("unmarshaling base CRD: %w", err)
	}

	ptr, err := jsonpointer.New(specPointer)
	if err != nil {
		return nil, fmt.Errorf("invalid JSON pointer: %w", err)
	}

	spec, _, err := ptr.Get(crd)
	if err != nil {
		return nil, fmt.Errorf("getting spec from CRD: %w", err)
	}

	specMap, ok := spec.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("spec is not a map")
	}

	// Inject allOf with oneOf schema from registry
	specMap["allOf"] = []any{
		map[string]any{"oneOf": registry.ConfigSchema()},
	}

	return yaml.Marshal(crd)
}
