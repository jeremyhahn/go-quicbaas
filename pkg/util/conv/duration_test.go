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
	"time"
)

func TestSafeSecondsToDuration(t *testing.T) {
	tests := []struct {
		name         string
		input        int64
		want         time.Duration
		wantOverflow bool
	}{
		{name: "zero", input: 0, want: 0},
		{name: "thirty seconds", input: 30, want: 30 * time.Second},
		{name: "negative seconds", input: -5, want: -5 * time.Second},
		{name: "largest whole duration", input: maxDurationSeconds, want: time.Duration(maxDurationSeconds) * time.Second},
		{name: "most negative whole duration", input: -maxDurationSeconds, want: time.Duration(-maxDurationSeconds) * time.Second},
		{name: "one second past the largest", input: maxDurationSeconds + 1, wantOverflow: true},
		{name: "one second past the most negative", input: -maxDurationSeconds - 1, wantOverflow: true},
		{name: "max int64", input: math.MaxInt64, wantOverflow: true},
		{name: "min int64", input: math.MinInt64, wantOverflow: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeSecondsToDuration(tt.input)

			var overflow *ConversionOverflowError
			if errors.As(err, &overflow) != tt.wantOverflow {
				t.Fatalf("SafeSecondsToDuration(%d) error = %v, want overflow error %v", tt.input, err, tt.wantOverflow)
			}
			if got != tt.want {
				t.Errorf("SafeSecondsToDuration(%d) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
