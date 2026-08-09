package controller

import (
	"context"
	"fmt"
	"strings"

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

	// Services first: a policy naming one is refused until it exists, so
	// creating them in the same pass is what lets a fresh overlay converge
	// without a second round.
	appliedServices, servicesPending := r.ensureServices(ctx, declaration.Spec.Services)
	declaration.Status.AppliedServices = appliedServices

	applied := make([]string, 0, len(declaration.Spec.Policies))
	pending := servicesPending

	// A service role naming one service is written "@name", but the overlay
	// stores policies against ids and rejects a name it is handed as one. The
	// ziti CLI resolved this client-side; nothing does on this path, so the
	// names are resolved here before the policy is created. Role attributes
	// ("#attribute") are not names and pass through untouched.
	resolve := r.serviceNameResolver(ctx)

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
		resolvedRoles, err := resolve(policy.ServiceRoles)
		if err != nil {
			pending = append(pending, fmt.Sprintf("%s (%v)", policy.Name, err))
			continue
		}

		_, err = r.Ziti.CreateServicePolicy(ctx, connect.NewRequest(&zitiv1.CreateServicePolicyRequest{
			Name:           policy.Name,
			Type:           policyType,
			IdentityRoles:  policy.IdentityRoles,
			ServiceRoles:   resolvedRoles,
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
			fmt.Sprintf("%d of %d services and %d of %d policies applied; waiting on: %v",
				len(appliedServices), len(declaration.Spec.Services), len(applied), len(declaration.Spec.Policies), pending))
		return r.save(ctx, &declaration, requeue)
	}

	logger.Info("overlay configuration applied", "services", len(appliedServices), "policies", len(applied))
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

// serviceNameResolver turns "@name" service roles into "@id".
//
// OpenZiti stores a policy against service ids and rejects a name supplied as
// one. The ziti CLI resolved names before calling the API; this path had
// nothing doing that, so every policy naming a single service -- the Gateway,
// the LLM Proxy, Tracing, the vendor intercepts -- was refused while the ones
// written against role attributes went through.
//
// Lookups are cached for the pass: the same handful of services is named by
// several policies, and a service that does not exist yet is a "not yet" for
// every policy naming it.
func (r *OverlayPolicyReconciler) serviceNameResolver(ctx context.Context) func([]string) ([]string, error) {
	cache := map[string]string{}
	return func(roles []string) ([]string, error) {
		resolved := make([]string, 0, len(roles))
		for _, role := range roles {
			name, isServiceName := strings.CutPrefix(role, "@")
			if !isServiceName {
				// A role attribute matches by attribute, not identity.
				resolved = append(resolved, role)
				continue
			}
			id, ok := cache[name]
			if !ok {
				response, err := r.Ziti.ListServices(ctx, connect.NewRequest(&zitiv1.ListServicesRequest{
					Name:     name,
					PageSize: 2,
				}))
				if err != nil {
					return nil, fmt.Errorf("resolve service %q: %w", name, err)
				}
				services := response.Msg.GetServices()
				if len(services) == 0 {
					return nil, fmt.Errorf("service %q does not exist yet", name)
				}
				id = services[0].GetZitiServiceId()
				cache[name] = id
			}
			resolved = append(resolved, "@"+id)
		}
		return resolved, nil
	}
}

// ensureServices creates the services the platform hosts on the overlay.
//
// Create-if-absent through return_existing rather than compared and corrected:
// a service carries live terminators from whatever is bound to it, and
// recreating one to change a role attribute would cut every connection through
// it. A service that needs different configuration is a different service.
func (r *OverlayPolicyReconciler) ensureServices(ctx context.Context, services []provisioningv1alpha1.OverlayService) ([]string, []string) {
	applied := make([]string, 0, len(services))
	var pending []string

	for _, service := range services {
		request := &zitiv1.CreateServiceRequest{
			Name:           service.Name,
			RoleAttributes: service.RoleAttributes,
			Tags:           managedByProvisioning,
			ReturnExisting: true,
		}
		if service.Intercept != nil {
			request.InterceptV1Config = &zitiv1.InterceptV1Config{
				Protocols:  service.Intercept.Protocols,
				Addresses:  service.Intercept.Addresses,
				PortRanges: portRanges(service.Intercept.PortRanges),
			}
		}
		if _, err := r.Ziti.CreateService(ctx, connect.NewRequest(request)); err != nil {
			pending = append(pending, fmt.Sprintf("service %s (%v)", service.Name, err))
			continue
		}
		applied = append(applied, service.Name)
	}
	return applied, pending
}

func portRanges(ranges []provisioningv1alpha1.PortRange) []*zitiv1.PortRange {
	converted := make([]*zitiv1.PortRange, 0, len(ranges))
	for _, portRange := range ranges {
		converted = append(converted, &zitiv1.PortRange{Low: portRange.Low, High: portRange.High})
	}
	return converted
}
