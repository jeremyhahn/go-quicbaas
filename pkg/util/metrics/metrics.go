// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package metrics provides a convenient wrapper around Prometheus metrics
// for collecting and exposing application metrics.
package metrics

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// defaultRegistry is the default Prometheus registry used by this package.
	defaultRegistry = prometheus.NewRegistry()

	// registryMu protects access to the default registry.
	registryMu sync.RWMutex

	// metricCache deduplicates metric registrations within the same process & registry.
	metricCacheMu sync.RWMutex
	metricCache   = make(map[string]interface{})
)

func init() {
	// Register Go runtime collectors (go_goroutines, go_memstats, etc.)
	defaultRegistry.MustRegister(collectors.NewGoCollector())
	defaultRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

// Counter wraps a Prometheus counter metric.
type Counter struct {
	counter prometheus.Counter
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	c.counter.Inc()
}

// Add adds the given value to the counter.
func (c *Counter) Add(delta float64) {
	c.counter.Add(delta)
}

// Gauge wraps a Prometheus gauge metric.
type Gauge struct {
	gauge prometheus.Gauge
}

// Set sets the gauge to the given value.
func (g *Gauge) Set(value float64) {
	g.gauge.Set(value)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	g.gauge.Inc()
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	g.gauge.Dec()
}

// Add adds the given value to the gauge.
func (g *Gauge) Add(delta float64) {
	g.gauge.Add(delta)
}

// Sub subtracts the given value from the gauge.
func (g *Gauge) Sub(delta float64) {
	g.gauge.Sub(delta)
}

// Histogram wraps a Prometheus histogram metric.
type Histogram struct {
	histogram prometheus.Histogram
}

// Observe adds a single observation to the histogram.
func (h *Histogram) Observe(value float64) {
	if h.histogram == nil {
		return
	}
	h.histogram.Observe(value)
}

// CounterVec wraps a Prometheus counter vector.
type CounterVec struct {
	counterVec *prometheus.CounterVec
}

// WithLabels returns a Counter for the given label values.
func (cv *CounterVec) WithLabels(labels prometheus.Labels) *Counter {
	return &Counter{counter: cv.counterVec.With(labels)}
}

// With returns a Counter for the given label values (variadic).
func (cv *CounterVec) With(labelValues ...string) *Counter {
	return &Counter{counter: cv.counterVec.WithLabelValues(labelValues...)}
}

// GaugeVec wraps a Prometheus gauge vector.
type GaugeVec struct {
	gaugeVec *prometheus.GaugeVec
}

// WithLabels returns a Gauge for the given label values.
func (gv *GaugeVec) WithLabels(labels prometheus.Labels) *Gauge {
	return &Gauge{gauge: gv.gaugeVec.With(labels)}
}

// With returns a Gauge for the given label values (variadic).
func (gv *GaugeVec) With(labelValues ...string) *Gauge {
	return &Gauge{gauge: gv.gaugeVec.WithLabelValues(labelValues...)}
}

// HistogramVec wraps a Prometheus histogram vector.
type HistogramVec struct {
	histogramVec *prometheus.HistogramVec
}

// WithLabels returns a Histogram for the given label values.
// If the underlying observer cannot be asserted as a prometheus.Histogram,
// a no-op Histogram is returned and a warning is logged.
func (hv *HistogramVec) WithLabels(labels prometheus.Labels) *Histogram {
	observer := hv.histogramVec.With(labels)
	if h, ok := observer.(prometheus.Histogram); ok {
		return &Histogram{histogram: h}
	}
	slog.Warn("metrics: observer type assertion failed in WithLabels, returning no-op histogram",
		"type", fmt.Sprintf("%T", observer))
	return &Histogram{}
}

// With returns a Histogram for the given label values (variadic).
// If the underlying observer cannot be asserted as a prometheus.Histogram,
// a no-op Histogram is returned and a warning is logged.
func (hv *HistogramVec) With(labelValues ...string) *Histogram {
	observer := hv.histogramVec.WithLabelValues(labelValues...)
	if h, ok := observer.(prometheus.Histogram); ok {
		return &Histogram{histogram: h}
	}
	slog.Warn("metrics: observer type assertion failed in With, returning no-op histogram",
		"type", fmt.Sprintf("%T", observer))
	return &Histogram{}
}

// CounterOpts defines options for creating a Counter.
type CounterOpts struct {
	Namespace   string
	Subsystem   string
	Name        string
	Help        string
	ConstLabels prometheus.Labels
}

// GaugeOpts defines options for creating a Gauge.
type GaugeOpts struct {
	Namespace   string
	Subsystem   string
	Name        string
	Help        string
	ConstLabels prometheus.Labels
}

// HistogramOpts defines options for creating a Histogram.
type HistogramOpts struct {
	Namespace   string
	Subsystem   string
	Name        string
	Help        string
	ConstLabels prometheus.Labels
	Buckets     []float64
}

// cachedMetric returns the metric cached under key, registering one with
// build on a miss. The re-check and the registration run under one write
// lock, so callers that miss the cache together register exactly once and
// all receive the same collector. A cached entry of the wrong type is
// evicted and replaced by a fresh registration.
func cachedMetric[T any](key, kind string, build func(promauto.Factory) T) T {
	registryMu.RLock()
	reg := defaultRegistry
	registryMu.RUnlock()

	metricCacheMu.RLock()
	v, ok := metricCache[key]
	metricCacheMu.RUnlock()
	if m, isT := v.(T); ok && isT {
		return m
	}

	metricCacheMu.Lock()
	defer metricCacheMu.Unlock()

	if v, ok := metricCache[key]; ok {
		if m, isT := v.(T); isT {
			return m
		}
		slog.Warn("metrics: cached metric type mismatch for "+kind+", re-registering",
			"key", key, "type", fmt.Sprintf("%T", v))
		delete(metricCache, key)
	}

	m := build(promauto.With(reg))
	metricCache[key] = m
	return m
}

// NewCounter creates and registers a new counter metric.
func NewCounter(opts CounterOpts) *Counter {
	key := "counter:" + opts.Namespace + ":" + opts.Subsystem + ":" + opts.Name
	counter := cachedMetric(key, "counter", func(f promauto.Factory) prometheus.Counter {
		return f.NewCounter(prometheus.CounterOpts{
			Namespace:   opts.Namespace,
			Subsystem:   opts.Subsystem,
			Name:        opts.Name,
			Help:        opts.Help,
			ConstLabels: opts.ConstLabels,
		})
	})
	return &Counter{counter: counter}
}

// NewGauge creates and registers a new gauge metric.
func NewGauge(opts GaugeOpts) *Gauge {
	key := "gauge:" + opts.Namespace + ":" + opts.Subsystem + ":" + opts.Name
	gauge := cachedMetric(key, "gauge", func(f promauto.Factory) prometheus.Gauge {
		return f.NewGauge(prometheus.GaugeOpts{
			Namespace:   opts.Namespace,
			Subsystem:   opts.Subsystem,
			Name:        opts.Name,
			Help:        opts.Help,
			ConstLabels: opts.ConstLabels,
		})
	})
	return &Gauge{gauge: gauge}
}

// NewHistogram creates and registers a new histogram metric.
func NewHistogram(opts HistogramOpts) *Histogram {
	buckets := opts.Buckets
	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}

	key := "hist:" + opts.Namespace + ":" + opts.Subsystem + ":" + opts.Name + ":" + fmt.Sprint(buckets)
	histogram := cachedMetric(key, "histogram", func(f promauto.Factory) prometheus.Histogram {
		return f.NewHistogram(prometheus.HistogramOpts{
			Namespace:   opts.Namespace,
			Subsystem:   opts.Subsystem,
			Name:        opts.Name,
			Help:        opts.Help,
			ConstLabels: opts.ConstLabels,
			Buckets:     buckets,
		})
	})
	return &Histogram{histogram: histogram}
}

// NewCounterVec creates and registers a new counter vector metric.
func NewCounterVec(opts CounterOpts, labelNames []string) *CounterVec {
	key := "countervec:" + opts.Namespace + ":" + opts.Subsystem + ":" + opts.Name + ":" + fmt.Sprint(labelNames)
	counterVec := cachedMetric(key, "counter vec", func(f promauto.Factory) *prometheus.CounterVec {
		return f.NewCounterVec(prometheus.CounterOpts{
			Namespace:   opts.Namespace,
			Subsystem:   opts.Subsystem,
			Name:        opts.Name,
			Help:        opts.Help,
			ConstLabels: opts.ConstLabels,
		}, labelNames)
	})
	return &CounterVec{counterVec: counterVec}
}

// NewGaugeVec creates and registers a new gauge vector metric.
func NewGaugeVec(opts GaugeOpts, labelNames []string) *GaugeVec {
	key := "gaugevec:" + opts.Namespace + ":" + opts.Subsystem + ":" + opts.Name + ":" + fmt.Sprint(labelNames)
	gaugeVec := cachedMetric(key, "gauge vec", func(f promauto.Factory) *prometheus.GaugeVec {
		return f.NewGaugeVec(prometheus.GaugeOpts{
			Namespace:   opts.Namespace,
			Subsystem:   opts.Subsystem,
			Name:        opts.Name,
			Help:        opts.Help,
			ConstLabels: opts.ConstLabels,
		}, labelNames)
	})
	return &GaugeVec{gaugeVec: gaugeVec}
}

// NewHistogramVec creates and registers a new histogram vector metric.
func NewHistogramVec(opts HistogramOpts, labelNames []string) *HistogramVec {
	buckets := opts.Buckets
	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}

	key := "histvec:" + opts.Namespace + ":" + opts.Subsystem + ":" + opts.Name + ":" + fmt.Sprint(labelNames) + ":" + fmt.Sprint(buckets)
	histogramVec := cachedMetric(key, "histogram vec", func(f promauto.Factory) *prometheus.HistogramVec {
		return f.NewHistogramVec(prometheus.HistogramOpts{
			Namespace:   opts.Namespace,
			Subsystem:   opts.Subsystem,
			Name:        opts.Name,
			Help:        opts.Help,
			ConstLabels: opts.ConstLabels,
			Buckets:     buckets,
		}, labelNames)
	})
	return &HistogramVec{histogramVec: histogramVec}
}

// Registry returns the default Prometheus registry.
func Registry() *prometheus.Registry {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return defaultRegistry
}

// SetRegistry sets a custom Prometheus registry as the default.
func SetRegistry(registry *prometheus.Registry) {
	registryMu.Lock()
	defer registryMu.Unlock()
	defaultRegistry = registry
	// Reset cached collectors so new registry can re-register metrics without conflicts
	collectorsMu.Lock()
	collectorCache = make(map[string]*Collector)
	collectorsMu.Unlock()
	metricCacheMu.Lock()
	metricCache = make(map[string]interface{})
	metricCacheMu.Unlock()
}

// MustRegister registers the provided collectors with the default registry
// and panics if any error occurs.
func MustRegister(collectors ...prometheus.Collector) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	defaultRegistry.MustRegister(collectors...)
}

// Register registers the provided collectors with the default registry.
func Register(collectors ...prometheus.Collector) error {
	registryMu.RLock()
	defer registryMu.RUnlock()

	for _, c := range collectors {
		if err := defaultRegistry.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// Unregister removes the provided collector from the default registry.
func Unregister(collector prometheus.Collector) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return defaultRegistry.Unregister(collector)
}
