// Package framework provides the shared reconciler and types for client-secret-operator providers.
package framework

import (
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Finalizer is applied to all managed CRDs to ensure key cleanup on deletion.
	Finalizer = "cso.ngl.cx/finalizer"

	// RenewalThreshold is the maximum time before expiry to trigger renewal.
	// For keys with shorter validity, a dynamic threshold of 10% of the
	// validity period is used instead.
	RenewalThreshold = 7 * 24 * time.Hour

	// ConditionReady is the condition type indicating whether credentials
	// are provisioned and up to date.
	ConditionReady = "Ready"

	// PhasePending indicates the resource has been created but not yet reconciled.
	PhasePending = "Pending"
	// PhaseReady indicates credentials are provisioned and the output secret is up to date.
	PhaseReady = "Ready"
	// PhaseFailed indicates the last reconciliation attempt failed.
	PhaseFailed = "Failed"
)

// SecretReference contains the reference to the target Secret.
type SecretReference struct {
	// Name of the secret to create/update.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ActiveKey represents a provisioned credential key tracked by the operator.
type ActiveKey struct {
	// KeyID is the provider-specific identifier for this key.
	KeyID string `json:"keyId"`
	// CreatedAt is when this key was provisioned.
	CreatedAt metav1.Time `json:"createdAt"`
	// ExpiresAt is when this key will expire.
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// NearExpiry reports whether the key is expired or within its renewal window.
// The renewal window is the smaller of 10% of the key's validity period and
// [RenewalThreshold].
func (k *ActiveKey) NearExpiry() bool {
	now := time.Now()
	if k.ExpiresAt.Time.Before(now) {
		return true
	}
	validity := k.ExpiresAt.Sub(k.CreatedAt.Time)
	threshold := min(validity/10, RenewalThreshold)
	return time.Until(k.ExpiresAt.Time) < threshold
}

// ActiveKeys is a list of provisioned credential keys.
type ActiveKeys []ActiveKey

// Newest returns the most recently created key, or nil if the list is empty.
func (keys ActiveKeys) Newest() *ActiveKey {
	var newest *ActiveKey
	for i := range keys {
		if newest == nil || keys[i].CreatedAt.After(newest.CreatedAt.Time) {
			newest = &keys[i]
		}
	}
	return newest
}

// DropExpired removes expired keys in place and returns the dropped ones.
// The keep callback is invoked for each expired key — return true to retain it
// (e.g. when provider deletion fails), false to drop it. The backing array is
// reused to avoid allocations.
func (keys *ActiveKeys) DropExpired(now time.Time, keep func(ActiveKey) bool) []ActiveKey {
	idx := 0
	var dropped []ActiveKey
	for _, k := range *keys {
		if !k.ExpiresAt.Time.Before(now) || keep(k) {
			(*keys)[idx] = k
			idx++
		} else {
			dropped = append(dropped, k)
		}
	}
	*keys = (*keys)[:idx]
	return dropped
}

// DeepCopy returns a deep copy of the keys.
func (keys ActiveKeys) DeepCopy() ActiveKeys {
	if keys == nil {
		return nil
	}
	cp := make(ActiveKeys, len(keys))
	copy(cp, keys)
	return cp
}

// ClientSecretStatus defines the observed state shared by all provider CRDs.
// It is embedded in each provider's CRD status and managed by the framework
// reconciler via the [Object] interface.
type ClientSecretStatus struct {
	// ObservedGeneration is the generation of the spec that was last processed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase represents the current lifecycle phase.
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	Phase string `json:"phase,omitempty"`

	// CurrentKeyID is the identifier of the active credential.
	CurrentKeyID string `json:"currentKeyId,omitempty"`

	// ActiveKeys lists all non-expired credentials.
	// +optional
	ActiveKeys ActiveKeys `json:"activeKeys,omitempty"`

	// FailureCount tracks consecutive failures for observability.
	FailureCount int `json:"failureCount,omitempty"`

	// LastFailure is the timestamp of the last failure.
	// +optional
	LastFailure *metav1.Time `json:"lastFailure,omitempty"`

	// LastFailureMessage contains the error from the last failure.
	// +optional
	LastFailureMessage string `json:"lastFailureMessage,omitempty"`

	// Conditions represent the latest available observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NeedsRenewal reports whether credentials need to be provisioned or renewed.
// It returns true when there are no active keys, the spec generation changed,
// the output secret is missing or empty, or the newest key is near expiry.
func (s *ClientSecretStatus) NeedsRenewal(currentGeneration int64, secretHasData bool) bool {
	if len(s.ActiveKeys) == 0 {
		return true
	}
	if s.ObservedGeneration != currentGeneration {
		return true
	}
	if !secretHasData {
		return true
	}
	newest := s.ActiveKeys.Newest()
	if newest == nil {
		return true
	}
	return newest.NearExpiry()
}

// RenewalDuration returns how long to wait before the next renewal check.
// Returns 0 when there are no active keys, signaling an immediate requeue.
func (s *ClientSecretStatus) RenewalDuration() time.Duration {
	newest := s.ActiveKeys.Newest()
	if newest == nil {
		return 0
	}
	validity := newest.ExpiresAt.Sub(newest.CreatedAt.Time)
	threshold := min(validity/10, RenewalThreshold)
	d := time.Until(newest.ExpiresAt.Time) - threshold
	return max(d, time.Minute)
}

// SetReady transitions the status to Ready after successful provisioning.
// It clears failure counters, appends the new key to ActiveKeys, and sets
// the Ready condition to true.
func (s *ClientSecretStatus) SetReady(generation int64, result *Result) {
	s.Phase = PhaseReady
	s.ObservedGeneration = generation
	s.CurrentKeyID = result.KeyID
	s.FailureCount = 0
	s.LastFailure = nil
	s.LastFailureMessage = ""

	if result.KeyID != "" {
		s.ActiveKeys = append(s.ActiveKeys, ActiveKey{
			KeyID:     result.KeyID,
			CreatedAt: metav1.NewTime(result.ProvisionedAt),
			ExpiresAt: metav1.NewTime(result.ValidUntil),
		})
	}

	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "Credentials provisioned successfully",
		ObservedGeneration: generation,
	})
}

// SetFailed transitions the status to Failed. It increments the failure
// counter, records the error, and sets the Ready condition to false.
func (s *ClientSecretStatus) SetFailed(generation int64, err error) {
	s.Phase = PhaseFailed
	s.FailureCount++
	now := metav1.Now()
	s.LastFailure = &now
	s.LastFailureMessage = err.Error()

	meta.SetStatusCondition(&s.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ProvisioningFailed",
		Message:            err.Error(),
		ObservedGeneration: generation,
	})
}

// DeepCopy returns a deep copy of the status.
func (s *ClientSecretStatus) DeepCopy() ClientSecretStatus {
	out := *s
	out.ActiveKeys = s.ActiveKeys.DeepCopy()
	if s.LastFailure != nil {
		t := *s.LastFailure
		out.LastFailure = &t
	}
	if s.Conditions != nil {
		out.Conditions = make([]metav1.Condition, len(s.Conditions))
		copy(out.Conditions, s.Conditions)
	}
	return out
}
