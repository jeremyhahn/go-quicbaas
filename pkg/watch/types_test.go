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

package watch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestEventType_String verifies the string representation of all event types.
func TestEventType_String(t *testing.T) {
	tests := []struct {
		name     string
		et       EventType
		expected string
	}{
		{"PUT", PUT, "PUT"},
		{"DELETE", DELETE, "DELETE"},
		{"unknown positive", EventType(99), "UNKNOWN"},
		{"unknown negative", EventType(-1), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.et.String())
		})
	}
}

// TestEventType_String_InvalidValue ensures unknown event types produce UNKNOWN.
func TestEventType_String_InvalidValue(t *testing.T) {
	invalid := EventType(255)
	result := invalid.String()
	assert.Equal(t, "UNKNOWN", result, "out-of-range EventType should produce UNKNOWN")
}
