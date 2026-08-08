package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ImageSpec declares an image the release ships. The declaration is
// authoritative: a tag filter or description changed here is corrected on the
// next pass, which is what lets a release change an image it shipped earlier.
type ImageSpec struct {
	OrganizationRef OrganizationReference `json:"organizationRef"`

	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +optional
	Description string `json:"description,omitempty"`

	// +kubebuilder:validation:Enum=workspace;agent_runtime;mcp
	Type string `json:"type"`

	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`

	// Visibility public is what makes an image usable from every organization
	// on the platform.
	// +kubebuilder:validation:Enum=public;internal
	// +kubebuilder:default=public
	// +optional
	Visibility string `json:"visibility,omitempty"`

	// +optional
	TagFilter string `json:"tagFilter,omitempty"`

	// CredentialsSecretRef names a Secret holding `username` and `password` for
	// a private registry. Never a literal: a values file that carries a registry
	// password renders it into the release.
	// +optional
	CredentialsSecretRef *corev1.LocalObjectReference `json:"credentialsSecretRef,omitempty"`
}

type ImageStatus struct {
	ObjectStatus `json:",inline"`

	// +optional
	ImageID string `json:"imageId,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=plimage
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=`.spec.repository`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Image struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageSpec   `json:"spec,omitempty"`
	Status ImageStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ImageList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Image `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Image{}, &ImageList{})
}
