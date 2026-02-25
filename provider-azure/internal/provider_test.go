package internal

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
			json.NewEncoder(w).Encode(map[string]string{"ok": "true"})
		}))
		defer srv.Close()

		p := New(WithHTTPClient(srv.Client()), WithBaseURL(srv.URL))
		body, err := p.graphRequest(context.Background(), "POST", "/test", map[string]string{"key": "val"})
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
			w.Write([]byte(`{"id":"123"}`))
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
			w.Write([]byte(`{"error":"access denied"}`))
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

func TestGraphRequestWithBadCredentials(t *testing.T) {
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
