package v1alpha1

import (
	"testing"
	"time"

	"github.com/lukasngl/valet/framework"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidate(t *testing.T) {
	valid := &AzureClientSecret{
		Spec: AzureClientSecretSpec{
			SecretRef: framework.SecretReference{Name: "out"},
			ObjectID:  "obj-id",
			Template:  map[string]string{"KEY": "{{ .ClientSecret }}"},
		},
	}

	tests := []struct {
		name    string
		modify  func(*AzureClientSecret)
		wantErr string
	}{
		{name: "valid", modify: func(_ *AzureClientSecret) {}},
		{
			name:    "missing secretRef",
			modify:  func(a *AzureClientSecret) { a.Spec.SecretRef.Name = "" },
			wantErr: "secretRef.name",
		},
		{
			name:    "missing objectId",
			modify:  func(a *AzureClientSecret) { a.Spec.ObjectID = "" },
			wantErr: "objectId",
		},
		{
			name:    "empty template",
			modify:  func(a *AzureClientSecret) { a.Spec.Template = nil },
			wantErr: "template",
		},
		{
			name:    "invalid template syntax",
			modify:  func(a *AzureClientSecret) { a.Spec.Template = map[string]string{"bad": "{{ .Foo"} },
			wantErr: "template",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := valid.DeepCopyObject().(*AzureClientSecret)
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
	obj := &AzureClientSecret{
		Spec: AzureClientSecretSpec{
			SecretRef: framework.SecretReference{Name: "my-secret"},
		},
	}
	if got := obj.GetSecretRef().Name; got != "my-secret" {
		t.Fatalf("GetSecretRef().Name = %q, want %q", got, "my-secret")
	}
}

func TestGetStatus(t *testing.T) {
	obj := &AzureClientSecret{}
	obj.Status.Phase = "Ready"
	if got := obj.GetStatus().Phase; got != "Ready" {
		t.Fatalf("GetStatus().Phase = %q, want %q", got, "Ready")
	}
}

func TestDeepCopyObject(t *testing.T) {
	validity := metav1.Duration{Duration: 48 * time.Hour}
	obj := &AzureClientSecret{
		Spec: AzureClientSecretSpec{
			SecretRef: framework.SecretReference{Name: "s"},
			ObjectID:  "id",
			Template:  map[string]string{"K": "V"},
			Validity:  &validity,
		},
	}
	obj.Status.Phase = "Ready"

	cp := obj.DeepCopyObject().(*AzureClientSecret)

	// Verify independence.
	cp.Spec.Template["K"] = "changed"
	if obj.Spec.Template["K"] != "V" {
		t.Fatal("DeepCopyObject did not copy template map")
	}

	cp.Spec.Validity.Duration = time.Hour
	if obj.Spec.Validity.Duration != 48*time.Hour {
		t.Fatal("DeepCopyObject did not copy validity")
	}
}

func TestDeepCopyObjectList(t *testing.T) {
	list := &AzureClientSecretList{
		Items: []AzureClientSecret{
			{Spec: AzureClientSecretSpec{ObjectID: "a"}},
		},
	}

	cp := list.DeepCopyObject().(*AzureClientSecretList)
	cp.Items[0].Spec.ObjectID = "changed"
	if list.Items[0].Spec.ObjectID != "a" {
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
