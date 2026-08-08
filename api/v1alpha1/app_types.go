package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// AppSpec declares an app bundled with the release.
//
// The slug is not cosmetic: CreateApp names the app's OpenZiti service
// `app-<slug>`, and that is the name the app binds on.
type AppSpec struct {
	OrganizationRef OrganizationReference `json:"organizationRef"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:Pattern=`^[a-z0-9][a-z0-9-]*$`
	Slug string `json:"slug"`

	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:validation:Enum=internal;public
	// +kubebuilder:default=internal
	// +optional
	Visibility string `json:"visibility,omitempty"`

	// +optional
	Permissions []string `json:"permissions,omitempty"`

	// TokenSecretName is the Secret the controller writes `token` and `app_id`
	// into, and the one the app workload mounts.
	// +kubebuilder:validation:MinLength=1
	TokenSecretName string `json:"tokenSecretName"`
}

type AppStatus struct {
	ObjectStatus `json:",inline"`

	// +optional
	AppID string `json:"appId,omitempty"`

	// +optional
	TokenSecretRef string `json:"tokenSecretRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=plapp
// +kubebuilder:printcolumn:name="Slug",type=string,JSONPath=`.spec.slug`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Credential",type=string,JSONPath=`.status.tokenSecretRef`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type App struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppSpec   `json:"spec,omitempty"`
	Status AppStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type AppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []App `json:"items"`
}

func init() {
	SchemeBuilder.Register(&App{}, &AppList{})
}
