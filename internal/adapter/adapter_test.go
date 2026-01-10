package adapter

import (
	"encoding/json"
	"testing"
)

type testConfig struct {
	Name     string `json:"name" jsonschema:"required"`
	Value    int    `json:"value,omitempty"`
	Optional string `json:"optional,omitempty"`
}

func TestMustSchema(t *testing.T) {
	schema := MustSchema(testConfig{})

	if schema == nil {
		t.Fatal("expected non-nil schema")
	}

	if schema.raw == nil {
		t.Error("expected non-nil raw schema")
	}

	if schema.compiled == nil {
		t.Error("expected non-nil compiled schema")
	}
}

func TestMustSchema_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid type")
		}
	}()

	// This should panic because a function cannot be converted to JSON schema
	MustSchema(func() {})
}

func TestSchema_MarshalJSON(t *testing.T) {
	schema := MustSchema(testConfig{})

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}

	// Verify it's valid JSON
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("marshaled schema is not valid JSON: %v", err)
	}

	// Schema uses $defs with $ref structure (JSON Schema draft 2020-12)
	defs, ok := decoded["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("expected $defs in schema, got: %+v", decoded)
	}

	// Find the testConfig definition
	testConfigDef, ok := defs["testConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected testConfig in $defs")
	}

	props, ok := testConfigDef["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in testConfig")
	}

	if _, ok := props["name"]; !ok {
		t.Error("expected 'name' property in schema")
	}

	if _, ok := props["value"]; !ok {
		t.Error("expected 'value' property in schema")
	}
}

func TestSchema_Validate_Valid(t *testing.T) {
	schema := MustSchema(testConfig{})

	validConfig := json.RawMessage(`{"name": "test", "value": 42}`)
	if err := schema.Validate(validConfig); err != nil {
		t.Errorf("expected valid config to pass validation: %v", err)
	}
}

func TestSchema_Validate_MissingRequired(t *testing.T) {
	schema := MustSchema(testConfig{})

	// Missing required 'name' field
	invalidConfig := json.RawMessage(`{"value": 42}`)
	err := schema.Validate(invalidConfig)
	if err == nil {
		t.Error("expected validation error for missing required field")
	}
}

func TestSchema_Validate_InvalidType(t *testing.T) {
	schema := MustSchema(testConfig{})

	// Wrong type for 'value' field
	invalidConfig := json.RawMessage(`{"name": "test", "value": "not-a-number"}`)
	err := schema.Validate(invalidConfig)
	if err == nil {
		t.Error("expected validation error for invalid type")
	}
}

func TestSchema_Validate_InvalidJSON(t *testing.T) {
	schema := MustSchema(testConfig{})

	invalidJSON := json.RawMessage(`{invalid json}`)
	err := schema.Validate(invalidJSON)
	if err == nil {
		t.Error("expected validation error for invalid JSON")
	}
}
