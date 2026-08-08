package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultGatewayURL     = "http://gateway:8080"
	defaultZitiTarget     = "http://ziti-management:50051"
	defaultRequestTimeout = 30 * time.Second
)

// Config is what the controller needs to reach the platform. Everything else it
// needs is in the objects it reconciles.
type Config struct {
	// GatewayURL is the ordinary API. There is no internal provisioning
	// surface, so this is the only way in.
	GatewayURL string
	// BootstrapTokenFile holds the platform admin identity's credential. Read
	// per call rather than at startup: the kubelet remounts a changed Secret
	// without restarting the pod.
	BootstrapTokenFile string
	ZitiTarget         string
	// Namespace the declarations and the Secrets they produce live in.
	Namespace      string
	RequestTimeout time.Duration
	MetricsAddress string
	ProbeAddress   string
}

func Load() (Config, error) {
	config := Config{
		GatewayURL:         envOrDefault("GATEWAY_URL", defaultGatewayURL),
		BootstrapTokenFile: envOrDefault("BOOTSTRAP_TOKEN_FILE", "/etc/agyn/bootstrap/token"),
		ZitiTarget:         envOrDefault("ZITI_MANAGEMENT_TARGET", defaultZitiTarget),
		Namespace:          strings.TrimSpace(os.Getenv("WATCH_NAMESPACE")),
		RequestTimeout:     defaultRequestTimeout,
		MetricsAddress:     envOrDefault("METRICS_ADDRESS", ":8080"),
		ProbeAddress:       envOrDefault("PROBE_ADDRESS", ":8081"),
	}
	if config.Namespace == "" {
		return Config{}, fmt.Errorf("WATCH_NAMESPACE must be set")
	}
	if raw := strings.TrimSpace(os.Getenv("REQUEST_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return Config{}, fmt.Errorf("REQUEST_TIMEOUT must be a positive duration")
		}
		config.RequestTimeout = timeout
	}
	return config, nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
