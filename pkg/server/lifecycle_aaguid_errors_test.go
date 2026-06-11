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

// lifecycle_aaguid_errors_test.go covers the typed-error Error/Unwrap
// methods on AAGUIDAllowlistReadError and AAGUIDAllowlistParseError.
// Their happy paths are exercised indirectly by lifecycle_iam_aaguid
// tests; this file pins their diagnostic shape so operators see
// consistent messages regardless of failure mode.

package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAAGUIDAllowlistReadError_FormatAndUnwrap(t *testing.T) {
	cause := errors.New("permission denied")
	e := &AAGUIDAllowlistReadError{Path: "/etc/aaguid.yaml", Cause: cause}
	got := e.Error()
	assert.Contains(t, got, "iam.aaguid")
	assert.Contains(t, got, "read")
	assert.Contains(t, got, "/etc/aaguid.yaml")
	assert.Contains(t, got, "permission denied")
	assert.Same(t, cause, e.Unwrap())
}

func TestAAGUIDAllowlistParseError_FormatAndUnwrap(t *testing.T) {
	cause := errors.New("invalid yaml")
	e := &AAGUIDAllowlistParseError{Path: "/etc/aaguid.yaml", Cause: cause}
	got := e.Error()
	assert.Contains(t, got, "iam.aaguid")
	assert.Contains(t, got, "parse")
	assert.Contains(t, got, "/etc/aaguid.yaml")
	assert.Contains(t, got, "invalid yaml")
	assert.Same(t, cause, e.Unwrap())
}
