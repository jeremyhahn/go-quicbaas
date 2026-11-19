//go:build !integration && qrdb_ts

package server

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	"github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	"github.com/jeremyhahn/go-qrdb/pkg/database"
	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
	"github.com/jeremyhahn/go-qrdb/pkg/timeseries"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newScopedTimeSeriesTestServer starts a real standalone qrdb node with a
// memory "default" database and two prefix-isolated tenants, acme and globex,
// and returns a server holding it.
func newScopedTimeSeriesTestServer(t *testing.T) *Server {
	t.Helper()
	srv := &Server{}
	srv.setQRDBNode(startScopedTimeSeriesNode(t))
	return srv
}

// startScopedTimeSeriesNode starts a real standalone qrdb node with a pebble
// "default" database and two prefix-isolated tenants, acme and globex.
func startScopedTimeSeriesNode(t *testing.T) *qrdbnode.Node {
	t.Helper()
	ctx := context.Background()
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		// pebble, not memory: the memory engine serves no time series.
		{Name: "default", Engine: "mvcc", Backend: "pebble"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })

	for _, id := range []string{"acme", "globex"} {
		_, err := node.Services().Tenant.CreateTenant(ctx, id, id, tenant.IsolationPrefix)
		require.NoError(t, err, "create tenant %s", id)
	}
	return node
}

// TestQRDBTimeSeriesGRPC_HonoursDatabaseMetadata drives qrdb.v1 TS RPCs
// through the broker's own unary interceptor chain over a Unix socket and
// asserts the x-qrdb-database metadata selects the database: a policy set in
// alpha is visible in alpha and absent from beta.
func TestQRDBTimeSeriesGRPC_HonoursDatabaseMetadata(t *testing.T) {
	srv := withTempServer(t)
	node := startScopedTimeSeriesNode(t)
	srv.setQRDBNode(node)
	acmeCtx := tenant.WithTenantID(context.Background(), "acme")
	for _, name := range []string{"alpha", "beta"} {
		_, err := node.Services().Database.CreateForTenant(acmeCtx,
			database.DatabaseConfig{Name: name, Engine: database.EngineTimeSeries, Backend: database.BackendPebble})
		require.NoError(t, err, "create database %s", name)
	}

	unary, _, err := srv.grpcServerInterceptors()
	require.NoError(t, err)
	stampAcme := func(ctx context.Context, req any, _ *ggrpc.UnaryServerInfo, handler ggrpc.UnaryHandler) (any, error) {
		return handler(tenantctx.WithTenant(ctx, "acme"), req)
	}
	gs := ggrpc.NewServer(ggrpc.ChainUnaryInterceptor(append([]ggrpc.UnaryServerInterceptor{stampAcme}, unary...)...))
	srv.registerQRDBService(gs)

	dir, err := os.MkdirTemp("", "qbts")
	require.NoError(t, err)
	socket := filepath.Join(dir, "s.sock")
	lis, err := net.Listen("unix", socket)
	require.NoError(t, err)
	go func() { _ = gs.Serve(lis) }()

	conn, err := ggrpc.NewClient("unix://"+socket, ggrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = conn.Close()
		gs.Stop()
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove socket dir: %v", err)
		}
	})
	client := qrdbv1.NewQRDBServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	alpha := metadata.AppendToOutgoingContext(ctx, qrdbgrpc.MetadataDatabase, "alpha")
	beta := metadata.AppendToOutgoingContext(ctx, qrdbgrpc.MetadataDatabase, "beta")

	_, err = client.TSSetRetention(alpha, &qrdbv1.TSSetRetentionRequest{Metric: "m", DurationNs: int64(time.Hour)})
	require.NoError(t, err)

	got, err := client.TSGetRetention(alpha, &qrdbv1.TSGetRetentionRequest{Metric: "m"})
	require.NoError(t, err)
	require.Equal(t, int64(time.Hour), got.GetDurationNs(), "alpha must see its own policy")

	other, err := client.TSGetRetention(beta, &qrdbv1.TSGetRetentionRequest{Metric: "m"})
	require.NoError(t, err)
	require.Equal(t, int64(0), other.GetDurationNs(), "beta must not see alpha's policy")
}

// TestQRDBTimeSeriesProxy_ScopesPerTenant pins that the proxy behind the
// embedded transport and the qrdb.v1 surface resolves the time-series
// servicer per (tenant, database): one tenant's points and retention policy
// are invisible to another.
func TestQRDBTimeSeriesProxy_ScopesPerTenant(t *testing.T) {
	srv := newScopedTimeSeriesTestServer(t)
	proxy := newQRDBTimeSeriesServicerProxy(srv)

	acme := tenantctx.WithTenant(context.Background(), "acme")
	globex := tenantctx.WithTenant(context.Background(), "globex")

	now := time.Now()
	require.NoError(t, proxy.Write(acme, "m", 1, now, nil))
	require.NoError(t, proxy.SetRetentionPolicy(acme, "m", &timeseries.RetentionPolicy{Duration: time.Hour}))

	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	acmePoints, err := proxy.Query(acme, "m", start, end)
	require.NoError(t, err)
	require.Len(t, acmePoints, 1, "acme reads its own point")

	globexPoints, err := proxy.Query(globex, "m", start, end)
	require.NoError(t, err)
	require.Empty(t, globexPoints, "globex must not see acme's point")

	// The qrdb.v1 surface's registry resolves the same per-scope servicers:
	// acme's 1h policy on m is acme's alone.
	reg := newQRDBTimeSeriesRegistry(srv)
	acmeSvc, err := reg.TimeSeriesService("acme", tenant.DefaultDatabase)
	require.NoError(t, err)
	acmePolicy, err := acmeSvc.GetRetentionPolicy("m")
	require.NoError(t, err)
	require.NotNil(t, acmePolicy)
	require.Equal(t, time.Hour, acmePolicy.Duration, "acme keeps its policy")

	globexSvc, err := reg.TimeSeriesService("globex", tenant.DefaultDatabase)
	require.NoError(t, err)
	globexPolicy, err := globexSvc.GetRetentionPolicy("m")
	require.NoError(t, err)
	if globexPolicy != nil {
		require.NotEqual(t, time.Hour, globexPolicy.Duration, "globex must not see acme's policy")
	}
}

// TestQRDBTimeSeriesProxy_ContextlessMethodsFailClosed pins that the two
// methods with no context are refused rather than served unscoped.
func TestQRDBTimeSeriesProxy_ContextlessMethodsFailClosed(t *testing.T) {
	srv := newScopedTimeSeriesTestServer(t)
	proxy := newQRDBTimeSeriesServicerProxy(srv)

	_, err := proxy.GetRetentionPolicy("m")
	var unscoped *qrdbTimeSeriesUnscopedError
	require.ErrorAs(t, err, &unscoped)
	require.Equal(t, "GetRetentionPolicy", unscoped.Method)

	_, err = proxy.ListMetrics("", 10)
	require.ErrorAs(t, err, &unscoped)
	require.Equal(t, "ListMetrics", unscoped.Method)
}

// TestQRDBTimeSeriesProxy_NoNodeUnavailable pins the fail-closed answer while
// the server holds no node.
func TestQRDBTimeSeriesProxy_NoNodeUnavailable(t *testing.T) {
	proxy := newQRDBTimeSeriesServicerProxy(&Server{})
	err := proxy.Write(tenantctx.WithTenant(context.Background(), "acme"), "m", 1, time.Now(), nil)
	var unavailable *QRDBUserServiceUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, qrdbTimeSeriesSurface, unavailable.Surface)

	_, err = newQRDBTimeSeriesRegistry(&Server{}).TimeSeriesService("acme", tenant.DefaultDatabase)
	require.ErrorAs(t, err, &unavailable)
}
