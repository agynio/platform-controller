package controller

import (
	"context"

	"connectrpc.com/connect"
	appsv1 "github.com/agynio/provisioning/.gen/go/agynio/api/apps/v1"
	"github.com/agynio/provisioning/.gen/go/agynio/api/gateway/v1/gatewayv1connect"
	imagesv1 "github.com/agynio/provisioning/.gen/go/agynio/api/images/v1"
	organizationsv1 "github.com/agynio/provisioning/.gen/go/agynio/api/organizations/v1"
	runnersv1 "github.com/agynio/provisioning/.gen/go/agynio/api/runners/v1"
	usersv1 "github.com/agynio/provisioning/.gen/go/agynio/api/users/v1"
	provisioningv1alpha1 "github.com/agynio/provisioning/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// The generated client interfaces carry every RPC on a surface. Embedding one
// leaves the rest nil, so a reconciler that reaches for a method these tests did
// not intend to exercise panics rather than passing quietly.

type fakeOrganizations struct {
	gatewayv1connect.OrganizationsGatewayClient
	organizations []*organizationsv1.Organization
	listErr       error
	created       []*organizationsv1.CreateOrganizationRequest
	updated       []*organizationsv1.UpdateOrganizationRequest
}

func (f *fakeOrganizations) ListOrganizations(context.Context, *connect.Request[organizationsv1.ListOrganizationsRequest]) (*connect.Response[organizationsv1.ListOrganizationsResponse], error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return connect.NewResponse(&organizationsv1.ListOrganizationsResponse{Organizations: f.organizations}), nil
}

func (f *fakeOrganizations) CreateOrganization(_ context.Context, req *connect.Request[organizationsv1.CreateOrganizationRequest]) (*connect.Response[organizationsv1.CreateOrganizationResponse], error) {
	f.created = append(f.created, req.Msg)
	organization := &organizationsv1.Organization{Id: "org-1", Slug: req.Msg.GetSlug(), Name: req.Msg.GetName()}
	f.organizations = append(f.organizations, organization)
	return connect.NewResponse(&organizationsv1.CreateOrganizationResponse{Organization: organization}), nil
}

func (f *fakeOrganizations) UpdateOrganization(_ context.Context, req *connect.Request[organizationsv1.UpdateOrganizationRequest]) (*connect.Response[organizationsv1.UpdateOrganizationResponse], error) {
	f.updated = append(f.updated, req.Msg)
	return connect.NewResponse(&organizationsv1.UpdateOrganizationResponse{}), nil
}

type fakeImages struct {
	gatewayv1connect.ImagesGatewayClient
	images  []*imagesv1.Image
	created []*imagesv1.CreateImageRequest
	updated []*imagesv1.UpdateImageRequest
}

func (f *fakeImages) ListImages(context.Context, *connect.Request[imagesv1.ListImagesRequest]) (*connect.Response[imagesv1.ListImagesResponse], error) {
	return connect.NewResponse(&imagesv1.ListImagesResponse{Images: f.images}), nil
}

func (f *fakeImages) CreateImage(_ context.Context, req *connect.Request[imagesv1.CreateImageRequest]) (*connect.Response[imagesv1.CreateImageResponse], error) {
	f.created = append(f.created, req.Msg)
	return connect.NewResponse(&imagesv1.CreateImageResponse{
		Image: &imagesv1.Image{Meta: &imagesv1.EntityMeta{Id: "image-1"}, Name: req.Msg.GetName()},
	}), nil
}

func (f *fakeImages) UpdateImage(_ context.Context, req *connect.Request[imagesv1.UpdateImageRequest]) (*connect.Response[imagesv1.UpdateImageResponse], error) {
	f.updated = append(f.updated, req.Msg)
	return connect.NewResponse(&imagesv1.UpdateImageResponse{}), nil
}

type fakeRunners struct {
	gatewayv1connect.RunnersGatewayClient
	runners    []*runnersv1.Runner
	registered []*runnersv1.RegisterRunnerRequest
	token      string
}

func (f *fakeRunners) ListRunners(context.Context, *connect.Request[runnersv1.ListRunnersRequest]) (*connect.Response[runnersv1.ListRunnersResponse], error) {
	return connect.NewResponse(&runnersv1.ListRunnersResponse{Runners: f.runners}), nil
}

func (f *fakeRunners) RegisterRunner(_ context.Context, req *connect.Request[runnersv1.RegisterRunnerRequest]) (*connect.Response[runnersv1.RegisterRunnerResponse], error) {
	f.registered = append(f.registered, req.Msg)
	runner := &runnersv1.Runner{Meta: &runnersv1.EntityMeta{Id: "runner-1"}, Name: req.Msg.GetName()}
	f.runners = append(f.runners, runner)
	return connect.NewResponse(&runnersv1.RegisterRunnerResponse{Runner: runner, ServiceToken: f.token}), nil
}

type fakeApps struct {
	gatewayv1connect.AppsGatewayClient
	apps    map[string]*appsv1.App
	created []*appsv1.CreateAppRequest
	token   string
}

func (f *fakeApps) GetAppBySlug(_ context.Context, req *connect.Request[appsv1.GetAppBySlugRequest]) (*connect.Response[appsv1.GetAppBySlugResponse], error) {
	app, ok := f.apps[req.Msg.GetSlug()]
	if !ok {
		return nil, connect.NewError(connect.CodeNotFound, context.Canceled)
	}
	return connect.NewResponse(&appsv1.GetAppBySlugResponse{App: app}), nil
}

func (f *fakeApps) CreateApp(_ context.Context, req *connect.Request[appsv1.CreateAppRequest]) (*connect.Response[appsv1.CreateAppResponse], error) {
	f.created = append(f.created, req.Msg)
	app := &appsv1.App{Meta: &appsv1.EntityMeta{Id: "app-1"}, Slug: req.Msg.GetSlug(), Name: req.Msg.GetName()}
	if f.apps == nil {
		f.apps = map[string]*appsv1.App{}
	}
	f.apps[app.GetSlug()] = app
	return connect.NewResponse(&appsv1.CreateAppResponse{App: app, ServiceToken: f.token}), nil
}

type fakeUsers struct {
	gatewayv1connect.UsersGatewayClient
	directory map[string]string // identity id -> email
	updates   []*usersv1.UpdateUserRequest
}

func (f *fakeUsers) SearchUsers(context.Context, *connect.Request[usersv1.SearchUsersRequest]) (*connect.Response[usersv1.SearchUsersResponse], error) {
	entries := make([]*usersv1.UserDirectoryEntry, 0, len(f.directory))
	for identityID := range f.directory {
		entries = append(entries, &usersv1.UserDirectoryEntry{IdentityId: identityID})
	}
	return connect.NewResponse(&usersv1.SearchUsersResponse{Users: entries}), nil
}

func (f *fakeUsers) GetUser(_ context.Context, req *connect.Request[usersv1.GetUserRequest]) (*connect.Response[usersv1.GetUserResponse], error) {
	return connect.NewResponse(&usersv1.GetUserResponse{
		User: &usersv1.User{Email: f.directory[req.Msg.GetIdentityId()]},
	}), nil
}

func (f *fakeUsers) UpdateUser(_ context.Context, req *connect.Request[usersv1.UpdateUserRequest]) (*connect.Response[usersv1.UpdateUserResponse], error) {
	f.updates = append(f.updates, req.Msg)
	return connect.NewResponse(&usersv1.UpdateUserResponse{}), nil
}

func testScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(err)
	}
	if err := provisioningv1alpha1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	return scheme
}

func newFakeClient(scheme *runtime.Scheme, objects ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(
			&provisioningv1alpha1.Organization{},
			&provisioningv1alpha1.Image{},
			&provisioningv1alpha1.Runner{},
			&provisioningv1alpha1.App{},
			&provisioningv1alpha1.ClusterAdmin{},
			&provisioningv1alpha1.OverlayPolicy{},
		).
		Build()
}

func readyCondition(status provisioningv1alpha1.ObjectStatus) (string, string) {
	for _, condition := range status.Conditions {
		if condition.Type == provisioningv1alpha1.ConditionReady {
			return string(condition.Status), condition.Reason
		}
	}
	return "", ""
}
