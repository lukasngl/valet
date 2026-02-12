package e2e

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// testCfg, testScheme, and testEnv are initialized in TestMain (suite_test.go)
// and shared across all scenarios.
var (
	testCfg    *rest.Config
	testScheme *runtime.Scheme
	testEnv    *envtest.Environment
)
