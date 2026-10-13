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

package conv

import (
	"errors"
	"math"
	"testing"
)

func TestSafeFloat64ToUint64(t *testing.T) {
	tests := []struct {
		name         string
		input        float64
		want         uint64
		wantNegative bool
		wantOverflow bool
	}{
		{name: "zero value", input: 0, want: 0},
		{name: "whole value", input: 42, want: 42},
		{name: "fraction truncates", input: 2.9, want: 2},
		{name: "largest float below 2^64", input: math.Nextafter(uint64Span, 0), want: 1<<64 - 2048},
		{name: "negative value", input: -1, wantNegative: true},
		{name: "negative fraction", input: -0.5, wantNegative: true},
		{name: "2^64", input: uint64Span, wantOverflow: true},
		{name: "positive infinity", input: math.Inf(1), wantOverflow: true},
		{name: "negative infinity", input: math.Inf(-1), wantNegative: true},
		{name: "NaN", input: math.NaN(), wantOverflow: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeFloat64ToUint64(tt.input)

			var negative *ConversionNegativeError
			if errors.As(err, &negative) != tt.wantNegative {
				t.Fatalf("SafeFloat64ToUint64(%g) error = %v, want negative error %v", tt.input, err, tt.wantNegative)
			}
			var overflow *ConversionOverflowError
			if errors.As(err, &overflow) != tt.wantOverflow {
				t.Fatalf("SafeFloat64ToUint64(%g) error = %v, want overflow error %v", tt.input, err, tt.wantOverflow)
			}
			if got != tt.want {
				t.Errorf("SafeFloat64ToUint64(%g) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSafeFloat64ToInt(t *testing.T) {
	tests := []struct {
		name         string
		input        float64
		want         int
		wantOverflow bool
	}{
		{name: "zero value", input: 0, want: 0},
		{name: "whole value", input: 42, want: 42},
		{name: "negative value", input: -42, want: -42},
		{name: "fraction truncates toward zero", input: 2.9, want: 2},
		{name: "negative fraction truncates toward zero", input: -2.9, want: -2},
		{name: "most negative int", input: -intSpan, want: math.MinInt},
		{name: "largest float below the int span", input: math.Nextafter(intSpan, 0), want: int(math.Nextafter(intSpan, 0))},
		{name: "int span", input: intSpan, wantOverflow: true},
		{name: "below the most negative int", input: math.Nextafter(-intSpan, math.Inf(-1)), wantOverflow: true},
		{name: "positive infinity", input: math.Inf(1), wantOverflow: true},
		{name: "negative infinity", input: math.Inf(-1), wantOverflow: true},
		{name: "NaN", input: math.NaN(), wantOverflow: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeFloat64ToInt(tt.input)

			var overflow *ConversionOverflowError
			if errors.As(err, &overflow) != tt.wantOverflow {
				t.Fatalf("SafeFloat64ToInt(%g) error = %v, want overflow error %v", tt.input, err, tt.wantOverflow)
			}
			if got != tt.want {
				t.Errorf("SafeFloat64ToInt(%g) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
