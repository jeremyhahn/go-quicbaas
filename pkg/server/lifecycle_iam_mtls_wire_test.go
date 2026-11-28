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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWireIAMMTLSAuthenticators_FullWiring drives the full success path of
// wireIAMMTLSAuthenticators: a non-embedded server with REST enabled brings up
// the REST IAM handler and the iamComponents (and thus AppStorage), and the
// operator MTLS config supplies a PKI client-auth CA, a TPM EK roots file, and
// an xkey roots file. Running initialize wires all three authenticators plus the
// TPM attestation resolver, covering the PKI/TPM/xkey load branches that the
// no-op and error-only unit tests cannot reach.
func TestWireIAMMTLSAuthenticators_FullWiring(t *testing.T) {
	saveAndRestoreSeams(t)

	dir := t.TempDir()
	pkiCA, _ := writeCertPEM(t, dir, "pki")
	tpmRoots, _ := writeCertPEM(t, dir, "tpm")
	xkeyRoots, _ := writeCertPEM(t, dir, "xkey")

	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)

	srv.config.Security.TLSCAFile = pkiCA
	srv.config.IAM.MTLS.TPMEKRootsFile = tpmRoots
	srv.config.IAM.MTLS.XKeyRootsFile = xkeyRoots

	require.NoError(t, srv.initialize(), "initialize with full mTLS wiring must succeed")
	require.NotNil(t, srv.restServer, "REST server must be started so the IAM handler exists")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))
}

// TestWireIAMMTLSAuthenticators_BadPKICAFile verifies the error branch when the
// configured PKI client-auth CA file is not valid PEM. The REST server is wired
// first so wireIAMMTLSAuthenticators reaches the CA-load step.
func TestWireIAMMTLSAuthenticators_BadPKICAFile(t *testing.T) {
	saveAndRestoreSeams(t)

	srv := withTempServer(t)
	srv.config.Server.Embedded = false
	srv.config.Server.REST.Enabled = true
	srv.config.Server.REST.Port = getFreePort(t)
	// A security.tls_ca_file that does not exist is a typed boot error.
	srv.config.Security.TLSCAFile = filepath.Join(t.TempDir(), "missing-pki.pem")

	err := srv.initialize()
	var anchorsErr *ClientTrustAnchorsError
	require.ErrorAs(t, err, &anchorsErr, "a missing tls_ca_file must abort initialize")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	assert.NoError(t, srv.Shutdown(ctx))
}
