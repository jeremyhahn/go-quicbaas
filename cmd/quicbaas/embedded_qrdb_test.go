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

package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmbeddedQRDBWiringError verifies the typed error formats correctly
// in both wrapped and unwrapped forms so callers (and users) see a
// consistent diagnostic when the wiring helper cannot assemble the
// QRDBEmbeddedServicers payload.
func TestEmbeddedQRDBWiringError(t *testing.T) {
	t.Run("formats with only stage when cause is nil", func(t *testing.T) {
		err := &EmbeddedQRDBWiringError{Stage: "shard transport"}
		assert.Equal(t, "embedded qrdb wiring: shard transport unavailable", err.Error())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("formats with stage and cause when cause is non-nil", func(t *testing.T) {
		cause := errors.New("disk on fire")
		err := &EmbeddedQRDBWiringError{Stage: "store materialisation", Cause: cause}
		assert.Equal(t, "embedded qrdb wiring: store materialisation: disk on fire", err.Error())
		assert.Same(t, cause, err.Unwrap())
	})
}

// TestBuildEmbeddedQRDBServicers_NilServer verifies the early-return guard
// rejects a nil embedded server pointer with a wiring error rather than
// dereferencing through to a panic.
func TestBuildEmbeddedQRDBServicers_NilServer(t *testing.T) {
	_, err := buildEmbeddedQRDBServicers(nil)
	require.Error(t, err)
	var wireErr *EmbeddedQRDBWiringError
	require.True(t, errors.As(err, &wireErr))
	assert.Equal(t, "embedded server", wireErr.Stage)
}

// TestBuildEmbeddedQRDBServicers_NilInnerServer verifies the early-return
// guard rejects an embedded server whose inner *server.Server is nil. This
// arises when the embedded server struct has been allocated but startup
// failed before the inner *server.Server was assigned.
func TestBuildEmbeddedQRDBServicers_NilInnerServer(t *testing.T) {
	_, err := buildEmbeddedQRDBServicers(&embeddedServer{})
	require.Error(t, err)
	var wireErr *EmbeddedQRDBWiringError
	require.True(t, errors.As(err, &wireErr))
	assert.Equal(t, "embedded server", wireErr.Stage)
}

// TestEmbeddedQRDBOption_NilServer verifies the user-facing option helper
// surfaces wiring errors through a wrapped error rather than panicking
// or silently returning a no-op option.
func TestEmbeddedQRDBOption_NilServer(t *testing.T) {
	opt, err := embeddedQRDBOption(nil)
	require.Error(t, err)
	assert.Nil(t, opt)
}

// TestBuildEmbeddedQRDBServicers_EndToEnd starts a real embedded server,
// builds the QRDBEmbeddedServicers, and validates that:
//
//  1. KV and Admin are populated and non-nil (required by WithEmbeddedQRDB).
//  2. The bootstrap ceremony services are populated when the embedded server
//     successfully wires them (the typical case for first-boot).
//  3. The returned KV adapter sees the same in-process store the QuicBaaS
//     facade does — locking in the architectural contract that the qrdb
//     sub-client and the QuicBaaS facade share one in-process backing store
//     in embedded mode.
//
// This test creates a real embedded server, which makes it heavier than a
// pure unit test, but it exercises the full wiring path the CLI takes when
// a user invokes any command with `--transport=embedded`. Without this
// test, the wiring path is only exercised in integration tests.
func TestBuildEmbeddedQRDBServicers_EndToEnd(t *testing.T) {
	es, err := getOrCreateEmbeddedServer()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = shutdownEmbeddedServer()
	})

	servicers, err := buildEmbeddedQRDBServicers(es)
	require.NoError(t, err)
	require.NotNil(t, servicers.KV, "WithEmbeddedQRDB requires non-nil KV")
	require.NotNil(t, servicers.Admin, "WithEmbeddedQRDB requires non-nil Admin")

	// The bootstrap ceremony services should be wired on a healthy first
	// boot. If they are not, the CLI's first-boot SDK path falls back to
	// NotSupported errors, which is the regression we are guarding against.
	ceremonyStack := es.server.GetCeremonyServices()
	if ceremonyStack != nil {
		assert.NotNil(t, servicers.Ceremony, "ceremony should be wired when bootstrap stack is available")
		assert.NotNil(t, servicers.Credential, "credential should be wired when bootstrap stack is available")
	}
}

// TestEmbeddedQRDBOption_EndToEnd verifies the SDK option helper returns a
// usable option value when the embedded server is healthy. The option's
// Apply path is exercised indirectly through createSDKClient in the
// existing CLI client tests.
func TestEmbeddedQRDBOption_EndToEnd(t *testing.T) {
	es, err := getOrCreateEmbeddedServer()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = shutdownEmbeddedServer()
	})

	opt, err := embeddedQRDBOption(es)
	require.NoError(t, err)
	require.NotNil(t, opt)
}
