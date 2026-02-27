package framework

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// InstrumentedProvider wraps a [Provider] with Prometheus metrics and
// structured logging for Provision and DeleteKey calls. Create via [Instrument].
type InstrumentedProvider[O Object] struct {
	Provider[O]

	// ProvisionDuration observes the duration of Provision calls.
	ProvisionDuration *prometheus.HistogramVec
	// ProvisionTotal counts Provision calls by result.
	ProvisionTotal *prometheus.CounterVec
	// DeleteKeyDuration observes the duration of DeleteKey calls.
	DeleteKeyDuration *prometheus.HistogramVec
	// DeleteKeyTotal counts DeleteKey calls by result.
	DeleteKeyTotal *prometheus.CounterVec
}

// Instrument wraps a provider with Prometheus metrics collection and
// structured logging. Metrics are registered on the given registerer (use
// [sigs.k8s.io/controller-runtime/pkg/metrics.Registry] in production).
func Instrument[O Object](p Provider[O], reg prometheus.Registerer) *InstrumentedProvider[O] {
	ip := &InstrumentedProvider[O]{
		Provider: p,
		ProvisionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "valet_provision_duration_seconds",
			Help: "Duration of provider Provision calls in seconds.",
		}, []string{"result"}),
		ProvisionTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "valet_provision_total",
			Help: "Total number of provider Provision calls.",
		}, []string{"result"}),
		DeleteKeyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "valet_delete_key_duration_seconds",
			Help: "Duration of provider DeleteKey calls in seconds.",
		}, []string{"result"}),
		DeleteKeyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "valet_delete_key_total",
			Help: "Total number of provider DeleteKey calls.",
		}, []string{"result"}),
	}
	reg.MustRegister(
		ip.ProvisionDuration, ip.ProvisionTotal,
		ip.DeleteKeyDuration, ip.DeleteKeyTotal,
	)
	return ip
}

// Provision delegates to the inner provider and records duration and outcome.
// The context logger is enriched with operation and duration fields.
func (p *InstrumentedProvider[O]) Provision(ctx context.Context, obj O) (*Result, error) {
	ctx = log.IntoContext(ctx,
		log.FromContext(ctx).WithValues("operation", "provision"))

	start := time.Now()
	result, err := p.Provider.Provision(ctx, obj)
	duration := time.Since(start)

	label := resultLabel(err)
	p.ProvisionDuration.WithLabelValues(label).Observe(duration.Seconds())
	p.ProvisionTotal.WithLabelValues(label).Inc()

	l := log.FromContext(ctx).WithValues("duration", duration)
	if err != nil {
		l.Error(err, "provision failed")
	} else {
		l.Info("provision complete", "keyId", result.KeyID)
	}
	return result, err
}

// DeleteKey delegates to the inner provider and records duration and outcome.
// The context logger is enriched with operation, keyId, and duration fields.
func (p *InstrumentedProvider[O]) DeleteKey(ctx context.Context, obj O, keyID string) error {
	ctx = log.IntoContext(ctx,
		log.FromContext(ctx).WithValues("operation", "deleteKey", "keyId", keyID))

	start := time.Now()
	err := p.Provider.DeleteKey(ctx, obj, keyID)
	duration := time.Since(start)

	label := resultLabel(err)
	p.DeleteKeyDuration.WithLabelValues(label).Observe(duration.Seconds())
	p.DeleteKeyTotal.WithLabelValues(label).Inc()

	l := log.FromContext(ctx).WithValues("duration", duration)
	if err != nil {
		l.Error(err, "delete key failed")
	} else {
		l.Info("delete key complete")
	}
	return err
}

func resultLabel(err error) string {
	if err != nil {
		return "error"
	}
	return "success"
}
