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
	"time"
)

// maxDurationSeconds is the largest whole number of seconds a time.Duration
// holds; one more second overflows its int64 nanosecond count.
const maxDurationSeconds = math.MaxInt64 / int64(time.Second)

// SafeSecondsToDuration converts a whole number of seconds to a
// time.Duration. A count whose nanoseconds do not fit in an int64, roughly
// 292 years either side of zero, is refused rather than wrapped.
func SafeSecondsToDuration(seconds int64) (time.Duration, error) {
	if seconds > maxDurationSeconds || seconds < -maxDurationSeconds {
		return 0, &ConversionOverflowError{FromType: "seconds", ToType: "time.Duration", Value: strconv.FormatInt(seconds, 10)}
	}
	return time.Duration(seconds) * time.Second, nil
}
