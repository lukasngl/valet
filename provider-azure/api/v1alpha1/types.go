package v1alpha1

import (
	"fmt"
	"text/template"

	"github.com/lukasngl/client-secret-operator/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	SchemeBuilder.Register(&AzureClientSecret{}, &AzureClientSecretList{})
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=acs
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// AzureClientSecret provisions and rotates client secrets for Azure AD applications.
type AzureClientSecret struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	Spec   AzureClientSecretSpec        `json:"spec,omitzero"`
	Status framework.ClientSecretStatus `json:"status,omitzero"`
}

// AzureClientSecretSpec defines the desired state.
type AzureClientSecretSpec struct {
	// SecretRef is the Kubernetes Secret to create/update with the provisioned credentials.
	SecretRef framework.SecretReference `json:"secretRef"`

	// ObjectID is the Azure AD application Object ID.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	ObjectID string `json:"objectId"`

	// Validity is how long each provisioned credential should be valid.
	// Defaults to 90 days (2160h).
	// +optional
	Validity *metav1.Duration `json:"validity,omitempty"`

	// Template maps output secret keys to Go template strings.
	// Available template variables: .ClientID, .ClientSecret
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinProperties=1
	Template map[string]string `json:"template"`
}

// GetSecretRef returns the reference to the target output Secret.
func (a *AzureClientSecret) GetSecretRef() framework.SecretReference {
	return a.Spec.SecretRef
}

// GetStatus returns a pointer to the shared status.
func (a *AzureClientSecret) GetStatus() *framework.ClientSecretStatus {
	return &a.Status
}

// DeepCopyObject implements [runtime.Object].
func (a *AzureClientSecret) DeepCopyObject() runtime.Object {
	cp := *a
	cp.ObjectMeta = *a.ObjectMeta.DeepCopy()
	cp.Status = a.Status.DeepCopy()
	if a.Spec.Template != nil {
		cp.Spec.Template = make(map[string]string, len(a.Spec.Template))
		for k, v := range a.Spec.Template {
			cp.Spec.Template[k] = v
		}
	}
	if a.Spec.Validity != nil {
		v := *a.Spec.Validity
		cp.Spec.Validity = &v
	}
	return &cp
}

// Validate performs structural validation of the spec.
func (a *AzureClientSecret) Validate() error {
	if a.Spec.SecretRef.Name == "" {
		return fmt.Errorf("secretRef.name is required")
	}
	if a.Spec.ObjectID == "" {
		return fmt.Errorf("objectId is required")
	}
	if len(a.Spec.Template) == 0 {
		return fmt.Errorf("template must have at least one entry")
	}
	for key, tmpl := range a.Spec.Template {
		if _, err := template.New(key).Parse(tmpl); err != nil {
			return fmt.Errorf("template %q: %w", key, err)
		}
	}
	return nil
}

// +kubebuilder:object:root=true

// AzureClientSecretList contains a list of AzureClientSecret resources.
type AzureClientSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureClientSecret `json:"items"`
}

// DeepCopyObject implements [runtime.Object].
func (a *AzureClientSecretList) DeepCopyObject() runtime.Object {
	cp := *a
	if a.Items != nil {
		cp.Items = make([]AzureClientSecret, len(a.Items))
		for i := range a.Items {
			cp.Items[i] = *a.Items[i].DeepCopyObject().(*AzureClientSecret)
		}
	}
	return &cp
}
