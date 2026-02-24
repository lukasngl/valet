package e2e

import (
	"flag"
	"fmt"
	"os"
	goruntime "runtime"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/lukasngl/valet/framework/bddtest"
	"github.com/lukasngl/valet/provider-mock/api/v1alpha1"
	"github.com/lukasngl/valet/provider-mock/mock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var godogOpts = godog.Options{
	Format:      "pretty",
	Output:      colors.Colored(os.Stdout),
	Paths:       []string{"../../features"},
	Concurrency: goruntime.GOMAXPROCS(0),
	Strict:      true,
}

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &godogOpts)
}

var testEnvCfg bddtest.Env

func TestMain(m *testing.M) {
	flag.Parse()

	if len(flag.Args()) > 0 {
		godogOpts.Paths = flag.Args()
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	testEnvCfg.Scheme = runtime.NewScheme()
	_ = corev1.AddToScheme(testEnvCfg.Scheme)
	_ = v1alpha1.AddToScheme(testEnvCfg.Scheme)

	env := &envtest.Environment{
		CRDDirectoryPaths: []string{"../../config/crd"},
		Scheme:            testEnvCfg.Scheme,
	}
	// kube-apiserver 1.35+ fails route detection in environments without a
	// default route (e.g. nix sandbox). Setting the addresses explicitly
	// avoids the lookup.
	env.ControlPlane.GetAPIServer().Configure().
		Append("advertise-address", "127.0.0.1").
		Append("bind-address", "127.0.0.1")

	cfg, err := env.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start envtest: %v\n", err)
		os.Exit(1)
	}
	testEnvCfg.Cfg = cfg

	code := m.Run()

	_ = env.Stop()
	os.Exit(code)
}

func TestFeatures(t *testing.T) {
	status := godog.TestSuite{
		Name: "provider-mock",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			p := mock.NewProvider()
			shared := bddtest.New[*v1alpha1.ClientSecret](&testEnvCfg, p, p.NewObject)
			bddtest.RegisterSteps(sc, shared)

			InitializeSteps(sc, &Suite{Suite: shared, provider: p})
		},
		Options: &godogOpts,
	}.Run()

	if status != 0 {
		t.Fatalf("godog tests failed with status %d", status)
	}
}
