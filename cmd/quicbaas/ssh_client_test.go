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

// Package main: coverage for ssh_client.go helpers and the
// CLISSHClientBuildError typed error.
package main

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// CLISSHClientBuildError
// =============================================================================

func TestCLISSHClientBuildError_MessageAndUnwrap(t *testing.T) {
	inner := stderrors.New("permission denied")
	e := &CLISSHClientBuildError{Op: "open_store", Err: inner}
	assert.Contains(t, e.Error(), "open_store")
	assert.Contains(t, e.Error(), "permission denied")
	assert.ErrorIs(t, e, inner)
}

// =============================================================================
// cliSSHStoreRoot
// =============================================================================

func TestCLISSHStoreRoot_UsesHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	got, err := cliSSHStoreRoot()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(got, dir), "expected store under HOME dir; got %s", got)
	assert.Contains(t, got, CLISSHStoreSubdir)
}

// =============================================================================
// cliSSHOwner
// =============================================================================

func TestCLISSHOwner_PrefersUSER(t *testing.T) {
	t.Setenv("USER", "alice")
	t.Setenv("LOGNAME", "bob")
	assert.Equal(t, "alice", cliSSHOwner())
}

func TestCLISSHOwner_FallsBackToLOGNAME(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "bob")
	assert.Equal(t, "bob", cliSSHOwner())
}

func TestCLISSHOwner_FallsBackToDefault(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	assert.Equal(t, "default", cliSSHOwner())
}

// =============================================================================
// buildCLISSHClient — embedded path; clears XKMSD_URL so the REST branch
// is not taken (REST requires connectivity to a live xkmsd).
// =============================================================================

func TestBuildCLISSHClient_EmbeddedStore(t *testing.T) {
	t.Setenv(XKMSDURLEnv, "")
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	c, err := buildCLISSHClient()
	require.NoError(t, err)
	require.NotNil(t, c)
	_ = c.Close()

	// Confirm the store directory was created where expected.
	storePath := filepath.Join(dir, CLISSHStoreSubdir)
	st, err := os.Stat(storePath)
	require.NoError(t, err)
	assert.True(t, st.IsDir())
}

// =============================================================================
// buildCLISSHHTTPClient — TLS knobs
// =============================================================================

func TestBuildCLISSHHTTPClient_NoFlags(t *testing.T) {
	withTLSGlobals(t)
	c, err := buildCLISSHHTTPClient()
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestBuildCLISSHHTTPClient_BadCAPath(t *testing.T) {
	withTLSGlobals(t)
	tlsCA = "/no/such/ca.pem"
	_, err := buildCLISSHHTTPClient()
	require.Error(t, err)
}

func TestBuildCLISSHHTTPClient_InvalidCAPEM(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, []byte("not a PEM"), 0o600))
	tlsCA = caPath
	_, err := buildCLISSHHTTPClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid PEM")
}

func TestBuildCLISSHHTTPClient_LoadsClientCert(t *testing.T) {
	withTLSGlobals(t)
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cli.crt")
	keyPath := filepath.Join(dir, "cli.key")
	writeSelfSignedKeyPair(t, certPath, keyPath)
	tlsCert = certPath
	tlsKey = keyPath

	c, err := buildCLISSHHTTPClient()
	require.NoError(t, err)
	require.NotNil(t, c)
}
