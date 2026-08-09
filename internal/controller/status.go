package controller

import (
	"time"

	"connectrpc.com/connect"
	provisioningv1alpha1 "github.com/agynio/platform-controller/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// A failed call is a retry, not an error. On a fresh install almost every
// failure means "not yet" — a service still starting, a schema still migrating,
// an overlay identity not yet enrolled — so an object is requeued rather than
// reported as broken.
const (
	retryInterval = 15 * time.Second
	// Drift is corrected continuously rather than only when a release moves, so
	// every object is re-examined on this period even when nothing changed.
	resyncInterval = 10 * time.Minute
)

// requeue is the answer to anything that is not yet true.
func requeue() (reconcile.Result, error) {
	return reconcile.Result{RequeueAfter: retryInterval}, nil
}

// done schedules the next drift check.
func done() (reconcile.Result, error) {
	return reconcile.Result{RequeueAfter: resyncInterval}, nil
}

func setReady(status *provisioningv1alpha1.ObjectStatus, generation int64) {
	setCondition(status, generation, metav1.ConditionTrue, provisioningv1alpha1.ReasonReconciled, "Reconciled against the platform API")
}

func setPending(status *provisioningv1alpha1.ObjectStatus, generation int64, message string) {
	setCondition(status, generation, metav1.ConditionFalse, provisioningv1alpha1.ReasonPending, message)
}

func setFailed(status *provisioningv1alpha1.ObjectStatus, generation int64, reason, message string) {
	setCondition(status, generation, metav1.ConditionFalse, reason, message)
}

func setCondition(status *provisioningv1alpha1.ObjectStatus, generation int64, value metav1.ConditionStatus, reason, message string) {
	status.ObservedGeneration = generation
	condition := metav1.Condition{
		Type:               provisioningv1alpha1.ConditionReady,
		Status:             value,
		Reason:             reason,
		Message:            truncate(message, 32768),
		ObservedGeneration: generation,
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type != condition.Type {
			continue
		}
		// LastTransitionTime only moves when the status does, so a condition
		// that has been false for an hour says so.
		if status.Conditions[i].Status == condition.Status {
			condition.LastTransitionTime = status.Conditions[i].LastTransitionTime
		} else {
			condition.LastTransitionTime = metav1.Now()
		}
		status.Conditions[i] = condition
		return
	}
	condition.LastTransitionTime = metav1.Now()
	status.Conditions = append(status.Conditions, condition)
}

// permanent reports whether the platform refused the declaration itself, rather
// than being unable to answer yet. Retrying an invalid slug forever would hide
// it behind a Pending that never resolves.
//
// PermissionDenied is deliberately absent: this controller's own cluster admin
// grant converges asynchronously at Identity's startup, so a denial in the first
// seconds means "not yet", not "never". Treating it as permanent stranded every
// declaration reconciled before that grant landed.
func permanent(err error) bool {
	switch connect.CodeOf(err) {
	case connect.CodeInvalidArgument, connect.CodeAlreadyExists:
		return true
	default:
		return false
	}
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
