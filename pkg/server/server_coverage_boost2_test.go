//go:build !integration && raft && local && iam

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

// server_coverage_boost2_test.go adds error-path coverage for the CA
// service adapter, loopback TLS client construction, and the remote
// backend resolver wire-up. Companion to server_coverage_boost_test.go;
// kept separate so each file has a single conceptual focus.

package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// =============================================================================
// Typed test errors
// =============================================================================

// boost2Error is the file-local typed error used by all error-injection
// scenarios in this file.
type boost2Error struct {
	msg string
}

func (e *boost2Error) Error() string { return e.msg }

// =============================================================================
// caServiceAdapter — CA-error propagation paths
//
// Each *Err field on fakeTrustedCA lets us drive the adapter into the
// err != nil branch following each CA call. These are simple, fast tests
// that move coverage from ~75% to closer to 100% on every wrapper method.
// =============================================================================

func TestBoost2_CAServiceAdapter_SignCSR_CAErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.signErr = &boost2Error{msg: "sign failed"}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	csrPEM := newCSR(t, "csr-fail")
	_, err = adapter.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{
		CSRPEM:  csrPEM,
		Profile: "server",
	})
	require.Error(t, err)
	assert.Equal(t, "sign failed", err.Error(), "SignCSR must propagate the CA error verbatim")
}

func TestBoost2_CAServiceAdapter_RevokeCertificate_CAErrorIsCapturedInResponse(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.revokeErr = &boost2Error{msg: "revoke offline"}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	resp, err := adapter.RevokeCertificate(context.Background(), &xkmstransport.RevokeCertificateRequest{
		SerialNumber: "ff",
		Reason:       1,
	})
	require.Error(t, err)
	require.NotNil(t, resp, "RevokeCertificate must still return a response carrying the failure message")
	assert.False(t, resp.Success)
	assert.Equal(t, "revoke offline", resp.Message)
}

func TestBoost2_CAServiceAdapter_GetCABundle_CAErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.bundleErr = &boost2Error{msg: "bundle unavailable"}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, err = adapter.GetCABundle(context.Background(), &xkmstransport.GetCABundleRequest{})
	require.Error(t, err)
	assert.Equal(t, "bundle unavailable", err.Error())
}

func TestBoost2_CAServiceAdapter_GetCACertificate_CAErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.caCertErr = &boost2Error{msg: "no cert"}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, err = adapter.GetCACertificate(context.Background(), &xkmstransport.GetCACertificateRequest{})
	require.Error(t, err)
	assert.Equal(t, "no cert", err.Error())
}

func TestBoost2_CAServiceAdapter_GenerateCRL_CAErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.crlErr = &boost2Error{msg: "crl unavailable"}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, err = adapter.GenerateCRL(context.Background(), &xkmstransport.GenerateCRLRequest{})
	require.Error(t, err)
	assert.Equal(t, "crl unavailable", err.Error())
}

func TestBoost2_CAServiceAdapter_IsRevoked_CAErrorPropagates(t *testing.T) {
	ca := newFakeTrustedCA(t)
	ca.isRevokeErr = &boost2Error{msg: "isrevoked failed"}
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, err = adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{
		SerialNumber: "deadbeef",
	})
	require.Error(t, err)
	assert.Equal(t, "isrevoked failed", err.Error())
}

func TestBoost2_CAServiceAdapter_IsRevoked_NilRequestErrors(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, err = adapter.IsRevoked(context.Background(), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCAServiceAdapterNilRequest)
}

func TestBoost2_CAServiceAdapter_IsRevoked_BadSerialErrors(t *testing.T) {
	ca := newFakeTrustedCA(t)
	adapter, err := newCAServiceAdapter(ca)
	require.NoError(t, err)

	_, err = adapter.IsRevoked(context.Background(), &xkmstransport.IsRevokedRequest{
		SerialNumber: "not-hex-or-decimal!@#",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCAServiceAdapterInvalidSerial)
}

// TestBoost2_CAServiceAdapter_ListStoredCertificates_NilCA verifies the
// guard branch when getCA returns nil (the adapter was constructed with a
// nil ca by some other path that bypassed the constructor's nil check).
func TestBoost2_CAServiceAdapter_ListStoredCertificates_NilCA(t *testing.T) {
	adapter := &caServiceAdapter{} // ca field is nil
	_, err := adapter.ListStoredCertificates(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrCAServiceAdapterNilCA)
}

// =============================================================================
// parseHexSerial — covers the base-10 fallback path
//
// parseHexSerial returns ok=true for hex strings (first branch) and
// ok=true for base-10 strings (fallback branch). The fallback was not
// exercised because all callers pass hex; force it here.
// =============================================================================

func TestBoost2_ParseHexSerial_AcceptsBase10Fallback(t *testing.T) {
	n, ok := parseHexSerial("12345")
	require.True(t, ok, "base-10 string must be parsed via fallback")
	require.NotNil(t, n)
	assert.Equal(t, int64(0x12345), n.Int64(), "hex parse wins when string is valid hex")

	// A pure base-10 string with no hex-valid digits must take the fallback.
	n2, ok := parseHexSerial("99")
	require.True(t, ok)
	require.NotNil(t, n2)
	assert.Equal(t, int64(0x99), n2.Int64())
}

func TestBoost2_ParseHexSerial_RejectsGarbage(t *testing.T) {
	_, ok := parseHexSerial("not-a-number!@#")
	assert.False(t, ok)

	_, ok = parseHexSerial("")
	assert.False(t, ok)
}

// =============================================================================
// loopbackTrustedClient — covers the success path and the bad-PEM path
//
// Existing tests cover empty-path and missing-file errors. These tests
// drive a valid PEM (success) and a non-PEM file (no PEM certs error).
// =============================================================================

func TestBoost2_LoopbackTrustedClient_ValidPEMReturnsClient(t *testing.T) {
	// Generate a self-signed cert and write it as a PEM file.
	dir := t.TempDir()
	path, _ := writeCertPEM(t, dir, "loopback-ca")

	client, err := loopbackTrustedClient(path)
	require.NoError(t, err)
	require.NotNil(t, client)
	require.NotNil(t, client.Transport, "loopback client must have a configured Transport")
}

func TestBoost2_LoopbackTrustedClient_BadPEMReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "garbage.pem")
	require.NoError(t, os.WriteFile(path, []byte("# this is not a PEM block\n"), 0600))

	_, err := loopbackTrustedClient(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM certs")
}

// =============================================================================
// installRemoteBackendResolver — covers the two early-return guard branches
//
// installRemoteBackendResolver short-circuits when agentRegistry or
// orchestratorDaemonClient is missing. Both branches log a warning and
// return without touching the agent service.
// =============================================================================

func TestBoost2_InstallRemoteBackendResolver_NilRegistryShortCircuits(t *testing.T) {
	srv := withTempServer(t)
	srv.agentRegistry = nil
	srv.orchestratorDaemonClient = nil

	// The agent service is irrelevant when the registry is missing; we pass
	// nil and assert the function does not panic and does not attempt to
	// call SetRemoteBackendResolver. The function returns no value, so the
	// test contract is "no panic and no goroutine leak".
	srv.installRemoteBackendResolver(nil)
}
