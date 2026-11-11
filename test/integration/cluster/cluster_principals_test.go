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

package cluster

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// systemAdminRPCs is the set of cluster RPCs whose permission go-qrdb
// registers for the system tenant only (add_member, remove_member,
// start_shard and the rebalance verbs). The system admin calls these; every
// other RPC of a cluster connection is called as the tenant operator, whose
// Operator role holds the entity verbs and admin:status.
var systemAdminRPCs = map[string]struct{}{
	pb.QuicBaaS_AddNode_FullMethodName:              {},
	pb.QuicBaaS_RemoveNode_FullMethodName:           {},
	pb.QuicBaaS_CreateShard_FullMethodName:          {},
	pb.QuicBaaS_RebalanceCluster_FullMethodName:     {},
	pb.QuicBaaS_GetRebalanceProgress_FullMethodName: {},
}

// systemAdminRoutes is systemAdminRPCs for REST: the method and the first
// four path segments of each route the system admin calls.
var systemAdminRoutes = map[string]struct{}{
	http.MethodPost + " /api/v1/cluster/nodes":     {},
	http.MethodDelete + " /api/v1/cluster/nodes":   {},
	http.MethodPost + " /api/v1/cluster/shards":    {},
	http.MethodPost + " /api/v1/cluster/rebalance": {},
}

// routeDepth is the number of path segments that name a cluster route.
const routeDepth = 4

// missingRPCInfoError reports a credentials lookup whose context names no RPC,
// so the principal cannot be chosen.
type missingRPCInfoError struct{}

func (*missingRPCInfoError) Error() string {
	return "cluster principals: the call context names no RPC method"
}

// clusterPrincipalCredentials sends the system admin's bearer on the RPCs of
// systemAdminRPCs and the tenant operator's bearer on every other RPC. An RPC
// this file does not list is therefore called with the lesser principal.
type clusterPrincipalCredentials struct {
	systemAdmin    credentials.PerRPCCredentials
	tenantOperator credentials.PerRPCCredentials
}

// GetRequestMetadata returns the authorization metadata of the principal that
// calls the RPC named in ctx.
func (c *clusterPrincipalCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	info, ok := credentials.RequestInfoFromContext(ctx)
	if !ok {
		return nil, &missingRPCInfoError{}
	}
	if _, admin := systemAdminRPCs[info.Method]; admin {
		return c.systemAdmin.GetRequestMetadata(ctx, uri...)
	}
	return c.tenantOperator.GetRequestMetadata(ctx, uri...)
}

// RequireTransportSecurity reports true: a bearer is never sent in clear.
func (c *clusterPrincipalCredentials) RequireTransportSecurity() bool {
	return true
}

// withTenantOperator is the dial option of a connection that only reads and
// writes entities and reads the cluster status: every RPC carries the tenant
// operator's bearer.
func withTenantOperator(t *testing.T) grpc.DialOption {
	t.Helper()
	return grpc.WithPerRPCCredentials(helpers.TenantOperatorGRPCCredentials(t))
}

// withClusterPrincipals is the dial option of a connection that also changes
// the cluster: the RPCs of systemAdminRPCs carry the system admin's bearer and
// every other RPC the tenant operator's.
func withClusterPrincipals(t *testing.T) grpc.DialOption {
	t.Helper()
	return grpc.WithPerRPCCredentials(&clusterPrincipalCredentials{
		systemAdmin:    helpers.SystemAdminGRPCCredentials(t),
		tenantOperator: helpers.TenantOperatorGRPCCredentials(t),
	})
}

// clusterPrincipalTransport forwards the routes of systemAdminRoutes with the
// system admin's bearer and every other request with the tenant operator's.
type clusterPrincipalTransport struct {
	systemAdmin    http.RoundTripper
	tenantOperator http.RoundTripper
}

// RoundTrip forwards req as the principal that calls its route.
func (c *clusterPrincipalTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, admin := systemAdminRoutes[routeKey(req)]; admin {
		return c.systemAdmin.RoundTrip(req)
	}
	return c.tenantOperator.RoundTrip(req)
}

// routeKey names req's route as systemAdminRoutes keys it.
func routeKey(req *http.Request) string {
	segments := strings.Split(strings.Trim(req.URL.Path, "/"), "/")
	if len(segments) > routeDepth {
		segments = segments[:routeDepth]
	}
	return req.Method + " /" + strings.Join(segments, "/")
}

// newTenantOperatorRESTClient returns an HTTP client whose every request
// carries the tenant operator's bearer. On the plain-HTTP cluster stack, which
// runs without IAM and so has no principal to enrol, it returns
// newInsecureHTTPClient's client as before.
func newTenantOperatorRESTClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	if _, err := helpers.IntegrationTLSConfig(); err != nil {
		return newInsecureHTTPClient(timeout)
	}
	return helpers.NewTenantOperatorHTTPClient(t, timeout)
}

// newClusterPrincipalsRESTClient returns an HTTP client that calls the routes
// of systemAdminRoutes as the system admin and every other route as the tenant
// operator, with newTenantOperatorRESTClient's plain-HTTP fallback.
func newClusterPrincipalsRESTClient(t *testing.T, timeout time.Duration) *http.Client {
	t.Helper()
	if _, err := helpers.IntegrationTLSConfig(); err != nil {
		return newInsecureHTTPClient(timeout)
	}
	client := helpers.NewTenantOperatorHTTPClient(t, timeout)
	client.Transport = &clusterPrincipalTransport{
		systemAdmin:    helpers.NewSystemAdminHTTPClient(t, timeout).Transport,
		tenantOperator: client.Transport,
	}
	return client
}
