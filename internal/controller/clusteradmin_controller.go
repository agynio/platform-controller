package controller

import (
	"context"
	"fmt"
	"strings"

	"connectrpc.com/connect"
	usersv1 "github.com/agynio/platform-controller/.gen/go/agynio/api/users/v1"
	provisioningv1alpha1 "github.com/agynio/platform-controller/api/v1alpha1"
	"github.com/agynio/platform-controller/internal/platform"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// revokeFinalizer is why this kind alone has one. Every other declaration
// orphans its resource on removal, because deleting an app would take
// everything it owns with it. A cluster admin grant is the exception: an
// unrevokable grant is a hole rather than a resource.
const revokeFinalizer = "platform.agyn.io/revoke-cluster-admin"

// ClusterAdminReconciler grants the cluster admin role to the accounts the
// install names.
//
// A declaration is satisfied when that account exists. Before the person's
// first sign-in there is no account to grant against, so it stays pending and
// is retried — signing in completes it rather than triggering it.
type ClusterAdminReconciler struct {
	client.Client
	Platform *platform.Client
}

// +kubebuilder:rbac:groups=platform.agyn.io,resources=clusteradmins,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=clusteradmins/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=clusteradmins/finalizers,verbs=update

func (r *ClusterAdminReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var admin provisioningv1alpha1.ClusterAdmin
	if err := r.Get(ctx, req.NamespacedName, &admin); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !admin.DeletionTimestamp.IsZero() {
		return r.revoke(ctx, &admin)
	}

	if !controllerutil.ContainsFinalizer(&admin, revokeFinalizer) {
		controllerutil.AddFinalizer(&admin, revokeFinalizer)
		if err := r.Update(ctx, &admin); err != nil {
			if apierrors.IsConflict(err) {
				return requeue()
			}
			return ctrl.Result{}, err
		}
	}

	identityID, err := r.findByAddress(ctx, admin.Spec.Address)
	if err != nil {
		setPending(&admin.Status.ObjectStatus, admin.Generation, fmt.Sprintf("searching for %q: %v", admin.Spec.Address, err))
		return r.save(ctx, &admin, requeue)
	}
	if identityID == "" {
		setPending(&admin.Status.ObjectStatus, admin.Generation,
			fmt.Sprintf("no account for %q yet; the grant completes when they first sign in", admin.Spec.Address))
		return r.save(ctx, &admin, requeue)
	}

	if err := r.setClusterRole(ctx, identityID, usersv1.ClusterRole_CLUSTER_ROLE_ADMIN); err != nil {
		if permanent(err) {
			setFailed(&admin.Status.ObjectStatus, admin.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("granting cluster admin: %v", err))
			return r.save(ctx, &admin, done)
		}
		setPending(&admin.Status.ObjectStatus, admin.Generation, fmt.Sprintf("granting cluster admin: %v", err))
		return r.save(ctx, &admin, requeue)
	}

	if admin.Status.IdentityID != identityID {
		logger.Info("granted cluster admin", "address", admin.Spec.Address, "identityId", identityID)
	}
	admin.Status.IdentityID = identityID
	setReady(&admin.Status.ObjectStatus, admin.Generation)
	return r.save(ctx, &admin, done)
}

// revoke removes the grant before the declaration goes away. Retried until it
// lands: dropping the finalizer while the tuple is still written would leave a
// cluster admin nobody declared.
func (r *ClusterAdminReconciler) revoke(ctx context.Context, admin *provisioningv1alpha1.ClusterAdmin) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(admin, revokeFinalizer) {
		return ctrl.Result{}, nil
	}
	if admin.Status.IdentityID != "" {
		if err := r.setClusterRole(ctx, admin.Status.IdentityID, usersv1.ClusterRole_CLUSTER_ROLE_UNSPECIFIED); err != nil && !permanent(err) {
			return requeue()
		}
		logger.Info("revoked cluster admin", "address", admin.Spec.Address, "identityId", admin.Status.IdentityID)
	}

	controllerutil.RemoveFinalizer(admin, revokeFinalizer)
	if err := r.Update(ctx, admin); err != nil {
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// findByAddress resolves the address the identity provider asserts to an
// account. SearchUsers narrows by prefix but does not return an address, so
// each candidate is read back and matched exactly — a prefix match is not a
// person.
func (r *ClusterAdminReconciler) findByAddress(ctx context.Context, address string) (string, error) {
	candidates, err := r.Platform.Users.SearchUsers(ctx, connect.NewRequest(&usersv1.SearchUsersRequest{
		Prefix: address,
		Limit:  20,
	}))
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates.Msg.GetUsers() {
		user, err := r.Platform.Users.GetUser(ctx, connect.NewRequest(&usersv1.GetUserRequest{
			IdentityId: candidate.GetIdentityId(),
		}))
		if err != nil {
			return "", err
		}
		if strings.EqualFold(strings.TrimSpace(user.Msg.GetUser().GetEmail()), strings.TrimSpace(address)) {
			return candidate.GetIdentityId(), nil
		}
	}
	return "", nil
}

func (r *ClusterAdminReconciler) setClusterRole(ctx context.Context, identityID string, role usersv1.ClusterRole) error {
	_, err := r.Platform.Users.UpdateUser(ctx, connect.NewRequest(&usersv1.UpdateUserRequest{
		IdentityId:  identityID,
		ClusterRole: &role,
	}))
	return err
}

func (r *ClusterAdminReconciler) save(ctx context.Context, admin *provisioningv1alpha1.ClusterAdmin, result func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, admin); err != nil {
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return result()
}

func (r *ClusterAdminReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1alpha1.ClusterAdmin{}).
		Complete(r)
}
