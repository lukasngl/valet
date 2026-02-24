package v1alpha1

import (
	"fmt"
	"time"

	"github.com/lukasngl/valet/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// ClientSecret is a mock CRD that implements [framework.Object].
// It is used for framework and e2e tests without depending on a real provider.
// Failure modes and validity are configured directly in the spec, making test
// manifests self-describing.
type ClientSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec ClientSecretSpec `json:"spec,omitzero"`
	// +optional
	Status framework.ClientSecretStatus `json:"status,omitzero"`
}

// ClientSecretSpec defines the desired state for a mock client secret.
// Fields like ShouldFailProvision and ShouldFailDeleteKey allow per-resource
// control of failure behavior in tests.
type ClientSecretSpec struct {
	// SecretRef is the reference to the output Kubernetes Secret.
	SecretRef framework.SecretReference `json:"secretRef"`
	// SecretData is the data to include in the provisioned secret.
	SecretData map[string]string `json:"secretData,omitempty"`
	// Validity overrides the default 24h credential lifetime.
	// +optional
	Validity *metav1.Duration `json:"validity,omitempty"`
	// ShouldFailProvision causes Provision to return an error.
	ShouldFailProvision bool `json:"shouldFailProvision,omitempty"`
	// ShouldFailDeleteKey causes DeleteKey to return an error.
	ShouldFailDeleteKey bool `json:"shouldFailDeleteKey,omitempty"`
}

// GetSecretRef returns the reference to the target output Secret.
func (m *ClientSecret) GetSecretRef() framework.SecretReference {
	return m.Spec.SecretRef
}

// GetStatus returns a pointer to the shared status.
func (m *ClientSecret) GetStatus() *framework.ClientSecretStatus {
	return &m.Status
}

// Validate performs structural validation of the mock spec.
func (m *ClientSecret) Validate() error {
	if m.Spec.SecretRef.Name == "" {
		return fmt.Errorf("secretRef.name is required")
	}
	if len(m.Spec.SecretData) == 0 {
		return fmt.Errorf("secretData must contain at least one key")
	}
	return nil
}

// GetValidity returns the configured credential lifetime, defaulting to 24h.
func (m *ClientSecret) GetValidity() time.Duration {
	if m.Spec.Validity != nil {
		return m.Spec.Validity.Duration
	}
	return 24 * time.Hour
}

// DeepCopyObject implements [runtime.Object].
func (m *ClientSecret) DeepCopyObject() runtime.Object {
	cp := *m
	cp.ObjectMeta = *m.DeepCopy()
	cp.Status = m.Status.DeepCopy()
	if m.Spec.SecretData != nil {
		cp.Spec.SecretData = make(map[string]string, len(m.Spec.SecretData))
		for k, v := range m.Spec.SecretData {
			cp.Spec.SecretData[k] = v
		}
	}
	if m.Spec.Validity != nil {
		v := *m.Spec.Validity
		cp.Spec.Validity = &v
	}
	return &cp
}

// +kubebuilder:object:root=true

// ClientSecretList contains a list of mock [ClientSecret] resources.
type ClientSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClientSecret `json:"items"`
}

// DeepCopyObject implements [runtime.Object].
func (m *ClientSecretList) DeepCopyObject() runtime.Object {
	cp := *m
	if m.Items != nil {
		cp.Items = make([]ClientSecret, len(m.Items))
		for i := range m.Items {
			cp.Items[i] = *m.Items[i].DeepCopyObject().(*ClientSecret)
		}
	}
	return &cp
}
