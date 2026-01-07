// Package adapter provides the interface for secret provisioning providers.
package adapter

import (
	"context"
	"encoding/json"
	"iter"
	"maps"
	"time"

	jschema "github.com/invopop/jsonschema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Result contains the secret data and metadata returned by a provider.
type Result struct {
	// StringData contains the rendered secret data (template already applied by provider).
	StringData map[string]string

	// ValidUntil is when the credentials expire.
	ValidUntil time.Time

	// ProvisionedAt is when the credentials were provisioned.
	ProvisionedAt time.Time

	// KeyID is the identifier for the created credential.
	// Used by the controller to track and cleanup expired credentials.
	KeyID string
}

// Schema holds both raw JSON schema (for serialization) and compiled schema (for validation).
type Schema struct {
	raw      json.RawMessage
	compiled *jsonschema.Schema
}

// MarshalJSON returns the raw JSON schema for serialization.
func (s *Schema) MarshalJSON() ([]byte, error) {
	return s.raw, nil
}

// Validate validates JSON data against the compiled schema.
func (s *Schema) Validate(data json.RawMessage) error {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	return s.compiled.Validate(v)
}

// MustSchema generates a Schema from a Go struct using jsonschema tags.
// Panics if schema generation or compilation fails.
func MustSchema(v any) *Schema {
	// Generate schema using invopop/jsonschema
	schema := jschema.Reflect(v)
	raw, err := json.Marshal(schema)
	if err != nil {
		panic("failed to marshal schema: " + err.Error())
	}

	// Unmarshal to any
	var schemaValue any
	err = json.Unmarshal(raw, &schemaValue)
	if err != nil {
		panic("failed to marshal schema: " + err.Error())
	}

	// Compile for validation using santhosh-tekuri/jsonschema
	compiler := jsonschema.NewCompiler()

	err = compiler.AddResource("schema.json", schemaValue)
	if err != nil {
		panic("failed to add schema resource: " + err.Error())
	}

	compiled, err := compiler.Compile("schema.json")
	if err != nil {
		panic("failed to compile schema: " + err.Error())
	}

	return &Schema{raw: raw, compiled: compiled}
}

// Provider provisions secrets from an external provider.
type Provider interface {
	// Type returns the provider identifier (e.g., "azure", "aws").
	Type() string

	// ConfigSchema returns the JSON Schema for this provider's config.
	ConfigSchema() *Schema

	// Validate validates the provider config.
	// This includes JSON schema validation and extended validation (e.g., template parsing).
	Validate(config json.RawMessage) error

	// Provision creates or renews credentials.
	// The provider unmarshals config into its typed struct.
	// Returns Result with rendered secret data (template applied).
	Provision(ctx context.Context, config json.RawMessage) (*Result, error)

	// DeleteKey removes a credential by its KeyID.
	// Called by the controller to clean up expired credentials.
	// Providers that don't need cleanup can return nil.
	DeleteKey(ctx context.Context, config json.RawMessage, keyID string) error
}

// Global registry
var providers = make(map[string]Provider)

// register adds a provider to the global registry.
// This is called from a provider's init() function.
func register(p Provider) {
	providers[p.Type()] = p
}

// Get returns a provider by type from the global registry.
func Get(providerType string) Provider {
	return providers[providerType]
}

// All returns an iterator over all registered providers.
// The returned iterator cannot be used to modify the registry.
func All() iter.Seq2[string, Provider] {
	return maps.All(providers)
}

// Types returns all registered provider types.
func Types() iter.Seq[string] {
	return maps.Keys(providers)
}

func ptr[T any](v T) *T {
	return &v
}
