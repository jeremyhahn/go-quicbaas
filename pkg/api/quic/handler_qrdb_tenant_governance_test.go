//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	apierrors "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// =============================================================================
// Tenant governance QUIC handlers (qrdb-quic ALPN).
//
// These handlers were uncovered: they apply engine-level quotas and feature
// entitlements to an identified tenant by delegating to the wired
// barrierTenantGovernancer facade. The tests below exercise the not-configured
// fail-closed branch, the protobuf unmarshal error branch, the validation
// branches (empty id, nil quotas/entitlements), the service-error propagation
// branch, and the happy-path field-mapping assertions for each handler.
// =============================================================================

// fakeGovernancer is a controllable double for the barrierTenantGovernancer
// interface. It records the last call's arguments for assertion.
type fakeGovernancer struct {
	updateQuotasErr       error
	updateEntitlementsErr error
	gotID                 string
	gotQuotas             qrdbsdk.Quotas
	gotEntitlements       qrdbsdk.Entitlements
}

var _ barrierTenantGovernancer = (*fakeGovernancer)(nil)

// UpdateEngineQuotas records the id and quotas, then returns the configured error.
func (f *fakeGovernancer) UpdateEngineQuotas(_ context.Context, id string, quotas qrdbsdk.Quotas) error {
	f.gotID = id
	f.gotQuotas = quotas
	return f.updateQuotasErr
}

// UpdateEngineEntitlements records the id and entitlements, then returns the
// configured error.
func (f *fakeGovernancer) UpdateEngineEntitlements(_ context.Context, id string, entitlements qrdbsdk.Entitlements) error {
	f.gotID = id
	f.gotEntitlements = entitlements
	return f.updateEntitlementsErr
}

// newGovernanceTestServer builds a QUIC server with the supplied governance
// facade installed. Pass nil to exercise the fail-closed (not-configured) path.
func newGovernanceTestServer(t *testing.T, gov barrierTenantGovernancer) *Server {
	t.Helper()
	server := newTenantBarrierTestServer(t, nil)
	if gov != nil {
		server.SetTenantGovernance(gov)
	}
	return server
}

// =============================================================================
// qrdbTenantUpdateQuotas
// =============================================================================

// TestQRDBTenantUpdateQuotas_NotConfigured verifies the handler fails closed
// with a ServiceUnavailableError when no governance facade is wired.
func TestQRDBTenantUpdateQuotas_NotConfigured(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, nil)
	_, err := server.qrdbTenantUpdateQuotas(ctx, mustMarshal(t, &pb.TenantUpdateQuotasRequest{
		Id: "t1", Quotas: &pb.TenantQuotas{MaxDatabases: 10},
	}))
	require.Error(t, err)
	var svcErr *apierrors.ServiceUnavailableError
	require.ErrorAs(t, err, &svcErr, "expected ServiceUnavailableError for unconfigured governance")
	require.Equal(t, "governance", svcErr.Service)
}

// TestQRDBTenantUpdateQuotas_UnmarshalError verifies that malformed protobuf
// bytes yield a QRDBUnmarshalError before any delegate call is made.
func TestQRDBTenantUpdateQuotas_UnmarshalError(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, &fakeGovernancer{})
	_, err := server.qrdbTenantUpdateQuotas(ctx, []byte{0xff, 0xff, 0xff})
	require.Error(t, err)
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)
}

// TestQRDBTenantUpdateQuotas_MissingID verifies that an empty tenant id field
// yields a RequiredFieldError for "tenant_id".
func TestQRDBTenantUpdateQuotas_MissingID(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, &fakeGovernancer{})
	_, err := server.qrdbTenantUpdateQuotas(ctx, mustMarshal(t, &pb.TenantUpdateQuotasRequest{
		Id: "", Quotas: &pb.TenantQuotas{MaxDatabases: 5},
	}))
	require.Error(t, err)
	var rfErr *apierrors.RequiredFieldError
	require.ErrorAs(t, err, &rfErr)
	require.Equal(t, "tenant_id", rfErr.Field)
}

// TestQRDBTenantUpdateQuotas_NilQuotas verifies that a nil Quotas sub-message
// yields a RequiredFieldError for "quotas".
func TestQRDBTenantUpdateQuotas_NilQuotas(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, &fakeGovernancer{})
	_, err := server.qrdbTenantUpdateQuotas(ctx, mustMarshal(t, &pb.TenantUpdateQuotasRequest{
		Id: "t1", Quotas: nil,
	}))
	require.Error(t, err)
	var rfErr *apierrors.RequiredFieldError
	require.ErrorAs(t, err, &rfErr)
	require.Equal(t, "quotas", rfErr.Field)
}

// TestQRDBTenantUpdateQuotas_ServiceError verifies that a governance delegate
// error propagates back to the caller unchanged.
func TestQRDBTenantUpdateQuotas_ServiceError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("quota update failed")
	gov := &fakeGovernancer{updateQuotasErr: boom}
	server := newGovernanceTestServer(t, gov)
	_, err := server.qrdbTenantUpdateQuotas(ctx, mustMarshal(t, &pb.TenantUpdateQuotasRequest{
		Id: "t1", Quotas: &pb.TenantQuotas{MaxDatabases: 10},
	}))
	require.ErrorIs(t, err, boom)
}

// TestQRDBTenantUpdateQuotas_HappyPath verifies that all proto quota fields are
// mapped to the correct qrdbsdk.Quotas fields and forwarded to the governance
// facade with the tenant id.
func TestQRDBTenantUpdateQuotas_HappyPath(t *testing.T) {
	ctx := context.Background()
	gov := &fakeGovernancer{}
	server := newGovernanceTestServer(t, gov)

	_, err := server.qrdbTenantUpdateQuotas(ctx, mustMarshal(t, &pb.TenantUpdateQuotasRequest{
		Id: "tenant-x",
		Quotas: &pb.TenantQuotas{
			MaxDatabases:          1,
			MaxRaftGroups:         2,
			MaxReplicasPerGroup:   3,
			MaxTotalReplicas:      4,
			MaxCollections:        5,
			MaxKeys:               6,
			MaxStorageBytes:       7,
			MaxDatabaseSizeBytes:  8,
			MaxUsers:              9,
			MaxRoles:              10,
			MaxCertificates:       11,
			MaxBackups:            12,
			MaxVectorDims:         13,
			MaxTsMetrics:          14,
			SystemBarrierQuota:    15,
			MaxDedicatedWalGroups: 16,
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "tenant-x", gov.gotID)

	q := gov.gotQuotas
	require.Equal(t, int64(1), q.MaxDatabases)
	require.Equal(t, int64(2), q.MaxRaftGroups)
	require.Equal(t, int64(3), q.MaxReplicasPerGroup)
	require.Equal(t, int64(4), q.MaxTotalReplicas)
	require.Equal(t, int64(5), q.MaxCollections)
	require.Equal(t, int64(6), q.MaxKeys)
	require.Equal(t, int64(7), q.MaxStorageBytes)
	require.Equal(t, int64(8), q.MaxDatabaseSizeBytes)
	require.Equal(t, int64(9), q.MaxUsers)
	require.Equal(t, int64(10), q.MaxRoles)
	require.Equal(t, int64(11), q.MaxCertificates)
	require.Equal(t, int64(12), q.MaxBackups)
	require.Equal(t, int64(13), q.MaxVectorDims)
	require.Equal(t, int64(14), q.MaxTSMetrics)
	require.Equal(t, int64(15), q.SystemBarrierQuota)
	require.Equal(t, int64(16), q.MaxDedicatedWALGroups)
}

// =============================================================================
// qrdbTenantUpdateEntitlements
// =============================================================================

// TestQRDBTenantUpdateEntitlements_NotConfigured verifies the handler fails
// closed with a ServiceUnavailableError when no governance facade is wired.
func TestQRDBTenantUpdateEntitlements_NotConfigured(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, nil)
	_, err := server.qrdbTenantUpdateEntitlements(ctx, mustMarshal(t, &pb.TenantUpdateEntitlementsRequest{
		Id: "t1", Entitlements: &pb.TenantEntitlements{AllowDedicatedRaft: true},
	}))
	require.Error(t, err)
	var svcErr *apierrors.ServiceUnavailableError
	require.ErrorAs(t, err, &svcErr, "expected ServiceUnavailableError for unconfigured governance")
	require.Equal(t, "governance", svcErr.Service)
}

// TestQRDBTenantUpdateEntitlements_UnmarshalError verifies that malformed
// protobuf bytes yield a QRDBUnmarshalError.
func TestQRDBTenantUpdateEntitlements_UnmarshalError(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, &fakeGovernancer{})
	_, err := server.qrdbTenantUpdateEntitlements(ctx, []byte{0xff, 0xff, 0xff})
	require.Error(t, err)
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)
}

// TestQRDBTenantUpdateEntitlements_MissingID verifies that an empty tenant id
// field yields a RequiredFieldError for "tenant_id".
func TestQRDBTenantUpdateEntitlements_MissingID(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, &fakeGovernancer{})
	_, err := server.qrdbTenantUpdateEntitlements(ctx, mustMarshal(t, &pb.TenantUpdateEntitlementsRequest{
		Id: "", Entitlements: &pb.TenantEntitlements{AllowDedicatedRaft: true},
	}))
	require.Error(t, err)
	var rfErr *apierrors.RequiredFieldError
	require.ErrorAs(t, err, &rfErr)
	require.Equal(t, "tenant_id", rfErr.Field)
}

// TestQRDBTenantUpdateEntitlements_NilEntitlements verifies that a nil
// Entitlements sub-message yields a RequiredFieldError for "entitlements".
func TestQRDBTenantUpdateEntitlements_NilEntitlements(t *testing.T) {
	ctx := context.Background()
	server := newGovernanceTestServer(t, &fakeGovernancer{})
	_, err := server.qrdbTenantUpdateEntitlements(ctx, mustMarshal(t, &pb.TenantUpdateEntitlementsRequest{
		Id: "t1", Entitlements: nil,
	}))
	require.Error(t, err)
	var rfErr *apierrors.RequiredFieldError
	require.ErrorAs(t, err, &rfErr)
	require.Equal(t, "entitlements", rfErr.Field)
}

// TestQRDBTenantUpdateEntitlements_ServiceError verifies that a governance
// delegate error propagates back to the caller unchanged.
func TestQRDBTenantUpdateEntitlements_ServiceError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("entitlement update failed")
	gov := &fakeGovernancer{updateEntitlementsErr: boom}
	server := newGovernanceTestServer(t, gov)
	_, err := server.qrdbTenantUpdateEntitlements(ctx, mustMarshal(t, &pb.TenantUpdateEntitlementsRequest{
		Id: "t1", Entitlements: &pb.TenantEntitlements{AllowDedicatedRaft: true},
	}))
	require.ErrorIs(t, err, boom)
}

// TestQRDBTenantUpdateEntitlements_HappyPath verifies that all proto entitlement
// fields are mapped to the correct qrdbsdk.Entitlements fields and forwarded to
// the governance facade with the tenant id.
func TestQRDBTenantUpdateEntitlements_HappyPath(t *testing.T) {
	ctx := context.Background()
	gov := &fakeGovernancer{}
	server := newGovernanceTestServer(t, gov)

	_, err := server.qrdbTenantUpdateEntitlements(ctx, mustMarshal(t, &pb.TenantUpdateEntitlementsRequest{
		Id: "tenant-y",
		Entitlements: &pb.TenantEntitlements{
			AllowedIsolationModes:     []string{"shared", "dedicated"},
			AllowDedicatedRaft:        true,
			AllowVolumeStorage:        true,
			AllowSystemManagedBarrier: true,
			AllowedBarrierModes:       []string{"passphrase", "shamir"},
			AllowDedicatedWal:         true,
		},
	}))
	require.NoError(t, err)
	require.Equal(t, "tenant-y", gov.gotID)

	e := gov.gotEntitlements
	require.Equal(t, []string{"shared", "dedicated"}, e.AllowedIsolationModes)
	require.True(t, e.AllowDedicatedRaft)
	require.True(t, e.AllowVolumeStorage)
	require.True(t, e.AllowSystemManagedBarrier)
	require.Equal(t, []string{"passphrase", "shamir"}, e.AllowedBarrierModes)
	require.True(t, e.AllowDedicatedWAL)
}
