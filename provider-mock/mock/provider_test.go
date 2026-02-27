package mock_test

import (
	"context"
	"testing"

	"github.com/lukasngl/valet/framework"
	"github.com/lukasngl/valet/provider-mock/api/v1alpha1"
	"github.com/lukasngl/valet/provider-mock/mock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestInstrumentedProvision(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		reg := prometheus.NewRegistry()
		p := framework.Instrument(mock.NewProvider(), reg)

		obj := &v1alpha1.ClientSecret{}
		obj.Spec.SecretData = map[string]string{"KEY": "val"}

		result, err := p.Provision(context.Background(), obj)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.KeyID == "" {
			t.Fatal("expected non-empty keyID")
		}
		if got := testutil.ToFloat64(p.ProvisionTotal.WithLabelValues("success")); got != 1 {
			t.Fatalf("provision_total{success} = %v, want 1", got)
		}
		if got := testutil.ToFloat64(p.ProvisionTotal.WithLabelValues("error")); got != 0 {
			t.Fatalf("provision_total{error} = %v, want 0", got)
		}
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		reg := prometheus.NewRegistry()
		p := framework.Instrument(mock.NewProvider(), reg)

		obj := &v1alpha1.ClientSecret{}
		obj.Spec.ShouldFailProvision = true

		_, err := p.Provision(context.Background(), obj)
		if err == nil {
			t.Fatal("expected error")
		}
		if got := testutil.ToFloat64(p.ProvisionTotal.WithLabelValues("error")); got != 1 {
			t.Fatalf("provision_total{error} = %v, want 1", got)
		}
		if got := testutil.ToFloat64(p.ProvisionTotal.WithLabelValues("success")); got != 0 {
			t.Fatalf("provision_total{success} = %v, want 0", got)
		}
	})
}

func TestInstrumentedDeleteKey(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		reg := prometheus.NewRegistry()
		p := framework.Instrument(mock.NewProvider(), reg)

		obj := &v1alpha1.ClientSecret{}
		if err := p.DeleteKey(context.Background(), obj, "key-1"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := testutil.ToFloat64(p.DeleteKeyTotal.WithLabelValues("success")); got != 1 {
			t.Fatalf("delete_key_total{success} = %v, want 1", got)
		}
	})

	t.Run("error", func(t *testing.T) {
		t.Parallel()
		reg := prometheus.NewRegistry()
		p := framework.Instrument(mock.NewProvider(), reg)

		obj := &v1alpha1.ClientSecret{}
		obj.Spec.ShouldFailDeleteKey = true

		if err := p.DeleteKey(context.Background(), obj, "key-1"); err == nil {
			t.Fatal("expected error")
		}
		if got := testutil.ToFloat64(p.DeleteKeyTotal.WithLabelValues("error")); got != 1 {
			t.Fatalf("delete_key_total{error} = %v, want 1", got)
		}
	})
}
