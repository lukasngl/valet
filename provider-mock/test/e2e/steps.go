//go:generate godogen

package e2e

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"github.com/lukasngl/client-secret-operator/framework"
	"github.com/lukasngl/client-secret-operator/provider-mock/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// Suite holds per-scenario state. A fresh instance is created for each
// scenario by the godogen-generated initializer, providing isolation.
type Suite struct {
	ctx       context.Context
	cancel    context.CancelFunc
	k8sClient client.Client
	provider  *mock.Provider
	mgrCancel context.CancelFunc
	namespace string
}

//godogen:before
func (s *Suite) before(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	s.provider = mock.NewProvider()
	s.namespace = fmt.Sprintf("test-%s", uuid.New().String()[:8])

	k8sClient, err := client.New(testCfg, client.Options{Scheme: testScheme})
	if err != nil {
		return ctx, fmt.Errorf("creating k8s client: %w", err)
	}
	s.k8sClient = k8sClient

	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: s.namespace}}
	if err := s.k8sClient.Create(s.ctx, ns); err != nil {
		return ctx, fmt.Errorf("creating namespace %s: %w", s.namespace, err)
	}

	return ctx, nil
}

//godogen:after
func (s *Suite) after(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
	if s.mgrCancel != nil {
		s.mgrCancel()
	}

	if s.k8sClient != nil && s.namespace != "" {
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: s.namespace}}
		_ = s.k8sClient.Delete(
			s.ctx,
			ns,
			client.PropagationPolicy(metav1.DeletePropagationBackground),
		)
	}

	if s.cancel != nil {
		s.cancel()
	}
	return ctx, nil
}

//godogen:given ^a Kubernetes cluster is running$
func (s *Suite) aKubernetesClusterIsRunning(_ context.Context) error {
	if testCfg == nil {
		return fmt.Errorf("envtest not started")
	}
	return nil
}

//godogen:given ^the CRDs are installed$
func (s *Suite) theCRDsAreInstalled(_ context.Context) error {
	// CRDs are installed by envtest.Environment via CRDDirectoryPaths.
	return nil
}

//godogen:given ^the operator is running$
func (s *Suite) theOperatorIsRunning(_ context.Context) error {
	mgr, err := ctrl.NewManager(testCfg, ctrl.Options{
		Scheme:  testScheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{
				s.namespace: {},
			},
		},
	})
	if err != nil {
		return err
	}

	reconciler := &framework.Reconciler[*mock.ClientSecret]{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Provider: s.provider,
	}

	if err := reconciler.SetupWithManager(mgr, func(b *builder.Builder) {
		b.Named("clientsecret-" + s.namespace)
	}); err != nil {
		return err
	}

	mgrCtx, cancel := context.WithCancel(s.ctx)
	s.mgrCancel = cancel

	go func() {
		_ = mgr.Start(mgrCtx)
	}()

	return nil
}

//godogen:when ^I create a ClientSecret:$
func (s *Suite) iCreateAClientSecret(_ context.Context, doc *godog.DocString) error {
	var cs mock.ClientSecret
	if err := yaml.Unmarshal([]byte(doc.Content), &cs); err != nil {
		return err
	}
	cs.Namespace = s.namespace
	return s.k8sClient.Create(s.ctx, &cs)
}

//
//godogen:when ^I create a ClientSecret "([^"]*)" with:$
func (s *Suite) iCreateAClientSecretNamed(
	_ context.Context,
	name string,
	doc *godog.DocString,
) error {
	var cs mock.ClientSecret
	if err := yaml.Unmarshal([]byte(doc.Content), &cs); err != nil {
		return err
	}
	cs.Name = name
	cs.Namespace = s.namespace
	return s.k8sClient.Create(s.ctx, &cs)
}

//
//godogen:when ^I update the ClientSecret "([^"]*)" with:$
func (s *Suite) iUpdateTheClientSecretWith(
	_ context.Context,
	name string,
	doc *godog.DocString,
) error {
	var existing mock.ClientSecret
	if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
		Name:      name,
	}, &existing); err != nil {
		return err
	}

	var patch mock.ClientSecret
	if err := yaml.Unmarshal([]byte(doc.Content), &patch); err != nil {
		return err
	}

	existing.Spec = patch.Spec
	return s.k8sClient.Update(s.ctx, &existing)
}

//godogen:then ^the ClientSecret "([^"]*)" should have phase "([^"]*)"$
func (s *Suite) theClientSecretShouldHavePhase(_ context.Context, name, phase string) error {
	return s.theClientSecretShouldHavePhaseWithin(nil, name, phase, 30)
}

//
//godogen:then ^the ClientSecret "([^"]*)" should have phase "([^"]*)" within (\d+) seconds$
func (s *Suite) theClientSecretShouldHavePhaseWithin(
	_ context.Context,
	name, phase string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var lastPhase string
	for time.Now().Before(deadline) {
		var cs mock.ClientSecret
		if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
			Namespace: s.namespace,
			Name:      name,
		}, &cs); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		lastPhase = cs.Status.Phase
		if lastPhase == phase {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("ClientSecret %q phase is %q, expected %q", name, lastPhase, phase)
}

//godogen:then ^a Secret "([^"]*)" should exist$
func (s *Suite) aSecretShouldExist(_ context.Context, name string) error {
	var secret corev1.Secret
	return s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
		Name:      name,
	}, &secret)
}

//
//godogen:then ^the Secret "([^"]*)" should contain key "([^"]*)" with value "([^"]*)"$
func (s *Suite) theSecretShouldContainKeyWithValue(
	_ context.Context,
	name, key, value string,
) error {
	var secret corev1.Secret
	if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
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

//
//godogen:then ^the Secret "([^"]*)" should contain key "([^"]*)" with value "([^"]*)" within (\d+) seconds$
func (s *Suite) theSecretShouldContainKeyWithValueWithin(
	_ context.Context,
	name, key, value string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	for time.Now().Before(deadline) {
		var secret corev1.Secret
		if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
			Namespace: s.namespace,
			Name:      name,
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

//godogen:then ^the Secret "([^"]*)" should not exist$
func (s *Suite) theSecretShouldNotExist(_ context.Context, name string) error {
	var secret corev1.Secret
	err := s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
		Name:      name,
	}, &secret)
	if err == nil {
		return fmt.Errorf("secret %q exists but should not", name)
	}
	return client.IgnoreNotFound(err)
}

//godogen:when ^I delete the ClientSecret "([^"]*)"$
func (s *Suite) iDeleteTheClientSecret(_ context.Context, name string) error {
	cs := &mock.ClientSecret{}
	cs.Name = name
	cs.Namespace = s.namespace
	return s.k8sClient.Delete(s.ctx, cs)
}

//
//godogen:then ^the ClientSecret "([^"]*)" should not exist within (\d+) seconds$
func (s *Suite) theClientSecretShouldNotExistWithin(
	_ context.Context,
	name string,
	seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	for time.Now().Before(deadline) {
		var cs mock.ClientSecret
		err := s.k8sClient.Get(s.ctx, client.ObjectKey{
			Namespace: s.namespace,
			Name:      name,
		}, &cs)
		if err != nil {
			return client.IgnoreNotFound(err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("ClientSecret %q still exists after %d seconds", name, seconds)
}

//
//godogen:then ^the ClientSecret "([^"]*)" status should contain message "([^"]*)"$
func (s *Suite) theClientSecretStatusShouldContainMessage(
	_ context.Context,
	name, message string,
) error {
	var cs mock.ClientSecret
	if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
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

//
//godogen:then ^the mock provider should have received at least (\d+) provision calls$
func (s *Suite) theMockProviderShouldHaveReceivedAtLeastProvisionCalls(
	_ context.Context,
	count int,
) error {
	actual := s.provider.ProvisionCount
	if actual < count {
		return fmt.Errorf("expected at least %d provision calls, got %d", count, actual)
	}
	return nil
}

//
//godogen:then ^the mock provider should have received at least (\d+) provision calls within (\d+) seconds$
func (s *Suite) theMockProviderShouldHaveReceivedAtLeastProvisionCallsWithin(
	_ context.Context,
	count, seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var actual int
	for time.Now().Before(deadline) {
		actual = s.provider.ProvisionCount
		if actual >= count {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf(
		"expected at least %d provision calls, got %d after %d seconds",
		count,
		actual,
		seconds,
	)
}

//
//godogen:then ^the mock provider should have received at least (\d+) delete key calls within (\d+) seconds$
func (s *Suite) theMockProviderShouldHaveReceivedAtLeastDeleteKeyCallsWithin(
	_ context.Context,
	count, seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var actual int
	for time.Now().Before(deadline) {
		actual = len(s.provider.DeleteKeyCalls)
		if actual >= count {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf(
		"expected at least %d delete key calls, got %d after %d seconds",
		count,
		actual,
		seconds,
	)
}

//godogen:when ^I expire the credentials for ClientSecret "([^"]*)"$
func (s *Suite) iExpireTheCredentialsForClientSecret(_ context.Context, name string) error {
	var cs mock.ClientSecret
	if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
		Name:      name,
	}, &cs); err != nil {
		return err
	}

	expired := time.Now().Add(-time.Hour)
	for i := range cs.Status.ActiveKeys {
		cs.Status.ActiveKeys[i].ExpiresAt = metav1.NewTime(expired)
	}

	return s.k8sClient.Status().Update(s.ctx, &cs)
}

//
//godogen:then ^the ClientSecret "([^"]*)" should have (\d+) active keys$
func (s *Suite) theClientSecretShouldHaveActiveKeys(
	_ context.Context,
	name string,
	count int,
) error {
	var cs mock.ClientSecret
	if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
		Namespace: s.namespace,
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

//
//godogen:then ^the ClientSecret "([^"]*)" should have at least (\d+) active keys within (\d+) seconds$
func (s *Suite) theClientSecretShouldHaveAtLeastActiveKeysWithin(
	_ context.Context,
	name string,
	count, seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var lastCount int
	for time.Now().Before(deadline) {
		var cs mock.ClientSecret
		if err := s.k8sClient.Get(s.ctx, client.ObjectKey{
			Namespace: s.namespace,
			Name:      name,
		}, &cs); err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		lastCount = len(cs.Status.ActiveKeys)
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
