//go:build !integration

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

// errors_typed_test.go exercises the typed-error Error() / Unwrap()
// branches that the broader test surface only reaches indirectly. Each
// test constructs the typed error directly and asserts both the human-
// readable message and the sentinel chain so errors.Is + errors.As callers
// see consistent diagnostics regardless of which code path produced the
// error.

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIAMComponentInitError_FormatAndUnwrap(t *testing.T) {
	cause := errors.New("disk full")
	e := &IAMComponentInitError{Component: "audit logger", Cause: cause}
	assert.Equal(t, "failed to create audit logger: disk full", e.Error())
	// Unwrap returns the sentinel for errors.Is traversal.
	assert.True(t, errors.Is(e, ErrIAMComponentInit))
}

func TestBootstrapPasswordRequiredError_Format(t *testing.T) {
	e := &BootstrapPasswordRequiredError{}
	assert.Contains(t, e.Error(), "required")
	assert.True(t, errors.Is(e, ErrInsecureBootstrapPassword))
}

func TestBootstrapPasswordNotAllowedError_Format(t *testing.T) {
	e := &BootstrapPasswordNotAllowedError{}
	assert.Contains(t, e.Error(), "not allowed")
	assert.True(t, errors.Is(e, ErrInsecureBootstrapPassword))
}

func TestBootstrapPasswordMatchesUsernameError_Format(t *testing.T) {
	e := &BootstrapPasswordMatchesUsernameError{}
	assert.Contains(t, e.Error(), "must not match")
	assert.True(t, errors.Is(e, ErrInsecureBootstrapPassword))
}

func TestUpdateCANotReadyError_FormatAndUnwrap(t *testing.T) {
	e := UpdateCANotReadyError{}
	assert.Contains(t, e.Error(), "CA adapter not ready")
	assert.True(t, errors.Is(e, ErrUpdateCANotReady))
}

// TODO Phase F: Add integration test that asserts node ForShard returns the
// same node-backed client across shard IDs (the ShardedClientNotEmbeddedError
// guard was removed with EmbeddedTransportForShard in Phase E).
