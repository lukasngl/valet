package e2e

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
	"github.com/google/uuid"
	"github.com/lukasngl/valet/framework/bddtest"
	"github.com/lukasngl/valet/provider-azure/api/v1alpha1"
	"github.com/lukasngl/valet/provider-azure/internal"
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
	Concurrency: 1,
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

// TestMock runs all scenarios with a canned HTTP transport.
func TestMock(t *testing.T) {
	t.Setenv("TEST_AZURE_OWNED_APP_OBJECT_ID", "00000000-0000-0000-0000-000000000001")

	opts := godogOpts
	status := godog.TestSuite{
		Name: "provider-azure-mock",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			p := internal.New(
				internal.WithHTTPClient(&http.Client{Transport: &graphMock{}}),
				internal.WithBaseURL("http://graph.mock"),
			)
			shared := bddtest.New[*v1alpha1.AzureClientSecret](&testEnvCfg, p, p.NewObject)
			bddtest.RegisterSteps(sc, shared)
		},
		Options: &opts,
	}.Run()

	if status != 0 {
		t.Fatalf("godog tests failed with status %d", status)
	}
}

// TestE2E runs non-mock scenarios against a real Azure Entra ID instance.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	if os.Getenv("AZURE_TENANT_ID") == "" {
		t.Skip("skipping e2e tests: AZURE_TENANT_ID not set")
	}

	opts := godogOpts
	opts.Tags = "~@mock"
	status := godog.TestSuite{
		Name: "provider-azure-e2e",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			p := internal.New()
			shared := bddtest.New[*v1alpha1.AzureClientSecret](&testEnvCfg, p, p.NewObject)
			bddtest.RegisterSteps(sc, shared)
		},
		Options: &opts,
	}.Run()

	if status != 0 {
		t.Fatalf("godog tests failed with status %d", status)
	}
}

// graphMock is an [http.RoundTripper] that returns canned Microsoft Graph API
// responses. Each call to addPassword returns a unique keyId and a fixed
// secret text; getApplication returns a fixed appId; removePassword succeeds.
type graphMock struct{}

func (m *graphMock) RoundTrip(req *http.Request) (*http.Response, error) {
	path := req.URL.Path
	switch {
	case strings.HasSuffix(path, "/addPassword"):
		return jsonResponse(http.StatusOK, map[string]string{
			"keyId":      uuid.New().String(),
			"secretText": "fake-secret-text",
		})
	case strings.HasSuffix(path, "/removePassword"):
		return jsonResponse(http.StatusNoContent, nil)
	case req.Method == http.MethodGet && strings.Contains(path, "/applications/"):
		return jsonResponse(http.StatusOK, map[string]string{
			"appId": "fake-app-id",
		})
	default:
		return jsonResponse(http.StatusNotFound, map[string]string{
			"error": fmt.Sprintf("unexpected request: %s %s", req.Method, path),
		})
	}
}

func jsonResponse(status int, body any) (*http.Response, error) {
	var bodyReader io.ReadCloser
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = io.NopCloser(strings.NewReader(string(data)))
	} else {
		bodyReader = io.NopCloser(strings.NewReader(""))
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       bodyReader,
	}, nil
}
