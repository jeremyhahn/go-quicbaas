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

package service

import (
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
)

// =============================================================================
// isAlreadyInitializedError / containsAlreadyInitializedMsg.
//
// These guards detect the qrdb "already initialized" diagnostic across the
// sentinel-error and gRPC-wrapped-string transports. The substring scanner was
// only partially exercised; this pins both the sentinel and message paths plus
// the negative branches.
// =============================================================================

func TestIsAlreadyInitializedError(t *testing.T) {
	t.Run("nil error is not already-initialized", func(t *testing.T) {
		assert.False(t, isAlreadyInitializedError(nil))
	})

	t.Run("sentinel error", func(t *testing.T) {
		assert.True(t, isAlreadyInitializedError(qrdbsdk.ErrAlreadyInitialized))
	})

	t.Run("wrapped sentinel", func(t *testing.T) {
		wrapped := errors.Join(errors.New("rpc error"), qrdbsdk.ErrAlreadyInitialized)
		assert.True(t, isAlreadyInitializedError(wrapped))
	})

	t.Run("gRPC string carrying the marker", func(t *testing.T) {
		err := errors.New("rpc error: code = FailedPrecondition desc = barrier already initialized")
		assert.True(t, isAlreadyInitializedError(err))
	})

	t.Run("unrelated error", func(t *testing.T) {
		assert.False(t, isAlreadyInitializedError(errors.New("connection refused")))
	})
}

func TestContainsAlreadyInitializedMsg(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want bool
	}{
		{"exact marker", "barrier already initialized", true},
		{"marker at end", "error: barrier already initialized", true},
		{"marker at start", "barrier already initialized now", true},
		{"empty string", "", false},
		{"shorter than marker", "barrier", false},
		{"no marker", "barrier not initialized yet", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, containsAlreadyInitializedMsg(tc.msg))
		})
	}
}
