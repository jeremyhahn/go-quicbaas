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
	"math"
	"testing"
)

// TestSafeIntToUint64 tests SafeIntToUint64 with both valid and invalid inputs.
func TestSafeIntToUint64(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    uint64
		wantErr bool
	}{
		{
			name:    "positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max int value",
			input:   math.MaxInt,
			want:    uint64(math.MaxInt),
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   -1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large negative value",
			input:   math.MinInt,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeIntToUint64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeIntToUint64(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeIntToUint64(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeInt64ToUint64 tests SafeInt64ToUint64 with both valid and invalid inputs.
func TestSafeInt64ToUint64(t *testing.T) {
	tests := []struct {
		name    string
		input   int64
		want    uint64
		wantErr bool
	}{
		{
			name:    "positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max int64 value",
			input:   math.MaxInt64,
			want:    uint64(math.MaxInt64),
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   -1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "min int64 value",
			input:   math.MinInt64,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeInt64ToUint64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeInt64ToUint64(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeInt64ToUint64(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeUint64ToInt tests SafeUint64ToInt with both valid and invalid inputs.
func TestSafeUint64ToInt(t *testing.T) {
	tests := []struct {
		name    string
		input   uint64
		want    int
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max int value",
			input:   uint64(math.MaxInt),
			want:    math.MaxInt,
			wantErr: false,
		},
		{
			name:    "exceeds max int",
			input:   uint64(math.MaxInt) + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "max uint64 value",
			input:   math.MaxUint64,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeUint64ToInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeUint64ToInt(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeUint64ToInt(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeUint64ToInt64 tests SafeUint64ToInt64 with both valid and invalid inputs.
func TestSafeUint64ToInt64(t *testing.T) {
	tests := []struct {
		name    string
		input   uint64
		want    int64
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max int64 value",
			input:   math.MaxInt64,
			want:    math.MaxInt64,
			wantErr: false,
		},
		{
			name:    "exceeds max int64",
			input:   uint64(math.MaxInt64) + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "max uint64 value",
			input:   math.MaxUint64,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeUint64ToInt64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeUint64ToInt64(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeUint64ToInt64(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeUint64ToInt32 tests SafeUint64ToInt32 with both valid and invalid inputs.
func TestSafeUint64ToInt32(t *testing.T) {
	tests := []struct {
		name    string
		input   uint64
		want    int32
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max int32 value",
			input:   math.MaxInt32,
			want:    math.MaxInt32,
			wantErr: false,
		},
		{
			name:    "exceeds max int32",
			input:   uint64(math.MaxInt32) + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large value",
			input:   math.MaxUint64,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeUint64ToInt32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeUint64ToInt32(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeUint64ToInt32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeIntToUint16 tests SafeIntToUint16 with both valid and invalid inputs.
func TestSafeIntToUint16(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    uint16
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max uint16 value",
			input:   math.MaxUint16,
			want:    math.MaxUint16,
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   -1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "exceeds max uint16",
			input:   math.MaxUint16 + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large positive value",
			input:   100000,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeIntToUint16(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeIntToUint16(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeIntToUint16(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeIntToUint32 tests SafeIntToUint32 with both valid and invalid inputs.
func TestSafeIntToUint32(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    uint32
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max uint32 value",
			input:   math.MaxUint32,
			want:    math.MaxUint32,
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   -1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "exceeds max uint32 (64-bit arch)",
			input:   int(math.MaxUint32) + 1,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeIntToUint32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeIntToUint32(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeIntToUint32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeInt64ToUint32 tests SafeInt64ToUint32 with both valid and invalid inputs.
func TestSafeInt64ToUint32(t *testing.T) {
	tests := []struct {
		name    string
		input   int64
		want    uint32
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max uint32 value",
			input:   math.MaxUint32,
			want:    math.MaxUint32,
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   -1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "exceeds max uint32",
			input:   int64(math.MaxUint32) + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large negative value",
			input:   math.MinInt64,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeInt64ToUint32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeInt64ToUint32(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeInt64ToUint32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeIntToInt32 tests SafeIntToInt32 with both valid and invalid inputs.
func TestSafeIntToInt32(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    int32
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "small negative value",
			input:   -42,
			want:    -42,
			wantErr: false,
		},
		{
			name:    "max int32 value",
			input:   math.MaxInt32,
			want:    math.MaxInt32,
			wantErr: false,
		},
		{
			name:    "min int32 value",
			input:   math.MinInt32,
			want:    math.MinInt32,
			wantErr: false,
		},
		{
			name:    "exceeds max int32",
			input:   int(math.MaxInt32) + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "below min int32",
			input:   int(math.MinInt32) - 1,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeIntToInt32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeIntToInt32(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeIntToInt32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestSafeUint32ToInt32(t *testing.T) {
	tests := []struct {
		name    string
		input   uint32
		want    int32
		wantErr bool
	}{
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max int32 value",
			input:   math.MaxInt32,
			want:    math.MaxInt32,
			wantErr: false,
		},
		{
			name:    "exceeds max int32",
			input:   uint32(math.MaxInt32) + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "max uint32 value",
			input:   math.MaxUint32,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeUint32ToInt32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeUint32ToInt32(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeUint32ToInt32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeUint32ToUint8 tests SafeUint32ToUint8 with both valid and invalid inputs.
func TestSafeUint32ToUint8(t *testing.T) {
	tests := []struct {
		name    string
		input   uint32
		want    uint8
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "max uint8 value",
			input:   math.MaxUint8,
			want:    math.MaxUint8,
			wantErr: false,
		},
		{
			name:    "exceeds max uint8",
			input:   math.MaxUint8 + 1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large value",
			input:   math.MaxUint32,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeUint32ToUint8(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeUint32ToUint8(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeUint32ToUint8(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestSafeIntToUint tests SafeIntToUint with both valid and invalid inputs.
func TestSafeIntToUint(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    uint
		wantErr bool
	}{
		{
			name:    "small positive value",
			input:   42,
			want:    42,
			wantErr: false,
		},
		{
			name:    "zero value",
			input:   0,
			want:    0,
			wantErr: false,
		},
		{
			name:    "large positive value",
			input:   math.MaxInt,
			want:    uint(math.MaxInt),
			wantErr: false,
		},
		{
			name:    "negative value",
			input:   -1,
			want:    0,
			wantErr: true,
		},
		{
			name:    "large negative value",
			input:   math.MinInt,
			want:    0,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeIntToUint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeIntToUint(%d) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeIntToUint(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestMustIntToUint64 tests MustIntToUint64 with valid inputs and panics.
func TestMustIntToUint64(t *testing.T) {
	tests := []struct {
		name        string
		input       int
		want        uint64
		shouldPanic bool
	}{
		{
			name:        "small positive value",
			input:       42,
			want:        42,
			shouldPanic: false,
		},
		{
			name:        "zero value",
			input:       0,
			want:        0,
			shouldPanic: false,
		},
		{
			name:        "max int value",
			input:       math.MaxInt,
			want:        uint64(math.MaxInt),
			shouldPanic: false,
		},
		{
			name:        "negative value should panic",
			input:       -1,
			want:        0,
			shouldPanic: true,
		},
		{
			name:        "large negative value should panic",
			input:       math.MinInt,
			want:        0,
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("MustIntToUint64(%d) did not panic", tt.input)
					}
				}()
				_ = MustIntToUint64(tt.input)
			} else {
				got := MustIntToUint64(tt.input)
				if got != tt.want {
					t.Errorf("MustIntToUint64(%d) = %d, want %d", tt.input, got, tt.want)
				}
			}
		})
	}
}

// TestMustUint64ToInt tests MustUint64ToInt with valid inputs and panics.
func TestMustUint64ToInt(t *testing.T) {
	tests := []struct {
		name        string
		input       uint64
		want        int
		shouldPanic bool
	}{
		{
			name:        "small positive value",
			input:       42,
			want:        42,
			shouldPanic: false,
		},
		{
			name:        "zero value",
			input:       0,
			want:        0,
			shouldPanic: false,
		},
		{
			name:        "max int value",
			input:       uint64(math.MaxInt),
			want:        math.MaxInt,
			shouldPanic: false,
		},
		{
			name:        "exceeds max int should panic",
			input:       uint64(math.MaxInt) + 1,
			want:        0,
			shouldPanic: true,
		},
		{
			name:        "max uint64 should panic",
			input:       math.MaxUint64,
			want:        0,
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("MustUint64ToInt(%d) did not panic", tt.input)
					}
				}()
				_ = MustUint64ToInt(tt.input)
			} else {
				got := MustUint64ToInt(tt.input)
				if got != tt.want {
					t.Errorf("MustUint64ToInt(%d) = %d, want %d", tt.input, got, tt.want)
				}
			}
		})
	}
}

// TestMustUint64ToInt64 tests MustUint64ToInt64 with valid inputs and panics.
func TestMustUint64ToInt64(t *testing.T) {
	tests := []struct {
		name        string
		input       uint64
		want        int64
		shouldPanic bool
	}{
		{
			name:        "small positive value",
			input:       42,
			want:        42,
			shouldPanic: false,
		},
		{
			name:        "zero value",
			input:       0,
			want:        0,
			shouldPanic: false,
		},
		{
			name:        "max int64 value",
			input:       math.MaxInt64,
			want:        math.MaxInt64,
			shouldPanic: false,
		},
		{
			name:        "exceeds max int64 should panic",
			input:       uint64(math.MaxInt64) + 1,
			want:        0,
			shouldPanic: true,
		},
		{
			name:        "max uint64 should panic",
			input:       math.MaxUint64,
			want:        0,
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("MustUint64ToInt64(%d) did not panic", tt.input)
					}
				}()
				_ = MustUint64ToInt64(tt.input)
			} else {
				got := MustUint64ToInt64(tt.input)
				if got != tt.want {
					t.Errorf("MustUint64ToInt64(%d) = %d, want %d", tt.input, got, tt.want)
				}
			}
		})
	}
}

// TestClampIntToUint32 tests ClampIntToUint32 clamping behavior.
func TestClampIntToUint32(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  uint32
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max uint32 value",
			input: math.MaxUint32,
			want:  math.MaxUint32,
		},
		{
			name:  "negative value clamped to 0",
			input: -1,
			want:  0,
		},
		{
			name:  "large negative value clamped to 0",
			input: math.MinInt,
			want:  0,
		},
		{
			name:  "exceeds max uint32 clamped to max",
			input: int(math.MaxUint32) + 1,
			want:  math.MaxUint32,
		},
		{
			name:  "max int clamped to max uint32",
			input: math.MaxInt,
			want:  math.MaxUint32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampIntToUint32(tt.input)
			if got != tt.want {
				t.Errorf("ClampIntToUint32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampIntToInt32 tests ClampIntToInt32 clamping behavior.
func TestClampIntToInt32(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int32
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "small negative value",
			input: -42,
			want:  -42,
		},
		{
			name:  "max int32 value",
			input: math.MaxInt32,
			want:  math.MaxInt32,
		},
		{
			name:  "min int32 value",
			input: math.MinInt32,
			want:  math.MinInt32,
		},
		{
			name:  "exceeds max int32 clamped to max",
			input: int(math.MaxInt32) + 1,
			want:  math.MaxInt32,
		},
		{
			name:  "below min int32 clamped to min",
			input: int(math.MinInt32) - 1,
			want:  math.MinInt32,
		},
		{
			name:  "max int clamped to max int32",
			input: math.MaxInt,
			want:  math.MaxInt32,
		},
		{
			name:  "min int clamped to min int32",
			input: math.MinInt,
			want:  math.MinInt32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampIntToInt32(tt.input)
			if got != tt.want {
				t.Errorf("ClampIntToInt32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestClampUint64ToInt32(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  int32
	}{
		{name: "zero value", input: 0, want: 0},
		{name: "small value", input: 42, want: 42},
		{name: "max int32 value", input: math.MaxInt32, want: math.MaxInt32},
		{name: "just above max int32 saturates", input: math.MaxInt32 + 1, want: math.MaxInt32},
		{name: "max uint64 saturates", input: math.MaxUint64, want: math.MaxInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClampUint64ToInt32(tt.input); got != tt.want {
				t.Errorf("ClampUint64ToInt32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestClampInt64ToInt32(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  int32
	}{
		{name: "zero value", input: 0, want: 0},
		{name: "small negative value", input: -42, want: -42},
		{name: "max int32 value", input: math.MaxInt32, want: math.MaxInt32},
		{name: "min int32 value", input: math.MinInt32, want: math.MinInt32},
		{name: "just above max int32 saturates", input: math.MaxInt32 + 1, want: math.MaxInt32},
		{name: "just below min int32 saturates", input: math.MinInt32 - 1, want: math.MinInt32},
		{name: "max int64 saturates", input: math.MaxInt64, want: math.MaxInt32},
		{name: "min int64 saturates", input: math.MinInt64, want: math.MinInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClampInt64ToInt32(tt.input); got != tt.want {
				t.Errorf("ClampInt64ToInt32(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampIntToUint16 tests ClampIntToUint16 clamping behavior.
func TestClampIntToUint16(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  uint16
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max uint16 value",
			input: math.MaxUint16,
			want:  math.MaxUint16,
		},
		{
			name:  "negative value clamped to 0",
			input: -1,
			want:  0,
		},
		{
			name:  "large negative value clamped to 0",
			input: math.MinInt,
			want:  0,
		},
		{
			name:  "exceeds max uint16 clamped to max",
			input: math.MaxUint16 + 1,
			want:  math.MaxUint16,
		},
		{
			name:  "large value clamped to max uint16",
			input: 100000,
			want:  math.MaxUint16,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampIntToUint16(tt.input)
			if got != tt.want {
				t.Errorf("ClampIntToUint16(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampIntToUint tests ClampIntToUint clamping behavior.
func TestClampIntToUint(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  uint
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "large positive value",
			input: math.MaxInt,
			want:  uint(math.MaxInt),
		},
		{
			name:  "negative value clamped to 0",
			input: -1,
			want:  0,
		},
		{
			name:  "large negative value clamped to 0",
			input: math.MinInt,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampIntToUint(tt.input)
			if got != tt.want {
				t.Errorf("ClampIntToUint(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampUint64ToInt tests ClampUint64ToInt clamping behavior.
func TestClampUint64ToInt(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  int
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max int value",
			input: uint64(math.MaxInt),
			want:  math.MaxInt,
		},
		{
			name:  "exceeds max int clamped to max",
			input: uint64(math.MaxInt) + 1,
			want:  math.MaxInt,
		},
		{
			name:  "max uint64 clamped to max int",
			input: math.MaxUint64,
			want:  math.MaxInt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampUint64ToInt(tt.input)
			if got != tt.want {
				t.Errorf("ClampUint64ToInt(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampUint64ToInt64 tests ClampUint64ToInt64 clamping behavior.
func TestClampUint64ToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  int64
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max int64 value",
			input: math.MaxInt64,
			want:  math.MaxInt64,
		},
		{
			name:  "exceeds max int64 clamped to max",
			input: uint64(math.MaxInt64) + 1,
			want:  math.MaxInt64,
		},
		{
			name:  "max uint64 clamped to max int64",
			input: math.MaxUint64,
			want:  math.MaxInt64,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampUint64ToInt64(tt.input)
			if got != tt.want {
				t.Errorf("ClampUint64ToInt64(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampInt64ToUint64 tests ClampInt64ToUint64 clamping behavior.
func TestClampInt64ToUint64(t *testing.T) {
	tests := []struct {
		name  string
		input int64
		want  uint64
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max int64 value",
			input: math.MaxInt64,
			want:  uint64(math.MaxInt64),
		},
		{
			name:  "negative value clamped to 0",
			input: -1,
			want:  0,
		},
		{
			name:  "min int64 clamped to 0",
			input: math.MinInt64,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampInt64ToUint64(tt.input)
			if got != tt.want {
				t.Errorf("ClampInt64ToUint64(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampUint32ToUint8 tests ClampUint32ToUint8 clamping behavior.
func TestClampUint32ToUint8(t *testing.T) {
	tests := []struct {
		name  string
		input uint32
		want  uint8
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max uint8 value",
			input: math.MaxUint8,
			want:  math.MaxUint8,
		},
		{
			name:  "exceeds max uint8 clamped to max",
			input: math.MaxUint8 + 1,
			want:  math.MaxUint8,
		},
		{
			name:  "max uint32 clamped to max uint8",
			input: math.MaxUint32,
			want:  math.MaxUint8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampUint32ToUint8(tt.input)
			if got != tt.want {
				t.Errorf("ClampUint32ToUint8(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestClampIntToUint64 tests ClampIntToUint64 clamping behavior.
func TestClampIntToUint64(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  uint64
	}{
		{
			name:  "small positive value",
			input: 42,
			want:  42,
		},
		{
			name:  "zero value",
			input: 0,
			want:  0,
		},
		{
			name:  "max int value",
			input: math.MaxInt,
			want:  uint64(math.MaxInt),
		},
		{
			name:  "negative value clamped to 0",
			input: -1,
			want:  0,
		},
		{
			name:  "min int clamped to 0",
			input: math.MinInt,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampIntToUint64(tt.input)
			if got != tt.want {
				t.Errorf("ClampIntToUint64(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
