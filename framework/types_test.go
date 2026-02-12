package framework_test

import (
	"errors"
	"testing"
	"time"

	"github.com/lukasngl/valet/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestActiveKeys_Newest_Empty(t *testing.T) {
	var keys framework.ActiveKeys
	if keys.Newest() != nil {
		t.Error("expected nil for empty keys")
	}
}

func TestActiveKeys_Newest(t *testing.T) {
	now := time.Now()
	keys := framework.ActiveKeys{
		{KeyID: "old", CreatedAt: metav1.NewTime(now.Add(-2 * time.Hour))},
		{KeyID: "newest", CreatedAt: metav1.NewTime(now)},
		{KeyID: "middle", CreatedAt: metav1.NewTime(now.Add(-1 * time.Hour))},
	}
	got := keys.Newest()
	if got.KeyID != "newest" {
		t.Errorf("expected newest, got %s", got.KeyID)
	}
}

func TestActiveKeys_DropExpired(t *testing.T) {
	now := time.Now()
	keys := framework.ActiveKeys{
		{KeyID: "expired", ExpiresAt: metav1.NewTime(now.Add(-1 * time.Hour))},
		{KeyID: "valid", ExpiresAt: metav1.NewTime(now.Add(1 * time.Hour))},
		{KeyID: "also-expired", ExpiresAt: metav1.NewTime(now.Add(-2 * time.Hour))},
	}

	// Don't keep any expired keys (successful deletion).
	dropped := keys.DropExpired(now, func(framework.ActiveKey) bool { return false })

	if len(dropped) != 2 {
		t.Fatalf("expected 2 dropped, got %d", len(dropped))
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(keys))
	}
	if keys[0].KeyID != "valid" {
		t.Errorf("expected valid key to remain, got %s", keys[0].KeyID)
	}
}

func TestActiveKeys_DropExpired_None(t *testing.T) {
	now := time.Now()
	keys := framework.ActiveKeys{
		{KeyID: "valid", ExpiresAt: metav1.NewTime(now.Add(1 * time.Hour))},
	}

	dropped := keys.DropExpired(now, func(framework.ActiveKey) bool { return false })
	if len(dropped) != 0 {
		t.Errorf("expected no dropped, got %d", len(dropped))
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 remaining, got %d", len(keys))
	}
}

func TestActiveKeys_DropExpired_KeepOnFailure(t *testing.T) {
	now := time.Now()
	keys := framework.ActiveKeys{
		{KeyID: "fail-delete", ExpiresAt: metav1.NewTime(now.Add(-1 * time.Hour))},
		{KeyID: "ok-delete", ExpiresAt: metav1.NewTime(now.Add(-2 * time.Hour))},
		{KeyID: "valid", ExpiresAt: metav1.NewTime(now.Add(1 * time.Hour))},
	}

	// Keep "fail-delete" (simulating provider deletion failure).
	dropped := keys.DropExpired(now, func(k framework.ActiveKey) bool {
		return k.KeyID == "fail-delete"
	})

	if len(dropped) != 1 || dropped[0].KeyID != "ok-delete" {
		t.Fatalf("expected ok-delete dropped, got %v", dropped)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 remaining, got %d", len(keys))
	}
}

func TestActiveKey_NearExpiry_Fresh(t *testing.T) {
	now := time.Now()
	k := framework.ActiveKey{
		CreatedAt: metav1.NewTime(now),
		ExpiresAt: metav1.NewTime(now.Add(24 * time.Hour)),
	}
	if k.NearExpiry() {
		t.Error("expected fresh key to not be near expiry")
	}
}

func TestActiveKey_NearExpiry_Expired(t *testing.T) {
	now := time.Now()
	k := framework.ActiveKey{
		CreatedAt: metav1.NewTime(now.Add(-25 * time.Hour)),
		ExpiresAt: metav1.NewTime(now.Add(-1 * time.Hour)),
	}
	if !k.NearExpiry() {
		t.Error("expected expired key to be near expiry")
	}
}

func TestActiveKey_NearExpiry_WithinThreshold(t *testing.T) {
	now := time.Now()
	// 24h validity, 10% threshold = 2.4h, key expires in 1h → near expiry
	k := framework.ActiveKey{
		CreatedAt: metav1.NewTime(now.Add(-23 * time.Hour)),
		ExpiresAt: metav1.NewTime(now.Add(1 * time.Hour)),
	}
	if !k.NearExpiry() {
		t.Error("expected key within threshold to be near expiry")
	}
}

func TestClientSecretStatus_NeedsRenewal_NoKeys(t *testing.T) {
	s := framework.ClientSecretStatus{}
	if !s.NeedsRenewal(1, true) {
		t.Error("expected renewal when no active keys")
	}
}

func TestClientSecretStatus_NeedsRenewal_GenerationChanged(t *testing.T) {
	now := time.Now()
	s := framework.ClientSecretStatus{
		ObservedGeneration: 1,
		ActiveKeys: framework.ActiveKeys{
			{
				KeyID:     "k",
				CreatedAt: metav1.NewTime(now),
				ExpiresAt: metav1.NewTime(now.Add(24 * time.Hour)),
			},
		},
	}
	if !s.NeedsRenewal(2, true) {
		t.Error("expected renewal when generation changed")
	}
}

func TestClientSecretStatus_NeedsRenewal_SecretMissing(t *testing.T) {
	now := time.Now()
	s := framework.ClientSecretStatus{
		ObservedGeneration: 1,
		ActiveKeys: framework.ActiveKeys{
			{
				KeyID:     "k",
				CreatedAt: metav1.NewTime(now),
				ExpiresAt: metav1.NewTime(now.Add(24 * time.Hour)),
			},
		},
	}
	if !s.NeedsRenewal(1, false) {
		t.Error("expected renewal when secret has no data")
	}
}

func TestClientSecretStatus_NeedsRenewal_NotNeeded(t *testing.T) {
	now := time.Now()
	s := framework.ClientSecretStatus{
		ObservedGeneration: 1,
		ActiveKeys: framework.ActiveKeys{
			{
				KeyID:     "k",
				CreatedAt: metav1.NewTime(now),
				ExpiresAt: metav1.NewTime(now.Add(24 * time.Hour)),
			},
		},
	}
	if s.NeedsRenewal(1, true) {
		t.Error("expected no renewal when key is fresh and generation matches")
	}
}

func TestClientSecretStatus_RenewalDuration(t *testing.T) {
	now := time.Now()
	s := framework.ClientSecretStatus{
		ActiveKeys: framework.ActiveKeys{
			{
				KeyID:     "k",
				CreatedAt: metav1.NewTime(now),
				ExpiresAt: metav1.NewTime(now.Add(24 * time.Hour)),
			},
		},
	}
	d := s.RenewalDuration()
	if d <= 0 {
		t.Fatal("expected positive duration")
	}
	// 24h validity, 10% threshold = 2.4h → requeue after ~21.6h
	expected := 24*time.Hour - (24*time.Hour)/10
	tolerance := time.Minute
	if d < expected-tolerance || d > expected+tolerance {
		t.Errorf("expected ~%v, got %v", expected, d)
	}
}

func TestClientSecretStatus_RenewalDuration_NoKeys(t *testing.T) {
	s := framework.ClientSecretStatus{}
	if d := s.RenewalDuration(); d != 0 {
		t.Errorf("expected 0 for no keys, got %v", d)
	}
}

func TestClientSecretStatus_SetReady(t *testing.T) {
	now := time.Now()
	s := &framework.ClientSecretStatus{
		Phase:        framework.PhaseFailed,
		FailureCount: 3,
	}

	result := &framework.Result{
		KeyID:         "new-key",
		ProvisionedAt: now,
		ValidUntil:    now.Add(24 * time.Hour),
	}

	s.SetReady(2, result)

	if s.Phase != framework.PhaseReady {
		t.Errorf("expected phase Ready, got %s", s.Phase)
	}
	if s.ObservedGeneration != 2 {
		t.Errorf("expected observedGeneration 2, got %d", s.ObservedGeneration)
	}
	if s.CurrentKeyID != "new-key" {
		t.Errorf("expected currentKeyID new-key, got %s", s.CurrentKeyID)
	}
	if s.FailureCount != 0 {
		t.Errorf("expected failureCount 0, got %d", s.FailureCount)
	}
	if len(s.ActiveKeys) != 1 || s.ActiveKeys[0].KeyID != "new-key" {
		t.Errorf("expected 1 active key with ID new-key, got %v", s.ActiveKeys)
	}
	if len(s.Conditions) != 1 || s.Conditions[0].Status != metav1.ConditionTrue {
		t.Errorf("expected Ready=True condition, got %v", s.Conditions)
	}
}

func TestClientSecretStatus_SetFailed(t *testing.T) {
	s := &framework.ClientSecretStatus{}

	s.SetFailed(1, errors.New("something broke"))

	if s.Phase != framework.PhaseFailed {
		t.Errorf("expected phase Failed, got %s", s.Phase)
	}
	if s.FailureCount != 1 {
		t.Errorf("expected failureCount 1, got %d", s.FailureCount)
	}
	if s.LastFailureMessage != "something broke" {
		t.Errorf("expected failure message, got %s", s.LastFailureMessage)
	}
	if s.LastFailure == nil {
		t.Error("expected LastFailure to be set")
	}

	// Second failure increments count.
	s.SetFailed(1, errors.New("broke again"))
	if s.FailureCount != 2 {
		t.Errorf("expected failureCount 2, got %d", s.FailureCount)
	}
	if len(s.Conditions) != 1 || s.Conditions[0].Status != metav1.ConditionFalse {
		t.Errorf("expected Ready=False condition, got %v", s.Conditions)
	}
}
