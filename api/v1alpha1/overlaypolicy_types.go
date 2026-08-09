package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// OverlayPolicySpec declares the platform's baseline OpenZiti connectivity.
//
// The one kind not reconciled against the platform API: these are configuration
// on the OpenZiti controller rather than platform resources. They are declared
// alongside the rest because their failure mode is identical — OpenZiti denies
// by default, so a missing policy makes a service invisible rather than refused
// — and because a policy naming a service cannot be applied until that service
// exists, which is exactly the kind of "not yet" reconciliation absorbs.
type OverlayPolicySpec struct {
	// Services the platform binds and dials over the overlay. Declared here
	// with the policies because a policy naming one cannot be applied until it
	// exists, and both are overlay configuration rather than platform
	// resources.
	// +optional
	Services []OverlayService `json:"services,omitempty"`

	// +kubebuilder:validation:MinItems=1
	Policies []ServicePolicy `json:"policies"`
}

// OverlayService is a service the platform hosts on the overlay. Its intercept
// config is what tells a dialing tunneler which address to capture.
type OverlayService struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +optional
	RoleAttributes []string `json:"roleAttributes,omitempty"`

	// +optional
	Intercept *InterceptConfig `json:"intercept,omitempty"`
}

type InterceptConfig struct {
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`

	// +kubebuilder:default={"tcp"}
	// +optional
	Protocols []string `json:"protocols,omitempty"`

	// +kubebuilder:validation:MinItems=1
	PortRanges []PortRange `json:"portRanges"`
}

type PortRange struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Low int32 `json:"low"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	High int32 `json:"high"`
}

// ServicePolicy is written against role attributes rather than named services,
// so a rule covers every runner and app registered later and nothing has to be
// reapplied when one is.
type ServicePolicy struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// +kubebuilder:validation:Enum=Dial;Bind
	Type string `json:"type"`

	// +kubebuilder:validation:MinItems=1
	IdentityRoles []string `json:"identityRoles"`

	// +kubebuilder:validation:MinItems=1
	ServiceRoles []string `json:"serviceRoles"`
}

type OverlayPolicyStatus struct {
	ObjectStatus `json:",inline"`

	// AppliedServices names the services that exist on the overlay.
	// +optional
	AppliedServices []string `json:"appliedServices,omitempty"`

	// AppliedPolicies names the policies that exist on the overlay, so a
	// declaration blocked on one service still reports the rest as done.
	// +optional
	AppliedPolicies []string `json:"appliedPolicies,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ploverlay
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Applied",type=integer,JSONPath=`.status.appliedPolicies[*]`,priority=1
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type OverlayPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OverlayPolicySpec   `json:"spec,omitempty"`
	Status OverlayPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type OverlayPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OverlayPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OverlayPolicy{}, &OverlayPolicyList{})
}
