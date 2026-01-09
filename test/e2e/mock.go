// Package e2e provides end-to-end testing infrastructure.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lukasngl/secret-manager/internal/adapter"
)

// MockConfig is the configuration for the mock provider.
type MockConfig struct {
	// ShouldFail causes Provision to return an error.
	ShouldFail bool `json:"shouldFail,omitempty"`

	// FailureMessage is the error message when ShouldFail is true.
	FailureMessage string `json:"failureMessage,omitempty"`

	// Delay adds a delay before returning from Provision.
	Delay string `json:"delay,omitempty"`

	// SecretData is the data to return in the provisioned secret.
	SecretData map[string]string `json:"secretData" jsonschema:"required"`

	// Validity is how long the secret is valid for (default: 24h).
	Validity string `json:"validity,omitempty"`
}

// MockProvider is a test provider that returns configurable responses.
type MockProvider struct {
	schema *adapter.Schema

	// ProvisionCalls tracks all calls to Provision for assertions.
	ProvisionCalls []MockConfig

	// DeleteKeyCalls tracks all calls to DeleteKey for assertions.
	DeleteKeyCalls []string
}

// NewMockProvider creates a new mock provider with the default schema.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		schema: adapter.MustSchema(MockConfig{}),
	}
}

// WithSchema allows overriding the schema for testing schema validation.
func (m *MockProvider) WithSchema(schema *adapter.Schema) *MockProvider {
	m.schema = schema
	return m
}

// Type returns "mock".
func (m *MockProvider) Type() string {
	return "mock"
}

// ConfigSchema returns the JSON schema for MockConfig.
func (m *MockProvider) ConfigSchema() *adapter.Schema {
	return m.schema
}

// Validate validates the config against the schema.
func (m *MockProvider) Validate(config json.RawMessage) error {
	return m.schema.Validate(config)
}

// Provision returns secret data based on the config.
func (m *MockProvider) Provision(ctx context.Context, config json.RawMessage) (*adapter.Result, error) {
	var cfg MockConfig
	if err := json.Unmarshal(config, &cfg); err != nil {
		return nil, err
	}

	m.ProvisionCalls = append(m.ProvisionCalls, cfg)

	if cfg.Delay != "" {
		d, err := time.ParseDuration(cfg.Delay)
		if err != nil {
			return nil, err
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if cfg.ShouldFail {
		msg := cfg.FailureMessage
		if msg == "" {
			msg = "mock provider failure"
		}
		return nil, errors.New(msg)
	}

	validity := 24 * time.Hour
	if cfg.Validity != "" {
		var err error
		validity, err = time.ParseDuration(cfg.Validity)
		if err != nil {
			return nil, err
		}
	}

	now := time.Now()
	return &adapter.Result{
		StringData:    cfg.SecretData,
		ProvisionedAt: now,
		ValidUntil:    now.Add(validity),
		KeyID:         uuid.New().String(),
	}, nil
}

// DeleteKey records the deletion and returns nil.
func (m *MockProvider) DeleteKey(_ context.Context, _ json.RawMessage, keyID string) error {
	m.DeleteKeyCalls = append(m.DeleteKeyCalls, keyID)
	return nil
}

// Reset clears the recorded calls.
func (m *MockProvider) Reset() {
	m.ProvisionCalls = nil
	m.DeleteKeyCalls = nil
}
