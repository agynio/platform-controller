package controller

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	organizationsv1 "github.com/agynio/platform-controller/.gen/go/agynio/api/organizations/v1"
	provisioningv1alpha1 "github.com/agynio/platform-controller/api/v1alpha1"
	"github.com/agynio/platform-controller/internal/platform"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// OrganizationReconciler creates the organization platform-provisioned
// resources live in, through CreateOrganization like any other.
type OrganizationReconciler struct {
	client.Client
	Platform *platform.Client
}

// +kubebuilder:rbac:groups=platform.agyn.io,resources=organizations,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=organizations/status,verbs=get;update;patch

func (r *OrganizationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var organization provisioningv1alpha1.Organization
	if err := r.Get(ctx, req.NamespacedName, &organization); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Removing a declaration is not a request to destroy data: an organization
	// that leaves the declared set is left in place. Nothing to do on delete,
	// so there is no finalizer.
	if !organization.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	existing, err := r.findBySlug(ctx, organization.Spec.Slug)
	if err != nil {
		setPending(&organization.Status.ObjectStatus, organization.Generation, fmt.Sprintf("listing organizations: %v", err))
		return r.save(ctx, &organization, requeue)
	}

	if existing == nil {
		created, err := r.Platform.Organizations.CreateOrganization(ctx, connect.NewRequest(&organizationsv1.CreateOrganizationRequest{
			Name: r.displayName(&organization),
			Slug: organization.Spec.Slug,
		}))
		if err != nil {
			if permanent(err) {
				setFailed(&organization.Status.ObjectStatus, organization.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("creating organization: %v", err))
				return r.save(ctx, &organization, done)
			}
			setPending(&organization.Status.ObjectStatus, organization.Generation, fmt.Sprintf("creating organization: %v", err))
			return r.save(ctx, &organization, requeue)
		}
		existing = created.Msg.GetOrganization()
		logger.Info("created organization", "slug", organization.Spec.Slug, "organizationId", existing.GetId())
	} else if existing.GetName() != r.displayName(&organization) {
		// The declaration is authoritative: a name changed by hand is corrected
		// on the next pass, which is what lets a release change a resource it
		// shipped earlier.
		name := r.displayName(&organization)
		if _, err := r.Platform.Organizations.UpdateOrganization(ctx, connect.NewRequest(&organizationsv1.UpdateOrganizationRequest{
			Id:   existing.GetId(),
			Name: &name,
		})); err != nil {
			setPending(&organization.Status.ObjectStatus, organization.Generation, fmt.Sprintf("updating organization: %v", err))
			return r.save(ctx, &organization, requeue)
		}
		logger.Info("corrected organization name", "slug", organization.Spec.Slug)
	}

	organization.Status.OrganizationID = existing.GetId()
	setReady(&organization.Status.ObjectStatus, organization.Generation)
	return r.save(ctx, &organization, done)
}

// findBySlug pages the organizations this identity can see. GetOrganizationBySlug
// is internal and deliberately not on the Gateway, so the slug is matched here.
func (r *OrganizationReconciler) findBySlug(ctx context.Context, slug string) (*organizationsv1.Organization, error) {
	pageToken := ""
	for {
		response, err := r.Platform.Organizations.ListOrganizations(ctx, connect.NewRequest(&organizationsv1.ListOrganizationsRequest{
			PageSize:  100,
			PageToken: pageToken,
		}))
		if err != nil {
			return nil, err
		}
		for _, candidate := range response.Msg.GetOrganizations() {
			if candidate.GetSlug() == slug {
				return candidate, nil
			}
		}
		pageToken = response.Msg.GetNextPageToken()
		if pageToken == "" {
			return nil, nil
		}
	}
}

func (r *OrganizationReconciler) displayName(organization *provisioningv1alpha1.Organization) string {
	if organization.Spec.Name != "" {
		return organization.Spec.Name
	}
	return organization.Spec.Slug
}

func (r *OrganizationReconciler) save(ctx context.Context, organization *provisioningv1alpha1.Organization, result func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, organization); err != nil {
		// A conflict means someone else wrote first; the next pass reads it.
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return result()
}

func (r *OrganizationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1alpha1.Organization{}).
		Complete(r)
}
