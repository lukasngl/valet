package v1alpha1

import (
	"testing"
	"time"

	"github.com/lukasngl/valet/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidate(t *testing.T) {
	valid := &ClientSecret{
		Spec: ClientSecretSpec{
			SecretRef:  framework.SecretReference{Name: "out"},
			SecretData: map[string]string{"KEY": "value"},
		},
	}

	tests := []struct {
		name    string
		modify  func(*ClientSecret)
		wantErr string
	}{
		{name: "valid", modify: func(_ *ClientSecret) {}},
		{
			name:    "missing secretRef",
			modify:  func(c *ClientSecret) { c.Spec.SecretRef.Name = "" },
			wantErr: "secretRef.name",
		},
		{
			name:    "empty secretData",
			modify:  func(c *ClientSecret) { c.Spec.SecretData = nil },
			wantErr: "secretData",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := valid.DeepCopyObject().(*ClientSecret)
			tt.modify(obj)
			err := obj.Validate()

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tt.wantErr) {
				t.Fatalf("error %q does not contain %q", got, tt.wantErr)
			}
		})
	}
}

func TestGetSecretRef(t *testing.T) {
	obj := &ClientSecret{
		Spec: ClientSecretSpec{
			SecretRef: framework.SecretReference{Name: "my-secret"},
		},
	}
	if got := obj.GetSecretRef().Name; got != "my-secret" {
		t.Fatalf("GetSecretRef().Name = %q, want %q", got, "my-secret")
	}
}

func TestGetStatus(t *testing.T) {
	obj := &ClientSecret{}
	obj.Status.Phase = framework.PhaseReady
	if got := obj.GetStatus().Phase; got != framework.PhaseReady {
		t.Fatalf("GetStatus().Phase = %q, want %q", got, framework.PhaseReady)
	}
}

func TestGetValidity(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		obj := &ClientSecret{}
		if got := obj.GetValidity(); got != 24*time.Hour {
			t.Fatalf("GetValidity() = %v, want %v", got, 24*time.Hour)
		}
	})

	t.Run("custom", func(t *testing.T) {
		obj := &ClientSecret{
			Spec: ClientSecretSpec{
				Validity: &metav1.Duration{Duration: 48 * time.Hour},
			},
		}
		if got := obj.GetValidity(); got != 48*time.Hour {
			t.Fatalf("GetValidity() = %v, want %v", got, 48*time.Hour)
		}
	})
}

func TestDeepCopyObject(t *testing.T) {
	validity := metav1.Duration{Duration: 48 * time.Hour}
	obj := &ClientSecret{
		Spec: ClientSecretSpec{
			SecretRef:  framework.SecretReference{Name: "s"},
			SecretData: map[string]string{"K": "V"},
			Validity:   &validity,
		},
	}
	obj.Status.Phase = framework.PhaseReady

	cp := obj.DeepCopyObject().(*ClientSecret)

	// Verify independence.
	cp.Spec.SecretData["K"] = "changed"
	if obj.Spec.SecretData["K"] != "V" {
		t.Fatal("DeepCopyObject did not copy secretData map")
	}

	cp.Spec.Validity.Duration = time.Hour
	if obj.Spec.Validity.Duration != 48*time.Hour {
		t.Fatal("DeepCopyObject did not copy validity")
	}
}

func TestDeepCopyObjectList(t *testing.T) {
	list := &ClientSecretList{
		Items: []ClientSecret{
			{Spec: ClientSecretSpec{SecretData: map[string]string{"K": "V"}}},
		},
	}

	cp := list.DeepCopyObject().(*ClientSecretList)
	cp.Items[0].Spec.SecretData["K"] = "changed"
	if list.Items[0].Spec.SecretData["K"] != "V" {
		t.Fatal("DeepCopyObject did not deep copy list items")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
