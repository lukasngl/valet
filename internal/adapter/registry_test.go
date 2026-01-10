package adapter

import (
	"context"
	"encoding/json"
	"testing"
)

// mockProvider implements Provider for testing.
type mockProvider struct {
	name   string
	schema *Schema
}

func (m *mockProvider) Type() string                   { return m.name }
func (m *mockProvider) ConfigSchema() *Schema          { return m.schema }
func (m *mockProvider) Validate(json.RawMessage) error { return nil }
func (m *mockProvider) Provision(context.Context, json.RawMessage) (*Result, error) {
	return nil, nil
}
func (m *mockProvider) DeleteKey(context.Context, json.RawMessage, string) error { return nil }

func TestRegistry_Register_Get(t *testing.T) {
	reg := Registry{}

	mock := &mockProvider{name: "test", schema: MustSchema(struct{}{})}
	reg.Register(mock)

	got := reg.Get("test")
	if got != mock {
		t.Error("expected to get registered provider")
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	reg := Registry{}

	got := reg.Get("nonexistent")
	if got != nil {
		t.Error("expected nil for nonexistent provider")
	}
}

func TestRegistry_All(t *testing.T) {
	reg := Registry{}

	mock1 := &mockProvider{name: "test1", schema: MustSchema(struct{}{})}
	mock2 := &mockProvider{name: "test2", schema: MustSchema(struct{}{})}
	reg.Register(mock1)
	reg.Register(mock2)

	count := 0
	for range reg.All() {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 providers, got %d", count)
	}
}

func TestRegistry_ConfigSchema(t *testing.T) {
	reg := Registry{}

	type testConfig struct {
		Field string `json:"field"`
	}
	mock := &mockProvider{name: "test", schema: MustSchema(testConfig{})}
	reg.Register(mock)

	schemas := reg.ConfigSchema()
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema variant, got %d", len(schemas))
	}

	// Verify oneOf structure has provider and config properties
	variant := schemas[0]
	props, ok := variant["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema variant")
	}

	if _, ok := props["provider"]; !ok {
		t.Error("expected provider property in schema")
	}
	if _, ok := props["config"]; !ok {
		t.Error("expected config property in schema")
	}
}

func TestRegistry_ConfigSchema_Empty(t *testing.T) {
	reg := Registry{}

	schemas := reg.ConfigSchema()
	if len(schemas) != 0 {
		t.Errorf("expected 0 schema variants for empty registry, got %d", len(schemas))
	}
}

func TestDefaultRegistry(t *testing.T) {
	reg := DefaultRegistry()
	if reg == nil {
		t.Error("expected non-nil default registry")
	}

	// DefaultRegistry returns the global registry
	// Providers register via init() from their packages
	// In isolated test, no providers may be registered yet
	// Just verify the registry is functional
	reg.Register(&mockProvider{name: "test-in-default", schema: MustSchema(struct{}{})})
	if got := reg.Get("test-in-default"); got == nil {
		t.Error("expected to retrieve provider from default registry")
	}
}
