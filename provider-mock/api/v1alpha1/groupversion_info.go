// Package v1alpha1 contains API schema definitions for mock.valet.ngl.cx v1alpha1.
// +groupName=mock.valet.ngl.cx
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the API group and version for mock CRDs in tests.
	GroupVersion = schema.GroupVersion{Group: "mock.valet.ngl.cx", Version: "v1alpha1"}

	// SchemeBuilder is used to register mock types with a runtime.Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addTypes)

	// AddToScheme adds mock types to a runtime.Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

func addTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&ClientSecret{},
		&ClientSecretList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
