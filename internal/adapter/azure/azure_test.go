package azure

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestProvision_SmokeTest(t *testing.T) {
	objectID := os.Getenv("TEST_AZURE_OBJECT_ID")
	if objectID == "" {
		t.Skip("TEST_AZURE_OBJECT_ID not set, skipping smoke test")
	}

	adapter, err := New()
	if err != nil {
		t.Fatalf("failed to create adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	annotations := map[string]string{
		"object-id":            objectID,
		"validity":             "1h", // Short validity for testing
		"client-id-target":     "/data/CLIENT_ID",
		"client-secret-target": "/data/CLIENT_SECRET",
	}

	result, err := adapter.Provision(ctx, annotations)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	// Verify result
	if result == nil {
		t.Fatal("result is nil")
	}

	if len(result.Data) == 0 {
		t.Fatal("result.Data is empty")
	}

	clientID, ok := result.Data["CLIENT_ID"]
	if !ok || len(clientID) == 0 {
		t.Error("CLIENT_ID not in result data")
	} else {
		t.Logf("CLIENT_ID: %s", string(clientID))
	}

	clientSecret, ok := result.Data["CLIENT_SECRET"]
	if !ok || len(clientSecret) == 0 {
		t.Error("CLIENT_SECRET not in result data")
	} else {
		t.Logf("CLIENT_SECRET: %s... (truncated)", string(clientSecret)[:8])
	}

	t.Logf("ProvisionedAt: %s", result.ProvisionedAt)
	t.Logf("ValidUntil: %s", result.ValidUntil)

	if result.ValidUntil.Before(time.Now()) {
		t.Error("ValidUntil is in the past")
	}
}

func TestListSecrets(t *testing.T) {
	objectID := os.Getenv("TEST_AZURE_OBJECT_ID")
	if objectID == "" {
		t.Skip("TEST_AZURE_OBJECT_ID not set, skipping")
	}

	adapter, err := New()
	if err != nil {
		t.Fatalf("failed to create adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	app, err := adapter.client.Applications().ByApplicationId(objectID).Get(ctx, nil)
	if err != nil {
		t.Fatalf("failed to get application: %v", err)
	}

	creds := app.GetPasswordCredentials()
	t.Logf("Application has %d password credentials:", len(creds))
	for i, cred := range creds {
		displayName := "<no name>"
		if cred.GetDisplayName() != nil {
			displayName = *cred.GetDisplayName()
		}
		keyID := "<no id>"
		if cred.GetKeyId() != nil {
			keyID = cred.GetKeyId().String()
		}
		endDateTime := "<no expiry>"
		if cred.GetEndDateTime() != nil {
			endDateTime = cred.GetEndDateTime().Format(time.RFC3339)
		}
		t.Logf("  [%d] %s (keyId: %s, expires: %s)", i, displayName, keyID, endDateTime)
	}
}

func TestProvisionAndDelete(t *testing.T) {
	objectID := os.Getenv("TEST_AZURE_OBJECT_ID")
	if objectID == "" {
		t.Skip("TEST_AZURE_OBJECT_ID not set, skipping")
	}

	adapter, err := New()
	if err != nil {
		t.Fatalf("failed to create adapter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	annotations := map[string]string{
		"object-id":            objectID,
		"validity":             "1h",
		"client-id-target":     "/data/CLIENT_ID",
		"client-secret-target": "/data/CLIENT_SECRET",
	}

	// Provision
	result, err := adapter.Provision(ctx, annotations)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if result.KeyID == "" {
		t.Fatal("KeyID is empty")
	}
	t.Logf("Created key: %s", result.KeyID)

	// Delete
	err = adapter.DeleteKey(ctx, annotations, result.KeyID)
	if err != nil {
		t.Fatalf("DeleteKey failed: %v", err)
	}
	t.Logf("Deleted key: %s", result.KeyID)

	// Verify deletion by trying to delete again (should fail or no-op)
	// Azure returns an error if the key doesn't exist
	err = adapter.DeleteKey(ctx, annotations, result.KeyID)
	if err == nil {
		t.Log("Second delete succeeded (key might have already been removed)")
	} else {
		t.Logf("Second delete failed as expected: %v", err)
	}
}
