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
	"fmt"
	"time"

	secretmanagerv1alpha1 "github.com/lukasngl/client-secret-operator/api/v1alpha1"
	"github.com/lukasngl/client-secret-operator/internal/adapter"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// Finalizer for cleanup on deletion.
	Finalizer = "secret-manager.ngl.cx/finalizer"

	// RenewalThreshold is how long before expiry to trigger renewal.
	RenewalThreshold = 7 * 24 * time.Hour // 7 days

	// Condition types
	ConditionReady = "Ready"

	// Phase values
	PhasePending = "Pending"
	PhaseReady   = "Ready"
	PhaseFailed  = "Failed"
)

// ClientSecretReconciler reconciles a ClientSecret object
type ClientSecretReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=secret-manager.ngl.cx,resources=clientsecrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=secret-manager.ngl.cx,resources=clientsecrets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=secret-manager.ngl.cx,resources=clientsecrets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile handles ClientSecret resources
func (r *ClientSecretReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the ClientSecret
	var cs secretmanagerv1alpha1.ClientSecret
	if err := r.Get(ctx, req.NamespacedName, &cs); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("reconciling ClientSecret", "name", cs.Name, "provider", cs.Spec.Provider)

	// Get provider
	prov := adapter.Get(cs.Spec.Provider)
	if prov == nil {
		return r.setFailedCondition(ctx, &cs, fmt.Errorf("unknown provider %q", cs.Spec.Provider))
	}

	// Handle deletion
	if !cs.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &cs, prov)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&cs, Finalizer) {
		controllerutil.AddFinalizer(&cs, Finalizer)
		if err := r.Update(ctx, &cs); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Cleanup expired keys
	keysChanged, err := r.cleanupExpiredKeys(ctx, &cs, prov)
	if err != nil {
		log.Error(err, "failed to cleanup expired keys")
	}
	if keysChanged {
		if err := r.Status().Update(ctx, &cs); err != nil {
			log.Error(err, "failed to update status after key cleanup")
		}
	}

	// Validate config - don't retry on validation errors, wait for spec change
	if err := prov.Validate(cs.Spec.Config.Raw); err != nil {
		_, _ = r.setFailedCondition(ctx, &cs, fmt.Errorf("invalid config: %w", err))
		return ctrl.Result{}, nil
	}

	// Check if renewal is needed
	if !r.needsRenewal(ctx, &cs) {
		return r.scheduleNextRenewal(&cs), nil
	}

	// Provision new credentials
	log.Info("provisioning credentials", "provider", cs.Spec.Provider)
	result, err := prov.Provision(ctx, cs.Spec.Config.Raw)
	if err != nil {
		return r.setFailedCondition(ctx, &cs, fmt.Errorf("provisioning failed: %w", err))
	}

	// Create/update output Secret
	if err := r.reconcileOutputSecret(ctx, &cs, result); err != nil {
		return r.setFailedCondition(ctx, &cs, fmt.Errorf("failed to update output secret: %w", err))
	}

	// Update status to ready
	return r.setReadyCondition(ctx, &cs, result)
}

// reconcileOutputSecret creates or updates the output Secret
func (r *ClientSecretReconciler) reconcileOutputSecret(
	ctx context.Context,
	cs *secretmanagerv1alpha1.ClientSecret,
	result *adapter.Result,
) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cs.Spec.SecretRef.Name,
			Namespace: cs.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		// Set owner reference for garbage collection
		if err := controllerutil.SetControllerReference(cs, secret, r.Scheme); err != nil {
			return err
		}

		// Provider has already rendered templates, just copy the data
		secret.StringData = result.StringData

		return nil
	})

	return err
}

// setReadyCondition updates status to Ready
func (r *ClientSecretReconciler) setReadyCondition(
	ctx context.Context,
	cs *secretmanagerv1alpha1.ClientSecret,
	result *adapter.Result,
) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	cs.Status.Phase = PhaseReady
	cs.Status.ObservedGeneration = cs.Generation
	cs.Status.CurrentKeyId = result.KeyID
	cs.Status.FailureCount = 0
	cs.Status.LastFailure = nil
	cs.Status.LastFailureMessage = ""

	// Add active key
	if result.KeyID != "" {
		cs.Status.ActiveKeys = append(cs.Status.ActiveKeys, secretmanagerv1alpha1.ActiveKey{
			KeyID:     result.KeyID,
			CreatedAt: metav1.NewTime(result.ProvisionedAt),
			ExpiresAt: metav1.NewTime(result.ValidUntil),
		})
	}

	// Set condition
	meta.SetStatusCondition(&cs.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Provisioned",
		Message:            "Credentials provisioned successfully",
		ObservedGeneration: cs.Generation,
	})

	if err := r.Status().Update(ctx, cs); err != nil {
		return ctrl.Result{}, err
	}

	log.Info(
		"credentials provisioned successfully",
		"keyId",
		result.KeyID,
		"validUntil",
		result.ValidUntil,
	)
	return r.scheduleNextRenewal(cs), nil
}

// setFailedCondition updates status to Failed and returns error for backoff
func (r *ClientSecretReconciler) setFailedCondition(
	ctx context.Context,
	cs *secretmanagerv1alpha1.ClientSecret,
	err error,
) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Error(err, "reconciliation failed")

	cs.Status.Phase = PhaseFailed
	cs.Status.FailureCount++
	now := metav1.Now()
	cs.Status.LastFailure = &now
	cs.Status.LastFailureMessage = err.Error()

	meta.SetStatusCondition(&cs.Status.Conditions, metav1.Condition{
		Type:               ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ProvisioningFailed",
		Message:            err.Error(),
		ObservedGeneration: cs.Generation,
	})

	if updateErr := r.Status().Update(ctx, cs); updateErr != nil {
		log.Error(updateErr, "failed to update status")
		return ctrl.Result{}, updateErr
	}

	// Return error to trigger exponential backoff
	return ctrl.Result{}, err
}

// handleDeletion cleans up all managed keys and removes the finalizer
func (r *ClientSecretReconciler) handleDeletion(
	ctx context.Context,
	cs *secretmanagerv1alpha1.ClientSecret,
	prov adapter.Provider,
) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(cs, Finalizer) {
		return ctrl.Result{}, nil
	}

	log.Info("cleaning up managed keys before deletion")

	// Delete all active keys
	for _, key := range cs.Status.ActiveKeys {
		if err := prov.DeleteKey(ctx, cs.Spec.Config.Raw, key.KeyID); err != nil {
			log.Error(err, "failed to delete key", "keyId", key.KeyID)
			// Continue trying to delete other keys
		} else {
			log.Info("deleted key", "keyId", key.KeyID)
		}
	}

	controllerutil.RemoveFinalizer(cs, Finalizer)
	return ctrl.Result{}, r.Update(ctx, cs)
}

// cleanupExpiredKeys removes keys that have expired.
// Returns true if any keys were removed from the list.
func (r *ClientSecretReconciler) cleanupExpiredKeys(
	ctx context.Context,
	cs *secretmanagerv1alpha1.ClientSecret,
	prov adapter.Provider,
) (bool, error) {
	log := log.FromContext(ctx)

	now := time.Now()
	var activeKeys []secretmanagerv1alpha1.ActiveKey
	removed := 0

	for _, key := range cs.Status.ActiveKeys {
		if key.ExpiresAt.Time.Before(now) {
			log.Info("deleting expired key", "keyId", key.KeyID, "expired", key.ExpiresAt)
			if err := prov.DeleteKey(ctx, cs.Spec.Config.Raw, key.KeyID); err != nil {
				log.Error(err, "failed to delete expired key", "keyId", key.KeyID)
				// Keep it in the list to retry later
				activeKeys = append(activeKeys, key)
			} else {
				removed++
			}
		} else {
			activeKeys = append(activeKeys, key)
		}
	}

	cs.Status.ActiveKeys = activeKeys
	return removed > 0, nil
}

// needsRenewal checks if the credentials need to be provisioned or renewed
func (r *ClientSecretReconciler) needsRenewal(
	ctx context.Context,
	cs *secretmanagerv1alpha1.ClientSecret,
) bool {
	// No active keys means we need to provision
	if len(cs.Status.ActiveKeys) == 0 {
		return true
	}

	// Check if spec changed (config/template modified)
	if cs.Status.ObservedGeneration != cs.Generation {
		return true
	}

	// Check if the target Secret exists and has data
	var secret corev1.Secret
	secretKey := client.ObjectKey{Namespace: cs.Namespace, Name: cs.Spec.SecretRef.Name}
	if err := r.Get(ctx, secretKey, &secret); err != nil {
		// Secret doesn't exist or error fetching - need to provision
		return true
	}
	if len(secret.Data) == 0 {
		return true
	}

	// Find the newest key
	var newestKey *secretmanagerv1alpha1.ActiveKey
	for i := range cs.Status.ActiveKeys {
		key := &cs.Status.ActiveKeys[i]
		if newestKey == nil || key.CreatedAt.After(newestKey.CreatedAt.Time) {
			newestKey = key
		}
	}

	if newestKey == nil {
		return true
	}

	// Already expired
	if newestKey.ExpiresAt.Time.Before(time.Now()) {
		return true
	}

	// Calculate renewal threshold
	validityDuration := newestKey.ExpiresAt.Sub(newestKey.CreatedAt.Time)
	dynamicThreshold := validityDuration / 10 // 10% of validity
	threshold := min(dynamicThreshold, RenewalThreshold)

	return time.Until(newestKey.ExpiresAt.Time) < threshold
}

// scheduleNextRenewal calculates when to requeue for renewal
func (r *ClientSecretReconciler) scheduleNextRenewal(
	cs *secretmanagerv1alpha1.ClientSecret,
) ctrl.Result {
	if len(cs.Status.ActiveKeys) == 0 {
		return ctrl.Result{Requeue: true}
	}

	// Find the newest key
	var newestKey *secretmanagerv1alpha1.ActiveKey
	for i := range cs.Status.ActiveKeys {
		key := &cs.Status.ActiveKeys[i]
		if newestKey == nil || key.CreatedAt.After(newestKey.CreatedAt.Time) {
			newestKey = key
		}
	}

	if newestKey == nil {
		return ctrl.Result{Requeue: true}
	}

	// Calculate renewal threshold
	validityDuration := newestKey.ExpiresAt.Sub(newestKey.CreatedAt.Time)
	dynamicThreshold := validityDuration / 10
	threshold := min(dynamicThreshold, RenewalThreshold)
	requeueAfter := max(time.Until(newestKey.ExpiresAt.Time)-threshold, time.Minute)

	return ctrl.Result{RequeueAfter: requeueAfter}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClientSecretReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&secretmanagerv1alpha1.ClientSecret{}).
		Owns(&corev1.Secret{}).
		Complete(r)
}
