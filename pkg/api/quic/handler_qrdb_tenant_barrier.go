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

	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-tenant barrier handlers (qrdb wire format, qrdb-quic ALPN)
//
// Each handler follows the same pattern as the system barrier handlers in
// handler_qrdb_credentials.go: unmarshal the canonical qrdbv1 protobuf
// request, delegate to the TenantBarrierServicer facade, and marshal the
// qrdbv1 response. The facade is the SAME instance the gRPC/REST/MCP transports
// use, so behaviour is identical across every transport.
//
// Method names match the qrdb TenantBarrierService RPC names so a qrdb SDK
// QUIC client reaching quicbaasd over the "qrdb-quic" ALPN is routed here
// without any SDK changes.
// =============================================================================

// QRDBTenantBarrierNotConfiguredError indicates a tenant.Barrier* RPC arrived
// on the qrdb-quic ALPN but no TenantBarrierServicer was wired into the QUIC
// server (SetTenantBarrierService not called or called with nil).
type QRDBTenantBarrierNotConfiguredError struct{}

// Error implements the error interface.
func (*QRDBTenantBarrierNotConfiguredError) Error() string {
	return "qrdb tenant barrier service not configured"
}

// qrdbTenantBarrierService returns the currently wired per-tenant barrier
// facade, or nil if SetTenantBarrierService has not been called with a non-nil
// value.
func (s *Server) qrdbTenantBarrierService() service.TenantBarrierServicer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.tenantBarrierSvc
}

// qrdbTenantBarrierInit handles the "tenant.BarrierInit" qrdb-quic method.
func (s *Server) qrdbTenantBarrierInit(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbTenantBarrierService()
	if svc == nil {
		return nil, &QRDBTenantBarrierNotConfiguredError{}
	}
	var req pb.TenantBarrierInitRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	result, err := svc.TenantBarrierInit(
		ctx,
		req.GetTenantId(),
		req.GetPassphrase(),
		int(req.GetThreshold()),
		int(req.GetShares()),
		req.GetStrategy(),
	)
	if err != nil {
		return nil, err
	}
	// Share-free contract: share bytes are never returned over the wire;
	// officers claim their shares individually via the share-claim flow.
	out := &pb.TenantBarrierInitResponse{}
	if result != nil {
		out.TenantId = result.TenantID
		out.Strategy = result.Strategy
		out.Threshold = int32(result.Threshold)     //nolint:gosec // small count
		out.TotalShares = int32(result.TotalShares) //nolint:gosec // small count
		out.ShareCount = int32(result.ShareCount)   //nolint:gosec // small count
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbTenantBarrierUnseal handles the "tenant.BarrierUnseal" qrdb-quic method.
func (s *Server) qrdbTenantBarrierUnseal(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbTenantBarrierService()
	if svc == nil {
		return nil, &QRDBTenantBarrierNotConfiguredError{}
	}
	var req pb.TenantBarrierUnsealRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.TenantBarrierUnseal(ctx, req.GetTenantId(), req.GetKey()); err != nil {
		return nil, err
	}
	return nil, nil
}

// qrdbTenantBarrierUnsealShare handles the "tenant.BarrierUnsealShare"
// qrdb-quic method.
func (s *Server) qrdbTenantBarrierUnsealShare(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbTenantBarrierService()
	if svc == nil {
		return nil, &QRDBTenantBarrierNotConfiguredError{}
	}
	var req pb.TenantBarrierUnsealShareRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	progress, err := svc.TenantBarrierUnsealShare(ctx, req.GetTenantId(), req.GetShare())
	if err != nil {
		return nil, err
	}
	out := &pb.TenantBarrierUnsealProgress{}
	if progress != nil {
		out.TenantId = progress.TenantID
		out.Required = int32(progress.Required)   //nolint:gosec // small count
		out.Submitted = int32(progress.Submitted) //nolint:gosec // small count
		out.Complete = progress.Complete
		out.Sealed = progress.Sealed
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbTenantBarrierSeal handles the "tenant.BarrierSeal" qrdb-quic method.
func (s *Server) qrdbTenantBarrierSeal(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbTenantBarrierService()
	if svc == nil {
		return nil, &QRDBTenantBarrierNotConfiguredError{}
	}
	var req pb.TenantBarrierSealRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if err := svc.TenantBarrierSeal(ctx, req.GetTenantId()); err != nil {
		return nil, err
	}
	return nil, nil
}

// qrdbTenantBarrierStatus handles the "tenant.BarrierStatus" qrdb-quic method.
func (s *Server) qrdbTenantBarrierStatus(ctx context.Context, payload []byte) ([]byte, error) {
	svc := s.qrdbTenantBarrierService()
	if svc == nil {
		return nil, &QRDBTenantBarrierNotConfiguredError{}
	}
	var req pb.TenantBarrierStatusRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	info, err := svc.TenantBarrierStatus(ctx, req.GetTenantId())
	if err != nil {
		return nil, err
	}
	out := &pb.TenantBarrierStatusResponse{}
	if info != nil {
		out.TenantId = info.TenantID
		out.Sealed = info.Sealed
		out.Initialized = info.Initialized
		out.Strategy = info.Strategy
		out.HardwareBacked = info.HardwareBacked
		out.ShamirThreshold = int32(info.ShamirThreshold) //nolint:gosec // small count
		out.ShamirTotal = int32(info.ShamirTotal)         //nolint:gosec // small count
	}
	resp, marshalErr := proto.Marshal(out)
	if marshalErr != nil {
		return nil, &QRDBMarshalError{Err: marshalErr}
	}
	return resp, nil
}

// qrdbTenantGovernance returns the currently wired barrierTenantGovernancer, or
// nil if SetTenantGovernance has not been called with a non-nil value.
func (s *Server) qrdbTenantGovernance() barrierTenantGovernancer {
	s.svcMu.RLock()
	defer s.svcMu.RUnlock()
	return s.tenantGovernance
}

// qrdbTenantBarrierEnableEncryption handles the
// "tenant.BarrierEnableEncryption" qrdb-quic method. It migrates a tenant's
// dedicated barrier from none to a keyed strategy by delegating to the system
// BarrierServicer.EnableEncryption with the tenant_id scope. The QUIC handler
// uses the same BarrierServicer that carries the EncryptionMigrator.
func (s *Server) qrdbTenantBarrierEnableEncryption(ctx context.Context, payload []byte) ([]byte, error) {
	// EnableEncryption lives on BarrierServicer (which has the node migrator),
	// not TenantBarrierServicer. Reuse qrdbBarrierService() which accesses
	// s.barrierService via the server's RLock.
	svc := s.qrdbBarrierService()
	if svc == nil {
		return nil, &QRDBBarrierNotConfiguredError{}
	}
	var req pb.BarrierEnableEncryptionRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if req.GetTenantId() == "" {
		return nil, errors.New("tenant_id is required for tenant barrier enable-encryption")
	}

	mech := req.GetMechanism()
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := req.GetAvailability()
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}
	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(req.GetStrategy()),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: int(req.GetShamirThreshold()),
		ShamirTotal:     int(req.GetShamirTotal()),
	}
	if p := req.GetPassphrase(); p != "" {
		opts.Credentials = qrdbsdk.Credentials{Secret: []byte(p)}
	}
	if err := svc.EnableEncryption(ctx, req.GetTenantId(), opts); err != nil {
		return nil, err
	}
	return nil, nil
}

// qrdbTenantUpdateQuotas handles the "tenant.UpdateQuotas" qrdb-quic method.
// It applies engine-level numeric resource limits to the identified tenant by
// delegating to the barrierTenantGovernancer (iam.DefaultTenantManager). The
// method maps every proto quota field to the canonical qrdbsdk.Quotas struct.
func (s *Server) qrdbTenantUpdateQuotas(ctx context.Context, payload []byte) ([]byte, error) {
	gov := s.qrdbTenantGovernance()
	if gov == nil {
		return nil, api.NewServiceUnavailableError("governance")
	}
	var req pb.TenantUpdateQuotasRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if req.GetId() == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}
	q := req.GetQuotas()
	if q == nil {
		return nil, api.NewRequiredFieldError("quotas")
	}
	quotas := qrdbsdk.Quotas{
		MaxDatabases:          q.GetMaxDatabases(),
		MaxRaftGroups:         q.GetMaxRaftGroups(),
		MaxReplicasPerGroup:   q.GetMaxReplicasPerGroup(),
		MaxTotalReplicas:      q.GetMaxTotalReplicas(),
		MaxCollections:        q.GetMaxCollections(),
		MaxKeys:               q.GetMaxKeys(),
		MaxStorageBytes:       q.GetMaxStorageBytes(),
		MaxDatabaseSizeBytes:  q.GetMaxDatabaseSizeBytes(),
		MaxUsers:              q.GetMaxUsers(),
		MaxRoles:              q.GetMaxRoles(),
		MaxCertificates:       q.GetMaxCertificates(),
		MaxBackups:            q.GetMaxBackups(),
		MaxVectorDims:         q.GetMaxVectorDims(),
		MaxTSMetrics:          q.GetMaxTsMetrics(),
		SystemBarrierQuota:    q.GetSystemBarrierQuota(),
		MaxDedicatedWALGroups: q.GetMaxDedicatedWalGroups(),
	}
	if err := gov.UpdateEngineQuotas(ctx, req.GetId(), quotas); err != nil {
		return nil, err
	}
	return nil, nil
}

// qrdbTenantUpdateEntitlements handles the "tenant.UpdateEntitlements" qrdb-quic
// method. It applies engine-level feature entitlements to the identified tenant
// by delegating to the barrierTenantGovernancer (iam.DefaultTenantManager).
func (s *Server) qrdbTenantUpdateEntitlements(ctx context.Context, payload []byte) ([]byte, error) {
	gov := s.qrdbTenantGovernance()
	if gov == nil {
		return nil, api.NewServiceUnavailableError("governance")
	}
	var req pb.TenantUpdateEntitlementsRequest
	if err := proto.Unmarshal(payload, &req); err != nil {
		return nil, &QRDBUnmarshalError{Err: err}
	}
	if req.GetId() == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}
	e := req.GetEntitlements()
	if e == nil {
		return nil, api.NewRequiredFieldError("entitlements")
	}
	entitlements := qrdbsdk.Entitlements{
		AllowedIsolationModes:     e.GetAllowedIsolationModes(),
		AllowDedicatedRaft:        e.GetAllowDedicatedRaft(),
		AllowVolumeStorage:        e.GetAllowVolumeStorage(),
		AllowSystemManagedBarrier: e.GetAllowSystemManagedBarrier(),
		AllowedBarrierModes:       e.GetAllowedBarrierModes(),
		AllowDedicatedWAL:         e.GetAllowDedicatedWal(),
	}
	if err := gov.UpdateEngineEntitlements(ctx, req.GetId(), entitlements); err != nil {
		return nil, err
	}
	return nil, nil
}
