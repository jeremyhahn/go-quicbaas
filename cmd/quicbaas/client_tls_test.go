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

// Package main: coverage for buildTLSConfig in client.go.
package main

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withClientTLSGlobals installs a clean set of TLS-related globals and
// restores the originals on cleanup. (Distinct from agent_deploy's
// withTLSGlobals because that helper also resets tlsEnabled, which the
// client.go path does not honour.)
func withClientTLSGlobals(t *testing.T) {
	t.Helper()
	origDataDir := dataDir
	origCA := tlsCA
	origCert := tlsCert
	origKey := tlsKey
	t.Cleanup(func() {
		dataDir = origDataDir
		tlsCA = origCA
		tlsCert = origCert
		tlsKey = origKey
	})
	dataDir = ""
	tlsCA = ""
	tlsCert = ""
	tlsKey = ""
}

func TestBuildTLSConfig_NoCANoData(t *testing.T) {
	withClientTLSGlobals(t)
	cfg, err := buildTLSConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg, "without CA file or data-dir CA, expected nil tlsCfg")
}

func TestBuildTLSConfig_ExplicitCA(t *testing.T) {
	withClientTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedCertPEM(t, caPath)
	tlsCA = caPath

	cfg, err := buildTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
}

func TestBuildTLSConfig_AutoDiscoverFromDataDir(t *testing.T) {
	withClientTLSGlobals(t)
	dir := t.TempDir()
	dataDir = dir
	certsDir := filepath.Join(dir, "certs")
	require.NoError(t, os.MkdirAll(certsDir, 0o700))
	caPath := filepath.Join(certsDir, "ca-chain.crt")
	writeSelfSignedCertPEM(t, caPath)

	cfg, err := buildTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
}

func TestBuildTLSConfig_BadCAPath(t *testing.T) {
	withClientTLSGlobals(t)
	tlsCA = "/no/such/ca.pem"
	_, err := buildTLSConfig()
	require.Error(t, err)
}

func TestBuildTLSConfig_InvalidPEM(t *testing.T) {
	withClientTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, []byte("not a PEM"), 0o600))
	tlsCA = caPath
	_, err := buildTLSConfig()
	require.Error(t, err)
}

func TestBuildTLSConfig_WithClientCert(t *testing.T) {
	withClientTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedCertPEM(t, caPath)
	certPath := filepath.Join(dir, "cli.crt")
	keyPath := filepath.Join(dir, "cli.key")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	tlsCA = caPath
	tlsCert = certPath
	tlsKey = keyPath

	cfg, err := buildTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Len(t, cfg.Certificates, 1)
}

// TestBuildTLSConfig_ClientCertWithoutCAUsesSystemRoots covers an officer on
// another machine: no --tls-ca and no local ca-chain.crt, but a client pair.
// The pair must still be presented, and the server verified by the system
// roots.
func TestBuildTLSConfig_ClientCertWithoutCAUsesSystemRoots(t *testing.T) {
	withClientTLSGlobals(t)
	dataDir = t.TempDir() // no certs directory
	dir := t.TempDir()
	certPath := filepath.Join(dir, "officer.crt")
	keyPath := filepath.Join(dir, "officer.key")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	tlsCA = ""
	tlsCert = certPath
	tlsKey = keyPath

	cfg, err := buildTLSConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg, "a client pair without a CA file must still yield a TLS config")
	assert.Len(t, cfg.Certificates, 1, "the client certificate must be presented")
	assert.Nil(t, cfg.RootCAs, "without a CA file the system roots verify the server")
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)

	// A missing pair without a CA file is an error, not silently ignored.
	tlsCert = filepath.Join(dir, "missing.crt")
	tlsKey = filepath.Join(dir, "missing.key")
	cfg, err = buildTLSConfig()
	require.Error(t, err, "missing client cert files must be reported")
	assert.Nil(t, cfg)
}

func TestBuildTLSConfig_BadClientCert(t *testing.T) {
	withClientTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	writeSelfSignedCertPEM(t, caPath)
	tlsCA = caPath
	tlsCert = filepath.Join(dir, "missing.crt")
	tlsKey = filepath.Join(dir, "missing.key")

	_, err := buildTLSConfig()
	require.Error(t, err)
}
