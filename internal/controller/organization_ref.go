package controller

import (
	"context"
	"fmt"

	provisioningv1alpha1 "github.com/agynio/provisioning/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// resolveOrganization reads the organization ID a resource is created inside.
//
// This is the only true dependency between declarations, and it is expressed as
// waiting on the referenced object's status rather than on a step having run.
// An empty ID with no error means "not yet".
func resolveOrganization(ctx context.Context, reader client.Reader, namespace string, ref provisioningv1alpha1.OrganizationReference) (string, error) {
	var organization provisioningv1alpha1.Organization
	err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ref.Name}, &organization)
	if apierrors.IsNotFound(err) {
		return "", fmt.Errorf("Organization %q is not declared", ref.Name)
	}
	if err != nil {
		return "", err
	}
	return organization.Status.OrganizationID, nil
}

// enqueueForOrganization wakes every object of a kind when the organization it
// waits on becomes ready, so nothing sits out a full resync period after its
// only dependency is satisfied.
func enqueueForOrganization[L client.ObjectList](reader client.Reader, list func() L, refs func(L) []reconcile.Request) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, organization client.Object) []reconcile.Request {
		items := list()
		if err := reader.List(ctx, items, client.InNamespace(organization.GetNamespace())); err != nil {
			return nil
		}
		return refs(items)
	})
}
