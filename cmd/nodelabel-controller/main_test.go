package main

import (
	"crypto/tls"
	"slices"
	"testing"
)

func TestMetricsSecureEnablesAuthFilter(t *testing.T) {
	opts := metricsConfig{addr: ":8443", secure: true}.options()

	if !opts.SecureServing {
		t.Error("SecureServing = false, want true")
	}
	if opts.FilterProvider == nil {
		t.Error("FilterProvider is nil: the metrics endpoint would serve without authn/authz")
	}
}

func TestMetricsInsecureHasNoAuthFilter(t *testing.T) {
	opts := metricsConfig{addr: ":8080", secure: false}.options()

	if opts.SecureServing {
		t.Error("SecureServing = true, want false")
	}
	if opts.FilterProvider != nil {
		t.Error("FilterProvider is set, want nil")
	}
}

func TestMetricsDisablesHTTP2ByDefault(t *testing.T) {
	opts := metricsConfig{addr: ":8443", secure: true}.options()

	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	for _, apply := range opts.TLSOpts {
		apply(cfg)
	}
	if !slices.Equal(cfg.NextProtos, []string{"http/1.1"}) {
		t.Errorf("NextProtos = %v, want [http/1.1]", cfg.NextProtos)
	}
}

func TestMetricsHTTP2OptIn(t *testing.T) {
	opts := metricsConfig{addr: ":8443", secure: true, enableHTTP2: true}.options()

	if len(opts.TLSOpts) != 0 {
		t.Errorf("len(TLSOpts) = %d, want 0", len(opts.TLSOpts))
	}
}

func TestMetricsCertPathSetsServingFiles(t *testing.T) {
	opts := metricsConfig{
		addr: ":8443", secure: true,
		certPath: "/tmp/certs", certName: "tls.crt", certKey: "tls.key",
	}.options()

	if opts.CertDir != "/tmp/certs" || opts.CertName != "tls.crt" || opts.KeyName != "tls.key" {
		t.Errorf("cert files = %q %q %q", opts.CertDir, opts.CertName, opts.KeyName)
	}
}

func TestMetricsCertPathEmptyLeavesDefaults(t *testing.T) {
	opts := metricsConfig{addr: ":8443", secure: true, certName: "tls.crt", certKey: "tls.key"}.options()

	if opts.CertDir != "" || opts.CertName != "" || opts.KeyName != "" {
		t.Errorf("cert files set without certPath: %q %q %q", opts.CertDir, opts.CertName, opts.KeyName)
	}
}
