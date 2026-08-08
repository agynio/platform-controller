// Command provisioning reconciles what a release declares the platform should
// contain against the platform's own API.
//
// It does not probe for readiness and does not wait for a signal. Every
// precondition provisioning has becomes true at a moment no chart can predict,
// so nothing is sequenced and anything that is not yet possible is requeued.
package main

import (
	"fmt"
	"os"
	"strings"

	provisioningv1alpha1 "github.com/agynio/provisioning/api/v1alpha1"
	"github.com/agynio/provisioning/internal/config"
	"github.com/agynio/provisioning/internal/controller"
	"github.com/agynio/provisioning/internal/platform"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "provisioning: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctrl.SetLogger(zap.New(zap.UseDevMode(false)))
	logger := ctrl.Log.WithName("setup")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return err
	}
	if err := provisioningv1alpha1.AddToScheme(scheme); err != nil {
		return err
	}

	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: cfg.MetricsAddress},
		HealthProbeBindAddress: cfg.ProbeAddress,
		// Declarations and the Secrets they produce live alongside the release,
		// so nothing needs cluster-wide read.
		Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{cfg.Namespace: {}}},
	})
	if err != nil {
		return fmt.Errorf("build manager: %w", err)
	}

	token := func() (string, error) {
		raw, err := os.ReadFile(cfg.BootstrapTokenFile)
		if err != nil {
			return "", fmt.Errorf("read the bootstrap token: %w", err)
		}
		value := strings.TrimSpace(string(raw))
		if value == "" {
			return "", fmt.Errorf("the bootstrap token in %s is empty", cfg.BootstrapTokenFile)
		}
		return value, nil
	}

	api := platform.New(cfg.GatewayURL, token, cfg.RequestTimeout)
	ziti := platform.NewZiti(cfg.ZitiTarget, cfg.RequestTimeout)

	if err := (&controller.OrganizationReconciler{Client: manager.GetClient(), Platform: api}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up the Organization controller: %w", err)
	}
	if err := (&controller.ImageReconciler{Client: manager.GetClient(), Platform: api}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up the Image controller: %w", err)
	}
	if err := (&controller.RunnerReconciler{Client: manager.GetClient(), Scheme: scheme, Platform: api}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up the Runner controller: %w", err)
	}
	if err := (&controller.AppReconciler{Client: manager.GetClient(), Scheme: scheme, Platform: api}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up the App controller: %w", err)
	}
	if err := (&controller.ClusterAdminReconciler{Client: manager.GetClient(), Platform: api}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up the ClusterAdmin controller: %w", err)
	}
	if err := (&controller.OverlayPolicyReconciler{Client: manager.GetClient(), Ziti: ziti}).SetupWithManager(manager); err != nil {
		return fmt.Errorf("set up the OverlayPolicy controller: %w", err)
	}

	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	// Deliberately not a check on whether the platform answers: the controller
	// is healthy while the platform is still starting, and reporting otherwise
	// would restart it through exactly the window it exists to wait out.
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	logger.Info("starting", "gateway", cfg.GatewayURL, "namespace", cfg.Namespace)
	return manager.Start(ctrl.SetupSignalHandler())
}
