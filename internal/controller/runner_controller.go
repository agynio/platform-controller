package controller

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	runnersv1 "github.com/agynio/provisioning/.gen/go/agynio/api/runners/v1"
	provisioningv1alpha1 "github.com/agynio/provisioning/api/v1alpha1"
	"github.com/agynio/provisioning/internal/platform"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// RunnerReconciler registers the runner that executes agent workloads and
// writes the service token it mounts.
type RunnerReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Platform *platform.Client
}

// +kubebuilder:rbac:groups=platform.agyn.io,resources=runners,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=runners/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create

func (r *RunnerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var runner provisioningv1alpha1.Runner
	if err := r.Get(ctx, req.NamespacedName, &runner); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// A runner that leaves the declared set is left registered.
	if !runner.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	existing, err := r.findByName(ctx, runner.Spec.Name)
	if err != nil {
		setPending(&runner.Status.ObjectStatus, runner.Generation, fmt.Sprintf("listing runners: %v", err))
		return r.save(ctx, &runner, requeue)
	}

	if existing != nil {
		runner.Status.RunnerID = existing.GetMeta().GetId()
		present, err := serviceTokenPresent(ctx, r.Client, runner.Namespace, runner.Spec.TokenSecretName)
		if err != nil {
			setPending(&runner.Status.ObjectStatus, runner.Generation, fmt.Sprintf("reading the service token secret: %v", err))
			return r.save(ctx, &runner, requeue)
		}
		if !present {
			// Registering again would mint a second token and orphan the first.
			// Say so instead of converging silently; recovery is deleting the
			// runner so this declaration recreates it.
			runner.Status.TokenSecretRef = ""
			setFailed(&runner.Status.ObjectStatus, runner.Generation, provisioningv1alpha1.ReasonCredentialMissing,
				fmt.Sprintf("runner %q is registered but secret %q holds no token; no method reissues one, so delete the runner to re-register it",
					runner.Spec.Name, runner.Spec.TokenSecretName))
			return r.save(ctx, &runner, done)
		}
		runner.Status.TokenSecretRef = runner.Spec.TokenSecretName
		setReady(&runner.Status.ObjectStatus, runner.Generation)
		return r.save(ctx, &runner, done)
	}

	// Cluster-scoped: no organization, which is what makes this require cluster
	// admin rather than organization ownership.
	registered, err := r.Platform.Runners.RegisterRunner(ctx, connect.NewRequest(&runnersv1.RegisterRunnerRequest{
		Name:         runner.Spec.Name,
		Labels:       runner.Spec.Labels,
		Capabilities: runner.Spec.Capabilities,
	}))
	if err != nil {
		if permanent(err) {
			setFailed(&runner.Status.ObjectStatus, runner.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("registering runner: %v", err))
			return r.save(ctx, &runner, done)
		}
		setPending(&runner.Status.ObjectStatus, runner.Generation, fmt.Sprintf("registering runner: %v", err))
		return r.save(ctx, &runner, requeue)
	}

	if err := writeServiceToken(ctx, r.Client, r.Scheme, &runner, runner.Spec.TokenSecretName, map[string]string{
		"token": registered.Msg.GetServiceToken(),
	}); err != nil {
		// The token is already spent and this is its only copy, so the failure
		// is reported rather than retried into a second registration.
		setFailed(&runner.Status.ObjectStatus, runner.Generation, provisioningv1alpha1.ReasonCredentialMissing,
			fmt.Sprintf("runner %q registered but its token could not be stored: %v", runner.Spec.Name, err))
		runner.Status.RunnerID = registered.Msg.GetRunner().GetMeta().GetId()
		return r.save(ctx, &runner, done)
	}

	runner.Status.RunnerID = registered.Msg.GetRunner().GetMeta().GetId()
	runner.Status.TokenSecretRef = runner.Spec.TokenSecretName
	logger.Info("registered runner", "name", runner.Spec.Name, "runnerId", runner.Status.RunnerID)
	setReady(&runner.Status.ObjectStatus, runner.Generation)
	return r.save(ctx, &runner, done)
}

// findByName looks the runner up rather than trusting status alone, so a
// controller that lost its status update does not register a second one.
func (r *RunnerReconciler) findByName(ctx context.Context, name string) (*runnersv1.Runner, error) {
	pageToken := ""
	for {
		response, err := r.Platform.Runners.ListRunners(ctx, connect.NewRequest(&runnersv1.ListRunnersRequest{
			PageSize:  100,
			PageToken: pageToken,
		}))
		if err != nil {
			return nil, err
		}
		for _, candidate := range response.Msg.GetRunners() {
			if candidate.GetName() == name && candidate.GetOrganizationId() == "" {
				return candidate, nil
			}
		}
		pageToken = response.Msg.GetNextPageToken()
		if pageToken == "" {
			return nil, nil
		}
	}
}

func (r *RunnerReconciler) save(ctx context.Context, runner *provisioningv1alpha1.Runner, result func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, runner); err != nil {
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return result()
}

func (r *RunnerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1alpha1.Runner{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
