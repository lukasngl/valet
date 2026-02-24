package framework

import (
	"context"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Provider provisions secrets from an external identity provider.
// The type parameter O is the provider's CRD type.
type Provider[O Object] interface {
	// NewObject returns a zero-value instance of the CRD type.
	NewObject() O

	// Provision creates or renews credentials.
	Provision(ctx context.Context, obj O) (*Result, error)

	// DeleteKey removes a credential by its KeyID.
	// Providers that don't support key deletion can return nil.
	DeleteKey(ctx context.Context, obj O, keyID string) error
}

// Object is the constraint for provider CRD types. Each provider's CRD struct
// must implement client.Object (for Kubernetes API operations) plus the shared
// accessors that the framework reconciler needs.
type Object interface {
	client.Object

	// GetSecretRef returns the reference to the target output Secret.
	GetSecretRef() SecretReference

	// GetStatus returns a pointer to the shared status embedded in the CRD.
	GetStatus() *ClientSecretStatus

	// Validate performs structural validation of the CRD spec.
	Validate() error
}

// Result contains the secret data and metadata returned by a provider.
type Result struct {
	// StringData contains the rendered secret data.
	StringData map[string]string

	// ValidUntil is when the credentials expire.
	ValidUntil time.Time

	// ProvisionedAt is when the credentials were provisioned.
	ProvisionedAt time.Time

	// KeyID is the identifier for the created credential.
	KeyID string
}
