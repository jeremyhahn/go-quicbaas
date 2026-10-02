//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package rest

import (
	"context"
	"crypto/x509"
	"errors"
	"net/http"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// ErrLoginTenantUnresolved is returned when a login presents neither a valid
// tenant-scoped SPIFFE SAN nor a home-realm-resolvable hint. The login MUST be
// rejected (fail-closed); it is never defaulted to the system tenant for a
// non-bootstrap login.
var ErrLoginTenantUnresolved = errors.New("login tenant could not be resolved: present a tenant-scoped client certificate or register a home-realm domain")

// ErrLoginCertReissueRequired is returned when a broker client certificate
// lacks the tenant-scoped SPIFFE SAN required after the hard cutover. The
// certificate must be reissued through the broker so it carries the SAN.
var ErrLoginCertReissueRequired = errors.New("client certificate has no tenant SPIFFE SAN; reissue it through the broker (hard cutover)")

// deriveLoginTenant resolves the tenant for a login in the locked priority
// order:
//
//  1. The tenant-scoped SPIFFE URI SAN on the verified client certificate,
//     with the operator-configured trust domain PINNED (a foreign trust domain
//     fails closed). The SO / bootstrap cert's tenant/system SAN resolves the
//     system tenant so the control plane works pre-unseal.
//  2. Home-realm discovery from the request's SNI/Host and/or the supplied
//     email, read from the shared secret-free routing directory.
//  3. FAIL CLOSED — a non-bootstrap login is never defaulted to a tenant.
//
// cert may be nil for non-mTLS logins (browser/WebAuthn/federated), in which
// case derivation goes straight to HRD.
//
// The resolution itself is delegated to the transport-neutral
// iam.TenantFromPeerCert core shared with the gRPC/QUIC transports. This
// wrapper layers REST's fail-closed login semantics on top of the core's pure
// resolution: an unresolved tenant is fatal for a login (unlike the
// best-effort interceptors), and the failure is mapped to the precise
// reissue-required / unresolved error.
func (s *Server) deriveLoginTenant(ctx context.Context, r *http.Request, cert *x509.Certificate, email string) (string, error) {
	var hrd iam.HomeRealmResolver
	if s.config.HomeRealmResolver != nil {
		hrd = restHomeRealmResolver{inner: s.config.HomeRealmResolver}
	}
	hints := iam.RealmHints{
		TenantHint: strings.TrimSpace(r.Header.Get("X-Tenant")),
		SNIHost:    requestSNIHost(r),
		Email:      email,
	}

	tenantID, err := iam.TenantFromPeerCert(ctx, cert, s.config.SPIFFETrustDomain, hrd, hints)
	if err == nil {
		return tenantID, nil
	}

	// Map the neutral unresolved signal to REST's fail-closed login errors,
	// preserving the original behavior exactly.
	var unresolved *iam.TenantUnresolvedError
	if errors.As(err, &unresolved) {
		switch {
		case errors.Is(unresolved.Cause, qrdbsdk.ErrSpiffeTrustDomainMismatch):
			// Foreign trust domain: fail closed.
			return "", ErrLoginTenantUnresolved
		case unresolved.Cause != nil && !errors.Is(unresolved.Cause, qrdbsdk.ErrSpiffeNoTenantID):
			// Malformed SPIFFE SAN: fail closed.
			return "", ErrLoginTenantUnresolved
		case cert != nil:
			// A broker cert with no usable SPIFFE SAN must be reissued.
			return "", ErrLoginCertReissueRequired
		default:
			return "", ErrLoginTenantUnresolved
		}
	}
	return "", err
}

// restHomeRealmResolver adapts the REST package's HomeRealmResolver contract to
// the iam.HomeRealmResolver the transport-neutral core consumes, keeping the
// core free of any REST dependency.
type restHomeRealmResolver struct {
	inner HomeRealmResolver
}

func (a restHomeRealmResolver) Resolve(ctx context.Context, hints iam.RealmHints) (string, error) {
	return a.inner.Resolve(ctx, RealmHints{
		TenantHint: hints.TenantHint,
		SNIHost:    hints.SNIHost,
		Email:      hints.Email,
	})
}

// tenantFromOAuthClientID extracts the tenant from an OAuth2 client identifier.
// Returns "" when the identifier does not carry a tenant.
//
// It delegates rather than reimplementing the grammar. The token endpoints
// scope their STORAGE context with this value while pkg/iam resolves the record
// with its own parser, so a second copy that drifts by one character puts the
// read in a namespace the write never touched -- which is the shape of
// go-quicbaas#644, and this was one of the two copies that had it.
func tenantFromOAuthClientID(clientID string) string {
	return iam.TenantFromClientID(clientID)
}

// requestSNIHost returns the SNI server name (preferred) or the HTTP Host.
func requestSNIHost(r *http.Request) string {
	if r.TLS != nil && r.TLS.ServerName != "" {
		return r.TLS.ServerName
	}
	host := r.Host
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	return host
}

// loginContextWithTenant returns a context carrying the derived tenant so the
// IAM index lookups that follow are scoped to it.
func loginContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return tenantctx.WithTenant(ctx, tenantID)
}
