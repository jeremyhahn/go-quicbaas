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

// Package main: coverage for tls_helper.go.
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGRPCCAOnlyTLSOption_EmptyCAFile(t *testing.T) {
	_, err := grpcCAOnlyTLSOption("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CA file path is required")
}

func TestGRPCCAOnlyTLSOption_MissingFile(t *testing.T) {
	_, err := grpcCAOnlyTLSOption("/no/such/ca.pem")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read CA cert")
}

func TestGRPCCAOnlyTLSOption_NotPEM(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(p, []byte("not a PEM"), 0o600))
	_, err := grpcCAOnlyTLSOption(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM certs in CA bundle")
}

func TestGRPCCAOnlyTLSOption_Happy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ca.pem")
	writeSelfSignedCertPEM(t, p)
	opt, err := grpcCAOnlyTLSOption(p)
	require.NoError(t, err)
	require.NotNil(t, opt)
}
