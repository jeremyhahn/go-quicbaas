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
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// shardProbeNode stands in for one cluster node: its PIV login answers
// loginStatus for the first refusals logins and then issues token; every
// other request answers probeStatus and records its Authorization header.
type shardProbeNode struct {
	server  *httptest.Server
	headers chan string
	logins  atomic.Int32
}

func newShardProbeNode(t *testing.T, token string, refusals int32, loginStatus, probeStatus int) *shardProbeNode {
	t.Helper()
	node := &shardProbeNode{headers: make(chan string, 1)}
	node.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != pivLoginPath {
			select {
			case node.headers <- r.Header.Get("Authorization"):
			default:
			}
			w.WriteHeader(probeStatus)
			if _, err := w.Write([]byte(`{"error":"probe"}`)); err != nil {
				t.Errorf("write probe response: %v", err)
			}
			return
		}
		body := `{"access_token":"` + token + `","token_type":"Bearer","expires_in":900}`
		if node.logins.Add(1) <= refusals {
			w.WriteHeader(loginStatus)
			body = `{"error":"refused"}`
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write login response: %v", err)
		}
	}))
	node.server.TLS = &tls.Config{ClientAuth: tls.RequestClientCert}
	node.server.StartTLS()
	t.Cleanup(node.server.Close)
	return node
}

// builderFor returns a builder whose one node is node, trusting its TLS
// certificate and, when admin is true, logging in as the system admin.
func builderFor(t *testing.T, node *shardProbeNode, admin bool) *ClusterBuilder {
	t.Helper()
	transport, ok := node.server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("httptest client transport is not *http.Transport")
	}
	cb := NewClusterBuilder().WithEndpoints([]string{node.server.URL}).WithRetries(0)
	cb.restEndpoints = []string{node.server.URL}
	cb.tlsConfig = func() (*tls.Config, error) { return transport.TLSClientConfig, nil }
	if admin {
		certPath, keyPath, caPath := pivLoginFiles(t, node.server)
		cb.login = pivLogin(certPath, keyPath, caPath)
	}
	return cb
}

// probeAuthorization builds cb's cluster, sends one Status probe and returns
// the Authorization header the node saw.
func probeAuthorization(t *testing.T, cb *ClusterBuilder, node *shardProbeNode) string {
	t.Helper()
	tc, err := cb.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		if err := tc.Close(); err != nil {
			t.Errorf("close cluster: %v", err)
		}
	})
	if _, err := tc.Clients[0].Cluster().Status(context.Background()); err == nil {
		t.Fatal("Status against the refusing probe node succeeded")
	}
	return <-node.headers
}

// require_authz makes Cluster().Status need admin:status, so the readiness
// probe must carry the bearer its own node issued to the system admin.
func TestClusterBuilder_WithSystemAdmin_ProbeCarriesNodeBearer(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if got := probeAuthorization(t, builderFor(t, node, true), node); got != "Bearer admin-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer admin-token")
	}
}

func TestClusterBuilder_DefaultProbeCarriesNoBearer(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if got := probeAuthorization(t, builderFor(t, node, false), node); got != "" {
		t.Errorf("Authorization = %q, want none from the default builder", got)
	}
}

func TestClusterBuilder_WithSystemAdmin_SetsLogin(t *testing.T) {
	if NewClusterBuilder().login != nil {
		t.Error("default builder logs in, want unauthenticated clients")
	}
	if NewClusterBuilder().WithSystemAdmin().login == nil {
		t.Error("WithSystemAdmin left the builder without a login")
	}
}

// A node still electing refuses the login; the builder retries it within its
// retries instead of failing on the first refusal.
func TestClusterBuilder_RetriesRefusedLogin(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 1, http.StatusServiceUnavailable, http.StatusForbidden)
	cb := builderFor(t, node, true).WithRetries(1)
	if got := probeAuthorization(t, cb, node); got != "Bearer admin-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer admin-token")
	}
	if got := node.logins.Load(); got != 2 {
		t.Errorf("logins = %d, want 2", got)
	}
}

func TestClusterBuilder_RefusedLoginFails(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 1, http.StatusUnauthorized, http.StatusOK)
	_, err := builderFor(t, node, true).Build(context.Background())
	var authErr *PrincipalAuthError
	if !errors.As(err, &authErr) || authErr.Principal != systemAdminPrincipal || authErr.Step != "login" {
		t.Fatalf("error = %v, want system admin login *PrincipalAuthError", err)
	}
	var badStatus *AuthBadStatusError
	if !errors.As(err, &badStatus) || badStatus.Status != http.StatusUnauthorized {
		t.Errorf("cause = %v, want *AuthBadStatusError with status 401", err)
	}
}

func TestClusterBuilder_WithPrincipal_ProbeCarriesPrincipalBearer(t *testing.T) {
	node := newShardProbeNode(t, "operator-token", 0, http.StatusOK, http.StatusForbidden)
	fake := &fakeEnrollment{expiresIn: 900}
	registry := newPrincipalRegistry("test", fake.enroll, func(context.Context, string, []byte, []byte) (string, int, error) {
		return "operator-token", 900, nil
	})
	cb := builderFor(t, node, false).withPrincipal(registry)
	if got := probeAuthorization(t, cb, node); got != "Bearer operator-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer operator-token")
	}
	if fake.enrolls != 1 {
		t.Errorf("enrolls = %d, want 1", fake.enrolls)
	}
}

func TestClusterBuilder_WithPrincipal_RefusedEnrollmentFails(t *testing.T) {
	node := newShardProbeNode(t, "operator-token", 0, http.StatusOK, http.StatusOK)
	fake := &fakeEnrollment{enrollFail: true}
	registry := newPrincipalRegistry("test", fake.enroll, func(context.Context, string, []byte, []byte) (string, int, error) {
		return "operator-token", 900, nil
	})
	_, err := builderFor(t, node, false).WithRetries(0).withPrincipal(registry).Build(context.Background())
	if !errors.Is(err, errEnrollRefused) {
		t.Fatalf("error = %v, want the refused enrollment", err)
	}
}

func TestClusterBuilder_WithTenantOperator_SetsLogin(t *testing.T) {
	if NewClusterBuilder().WithTenantOperator().login == nil {
		t.Fatal("WithTenantOperator left the builder unauthenticated")
	}
}

func TestClusterBuilder_SystemAdminWithoutRESTEndpointFails(t *testing.T) {
	cb := NewClusterBuilder().WithEndpoints([]string{"node1:50051"}).WithSystemAdmin()
	cb.restEndpoints = nil
	_, err := cb.Build(context.Background())
	var restErr *ClusterRESTEndpointError
	if !errors.As(err, &restErr) || restErr.Node != 1 || restErr.Endpoint != "node1:50051" {
		t.Fatalf("error = %v, want *ClusterRESTEndpointError for node 1", err)
	}
	if restErr.Error() == "" {
		t.Error("ClusterRESTEndpointError has an empty message")
	}
}

func TestWaitForClusterShardLeaders_AuthenticatedProbeSucceeds(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if err := waitForClusterShardLeaders(context.Background(), builderFor(t, node, true), nil, time.Second); err != nil {
		t.Fatalf("waitForClusterShardLeaders: %v", err)
	}
}

// A refused login is named in the timeout, not reported only as a missing
// leader.
func TestWaitForClusterShardLeaders_RefusedLoginSurfaces(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 1<<30, http.StatusUnauthorized, http.StatusOK)
	err := waitForClusterShardLeaders(context.Background(), builderFor(t, node, true), []uint64{1}, 300*time.Millisecond)
	var leadersErr *ShardLeadersError
	if !errors.As(err, &leadersErr) {
		t.Fatalf("error = %v, want *ShardLeadersError", err)
	}
	var badStatus *AuthBadStatusError
	if !errors.As(leadersErr.Err, &badStatus) || badStatus.Status != http.StatusUnauthorized {
		t.Errorf("ShardLeadersError.Err = %v, want the login's *AuthBadStatusError 401", leadersErr.Err)
	}
}

// twoNodeBuilderFor returns a system-admin builder for a stopped node followed
// by node, the way a failover target runs with node1 down.
func twoNodeBuilderFor(t *testing.T, node *shardProbeNode) *ClusterBuilder {
	t.Helper()
	stopped := httptest.NewTLSServer(http.NotFoundHandler())
	stopped.Close()
	cb := builderFor(t, node, true)
	cb.endpoints = []string{stopped.URL, node.server.URL}
	cb.restEndpoints = []string{stopped.URL, node.server.URL}
	return cb
}

// A stopped node is skipped: the node that answers carries the wait.
func TestWaitForClusterShardLeaders_StoppedNodeSkipped(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	if err := waitForClusterShardLeaders(context.Background(), twoNodeBuilderFor(t, node), nil, time.Second); err != nil {
		t.Fatalf("waitForClusterShardLeaders: %v", err)
	}
}

// With every node stopped no node answers, and the timeout carries the last
// failed build.
func TestWaitForClusterShardLeaders_AllNodesStoppedSurfaces(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusOK)
	cb := twoNodeBuilderFor(t, node)
	cb.endpoints = []string{cb.endpoints[0], cb.endpoints[0]}
	cb.restEndpoints = []string{cb.restEndpoints[0], cb.restEndpoints[0]}
	err := waitForClusterShardLeaders(context.Background(), cb, nil, 300*time.Millisecond)
	var leadersErr *ShardLeadersError
	if !errors.As(err, &leadersErr) {
		t.Fatalf("error = %v, want *ShardLeadersError", err)
	}
	var timeoutErr *PollTimeoutError
	if !errors.As(leadersErr.Err, &timeoutErr) || timeoutErr.LastErr == nil {
		t.Errorf("ShardLeadersError.Err = %v, want a *PollTimeoutError carrying the failed build", leadersErr.Err)
	}
}

// A refused Status probe is kept as the poll's last error.
func TestWaitForClusterShardLeaders_RefusedStatusSurfaces(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	err := waitForClusterShardLeaders(context.Background(), builderFor(t, node, true), []uint64{1}, 300*time.Millisecond)
	var leadersErr *ShardLeadersError
	if !errors.As(err, &leadersErr) {
		t.Fatalf("error = %v, want *ShardLeadersError", err)
	}
	var timeoutErr *PollTimeoutError
	if !errors.As(leadersErr.Err, &timeoutErr) || timeoutErr.LastErr == nil {
		t.Errorf("ShardLeadersError.Err = %v, want a *PollTimeoutError carrying the refused probe", leadersErr.Err)
	}
}

func TestStandaloneMode_ProbeCarriesSystemAdminBearer(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusOK)
	status := newLeaderlessStatusNode(t, node)
	cb := builderFor(t, node, true).WithEndpoints([]string{status.addr})
	standalone, err := standaloneMode(context.Background(), cb, []string{status.addr})
	if err != nil {
		t.Fatalf("standaloneMode: %v", err)
	}
	if !standalone {
		t.Error("standaloneMode = false, want true for a leaderless Status")
	}
	if got := <-status.headers; got != "Bearer admin-token" {
		t.Errorf("probe Authorization = %q, want the system admin's bearer", got)
	}
}

// leaderlessStatusNode is a gRPC node whose ClusterStatus reports a cluster
// with no leader. The probe reaches cluster status over gRPC, as the
// integration cluster's endpoints do, while the system admin logs in at the
// REST shardProbeNode.
type leaderlessStatusNode struct {
	pb.UnimplementedQuicBaaSServer
	addr    string
	headers chan string
}

// newLeaderlessStatusNode serves ClusterStatus over TLS with the certificate
// of rest, so the builder's TLS configuration trusts both nodes.
func newLeaderlessStatusNode(t *testing.T, rest *shardProbeNode) *leaderlessStatusNode {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	node := &leaderlessStatusNode{addr: listener.Addr().String(), headers: make(chan string, 1)}
	creds := credentials.NewTLS(&tls.Config{Certificates: rest.server.TLS.Certificates})
	server := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterQuicBaaSServer(server, node)
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		if err := <-served; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	})
	return node
}

// ClusterStatus records the caller's Authorization and answers with a cluster
// that has no leader.
func (n *leaderlessStatusNode) ClusterStatus(ctx context.Context, _ *pb.ClusterStatusRequest) (*pb.ClusterStatusResponse, error) {
	var authorization string
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if values := md.Get("authorization"); len(values) > 0 {
			authorization = values[0]
		}
	}
	select {
	case n.headers <- authorization:
	default:
	}
	return &pb.ClusterStatusResponse{ClusterId: "standalone"}, nil
}

func TestStandaloneMode_RefusedStatusFails(t *testing.T) {
	node := newShardProbeNode(t, "admin-token", 0, http.StatusOK, http.StatusForbidden)
	standalone, err := standaloneMode(context.Background(), builderFor(t, node, true), []string{node.server.URL})
	var probeErr *StandaloneProbeError
	if !errors.As(err, &probeErr) {
		t.Fatalf("error = %v, want *StandaloneProbeError", err)
	}
	if standalone {
		t.Error("standaloneMode = true on a refused probe, want false")
	}
}
