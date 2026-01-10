package e2e

import (
	"flag"
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

var godogOpts = godog.Options{
	Format:      "pretty",
	Output:      colors.Colored(os.Stdout),
	Paths:       []string{"../../features"},
	Concurrency: 1,
	Strict:      true, // Fail if there are any undefined or pending steps
}

func init() {
	godog.BindFlags("godog.", flag.CommandLine, &godogOpts)
}

func TestMain(m *testing.M) {
	flag.Parse()

	// Override paths if specified via --godog.paths or positional args
	if len(flag.Args()) > 0 {
		godogOpts.Paths = flag.Args()
	}

	os.Exit(m.Run())
}

func TestFeatures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	status := godog.TestSuite{
		Name: "secret-manager",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			InitializeSteps(sc)      // mock provider e2e steps
			InitializeAzureSteps(sc) // azure provider integration steps
		},
		Options: &godogOpts,
	}.Run()

	if status != 0 {
		t.Fatalf("godog tests failed with status %d", status)
	}
}
