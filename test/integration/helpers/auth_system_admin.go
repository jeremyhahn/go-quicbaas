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
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// systemAdminBearer is one system-admin bearer and the instant it stops
// being presented.
type systemAdminBearer struct {
	token  string
	expiry time.Time
}

// systemAdminBearerCache memoises the fixture system admin's bearer per REST
// base URL, so a client that sends it on every call logs in once per bearer
// lifetime rather than once per call.
type systemAdminBearerCache struct {
	mu      sync.Mutex
	bearers map[string]systemAdminBearer
}

// systemAdminBearers holds the system-admin bearers of this suite process.
var systemAdminBearers = &systemAdminBearerCache{bearers: map[string]systemAdminBearer{}}

// bearer returns the cached bearer for baseURL, logging in with the fixture
// system-admin certificate when there is none or it has expired.
func (c *systemAdminBearerCache) bearer(ctx context.Context, baseURL string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.bearers[baseURL]; ok && time.Now().Before(cached.expiry) {
		return cached.token, nil
	}
	token, expiry, err := AuthPIVSession(ctx, baseURL, "", "", "")
	if err != nil {
		return "", err
	}
	c.bearers[baseURL] = systemAdminBearer{token: token, expiry: expiry}
	return token, nil
}

// systemAdminTokenSource logs in up front, so a login failure surfaces with
// its cause rather than later as a refused call, and then refreshes the
// bearer on demand.
func systemAdminTokenSource(ctx context.Context) (dataplane.TokenSource, error) {
	baseURL := GetRESTEndpoints()[0]
	if _, err := systemAdminBearers.bearer(ctx, baseURL); err != nil {
		return nil, err
	}
	return func(ctx context.Context) (string, error) {
		return systemAdminBearers.bearer(ctx, baseURL)
	}, nil
}

// SystemAdminGRPCCredentials returns per-RPC credentials that send the
// fixture system admin's bearer of the first REST endpoint's cluster on every
// gRPC call. Only a suite whose calls need the cluster-wide admin grants
// (cluster membership, status, rebalancing) acts as this principal; a suite
// that reads or writes tenant data uses the tenant operator instead.
func SystemAdminGRPCCredentials(t *testing.T) credentials.PerRPCCredentials {
	t.Helper()
	source, err := systemAdminTokenSource(context.Background())
	if err != nil {
		t.Fatalf("system admin token source: %v", err)
	}
	creds, err := dataplane.NewBearerCredentials(source)
	if err != nil {
		t.Fatalf("system admin gRPC credentials: %v", err)
	}
	return creds
}

// NewSystemAdminHTTPClient returns NewHTTPClient's client with every request
// carrying the fixture system admin's bearer of the first REST endpoint's
// cluster. The same restriction as SystemAdminGRPCCredentials applies.
func NewSystemAdminHTTPClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	source, err := systemAdminTokenSource(context.Background())
	if err != nil {
		t.Fatalf("system admin token source: %v", err)
	}
	client := NewHTTPClient(timeout)
	transport, err := dataplane.NewBearerTransport(source, client.Transport)
	if err != nil {
		t.Fatalf("system admin HTTP client: %v", err)
	}
	client.Transport = transport
	return client
}
