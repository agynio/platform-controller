package controller

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Registering a runner or an app mints a service token, returned once and
// stored hashed. The controller writes it into the Secret the workload mounts,
// which is what lets both enroll on first start without an operator copying a
// credential between two installs. Because the token is returned once, its
// Secret is the only copy.

// writeServiceToken stores a freshly minted token. Owned by the declaration, so
// deleting the declaration takes the credential with it — which is exactly the
// documented recovery for a lost Secret: delete and re-declare.
func writeServiceToken(ctx context.Context, writer client.Client, scheme *runtime.Scheme, owner client.Object, name string, data map[string]string) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: owner.GetNamespace(),
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: data,
	}
	if err := controllerutil.SetControllerReference(owner, secret, scheme); err != nil {
		return err
	}
	err := writer.Create(ctx, secret)
	if apierrors.IsAlreadyExists(err) {
		// A previous pass wrote it and lost the status update. The existing
		// value is the only copy and is never overwritten with a second
		// registration's token.
		return nil
	}
	return err
}

// serviceTokenPresent reports whether the credential a registered workload
// mounts is readable. A lost Secret is not recoverable in place: the record
// exists, its token is stored only as a hash, and no method reissues one.
func serviceTokenPresent(ctx context.Context, reader client.Reader, namespace, name string) (bool, error) {
	var secret corev1.Secret
	err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &secret)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return len(secret.Data["token"]) > 0, nil
}
