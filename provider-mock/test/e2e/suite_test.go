package e2e

import (
	"flag"
	"fmt"
	"os"
	goruntime "runtime"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/lukasngl/client-secret-operator/provider-mock/mock"
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

func TestMain(m *testing.M) {
	flag.Parse()

	if len(flag.Args()) > 0 {
		godogOpts.Paths = flag.Args()
	}

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	testScheme = runtime.NewScheme()
	_ = corev1.AddToScheme(testScheme)
	_ = mock.AddToScheme(testScheme)

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{"../../config/crd"},
		Scheme:            testScheme,
	}

	cfg, err := testEnv.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start envtest: %v\n", err)
		os.Exit(1)
	}
	testCfg = cfg

	code := m.Run()

	_ = testEnv.Stop()
	os.Exit(code)
}

func TestFeatures(t *testing.T) {
	status := godog.TestSuite{
		Name: "provider-mock",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			InitializeSteps(sc, &Suite{})
		},
		Options: &godogOpts,
	}.Run()

	if status != 0 {
		t.Fatalf("godog tests failed with status %d", status)
	}
}
