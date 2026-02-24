// Package internal contains the Azure provider implementation.
package internal

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
	"github.com/lukasngl/valet/framework"
	"github.com/lukasngl/valet/provider-azure/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultValidity is the default secret validity duration (90 days).
	DefaultValidity = 90 * 24 * time.Hour

	// graphBaseURL is the Microsoft Graph API base URL.
	graphBaseURL = "https://graph.microsoft.com/v1.0"

	// retryDelay is the wait time before retrying after a rate limit error.
	retryDelay = 500 * time.Millisecond

	// maxRetries is the maximum number of retries for rate-limited requests.
	maxRetries = 5
)

// Provider provisions Azure AD client secrets using Microsoft Graph API.
// It implements [framework.Provider] for [*v1alpha1.AzureClientSecret].
type Provider struct {
	cred      *azidentity.DefaultAzureCredential
	client    *http.Client
	baseURL   string
	initOnce  sync.Once
	initErr   error
	requestMu sync.Mutex // Serialize requests to avoid rate limiting.
}

// Option configures a [Provider].
type Option func(*Provider)

// WithHTTPClient sets a custom HTTP client, skipping Azure credential
// initialization. Useful for testing with a mock transport.
func WithHTTPClient(c *http.Client) Option {
	return func(p *Provider) { p.client = c }
}

// WithBaseURL overrides the Microsoft Graph API base URL.
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// New creates a [Provider] with the given options.
func New(opts ...Option) *Provider {
	p := &Provider{baseURL: graphBaseURL}
	for _, o := range opts {
		o(p)
	}
	return p
}

// NewObject returns a zero-value AzureClientSecret.
func (p *Provider) NewObject() *v1alpha1.AzureClientSecret {
	return &v1alpha1.AzureClientSecret{}
}

// Provision creates a new client secret for an Azure AD application.
func (p *Provider) Provision(
	ctx context.Context,
	obj *v1alpha1.AzureClientSecret,
) (*framework.Result, error) {
	if err := p.initClient(); err != nil {
		return nil, err
	}

	validity := DefaultValidity
	if obj.Spec.Validity != nil {
		validity = obj.Spec.Validity.Duration
	}

	now := time.Now()
	endDateTime := now.Add(validity)
	displayName := fmt.Sprintf("valet-%s", now.Format("2006-01-02"))

	reqBody := addPasswordRequest{
		PasswordCredential: passwordCredential{
			DisplayName: &displayName,
			EndDateTime: &endDateTime,
		},
	}

	// Serialize requests to avoid rate limiting.
	p.requestMu.Lock()
	defer p.requestMu.Unlock()

	respBody, err := withRetry(ctx, func() ([]byte, error) {
		return p.graphRequest(
			ctx,
			"POST",
			"/applications/"+obj.Spec.ObjectID+"/addPassword",
			reqBody,
		)
	})
	if err != nil {
		return nil, fmt.Errorf("adding password to application %s: %w", obj.Spec.ObjectID, err)
	}

	var passwordResult addPasswordResponse
	if err := json.Unmarshal(respBody, &passwordResult); err != nil {
		return nil, fmt.Errorf("parsing addPassword response: %w", err)
	}

	if passwordResult.SecretText == "" {
		return nil, errors.New("no secret text returned from Graph API")
	}

	// Get the application to retrieve client ID.
	appBody, err := withRetry(ctx, func() ([]byte, error) {
		return p.graphRequest(ctx, "GET", "/applications/"+obj.Spec.ObjectID, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("getting application %s: %w", obj.Spec.ObjectID, err)
	}

	var app applicationResponse
	if err := json.Unmarshal(appBody, &app); err != nil {
		return nil, fmt.Errorf("parsing application response: %w", err)
	}

	// Render templates.
	templateData := map[string]string{
		"ClientID":     app.AppID,
		"ClientSecret": passwordResult.SecretText,
	}

	data := make(map[string]string, len(obj.Spec.Template))
	for key, tmpl := range obj.Spec.Template {
		rendered, err := renderTemplate(tmpl, templateData)
		if err != nil {
			return nil, fmt.Errorf("rendering template %q: %w", key, err)
		}
		data[key] = rendered
	}

	return &framework.Result{
		StringData:    data,
		ProvisionedAt: now,
		ValidUntil:    endDateTime,
		KeyID:         passwordResult.KeyID,
	}, nil
}

// DeleteKey removes a password credential from an Azure AD application.
// Returns nil if the key has already been deleted (idempotent).
func (p *Provider) DeleteKey(
	ctx context.Context,
	obj *v1alpha1.AzureClientSecret,
	keyID string,
) error {
	if keyID == "" {
		return nil
	}

	if err := p.initClient(); err != nil {
		return err
	}

	reqBody := removePasswordRequest{KeyID: keyID}

	p.requestMu.Lock()
	defer p.requestMu.Unlock()

	err := withRetryNoResult(ctx, func() error {
		_, err := p.graphRequest(
			ctx,
			"POST",
			"/applications/"+obj.Spec.ObjectID+"/removePassword",
			reqBody,
		)
		return err
	})
	if err != nil {
		// Key already deleted at the provider — not an error.
		if strings.Contains(err.Error(), "No password credential found") {
			log.FromContext(ctx).
				Info("key already deleted", "keyId", keyID, "objectId", obj.Spec.ObjectID)
			return nil
		}
		return fmt.Errorf(
			"removing password %s from application %s: %w",
			keyID,
			obj.Spec.ObjectID,
			err,
		)
	}

	return nil
}

// initClient initializes the Azure credential and HTTP client on first use.
// If the client was pre-configured via [WithHTTPClient], initialization is
// skipped (no Azure credentials required).
func (p *Provider) initClient() error {
	p.initOnce.Do(func() {
		if p.client != nil {
			return // pre-configured, e.g. for testing
		}
		cred, err := azidentity.NewDefaultAzureCredential(nil)
		if err != nil {
			p.initErr = fmt.Errorf("creating Azure credential: %w", err)
			return
		}
		p.cred = cred
		p.client = &http.Client{Timeout: 30 * time.Second}
	})
	return p.initErr
}

// graphRequest makes an authenticated request to Microsoft Graph API.
func (p *Provider) graphRequest(
	ctx context.Context,
	method, path string,
	body any,
) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshalling request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if p.cred != nil {
		token, err := p.cred.GetToken(ctx, policy.TokenRequestOptions{
			Scopes: []string{"https://graph.microsoft.com/.default"},
		})
		if err != nil {
			return nil, fmt.Errorf("getting token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.Token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("graph API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Graph API request/response types.

type addPasswordRequest struct {
	PasswordCredential passwordCredential `json:"passwordCredential"`
}

type passwordCredential struct {
	DisplayName *string    `json:"displayName,omitempty"`
	EndDateTime *time.Time `json:"endDateTime,omitempty"`
}

type addPasswordResponse struct {
	KeyID      string `json:"keyId"`
	SecretText string `json:"secretText"`
}

type applicationResponse struct {
	AppID string `json:"appId"`
}

type removePasswordRequest struct {
	KeyID string `json:"keyId"`
}

// Retry helpers.

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

// withRetry executes fn with retry logic for rate limiting errors.
func withRetry[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var result T
	var err error

	for attempt := range maxRetries + 1 {
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

// withRetryNoResult executes fn with retry logic for rate limiting errors.
func withRetryNoResult(ctx context.Context, fn func() error) error {
	_, err := withRetry(ctx, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// renderTemplate renders a Go template string with the given data.
func renderTemplate(tmpl string, data map[string]string) (string, error) {
	t, err := template.New("").Parse(tmpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
