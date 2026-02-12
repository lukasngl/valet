// provider-mock runs a mock valet provider for testing.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"os"

	"github.com/lukasngl/valet/framework"
	"github.com/lukasngl/valet/provider-mock/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

var version = "dev"

var (
	metricsAddr = flag.String(
		"metrics-bind-address",
		":8080",
		"Metrics endpoint bind address.",
	)
	probeAddr = flag.String(
		"health-probe-bind-address",
		":8081",
		"Health probe bind address.",
	)
	enableLeaderElection = flag.Bool("leader-elect", false, "Enable leader election.")
	enableHTTP2          = flag.Bool(
		"enable-http2",
		false,
		"Enable HTTP/2 for metrics and webhooks.",
	)
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// +kubebuilder:rbac:groups=mock.valet.ngl.cx,resources=clientsecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mock.valet.ngl.cx,resources=clientsecrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mock.valet.ngl.cx,resources=clientsecrets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func run() error {
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog := ctrl.Log.WithName("setup")

	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(mock.AddToScheme(scheme))

	tlsOpts := []func(*tls.Config){}
	if !*enableHTTP2 {
		tlsOpts = append(tlsOpts, func(c *tls.Config) {
			c.NextProtos = []string{"http/1.1"}
		})
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: *metricsAddr,
			TLSOpts:     tlsOpts,
		},
		WebhookServer:          webhook.NewServer(webhook.Options{TLSOpts: tlsOpts}),
		HealthProbeBindAddress: *probeAddr,
		LeaderElection:         *enableLeaderElection,
		LeaderElectionID:       "provider-mock.valet.ngl.cx",
	})
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}

	reconciler := &framework.Reconciler[*mock.ClientSecret]{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Provider: mock.NewProvider(),
	}
	if err := reconciler.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up controller: %w", err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("setting up ready check: %w", err)
	}

	setupLog.Info("starting manager", "version", version)
	return mgr.Start(ctrl.SetupSignalHandler())
}
