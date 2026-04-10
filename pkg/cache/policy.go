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

package cache

import "fmt"

// WritePolicy defines the policy for writing to L2 cache.
type WritePolicy int

const (
	// WriteThroughSync writes to L2 cache synchronously.
	// Set() operations will block until L2 write completes.
	// Provides strongest consistency but may impact latency.
	WriteThroughSync WritePolicy = iota

	// WriteThroughAsync writes to L2 cache asynchronously.
	// Set() operations return immediately after L1 write.
	// L2 writes happen in background goroutines (fire-and-forget).
	// Provides better latency but weaker consistency guarantees.
	WriteThroughAsync

	// WriteBack only writes to L1 cache.
	// L2 writes happen on L1 eviction asynchronously.
	// Provides best write latency but weakest consistency guarantees.
	// Evicted entries are written to L2 in background goroutines.
	WriteBack
)

// String returns the string representation of the write policy.
func (p WritePolicy) String() string {
	switch p {
	case WriteThroughSync:
		return "WriteThroughSync"
	case WriteThroughAsync:
		return "WriteThroughAsync"
	case WriteBack:
		return "WriteBack"
	default:
		return fmt.Sprintf("WritePolicy(%d)", p)
	}
}

// IsValid returns true if the write policy is valid and implemented.
func (p WritePolicy) IsValid() bool {
	return p >= WriteThroughSync && p <= WriteBack
}

// IsWriteThrough returns true if the policy writes through to L2.
func (p WritePolicy) IsWriteThrough() bool {
	return p == WriteThroughSync || p == WriteThroughAsync
}

// IsAsync returns true if the policy uses async writes.
func (p WritePolicy) IsAsync() bool {
	return p == WriteThroughAsync
}
