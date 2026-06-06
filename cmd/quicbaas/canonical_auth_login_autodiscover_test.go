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

// Package main: coverage for autoDiscoverCanonicalAuthMethod (mTLS branch).
package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutoDiscoverCanonicalAuthMethod_MTLSFound returns "mtls" when an
// explicit client-cert is supplied. The downstream FIDO2/PIV probes are
// skipped entirely (no hardware mocks needed).
func TestAutoDiscoverCanonicalAuthMethod_MTLSFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.pem")
	writePEMCertToPath(t, path)

	got, err := autoDiscoverCanonicalAuthMethod(context.Background(),
		&canonicalAuthLoginOptions{clientCertPath: path})
	require.NoError(t, err)
	assert.Equal(t, "mtls", got)
}
