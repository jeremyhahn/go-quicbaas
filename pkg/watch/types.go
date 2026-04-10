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

package watch

// EventType represents the type of a watch event.
type EventType int32

const (
	// PUT indicates a key was created or updated.
	PUT EventType = 0
	// DELETE indicates a key was deleted.
	DELETE EventType = 1
)

// String returns the string representation of the event type.
func (et EventType) String() string {
	switch et {
	case PUT:
		return "PUT"
	case DELETE:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

// KeyValue represents a versioned key-value pair in the watch system.
type KeyValue struct {
	// Key is the key in bytes.
	Key []byte
	// CreateRevision is the revision of last creation on this key.
	CreateRevision int64
	// ModRevision is the revision of last modification on this key.
	ModRevision int64
	// Version is the version of the key. A deletion resets the version to zero.
	Version int64
	// Value is the value held by the key, in bytes.
	Value []byte
	// Lease is the ID of the lease that attached to key. 0 for no lease.
	Lease int64
}
