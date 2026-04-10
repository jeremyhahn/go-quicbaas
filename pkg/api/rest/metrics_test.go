//go:build rest

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

package rest

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitAPIMetrics(t *testing.T) {
	// Reset metrics for testing
	apiMetrics = nil
	apiMetricsOnce = sync.Once{}

	// Initialize metrics
	initAPIMetrics()

	// Verify metrics were initialized
	assert.NotNil(t, apiMetrics)
	assert.NotNil(t, apiMetrics.requestDuration)
	assert.NotNil(t, apiMetrics.requestSize)
	assert.NotNil(t, apiMetrics.responseSize)
	assert.NotNil(t, apiMetrics.errorsByType)
	assert.NotNil(t, apiMetrics.activeConnections)
	assert.NotNil(t, apiMetrics.totalConnections)
	assert.NotNil(t, apiMetrics.putOperations)
	assert.NotNil(t, apiMetrics.getOperations)
	assert.NotNil(t, apiMetrics.deleteOperations)
	assert.NotNil(t, apiMetrics.queryOperations)
	assert.NotNil(t, apiMetrics.listOperations)
	assert.NotNil(t, apiMetrics.successfulOps)
	assert.NotNil(t, apiMetrics.failedOps)
	assert.NotNil(t, apiMetrics.batchSize)
	assert.NotNil(t, apiMetrics.batchLatency)
}

func TestGetAPIMetrics(t *testing.T) {
	// Reset metrics for testing
	apiMetrics = nil
	apiMetricsOnce = sync.Once{}

	// Get metrics (should initialize if needed)
	metrics := getAPIMetrics()

	assert.NotNil(t, metrics)
	assert.NotNil(t, metrics.requestDuration)
	assert.NotNil(t, metrics.requestSize)
	assert.NotNil(t, metrics.responseSize)

	// Calling again should return same instance
	metrics2 := getAPIMetrics()
	assert.Equal(t, metrics, metrics2)
}

func TestInitAPIMetrics_IdempotentInit(t *testing.T) {
	// Reset metrics for testing
	apiMetrics = nil
	apiMetricsOnce = sync.Once{}

	// Initialize multiple times
	initAPIMetrics()
	metrics1 := apiMetrics

	initAPIMetrics()
	metrics2 := apiMetrics

	// Should be same instance
	assert.Equal(t, metrics1, metrics2)
}
