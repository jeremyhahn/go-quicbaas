//go:build mcp

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

package mcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/xkmsca"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	"github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// The xkms.ca.* tools serve through the same xkmsca adapter as the REST CA
// routes, so each returns the servicer's result instead of
// OperationNotSupported (go-quicbaas#1631).
func TestTrustedCATools_ServeThroughTheSharedServicer(t *testing.T) {
	servicer := xkmsca.New(testca.New(t, "mcp-ca"), "example.test")
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	registry.SetCAService(servicer)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	// Read-only verbs return exactly what the servicer (and so REST) returns.
	wantBundle, err := servicer.GetCABundle(ctx, &transport.GetCABundleRequest{})
	require.NoError(t, err)
	gotBundle, err := registry.handleTrustedCAGetBundle(ctx, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, mustMarshalResult(t, wantBundle), gotBundle)

	wantCert, err := servicer.GetCACertificate(ctx, &transport.GetCACertificateRequest{})
	require.NoError(t, err)
	gotCert, err := registry.handleTrustedCAGetCert(ctx, map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, mustMarshalResult(t, wantCert), gotCert)

	// Issue, sign, revoke and check: a full round trip through the tools.
	issued, err := registry.handleTrustedCAIssueCert(ctx, map[string]interface{}{
		"profile": "client", "common_name": "bob", "validity_days": 1, "tenant_id": "tenant-a",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, caResultField(t, issued, "certificate_pem"))

	signed, err := registry.handleTrustedCASignCSR(ctx, map[string]interface{}{
		"csr_pem": newMCPTestCSR(t), "profile": "client", "tenant_id": "tenant-a",
	})
	require.NoError(t, err)
	serial, ok := caResultField(t, signed, "serial_number").(string)
	require.True(t, ok)
	require.NotEmpty(t, serial)

	revoked, err := registry.handleTrustedCAIsRevoked(ctx, map[string]interface{}{"serial_number": serial})
	require.NoError(t, err)
	assert.Equal(t, false, caResultField(t, revoked, "revoked"))

	_, err = registry.handleTrustedCARevokeCert(ctx, map[string]interface{}{"serial_number": serial, "reason": 1})
	require.NoError(t, err)

	revoked, err = registry.handleTrustedCAIsRevoked(ctx, map[string]interface{}{"serial_number": serial})
	require.NoError(t, err)
	assert.Equal(t, true, caResultField(t, revoked, "revoked"))

	crl, err := registry.handleTrustedCAGenerateCRL(ctx, map[string]interface{}{})
	require.NoError(t, err)
	assert.NotEmpty(t, caResultField(t, crl, "crl_pem"))
}

// Another tenant's issuance is refused by the shared servicer's tenant check.
func TestTrustedCATools_RefuseAnotherTenant(t *testing.T) {
	registry := NewToolRegistry(newMinimalMockService(), newTestLogger(t))
	registry.SetCAService(xkmsca.New(testca.New(t, "mcp-ca-tenant"), "example.test"))
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	_, err := registry.handleTrustedCAIssueCert(ctx, map[string]interface{}{
		"profile": "client", "common_name": "bob", "validity_days": 1, "tenant_id": "tenant-b",
	})
	require.Error(t, err)
}

func mustMarshalResult(t *testing.T, v interface{}) interface{} {
	t.Helper()
	out, err := marshalResult(v)
	require.NoError(t, err)
	return out
}

func caResultField(t *testing.T, result interface{}, key string) interface{} {
	t.Helper()
	m, ok := result.(map[string]interface{})
	require.True(t, ok, "result is %T, want a JSON object", result)
	return m[key]
}

func newMCPTestCSR(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "alice"},
	}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}
