package controller

import (
	"context"
	"fmt"
	"slices"
	"sort"
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

	matches, err := r.findByAddress(ctx, admin.Spec.Address)
	if err != nil {
		setPending(&admin.Status.ObjectStatus, admin.Generation, fmt.Sprintf("searching for %q: %v", admin.Spec.Address, err))
		return r.save(ctx, &admin, requeue)
	}
	if len(matches) == 0 {
		setPending(&admin.Status.ObjectStatus, admin.Generation,
			fmt.Sprintf("no account for %q yet; the grant completes when they first sign in", admin.Spec.Address))
		return r.save(ctx, &admin, requeue)
	}
	// Every account, not one of them. The declaration names a person, and an
	// account is keyed on the subject its issuer asserts -- so changing issuer
	// leaves that person signing in as a new account with the old one still
	// holding the address. Picking one grants the role to whichever half the
	// roster happened to list first, which after a migration is the abandoned
	// one nobody can sign into.
	for _, identityID := range matches {
		if err := r.setClusterRole(ctx, identityID, usersv1.ClusterRole_CLUSTER_ROLE_ADMIN); err != nil {
			if permanent(err) {
				setFailed(&admin.Status.ObjectStatus, admin.Generation, provisioningv1alpha1.ReasonFailed,
					fmt.Sprintf("granting cluster admin to %s: %v", identityID, err))
				return r.save(ctx, &admin, done)
			}
			setPending(&admin.Status.ObjectStatus, admin.Generation,
				fmt.Sprintf("granting cluster admin to %s: %v", identityID, err))
			return r.save(ctx, &admin, requeue)
		}
	}

	// Recorded before the grants are reported, so a revocation covers accounts
	// granted on an earlier pass as well as the ones just granted.
	granted := union(admin.Status.IdentityIDs, matches)
	if !slices.Equal(admin.Status.IdentityIDs, granted) {
		logger.Info("granted cluster admin", "address", admin.Spec.Address, "identityIds", matches)
	}
	admin.Status.IdentityIDs = granted
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
	for _, identityID := range admin.Status.IdentityIDs {
		if err := r.setClusterRole(ctx, identityID, usersv1.ClusterRole_CLUSTER_ROLE_UNSPECIFIED); err != nil && !permanent(err) {
			return requeue()
		}
		logger.Info("revoked cluster admin", "address", admin.Spec.Address, "identityId", identityID)
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

// findByAddress resolves the address the identity provider asserts to accounts.
// There is no lookup by address, and SearchUsers matches a username prefix -- an
// address is not one, so it was rejected outright rather than simply missing the
// account -- so this pages the roster and matches exactly.
//
// Every match is returned, not the first: nothing stops two accounts asserting
// one address, and the caller has to see that rather than be handed a guess.
// Sorted so a status message does not churn with roster order.
func (r *ClusterAdminReconciler) findByAddress(ctx context.Context, address string) ([]string, error) {
	const pageSize = 100
	wanted := strings.ToLower(strings.TrimSpace(address))
	var found []string

	for pageToken := ""; ; {
		page, err := r.Platform.Users.ListUsers(ctx, connect.NewRequest(&usersv1.ListUsersRequest{
			PageSize:  pageSize,
			PageToken: pageToken,
		}))
		if err != nil {
			return nil, err
		}
		for _, user := range page.Msg.GetUsers() {
			if strings.ToLower(strings.TrimSpace(user.GetEmail())) == wanted {
				found = append(found, user.GetMeta().GetId())
			}
		}
		pageToken = page.Msg.GetNextPageToken()
		if pageToken == "" {
			sort.Strings(found)
			return found, nil
		}
	}
}

// union keeps every account this declaration has granted to, so one that drops
// off the roster mid-life is still revoked when the declaration goes away.
func union(existing, found []string) []string {
	seen := make(map[string]struct{}, len(existing)+len(found))
	var all []string
	for _, group := range [][]string{existing, found} {
		for _, identityID := range group {
			if _, ok := seen[identityID]; ok {
				continue
			}
			seen[identityID] = struct{}{}
			all = append(all, identityID)
		}
	}
	sort.Strings(all)
	return all
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
