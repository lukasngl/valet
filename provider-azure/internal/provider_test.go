package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukasngl/valet/provider-azure/api/v1alpha1"
)

func TestIsRateLimitError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("something else"), false},
		{errors.New("concurrent request detected"), true},
		{errors.New("request was throttled"), true},
		{errors.New("rate limit exceeded"), true},
		{errors.New("too many requests"), true},
		{errors.New("graph API error (status 429): retry later"), true},
	}

	for _, tt := range tests {
		name := "<nil>"
		if tt.err != nil {
			name = tt.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			if got := isRateLimitError(tt.err); got != tt.want {
				t.Fatalf("isRateLimitError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestWithRetry(t *testing.T) {
	t.Run("succeeds immediately", func(t *testing.T) {
		calls := 0
		result, err := withRetry(context.Background(), func() (string, error) {
			calls++
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "ok" {
			t.Fatalf("got %q, want %q", result, "ok")
		}
		if calls != 1 {
			t.Fatalf("expected 1 call, got %d", calls)
		}
	})

	t.Run("non-retryable error stops immediately", func(t *testing.T) {
		calls := 0
		_, err := withRetry(context.Background(), func() (string, error) {
			calls++
			return "", errors.New("permanent error")
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if calls != 1 {
			t.Fatalf("expected 1 call, got %d", calls)
		}
	})

	t.Run("retries on rate limit", func(t *testing.T) {
		calls := 0
		result, err := withRetry(context.Background(), func() (string, error) {
			calls++
			if calls < 3 {
				return "", errors.New("too many requests")
			}
			return "recovered", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "recovered" {
			t.Fatalf("got %q, want %q", result, "recovered")
		}
		if calls != 3 {
			t.Fatalf("expected 3 calls, got %d", calls)
		}
	})
}

func TestGraphRequest(t *testing.T) {
	t.Run("successful POST with body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			if ct := r.Header.Get("Content-Type"); ct != "application/json" {
				t.Fatalf("expected application/json, got %s", ct)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		body, err := p.graphRequest(
			context.Background(),
			"POST",
			"/test",
			map[string]string{"key": "val"},
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(body), "ok") {
			t.Fatalf("unexpected body: %s", body)
		}
	})

	t.Run("successful GET without body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Fatalf("expected GET, got %s", r.Method)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"123"}`))
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		body, err := p.graphRequest(context.Background(), "GET", "/apps/123", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(string(body), "123") {
			t.Fatalf("unexpected body: %s", body)
		}
	})

	t.Run("HTTP error response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"access denied"}`))
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := p.graphRequest(context.Background(), "GET", "/secret", nil)
		if err == nil {
			t.Fatal("expected error for 403 response")
		}
		if !strings.Contains(err.Error(), "status 403") {
			t.Fatalf("expected status 403 in error, got: %v", err)
		}
	})

	t.Run("request failure", func(t *testing.T) {
		p := New(WithHTTPClient(&http.Client{}), WithBaseURL("http://127.0.0.1:1"))
		_, err := p.graphRequest(context.Background(), "GET", "/test", nil)
		if err == nil {
			t.Fatal("expected connection error")
		}
		if !strings.Contains(err.Error(), "request failed") {
			t.Fatalf("expected 'request failed' in error, got: %v", err)
		}
	})
}

func TestInitClient(t *testing.T) {
	t.Run("skips when client is pre-configured", func(t *testing.T) {
		p := New(WithHTTPClient(&http.Client{}))
		if err := p.initClient(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.cred != nil {
			t.Fatal("expected cred to remain nil with pre-configured client")
		}
	})

	t.Run("initializes credential from environment", func(t *testing.T) {
		t.Setenv("AZURE_TENANT_ID", "fake-tenant")
		t.Setenv("AZURE_CLIENT_ID", "fake-client")
		t.Setenv("AZURE_CLIENT_SECRET", "fake-secret")

		p := New()
		// initClient succeeds — credential creation is lazy.
		if err := p.initClient(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.cred == nil {
			t.Fatal("expected cred to be initialized")
		}
		if p.client == nil {
			t.Fatal("expected HTTP client to be created")
		}
	})
}

// TestE2E groups tests that require network access (e.g. Azure AD).
// Skipped with -short; targeted by the e2e nix app via -run TestE2E.
func TestE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e tests in short mode")
	}

	t.Run("bad credentials", func(t *testing.T) {
		t.Setenv("AZURE_TENANT_ID", "fake-tenant")
		t.Setenv("AZURE_CLIENT_ID", "fake-client")
		t.Setenv("AZURE_CLIENT_SECRET", "fake-secret")

		p := New()
		if err := p.initClient(); err != nil {
			t.Fatalf("unexpected initClient error: %v", err)
		}

		// graphRequest calls GetToken which contacts Azure AD and fails.
		_, err := p.graphRequest(context.Background(), "GET", "/test", nil)
		if err == nil {
			t.Fatal("expected token acquisition error")
		}
		if !strings.Contains(err.Error(), "getting token") {
			t.Fatalf("expected 'getting token' error, got: %v", err)
		}
	})
}

func TestProvision(t *testing.T) {
	newObj := func(objectID string, template map[string]string) *v1alpha1.AzureClientSecret {
		return &v1alpha1.AzureClientSecret{
			Spec: v1alpha1.AzureClientSecretSpec{
				ObjectID: objectID,
				Template: template,
			},
		}
	}

	t.Run("happy path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/addPassword") {
				_ = json.NewEncoder(w).Encode(addPasswordResponse{
					KeyID: "key-1", SecretText: "s3cret",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(applicationResponse{AppID: "app-123"})
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		obj := newObj("obj-1", map[string]string{
			"CLIENT_ID":     "{{ .ClientID }}",
			"CLIENT_SECRET": "{{ .ClientSecret }}",
		})

		result, err := p.Provision(context.Background(), obj)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.KeyID != "key-1" {
			t.Fatalf("got keyID %q, want %q", result.KeyID, "key-1")
		}
		if result.StringData["CLIENT_ID"] != "app-123" {
			t.Fatalf("got CLIENT_ID %q, want %q", result.StringData["CLIENT_ID"], "app-123")
		}
		if result.StringData["CLIENT_SECRET"] != "s3cret" {
			t.Fatalf("got CLIENT_SECRET %q, want %q", result.StringData["CLIENT_SECRET"], "s3cret")
		}
	})

	t.Run("empty secret text", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(addPasswordResponse{KeyID: "key-1", SecretText: ""})
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := p.Provision(context.Background(), newObj("obj-1", map[string]string{"K": "v"}))
		if err == nil {
			t.Fatal("expected error for empty secret text")
		}
		if !strings.Contains(err.Error(), "no secret text") {
			t.Fatalf("expected 'no secret text' error, got: %v", err)
		}
	})

	t.Run("bad addPassword JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := p.Provision(context.Background(), newObj("obj-1", map[string]string{"K": "v"}))
		if err == nil {
			t.Fatal("expected unmarshal error")
		}
		if !strings.Contains(err.Error(), "parsing addPassword response") {
			t.Fatalf("expected 'parsing addPassword response' error, got: %v", err)
		}
	})

	t.Run("bad application JSON", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/addPassword") {
				_ = json.NewEncoder(w).Encode(addPasswordResponse{
					KeyID: "key-1", SecretText: "s3cret",
				})
				return
			}
			_, _ = w.Write([]byte(`not json`))
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := p.Provision(context.Background(), newObj("obj-1", map[string]string{"K": "v"}))
		if err == nil {
			t.Fatal("expected unmarshal error")
		}
		if !strings.Contains(err.Error(), "parsing application response") {
			t.Fatalf("expected 'parsing application response' error, got: %v", err)
		}
	})

	t.Run("bad template", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/addPassword") {
				_ = json.NewEncoder(w).Encode(addPasswordResponse{
					KeyID: "key-1", SecretText: "s3cret",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(applicationResponse{AppID: "app-123"})
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		_, err := p.Provision(context.Background(), newObj("obj-1", map[string]string{
			"BAD": "{{ .Unclosed",
		}))
		if err == nil {
			t.Fatal("expected template error")
		}
		if !strings.Contains(err.Error(), "rendering template") {
			t.Fatalf("expected 'rendering template' error, got: %v", err)
		}
	})
}

func TestDeleteKey(t *testing.T) {
	t.Run("empty keyID is a no-op", func(t *testing.T) {
		p := New(WithHTTPClient(&http.Client{}))
		if err := p.DeleteKey(context.Background(), &v1alpha1.AzureClientSecret{}, ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("happy path", func(t *testing.T) {
		var called bool
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		obj := &v1alpha1.AzureClientSecret{
			Spec: v1alpha1.AzureClientSecretSpec{ObjectID: "obj-1"},
		}
		if err := p.DeleteKey(context.Background(), obj, "key-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !called {
			t.Fatal("expected server to be called")
		}
	})

	t.Run("already deleted is idempotent", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"No password credential found"}`))
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		obj := &v1alpha1.AzureClientSecret{
			Spec: v1alpha1.AzureClientSecretSpec{ObjectID: "obj-1"},
		}
		if err := p.DeleteKey(context.Background(), obj, "gone-key"); err != nil {
			t.Fatalf("expected nil for already-deleted key, got: %v", err)
		}
	})

	t.Run("other error propagates", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal"}`))
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		obj := &v1alpha1.AzureClientSecret{
			Spec: v1alpha1.AzureClientSecretSpec{ObjectID: "obj-1"},
		}
		err := p.DeleteKey(context.Background(), obj, "key-1")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "removing password") {
			t.Fatalf("expected 'removing password' error, got: %v", err)
		}
	})
}

func TestRenderTemplate(t *testing.T) {
	data := map[string]string{"ClientID": "id-123", "ClientSecret": "secret-456"}

	t.Run("valid", func(t *testing.T) {
		got, err := renderTemplate("{{ .ClientID }}", data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "id-123" {
			t.Fatalf("got %q, want %q", got, "id-123")
		}
	})

	t.Run("parse error", func(t *testing.T) {
		_, err := renderTemplate("{{ .Unclosed", data)
		if err == nil {
			t.Fatal("expected parse error")
		}
	})

	t.Run("execute error", func(t *testing.T) {
		// Calling a method on a string triggers an execute error.
		_, err := renderTemplate("{{ .ClientID.Missing }}", data)
		if err == nil {
			t.Fatal("expected execute error")
		}
	})
}
