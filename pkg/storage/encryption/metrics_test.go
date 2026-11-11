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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMetrics(t *testing.T) {
	// This test verifies idempotency of InitMetrics
	// Note: We can't reset metricsInitialized because metrics are registered globally
	// and Prometheus doesn't allow duplicate registration

	// Call InitMetrics (may already be initialized from other tests)
	InitMetrics()
	assert.True(t, metricsInitialized)

	// Second call should be idempotent (should not panic)
	assert.NotPanics(t, func() {
		InitMetrics()
	})
	assert.True(t, metricsInitialized)
}

func TestGetMetrics(t *testing.T) {
	// Ensure metrics are initialized
	if !metricsInitialized {
		InitMetrics()
	}

	metrics := GetMetrics()
	require.Len(t, metrics, 8)

	// Check all expected metrics are present
	assert.Contains(t, metrics, "encryption_ops")
	assert.Contains(t, metrics, "decryption_ops")
	assert.Contains(t, metrics, "encryption_errors")
	assert.Contains(t, metrics, "decryption_errors")
	assert.Contains(t, metrics, "encryption_duration")
	assert.Contains(t, metrics, "decryption_duration")
	assert.Contains(t, metrics, "encrypted_bytes_total")
	assert.Contains(t, metrics, "decrypted_bytes_total")

	for name, metric := range metrics {
		assert.NotNil(t, metric, "metric %s should not be nil", name)
	}
}
