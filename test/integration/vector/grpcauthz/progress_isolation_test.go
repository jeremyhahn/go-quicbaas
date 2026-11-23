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

package grpcauthz

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// progressProbe is one request tenant A sends for tenant B's progress.
type progressProbe struct {
	label       string
	namespace   string
	operationID string
}

// dialVectorAdmin returns a VectorAdmin client over the harness's connection
// to the daemon's Unix-socket gRPC listener.
func dialVectorAdmin(t *testing.T) pb.VectorAdminClient {
	t.Helper()
	require.NotNil(t, harness.conn, "harness connection")
	return pb.NewVectorAdminClient(harness.conn)
}

// startRebuild starts an asynchronous index rebuild of namespace as
// principal p and returns its operation ID.
func startRebuild(t *testing.T, client pb.VectorDBClient, p principal, namespace string) string {
	t.Helper()
	ctx, cancel := p.ctx(t)
	defer cancel()
	resp, err := client.RebuildIndex(ctx, &pb.RebuildIndexRequest{Namespace: namespace, Async: true})
	require.NoErrorf(t, err, "%s: rebuild index of %q", p.name, namespace)
	require.NotEmptyf(t, resp.GetOperationId(), "%s: rebuild of %q returned no operation ID", p.name, namespace)
	return resp.GetOperationId()
}

// startCompaction starts an asynchronous compaction of namespace as
// principal p and returns its operation ID.
func startCompaction(t *testing.T, admin pb.VectorAdminClient, p principal, namespace string) string {
	t.Helper()
	ctx, cancel := p.ctx(t)
	defer cancel()
	resp, err := admin.CompactNamespace(ctx, &pb.CompactNamespaceRequest{Namespace: namespace, Async: true})
	require.NoErrorf(t, err, "%s: compact %q", p.name, namespace)
	require.NotEmptyf(t, resp.GetOperationId(), "%s: compaction of %q returned no operation ID", p.name, namespace)
	return resp.GetOperationId()
}

// TestVectorGRPCAuthz_TenantCannotReadAnotherTenantsRebuildProgress proves a
// tenant holding the full vector grant cannot read another tenant's index
// rebuild progress by operation ID, by the namespace name both tenants hold,
// by a namespace only the other tenant holds, or by naming the other tenant's
// storage path; the owning tenant still reads its own.
func TestVectorGRPCAuthz_TenantCannotReadAnotherTenantsRebuildProgress(t *testing.T) {
	p := newTenantPair(t)

	bOnlyOp := startRebuild(t, p.client, p.b, p.bOnly)
	sharedOp := startRebuild(t, p.client, p.b, p.shared)

	probes := []progressProbe{
		{label: "other tenant operation ID", operationID: bOnlyOp},
		{label: "other tenant operation ID in shared namespace", operationID: sharedOp, namespace: p.shared},
		{label: "shared namespace name", namespace: p.shared},
		{label: "other tenant namespace", namespace: p.bOnly},
		{label: "other tenant raw prefix", namespace: "tenant/" + p.b.tenantID + "/" + p.bOnly},
	}
	for _, probe := range probes {
		t.Run(probe.label, func(t *testing.T) {
			ctx, cancel := p.a.ctx(t)
			defer cancel()
			resp, err := p.client.GetIndexProgress(ctx, &pb.GetIndexProgressRequest{
				Namespace:   probe.namespace,
				OperationId: probe.operationID,
			})
			requireCode(t, err, codes.NotFound, "GetIndexProgress %q/%q as tenant A", probe.namespace, probe.operationID)
			require.Nil(t, resp, "tenant A received tenant B's rebuild progress")
		})
	}

	own := []*pb.GetIndexProgressRequest{
		{OperationId: bOnlyOp},
		{Namespace: p.bOnly},
	}
	for _, req := range own {
		ctx, cancel := p.b.ctx(t)
		resp, err := p.client.GetIndexProgress(ctx, req)
		cancel()
		require.NoErrorf(t, err, "tenant B: GetIndexProgress %q/%q", req.GetNamespace(), req.GetOperationId())
		require.Equal(t, bOnlyOp, resp.GetOperationId(), "tenant B must read its own rebuild progress")
	}

	p.requireBIntact(t)
}

// TestVectorGRPCAuthz_TenantCannotReadAnotherTenantsCompactionProgress proves
// a tenant holding the full vector grant cannot read another tenant's
// compaction progress over the VectorAdmin service by operation ID, by the
// namespace name both tenants hold, by a namespace only the other tenant
// holds, or by naming the other tenant's storage path; the owning tenant
// still reads its own.
func TestVectorGRPCAuthz_TenantCannotReadAnotherTenantsCompactionProgress(t *testing.T) {
	p := newTenantPair(t)
	admin := dialVectorAdmin(t)

	bOnlyOp := startCompaction(t, admin, p.b, p.bOnly)
	sharedOp := startCompaction(t, admin, p.b, p.shared)

	probes := []progressProbe{
		{label: "other tenant operation ID in shared namespace", namespace: p.shared, operationID: sharedOp},
		{label: "other tenant operation ID", namespace: p.bOnly, operationID: bOnlyOp},
		{label: "shared namespace name", namespace: p.shared},
		{label: "other tenant namespace", namespace: p.bOnly},
		{label: "other tenant raw prefix", namespace: "tenant/" + p.b.tenantID + "/" + p.bOnly},
	}
	for _, probe := range probes {
		t.Run(probe.label, func(t *testing.T) {
			ctx, cancel := p.a.ctx(t)
			defer cancel()
			resp, err := admin.GetCompactionProgress(ctx, &pb.GetCompactionProgressRequest{
				Namespace:   probe.namespace,
				OperationId: probe.operationID,
			})
			requireCode(t, err, codes.NotFound, "GetCompactionProgress %q/%q as tenant A", probe.namespace, probe.operationID)
			require.Nil(t, resp, "tenant A received tenant B's compaction progress")
		})
	}

	own := []*pb.GetCompactionProgressRequest{
		{Namespace: p.bOnly, OperationId: bOnlyOp},
		{Namespace: p.bOnly},
	}
	for _, req := range own {
		ctx, cancel := p.b.ctx(t)
		resp, err := admin.GetCompactionProgress(ctx, req)
		cancel()
		require.NoErrorf(t, err, "tenant B: GetCompactionProgress %q/%q", req.GetNamespace(), req.GetOperationId())
		require.Equal(t, bOnlyOp, resp.GetOperationId(), "tenant B must read its own compaction progress")
	}

	p.requireBIntact(t)
}
