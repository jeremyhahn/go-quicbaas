// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest

package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// otherTenantID is a tenant the bootstrap admin does not belong to.
const otherTenantID = "cert-identity-other"

// certIdentityCase is one certificate-to-user resolution every transport must
// answer the same way. They are the cases the unit tier runs on the embedded,
// gRPC and Unix transports (sdk/go services_security_cert_identity_transports_test.go),
// here against the live daemon, whose qrdb node serves the lookup natively.
type certIdentityCase struct {
	name string
	// unbound presents a fresh certificate no user is bound to; otherwise the
	// bootstrap admin certificate, SPKI-bound in the system tenant.
	unbound bool
	// ctxTenant is the tenant carried on the request context.
	ctxTenant string
	// options configure the client, e.g. WithTenant.
	options   []quicbaas.Option
	wantFound bool
}

func certIdentityCases() []certIdentityCase {
	return []certIdentityCase{
		{
			name:      "found in the caller's tenant",
			ctxTenant: systemTenantID,
			wantFound: true,
		},
		{
			name:      "unbound certificate is NotFound",
			unbound:   true,
			ctxTenant: systemTenantID,
		},
		{
			name:      "another tenant's user is NotFound by context tenant",
			ctxTenant: otherTenantID,
		},
		{
			name:    "another tenant's user is NotFound by client tenant",
			options: []quicbaas.Option{quicbaas.WithTenant(otherTenantID)},
		},
		{
			name:      "context tenant overrides client tenant",
			ctxTenant: otherTenantID,
			options:   []quicbaas.Option{quicbaas.WithTenant(systemTenantID)},
		},
		{
			name:      "caller with no tenant sees every tenant",
			wantFound: true,
		},
	}
}

// TestAuthUserByCertificate_AcrossTransports proves Security().Auth()
// certificate identity on every network transport: the call reaches the
// server, a bound certificate resolves its user, and an unbound certificate
// or another tenant's user is the SDK's typed NotFound.
func TestAuthUserByCertificate_AcrossTransports(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	adminLeaf := loadAdminLeaf(t)
	unboundCert := newUnboundCert(t)

	for _, tr := range allLoginTransports() {
		for _, tc := range certIdentityCases() {
			t.Run(string(tr)+"/"+tc.name, func(t *testing.T) {
				c, err := newLoginClient(t, tr, mtlsTLSConfig(t), tc.options...)
				require.NoError(t, err, "build client")
				defer c.Close()

				cert := adminLeaf
				if tc.unbound {
					cert = unboundCert
				}
				callCtx := ctx
				if tc.ctxTenant != "" {
					callCtx = transport.ContextWithTenant(ctx, tc.ctxTenant)
				}

				got, err := c.Security().Auth().UserByCertificate(callCtx, cert)
				if !tc.wantFound {
					require.True(t, sdkerrors.IsNotFound(err),
						"want NotFoundError, got %+v, %T: %v", got, err, err)
					return
				}
				require.NoError(t, err, "UserByCertificate")
				require.Equal(t, adminSubject, got.Username, "resolved user")
				require.Equal(t, systemTenantID, got.TenantID, "resolved tenant")
			})
		}
	}
}

// newUnboundCert returns a certificate carrying a fresh public key, which no
// user can be bound to. UserByCertificate reads only the SubjectPublicKeyInfo.
func newUnboundCert(t *testing.T) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate key")
	spki, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err, "marshal public key")
	return &x509.Certificate{RawSubjectPublicKeyInfo: spki}
}
