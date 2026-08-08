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

package kvstore

import "errors"

// ErrKVStore is the sentinel every error from this package unwraps to.
var ErrKVStore = errors.New("kvstore")

// ErrEmptyKey indicates an operation was given an empty key.
var ErrEmptyKey = errors.New("kvstore: empty key")

// ErrClosed indicates an operation was attempted on a closed store.
var ErrClosed = errors.New("kvstore: store is closed")

// EmptyKeyError is returned when a write is attempted with an empty key.
// Keys are the only addressing mechanism the engine has, so an empty key is a
// programming error rather than a missing record.
type EmptyKeyError struct {
	Op string
}

// Error implements the error interface.
func (e *EmptyKeyError) Error() string {
	return ErrEmptyKey.Error() + ": " + e.Op
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *EmptyKeyError) Unwrap() error { return ErrEmptyKey }

// OpError wraps a failure reported by the underlying qrdb storage engine and
// records which operation and key produced it.
type OpError struct {
	Op    string
	Key   string
	Cause error
}

// Error implements the error interface.
func (e *OpError) Error() string {
	msg := ErrKVStore.Error() + ": " + e.Op + " failed"
	if e.Key != "" {
		msg += " for key " + e.Key
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying engine error so callers can inspect it, and
// falls back to the package sentinel when there is none.
func (e *OpError) Unwrap() error {
	if e.Cause != nil {
		return e.Cause
	}
	return ErrKVStore
}
