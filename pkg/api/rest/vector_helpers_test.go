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
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConvertStringMapToInterface tests converting string maps to interface maps
func TestConvertStringMapToInterface(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]interface{}
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map",
			input:    map[string]string{},
			expected: map[string]interface{}{},
		},
		{
			name: "populated map",
			input: map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			expected: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertStringMapToInterface(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestConvertInterfaceMapToString tests converting interface maps to string maps
func TestConvertInterfaceMapToString(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]string
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: nil,
		},
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: map[string]string{},
		},
		{
			name: "string values",
			input: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "int value",
			input: map[string]interface{}{
				"age": 42,
			},
			expected: map[string]string{
				"age": "42",
			},
		},
		{
			name: "float value",
			input: map[string]interface{}{
				"price": 19.99,
			},
			expected: map[string]string{
				"price": "19.99",
			},
		},
		{
			name: "bool value",
			input: map[string]interface{}{
				"active": true,
			},
			expected: map[string]string{
				"active": "true",
			},
		},
		{
			name: "mixed types",
			input: map[string]interface{}{
				"name":   "John",
				"age":    30,
				"score":  95.5,
				"active": false,
			},
			expected: map[string]string{
				"name":   "John",
				"age":    "30",
				"score":  "95.5",
				"active": "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInterfaceMapToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestTimestampToTime tests converting Unix timestamps to time.Time
func TestTimestampToTime(t *testing.T) {
	tests := []struct {
		name      string
		timestamp int64
		checkZero bool
	}{
		{
			name:      "zero timestamp returns current time",
			timestamp: 0,
			checkZero: true,
		},
		{
			name:      "positive timestamp",
			timestamp: 1700000000000000000,
			checkZero: false,
		},
		{
			name:      "another positive timestamp",
			timestamp: 1609459200000000000,
			checkZero: false,
		},
		{
			name:      "small positive timestamp",
			timestamp: 1000000,
			checkZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := timestampToTime(tt.timestamp)
			if tt.checkZero {
				// For zero timestamp, it returns current time, so just check it's not zero
				assert.False(t, result.IsZero())
			} else {
				// For non-zero, check exact conversion
				assert.NotZero(t, result)
				assert.False(t, result.IsZero())
			}
		})
	}
}
