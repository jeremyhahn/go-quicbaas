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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errorReader simulates a read error for testing error handling paths.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, stderrors.New("simulated read error")
}

func TestConvertWriteMode(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected service.WriteMode
	}{
		{
			name:     "sync write mode",
			input:    "sync",
			expected: service.WriteModeSync,
		},
		{
			name:     "batched write mode",
			input:    "batched",
			expected: service.WriteModeBatched,
		},
		{
			name:     "wal_durable write mode",
			input:    "wal_durable",
			expected: service.WriteModeWALDurable,
		},
		{
			name:     "empty write mode defaults to sync",
			input:    "",
			expected: service.WriteModeSync,
		},
		{
			name:     "unknown write mode defaults to sync",
			input:    "unknown",
			expected: service.WriteModeSync,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertWriteMode(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertHealthStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    service.HealthStatus
		expected string
	}{
		{
			name:     "healthy status",
			input:    service.HealthStatusHealthy,
			expected: "healthy",
		},
		{
			name:     "degraded status",
			input:    service.HealthStatusDegraded,
			expected: "degraded",
		},
		{
			name:     "unhealthy status",
			input:    service.HealthStatusUnhealthy,
			expected: "unhealthy",
		},
		{
			name:     "unknown status",
			input:    service.HealthStatus(99),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHealthStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertHealthStatusToString(t *testing.T) {
	tests := []struct {
		name     string
		input    service.HealthStatus
		expected string
	}{
		{
			name:     "healthy status",
			input:    service.HealthStatusHealthy,
			expected: "healthy",
		},
		{
			name:     "degraded status",
			input:    service.HealthStatusDegraded,
			expected: "degraded",
		},
		{
			name:     "unhealthy status",
			input:    service.HealthStatusUnhealthy,
			expected: "unhealthy",
		},
		{
			name:     "unknown status",
			input:    service.HealthStatus(99),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertHealthStatusToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInterfaceToBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected []byte
	}{
		{
			name:     "string input",
			input:    "hello",
			expected: []byte("hello"),
		},
		{
			name:     "byte slice input",
			input:    []byte{0x01, 0x02, 0x03},
			expected: []byte{0x01, 0x02, 0x03},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := interfaceToBytes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
