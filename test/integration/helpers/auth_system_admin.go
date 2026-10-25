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

//go:build integration

package helpers

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// systemAdminPrincipalName names the fixture system admin in errors.
const systemAdminPrincipalName = "system admin"

// systemAdminAuth is the bootstrap system admin: the fixture client cert the
// cluster binds at first boot. It belongs to the system tenant, so it is the
// principal for cluster-wide routes (backups, cluster and tenant management,
// metrics) that no tenant principal may reach. It is never released: the
// system tenant outlives every suite.
var systemAdminAuth = newPrincipalRegistry(systemAdminPrincipalName, enrollSystemAdmin, loginWithPIVCA)

// NewSystemAdminHTTPClient returns NewHTTPClient's client with every request
// carrying the system admin's bearer.
func NewSystemAdminHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	return systemAdminAuth.httpClient(t, timeout)
}

// SystemAdminHTTPClient is NewSystemAdminHTTPClient returning its error, for
// callers without a *testing.T.
func SystemAdminHTTPClient(ctx context.Context, timeout time.Duration) (*http.Client, error) {
	return systemAdminAuth.newHTTPClient(ctx, timeout)
}

// SystemAdminGRPCCredentials returns per-RPC credentials that send the system
// admin's bearer on every gRPC call.
func SystemAdminGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	return systemAdminAuth.grpcCredentials(t)
}

// NewSystemAdminGRPCCredentials is SystemAdminGRPCCredentials returning its
// error, for callers without a *testing.T.
func NewSystemAdminGRPCCredentials(ctx context.Context) (credentials.PerRPCCredentials, error) {
	return systemAdminAuth.newGRPCCredentials(ctx)
}

// SystemAdminTransport wraps base so every request carries the system admin's
// bearer. The login happens on the first request, not here, so package-level
// clients can use it; a failed login fails that request rather than sending
// it anonymously.
func SystemAdminTransport(base http.RoundTripper) http.RoundTripper {
	return systemAdminAuth.lazyTransport(base)
}

// lazyTransport returns base wrapped with the principal's bearer, logging in
// against the first REST endpoint when a request first needs it.
func (r *principalRegistry) lazyTransport(base http.RoundTripper) http.RoundTripper {
	return &lazyBearerTransport{base: base, source: r.restBearer}
}

// SystemAdminBearer returns the system admin's bearer, for SDK clients built
// without a *testing.T (quicbaas.WithAuthToken).
func SystemAdminBearer(ctx context.Context) (string, error) {
	return systemAdminAuth.restBearer(ctx)
}

// restBearer returns the principal's bearer, logging in against the first
// REST endpoint when needed.
func (r *principalRegistry) restBearer(ctx context.Context) (string, error) {
	return r.bearer(ctx, GetRESTEndpoints()[0])
}

// lazyBearerTransport sets Authorization from source on every request.
type lazyBearerTransport struct {
	base   http.RoundTripper
	source func(context.Context) (string, error)
}

// RoundTrip implements http.RoundTripper.
func (l *lazyBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	bearer, err := l.source(req.Context())
	if err != nil {
		return nil, err
	}
	authed := req.Clone(req.Context())
	authed.Header.Set("Authorization", "Bearer "+bearer)
	return l.base.RoundTrip(authed)
}

// SystemAdminDefaultTransport wraps base so a request with no Authorization
// header carries the system admin's bearer, while a request that sets its own
// header (another principal's token, or a bad token on purpose) keeps it. It
// is for suites whose one shared client makes both kinds of request.
func SystemAdminDefaultTransport(t *testing.T, base http.RoundTripper) http.RoundTripper {
	t.Helper()
	transport, err := systemAdminAuth.newDefaultTransport(context.Background(), base)
	if err != nil {
		t.Fatalf("%s HTTP transport: %v", systemAdminPrincipalName, err)
	}
	return transport
}

// newDefaultTransport returns base wrapped so the principal's bearer is the
// default Authorization, never an override.
func (r *principalRegistry) newDefaultTransport(ctx context.Context, base http.RoundTripper) (http.RoundTripper, error) {
	source, err := r.newTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	authed, err := dataplane.NewBearerTransport(source, base)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: r.name, Step: "HTTP transport", Cause: err}
	}
	return &defaultBearerTransport{base: base, authed: authed}, nil
}

// defaultBearerTransport sends a request that already carries an
// Authorization header through base untouched, and any other through authed.
type defaultBearerTransport struct {
	base   http.RoundTripper
	authed http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (d *defaultBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("Authorization") != "" {
		return d.base.RoundTrip(req)
	}
	return d.authed.RoundTrip(req)
}

// enrollSystemAdmin loads the fixture system admin cert and key. Nothing is
// provisioned: first boot already bound the cert to the system admin.
func enrollSystemAdmin(_ context.Context, _ string) (*enrolledPrincipal, error) {
	certPEM, err := os.ReadFile(pivCertPath())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipalName, Step: "read client cert", Cause: err}
	}
	keyPEM, err := os.ReadFile(pivKeyPath())
	if err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipalName, Step: "read client key", Cause: err}
	}
	return &enrolledPrincipal{certPEM: certPEM, keyPEM: keyPEM}, nil
}
