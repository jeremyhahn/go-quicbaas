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
	"strconv"
)

// uint64Span is 2^64, the first float64 a uint64 cannot hold.
const uint64Span = 1 << 64

// SafeFloat64ToUint64 converts a float64 to uint64, truncating any fraction
// toward zero as a Go conversion does. A negative value, NaN, or a value of
// 2^64 or more has no uint64 form and is refused, where a bare conversion
// would yield an implementation-defined result.
func SafeFloat64ToUint64(v float64) (uint64, error) {
	if v < 0 {
		return 0, &ConversionNegativeError{FromType: "float64", ToType: "uint64", Value: formatFloat64(v)}
	}
	if math.IsNaN(v) || v >= uint64Span {
		return 0, &ConversionOverflowError{FromType: "float64", ToType: "uint64", Value: formatFloat64(v)}
	}
	return uint64(v), nil
}

// intSpan is 2^63 on a 64-bit platform (2^31 on 32-bit): the magnitude of
// math.MinInt, and the first positive float64 an int cannot hold.
const intSpan = -float64(math.MinInt)

// SafeFloat64ToInt converts a float64 to int, truncating any fraction
// toward zero as a Go conversion does. NaN, or a value outside the range of
// int, is refused with a ConversionOverflowError.
func SafeFloat64ToInt(v float64) (int, error) {
	if math.IsNaN(v) || v >= intSpan || v < -intSpan {
		return 0, &ConversionOverflowError{FromType: "float64", ToType: "int", Value: formatFloat64(v)}
	}
	return int(v), nil
}

func formatFloat64(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
