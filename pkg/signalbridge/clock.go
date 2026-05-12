// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"sync/atomic"
	"time"
)

// clockNowMilli returns a strictly-monotonic millisecond timestamp.
// This is the package-wide source of truth for Signal message
// timestamps; both SignalClient and EventTranslator use it so their
// timestamp streams do not collide when the same conversation has
// simultaneous outbound activity.
//
// The atomic counter guarantees that two calls in the same wall-clock
// millisecond still produce distinct IDs, which is required by the
// Signal Protocol (timestamp is the message ID used for Edit and
// ReplyQuote references).
func clockNowMilli() int64 {
	for {
		cur := clockCounter.Load()
		now := time.Now().UnixMilli()
		next := cur + 1
		if now > next {
			next = now
		}
		if clockCounter.CompareAndSwap(cur, next) {
			return next
		}
	}
}

// clockCounter is the process-wide monotonic clock state.
var clockCounter atomic.Int64
