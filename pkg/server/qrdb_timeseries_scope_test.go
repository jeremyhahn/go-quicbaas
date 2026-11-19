//go:build !integration && qrdb_ts

package server

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-qrdb/pkg/tenant"
	"github.com/jeremyhahn/go-qrdb/pkg/timeseries"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// newScopedTimeSeriesTestServer starts a real standalone qrdb node with a
// memory "default" database and two prefix-isolated tenants, acme and globex,
// and returns a server holding it.
func newScopedTimeSeriesTestServer(t *testing.T) *Server {
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

	srv := &Server{}
	srv.setQRDBNode(node)
	return srv
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
