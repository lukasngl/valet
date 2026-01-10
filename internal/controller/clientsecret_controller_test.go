package controller

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	secretmanagerv1alpha1 "github.com/lukasngl/secret-manager/api/v1alpha1"
	"github.com/lukasngl/secret-manager/internal/adapter"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// mockProvider implements adapter.Provider for testing.
type mockProvider struct {
	name   string
	schema *adapter.Schema
}

func (m *mockProvider) Type() string                  { return m.name }
func (m *mockProvider) ConfigSchema() *adapter.Schema { return m.schema }
func (m *mockProvider) Validate(json.RawMessage) error {
	return nil
}
func (m *mockProvider) Provision(context.Context, json.RawMessage) (*adapter.Result, error) {
	return &adapter.Result{
		StringData:    map[string]string{"key": "value"},
		ProvisionedAt: time.Now(),
		ValidUntil:    time.Now().Add(24 * time.Hour),
		KeyID:         "test-key-id",
	}, nil
}
func (m *mockProvider) DeleteKey(context.Context, json.RawMessage, string) error { return nil }

func newTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	_ = secretmanagerv1alpha1.AddToScheme(s)
	return s
}

func TestNeedsRenewal_EmptySecretData(t *testing.T) {
	scheme := newTestScheme()

	// Create a Secret with empty data
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{}, // Empty data
	}

	// Create a ClientSecret with active keys
	cs := &secretmanagerv1alpha1.ClientSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cs",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: secretmanagerv1alpha1.ClientSecretSpec{
			SecretRef: secretmanagerv1alpha1.SecretReference{Name: "test-secret"},
		},
		Status: secretmanagerv1alpha1.ClientSecretStatus{
			ObservedGeneration: 1,
			ActiveKeys: []secretmanagerv1alpha1.ActiveKey{
				{
					KeyID:     "key-1",
					CreatedAt: metav1.Now(),
					ExpiresAt: metav1.NewTime(time.Now().Add(24 * time.Hour)),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, cs).
		Build()

	r := &ClientSecretReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// needsRenewal should return true because Secret has empty data
	ctx := context.Background()
	needs := r.needsRenewal(ctx, cs)
	if !needs {
		t.Error("expected needsRenewal to return true for empty Secret data")
	}
}

func TestNeedsRenewal_SecretNotExists(t *testing.T) {
	scheme := newTestScheme()

	// Create a ClientSecret without a corresponding Secret
	cs := &secretmanagerv1alpha1.ClientSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cs",
			Namespace:  "default",
			Generation: 1,
		},
		Spec: secretmanagerv1alpha1.ClientSecretSpec{
			SecretRef: secretmanagerv1alpha1.SecretReference{Name: "nonexistent-secret"},
		},
		Status: secretmanagerv1alpha1.ClientSecretStatus{
			ObservedGeneration: 1,
			ActiveKeys: []secretmanagerv1alpha1.ActiveKey{
				{
					KeyID:     "key-1",
					CreatedAt: metav1.Now(),
					ExpiresAt: metav1.NewTime(time.Now().Add(24 * time.Hour)),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cs).
		Build()

	r := &ClientSecretReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// needsRenewal should return true because Secret doesn't exist
	ctx := context.Background()
	needs := r.needsRenewal(ctx, cs)
	if !needs {
		t.Error("expected needsRenewal to return true when Secret doesn't exist")
	}
}

func TestNeedsRenewal_GenerationChanged(t *testing.T) {
	scheme := newTestScheme()

	// Create a Secret with data
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "default",
		},
		Data: map[string][]byte{"key": []byte("value")},
	}

	// Create a ClientSecret with mismatched generation
	cs := &secretmanagerv1alpha1.ClientSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-cs",
			Namespace:  "default",
			Generation: 2, // Changed
		},
		Spec: secretmanagerv1alpha1.ClientSecretSpec{
			SecretRef: secretmanagerv1alpha1.SecretReference{Name: "test-secret"},
		},
		Status: secretmanagerv1alpha1.ClientSecretStatus{
			ObservedGeneration: 1, // Old generation
			ActiveKeys: []secretmanagerv1alpha1.ActiveKey{
				{
					KeyID:     "key-1",
					CreatedAt: metav1.Now(),
					ExpiresAt: metav1.NewTime(time.Now().Add(24 * time.Hour)),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, cs).
		Build()

	r := &ClientSecretReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	// needsRenewal should return true because generation changed
	ctx := context.Background()
	needs := r.needsRenewal(ctx, cs)
	if !needs {
		t.Error("expected needsRenewal to return true when generation changed")
	}
}

func TestScheduleNextRenewal_NoActiveKeys(t *testing.T) {
	cs := &secretmanagerv1alpha1.ClientSecret{
		Status: secretmanagerv1alpha1.ClientSecretStatus{
			ActiveKeys: []secretmanagerv1alpha1.ActiveKey{},
		},
	}

	r := &ClientSecretReconciler{}
	result := r.scheduleNextRenewal(cs)

	if !result.Requeue {
		t.Error("expected Requeue=true when no active keys")
	}
}

func TestScheduleNextRenewal_WithActiveKey(t *testing.T) {
	now := time.Now()
	cs := &secretmanagerv1alpha1.ClientSecret{
		Status: secretmanagerv1alpha1.ClientSecretStatus{
			ActiveKeys: []secretmanagerv1alpha1.ActiveKey{
				{
					KeyID:     "key-1",
					CreatedAt: metav1.NewTime(now),
					ExpiresAt: metav1.NewTime(now.Add(24 * time.Hour)),
				},
			},
		},
	}

	r := &ClientSecretReconciler{}
	result := r.scheduleNextRenewal(cs)

	if result.RequeueAfter <= 0 {
		t.Error("expected RequeueAfter > 0 when active key exists")
	}

	// Should requeue well before expiry (at least 10% before, max 7 days)
	expectedMaxDelay := 24*time.Hour - (24*time.Hour)/10
	if result.RequeueAfter > expectedMaxDelay {
		t.Errorf("expected RequeueAfter <= %v, got %v", expectedMaxDelay, result.RequeueAfter)
	}
}

func TestHandleDeletion_NoFinalizer(t *testing.T) {
	mock := &mockProvider{name: "mock", schema: adapter.MustSchema(struct{}{})}

	// Create a ClientSecret without finalizer - no fake client needed
	// since handleDeletion returns early before touching the client
	cs := &secretmanagerv1alpha1.ClientSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cs",
			Namespace: "default",
			// No finalizers - handleDeletion will return early
		},
	}

	r := &ClientSecretReconciler{}

	ctx := context.Background()
	result, err := r.handleDeletion(ctx, cs, mock)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if result.Requeue || result.RequeueAfter > 0 {
		t.Error("expected no requeue when no finalizer present")
	}
}

func TestReconcileOutputSecret_SetsOwnerReference(t *testing.T) {
	scheme := newTestScheme()

	cs := &secretmanagerv1alpha1.ClientSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cs",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: secretmanagerv1alpha1.ClientSecretSpec{
			SecretRef: secretmanagerv1alpha1.SecretReference{Name: "test-secret"},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(cs).
		Build()

	r := &ClientSecretReconciler{
		Client: fakeClient,
		Scheme: scheme,
	}

	result := &adapter.Result{
		StringData: map[string]string{"key": "value"},
	}

	ctx := context.Background()
	err := r.reconcileOutputSecret(ctx, cs, result)
	if err != nil {
		t.Fatalf("reconcileOutputSecret failed: %v", err)
	}

	// Verify the Secret was created with owner reference
	var secret corev1.Secret
	err = fakeClient.Get(ctx, client.ObjectKey{
		Namespace: "default",
		Name:      "test-secret",
	}, &secret)
	if err != nil {
		t.Fatalf("failed to get Secret: %v", err)
	}

	if len(secret.OwnerReferences) == 0 {
		t.Error("expected owner reference to be set")
	}

	if secret.OwnerReferences[0].UID != cs.UID {
		t.Error("owner reference UID mismatch")
	}
}
