/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/lukasngl/client-secret-operator/pkg/adapter"
)

const (
	// AnnotationDomain is the base domain for all annotations.
	AnnotationDomain = "secret-manager.ngl.cx"

	// AnnotationManaged marks a Secret as managed by this operator.
	AnnotationManaged = AnnotationDomain + "/managed"

	// AnnotationType specifies which adapter to use (e.g., "azure").
	AnnotationType = AnnotationDomain + "/type"

	// AnnotationProvisionedAt records when the secret was provisioned.
	AnnotationProvisionedAt = AnnotationDomain + "/provisioned-at"

	// AnnotationValidUntil records when the secret expires.
	AnnotationValidUntil = AnnotationDomain + "/valid-until"

	// AnnotationStatus records the current status of the secret.
	AnnotationStatus = AnnotationDomain + "/status"

	// AnnotationError records error details if provisioning failed.
	AnnotationError = AnnotationDomain + "/error"

	// AnnotationManagedKeys stores JSON array of managed key IDs and their expiry.
	AnnotationManagedKeys = AnnotationDomain + "/managed-keys"

	// Finalizer for cleanup on deletion.
	Finalizer = AnnotationDomain + "/finalizer"

	// RenewalThreshold is how long before expiry to trigger renewal.
	RenewalThreshold = 7 * 24 * time.Hour // 7 days
)

// ManagedKey tracks a provisioned key for cleanup.
type ManagedKey struct {
	KeyID   string    `json:"keyId"`
	Expires time.Time `json:"expires"`
}

// SecretReconciler reconciles Secrets with secret-manager.ngl.cx/managed annotation
type SecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles Secrets annotated with secret-manager.ngl.cx/managed=true
func (r *SecretReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var secret corev1.Secret
	if err := r.Get(ctx, req.NamespacedName, &secret); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if secret.Annotations[AnnotationManaged] != "true" {
		return ctrl.Result{}, nil
	}

	log.Info("reconciling managed secret", "secret", req.NamespacedName)

	adapterType := secret.Annotations[AnnotationType]
	if adapterType == "" {
		return r.updateStatus(ctx, &secret, "error", fmt.Errorf("annotation %q is required", AnnotationType))
	}

	adp := adapter.Get(adapterType)
	if adp == nil {
		return r.updateStatus(ctx, &secret, "error", fmt.Errorf("unknown adapter type %q", adapterType))
	}

	adapterAnnotations := r.extractAdapterAnnotations(&secret, adapterType)

	// Handle deletion with finalizer
	if !secret.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &secret, adp, adapterAnnotations)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&secret, Finalizer) {
		controllerutil.AddFinalizer(&secret, Finalizer)
		if err := r.Update(ctx, &secret); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Clean up expired keys
	if err := r.cleanupExpiredKeys(ctx, &secret, adp, adapterAnnotations); err != nil {
		log.Error(err, "failed to cleanup expired keys")
		// Continue with reconciliation even if cleanup fails
	}

	// Check if renewal is needed
	if !r.needsRenewal(&secret) {
		validUntil := secret.Annotations[AnnotationValidUntil]
		if validUntil != "" {
			if t, err := time.Parse(time.RFC3339, validUntil); err == nil {
				requeueAfter := time.Until(t) - RenewalThreshold
				if requeueAfter > 0 {
					log.Info("secret still valid, requeuing", "requeueAfter", requeueAfter)
					return ctrl.Result{RequeueAfter: requeueAfter}, nil
				}
			}
		}
		return ctrl.Result{}, nil
	}

	// Provision new secret
	log.Info("provisioning secret", "adapter", adapterType)
	result, err := adp.Provision(ctx, adapterAnnotations)
	if err != nil {
		return r.updateStatus(ctx, &secret, "error", err)
	}

	// Update secret data
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	for k, v := range result.Data {
		secret.Data[k] = v
	}

	// Track the new key
	if result.KeyID != "" {
		if err := r.addManagedKey(ctx, &secret, result.KeyID, result.ValidUntil); err != nil {
			log.Error(err, "failed to track managed key")
		}
	}

	// Update status annotations
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}
	secret.Annotations[AnnotationStatus] = "ready"
	secret.Annotations[AnnotationProvisionedAt] = result.ProvisionedAt.Format(time.RFC3339)
	secret.Annotations[AnnotationValidUntil] = result.ValidUntil.Format(time.RFC3339)
	delete(secret.Annotations, AnnotationError)

	if err := r.Update(ctx, &secret); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update secret: %w", err)
	}

	requeueAfter := time.Until(result.ValidUntil) - RenewalThreshold
	if requeueAfter < time.Minute {
		requeueAfter = time.Minute
	}
	log.Info("secret provisioned successfully", "keyId", result.KeyID, "requeueAfter", requeueAfter)

	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// handleDeletion cleans up all managed keys and removes the finalizer.
func (r *SecretReconciler) handleDeletion(ctx context.Context, secret *corev1.Secret, adp adapter.Adapter, adapterAnnotations map[string]string) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(secret, Finalizer) {
		return ctrl.Result{}, nil
	}

	log.Info("cleaning up managed keys before deletion")

	keys := r.getManagedKeys(secret)
	for _, key := range keys {
		if err := adp.DeleteKey(ctx, adapterAnnotations, key.KeyID); err != nil {
			log.Error(err, "failed to delete key", "keyId", key.KeyID)
			// Continue trying to delete other keys
		} else {
			log.Info("deleted key", "keyId", key.KeyID)
		}
	}

	controllerutil.RemoveFinalizer(secret, Finalizer)
	if err := r.Update(ctx, secret); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
	}

	return ctrl.Result{}, nil
}

// cleanupExpiredKeys removes keys that have expired.
func (r *SecretReconciler) cleanupExpiredKeys(ctx context.Context, secret *corev1.Secret, adp adapter.Adapter, adapterAnnotations map[string]string) error {
	log := log.FromContext(ctx)

	keys := r.getManagedKeys(secret)
	now := time.Now()
	var activeKeys []ManagedKey

	for _, key := range keys {
		if key.Expires.Before(now) {
			log.Info("deleting expired key", "keyId", key.KeyID, "expired", key.Expires)
			if err := adp.DeleteKey(ctx, adapterAnnotations, key.KeyID); err != nil {
				log.Error(err, "failed to delete expired key", "keyId", key.KeyID)
				// Keep it in the list to retry later
				activeKeys = append(activeKeys, key)
			}
		} else {
			activeKeys = append(activeKeys, key)
		}
	}

	return r.setManagedKeys(secret, activeKeys)
}

// getManagedKeys parses the managed keys from the annotation.
func (r *SecretReconciler) getManagedKeys(secret *corev1.Secret) []ManagedKey {
	data := secret.Annotations[AnnotationManagedKeys]
	if data == "" {
		return nil
	}

	var keys []ManagedKey
	if err := json.Unmarshal([]byte(data), &keys); err != nil {
		return nil
	}
	return keys
}

// setManagedKeys updates the managed keys annotation.
func (r *SecretReconciler) setManagedKeys(secret *corev1.Secret, keys []ManagedKey) error {
	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}

	if len(keys) == 0 {
		delete(secret.Annotations, AnnotationManagedKeys)
		return nil
	}

	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	secret.Annotations[AnnotationManagedKeys] = string(data)
	return nil
}

// addManagedKey adds a new key to the managed keys list.
func (r *SecretReconciler) addManagedKey(ctx context.Context, secret *corev1.Secret, keyID string, expires time.Time) error {
	keys := r.getManagedKeys(secret)
	keys = append(keys, ManagedKey{
		KeyID:   keyID,
		Expires: expires,
	})
	return r.setManagedKeys(secret, keys)
}

// needsRenewal checks if the secret needs to be provisioned or renewed.
func (r *SecretReconciler) needsRenewal(secret *corev1.Secret) bool {
	if len(secret.Data) == 0 {
		return true
	}

	validUntil := secret.Annotations[AnnotationValidUntil]
	if validUntil == "" {
		return true
	}

	t, err := time.Parse(time.RFC3339, validUntil)
	if err != nil {
		return true
	}

	// Already expired
	if t.Before(time.Now()) {
		return true
	}

	// Calculate renewal threshold as minimum of configured threshold or 10% of validity period
	provisionedAt := secret.Annotations[AnnotationProvisionedAt]
	if provisionedAt != "" {
		if pt, err := time.Parse(time.RFC3339, provisionedAt); err == nil {
			validityDuration := t.Sub(pt)
			dynamicThreshold := validityDuration / 10 // 10% of validity
			if dynamicThreshold < RenewalThreshold {
				return time.Until(t) < dynamicThreshold
			}
		}
	}

	return time.Until(t) < RenewalThreshold
}

// extractAdapterAnnotations extracts annotations for a specific adapter type.
func (r *SecretReconciler) extractAdapterAnnotations(secret *corev1.Secret, adapterType string) map[string]string {
	prefix := adapterType + "." + AnnotationDomain + "/"
	result := make(map[string]string)

	for k, v := range secret.Annotations {
		if strings.HasPrefix(k, prefix) {
			key := strings.TrimPrefix(k, prefix)
			result[key] = v
		}
	}

	return result
}

// updateStatus updates the secret's status annotations.
func (r *SecretReconciler) updateStatus(ctx context.Context, secret *corev1.Secret, status string, err error) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if secret.Annotations == nil {
		secret.Annotations = make(map[string]string)
	}

	secret.Annotations[AnnotationStatus] = status
	if err != nil {
		secret.Annotations[AnnotationError] = err.Error()
		log.Error(err, "failed to provision secret")
	} else {
		delete(secret.Annotations, AnnotationError)
	}

	if updateErr := r.Update(ctx, secret); updateErr != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update secret status: %w", updateErr)
	}

	if err != nil {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			annotations := object.GetAnnotations()
			return annotations != nil && annotations[AnnotationManaged] == "true"
		})).
		Complete(r)
}
