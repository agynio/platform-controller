package platform

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/agynio/provisioning/.gen/go/agynio/api/ziti_management/v1/ziti_managementv1connect"
	"golang.org/x/net/http2"
)

// NewZiti reaches the Ziti Management service directly rather than through the
// Gateway. Overlay policies are configuration on the OpenZiti controller, not
// platform resources, so there is no Gateway method that carries them — and
// Ziti Management already holds the controller's credential, which is what
// keeps this from needing a second one.
//
// Internal gRPC, so h2c: no TLS, HTTP/2 negotiated by prior knowledge.
func NewZiti(target string, timeout time.Duration) ziti_managementv1connect.ZitiManagementServiceClient {
	httpClient := &http.Client{
		Timeout: timeout,
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, addr)
			},
		},
	}
	return ziti_managementv1connect.NewZitiManagementServiceClient(httpClient, target, connect.WithGRPC())
}
