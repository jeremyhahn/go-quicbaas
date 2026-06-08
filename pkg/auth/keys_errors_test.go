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

package auth

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReservedKeyPrefixError_Error verifies the error message includes the prefix.
func TestReservedKeyPrefixError_Error(t *testing.T) {
	t.Run("includes prefix in message", func(t *testing.T) {
		err := &ReservedKeyPrefixError{Prefix: "/_internal/"}
		msg := err.Error()
		assert.Contains(t, msg, "/_internal/")
		assert.Contains(t, msg, "reserved internal prefix")
	})

	t.Run("errors.As extracts typed error", func(t *testing.T) {
		err := &ReservedKeyPrefixError{Prefix: "/_system/"}
		var target *ReservedKeyPrefixError
		require.True(t, errors.As(err, &target))
		assert.Equal(t, "/_system/", target.Prefix)
	})
}
