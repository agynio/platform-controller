package controller

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	appsv1 "github.com/agynio/platform-controller/.gen/go/agynio/api/apps/v1"
	provisioningv1alpha1 "github.com/agynio/platform-controller/api/v1alpha1"
	"github.com/agynio/platform-controller/internal/platform"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// AppReconciler registers the apps bundled with the release and writes the
// service token each one mounts.
type AppReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Platform *platform.Client
}

// +kubebuilder:rbac:groups=platform.agyn.io,resources=apps,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=apps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *AppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var app provisioningv1alpha1.App
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Deleting an app would take everything it owns with it, so a declaration
	// that leaves the set orphans the app instead.
	if !app.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	organizationID, err := resolveOrganization(ctx, r.Client, app.Namespace, app.Spec.OrganizationRef)
	if err != nil {
		setFailed(&app.Status.ObjectStatus, app.Generation, provisioningv1alpha1.ReasonFailed, err.Error())
		return r.save(ctx, &app, requeue)
	}
	if organizationID == "" {
		setPending(&app.Status.ObjectStatus, app.Generation, fmt.Sprintf("waiting for Organization %q", app.Spec.OrganizationRef.Name))
		return r.save(ctx, &app, requeue)
	}

	visibility, err := appVisibility(app.Spec.Visibility)
	if err != nil {
		setFailed(&app.Status.ObjectStatus, app.Generation, provisioningv1alpha1.ReasonFailed, err.Error())
		return r.save(ctx, &app, done)
	}

	existing, err := r.find(ctx, organizationID, app.Spec.Slug)
	if err != nil {
		setPending(&app.Status.ObjectStatus, app.Generation, fmt.Sprintf("looking up app: %v", err))
		return r.save(ctx, &app, requeue)
	}

	if existing != nil {
		app.Status.AppID = existing.GetMeta().GetId()
		present, err := serviceTokenPresent(ctx, r.Client, app.Namespace, app.Spec.TokenSecretName)
		if err != nil {
			setPending(&app.Status.ObjectStatus, app.Generation, fmt.Sprintf("reading the service token secret: %v", err))
			return r.save(ctx, &app, requeue)
		}
		if !present {
			app.Status.TokenSecretRef = ""
			setFailed(&app.Status.ObjectStatus, app.Generation, provisioningv1alpha1.ReasonCredentialMissing,
				fmt.Sprintf("app %q is registered but secret %q holds no token; no method reissues one, so delete the app to re-register it",
					app.Spec.Slug, app.Spec.TokenSecretName))
			return r.save(ctx, &app, done)
		}
		app.Status.TokenSecretRef = app.Spec.TokenSecretName

		// The declaration is authoritative for everything that is not minted.
		if existing.GetName() != app.Spec.Name || existing.GetVisibility() != visibility {
			if _, err := r.Platform.Apps.UpdateApp(ctx, connect.NewRequest(&appsv1.UpdateAppRequest{
				Id:         existing.GetMeta().GetId(),
				Name:       &app.Spec.Name,
				Visibility: &visibility,
			})); err != nil {
				if permanent(err) {
					setFailed(&app.Status.ObjectStatus, app.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("updating app: %v", err))
					return r.save(ctx, &app, done)
				}
				setPending(&app.Status.ObjectStatus, app.Generation, fmt.Sprintf("updating app: %v", err))
				return r.save(ctx, &app, requeue)
			}
			logger.Info("corrected app", "slug", app.Spec.Slug, "appId", app.Status.AppID)
		}

		setReady(&app.Status.ObjectStatus, app.Generation)
		return r.save(ctx, &app, done)
	}

	created, err := r.Platform.Apps.CreateApp(ctx, connect.NewRequest(&appsv1.CreateAppRequest{
		OrganizationId: organizationID,
		Slug:           app.Spec.Slug,
		Name:           app.Spec.Name,
		Visibility:     visibility,
		Permissions:    app.Spec.Permissions,
	}))
	if err != nil {
		if permanent(err) {
			setFailed(&app.Status.ObjectStatus, app.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("creating app: %v", err))
			return r.save(ctx, &app, done)
		}
		setPending(&app.Status.ObjectStatus, app.Generation, fmt.Sprintf("creating app: %v", err))
		return r.save(ctx, &app, requeue)
	}

	appID := created.Msg.GetApp().GetMeta().GetId()
	// The app's own id is stored beside its token: a connector is handed it as
	// APP_ID, and CreateApp is the only place it is ever returned.
	if err := writeServiceToken(ctx, r.Client, r.Scheme, &app, app.Spec.TokenSecretName, map[string]string{
		"token":  created.Msg.GetServiceToken(),
		"app_id": appID,
	}); err != nil {
		setFailed(&app.Status.ObjectStatus, app.Generation, provisioningv1alpha1.ReasonCredentialMissing,
			fmt.Sprintf("app %q registered but its token could not be stored: %v", app.Spec.Slug, err))
		app.Status.AppID = appID
		return r.save(ctx, &app, done)
	}

	app.Status.AppID = appID
	app.Status.TokenSecretRef = app.Spec.TokenSecretName
	logger.Info("created app", "slug", app.Spec.Slug, "appId", appID)
	setReady(&app.Status.ObjectStatus, app.Generation)
	return r.save(ctx, &app, done)
}

func (r *AppReconciler) find(ctx context.Context, organizationID, slug string) (*appsv1.App, error) {
	response, err := r.Platform.Apps.GetAppBySlug(ctx, connect.NewRequest(&appsv1.GetAppBySlugRequest{
		OrganizationId: organizationID,
		Slug:           slug,
	}))
	if err != nil {
		if connect.CodeOf(err) == connect.CodeNotFound {
			return nil, nil
		}
		return nil, err
	}
	return response.Msg.GetApp(), nil
}

func appVisibility(value string) (appsv1.AppVisibility, error) {
	switch value {
	case "", "internal":
		return appsv1.AppVisibility_APP_VISIBILITY_INTERNAL, nil
	case "public":
		return appsv1.AppVisibility_APP_VISIBILITY_PUBLIC, nil
	default:
		return 0, fmt.Errorf("visibility %q: must be internal or public", value)
	}
}

func (r *AppReconciler) save(ctx context.Context, app *provisioningv1alpha1.App, result func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, app); err != nil {
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return result()
}

func (r *AppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1alpha1.App{}).
		Owns(&corev1.Secret{}).
		WatchesRawSource(source.Kind(mgr.GetCache(), client.Object(&provisioningv1alpha1.Organization{}),
			enqueueForOrganization(mgr.GetClient(),
				func() *provisioningv1alpha1.AppList { return &provisioningv1alpha1.AppList{} },
				func(list *provisioningv1alpha1.AppList) []reconcile.Request {
					requests := make([]reconcile.Request, 0, len(list.Items))
					for i := range list.Items {
						requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
					}
					return requests
				}))).
		Complete(r)
}
