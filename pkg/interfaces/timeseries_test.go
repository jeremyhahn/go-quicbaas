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

import "testing"

func TestAggFunc_String(t *testing.T) {
	tests := []struct {
		name     string
		f        AggFunc
		expected string
	}{
		{"none", AggNone, "none"},
		{"avg", AggAvg, "avg"},
		{"sum", AggSum, "sum"},
		{"min", AggMin, "min"},
		{"max", AggMax, "max"},
		{"count", AggCount, "count"},
		{"percentile", AggPercentile, "percentile"},
		{"first", AggFirst, "first"},
		{"last", AggLast, "last"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.f.String()
			if result != tt.expected {
				t.Errorf("AggFunc(%d).String() = %q, want %q", tt.f, result, tt.expected)
			}
		})
	}
}

func TestAggFunc_String_Unknown(t *testing.T) {
	unknown := AggFunc(255)
	result := unknown.String()
	if result != "unknown" {
		t.Errorf("expected 'unknown' for invalid AggFunc, got %q", result)
	}
}
