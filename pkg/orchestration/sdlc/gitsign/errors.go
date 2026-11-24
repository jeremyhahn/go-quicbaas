// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package gitsign

import (
	"errors"
	"strconv"
)

// Sentinels every error in this package unwraps to.
var (
	// ErrInvalidInvocation means git, or whoever ran the helper, passed
	// arguments the helper does not accept.
	ErrInvalidInvocation = errors.New("git sign: invalid invocation")

	// ErrInvalidConfig means the helper's environment does not say how to
	// reach quicbaas or which key to sign with.
	ErrInvalidConfig = errors.New("git sign: invalid configuration")

	// ErrKey means the public key git named could not be used.
	ErrKey = errors.New("git sign: unusable signing key")

	// ErrSign means quicbaas did not return a usable signature.
	ErrSign = errors.New("git sign: signing failed")

	// ErrIO means the data could not be read or the signature not written.
	ErrIO = errors.New("git sign: i/o failure")
)

// InvalidInvocationError names the argument the helper refused.
type InvalidInvocationError struct {
	// Arg is the flag or value at fault, "" when an argument is missing.
	Arg     string
	Message string
}

// Error implements the error interface.
func (e *InvalidInvocationError) Error() string {
	if e.Arg == "" {
		return ErrInvalidInvocation.Error() + ": " + e.Message
	}
	return ErrInvalidInvocation.Error() + ": " + strconv.Quote(e.Arg) + ": " + e.Message
}

// Unwrap returns the sentinel.
func (e *InvalidInvocationError) Unwrap() error { return ErrInvalidInvocation }

// InvalidConfigError names the environment variable that is missing or wrong.
type InvalidConfigError struct {
	Variable string
	Message  string
	Cause    error
}

// Error implements the error interface.
func (e *InvalidConfigError) Error() string {
	msg := ErrInvalidConfig.Error() + ": " + e.Variable + ": " + e.Message
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *InvalidConfigError) Unwrap() []error { return withSentinel(ErrInvalidConfig, e.Cause) }

// KeyFileError is a -f key file that is unreadable or holds no public key.
type KeyFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *KeyFileError) Error() string {
	return ErrKey.Error() + ": " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause.
func (e *KeyFileError) Unwrap() []error { return withSentinel(ErrKey, e.Cause) }

// KeyMismatchError is a session key that is not the key git asked to sign
// with, so a signature from it would not verify against the committer.
type KeyMismatchError struct {
	// Want is the fingerprint of the key git named, Got the session key's.
	Want string
	Got  string
}

// Error implements the error interface.
func (e *KeyMismatchError) Error() string {
	return ErrKey.Error() + ": git asked for " + e.Want + " but the session key is " + e.Got
}

// Unwrap returns the sentinel.
func (e *KeyMismatchError) Unwrap() error { return ErrKey }

// SignError is a remote signature that was refused, failed or is unusable.
type SignError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *SignError) Error() string {
	msg := ErrSign.Error() + ": " + e.Message
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause.
func (e *SignError) Unwrap() []error { return withSentinel(ErrSign, e.Cause) }

// IOError is data that could not be read or a signature that could not be
// written.
type IOError struct {
	// Op is what was being done: "read data" or "write signature".
	Op    string
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *IOError) Error() string {
	return ErrIO.Error() + ": " + e.Op + " " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause.
func (e *IOError) Unwrap() []error { return withSentinel(ErrIO, e.Cause) }

// withSentinel is the Unwrap list for an error carrying a sentinel and an
// optional cause.
func withSentinel(sentinel, cause error) []error {
	if cause != nil {
		return []error{sentinel, cause}
	}
	return []error{sentinel}
}
