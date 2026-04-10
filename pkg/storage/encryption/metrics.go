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

package encryption

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

var (
	// encryptionOpsTotal tracks the total number of encryption operations.
	encryptionOpsTotal *metrics.CounterVec

	// decryptionOpsTotal tracks the total number of decryption operations.
	decryptionOpsTotal *metrics.CounterVec

	// encryptionErrorsTotal tracks encryption operation failures.
	encryptionErrorsTotal *metrics.CounterVec

	// decryptionErrorsTotal tracks decryption operation failures.
	decryptionErrorsTotal *metrics.CounterVec

	// encryptionDuration tracks time spent encrypting.
	encryptionDuration *metrics.HistogramVec

	// decryptionDuration tracks time spent decrypting.
	decryptionDuration *metrics.HistogramVec

	// encryptedBytesTotal tracks total bytes encrypted.
	encryptedBytesTotal *metrics.CounterVec

	// decryptedBytesTotal tracks total bytes decrypted.
	decryptedBytesTotal *metrics.CounterVec

	// metricsInitialized tracks whether metrics have been initialized.
	metricsInitialized = false
)

// InitMetrics initializes Prometheus metrics for encryption operations.
// This should be called once at application startup.
func InitMetrics() {
	if metricsInitialized {
		return
	}

	encryptionOpsTotal = metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "operations_total",
			Help:      "Total number of encryption operations",
		},
		[]string{"algorithm", "status"},
	)

	decryptionOpsTotal = metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "decryption_operations_total",
			Help:      "Total number of decryption operations",
		},
		[]string{"algorithm", "status"},
	)

	encryptionErrorsTotal = metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "errors_total",
			Help:      "Total number of encryption errors",
		},
		[]string{"algorithm", "operation", "error_type"},
	)

	decryptionErrorsTotal = metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "decryption_errors_total",
			Help:      "Total number of decryption errors",
		},
		[]string{"algorithm", "operation", "error_type"},
	)

	encryptionDuration = metrics.NewHistogramVec(
		metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "duration_seconds",
			Help:      "Time spent performing encryption operations",
			Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 15), // 1μs to ~16ms
		},
		[]string{"algorithm", "operation"},
	)

	decryptionDuration = metrics.NewHistogramVec(
		metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "decryption_duration_seconds",
			Help:      "Time spent performing decryption operations",
			Buckets:   prometheus.ExponentialBuckets(0.000001, 2, 15), // 1μs to ~16ms
		},
		[]string{"algorithm", "operation"},
	)

	encryptedBytesTotal = metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "encrypted_bytes_total",
			Help:      "Total number of bytes encrypted",
		},
		[]string{"algorithm"},
	)

	decryptedBytesTotal = metrics.NewCounterVec(
		metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "encryption",
			Name:      "decrypted_bytes_total",
			Help:      "Total number of bytes decrypted",
		},
		[]string{"algorithm"},
	)

	metricsInitialized = true
}

// recordEncryption records metrics for an encryption operation.
func recordEncryption(algorithm string, plaintextSize int, duration time.Duration, err error) {
	if !metricsInitialized {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
		encryptionErrorsTotal.With(algorithm, "encrypt", "encryption_failed").Inc()
	} else {
		encryptedBytesTotal.With(algorithm).Add(float64(plaintextSize))
	}

	encryptionOpsTotal.With(algorithm, status).Inc()
	encryptionDuration.With(algorithm, "field").Observe(duration.Seconds())
}

// recordDecryption records metrics for a decryption operation.
func recordDecryption(algorithm string, ciphertextSize int, duration time.Duration, err error) {
	if !metricsInitialized {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
		decryptionErrorsTotal.With(algorithm, "decrypt", "decryption_failed").Inc()
	} else {
		decryptedBytesTotal.With(algorithm).Add(float64(ciphertextSize))
	}

	decryptionOpsTotal.With(algorithm, status).Inc()
	decryptionDuration.With(algorithm, "field").Observe(duration.Seconds())
}

// recordMarshal records metrics for marshal operations.
func recordMarshal(fieldCount int, duration time.Duration, err error) {
	if !metricsInitialized {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
	}

	encryptionOpsTotal.With("marshal", status).Inc()
	encryptionDuration.With("marshal", "full").Observe(duration.Seconds())
}

// recordUnmarshal records metrics for unmarshal operations.
func recordUnmarshal(fieldCount int, duration time.Duration, err error) {
	if !metricsInitialized {
		return
	}

	status := "success"
	if err != nil {
		status = "error"
	}

	decryptionOpsTotal.With("unmarshal", status).Inc()
	decryptionDuration.With("unmarshal", "full").Observe(duration.Seconds())
}

// GetMetrics returns the current metrics for testing and monitoring.
func GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"encryption_ops":        encryptionOpsTotal,
		"decryption_ops":        decryptionOpsTotal,
		"encryption_errors":     encryptionErrorsTotal,
		"decryption_errors":     decryptionErrorsTotal,
		"encryption_duration":   encryptionDuration,
		"decryption_duration":   decryptionDuration,
		"encrypted_bytes_total": encryptedBytesTotal,
		"decrypted_bytes_total": decryptedBytesTotal,
	}
}
