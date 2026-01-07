package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/google/uuid"
	msgraphsdk "github.com/microsoftgraph/msgraph-sdk-go"
	graphapplications "github.com/microsoftgraph/msgraph-sdk-go/applications"
	graphmodels "github.com/microsoftgraph/msgraph-sdk-go/models"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func init() {
	register(&Azure{})
}

const (
	// AzureType is the identifier for the Azure provider.
	AzureType = "azure"

	// AzureDefaultValidity is the default secret validity duration.
	AzureDefaultValidity = 90 * 24 * time.Hour // 90 days
)

// AzureConfig defines the configuration for the Azure AD provider.
type AzureConfig struct {
	// ObjectID is the Azure AD application Object ID (required).
	ObjectID string `json:"objectId" jsonschema:"required,description=Azure AD application Object ID"`

	// Validity is how long the secret should be valid (e.g., "2160h" for 90 days).
	// Defaults to 2160h (90 days).
	Validity string `json:"validity,omitempty" jsonschema:"default=2160h,description=Secret validity duration"`

	// Template maps output keys to secret data keys using Go templates.
	// Available template variables: clientId, clientSecret, tenantId
	Template map[string]string `json:"template" jsonschema:"required,description=Template mapping for secret data. Available variables: clientId clientSecret tenantId"`
}

// azureConfigSchema holds the generated and compiled JSON Schema for AzureConfig.
var azureConfigSchema = MustSchema(&AzureConfig{})

// Azure provisions Azure AD client secrets using Microsoft Graph API.
type Azure struct {
	client   *msgraphsdk.GraphServiceClient
	initOnce sync.Once
	initErr  error
}

// initClient initializes the Azure client on first use.
func (a *Azure) initClient() error {
	a.initOnce.Do(func() {
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			a.initErr = fmt.Errorf("failed to create Azure credential: %w", err)
			return
		}

		client, err := msgraphsdk.NewGraphServiceClientWithCredentials(
			cred,
			[]string{"https://graph.microsoft.com/.default"},
		)
		if err != nil {
			a.initErr = fmt.Errorf("failed to create Graph client: %w", err)
			return
		}

		a.client = client
	})
	return a.initErr
}

// Type returns the provider identifier.
func (a *Azure) Type() string {
	return AzureType
}

// ConfigSchema returns the JSON Schema for Azure provider config.
func (a *Azure) ConfigSchema() *Schema {
	return azureConfigSchema
}

// Validate validates the Azure provider config.
func (a *Azure) Validate(rawConfig json.RawMessage) error {
	// JSON Schema validation
	if err := a.ConfigSchema().Validate(rawConfig); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	var config AzureConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Extended validation: template syntax
	for key, tmpl := range config.Template {
		if _, err := template.New(key).Parse(tmpl); err != nil {
			return fmt.Errorf("template %q: %w", key, err)
		}
	}

	// Extended validation: validity duration format
	if config.Validity != "" {
		if _, err := time.ParseDuration(config.Validity); err != nil {
			return fmt.Errorf("invalid validity duration %q: %w", config.Validity, err)
		}
	}

	return nil
}

// Provision creates a new client secret for an Azure AD application.
func (a *Azure) Provision(ctx context.Context, rawConfig json.RawMessage) (*Result, error) {
	if err := a.initClient(); err != nil {
		return nil, err
	}

	var config AzureConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	validity := AzureDefaultValidity
	if config.Validity != "" {
		parsed, err := time.ParseDuration(config.Validity)
		if err != nil {
			return nil, fmt.Errorf("invalid validity duration %q: %w", config.Validity, err)
		}
		validity = parsed
	}

	// Create the password credential
	now := time.Now()
	endDateTime := now.Add(validity)

	passwordCredential := graphmodels.NewPasswordCredential()
	passwordCredential.SetDisplayName(
		ptr(fmt.Sprintf("secret-manager-%s", now.Format("2006-01-02"))),
	)
	passwordCredential.SetEndDateTime(&endDateTime)

	requestBody := graphapplications.NewItemAddPasswordPostRequestBody()
	requestBody.SetPasswordCredential(passwordCredential)

	// Call Graph API to add the password
	passwordResult, err := a.client.Applications().
		ByApplicationId(config.ObjectID).
		AddPassword().
		Post(ctx, requestBody, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add password to application %s: %w", config.ObjectID, err)
	}

	secretText := passwordResult.GetSecretText()
	if secretText == nil || *secretText == "" {
		return nil, errors.New("no secret text returned from Graph API")
	}

	keyID := ""
	if passwordResult.GetKeyId() != nil {
		keyID = passwordResult.GetKeyId().String()
	}

	// Get the application to retrieve client ID
	app, err := a.client.Applications().ByApplicationId(config.ObjectID).Get(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get application %s: %w", config.ObjectID, err)
	}

	clientID := ""
	if app.GetAppId() != nil {
		clientID = *app.GetAppId()
	}

	// Note: Tenant ID cannot be retrieved from DefaultAzureCredential
	tenantID := ""

	// Render templates
	templateData := map[string]string{
		"ClientID":     clientID,
		"ClientSecret": *secretText,
		"TenantID":     tenantID,
	}

	data := make(map[string]string)
	for key, tmpl := range config.Template {
		rendered, err := renderTemplate(tmpl, templateData)
		if err != nil {
			return nil, fmt.Errorf("render template %q: %w", key, err)
		}
		data[key] = string(rendered)
	}

	return &Result{
		StringData:    data,
		ProvisionedAt: now,
		ValidUntil:    endDateTime,
		KeyID:         keyID,
	}, nil
}

// DeleteKey removes a password credential from an Azure AD application.
func (a *Azure) DeleteKey(ctx context.Context, rawConfig json.RawMessage, keyID string) error {
	if keyID == "" {
		return nil // Nothing to delete
	}

	if err := a.initClient(); err != nil {
		return err
	}

	var config AzureConfig
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if config.ObjectID == "" {
		return errors.New("objectId is required")
	}

	keyUUID, err := uuid.Parse(keyID)
	if err != nil {
		return fmt.Errorf("invalid key ID %q: %w", keyID, err)
	}

	requestBody := graphapplications.NewItemRemovePasswordPostRequestBody()
	requestBody.SetKeyId(&keyUUID)

	err = a.client.Applications().
		ByApplicationId(config.ObjectID).
		RemovePassword().
		Post(ctx, requestBody, nil)
	if err != nil {
		// Ignore "not found" errors - key may have been deleted manually
		if strings.Contains(err.Error(), "No password credential found") {
			log.FromContext(ctx).
				Info("key already deleted", "keyId", keyID, "objectId", config.ObjectID)
			return nil
		}
		return fmt.Errorf(
			"failed to remove password %s from application %s: %w",
			keyID,
			config.ObjectID,
			err,
		)
	}

	return nil
}

func renderTemplate(tmpl string, data map[string]string) ([]byte, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
