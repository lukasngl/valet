// Package v1alpha1 contains API schema definitions for cso.ngl.cx v1alpha1.
// +groupName=cso.ngl.cx
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the API group and version for AzureClientSecret.
	GroupVersion = schema.GroupVersion{Group: "cso.ngl.cx", Version: "v1alpha1"}

	// SchemeBuilder is used to register types with a runtime.Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
