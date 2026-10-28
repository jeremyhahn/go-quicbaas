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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	grpcclient "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// systemAdminPrincipal names the system admin in PrincipalAuthError.
const systemAdminPrincipal = "system-admin"

// systemAdminTokenSource logs the system admin in at baseURL with the given
// PIV files once, so a bad certificate fails the caller up front, and returns
// a source that re-reads the cached bearer, logging in again on expiry.
func systemAdminTokenSource(ctx context.Context, baseURL, certPath, keyPath, caPath string) (dataplane.TokenSource, error) {
	if _, _, err := authPIV(ctx, baseURL, certPath, keyPath, caPath); err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipal, Step: "login", Cause: err}
	}
	return func(ctx context.Context) (string, error) {
		bearer, _, err := authPIV(ctx, baseURL, certPath, keyPath, caPath)
		return bearer, err
	}, nil
}

// newSystemAdminHTTPClient wraps client's transport so every request carries
// the system admin's bearer from baseURL.
func newSystemAdminHTTPClient(ctx context.Context, client *http.Client, baseURL, certPath, keyPath, caPath string) (*http.Client, error) {
	source, err := systemAdminTokenSource(ctx, baseURL, certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}
	transport, err := dataplane.NewBearerTransport(source, client.Transport)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipal, Step: "HTTP client", Cause: err}
	}
	client.Transport = transport
	return client, nil
}

// newSystemAdminGRPCCredentials returns per-RPC credentials carrying the
// system admin's bearer from baseURL.
func newSystemAdminGRPCCredentials(ctx context.Context, baseURL, certPath, keyPath, caPath string) (credentials.PerRPCCredentials, error) {
	source, err := systemAdminTokenSource(ctx, baseURL, certPath, keyPath, caPath)
	if err != nil {
		return nil, err
	}
	creds, err := dataplane.NewBearerCredentials(source)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: systemAdminPrincipal, Step: "gRPC credentials", Cause: err}
	}
	return creds, nil
}

// NewSystemAdminHTTPClient returns an HTTP client that authenticates every
// request as the system admin, for suites that exercise admin-resource or
// session routes (cache, routing debug, metrics). It fails the test when the
// admin cannot log in.
func NewSystemAdminHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	client, err := newSystemAdminHTTPClient(context.Background(), NewHTTPClient(timeout),
		GetRESTEndpoints()[0], pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		t.Fatalf("system admin HTTP client: %v", err)
	}
	return client
}

// SystemAdminGRPCCredentials returns per-RPC credentials that authenticate
// every call as the system admin. It fails the test when the admin cannot
// log in.
func SystemAdminGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	creds, err := newSystemAdminGRPCCredentials(context.Background(), GetRESTEndpoints()[0],
		pivCertPath(), pivKeyPath(), pivCAFile())
	if err != nil {
		t.Fatalf("system admin gRPC credentials: %v", err)
	}
	return creds
}

// SystemAdminGRPCClientOption returns the grpcclient option that sends the
// system admin's bearer on every call. IntegrationTLSConfig carries no
// credential the authenticators accept, so a client of protected RPCs built
// from it alone is refused with "platform bearer authorization is missing".
func SystemAdminGRPCClientOption(t *testing.T) grpcclient.ClientOption {
	t.Helper()
	return grpcclient.WithDialOptions(grpc.WithPerRPCCredentials(SystemAdminGRPCCredentials(t)))
}
