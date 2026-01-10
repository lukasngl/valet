//go:generate godogen

package e2e

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/cucumber/godog"
	secretmanagerv1alpha1 "github.com/lukasngl/secret-manager/api/v1alpha1"
	"github.com/lukasngl/secret-manager/internal/adapter"
	"github.com/lukasngl/secret-manager/internal/controller"
	"github.com/lukasngl/secret-manager/internal/crd"
	"github.com/testcontainers/testcontainers-go/modules/k3s"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// defaultNamespace is the Kubernetes namespace used for test resources.
const defaultNamespace = "default"

// envVarPattern matches ${VAR_NAME} patterns for expansion.
var envVarPattern = regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)

// expandEnvVars expands ${VAR_NAME} patterns in the input string.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := envVarPattern.FindStringSubmatch(match)[1]
		return os.Getenv(varName)
	})
}

// ScenarioContext holds all state for a single scenario.
type ScenarioContext struct {
	ctx        context.Context
	cancel     context.CancelFunc
	k3sC       *k3s.K3sContainer
	restConfig *rest.Config
	k8sClient  client.Client
	scheme     *runtime.Scheme
	registry   adapter.Registry
	mock       *MockProvider
	mgrCancel  context.CancelFunc
}

type scenarioCtxKey struct{}

func getScenarioContext(ctx context.Context) *ScenarioContext {
	return ctx.Value(scenarioCtxKey{}).(*ScenarioContext)
}

//godogen:before
func beforeScenario(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
	sctx := &ScenarioContext{}
	sctx.ctx, sctx.cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	sctx.mock = NewMockProvider()
	sctx.registry = adapter.Registry{}
	sctx.registry.Register(sctx.mock)
	return context.WithValue(ctx, scenarioCtxKey{}, sctx), nil
}

//godogen:after
func afterScenario(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
	sctx := ctx.Value(scenarioCtxKey{}).(*ScenarioContext)
	if sctx.mgrCancel != nil {
		sctx.mgrCancel()
	}
	if sctx.k3sC != nil {
		_ = sctx.k3sC.Terminate(sctx.ctx)
	}
	if sctx.cancel != nil {
		sctx.cancel()
	}
	return ctx, nil
}

//godogen:given ^a Kubernetes cluster is running$
func aKubernetesClusterIsRunning(ctx context.Context) error {
	sctx := getScenarioContext(ctx)
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	k3sContainer, err := k3s.Run(sctx.ctx, "rancher/k3s:v1.31.2-k3s1")
	if err != nil {
		return err
	}
	sctx.k3sC = k3sContainer

	kubeconfig, err := k3sContainer.GetKubeConfig(sctx.ctx)
	if err != nil {
		return err
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		return err
	}
	sctx.restConfig = restConfig

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = secretmanagerv1alpha1.AddToScheme(scheme)
	_ = apiextensionsv1.AddToScheme(scheme)
	sctx.scheme = scheme

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	sctx.k8sClient = k8sClient

	return nil
}

//godogen:given ^the CRDs are installed$
func theCRDsAreInstalled(ctx context.Context) error {
	sctx := getScenarioContext(ctx)

	crdBytes, err := crd.Patch(baseCRD, sctx.registry)
	if err != nil {
		return err
	}

	var crdObj apiextensionsv1.CustomResourceDefinition
	if err := yaml.Unmarshal(crdBytes, &crdObj); err != nil {
		return err
	}

	if err := sctx.k8sClient.Create(sctx.ctx, &crdObj); err != nil {
		return err
	}

	return waitForCRD(sctx.ctx, sctx.k8sClient, crdObj.Name)
}

//godogen:given ^the operator is running$
func theOperatorIsRunning(ctx context.Context) error {
	sctx := getScenarioContext(ctx)

	mgr, err := ctrl.NewManager(sctx.restConfig, ctrl.Options{
		Scheme: sctx.scheme,
	})
	if err != nil {
		return err
	}

	reconciler := &controller.ClientSecretReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Registry: sctx.registry,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		return err
	}

	mgrCtx, cancel := context.WithCancel(sctx.ctx)
	sctx.mgrCancel = cancel

	go func() {
		_ = mgr.Start(mgrCtx)
	}()

	return nil
}

//godogen:when ^I create a ClientSecret:$
func iCreateAClientSecret(ctx context.Context, doc *godog.DocString) error {
	sctx := getScenarioContext(ctx)

	content := expandEnvVars(doc.Content)
	var cs secretmanagerv1alpha1.ClientSecret
	if err := yaml.Unmarshal([]byte(content), &cs); err != nil {
		return err
	}

	if cs.Namespace == "" {
		cs.Namespace = defaultNamespace
	}

	return sctx.k8sClient.Create(sctx.ctx, &cs)
}

//godogen:when ^I create a ClientSecret "([^"]*)" with:$
func iCreateAClientSecretNamed(ctx context.Context, name string, doc *godog.DocString) error {
	sctx := getScenarioContext(ctx)

	content := expandEnvVars(doc.Content)
	var cs secretmanagerv1alpha1.ClientSecret
	if err := yaml.Unmarshal([]byte(content), &cs); err != nil {
		return err
	}

	cs.Name = name
	if cs.Namespace == "" {
		cs.Namespace = defaultNamespace
	}

	return sctx.k8sClient.Create(sctx.ctx, &cs)
}

//godogen:when ^I update the ClientSecret "([^"]*)" with:$
func iUpdateTheClientSecretWith(ctx context.Context, name string, doc *godog.DocString) error {
	sctx := getScenarioContext(ctx)

	// Get existing ClientSecret
	var existing secretmanagerv1alpha1.ClientSecret
	if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &existing); err != nil {
		return err
	}

	// Parse the new spec
	var patch secretmanagerv1alpha1.ClientSecret
	if err := yaml.Unmarshal([]byte(doc.Content), &patch); err != nil {
		return err
	}

	// Update the spec
	existing.Spec = patch.Spec
	return sctx.k8sClient.Update(sctx.ctx, &existing)
}

//godogen:then ^the ClientSecret "([^"]*)" should have phase "([^"]*)"$
func theClientSecretShouldHavePhase(ctx context.Context, name, phase string) error {
	return theClientSecretShouldHavePhaseWithin(ctx, name, phase, 30)
}

//godogen:then ^the ClientSecret "([^"]*)" should have phase "([^"]*)" within (\d+) seconds$
func theClientSecretShouldHavePhaseWithin(ctx context.Context, name, phase string, seconds int) error {
	sctx := getScenarioContext(ctx)
	timeout := time.Duration(seconds) * time.Second
	deadline := time.Now().Add(timeout)

	var lastPhase string
	for time.Now().Before(deadline) {
		var cs secretmanagerv1alpha1.ClientSecret
		if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
			Namespace: defaultNamespace,
			Name:      name,
		}, &cs); err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		lastPhase = cs.Status.Phase
		if lastPhase == phase {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("ClientSecret %q phase is %q, expected %q", name, lastPhase, phase)
}

//godogen:then ^a Secret "([^"]*)" should exist$
func aSecretShouldExist(ctx context.Context, name string) error {
	sctx := getScenarioContext(ctx)
	var secret corev1.Secret
	return sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &secret)
}

//godogen:then ^the Secret "([^"]*)" should contain key "([^"]*)"$
func theSecretShouldContainKey(ctx context.Context, name, key string) error {
	sctx := getScenarioContext(ctx)
	var secret corev1.Secret
	if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &secret); err != nil {
		return err
	}

	if _, ok := secret.Data[key]; !ok {
		return fmt.Errorf("key %q not found in secret %q", key, name)
	}
	return nil
}

//godogen:then ^the Secret "([^"]*)" should contain key "([^"]*)" with value "([^"]*)"$
func theSecretShouldContainKeyWithValue(ctx context.Context, name, key, value string) error {
	sctx := getScenarioContext(ctx)
	var secret corev1.Secret
	if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &secret); err != nil {
		return err
	}

	actual, ok := secret.Data[key]
	if !ok {
		return fmt.Errorf("key %q not found in secret %q", key, name)
	}

	if string(actual) != value {
		return fmt.Errorf("key %q has value %q, expected %q", key, string(actual), value)
	}
	return nil
}

//godogen:then ^the Secret "([^"]*)" should not exist$
func theSecretShouldNotExist(ctx context.Context, name string) error {
	sctx := getScenarioContext(ctx)
	var secret corev1.Secret
	err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &secret)
	if err == nil {
		return fmt.Errorf("secret %q exists but should not", name)
	}
	return client.IgnoreNotFound(err)
}

//godogen:then ^the Secret "([^"]*)" should not exist within (\d+) seconds$
func theSecretShouldNotExistWithin(ctx context.Context, name string, seconds int) error {
	sctx := getScenarioContext(ctx)
	timeout := time.Duration(seconds) * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var secret corev1.Secret
		err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
			Namespace: defaultNamespace,
			Name:      name,
		}, &secret)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("secret %q still exists after %d seconds", name, seconds)
}

//godogen:when ^I delete the ClientSecret "([^"]*)"$
func iDeleteTheClientSecret(ctx context.Context, name string) error {
	sctx := getScenarioContext(ctx)
	cs := &secretmanagerv1alpha1.ClientSecret{}
	cs.Name = name
	cs.Namespace = defaultNamespace
	return sctx.k8sClient.Delete(sctx.ctx, cs)
}

//godogen:then ^the ClientSecret "([^"]*)" should not exist within (\d+) seconds$
func theClientSecretShouldNotExistWithin(ctx context.Context, name string, seconds int) error {
	sctx := getScenarioContext(ctx)
	timeout := time.Duration(seconds) * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		var cs secretmanagerv1alpha1.ClientSecret
		err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
			Namespace: defaultNamespace,
			Name:      name,
		}, &cs)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("ClientSecret %q still exists after %d seconds", name, seconds)
}

//godogen:then ^the ClientSecret "([^"]*)" status should contain message "([^"]*)"$
func theClientSecretStatusShouldContainMessage(ctx context.Context, name, message string) error {
	sctx := getScenarioContext(ctx)
	var cs secretmanagerv1alpha1.ClientSecret
	if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &cs); err != nil {
		return err
	}

	if cs.Status.LastFailureMessage == "" {
		return fmt.Errorf("ClientSecret %q has no failure message", name)
	}

	if !strings.Contains(cs.Status.LastFailureMessage, message) {
		return fmt.Errorf(
			"ClientSecret %q failure message %q does not contain %q",
			name, cs.Status.LastFailureMessage, message)
	}
	return nil
}

//godogen:then ^the mock provider should have received (\d+) provision calls$
func theMockProviderShouldHaveReceivedProvisionCalls(ctx context.Context, count int) error {
	sctx := getScenarioContext(ctx)
	actual := len(sctx.mock.ProvisionCalls)
	if actual != count {
		return fmt.Errorf("expected %d provision calls, got %d", count, actual)
	}
	return nil
}

//godogen:then ^the mock provider should have received at least (\d+) provision calls$
func theMockProviderShouldHaveReceivedAtLeastProvisionCalls(ctx context.Context, count int) error {
	sctx := getScenarioContext(ctx)
	actual := len(sctx.mock.ProvisionCalls)
	if actual < count {
		return fmt.Errorf("expected at least %d provision calls, got %d", count, actual)
	}
	return nil
}

//godogen:then ^the mock provider should have received (\d+) delete key calls$
func theMockProviderShouldHaveReceivedDeleteKeyCalls(ctx context.Context, count int) error {
	sctx := getScenarioContext(ctx)
	actual := len(sctx.mock.DeleteKeyCalls)
	if actual != count {
		return fmt.Errorf("expected %d delete key calls, got %d", count, actual)
	}
	return nil
}

//godogen:then ^the mock provider should have received at least (\d+) delete key calls within (\d+) seconds$
func theMockProviderShouldHaveReceivedAtLeastDeleteKeyCallsWithin(ctx context.Context, count, seconds int) error {
	sctx := getScenarioContext(ctx)
	timeout := time.Duration(seconds) * time.Second
	deadline := time.Now().Add(timeout)

	var actual int
	for time.Now().Before(deadline) {
		actual = len(sctx.mock.DeleteKeyCalls)
		if actual >= count {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("expected at least %d delete key calls, got %d after %d seconds", count, actual, seconds)
}

//godogen:when ^I expire the credentials for ClientSecret "([^"]*)"$
func iExpireTheCredentialsForClientSecret(ctx context.Context, name string) error {
	sctx := getScenarioContext(ctx)

	var cs secretmanagerv1alpha1.ClientSecret
	if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &cs); err != nil {
		return err
	}

	// Set all active keys to expired
	expired := time.Now().Add(-time.Hour)
	for i := range cs.Status.ActiveKeys {
		cs.Status.ActiveKeys[i].ExpiresAt = metav1.NewTime(expired)
	}

	return sctx.k8sClient.Status().Update(sctx.ctx, &cs)
}

//godogen:then ^the ClientSecret "([^"]*)" should have (\d+) active keys$
func theClientSecretShouldHaveActiveKeys(ctx context.Context, name string, count int) error {
	sctx := getScenarioContext(ctx)

	var cs secretmanagerv1alpha1.ClientSecret
	if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
		Namespace: defaultNamespace,
		Name:      name,
	}, &cs); err != nil {
		return err
	}

	actual := len(cs.Status.ActiveKeys)
	if actual != count {
		return fmt.Errorf("ClientSecret %q has %d active keys, expected %d", name, actual, count)
	}
	return nil
}

//godogen:then ^the ClientSecret "([^"]*)" should have at least (\d+) active keys within (\d+) seconds$
func theClientSecretShouldHaveAtLeastActiveKeysWithin(ctx context.Context, name string, count, seconds int) error {
	sctx := getScenarioContext(ctx)
	timeout := time.Duration(seconds) * time.Second
	deadline := time.Now().Add(timeout)

	var lastCount int
	for time.Now().Before(deadline) {
		var cs secretmanagerv1alpha1.ClientSecret
		if err := sctx.k8sClient.Get(sctx.ctx, client.ObjectKey{
			Namespace: defaultNamespace,
			Name:      name,
		}, &cs); err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		lastCount = len(cs.Status.ActiveKeys)
		if lastCount >= count {
			return nil
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("ClientSecret %q has %d active keys, expected at least %d", name, lastCount, count)
}

func waitForCRD(ctx context.Context, c client.Client, name string) error {
	for i := 0; i < 30; i++ {
		var crdObj apiextensionsv1.CustomResourceDefinition
		if err := c.Get(ctx, client.ObjectKey{Name: name}, &crdObj); err != nil {
			time.Sleep(time.Second)
			continue
		}

		for _, cond := range crdObj.Status.Conditions {
			if cond.Type == apiextensionsv1.Established && cond.Status == apiextensionsv1.ConditionTrue {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("CRD %q not established after 30s", name)
}

var baseCRD []byte

func init() {
	var err error
	baseCRD, err = os.ReadFile("../../config/crd/secret-manager.ngl.cx_clientsecrets.yaml")
	if err != nil {
		panic("failed to read base CRD: " + err.Error())
	}
}
