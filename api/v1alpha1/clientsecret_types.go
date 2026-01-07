package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClientSecretSpec defines the desired state of ClientSecret
type ClientSecretSpec struct {
	// Provider specifies which provider to use (e.g., "azure", "aws")
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider"`

	// Config contains provider-specific configuration.
	// Each provider defines its own schema and validates at runtime.
	// +kubebuilder:validation:Required
	Config apiextensionsv1.JSON `json:"config"`

	// SecretRef specifies the target Secret to create/update
	// +kubebuilder:validation:Required
	SecretRef SecretReference `json:"secretRef"`
}

// SecretReference contains the reference to the target Secret
type SecretReference struct {
	// Name of the secret to create/update
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ActiveKey represents a provisioned credential key
type ActiveKey struct {
	// KeyID is the provider-specific identifier for this key
	KeyID string `json:"keyId"`
	// CreatedAt is when this key was provisioned
	CreatedAt metav1.Time `json:"createdAt"`
	// ExpiresAt is when this key will expire
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// ClientSecretStatus defines the observed state of ClientSecret
type ClientSecretStatus struct {
	// ObservedGeneration is the generation of the spec that was last processed.
	// Used to detect spec changes that require reprovisioning.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Phase represents the current lifecycle phase
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	Phase string `json:"phase,omitempty"`

	// CurrentKeyId is the identifier of the active credential
	CurrentKeyId string `json:"currentKeyId,omitempty"`

	// ActiveKeys lists all non-expired credentials
	// +optional
	ActiveKeys []ActiveKey `json:"activeKeys,omitempty"`

	// FailureCount tracks consecutive failures for observability
	FailureCount int `json:"failureCount,omitempty"`

	// LastFailure is the timestamp of the last failure
	// +optional
	LastFailure *metav1.Time `json:"lastFailure,omitempty"`

	// LastFailureMessage contains the error from the last failure
	// +optional
	LastFailureMessage string `json:"lastFailureMessage,omitempty"`

	// Conditions represent the latest available observations
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cs
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=`.spec.provider`
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// ClientSecret is the Schema for the clientsecrets API
type ClientSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClientSecretSpec   `json:"spec,omitempty"`
	Status ClientSecretStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClientSecretList contains a list of ClientSecret
type ClientSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClientSecret `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClientSecret{}, &ClientSecretList{})
}
