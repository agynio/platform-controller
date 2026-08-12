package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ClusterAdminSpec names a person who administers this cluster, by the address
// their identity provider will assert.
//
// A declaration is satisfied when that account exists. Before the person's
// first sign-in there is no account to grant against, so the declaration stays
// pending and is retried — signing in completes it rather than triggering it.
// Nobody is granted the role for arriving first, and an install that names no
// administrator has none.
type ClusterAdminSpec struct {
	// +kubebuilder:validation:MinLength=3
	Address string `json:"address"`
}

type ClusterAdminStatus struct {
	ObjectStatus `json:",inline"`

	// IdentityIDs are the accounts the grant was written against, recorded so
	// every one of them can be revoked after the address stops resolving to it.
	//
	// Plural because one address can name several accounts: an account is keyed
	// on the subject its issuer asserts, so changing issuer leaves the person
	// holding a new account and the old one behind. The declaration names a
	// person, not a row, so it grants to all of them rather than guessing which
	// is current.
	// +optional
	IdentityIDs []string `json:"identityIds,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=plclusteradmin
// +kubebuilder:printcolumn:name="Address",type=string,JSONPath=`.spec.address`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Identities",type=string,JSONPath=`.status.identityIds`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type ClusterAdmin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterAdminSpec   `json:"spec,omitempty"`
	Status ClusterAdminStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ClusterAdminList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterAdmin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterAdmin{}, &ClusterAdminList{})
}
