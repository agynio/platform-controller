package controller

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	imagesv1 "github.com/agynio/provisioning/.gen/go/agynio/api/images/v1"
	provisioningv1alpha1 "github.com/agynio/provisioning/api/v1alpha1"
	"github.com/agynio/provisioning/internal/platform"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ImageReconciler registers the images a release ships, public, so they are
// usable from every organization on the platform.
type ImageReconciler struct {
	client.Client
	Platform *platform.Client
}

// +kubebuilder:rbac:groups=platform.agyn.io,resources=images,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.agyn.io,resources=images/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var image provisioningv1alpha1.Image
	if err := r.Get(ctx, req.NamespacedName, &image); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	// Removed declarations orphan the image rather than deleting it.
	if !image.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	organizationID, err := resolveOrganization(ctx, r.Client, image.Namespace, image.Spec.OrganizationRef)
	if err != nil {
		setFailed(&image.Status.ObjectStatus, image.Generation, provisioningv1alpha1.ReasonFailed, err.Error())
		return r.save(ctx, &image, requeue)
	}
	if organizationID == "" {
		setPending(&image.Status.ObjectStatus, image.Generation, fmt.Sprintf("waiting for Organization %q", image.Spec.OrganizationRef.Name))
		return r.save(ctx, &image, requeue)
	}

	imageType, visibility, err := imageEnums(image.Spec)
	if err != nil {
		setFailed(&image.Status.ObjectStatus, image.Generation, provisioningv1alpha1.ReasonFailed, err.Error())
		return r.save(ctx, &image, done)
	}

	username, password, err := r.credentials(ctx, &image)
	if err != nil {
		setPending(&image.Status.ObjectStatus, image.Generation, fmt.Sprintf("reading registry credentials: %v", err))
		return r.save(ctx, &image, requeue)
	}

	existing, err := r.findByName(ctx, organizationID, image.Spec.Name)
	if err != nil {
		setPending(&image.Status.ObjectStatus, image.Generation, fmt.Sprintf("listing images: %v", err))
		return r.save(ctx, &image, requeue)
	}

	if existing == nil {
		created, err := r.Platform.Images.CreateImage(ctx, connect.NewRequest(&imagesv1.CreateImageRequest{
			OrganizationId: organizationID,
			Name:           image.Spec.Name,
			Description:    image.Spec.Description,
			Type:           imageType,
			Repository:     image.Spec.Repository,
			Username:       username,
			Password:       password,
			Visibility:     visibility,
			TagFilter:      image.Spec.TagFilter,
		}))
		if err != nil {
			if permanent(err) {
				setFailed(&image.Status.ObjectStatus, image.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("creating image: %v", err))
				return r.save(ctx, &image, done)
			}
			setPending(&image.Status.ObjectStatus, image.Generation, fmt.Sprintf("creating image: %v", err))
			return r.save(ctx, &image, requeue)
		}
		image.Status.ImageID = created.Msg.GetImage().GetMeta().GetId()
		logger.Info("created image", "name", image.Spec.Name, "imageId", image.Status.ImageID)
		setReady(&image.Status.ObjectStatus, image.Generation)
		return r.save(ctx, &image, done)
	}

	image.Status.ImageID = existing.GetMeta().GetId()

	// The declaration is authoritative, so metadata that drifted is corrected.
	// Type and repository are immutable on the platform side and are therefore
	// not compared: a declaration that changes one is a new image.
	if existing.GetDescription() != image.Spec.Description ||
		existing.GetVisibility() != visibility ||
		existing.GetTagFilter() != image.Spec.TagFilter ||
		existing.GetUsername() != username {
		update := &imagesv1.UpdateImageRequest{
			Id:          existing.GetMeta().GetId(),
			Description: &image.Spec.Description,
			Visibility:  &visibility,
			TagFilter:   &image.Spec.TagFilter,
			Username:    &username,
		}
		// A password is write-only and never returned, so it is only sent when
		// the declaration carries one — otherwise every pass would rewrite it.
		if password != "" {
			update.Password = &password
		}
		if _, err := r.Platform.Images.UpdateImage(ctx, connect.NewRequest(update)); err != nil {
			if permanent(err) {
				setFailed(&image.Status.ObjectStatus, image.Generation, provisioningv1alpha1.ReasonFailed, fmt.Sprintf("updating image: %v", err))
				return r.save(ctx, &image, done)
			}
			setPending(&image.Status.ObjectStatus, image.Generation, fmt.Sprintf("updating image: %v", err))
			return r.save(ctx, &image, requeue)
		}
		logger.Info("corrected image", "name", image.Spec.Name, "imageId", image.Status.ImageID)
	}

	setReady(&image.Status.ObjectStatus, image.Generation)
	return r.save(ctx, &image, done)
}

func (r *ImageReconciler) findByName(ctx context.Context, organizationID, name string) (*imagesv1.Image, error) {
	pageToken := ""
	for {
		response, err := r.Platform.Images.ListImages(ctx, connect.NewRequest(&imagesv1.ListImagesRequest{
			OrganizationId: organizationID,
			PageSize:       100,
			PageToken:      pageToken,
		}))
		if err != nil {
			return nil, err
		}
		for _, candidate := range response.Msg.GetImages() {
			if candidate.GetName() == name {
				return candidate, nil
			}
		}
		pageToken = response.Msg.GetNextPageToken()
		if pageToken == "" {
			return nil, nil
		}
	}
}

// credentials reads a private registry's username and password from the Secret
// the declaration names. A values file never carries the password itself.
func (r *ImageReconciler) credentials(ctx context.Context, image *provisioningv1alpha1.Image) (string, string, error) {
	if image.Spec.CredentialsSecretRef == nil {
		return "", "", nil
	}
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: image.Namespace, Name: image.Spec.CredentialsSecretRef.Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", fmt.Errorf("secret %q not found", image.Spec.CredentialsSecretRef.Name)
		}
		return "", "", err
	}
	return string(secret.Data["username"]), string(secret.Data["password"]), nil
}

func imageEnums(spec provisioningv1alpha1.ImageSpec) (imagesv1.ImageType, imagesv1.ImageVisibility, error) {
	// The declaration names types and visibilities the way the product does, so
	// an operator writes "workspace" rather than an enum constant.
	var imageType imagesv1.ImageType
	switch spec.Type {
	case "workspace":
		imageType = imagesv1.ImageType_IMAGE_TYPE_WORKSPACE
	case "agent_runtime":
		imageType = imagesv1.ImageType_IMAGE_TYPE_AGENT_RUNTIME
	case "mcp":
		imageType = imagesv1.ImageType_IMAGE_TYPE_MCP
	default:
		return 0, 0, fmt.Errorf("type %q: must be workspace, agent_runtime or mcp", spec.Type)
	}

	var visibility imagesv1.ImageVisibility
	switch spec.Visibility {
	case "", "public":
		visibility = imagesv1.ImageVisibility_IMAGE_VISIBILITY_PUBLIC
	case "internal":
		visibility = imagesv1.ImageVisibility_IMAGE_VISIBILITY_INTERNAL
	default:
		return 0, 0, fmt.Errorf("visibility %q: must be public or internal", spec.Visibility)
	}
	return imageType, visibility, nil
}

func (r *ImageReconciler) save(ctx context.Context, image *provisioningv1alpha1.Image, result func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.Status().Update(ctx, image); err != nil {
		if apierrors.IsConflict(err) {
			return requeue()
		}
		return ctrl.Result{}, err
	}
	return result()
}

func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&provisioningv1alpha1.Image{}).
		WatchesRawSource(source.Kind(mgr.GetCache(), client.Object(&provisioningv1alpha1.Organization{}),
			enqueueForOrganization(mgr.GetClient(),
				func() *provisioningv1alpha1.ImageList { return &provisioningv1alpha1.ImageList{} },
				func(list *provisioningv1alpha1.ImageList) []reconcile.Request {
					requests := make([]reconcile.Request, 0, len(list.Items))
					for i := range list.Items {
						requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&list.Items[i])})
					}
					return requests
				}))).
		Complete(r)
}
