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

// Package safeconv provides safe type conversion functions to prevent integer overflow.
package safeconv

import (
	"math"
	"strconv"
)

// NegativeConversionError is returned when a negative value cannot be converted to an unsigned type.
type NegativeConversionError struct {
	Value      string
	SourceType string
	TargetType string
}

// Error implements the error interface.
func (e *NegativeConversionError) Error() string {
	return "cannot convert negative " + e.SourceType + " " + e.Value + " to " + e.TargetType
}

// OverflowError is returned when a value exceeds the target type's maximum.
type OverflowError struct {
	Value      string
	SourceType string
	TargetType string
	Max        string
}

// Error implements the error interface.
func (e *OverflowError) Error() string {
	return e.SourceType + " value " + e.Value + " exceeds " + e.TargetType + " max (" + e.Max + ")"
}

// RangeError is returned when a value is outside the target type's range.
type RangeError struct {
	Value      string
	SourceType string
	TargetType string
	Min        string
	Max        string
}

// Error implements the error interface.
func (e *RangeError) Error() string {
	return e.SourceType + " value " + e.Value + " out of " + e.TargetType + " range [" + e.Min + ", " + e.Max + "]"
}

// ToUint32 safely converts an int32 to uint32 with bounds checking.
func ToUint32(v int32) (uint32, error) {
	if v < 0 {
		return 0, &NegativeConversionError{
			Value:      strconv.FormatInt(int64(v), 10),
			SourceType: "int32",
			TargetType: "uint32",
		}
	}
	return uint32(v), nil
}

// ToInt32 safely converts a uint32 to int32 with bounds checking.
func ToInt32(v uint32) (int32, error) {
	if v > math.MaxInt32 {
		return 0, &OverflowError{
			Value:      strconv.FormatUint(uint64(v), 10),
			SourceType: "uint32",
			TargetType: "int32",
			Max:        strconv.FormatInt(math.MaxInt32, 10),
		}
	}
	return int32(v), nil
}

// ToUint64 safely converts an int to uint64 with bounds checking.
func ToUint64(v int) (uint64, error) {
	if v < 0 {
		return 0, &NegativeConversionError{
			Value:      strconv.Itoa(v),
			SourceType: "int",
			TargetType: "uint64",
		}
	}
	return uint64(v), nil
}

// ToInt safely converts a uint64 to int with bounds checking.
func ToInt(v uint64) (int, error) {
	if v > uint64(math.MaxInt) {
		return 0, &OverflowError{
			Value:      strconv.FormatUint(v, 10),
			SourceType: "uint64",
			TargetType: "int",
			Max:        strconv.Itoa(math.MaxInt),
		}
	}
	return int(v), nil
}

// ToInt32FromInt safely converts an int to int32 with bounds checking.
func ToInt32FromInt(v int) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, &RangeError{
			Value:      strconv.Itoa(v),
			SourceType: "int",
			TargetType: "int32",
			Min:        strconv.FormatInt(math.MinInt32, 10),
			Max:        strconv.FormatInt(math.MaxInt32, 10),
		}
	}
	return int32(v), nil
}

// ToUint32FromInt safely converts an int to uint32 with bounds checking.
func ToUint32FromInt(v int) (uint32, error) {
	if v < 0 {
		return 0, &NegativeConversionError{
			Value:      strconv.Itoa(v),
			SourceType: "int",
			TargetType: "uint32",
		}
	}
	if v > math.MaxUint32 {
		return 0, &OverflowError{
			Value:      strconv.Itoa(v),
			SourceType: "int",
			TargetType: "uint32",
			Max:        strconv.FormatUint(math.MaxUint32, 10),
		}
	}
	return uint32(v), nil
}

// Uint64ToInt32 safely converts a uint64 to int32 with bounds checking.
// Values exceeding math.MaxInt32 are clamped to math.MaxInt32.
func Uint64ToInt32(v uint64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}

// Uint64ToInt64 safely converts a uint64 to int64 with bounds checking.
// Values exceeding math.MaxInt64 are clamped to math.MaxInt64.
func Uint64ToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// Float64ToInt64 safely converts a float64 to int64 with bounds checking.
// Values outside int64 range are clamped to min/max int64.
func Float64ToInt64(v float64) int64 {
	if v >= float64(math.MaxInt64) {
		return math.MaxInt64
	}
	if v <= float64(math.MinInt64) {
		return math.MinInt64
	}
	return int64(v)
}

// IntToInt32 safely converts an int to int32 with clamping.
// Values outside int32 range are clamped to math.MinInt32/math.MaxInt32.
func IntToInt32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// MustToUint32 converts int32 to uint32, panicking on error (use for validated values only).
func MustToUint32(v int32) uint32 {
	result, err := ToUint32(v)
	if err != nil {
		panic(err)
	}
	return result
}

// MustToInt32 converts uint32 to int32, panicking on error (use for validated values only).
func MustToInt32(v uint32) int32 {
	result, err := ToInt32(v)
	if err != nil {
		panic(err)
	}
	return result
}

// MustToUint32FromInt converts int to uint32, panicking on error.
func MustToUint32FromInt(v int) uint32 {
	result, err := ToUint32FromInt(v)
	if err != nil {
		panic(err)
	}
	return result
}

// MustToInt32FromInt converts int to int32, panicking on error.
func MustToInt32FromInt(v int) int32 {
	result, err := ToInt32FromInt(v)
	if err != nil {
		panic(err)
	}
	return result
}

// MustToInt converts uint64 to int, panicking on error.
func MustToInt(v uint64) int {
	result, err := ToInt(v)
	if err != nil {
		panic(err)
	}
	return result
}

// MustToUint64 converts int to uint64, panicking on error.
func MustToUint64(v int) uint64 {
	result, err := ToUint64(v)
	if err != nil {
		panic(err)
	}
	return result
}
