package controller

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	zitiv1 "github.com/agynio/platform-controller/.gen/go/agynio/api/ziti_management/v1"
	"github.com/agynio/platform-controller/.gen/go/agynio/api/ziti_management/v1/ziti_managementv1connect"
	provisioningv1alpha1 "github.com/agynio/platform-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeZiti struct {
	ziti_managementv1connect.ZitiManagementServiceClient
	servicesByName map[string]string
	created        []*zitiv1.CreateServicePolicyRequest
}

func (f *fakeZiti) ListServices(_ context.Context, req *connect.Request[zitiv1.ListServicesRequest]) (*connect.Response[zitiv1.ListServicesResponse], error) {
	id, ok := f.servicesByName[req.Msg.GetName()]
	if !ok {
		return connect.NewResponse(&zitiv1.ListServicesResponse{}), nil
	}
	return connect.NewResponse(&zitiv1.ListServicesResponse{
		Services: []*zitiv1.OpenZitiService{{ZitiServiceId: id, Name: req.Msg.GetName()}},
	}), nil
}

func (f *fakeZiti) CreateServicePolicy(_ context.Context, req *connect.Request[zitiv1.CreateServicePolicyRequest]) (*connect.Response[zitiv1.CreateServicePolicyResponse], error) {
	f.created = append(f.created, req.Msg)
	return connect.NewResponse(&zitiv1.CreateServicePolicyResponse{ZitiServicePolicyId: "policy-1"}), nil
}

func overlayDeclaration(policies ...provisioningv1alpha1.ServicePolicy) *provisioningv1alpha1.OverlayPolicy {
	return &provisioningv1alpha1.OverlayPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "baseline", Namespace: namespace},
		Spec:       provisioningv1alpha1.OverlayPolicySpec{Policies: policies},
	}
}

// OpenZiti stores a policy against service ids and rejects a name handed to it
// as one, so "@name" has to become "@id" first. Role attributes are not names.
func TestServiceNamesResolveToIdsAndAttributesDoNot(t *testing.T) {
	scheme := testScheme()
	declaration := overlayDeclaration(
		provisioningv1alpha1.ServicePolicy{
			Name: "gateway-bind", Type: "Bind",
			IdentityRoles: []string{"#gateway-hosts"},
			ServiceRoles:  []string{"@gateway"},
		},
		provisioningv1alpha1.ServicePolicy{
			Name: "runners-bind", Type: "Bind",
			IdentityRoles: []string{"#runners"},
			ServiceRoles:  []string{"#runner-services"},
		},
	)
	ziti := &fakeZiti{servicesByName: map[string]string{"gateway": "6trg1Z665eGCgMfpUAeRlF"}}
	k8s := newFakeClient(scheme, declaration)
	reconciler := &OverlayPolicyReconciler{Client: k8s, Ziti: ziti}

	if _, err := reconciler.Reconcile(context.Background(), request("baseline")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(ziti.created) != 2 {
		t.Fatalf("expected both policies created, got %d", len(ziti.created))
	}
	if got := ziti.created[0].GetServiceRoles()[0]; got != "@6trg1Z665eGCgMfpUAeRlF" {
		t.Fatalf("expected the name resolved to an id, got %q", got)
	}
	if got := ziti.created[1].GetServiceRoles()[0]; got != "#runner-services" {
		t.Fatalf("expected the role attribute untouched, got %q", got)
	}

	var updated provisioningv1alpha1.OverlayPolicy
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(declaration), &updated); err != nil {
		t.Fatal(err)
	}
	if status, _ := readyCondition(updated.Status.ObjectStatus); status != "True" {
		t.Fatalf("expected Ready=True, got %s", status)
	}
}

// A policy naming a service that does not exist yet is a "not yet" for that
// policy alone -- the rest of the set still converges.
func TestAMissingServiceHoldsBackOnlyItsOwnPolicy(t *testing.T) {
	scheme := testScheme()
	declaration := overlayDeclaration(
		provisioningv1alpha1.ServicePolicy{
			Name: "agents-dial-tracing", Type: "Dial",
			IdentityRoles: []string{"#agents"},
			ServiceRoles:  []string{"@tracing"},
		},
		provisioningv1alpha1.ServicePolicy{
			Name: "apps-bind", Type: "Bind",
			IdentityRoles: []string{"#apps"},
			ServiceRoles:  []string{"#app-services"},
		},
	)
	ziti := &fakeZiti{servicesByName: map[string]string{}}
	k8s := newFakeClient(scheme, declaration)
	reconciler := &OverlayPolicyReconciler{Client: k8s, Ziti: ziti}

	result, err := reconciler.Reconcile(context.Background(), request("baseline"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected a requeue while a service is missing")
	}
	if len(ziti.created) != 1 || ziti.created[0].GetName() != "apps-bind" {
		t.Fatalf("expected only the attribute-based policy applied, got %+v", ziti.created)
	}

	var updated provisioningv1alpha1.OverlayPolicy
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(declaration), &updated); err != nil {
		t.Fatal(err)
	}
	if len(updated.Status.AppliedPolicies) != 1 {
		t.Fatalf("expected one applied policy recorded, got %v", updated.Status.AppliedPolicies)
	}
}
