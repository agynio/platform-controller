package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// OrganizationSpec declares the organization platform-provisioned resources
// live in. It is created through CreateOrganization like any other, so the
// platform admin identity that creates it holds owner on it — which is what
// lets images and apps be created inside it without any service granting an
// exception.
type OrganizationSpec struct {
	// Slug is cluster-wide unique and user-visible: it appears in image
	// references and app addresses, so it is a deliberate choice rather than an
	// implementation detail.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	Slug string `json:"slug"`

	// +optional
	Name string `json:"name,omitempty"`
}

type OrganizationStatus struct {
	ObjectStatus `json:",inline"`

	// OrganizationID is what everything created inside this organization waits
	// on, rather than on a step having run.
	// +optional
	OrganizationID string `json:"organizationId,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=plorg
// +kubebuilder:printcolumn:name="Slug",type=string,JSONPath=`.spec.slug`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Organization",type=string,JSONPath=`.status.organizationId`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Organization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OrganizationSpec   `json:"spec,omitempty"`
	Status OrganizationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type OrganizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Organization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Organization{}, &OrganizationList{})
}
