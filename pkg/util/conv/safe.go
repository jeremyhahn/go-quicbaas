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

// Package conv provides safe type conversion utilities to prevent integer overflow.
package conv

import (
	"fmt"
	"math"
)

// ConversionOverflowError represents an integer overflow during type conversion.
type ConversionOverflowError struct {
	FromType string
	ToType   string
	Value    string
}

// Error implements the error interface.
func (e *ConversionOverflowError) Error() string {
	return e.FromType + " value " + e.Value + " overflows " + e.ToType
}

// ConversionNegativeError represents an attempt to convert a negative value to an unsigned type.
type ConversionNegativeError struct {
	FromType string
	ToType   string
	Value    string
}

// Error implements the error interface.
func (e *ConversionNegativeError) Error() string {
	return "cannot convert negative " + e.FromType + " " + e.Value + " to " + e.ToType
}

// SafeIntToUint64 safely converts int to uint64, checking for overflow.
func SafeIntToUint64(v int) (uint64, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "int", ToType: "uint64", Value: fmt.Sprintf("%d", v)}
	}
	return uint64(v), nil
}

// SafeInt64ToUint64 safely converts int64 to uint64, checking for overflow.
func SafeInt64ToUint64(v int64) (uint64, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "int64", ToType: "uint64", Value: fmt.Sprintf("%d", v)}
	}
	return uint64(v), nil
}

// SafeUint64ToInt safely converts uint64 to int, checking for overflow.
func SafeUint64ToInt(v uint64) (int, error) {
	if v > math.MaxInt {
		return 0, &ConversionOverflowError{FromType: "uint64", ToType: "int", Value: fmt.Sprintf("%d", v)}
	}
	return int(v), nil
}

// SafeUint64ToInt64 safely converts uint64 to int64, checking for overflow.
func SafeUint64ToInt64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, &ConversionOverflowError{FromType: "uint64", ToType: "int64", Value: fmt.Sprintf("%d", v)}
	}
	return int64(v), nil
}

// SafeUint64ToInt32 safely converts uint64 to int32, checking for overflow.
func SafeUint64ToInt32(v uint64) (int32, error) {
	if v > math.MaxInt32 {
		return 0, &ConversionOverflowError{FromType: "uint64", ToType: "int32", Value: fmt.Sprintf("%d", v)}
	}
	return int32(v), nil
}

// SafeIntToUint16 safely converts int to uint16, checking for overflow.
func SafeIntToUint16(v int) (uint16, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "int", ToType: "uint16", Value: fmt.Sprintf("%d", v)}
	}
	if v > math.MaxUint16 {
		return 0, &ConversionOverflowError{FromType: "int", ToType: "uint16", Value: fmt.Sprintf("%d", v)}
	}
	return uint16(v), nil
}

// SafeIntToUint32 safely converts int to uint32, checking for overflow.
func SafeIntToUint32(v int) (uint32, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "int", ToType: "uint32", Value: fmt.Sprintf("%d", v)}
	}
	if v > math.MaxUint32 {
		return 0, &ConversionOverflowError{FromType: "int", ToType: "uint32", Value: fmt.Sprintf("%d", v)}
	}
	return uint32(v), nil
}

// SafeInt64ToUint32 safely converts int64 to uint32, checking for overflow.
func SafeInt64ToUint32(v int64) (uint32, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "int64", ToType: "uint32", Value: fmt.Sprintf("%d", v)}
	}
	if v > math.MaxUint32 {
		return 0, &ConversionOverflowError{FromType: "int64", ToType: "uint32", Value: fmt.Sprintf("%d", v)}
	}
	return uint32(v), nil
}

// SafeIntToInt32 safely converts int to int32, checking for overflow.
func SafeIntToInt32(v int) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, &ConversionOverflowError{FromType: "int", ToType: "int32", Value: fmt.Sprintf("%d", v)}
	}
	return int32(v), nil
}

// SafeUint32ToUint8 safely converts uint32 to uint8, checking for overflow.
func SafeUint32ToUint8(v uint32) (uint8, error) {
	if v > math.MaxUint8 {
		return 0, &ConversionOverflowError{FromType: "uint32", ToType: "uint8", Value: fmt.Sprintf("%d", v)}
	}
	return uint8(v), nil
}

// SafeIntToUint safely converts int to uint, checking for overflow.
func SafeIntToUint(v int) (uint, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "int", ToType: "uint", Value: fmt.Sprintf("%d", v)}
	}
	return uint(v), nil
}

// MustIntToUint64 converts int to uint64, panicking on negative values (for cases where value is guaranteed non-negative).
func MustIntToUint64(v int) uint64 {
	if v < 0 {
		panic(fmt.Sprintf("invalid negative value for uint64 conversion: %d", v))
	}
	return uint64(v)
}

// MustUint64ToInt converts uint64 to int, panicking on overflow (for cases where value is guaranteed to fit).
func MustUint64ToInt(v uint64) int {
	if v > math.MaxInt {
		panic(fmt.Sprintf("uint64 value %d exceeds maximum int value", v))
	}
	return int(v)
}

// MustUint64ToInt64 converts uint64 to int64, panicking on overflow (for cases where value is guaranteed to fit).
func MustUint64ToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		panic(fmt.Sprintf("uint64 value %d exceeds maximum int64 value", v))
	}
	return int64(v)
}

// Clamping conversions (for gosec G115 fixes) - these clamp values instead of returning errors

// ClampIntToUint32 safely converts int to uint32 by clamping to valid range.
func ClampIntToUint32(v int) uint32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}

// ClampIntToInt32 safely converts int to int32 by clamping to valid range.
func ClampIntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// ClampIntToUint16 safely converts int to uint16 by clamping to valid range.
func ClampIntToUint16(v int) uint16 {
	if v < 0 {
		return 0
	}
	if v > math.MaxUint16 {
		return math.MaxUint16
	}
	return uint16(v)
}

// ClampIntToUint safely converts int to uint by clamping to valid range.
func ClampIntToUint(v int) uint {
	if v < 0 {
		return 0
	}
	return uint(v)
}

// ClampUint64ToInt safely converts uint64 to int by clamping to valid range.
func ClampUint64ToInt(v uint64) int {
	if v > math.MaxInt {
		return math.MaxInt
	}
	return int(v)
}

// ClampUint64ToInt64 safely converts uint64 to int64 by clamping to valid range.
func ClampUint64ToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// ClampInt64ToUint64 safely converts int64 to uint64 by clamping to valid range.
func ClampInt64ToUint64(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// ClampUint32ToUint8 safely converts uint32 to uint8 by clamping to valid range.
func ClampUint32ToUint8(v uint32) uint8 {
	if v > math.MaxUint8 {
		return math.MaxUint8
	}
	return uint8(v)
}

// ClampIntToUint64 safely converts int to uint64 by clamping to valid range.
func ClampIntToUint64(v int) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}
