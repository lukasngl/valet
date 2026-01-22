package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/google/uuid"
	"github.com/lukasngl/secret-manager/internal/adapter"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func init() {
	adapter.DefaultRegistry().Register(&Provider{})
}

const (
	// Type is the identifier for the Azure provider.
	Type = "azure"

	// DefaultValidity is the default secret validity duration.
	DefaultValidity = 90 * 24 * time.Hour // 90 days

	// graphBaseURL is the Microsoft Graph API base URL.
	graphBaseURL = "https://graph.microsoft.com/v1.0"
)

// Config defines the configuration for the Azure AD provider.
type Config struct {
	// ObjectID is the Azure AD application Object ID (required).
	ObjectID string `json:"objectId" jsonschema:"required,description=Azure AD application Object ID"`

	// Validity is how long the secret should be valid (e.g., "2160h" for 90 days).
	// Defaults to 2160h (90 days).
	Validity string `json:"validity,omitempty" jsonschema:"default=2160h,description=Secret validity duration"`

	// Template maps output keys to secret data keys using Go templates.
	// Available variables: ClientID, ClientSecret
	// Static values like TenantID can be hardcoded directly in the template.
	Template map[string]string `json:"template" jsonschema:"required,description=Template mapping for secret data. Available variables: ClientID ClientSecret"`
}

// azureConfigSchema holds the generated and compiled JSON Schema for AzureConfig.
var azureConfigSchema = adapter.MustSchema(&Config{})

const (
	// retryDelay is the wait time before retrying after a rate limit error.
	retryDelay = 500 * time.Millisecond
	// maxRetries is the maximum number of retries for rate-limited requests.
	maxRetries = 5
)

// Provider provisions Azure AD client secrets using Microsoft Graph API.
type Provider struct {
	cred      *azidentity.DefaultAzureCredential
	client    *http.Client
	initOnce  sync.Once
	initErr   error
	requestMu sync.Mutex // Serialize requests to avoid rate limiting
}

// initClient initializes the Azure client on first use.
func (a *Provider) initClient() error {
	a.initOnce.Do(func() {
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			a.initErr = fmt.Errorf("failed to create Azure credential: %w", err)
			return
		}
		a.cred = cred
		a.client = &http.Client{Timeout: 30 * time.Second}
	})
	return a.initErr
}

// graphRequest makes an authenticated request to Microsoft Graph API.
func (a *Provider) graphRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	token, err := a.cred.GetToken(ctx, policy.TokenRequestOptions{
		Scopes: []string{"https://graph.microsoft.com/.default"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get token: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, graphBaseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("graph API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// isRateLimitError checks if the error is a rate limiting error.
func isRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "concurrent") ||
		strings.Contains(msg, "throttl") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "status 429")
}

// withRetry executes the given function with retry logic for rate limiting errors.
func withRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var result T
	var err error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err = fn()
		if err == nil || !isRateLimitError(err) {
			return result, err
		}

		if attempt < maxRetries {
			log.FromContext(ctx).Info("rate limited, retrying",
				"attempt", attempt+1,
				"delay", retryDelay)
			time.Sleep(retryDelay)
		}
	}

	return result, err
}

// withRetryNoResult executes the given function with retry logic for rate limiting errors.
func withRetryNoResult(ctx context.Context, fn func() error) error {
	_, err := withRetry(ctx, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// Type returns the provider identifier.
func (a *Provider) Type() string {
	return Type
}

// ConfigSchema returns the JSON Schema for Azure provider config.
func (a *Provider) ConfigSchema() *adapter.Schema {
	return azureConfigSchema
}

// Validate validates the Azure provider config.
func (a *Provider) Validate(rawConfig json.RawMessage) error {
	// JSON Schema validation
	if err := a.ConfigSchema().Validate(rawConfig); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	var config Config
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

// addPasswordRequest is the request body for addPassword.
type addPasswordRequest struct {
	PasswordCredential passwordCredential `json:"passwordCredential"`
}

// passwordCredential represents a password credential.
type passwordCredential struct {
	DisplayName *string    `json:"displayName,omitempty"`
	EndDateTime *time.Time `json:"endDateTime,omitempty"`
}

// addPasswordResponse is the response from addPassword.
type addPasswordResponse struct {
	KeyID      string `json:"keyId"`
	SecretText string `json:"secretText"`
}

// applicationResponse is the response from getting an application.
type applicationResponse struct {
	AppID string `json:"appId"`
}

// removePasswordRequest is the request body for removePassword.
type removePasswordRequest struct {
	KeyID string `json:"keyId"`
}

// Provision creates a new client secret for an Azure AD application.
func (a *Provider) Provision(
	ctx context.Context,
	rawConfig json.RawMessage,
) (*adapter.Result, error) {
	if err := a.initClient(); err != nil {
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	validity := DefaultValidity
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
	displayName := fmt.Sprintf("secret-manager-%s", now.Format("2006-01-02"))

	reqBody := addPasswordRequest{
		PasswordCredential: passwordCredential{
			DisplayName: &displayName,
			EndDateTime: &endDateTime,
		},
	}

	// Serialize requests to avoid rate limiting
	a.requestMu.Lock()
	defer a.requestMu.Unlock()

	// Call Graph API to add the password with retry logic
	respBody, err := withRetry(ctx, func() ([]byte, error) {
		return a.graphRequest(ctx, "POST", "/applications/"+config.ObjectID+"/addPassword", reqBody)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to add password to application %s: %w", config.ObjectID, err)
	}

	var passwordResult addPasswordResponse
	if err := json.Unmarshal(respBody, &passwordResult); err != nil {
		return nil, fmt.Errorf("failed to parse addPassword response: %w", err)
	}

	if passwordResult.SecretText == "" {
		return nil, errors.New("no secret text returned from Graph API")
	}

	// Get the application to retrieve client ID
	appBody, err := withRetry(ctx, func() ([]byte, error) {
		return a.graphRequest(ctx, "GET", "/applications/"+config.ObjectID, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get application %s: %w", config.ObjectID, err)
	}

	var app applicationResponse
	if err := json.Unmarshal(appBody, &app); err != nil {
		return nil, fmt.Errorf("failed to parse application response: %w", err)
	}

	// Render templates
	templateData := map[string]string{
		"ClientID":     app.AppID,
		"ClientSecret": passwordResult.SecretText,
	}

	data := make(map[string]string)
	for key, tmpl := range config.Template {
		rendered, err := renderTemplate(tmpl, templateData)
		if err != nil {
			return nil, fmt.Errorf("render template %q: %w", key, err)
		}
		data[key] = string(rendered)
	}

	return &adapter.Result{
		StringData:    data,
		ProvisionedAt: now,
		ValidUntil:    endDateTime,
		KeyID:         passwordResult.KeyID,
	}, nil
}

// DeleteKey removes a password credential from an Azure AD application.
func (a *Provider) DeleteKey(ctx context.Context, rawConfig json.RawMessage, keyID string) error {
	if keyID == "" {
		return nil // Nothing to delete
	}

	if err := a.initClient(); err != nil {
		return err
	}

	var config Config
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	if config.ObjectID == "" {
		return errors.New("objectId is required")
	}

	if _, err := uuid.Parse(keyID); err != nil {
		return fmt.Errorf("invalid key ID %q: %w", keyID, err)
	}

	reqBody := removePasswordRequest{KeyID: keyID}

	// Serialize requests to avoid rate limiting
	a.requestMu.Lock()
	defer a.requestMu.Unlock()

	err := withRetryNoResult(ctx, func() error {
		_, err := a.graphRequest(ctx, "POST", "/applications/"+config.ObjectID+"/removePassword", reqBody)
		return err
	})
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
