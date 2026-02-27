package framework

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Option configures the controller builder in [Reconciler.SetupWithManager].
type Option func(*builder.Builder)

// Reconciler reconciles a provider-specific ClientSecret CRD.
// The type parameter O is the provider's CRD type, which must satisfy [Object].
type Reconciler[O Object] struct {
	client.Client
	Scheme   *runtime.Scheme
	Provider Provider[O]
}

// SetupWithManager sets up the controller with the Manager.
// Options can be used to further configure the controller builder,
// for example to set a custom controller name via [builder.Builder.Named].
func (r *Reconciler[O]) SetupWithManager(mgr ctrl.Manager, opts ...Option) error {
	b := ctrl.NewControllerManagedBy(mgr).
		For(r.Provider.NewObject()).
		Owns(&corev1.Secret{})
	for _, opt := range opts {
		opt(b)
	}
	return b.Complete(r)
}

// Reconcile handles the reconciliation loop. It fetches the CRD, ensures
// a finalizer, validates the spec, cleans up expired keys, and provisions
// or renews credentials when needed.
func (r *Reconciler[O]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := r.Provider.NewObject()
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion.
	if !obj.GetDeletionTimestamp().IsZero() {
		return r.handleDeletion(ctx, obj)
	}

	// Ensure finalizer is present.
	if !controllerutil.ContainsFinalizer(obj, Finalizer) {
		controllerutil.AddFinalizer(obj, Finalizer)
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("adding finalizer: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Validate before any work — don't retry, wait for spec change.
	if err := obj.Validate(); err != nil {
		log.FromContext(ctx).Error(err, "validation failed")
		obj.GetStatus().SetFailed(obj.GetGeneration(), fmt.Errorf("invalid config: %w", err))
		if updateErr := r.Status().Update(ctx, obj); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil
	}

	// Cleanup expired keys.
	if err := r.handleCleanup(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	// Check if renewal is needed and handle it.
	secretHasData := r.secretHasData(ctx, obj)
	if obj.GetStatus().NeedsRenewal(obj.GetGeneration(), secretHasData) {
		return r.handleRenewal(ctx, obj)
	}

	return r.scheduleNext(obj), nil
}

// handleRenewal provisions new credentials, writes them to the output secret,
// updates the CRD status to Ready, and schedules the next reconciliation.
func (r *Reconciler[O]) handleRenewal(ctx context.Context, obj O) (ctrl.Result, error) {
	result, err := r.Provider.Provision(ctx, obj)
	if err != nil {
		return r.failStatus(ctx, obj, fmt.Errorf("provisioning failed: %w", err))
	}

	if err := r.reconcileOutputSecret(ctx, obj, result); err != nil {
		return r.failStatus(ctx, obj, fmt.Errorf("output secret: %w", err))
	}

	obj.GetStatus().SetReady(obj.GetGeneration(), result)
	if err := r.Status().Update(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}

	return r.scheduleNext(obj), nil
}

// handleDeletion cleans up all managed keys and removes the finalizer.
// Active (non-expired) keys that fail to delete block deletion to prevent
// orphaning usable credentials. Expired keys are best-effort.
func (r *Reconciler[O]) handleDeletion(ctx context.Context, obj O) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(obj, Finalizer) {
		return ctrl.Result{}, nil
	}

	log.Info("cleaning up managed keys before deletion")
	now := time.Now()
	var activeFailures int
	for _, key := range obj.GetStatus().ActiveKeys {
		if err := r.Provider.DeleteKey(ctx, obj, key.KeyID); err != nil {
			log.Error(err, "failed to delete key", "keyId", key.KeyID)
			if !key.ExpiresAt.Time.Before(now) {
				activeFailures++
			}
		}
	}

	if activeFailures > 0 {
		return ctrl.Result{}, fmt.Errorf(
			"failed to delete %d active key(s), will retry",
			activeFailures,
		)
	}

	controllerutil.RemoveFinalizer(obj, Finalizer)

	return ctrl.Result{}, r.Update(ctx, obj)
}

// handleCleanup attempts to delete expired keys at the provider and removes
// successfully deleted keys from the status. Keys that fail to delete are
// retained for retry on the next reconciliation.
func (r *Reconciler[O]) handleCleanup(ctx context.Context, obj O) error {
	log := log.FromContext(ctx)

	expired := obj.GetStatus().ActiveKeys.DropExpired(time.Now(), func(key ActiveKey) bool {
		if err := r.Provider.DeleteKey(ctx, obj, key.KeyID); err != nil {
			log.Error(err, "failed to delete expired key", "keyId", key.KeyID)
			return true // keep in status to retry later
		}

		return false
	})

	if len(expired) > 0 {
		if err := r.Status().Update(ctx, obj); err != nil {
			log.Error(err, "failed to update status after key cleanup")
		}
	}

	return nil
}

// reconcileOutputSecret creates or updates the Kubernetes Secret that holds
// the provisioned credentials. The secret is owned by the CRD so it gets
// garbage-collected on deletion.
func (r *Reconciler[O]) reconcileOutputSecret(ctx context.Context, obj O, result *Result) error {
	ref := obj.GetSecretRef()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: obj.GetNamespace(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		if err := controllerutil.SetControllerReference(obj, secret, r.Scheme); err != nil {
			return err
		}
		secret.StringData = result.StringData
		return nil
	})

	return err
}

// failStatus persists a failed status and returns the error for backoff retry.
func (r *Reconciler[O]) failStatus(ctx context.Context, obj O, err error) (ctrl.Result, error) {
	obj.GetStatus().SetFailed(obj.GetGeneration(), err)
	if updateErr := r.Status().Update(ctx, obj); updateErr != nil {
		return ctrl.Result{}, updateErr
	}

	return ctrl.Result{}, err
}

// scheduleNext returns a ctrl.Result that requeues at the next renewal time.
// If no active keys exist, it triggers an immediate requeue.
func (r *Reconciler[O]) scheduleNext(obj O) ctrl.Result {
	if d := obj.GetStatus().RenewalDuration(); d > 0 {
		return ctrl.Result{RequeueAfter: d}
	}

	return ctrl.Result{Requeue: true}
}

// secretHasData checks whether the output secret exists and contains data.
func (r *Reconciler[O]) secretHasData(ctx context.Context, obj O) bool {
	var secret corev1.Secret
	key := client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetSecretRef().Name}
	if err := r.Get(ctx, key, &secret); err != nil {
		return false
	}

	return len(secret.Data) > 0
}
