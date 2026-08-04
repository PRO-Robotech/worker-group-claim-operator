/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/tls"
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	workergroupv1alpha1 "github.com/pointpu/worker-group-claim-operator/api/v1alpha1"
	"github.com/pointpu/worker-group-claim-operator/internal/controller"
	"github.com/pointpu/worker-group-claim-operator/internal/remote"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(workergroupv1alpha1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.AddToScheme(scheme))
}

type metricsConfig struct {
	addr        string
	secure      bool
	enableHTTP2 bool
	certPath    string
	certName    string
	certKey     string
}

func (c metricsConfig) options() metricsserver.Options {
	var tlsOpts []func(*tls.Config)
	// HTTP/2 is off by default: GHSA-qppj-fm5r-hxr3, GHSA-4374-p667-p6c8.
	if !c.enableHTTP2 {
		tlsOpts = append(tlsOpts, func(cfg *tls.Config) { cfg.NextProtos = []string{"http/1.1"} })
	}

	opts := metricsserver.Options{BindAddress: c.addr, SecureServing: c.secure, TLSOpts: tlsOpts}
	if c.secure {
		opts.FilterProvider = filters.WithAuthenticationAndAuthorization
	}
	if c.certPath != "" {
		opts.CertDir, opts.CertName, opts.KeyName = c.certPath, c.certName, c.certKey
	}

	return opts
}

func main() {
	var probeAddr string
	var enableLeaderElection, observeOnly, removeForeign bool
	var metrics metricsConfig

	flag.StringVar(&metrics.addr, "metrics-bind-address", "0",
		"The address the metrics endpoint binds to. Use :8443 for HTTPS or :8080 for HTTP, or 0 to disable.")
	flag.BoolVar(&metrics.secure, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS with authn/authz. "+
			"Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&metrics.enableHTTP2, "enable-http2", false, "If set, HTTP/2 will be enabled for the metrics server.")
	flag.StringVar(&metrics.certPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metrics.certName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metrics.certKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election.")
	flag.BoolVar(&observeOnly, "observe-only", true,
		"Compute and log the label set without writing to target clusters.")
	flag.BoolVar(&removeForeign, "remove-foreign-denied", false,
		"Also delete policy-denied labels owned by another writer (kubectl, kubelet).")

	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	logf.SetLogger(ctrl.Log)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metrics.options(),
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "nodelabel-controller.workergroup.in-cloud.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.NodeLabelDeliveryReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		Recorder:      mgr.GetEventRecorderFor("nodelabel-controller"),
		Clients:       remote.NewCache(),
		ObserveOnly:   observeOnly,
		RemoveForeign: removeForeign,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NodeLabelDelivery")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting node label delivery controller",
		"observeOnly", observeOnly, "removeForeignDenied", removeForeign)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
