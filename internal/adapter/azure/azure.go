package azure

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/google/uuid"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	graphapplications "github.com/microsoftgraph/msgraph-sdk-go/applications"
	graphmodels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/lukasngl/client-secret-operator/pkg/adapter"
)

const (
	// AdapterType is the identifier for this adapter.
	AdapterType = "azure"

	// Annotation keys (without prefix)
	AnnotationObjectID = "object-id" // Azure AD application Object ID (required)
	AnnotationValidity = "validity"  // How long the secret should be valid

	// Target annotation suffix
	TargetSuffix = "-target"

	// Output keys (canonical names)
	OutputClientID     = "client-id"
	OutputClientSecret = "client-secret"
	OutputTenantID     = "tenant-id"

	// Default validity duration
	DefaultValidity = 90 * 24 * time.Hour // 90 days
)

// Adapter provisions Azure AD client secrets using Microsoft Graph API.
type Adapter struct {
	client *msgraphsdk.GraphServiceClient
}

// New creates a new Azure adapter using DefaultAzureCredential.
// This supports Azure CLI, environment variables, managed identity, etc.
func New() (*Adapter, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	client, err := msgraphsdk.NewGraphServiceClientWithCredentials(cred, []string{"https://graph.microsoft.com/.default"})
	if err != nil {
		return nil, fmt.Errorf("failed to create Graph client: %w", err)
	}

	return &Adapter{client: client}, nil
}

// Type returns the adapter identifier.
func (a *Adapter) Type() string {
	return AdapterType
}

// Provision creates a new client secret for an Azure AD application.
func (a *Adapter) Provision(ctx context.Context, annotations map[string]string) (*adapter.Result, error) {
	objectID := annotations[AnnotationObjectID]
	if objectID == "" {
		return nil, fmt.Errorf("annotation %q is required", AnnotationObjectID)
	}

	validity := DefaultValidity
	if v := annotations[AnnotationValidity]; v != "" {
		parsed, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid validity duration %q: %w", v, err)
		}
		validity = parsed
	}

	// Create the password credential
	now := time.Now()
	endDateTime := now.Add(validity)

	passwordCredential := graphmodels.NewPasswordCredential()
	passwordCredential.SetDisplayName(stringPtr(fmt.Sprintf("secret-manager-%s", now.Format("2006-01-02"))))
	passwordCredential.SetEndDateTime(&endDateTime)

	requestBody := graphapplications.NewItemAddPasswordPostRequestBody()
	requestBody.SetPasswordCredential(passwordCredential)

	// Call Graph API to add the password
	passwordResult, err := a.client.Applications().ByApplicationId(objectID).AddPassword().Post(ctx, requestBody, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add password to application %s: %w", objectID, err)
	}

	secretText := passwordResult.GetSecretText()
	if secretText == nil || *secretText == "" {
		return nil, fmt.Errorf("no secret text returned from Graph API")
	}

	keyID := ""
	if passwordResult.GetKeyId() != nil {
		keyID = passwordResult.GetKeyId().String()
	}

	// Get the application to retrieve client ID and tenant info
	app, err := a.client.Applications().ByApplicationId(objectID).Get(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get application %s: %w", objectID, err)
	}

	clientID := ""
	if app.GetAppId() != nil {
		clientID = *app.GetAppId()
	}

	// Get tenant ID from the credential
	// Note: We can't easily get tenant ID from DefaultAzureCredential,
	// so we'll need to get it from the application's publisher domain or leave it empty
	// For now, the user can specify it via a target annotation if needed
	tenantID := ""

	// Build result data based on target annotations
	adapterResult := &adapter.Result{
		Data:          make(map[string][]byte),
		ProvisionedAt: now,
		ValidUntil:    endDateTime,
		KeyID:         keyID,
	}

	outputs := map[string]string{
		OutputClientID:     clientID,
		OutputClientSecret: *secretText,
		OutputTenantID:     tenantID,
	}

	for outputKey, outputValue := range outputs {
		targetKey := annotations[outputKey+TargetSuffix]
		if targetKey == "" {
			continue
		}
		targetKey = strings.TrimPrefix(targetKey, "/data/")
		adapterResult.Data[targetKey] = []byte(outputValue)
	}

	return adapterResult, nil
}

// DeleteKey removes a password credential from an Azure AD application.
func (a *Adapter) DeleteKey(ctx context.Context, annotations map[string]string, keyID string) error {
	if keyID == "" {
		return nil // Nothing to delete
	}

	objectID := annotations[AnnotationObjectID]
	if objectID == "" {
		return fmt.Errorf("annotation %q is required", AnnotationObjectID)
	}

	keyUUID, err := uuid.Parse(keyID)
	if err != nil {
		return fmt.Errorf("invalid key ID %q: %w", keyID, err)
	}

	requestBody := graphapplications.NewItemRemovePasswordPostRequestBody()
	requestBody.SetKeyId(&keyUUID)

	err = a.client.Applications().ByApplicationId(objectID).RemovePassword().Post(ctx, requestBody, nil)
	if err != nil {
		// Ignore "not found" errors - key may have been deleted manually or already cleaned up
		if strings.Contains(err.Error(), "No password credential found") {
			log.FromContext(ctx).Info("key already deleted", "keyId", keyID, "objectId", objectID)
			return nil
		}
		return fmt.Errorf("failed to remove password %s from application %s: %w", keyID, objectID, err)
	}

	return nil
}

func stringPtr(s string) *string {
	return &s
}
