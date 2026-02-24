// Package bddtest provides a shared BDD test suite for valet providers.
//
// It offers a generic [Suite] that handles envtest lifecycle, common Gherkin
// steps (create/update/delete CRD, phase and secret assertions, credential
// expiry), and controller-manager wiring. Provider-specific test packages
// embed the suite and register their own steps via godogen on top.
package bddtest

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"github.com/lukasngl/valet/framework"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Env holds the shared envtest configuration, initialised once in TestMain.
type Env struct {
	Cfg    *rest.Config
	Scheme *runtime.Scheme
}

// Suite holds per-scenario state. Create a fresh instance for each scenario
// in the godog ScenarioInitializer.
type Suite[O framework.Object] struct {
	Ctx       context.Context
	Cancel    context.CancelFunc
	K8sClient client.Client
	MgrCancel context.CancelFunc
	Namespace string
	Provider  framework.Provider[O]

	env       *Env
	newObject func() O
	lastErr   error
}

// New creates a Suite for one scenario. The provider and newObject factory
// are provider-specific; everything else comes from the shared [Env].
func New[O framework.Object](
	env *Env,
	provider framework.Provider[O],
	newObject func() O,
) *Suite[O] {
	return &Suite[O]{
		env:       env,
		Provider:  provider,
		newObject: newObject,
	}
}

// RegisterSteps binds all common BDD steps and lifecycle hooks to sc.
func RegisterSteps[O framework.Object](sc *godog.ScenarioContext, s *Suite[O]) {
	sc.Before(s.before)
	sc.After(s.after)

	sc.Given(`^a Kubernetes cluster is running$`, s.aKubernetesClusterIsRunning)
	sc.Given(`^the CRDs are installed$`, s.theCRDsAreInstalled)
	sc.Given(`^the operator is running$`, s.theOperatorIsRunning)

	sc.When(`^I create a ClientSecret:$`, s.iCreateAClientSecret)
	sc.When(`^I create a ClientSecret "([^"]*)" with:$`, s.iCreateAClientSecretNamed)
	sc.When(`^I try to create a ClientSecret "([^"]*)" with:$`, s.iTryToCreateAClientSecretNamed)
	sc.When(`^I update the ClientSecret "([^"]*)" with:$`, s.iUpdateTheClientSecretWith)
	sc.When(`^I delete the ClientSecret "([^"]*)"$`, s.iDeleteTheClientSecret)
	sc.When(
		`^I expire the credentials for ClientSecret "([^"]*)"$`,
		s.iExpireTheCredentialsForClientSecret,
	)

	sc.Then(
		`^the ClientSecret "([^"]*)" should have phase "([^"]*)"$`,
		s.theClientSecretShouldHavePhase,
	)
	sc.Then(
		`^the ClientSecret "([^"]*)" should have phase "([^"]*)" within (\d+) seconds$`,
		s.theClientSecretShouldHavePhaseWithin,
	)
	sc.Then(
		`^the ClientSecret "([^"]*)" should not exist within (\d+) seconds$`,
		s.theClientSecretShouldNotExistWithin,
	)
	sc.Then(
		`^the ClientSecret "([^"]*)" status should contain message "([^"]*)"$`,
		s.theClientSecretStatusShouldContainMessage,
	)
	sc.Then(
		`^the ClientSecret "([^"]*)" should have (\d+) active keys$`,
		s.theClientSecretShouldHaveActiveKeys,
	)
	sc.Then(
		`^the ClientSecret "([^"]*)" should have at least (\d+) active keys within (\d+) seconds$`,
		s.theClientSecretShouldHaveAtLeastActiveKeysWithin,
	)

	sc.Then(`^a Secret "([^"]*)" should exist$`, s.aSecretShouldExist)
	sc.Then(
		`^the Secret "([^"]*)" should contain key "([^"]*)"$`,
		s.theSecretShouldContainKey,
	)
	sc.Then(
		`^the Secret "([^"]*)" should contain key "([^"]*)" within (\d+) seconds$`,
		s.theSecretShouldContainKeyWithin,
	)
	sc.Then(
		`^the Secret "([^"]*)" should contain key "([^"]*)" with value "([^"]*)"$`,
		s.theSecretShouldContainKeyWithValue,
	)
	sc.Then(
		`^the Secret "([^"]*)" should contain key "([^"]*)" with value "([^"]*)" within (\d+) seconds$`,
		s.theSecretShouldContainKeyWithValueWithin,
	)
	sc.Then(
		`^the Secret "([^"]*)" should be owned by ClientSecret "([^"]*)"$`,
		s.theSecretShouldBeOwnedByClientSecret,
	)
	sc.Then(`^the operation should have failed$`, s.theOperationShouldHaveFailed)
	sc.Then(
		`^the operation should have failed with "([^"]*)"$`,
		s.theOperationShouldHaveFailedWith,
	)
	sc.Then(`^the Secret "([^"]*)" should not exist$`, s.theSecretShouldNotExist)
}

// --- Lifecycle hooks ---

func (s *Suite[O]) before(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
	s.Ctx, s.Cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	s.Namespace = fmt.Sprintf("test-%s", uuid.New().String()[:8])

	k8sClient, err := client.New(s.env.Cfg, client.Options{Scheme: s.env.Scheme})
	if err != nil {
		return ctx, fmt.Errorf("creating k8s client: %w", err)
	}
	s.K8sClient = k8sClient

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: s.Namespace}}
	if err := s.K8sClient.Create(s.Ctx, ns); err != nil {
		return ctx, fmt.Errorf("creating namespace %s: %w", s.Namespace, err)
	}

	return ctx, nil
}

func (s *Suite[O]) after(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
	if s.MgrCancel != nil {
		s.MgrCancel()
	}
	if s.K8sClient != nil && s.Namespace != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: s.Namespace}}
		_ = s.K8sClient.Delete(
			s.Ctx,
			ns,
			client.PropagationPolicy(metav1.DeletePropagationBackground),
		)
	}
	if s.Cancel != nil {
		s.Cancel()
	}
	return ctx, nil
}

// --- Given steps ---

func (s *Suite[O]) aKubernetesClusterIsRunning(_ context.Context) error {
	if s.env.Cfg == nil {
		return fmt.Errorf("envtest not started")
	}
	return nil
}

func (s *Suite[O]) theCRDsAreInstalled(_ context.Context) error {
	return nil // CRDs are installed by envtest.Environment via CRDDirectoryPaths.
}

func (s *Suite[O]) theOperatorIsRunning(_ context.Context) error {
	mgr, err := ctrl.NewManager(s.env.Cfg, ctrl.Options{
		Scheme:  s.env.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				s.Namespace: {},
			},
		},
	})
	if err != nil {
		return err
	}

	reconciler := &framework.Reconciler[O]{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Provider: s.Provider,
	}

	if err := reconciler.SetupWithManager(mgr, func(b *builder.Builder) {
		b.Named("clientsecret-" + s.Namespace)
	}); err != nil {
		return err
	}

	mgrCtx, cancel := context.WithCancel(s.Ctx)
	s.MgrCancel = cancel
	go func() { _ = mgr.Start(mgrCtx) }()

	return nil
}

// --- When steps ---

// expandDoc expands environment variables in a godog DocString.
func expandDoc(doc *godog.DocString) string {
	return os.ExpandEnv(doc.Content)
}

func (s *Suite[O]) iCreateAClientSecret(_ context.Context, doc *godog.DocString) error {
	obj := s.newObject()
	if err := yaml.Unmarshal([]byte(expandDoc(doc)), obj); err != nil {
		return err
	}
	obj.SetNamespace(s.Namespace)
	return s.K8sClient.Create(s.Ctx, obj)
}

func (s *Suite[O]) iCreateAClientSecretNamed(
	_ context.Context,
	name string,
	doc *godog.DocString,
) error {
	obj := s.newObject()
	if err := yaml.Unmarshal([]byte(expandDoc(doc)), obj); err != nil {
		return err
	}
	obj.SetName(name)
	obj.SetNamespace(s.Namespace)
	return s.K8sClient.Create(s.Ctx, obj)
}

func (s *Suite[O]) iTryToCreateAClientSecretNamed(
	ctx context.Context,
	name string,
	doc *godog.DocString,
) error {
	s.lastErr = s.iCreateAClientSecretNamed(ctx, name, doc)
	return nil
}

func (s *Suite[O]) iUpdateTheClientSecretWith(
	_ context.Context,
	name string,
	doc *godog.DocString,
) error {
	existing := s.newObject()
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, existing); err != nil {
		return err
	}

	// Unmarshal the patch into a fresh object, then copy its spec via JSON
	// round-trip. This works because the status subresource is separate.
	patch := s.newObject()
	if err := yaml.Unmarshal([]byte(expandDoc(doc)), patch); err != nil {
		return err
	}

	// Use the existing object's metadata with the patched spec by re-reading
	// the resource version from the server copy.
	patch.SetName(existing.GetName())
	patch.SetNamespace(existing.GetNamespace())
	patch.SetResourceVersion(existing.GetResourceVersion())
	patch.SetUID(existing.GetUID())
	patch.SetGeneration(existing.GetGeneration())
	patch.SetCreationTimestamp(existing.GetCreationTimestamp())
	patch.SetFinalizers(existing.GetFinalizers())

	return s.K8sClient.Update(s.Ctx, patch)
}

func (s *Suite[O]) iDeleteTheClientSecret(_ context.Context, name string) error {
	obj := s.newObject()
	obj.SetName(name)
	obj.SetNamespace(s.Namespace)
	return s.K8sClient.Delete(s.Ctx, obj)
}

func (s *Suite[O]) iExpireTheCredentialsForClientSecret(_ context.Context, name string) error {
	obj := s.newObject()
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, obj); err != nil {
		return err
	}

	expired := time.Now().Add(-time.Hour)
	status := obj.GetStatus()
	for i := range status.ActiveKeys {
		status.ActiveKeys[i].ExpiresAt = metav1.NewTime(expired)
	}

	return s.K8sClient.Status().Update(s.Ctx, obj)
}

// --- Then steps: ClientSecret assertions ---

func (s *Suite[O]) theClientSecretShouldHavePhase(_ context.Context, name, phase string) error {
	return s.theClientSecretShouldHavePhaseWithin(context.TODO(), name, phase, 30)
}

func (s *Suite[O]) theClientSecretShouldHavePhaseWithin(
	_ context.Context,
	name, phase string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	var lastPhase string
	for time.Now().Before(deadline) {
		obj := s.newObject()
		if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
			Namespace: s.Namespace, Name: name,
		}, obj); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		lastPhase = obj.GetStatus().Phase
		if lastPhase == phase {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("ClientSecret %q phase is %q, expected %q", name, lastPhase, phase)
}

func (s *Suite[O]) theClientSecretShouldNotExistWithin(
	_ context.Context,
	name string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		obj := s.newObject()
		err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
			Namespace: s.Namespace, Name: name,
		}, obj)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("ClientSecret %q still exists after %d seconds", name, seconds)
}

func (s *Suite[O]) theClientSecretStatusShouldContainMessage(
	_ context.Context,
	name, message string,
) error {
	obj := s.newObject()
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, obj); err != nil {
		return err
	}
	status := obj.GetStatus()
	if status.LastFailureMessage == "" {
		return fmt.Errorf("ClientSecret %q has no failure message", name)
	}
	if !strings.Contains(status.LastFailureMessage, message) {
		return fmt.Errorf(
			"ClientSecret %q failure message %q does not contain %q",
			name,
			status.LastFailureMessage,
			message,
		)
	}
	return nil
}

func (s *Suite[O]) theClientSecretShouldHaveActiveKeys(
	_ context.Context,
	name string,
	count int,
) error {
	obj := s.newObject()
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, obj); err != nil {
		return err
	}
	actual := len(obj.GetStatus().ActiveKeys)
	if actual != count {
		return fmt.Errorf("ClientSecret %q has %d active keys, expected %d", name, actual, count)
	}
	return nil
}

func (s *Suite[O]) theClientSecretShouldHaveAtLeastActiveKeysWithin(
	_ context.Context,
	name string,
	count, seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	var lastCount int
	for time.Now().Before(deadline) {
		obj := s.newObject()
		if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
			Namespace: s.Namespace, Name: name,
		}, obj); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		lastCount = len(obj.GetStatus().ActiveKeys)
		if lastCount >= count {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf(
		"ClientSecret %q has %d active keys, expected at least %d",
		name,
		lastCount,
		count,
	)
}

// --- Then steps: Secret assertions ---

func (s *Suite[O]) aSecretShouldExist(_ context.Context, name string) error {
	var secret corev1.Secret
	return s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, &secret)
}

func (s *Suite[O]) theSecretShouldContainKey(_ context.Context, name, key string) error {
	var secret corev1.Secret
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, &secret); err != nil {
		return err
	}
	val, ok := secret.Data[key]
	if !ok {
		return fmt.Errorf("key %q not found in secret %q", key, name)
	}
	if len(val) == 0 {
		return fmt.Errorf("key %q in secret %q is empty", key, name)
	}
	return nil
}

func (s *Suite[O]) theSecretShouldContainKeyWithin(
	_ context.Context,
	name, key string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		var secret corev1.Secret
		if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
			Namespace: s.Namespace, Name: name,
		}, &secret); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if val, ok := secret.Data[key]; ok && len(val) > 0 {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf(
		"key %q in secret %q not found or empty after %d seconds",
		key, name, seconds,
	)
}

func (s *Suite[O]) theSecretShouldContainKeyWithValue(
	_ context.Context,
	name, key, value string,
) error {
	var secret corev1.Secret
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
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

func (s *Suite[O]) theSecretShouldContainKeyWithValueWithin(
	_ context.Context,
	name, key, value string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for time.Now().Before(deadline) {
		var secret corev1.Secret
		if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
			Namespace: s.Namespace, Name: name,
		}, &secret); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		if actual, ok := secret.Data[key]; ok && string(actual) == value {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf(
		"key %q in secret %q did not reach value %q within %d seconds",
		key,
		name,
		value,
		seconds,
	)
}

func (s *Suite[O]) theSecretShouldBeOwnedByClientSecret(
	_ context.Context,
	secretName, ownerName string,
) error {
	var secret corev1.Secret
	if err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: secretName,
	}, &secret); err != nil {
		return err
	}
	for _, ref := range secret.OwnerReferences {
		if ref.Name == ownerName && ref.Controller != nil && *ref.Controller {
			return nil
		}
	}
	return fmt.Errorf(
		"secret %q has no controller ownerReference to %q",
		secretName,
		ownerName,
	)
}

func (s *Suite[O]) theOperationShouldHaveFailed(_ context.Context) error {
	if s.lastErr == nil {
		return fmt.Errorf("expected operation to fail, but it succeeded")
	}
	s.lastErr = nil
	return nil
}

func (s *Suite[O]) theOperationShouldHaveFailedWith(_ context.Context, message string) error {
	if s.lastErr == nil {
		return fmt.Errorf("expected operation to fail, but it succeeded")
	}
	err := s.lastErr
	s.lastErr = nil
	if !strings.Contains(err.Error(), message) {
		return fmt.Errorf("error %q does not contain %q", err.Error(), message)
	}
	return nil
}

func (s *Suite[O]) theSecretShouldNotExist(_ context.Context, name string) error {
	var secret corev1.Secret
	err := s.K8sClient.Get(s.Ctx, client.ObjectKey{
		Namespace: s.Namespace, Name: name,
	}, &secret)
	if err == nil {
		return fmt.Errorf("secret %q exists but should not", name)
	}
	return client.IgnoreNotFound(err)
}
