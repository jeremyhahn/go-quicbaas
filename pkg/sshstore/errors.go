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

package sshstore

import (
	"io/fs"
	"strconv"
)

// NilInputError reports a required argument that was not supplied.
type NilInputError struct {
	Field string
}

// Error implements the error interface.
func (e *NilInputError) Error() string {
	return "sshstore: " + e.Field + " is required"
}

// LegacyStoreError wraps a failure reading, closing, or destroying the legacy
// plaintext file store. Op names the stage; Path is the file or directory
// involved.
type LegacyStoreError struct {
	Op   string
	Path string
	Err  error
}

// Error implements the error interface.
func (e *LegacyStoreError) Error() string {
	return "sshstore: legacy store " + e.Op + " " + e.Path + ": " + e.Err.Error()
}

// Unwrap returns the underlying failure.
func (e *LegacyStoreError) Unwrap() error { return e.Err }

// SealedStoreError wraps a failure reading or writing the sealed store.
type SealedStoreError struct {
	Op  string
	Key string
	Err error
}

// Error implements the error interface.
func (e *SealedStoreError) Error() string {
	return "sshstore: sealed store " + e.Op + " " + e.Key + ": " + e.Err.Error()
}

// Unwrap returns the underlying failure.
func (e *SealedStoreError) Unwrap() error { return e.Err }

// RecordConflictError reports a record the sealed store already holds with
// different contents. The migration refuses to overwrite it and leaves the
// legacy store untouched, so an operator decides which copy is authoritative.
type RecordConflictError struct {
	Key string
}

// Error implements the error interface.
func (e *RecordConflictError) Error() string {
	return "sshstore: sealed store already holds a different record at " + e.Key
}

// RecordMismatchError reports a record whose bytes read back from the sealed
// store differ from the legacy bytes that were written.
type RecordMismatchError struct {
	Key string
}

// Error implements the error interface.
func (e *RecordMismatchError) Error() string {
	return "sshstore: sealed store read-back of " + e.Key + " does not match the legacy record"
}

// KeyVerificationError reports a key whose sealed copy failed to prove it
// holds the same key pair as the legacy copy. Reason names the check that
// failed; Err carries the underlying failure when there is one.
type KeyVerificationError struct {
	Key    string
	Reason string
	Err    error
}

// Error implements the error interface.
func (e *KeyVerificationError) Error() string {
	msg := "sshstore: verify key " + e.Key + ": " + e.Reason
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying failure, if any.
func (e *KeyVerificationError) Unwrap() error { return e.Err }

// UnparsedKeysError reports legacy key records the go-xkms key store could not
// parse. Such a record cannot be verified, so the migration refuses to destroy
// the plaintext it came from.
type UnparsedKeysError struct {
	Records int
	Parsed  int
}

// Error implements the error interface.
func (e *UnparsedKeysError) Error() string {
	return "sshstore: " + strconv.Itoa(e.Records) + " legacy key records but only " +
		strconv.Itoa(e.Parsed) + " parse as keys"
}

// UnexpectedEntryError reports a filesystem entry in the legacy tree that is
// neither a regular file nor a directory. The wipe does not follow or remove
// it; an operator must inspect it.
type UnexpectedEntryError struct {
	Path string
	Mode fs.FileMode
}

// Error implements the error interface.
func (e *UnexpectedEntryError) Error() string {
	return "sshstore: unexpected entry " + e.Path + " (" + e.Mode.String() + ") in legacy store"
}
