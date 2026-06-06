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

// Package main: coverage for proveCanonicalMTLS (the unified-login mTLS proof
// producer). mTLS possession is established by the TLS handshake, so the
// producer returns the certificate SHA-256 as the lookup key and a nil
// signature; the metadata carries the fingerprint + cert path.
package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProveCanonicalMTLS_NoCertReturnsMissingError(t *testing.T) {
	for _, env := range defaultCanonicalClientCertEnvs {
		t.Setenv(env, "")
	}
	t.Setenv("HOME", t.TempDir())
	_, _, _, err := proveCanonicalMTLS(context.Background(), &canonicalAuthLoginOptions{}, nil)
	require.Error(t, err)
	var typed *MissingClientCertError
	require.ErrorAs(t, err, &typed)
}

func TestProveCanonicalMTLS_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.pem")
	writePEMCertToPath(t, path)

	lookupKey, signature, meta, err := proveCanonicalMTLS(context.Background(), &canonicalAuthLoginOptions{
		clientCertPath: path,
		username:       "alice",
		factorID:       "factor-1",
	}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, lookupKey, "lookup key (cert sha256) must be present")
	assert.Nil(t, signature, "mTLS proof is the handshake; no in-band signature")
	assert.Contains(t, meta, "credential_sha256")
	assert.Contains(t, meta, "cert_path")
}

func TestProveCanonicalMTLS_BadPEM(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a PEM"), 0o600))

	_, _, _, err := proveCanonicalMTLS(context.Background(), &canonicalAuthLoginOptions{
		clientCertPath: path,
	}, nil)
	require.Error(t, err)
	var typed *CanonicalCertReadError
	require.ErrorAs(t, err, &typed)
}
