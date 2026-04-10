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

package interfaces

// MetricsCollector defines the interface for metrics collection.
// It provides methods for recording various metric types.
type MetricsCollector interface {
	// Counter operations

	// IncCounter increments a counter metric by 1.
	IncCounter(name string, labels ...string)

	// AddCounter adds a value to a counter metric.
	AddCounter(name string, value float64, labels ...string)

	// Gauge operations

	// SetGauge sets a gauge metric to a specific value.
	SetGauge(name string, value float64, labels ...string)

	// IncGauge increments a gauge metric by 1.
	IncGauge(name string, labels ...string)

	// DecGauge decrements a gauge metric by 1.
	DecGauge(name string, labels ...string)

	// AddGauge adds a value to a gauge metric.
	AddGauge(name string, value float64, labels ...string)

	// Histogram operations

	// ObserveHistogram records an observation for a histogram metric.
	ObserveHistogram(name string, value float64, labels ...string)

	// Summary operations

	// ObserveSummary records an observation for a summary metric.
	ObserveSummary(name string, value float64, labels ...string)
}

// MetricsRegistry defines the interface for registering metrics.
// It manages the lifecycle of metrics and their registration.
type MetricsRegistry interface {
	// Counter registration

	// RegisterCounter registers a new counter metric.
	RegisterCounter(name, help string, labels []string) error

	// Gauge registration

	// RegisterGauge registers a new gauge metric.
	RegisterGauge(name, help string, labels []string) error

	// Histogram registration

	// RegisterHistogram registers a new histogram metric with default buckets.
	RegisterHistogram(name, help string, labels []string) error

	// RegisterHistogramWithBuckets registers a histogram with custom buckets.
	RegisterHistogramWithBuckets(name, help string, labels []string, buckets []float64) error

	// Summary registration

	// RegisterSummary registers a new summary metric.
	RegisterSummary(name, help string, labels []string) error

	// Lifecycle

	// Unregister removes a metric from the registry.
	Unregister(name string) error

	// Clear removes all metrics from the registry.
	Clear() error
}

// MetricsExporter defines the interface for exporting metrics.
// It provides methods for exposing metrics to monitoring systems.
type MetricsExporter interface {
	// Export exports metrics in the specified format.
	// Common formats include Prometheus text format, JSON, etc.
	Export() ([]byte, error)

	// ExportText exports metrics in Prometheus text format.
	ExportText() (string, error)
}
