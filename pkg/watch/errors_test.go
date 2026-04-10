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

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWatchCreateError_Error verifies the error message includes the cause.
func TestWatchCreateError_Error(t *testing.T) {
	t.Run("formats message with cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := &WatchCreateError{Cause: cause}
		msg := err.Error()
		assert.Contains(t, msg, "failed to create watch")
		assert.Contains(t, msg, "connection refused")
	})

	t.Run("formats message with wrapped cause", func(t *testing.T) {
		inner := errors.New("timeout")
		cause := &WatchCreateError{Cause: inner}
		msg := cause.Error()
		assert.Contains(t, msg, "failed to create watch")
		assert.Contains(t, msg, "timeout")
	})
}

// TestWatchCreateError_Unwrap verifies the error chain supports errors.Is and errors.As.
func TestWatchCreateError_Unwrap(t *testing.T) {
	t.Run("unwrap returns cause", func(t *testing.T) {
		cause := errors.New("underlying issue")
		err := &WatchCreateError{Cause: cause}
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("errors.Is matches cause", func(t *testing.T) {
		sentinel := errors.New("sentinel error")
		err := &WatchCreateError{Cause: sentinel}
		require.True(t, errors.Is(err, sentinel))
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		cause := errors.New("some failure")
		err := &WatchCreateError{Cause: cause}

		var watchErr *WatchCreateError
		require.True(t, errors.As(err, &watchErr))
		assert.Equal(t, cause, watchErr.Cause)
	})
}
