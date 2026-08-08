package controller

import (
	"context"
	"testing"

	organizationsv1 "github.com/agynio/provisioning/.gen/go/agynio/api/organizations/v1"
	runnersv1 "github.com/agynio/provisioning/.gen/go/agynio/api/runners/v1"
	provisioningv1alpha1 "github.com/agynio/provisioning/api/v1alpha1"
	"github.com/agynio/provisioning/internal/platform"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const namespace = "platform"

func request(name string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Namespace: namespace, Name: name}}
}

func TestOrganizationIsCreatedWhenAbsent(t *testing.T) {
	scheme := testScheme()
	declaration := &provisioningv1alpha1.Organization{
		ObjectMeta: metav1.ObjectMeta{Name: "system", Namespace: namespace},
		Spec:       provisioningv1alpha1.OrganizationSpec{Slug: "agyn-platform", Name: "Agyn Platform"},
	}
	organizations := &fakeOrganizations{}
	k8s := newFakeClient(scheme, declaration)
	reconciler := &OrganizationReconciler{Client: k8s, Platform: &platform.Client{Organizations: organizations}}

	if _, err := reconciler.Reconcile(context.Background(), request("system")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(organizations.created) != 1 || organizations.created[0].GetSlug() != "agyn-platform" {
		t.Fatalf("expected the organization to be created, got %+v", organizations.created)
	}
	var updated provisioningv1alpha1.Organization
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(declaration), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.OrganizationID != "org-1" {
		t.Fatalf("expected the organization id on status, got %q", updated.Status.OrganizationID)
	}
	if status, reason := readyCondition(updated.Status.ObjectStatus); status != "True" || reason != provisioningv1alpha1.ReasonReconciled {
		t.Fatalf("expected Ready=True/Reconciled, got %s/%s", status, reason)
	}
}

// The declaration is authoritative: a name changed against the API is corrected
// rather than accepted, which is what a create-if-absent scheme cannot do.
func TestOrganizationNameDriftIsCorrected(t *testing.T) {
	scheme := testScheme()
	declaration := &provisioningv1alpha1.Organization{
		ObjectMeta: metav1.ObjectMeta{Name: "system", Namespace: namespace},
		Spec:       provisioningv1alpha1.OrganizationSpec{Slug: "agyn-platform", Name: "Agyn Platform"},
	}
	organizations := &fakeOrganizations{organizations: []*organizationsv1.Organization{
		{Id: "org-1", Slug: "agyn-platform", Name: "Renamed By Hand"},
	}}
	reconciler := &OrganizationReconciler{
		Client:   newFakeClient(scheme, declaration),
		Platform: &platform.Client{Organizations: organizations},
	}

	if _, err := reconciler.Reconcile(context.Background(), request("system")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(organizations.created) != 0 {
		t.Fatalf("expected no second organization, got %+v", organizations.created)
	}
	if len(organizations.updated) != 1 || organizations.updated[0].GetName() != "Agyn Platform" {
		t.Fatalf("expected the name to be corrected, got %+v", organizations.updated)
	}
}

// Objects are independent, and the organization is the only true dependency.
// An image whose organization is not ready reports why rather than failing.
func TestImageWaitsForItsOrganization(t *testing.T) {
	scheme := testScheme()
	organization := &provisioningv1alpha1.Organization{
		ObjectMeta: metav1.ObjectMeta{Name: "system", Namespace: namespace},
		Spec:       provisioningv1alpha1.OrganizationSpec{Slug: "agyn-platform"},
	}
	image := &provisioningv1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "devcontainer", Namespace: namespace},
		Spec: provisioningv1alpha1.ImageSpec{
			OrganizationRef: provisioningv1alpha1.OrganizationReference{Name: "system"},
			Name:            "devcontainer",
			Type:            "workspace",
			Repository:      "ghcr.io/agynio/devcontainer",
		},
	}
	images := &fakeImages{}
	k8s := newFakeClient(scheme, organization, image)
	reconciler := &ImageReconciler{Client: k8s, Platform: &platform.Client{Images: images}}

	result, err := reconciler.Reconcile(context.Background(), request("devcontainer"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.RequeueAfter == 0 {
		t.Fatal("expected a requeue while the organization is not ready")
	}
	if len(images.created) != 0 {
		t.Fatalf("expected no image before the organization exists, got %+v", images.created)
	}

	var updated provisioningv1alpha1.Image
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(image), &updated); err != nil {
		t.Fatal(err)
	}
	if status, reason := readyCondition(updated.Status.ObjectStatus); status != "False" || reason != provisioningv1alpha1.ReasonPending {
		t.Fatalf("expected Ready=False/Pending, got %s/%s", status, reason)
	}
}

func TestImageIsCreatedOnceTheOrganizationIsReady(t *testing.T) {
	scheme := testScheme()
	organization := &provisioningv1alpha1.Organization{
		ObjectMeta: metav1.ObjectMeta{Name: "system", Namespace: namespace},
		Spec:       provisioningv1alpha1.OrganizationSpec{Slug: "agyn-platform"},
		Status:     provisioningv1alpha1.OrganizationStatus{OrganizationID: "org-1"},
	}
	image := &provisioningv1alpha1.Image{
		ObjectMeta: metav1.ObjectMeta{Name: "devcontainer", Namespace: namespace},
		Spec: provisioningv1alpha1.ImageSpec{
			OrganizationRef: provisioningv1alpha1.OrganizationReference{Name: "system"},
			Name:            "devcontainer",
			Type:            "workspace",
			Repository:      "ghcr.io/agynio/devcontainer",
			Visibility:      "public",
		},
	}
	images := &fakeImages{}
	reconciler := &ImageReconciler{
		Client:   newFakeClient(scheme, organization, image),
		Platform: &platform.Client{Images: images},
	}

	if _, err := reconciler.Reconcile(context.Background(), request("devcontainer")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(images.created) != 1 || images.created[0].GetOrganizationId() != "org-1" {
		t.Fatalf("expected the image created in org-1, got %+v", images.created)
	}
}

func TestRunnerRegistersAndStoresItsServiceToken(t *testing.T) {
	scheme := testScheme()
	runner := &provisioningv1alpha1.Runner{
		ObjectMeta: metav1.ObjectMeta{Name: "k8s-runner", Namespace: namespace},
		Spec: provisioningv1alpha1.RunnerSpec{
			Name:            "k8s-runner",
			TokenSecretName: "k8s-runner-service-token",
			Capabilities:    []string{"docker"},
		},
	}
	runners := &fakeRunners{token: "service-token-value"}
	k8s := newFakeClient(scheme, runner)
	reconciler := &RunnerReconciler{Client: k8s, Scheme: scheme, Platform: &platform.Client{Runners: runners}}

	if _, err := reconciler.Reconcile(context.Background(), request("k8s-runner")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var secret corev1.Secret
	if err := k8s.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: "k8s-runner-service-token"}, &secret); err != nil {
		t.Fatalf("expected the service token secret to be written: %v", err)
	}
	// StringData rather than Data: the API server folds one into the other on
	// write, and the fake client does not.
	if secret.StringData["token"] != "service-token-value" {
		t.Fatalf("unexpected token stored")
	}
	if len(secret.OwnerReferences) != 1 {
		t.Fatalf("expected the secret owned by its declaration, got %+v", secret.OwnerReferences)
	}
}

// The token is returned once, so a runner whose record exists is never
// re-registered to obtain a new one. The object says the credential is missing
// instead of silently converging.
func TestRunnerWithALostSecretReportsRatherThanReRegistering(t *testing.T) {
	scheme := testScheme()
	runner := &provisioningv1alpha1.Runner{
		ObjectMeta: metav1.ObjectMeta{Name: "k8s-runner", Namespace: namespace},
		Spec:       provisioningv1alpha1.RunnerSpec{Name: "k8s-runner", TokenSecretName: "k8s-runner-service-token"},
	}
	runners := &fakeRunners{runners: []*runnersv1.Runner{
		{Meta: &runnersv1.EntityMeta{Id: "runner-1"}, Name: "k8s-runner"},
	}}
	k8s := newFakeClient(scheme, runner)
	reconciler := &RunnerReconciler{Client: k8s, Scheme: scheme, Platform: &platform.Client{Runners: runners}}

	if _, err := reconciler.Reconcile(context.Background(), request("k8s-runner")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(runners.registered) != 0 {
		t.Fatalf("expected no re-registration, got %+v", runners.registered)
	}
	var updated provisioningv1alpha1.Runner
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(runner), &updated); err != nil {
		t.Fatal(err)
	}
	if status, reason := readyCondition(updated.Status.ObjectStatus); status != "False" || reason != provisioningv1alpha1.ReasonCredentialMissing {
		t.Fatalf("expected Ready=False/CredentialMissing, got %s/%s", status, reason)
	}
}

// An install that names an administrator who has never signed in has no account
// to grant against. Signing in completes the declaration; it does not trigger it.
func TestClusterAdminStaysPendingUntilTheAccountExists(t *testing.T) {
	scheme := testScheme()
	admin := &provisioningv1alpha1.ClusterAdmin{
		ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: namespace},
		Spec:       provisioningv1alpha1.ClusterAdminSpec{Address: "operator@example.com"},
	}
	users := &fakeUsers{directory: map[string]string{}}
	k8s := newFakeClient(scheme, admin)
	reconciler := &ClusterAdminReconciler{Client: k8s, Platform: &platform.Client{Users: users}}

	if _, err := reconciler.Reconcile(context.Background(), request("operator")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users.updates) != 0 {
		t.Fatalf("expected no grant, got %+v", users.updates)
	}

	var updated provisioningv1alpha1.ClusterAdmin
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(admin), &updated); err != nil {
		t.Fatal(err)
	}
	if status, reason := readyCondition(updated.Status.ObjectStatus); status != "False" || reason != provisioningv1alpha1.ReasonPending {
		t.Fatalf("expected Ready=False/Pending, got %s/%s", status, reason)
	}
}

func TestClusterAdminGrantsTheNamedAccountOnly(t *testing.T) {
	scheme := testScheme()
	admin := &provisioningv1alpha1.ClusterAdmin{
		ObjectMeta: metav1.ObjectMeta{Name: "operator", Namespace: namespace},
		Spec:       provisioningv1alpha1.ClusterAdminSpec{Address: "operator@example.com"},
	}
	// A prefix match is not a person: SearchUsers returns both, and only the
	// exact address may be granted.
	users := &fakeUsers{directory: map[string]string{
		"identity-1": "operator@example.com",
		"identity-2": "operator@example.com.attacker.test",
	}}
	k8s := newFakeClient(scheme, admin)
	reconciler := &ClusterAdminReconciler{Client: k8s, Platform: &platform.Client{Users: users}}

	if _, err := reconciler.Reconcile(context.Background(), request("operator")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users.updates) != 1 || users.updates[0].GetIdentityId() != "identity-1" {
		t.Fatalf("expected exactly the named account to be granted, got %+v", users.updates)
	}

	var updated provisioningv1alpha1.ClusterAdmin
	if err := k8s.Get(context.Background(), client.ObjectKeyFromObject(admin), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Status.IdentityID != "identity-1" {
		t.Fatalf("expected the granted identity on status, got %q", updated.Status.IdentityID)
	}
}

// Removing a declared cluster administrator revokes the role — the one kind
// whose removal is not an orphan, because an unrevokable grant is a hole.
func TestClusterAdminRevokesOnRemoval(t *testing.T) {
	scheme := testScheme()
	now := metav1.Now()
	admin := &provisioningv1alpha1.ClusterAdmin{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "operator",
			Namespace:         namespace,
			DeletionTimestamp: &now,
			Finalizers:        []string{revokeFinalizer},
		},
		Spec:   provisioningv1alpha1.ClusterAdminSpec{Address: "operator@example.com"},
		Status: provisioningv1alpha1.ClusterAdminStatus{IdentityID: "identity-1"},
	}
	users := &fakeUsers{directory: map[string]string{"identity-1": "operator@example.com"}}
	reconciler := &ClusterAdminReconciler{
		Client:   newFakeClient(scheme, admin),
		Platform: &platform.Client{Users: users},
	}

	if _, err := reconciler.Reconcile(context.Background(), request("operator")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users.updates) != 1 {
		t.Fatalf("expected one revocation, got %+v", users.updates)
	}
	if users.updates[0].GetClusterRole() != 0 {
		t.Fatalf("expected the role cleared, got %v", users.updates[0].GetClusterRole())
	}
}
