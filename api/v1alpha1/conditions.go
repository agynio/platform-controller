package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// State is reported, not logged. A platform that installed without provisioning
// is visible as objects that are not Ready, rather than as a job whose logs have
// to be read.
const (
	// ConditionReady is true once the declaration matches what the platform holds.
	ConditionReady = "Ready"

	// ReasonReconciled is the only reason Ready is true.
	ReasonReconciled = "Reconciled"
	// ReasonPending covers everything that means "not yet" — a service still
	// starting, a schema still migrating, an account that has not signed in.
	ReasonPending = "Pending"
	// ReasonFailed covers a declaration the platform refused, which retrying
	// alone will not fix.
	ReasonFailed = "Failed"
	// ReasonCredentialMissing marks a runner or app whose record exists but
	// whose service token Secret does not. The token is returned once, so this
	// is not recoverable in place: delete the declaration to recreate it.
	ReasonCredentialMissing = "CredentialMissing"
)

// ObjectStatus is the part of every kind's status that reports progress. What
// has been created is recorded per object, so nothing is inferred from whether
// a previous run finished.
type ObjectStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the spec generation this status describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// OrganizationReference names the Organization a resource is created inside.
type OrganizationReference struct {
	// Name of the Organization object in this namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
