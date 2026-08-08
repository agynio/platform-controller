package controller

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	zitiv1 "github.com/agynio/platform-controller/.gen/go/agynio/api/ziti_management/v1"
	"github.com/agynio/platform-controller/.gen/go/agynio/api/ziti_management/v1/ziti_managementv1connect"
	provisioningv1alpha1 "github.com/agynio/platform-controller/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// managedByProvisioning marks the policies this controller owns, so an operator
// reading the overlay can tell a declared policy from one the Expose or
// EgressRules services created for a resource's lifetime.
var managedByProvisioning = map[string]string{"agyn.io/managed-by": "provisioning"}

// OverlayPolicyReconciler applies the platform's baseline OpenZiti
// connectivity. OpenZiti denies by default, so a missing policy makes a service
// invisible rather than refused — which is why these are declared with
// everything else rather than applied by hand before the platform can reach its
// own services.
type OverlayPolicyReconciler struct {
	client.Client
	Ziti ziti_managementv1connect.ZitiManagementServiceClient
}

// +kubebuilder:rbac:groups=platform.agyn.io,resources=overlaypolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=overlaypolicies/status,verbs=get;update;patch

func (r *OverlayPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var declaration provisioningv1alpha1.OverlayPolicy
	if err := r.Get(ctx, req.NamespacedName, &declaration); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Removing the declaration leaves the overlay as it is: tearing policies
	// down would cut every workload already relying on them.
	if !declaration.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	applied := make([]string, 0, len(declaration.Spec.Policies))
	var pending []string

	for _, policy := range declaration.Spec.Policies {
		policyType, err := servicePolicyType(policy.Type)
		if err != nil {
			setFailed(&declaration.Status.ObjectStatus, declaration.Generation, provisioningv1alpha1.ReasonFailed,
				fmt.Sprintf("policy %q: %v", policy.Name, err))
			return r.save(ctx, &declaration, done)
		}

		// A policy naming a service cannot be applied until that service
		// exists, which is exactly the kind of "not yet" reconciliation
		// absorbs: the rest of the set still converges.
		_, err = r.Ziti.CreateServicePolicy(ctx, connect.NewRequest(&zitiv1.CreateServicePolicyRequest{
			Name:           policy.Name,
			Type:           policyType,
			IdentityRoles:  policy.IdentityRoles,
			ServiceRoles:   policy.ServiceRoles,
			Tags:           managedByProvisioning,
			ReturnExisting: true,
		}))
		if err != nil {
			pending = append(pending, fmt.Sprintf("%s (%v)", policy.Name, err))
			continue
		}
		applied = append(applied, policy.Name)
	}

	declaration.Status.AppliedPolicies = applied

	if len(pending) > 0 {
		setPending(&declaration.Status.ObjectStatus, declaration.Generation,
			fmt.Sprintf("%d of %d policies applied; waiting on: %v", len(applied), len(declaration.Spec.Policies), pending))
		return r.save(ctx, &declaration, requeue)
	}

	logger.Info("overlay policies applied", "count", len(applied))
	setReady(&declaration.Status.ObjectStatus, declaration.Generation)
	return r.save(ctx, &declaration, done)
}

func servicePolicyType(value string) (zitiv1.ServicePolicyType, error) {
	switch value {
	case "Dial":
		return zitiv1.ServicePolicyType_SERVICE_POLICY_TYPE_DIAL, nil
	case "Bind":
		return zitiv1.ServicePolicyType_SERVICE_POLICY_TYPE_BIND, nil
	default:
		return 0, fmt.Errorf("type %q: must be Dial or Bind", value)
	}
}

func (r *OverlayPolicyReconciler) save(ctx context.Context, declaration *provisioningv1alpha1.OverlayPolicy, result func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, declaration); err != nil {
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return result()
}

func (r *OverlayPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1alpha1.OverlayPolicy{}).
		Complete(r)
}
