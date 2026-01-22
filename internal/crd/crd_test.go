package crd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/lukasngl/secret-manager/internal/adapter"
)

// findModuleRoot walks up from the current directory to find go.mod.
func findModuleRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Minimal base CRD for testing
var testBaseCRD = []byte(`
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clientsecrets.secret-manager.ngl.cx
spec:
  group: secret-manager.ngl.cx
  names:
    kind: ClientSecret
    listKind: ClientSecretList
    plural: clientsecrets
    singular: clientsecret
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              provider:
                type: string
              config:
                type: object
`)

// testProvider implements adapter.Provider for testing.
type testProvider struct {
	name   string
	schema *adapter.Schema
}

func (t *testProvider) Type() string                   { return t.name }
func (t *testProvider) ConfigSchema() *adapter.Schema  { return t.schema }
func (t *testProvider) Validate(json.RawMessage) error { return nil }
func (t *testProvider) Provision(context.Context, json.RawMessage) (*adapter.Result, error) {
	return nil, nil
}
func (t *testProvider) DeleteKey(context.Context, json.RawMessage, string) error { return nil }

type simpleConfig struct {
	APIKey string `json:"apiKey"           jsonschema:"required"`
	Region string `json:"region,omitempty"`
}

type complexConfig struct {
	Endpoint string            `json:"endpoint"       jsonschema:"required"`
	Auth     authConfig        `json:"auth"           jsonschema:"required"`
	Tags     map[string]string `json:"tags,omitempty"`
}

type authConfig struct {
	Type   string `json:"type"   jsonschema:"required,enum=basic,enum=token"`
	Secret string `json:"secret" jsonschema:"required"`
}

func TestPatch_SingleProvider(t *testing.T) {
	reg := adapter.Registry{}
	reg.Register(&testProvider{
		name:   "simple",
		schema: adapter.MustSchema(simpleConfig{}),
	})

	result, err := Patch(testBaseCRD, reg)
	if err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	snaps.MatchSnapshot(t, string(result))
}

func TestPatch_MultipleProviders(t *testing.T) {
	reg := adapter.Registry{}
	reg.Register(&testProvider{
		name:   "simple",
		schema: adapter.MustSchema(simpleConfig{}),
	})
	reg.Register(&testProvider{
		name:   "complex",
		schema: adapter.MustSchema(complexConfig{}),
	})

	result, err := Patch(testBaseCRD, reg)
	if err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	snaps.MatchSnapshot(t, string(result))
}

func TestPatch_EmptyRegistry(t *testing.T) {
	reg := adapter.Registry{}

	result, err := Patch(testBaseCRD, reg)
	if err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	// Should still produce valid YAML, just without oneOf constraints
	snaps.MatchSnapshot(t, string(result))
}

func TestGenerate(t *testing.T) {
	baseCRDPath := filepath.Join(findModuleRoot(), DefaultBaseCRDPath)
	result, err := Generate(baseCRDPath, adapter.DefaultRegistry())
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	snaps.MatchSnapshot(t, string(result))
}

func TestPatch_InvalidBaseCRD(t *testing.T) {
	reg := adapter.Registry{}
	reg.Register(&testProvider{
		name:   "simple",
		schema: adapter.MustSchema(simpleConfig{}),
	})

	_, err := Patch([]byte("not valid yaml: {{{"), reg)
	if err == nil {
		t.Error("expected error for invalid base CRD")
	}
}
