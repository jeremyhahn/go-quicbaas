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

// ceremony_wiring_errors_test.go covers the typed-error Error() / Unwrap()
// surface of the ceremony wiring path. The happy-path constructor lives in
// ceremony_wiring.go and is tested elsewhere via the lifecycle integration
// tests; this file exercises the diagnostic shape operators see when the
// wiring fails.

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCeremonyWiringError_FormatAndUnwrap(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("disk full")
		e := &CeremonyWiringError{Stage: "barrier", Cause: cause}
		got := e.Error()
		assert.Contains(t, got, "ceremony wiring")
		assert.Contains(t, got, "barrier")
		assert.Contains(t, got, "disk full")
		assert.Same(t, cause, e.Unwrap())
		assert.True(t, errors.Is(e, cause))
	})

	t.Run("without cause", func(t *testing.T) {
		e := &CeremonyWiringError{Stage: "credential-core"}
		got := e.Error()
		assert.Contains(t, got, "ceremony wiring")
		assert.Contains(t, got, "credential-core")
		// Trailing ": " from the with-cause branch must NOT be present.
		assert.NotContains(t, got, "failed:")
		assert.Nil(t, e.Unwrap())
	})
}
