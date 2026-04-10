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

package safeconv

import (
	"math"
	"testing"
)

func TestToUint32(t *testing.T) {
	tests := []struct {
		name    string
		input   int32
		want    uint32
		wantErr bool
	}{
		{"positive value", 100, 100, false},
		{"zero", 0, 0, false},
		{"max int32", math.MaxInt32, math.MaxInt32, false},
		{"negative value", -1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToUint32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToUint32() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToUint32() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToInt32(t *testing.T) {
	tests := []struct {
		name    string
		input   uint32
		want    int32
		wantErr bool
	}{
		{"small value", 100, 100, false},
		{"zero", 0, 0, false},
		{"max int32", math.MaxInt32, math.MaxInt32, false},
		{"overflow", math.MaxUint32, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToInt32(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToInt32() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToInt32() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToUint64(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    uint64
		wantErr bool
	}{
		{"positive value", 100, 100, false},
		{"zero", 0, 0, false},
		{"negative value", -1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToUint64(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToUint64() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToUint64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name    string
		input   uint64
		want    int
		wantErr bool
	}{
		{"small value", 100, 100, false},
		{"zero", 0, 0, false},
		{"max int", uint64(math.MaxInt), math.MaxInt, false},
		{"overflow", math.MaxUint64, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToInt32FromInt(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    int32
		wantErr bool
	}{
		{"small value", 100, 100, false},
		{"zero", 0, 0, false},
		{"max int32", math.MaxInt32, math.MaxInt32, false},
		{"min int32", math.MinInt32, math.MinInt32, false},
		{"overflow", math.MaxInt32 + 1, 0, true},
		{"underflow", math.MinInt32 - 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToInt32FromInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToInt32FromInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToInt32FromInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToUint32FromInt(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		want    uint32
		wantErr bool
	}{
		{"small value", 100, 100, false},
		{"zero", 0, 0, false},
		{"max uint32", math.MaxUint32, math.MaxUint32, false},
		{"negative value", -1, 0, true},
		{"overflow", int(math.MaxUint32) + 1, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ToUint32FromInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ToUint32FromInt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ToUint32FromInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUint64ToInt32(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  int32
	}{
		{"zero", 0, 0},
		{"small value", 42, 42},
		{"max int32 boundary", math.MaxInt32, math.MaxInt32},
		{"exceeds max int32", math.MaxInt32 + 1, math.MaxInt32},
		{"large uint64", math.MaxUint64, math.MaxInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uint64ToInt32(tt.input)
			if got != tt.want {
				t.Errorf("Uint64ToInt32(%d) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestUint64ToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input uint64
		want  int64
	}{
		{"zero", 0, 0},
		{"small value", 42, 42},
		{"max int64 boundary", math.MaxInt64, math.MaxInt64},
		{"exceeds max int64", uint64(math.MaxInt64) + 1, math.MaxInt64},
		{"max uint64", math.MaxUint64, math.MaxInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Uint64ToInt64(tt.input)
			if got != tt.want {
				t.Errorf("Uint64ToInt64(%d) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFloat64ToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input float64
		want  int64
	}{
		{"zero", 0.0, 0},
		{"positive", 42.7, 42},
		{"negative", -100.5, -100},
		{"exceeds max int64", float64(math.MaxInt64) * 2, math.MaxInt64},
		{"below min int64", float64(math.MinInt64) * 2, math.MinInt64},
		{"max int64 boundary", float64(math.MaxInt64), math.MaxInt64},
		{"min int64 boundary", float64(math.MinInt64), math.MinInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Float64ToInt64(tt.input)
			if got != tt.want {
				t.Errorf("Float64ToInt64(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIntToInt32(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int32
	}{
		{"zero", 0, 0},
		{"small positive", 42, 42},
		{"small negative", -42, -42},
		{"max int32 boundary", math.MaxInt32, math.MaxInt32},
		{"min int32 boundary", math.MinInt32, math.MinInt32},
		{"exceeds max int32", math.MaxInt32 + 1, math.MaxInt32},
		{"below min int32", math.MinInt32 - 1, math.MinInt32},
		{"large positive", math.MaxInt, math.MaxInt32},
		{"large negative", math.MinInt, math.MinInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IntToInt32(tt.input)
			if got != tt.want {
				t.Errorf("IntToInt32(%d) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMustFunctions(t *testing.T) {
	// Test the Must* variants with valid inputs
	t.Run("MustToUint32 with valid input", func(t *testing.T) {
		result := MustToUint32(100)
		if result != 100 {
			t.Errorf("MustToUint32(100) = %v, want 100", result)
		}
	})

	t.Run("MustToInt32 with valid input", func(t *testing.T) {
		result := MustToInt32(100)
		if result != 100 {
			t.Errorf("MustToInt32(100) = %v, want 100", result)
		}
	})

	t.Run("MustToUint32FromInt with valid input", func(t *testing.T) {
		result := MustToUint32FromInt(100)
		if result != 100 {
			t.Errorf("MustToUint32FromInt(100) = %v, want 100", result)
		}
	})

	t.Run("MustToInt32FromInt with valid input", func(t *testing.T) {
		result := MustToInt32FromInt(100)
		if result != 100 {
			t.Errorf("MustToInt32FromInt(100) = %v, want 100", result)
		}
	})

	t.Run("MustToInt with valid input", func(t *testing.T) {
		result := MustToInt(100)
		if result != 100 {
			t.Errorf("MustToInt(100) = %v, want 100", result)
		}
	})

	t.Run("MustToUint64 with valid input", func(t *testing.T) {
		result := MustToUint64(100)
		if result != 100 {
			t.Errorf("MustToUint64(100) = %v, want 100", result)
		}
	})
}

func TestMustToUint32Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustToUint32 did not panic on negative value")
		}
	}()
	MustToUint32(-1)
}

func TestMustToInt32Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustToInt32 did not panic on overflow")
		}
	}()
	MustToInt32(math.MaxUint32)
}

func TestMustToUint32FromIntPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustToUint32FromInt did not panic on negative value")
		}
	}()
	MustToUint32FromInt(-1)
}

func TestMustToInt32FromIntPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustToInt32FromInt did not panic on overflow")
		}
	}()
	MustToInt32FromInt(math.MaxInt32 + 1)
}

func TestMustToIntPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustToInt did not panic on overflow")
		}
	}()
	MustToInt(math.MaxUint64)
}

func TestMustToUint64Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustToUint64 did not panic on negative value")
		}
	}()
	MustToUint64(-1)
}
