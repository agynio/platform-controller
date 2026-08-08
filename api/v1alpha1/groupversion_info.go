// Package v1alpha1 declares what a release provisions: the resources a freshly
// installed platform must contain before anyone can use it.
//
// Each kind is reconciled against the ordinary platform API by the controller,
// authenticating as the platform admin identity. There is no separate
// provisioning surface — a provisioned resource is indistinguishable from one
// an operator created, because it was created the same way.
// +kubebuilder:object:generate=true
// +groupName=platform.agyn.io
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion = schema.GroupVersion{Group: "platform.agyn.io", Version: "v1alpha1"}

	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	AddToScheme = SchemeBuilder.AddToScheme
)
