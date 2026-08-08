package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// RunnerSpec declares the runner that executes agent workloads. Cluster-scoped:
// it names no organization, which is what RegisterRunner requires cluster admin
// for.
type RunnerSpec struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// +optional
	Capabilities []string `json:"capabilities,omitempty"`

	// TokenSecretName is the Secret the controller writes the service token
	// into, and the one the runner workload mounts. On a first install the
	// runner may start before it exists and retry until it does.
	// +kubebuilder:validation:MinLength=1
	TokenSecretName string `json:"tokenSecretName"`
}

type RunnerStatus struct {
	ObjectStatus `json:",inline"`

	// +optional
	RunnerID string `json:"runnerId,omitempty"`

	// TokenSecretRef is the Secret holding the only copy of the service token.
	// Registering returns it once, so the controller never re-registers an
	// existing runner in order to obtain a new one.
	// +optional
	TokenSecretRef string `json:"tokenSecretRef,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=plrunner
// +kubebuilder:printcolumn:name="Runner",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Credential",type=string,JSONPath=`.status.tokenSecretRef`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type Runner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RunnerSpec   `json:"spec,omitempty"`
	Status RunnerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type RunnerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Runner `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Runner{}, &RunnerList{})
}
