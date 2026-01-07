// Package adapter provides the interface for secret provisioning adapters.
// Custom adapters can be created by implementing the Adapter interface
// and registering via Register() in an init() function.
package adapter

import (
	"context"
	"sync"
	"time"
)

// Result contains the secret data and metadata returned by an adapter.
type Result struct {
	// Data to write into the Secret's .data field.
	// Keys are determined by the adapter based on *-target annotations.
	Data map[string][]byte

	// ValidUntil is when the credentials expire.
	ValidUntil time.Time

	// ProvisionedAt is when the credentials were provisioned.
	ProvisionedAt time.Time

	// KeyID is the identifier for the created credential.
	// Used by the controller to track and cleanup expired credentials.
	// Leave empty if the adapter handles its own cleanup (e.g., Vault).
	KeyID string
}

// Adapter provisions secrets from an external provider.
type Adapter interface {
	// Type returns the adapter identifier (e.g., "azure", "aws").
	Type() string

	// Provision creates or renews credentials.
	// Annotations are pre-stripped of the provider prefix (e.g., "azure.secret-manager.ngl.cx/").
	// The adapter reads config from annotations and uses *-target annotations
	// to determine output key names.
	Provision(ctx context.Context, annotations map[string]string) (*Result, error)

	// DeleteKey removes a credential by its KeyID.
	// Called by the controller to clean up expired credentials.
	// Adapters that don't need cleanup can return nil.
	DeleteKey(ctx context.Context, annotations map[string]string, keyID string) error
}

// Global registry instance
var (
	globalRegistry = &Registry{
		adapters: make(map[string]Adapter),
	}
	registryMu sync.RWMutex
)

// Register adds an adapter to the global registry.
// This is typically called from an adapter's init() function.
func Register(adapter Adapter) {
	registryMu.Lock()
	defer registryMu.Unlock()
	globalRegistry.adapters[adapter.Type()] = adapter
}

// Get returns an adapter by type from the global registry.
func Get(adapterType string) Adapter {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return globalRegistry.adapters[adapterType]
}

// Types returns all registered adapter types.
func Types() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	types := make([]string, 0, len(globalRegistry.adapters))
	for t := range globalRegistry.adapters {
		types = append(types, t)
	}
	return types
}

// Registry holds registered adapters.
// Use the global functions Register(), Get(), and Types() instead of
// creating a Registry directly.
type Registry struct {
	adapters map[string]Adapter
}
