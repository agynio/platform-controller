// Package platform reaches the platform's own API the way every other caller
// does: through the Gateway, over Connect, authenticated as the platform admin
// identity. There is no internal provisioning surface, so a resource this
// creates is indistinguishable from one an operator created.
package platform

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/agynio/platform-controller/.gen/go/agynio/api/gateway/v1/gatewayv1connect"
)

// Client holds one Connect client per Gateway surface the controller declares
// against.
type Client struct {
	Organizations gatewayv1connect.OrganizationsGatewayClient
	Images        gatewayv1connect.ImagesGatewayClient
	Apps          gatewayv1connect.AppsGatewayClient
	Runners       gatewayv1connect.RunnersGatewayClient
	Users         gatewayv1connect.UsersGatewayClient
}

// TokenSource reads the bootstrap token at call time rather than at
// construction. Rotation is replacing the Secret and restarting the Gateway,
// and the controller's own copy is remounted by the kubelet without a restart —
// so reading late is what keeps the two in step.
type TokenSource func() (string, error)

// New builds a client against baseURL, presenting the bootstrap token on every
// request.
func New(baseURL string, token TokenSource, timeout time.Duration) *Client {
	httpClient := &http.Client{Timeout: timeout}
	opts := []connect.ClientOption{connect.WithInterceptors(bearer(token))}

	return &Client{
		Organizations: gatewayv1connect.NewOrganizationsGatewayClient(httpClient, baseURL, opts...),
		Images:        gatewayv1connect.NewImagesGatewayClient(httpClient, baseURL, opts...),
		Apps:          gatewayv1connect.NewAppsGatewayClient(httpClient, baseURL, opts...),
		Runners:       gatewayv1connect.NewRunnersGatewayClient(httpClient, baseURL, opts...),
		Users:         gatewayv1connect.NewUsersGatewayClient(httpClient, baseURL, opts...),
	}
}

func bearer(token TokenSource) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			value, err := token()
			if err != nil {
				return nil, connect.NewError(connect.CodeUnauthenticated, err)
			}
			req.Header().Set("Authorization", "Bearer "+value)
			return next(ctx, req)
		}
	}
}
