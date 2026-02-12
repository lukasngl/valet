// Package mock provides test doubles for the framework.
package mock

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lukasngl/valet/framework"
)

// Provider implements [framework.Provider] for [*ClientSecret].
// It tracks calls for test assertions. Failure behavior is controlled
// per-resource via the CRD spec fields.
type Provider struct {
	// ProvisionCount is the number of times Provision has been called.
	ProvisionCount int
	// DeleteKeyCalls records the key IDs passed to DeleteKey.
	DeleteKeyCalls []string
}

// NewProvider returns a new mock provider with no recorded calls.
func NewProvider() *Provider {
	return &Provider{}
}

// NewObject returns a zero-value [ClientSecret].
func (p *Provider) NewObject() *ClientSecret {
	return &ClientSecret{}
}

// Provision returns credentials based on the CRD spec. If
// ShouldFailProvision is set, it returns an error. The credential
// lifetime is controlled by the Validity spec field.
func (p *Provider) Provision(_ context.Context, obj *ClientSecret) (*framework.Result, error) {
	p.ProvisionCount++

	if obj.Spec.ShouldFailProvision {
		return nil, errors.New("mock provider failure")
	}

	now := time.Now()
	return &framework.Result{
		StringData:    obj.Spec.SecretData,
		ProvisionedAt: now,
		ValidUntil:    now.Add(obj.GetValidity()),
		KeyID:         uuid.New().String(),
	}, nil
}

// DeleteKey records the key ID. If ShouldFailDeleteKey is set on the
// CRD spec, it returns an error.
func (p *Provider) DeleteKey(_ context.Context, obj *ClientSecret, keyID string) error {
	p.DeleteKeyCalls = append(p.DeleteKeyCalls, keyID)

	if obj.Spec.ShouldFailDeleteKey {
		return errors.New("mock delete key failure")
	}
	return nil
}

// Reset clears all recorded calls.
func (p *Provider) Reset() {
	p.ProvisionCount = 0
	p.DeleteKeyCalls = nil
}
