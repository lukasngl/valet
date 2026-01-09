/*
Copyright 2025.

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
	"fmt"
	"os"

	secretmanagerv1alpha1 "github.com/lukasngl/secret-manager/api/v1alpha1"
	"github.com/lukasngl/secret-manager/internal/adapter"
	_ "github.com/lukasngl/secret-manager/internal/adapter/azure"
	"github.com/lukasngl/secret-manager/internal/controller"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	// Import for auth plugins (also registers --kubeconfig flag)
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error running operator: %v\n", err)
		os.Exit(1)
	}
}

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	// CommandLine flags

	kubeContext = flag.String(
		"context",
		"",
		"Kubernetes context to use (uses current context if empty)",
	)
	metricsAddr = flag.String(
		"metrics-bind-address",
		":8080",
		"The address the metric endpoint binds to.",
	)
	probeAddr = flag.String(
		"health-probe-bind-address",
		":8081",
		"The address the probe endpoint binds to.",
	)
	enableLeaderElection = flag.Bool(
		"leader-elect",
		false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.",
	)
	secureMetrics = flag.Bool(
		"metrics-secure",
		false,
		"If set the metrics endpoint is served securely",
	)
	enableHTTP2 = flag.Bool(
		"enable-http2",
		false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers",
	)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(secretmanagerv1alpha1.AddToScheme(scheme))
}

// Run starts the operator with the registered adapters.
// This is the main entry point for custom builds.
func run() error {
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// Disable HTTP/2 due to vulnerabilities
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !*enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	// Use kubeconfig with context override
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: *kubeContext},
	)

	cfg, err := clientCfg.ClientConfig()
	if err != nil {
		setupLog.Error(err, "unable to get kubeconfig")
		return err
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   *metricsAddr,
			SecureServing: *secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "secret-manager.ngl.cx",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return err
	}

	controller := &controller.ClientSecretReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: adapter.DefaultRegistry(),
	}

	if err = controller.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ClientSecret")
		return err
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return err
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		return err
	}

	return nil
}
