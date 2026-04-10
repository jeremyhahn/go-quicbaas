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

package interfaces

import (
	"context"
	"io"
)

// StateMachine defines the interface for Raft replicated state machines.
// All state is maintained in memory and replicated via the Raft log.
type StateMachine interface {
	// Update applies committed Raft log entries to the state machine.
	// Each entry's result is written to the corresponding index in results.
	Update(ctx context.Context, entries []Entry, results []Result) error

	// Lookup queries the state machine without modifying state.
	// The query parameter is implementation-defined.
	Lookup(ctx context.Context, query interface{}) (interface{}, error)

	// SaveSnapshot serializes the state machine state to the writer.
	// The stopper channel signals cancellation from the Raft framework.
	SaveSnapshot(ctx context.Context, w io.Writer, stopper <-chan struct{}) error

	// RecoverFromSnapshot restores the state machine from a snapshot
	// read from the reader. The stopper channel signals cancellation.
	RecoverFromSnapshot(ctx context.Context, r io.Reader, stopper <-chan struct{}) error

	// Close releases all resources held by the state machine.
	Close(ctx context.Context) error
}

// OnDiskStateMachine defines the interface for on-disk state machines.
// These state machines persist their state to disk for durability and
// support efficient snapshotting through the prepare/save two-phase protocol.
type OnDiskStateMachine interface {
	// Open initializes the on-disk state machine using the provided directory.
	// Returns the last applied Raft log index for recovery. The stopper
	// channel signals cancellation from the Raft framework.
	Open(ctx context.Context, initialDir string, stopper <-chan struct{}) (uint64, error)

	// Update applies committed Raft log entries to the state machine.
	// Each entry's result is written to the corresponding index in results.
	Update(ctx context.Context, entries []Entry, results []Result) error

	// Lookup queries the state machine without modifying state.
	// The query parameter is implementation-defined.
	Lookup(ctx context.Context, query interface{}) (interface{}, error)

	// Sync flushes all pending writes to stable storage.
	Sync() error

	// PrepareSnapshot captures any state required for a consistent snapshot.
	// The returned context is passed to SaveSnapshot and is implementation-defined.
	PrepareSnapshot() (interface{}, error)

	// SaveSnapshot serializes the state machine state to the writer using
	// the snapshot context returned by PrepareSnapshot. The stopper channel
	// signals cancellation from the Raft framework.
	SaveSnapshot(ctx context.Context, snapshotCtx interface{}, w io.Writer, stopper <-chan struct{}) error

	// RecoverFromSnapshot restores the state machine from a snapshot
	// read from the reader. The stopper channel signals cancellation.
	RecoverFromSnapshot(ctx context.Context, r io.Reader, stopper <-chan struct{}) error

	// Close releases all resources held by the state machine.
	Close(ctx context.Context) error
}

// SnapshotManager defines the interface for managing Raft snapshots.
// It handles snapshot creation, loading, and streaming.
type SnapshotManager interface {
	// SaveSnapshot saves a snapshot to the specified writer.
	// Returns the number of bytes written and any error.
	SaveSnapshot(w io.Writer) (int64, error)

	// RecoverFromSnapshot loads a snapshot from the specified reader.
	// Returns the number of bytes read and any error.
	RecoverFromSnapshot(r io.Reader) (int64, error)

	// Close releases resources held by the snapshot manager.
	Close() error
}
