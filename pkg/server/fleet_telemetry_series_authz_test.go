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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

package server

import (
	"context"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fleetDaemonPrincipal holds exactly a fleet node daemon's grant.
const fleetDaemonPrincipal = "quicbaas-fleet-daemon-test"

// newFleetDaemonContext returns a context carrying the identity of a
// principal granted exactly iam.FleetRuntimeGrant(): the identity a node's
// heartbeat reaches the server with.
func newFleetDaemonContext(t *testing.T) context.Context {
	t.Helper()
	registerFleetRuntimeGrant(t)
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "memory"}}
	daemon, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil,
		qrdbnode.WithServicePrincipal(fleetDaemonPrincipal))
	require.NoError(t, err)
	t.Cleanup(func() { _ = daemon.Close() })
	ctx, err := daemon.ServiceContext(context.Background())
	require.NoError(t, err)
	return ctx
}

// newPrincipalTSServer returns a Server over an authorization-enabled,
// pebble-backed node (only pebble carries the time-series engine) whose
// service principal is the server's own (qrdbservice).
func newPrincipalTSServer(t *testing.T) (*Server, *qrdbnode.Node) {
	t.Helper()
	principal, err := qrdbservice.NodeOption()
	require.NoError(t, err)
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "pebble"}}
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, principal)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	s := &Server{}
	s.setQRDBNode(node)
	return s, node
}

// TestFleetTelemetrySeries_FilesUnderTheServiceIdentityOnAnAuthNode proves a
// heartbeat's device samples are stored and read back on an
// authorization-enabled node although the reporting daemon's grant holds no
// time-series permission: the server files them as itself.
func TestFleetTelemetrySeries_FilesUnderTheServiceIdentityOnAnAuthNode(t *testing.T) {
	s, node := newPrincipalTSServer(t)
	daemonCtx := newFleetDaemonContext(t)
	storage := newNodeTimeSeriesStorage(node.Client())

	// The daemon's own identity is refused the write.
	require.Error(t, storage.WriteBatch(daemonCtx, []interfaces.DataPoint{
		{Metric: "acme/fleet.device.power_watts", Timestamp: time.Now(), Value: 1},
	}))

	series, err := newFleetTelemetrySeries(storage, time.Hour, s.ServiceContext)
	require.NoError(t, err)
	telemetry, err := orchestration.NewFleetTelemetryStore(series)
	require.NoError(t, err)

	at := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, telemetry.Record(daemonCtx, "acme", "agent-1", "host-1", at, []orchestration.DeviceSample{{
		Kind:        orchestration.DeviceGPU,
		ID:          "GPU-0",
		PowerWatts:  orchestration.Measured(310),
		Utilization: orchestration.Measured(0.5),
	}}))

	points, err := series.QueryPoints(daemonCtx, "acme", orchestration.MetricDevicePowerWatts,
		nil, at.Add(-time.Minute), at.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.InDelta(t, 310, points[0].Value, 1e-9)
	require.NoError(t, series.ApplyRetention(daemonCtx))
}

// TestFleetTelemetrySeries_TenantBoundHeartbeatIsFiledAndQueried pins 7156
// P1 4: the heartbeat's context carries the reporting tenant, bound as the
// fleet transports bind it (orchestration.ContextWithTenant). The server files
// the samples on its own behalf, so the identity tenant and the target tenant
// must agree; the tenant the samples belong to stays in their metric prefix,
// and acme's turn-energy query, made in acme, finds them.
// fleetTenantContext binds ctx to tenantID exactly as the gRPC fleet
// transport does (orchestratorTenant): the qrdb tenant through
// tenantctx.ContextForTarget, then the orchestration tenant.
// registerFleetRuntimeGrant registers the fleet runtime grant under
// fleetDaemonPrincipal's role.
func registerFleetRuntimeGrant(t *testing.T) {
	t.Helper()
	grant := make([]qrdbsdk.Permission, 0, len(iam.FleetRuntimeGrant()))
	for _, name := range iam.FleetRuntimeGrant() {
		p, err := qrdbsdk.ParsePermission(name)
		require.NoError(t, err)
		grant = append(grant, p)
	}
	qrdbsdk.RegisterPermissionRoleGrant(fleetDaemonPrincipal, grant...)
}

// fleetAgentContext is a tenant's fleet agent as a heartbeat handler binds
// it: the agent's own identity in its tenant, the target resolved through
// tenantctx.ContextForTarget, and the orchestration tenant set.
func fleetAgentContext(t *testing.T, tenantID string) context.Context {
	t.Helper()
	registerFleetRuntimeGrant(t)
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject:  "agent-1",
		TenantID: tenantID,
		Roles:    []string{fleetDaemonPrincipal},
	})
	tenantCtx, bound, err := tenantctx.ContextForTarget(ctx, "fleet.heartbeat", tenantID)
	require.NoError(t, err)
	require.Equal(t, tenantID, bound)
	return orchestration.ContextWithTenant(tenantCtx, bound)
}

func TestFleetTelemetrySeries_TenantBoundHeartbeatIsFiledAndQueried(t *testing.T) {
	s, node := newPrincipalTSServer(t)
	heartbeatCtx := fleetAgentContext(t, "acme")
	storage := newNodeTimeSeriesStorage(node.Client())

	series, err := newFleetTelemetrySeries(storage, time.Hour, s.ServiceContext)
	require.NoError(t, err)
	telemetry, err := orchestration.NewFleetTelemetryStore(series)
	require.NoError(t, err)

	at := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, telemetry.Record(heartbeatCtx, "acme", "agent-1", "host-1", at, []orchestration.DeviceSample{{
		Kind:        orchestration.DeviceGPU,
		ID:          "GPU-0",
		PowerWatts:  orchestration.Measured(275),
		Utilization: orchestration.Measured(0.25),
	}}))

	queryCtx := fleetAgentContext(t, "acme")
	points, err := series.QueryPoints(queryCtx, "acme", orchestration.MetricDevicePowerWatts,
		nil, at.Add(-time.Minute), at.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, points, 1)
	assert.InDelta(t, 275, points[0].Value, 1e-9)

	other, err := series.QueryPoints(fleetAgentContext(t, "globex"), "globex",
		orchestration.MetricDevicePowerWatts, nil, at.Add(-time.Minute), at.Add(time.Minute))
	require.NoError(t, err)
	assert.Empty(t, other, "acme's samples were filed where another tenant reads")
	require.NoError(t, series.ApplyRetention(heartbeatCtx))
}
