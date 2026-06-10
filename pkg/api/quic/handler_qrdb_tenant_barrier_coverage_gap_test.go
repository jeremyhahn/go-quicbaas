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
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-tenant barrier QUIC handlers (qrdb-quic ALPN).
//
// These handlers were uncovered: they delegate the canonical qrdbv1 protobuf
// request to the TenantBarrierServicer facade. The tests below exercise the
// not-configured fail-closed branch, the protobuf unmarshal error branch, the
// service-error propagation branch, and the happy-path marshal for each method.
// =============================================================================

// mockTenantBarrierServicer is a controllable double for the per-tenant barrier
// facade. Each field lets a test inject a result or an error per RPC.
type mockTenantBarrierServicer struct {
	initResp     *qrdbtransport.TenantBarrierInitResponse
	initErr      error
	unsealErr    error
	shareResp    *qrdbtransport.TenantBarrierUnsealProgress
	shareErr     error
	sealErr      error
	statusResp   *qrdbtransport.TenantBarrierStatusResponse
	statusErr    error
	gotTenantID  string
	gotShare     string
	gotKey       []byte
	gotThreshold int
	gotShares    int
	gotStrategy  string
}

var _ service.TenantBarrierServicer = (*mockTenantBarrierServicer)(nil)

func (m *mockTenantBarrierServicer) TenantBarrierInit(_ context.Context, tenantID, passphrase string, threshold, shares int, strategy string) (*qrdbtransport.TenantBarrierInitResponse, error) {
	m.gotTenantID = tenantID
	m.gotThreshold = threshold
	m.gotShares = shares
	m.gotStrategy = strategy
	return m.initResp, m.initErr
}

func (m *mockTenantBarrierServicer) TenantBarrierUnseal(_ context.Context, tenantID string, key []byte) error {
	m.gotTenantID = tenantID
	m.gotKey = key
	return m.unsealErr
}

func (m *mockTenantBarrierServicer) TenantBarrierUnsealShare(_ context.Context, tenantID, share string) (*qrdbtransport.TenantBarrierUnsealProgress, error) {
	m.gotTenantID = tenantID
	m.gotShare = share
	return m.shareResp, m.shareErr
}

func (m *mockTenantBarrierServicer) TenantBarrierSeal(_ context.Context, tenantID string) error {
	m.gotTenantID = tenantID
	return m.sealErr
}

func (m *mockTenantBarrierServicer) TenantBarrierStatus(_ context.Context, tenantID string) (*qrdbtransport.TenantBarrierStatusResponse, error) {
	m.gotTenantID = tenantID
	return m.statusResp, m.statusErr
}

// newTenantBarrierTestServer builds a QUIC server with the supplied tenant
// barrier facade installed (nil disables the surface).
func newTenantBarrierTestServer(t *testing.T, svc service.TenantBarrierServicer) *Server {
	t.Helper()
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	server, err := NewServer(&mockService{}, config, createServerTestLogger())
	require.NoError(t, err)
	if svc != nil {
		server.SetTenantBarrierService(svc)
	}
	return server
}

// TestQRDBTenantBarrierService_AccessorAndError verifies the accessor returns
// the installed facade and the not-configured error carries a stable message.
func TestQRDBTenantBarrierService_AccessorAndError(t *testing.T) {
	server := newTenantBarrierTestServer(t, nil)
	require.Nil(t, server.qrdbTenantBarrierService())

	svc := &mockTenantBarrierServicer{}
	server.SetTenantBarrierService(svc)
	require.Equal(t, svc, server.qrdbTenantBarrierService())

	require.Equal(t, "qrdb tenant barrier service not configured",
		(&QRDBTenantBarrierNotConfiguredError{}).Error())
}

// TestServer_SetValidatorEnforcement verifies the unified platform-token
// authorizer can be installed (and replaced) on the server, taking effect on the
// next request.
func TestServer_SetValidatorEnforcement(t *testing.T) {
	server := newTenantBarrierTestServer(t, nil)
	require.Nil(t, server.validatorAuthorizer)

	v := &fakeValidatorAuthorizer{}
	server.SetValidatorEnforcement(v)
	require.Equal(t, v, server.validatorAuthorizer)

	server.SetValidatorEnforcement(nil)
	require.Nil(t, server.validatorAuthorizer)
}

// TestQRDBTenantBarrierInit covers the not-configured, unmarshal-error,
// service-error, and happy-path branches of the init handler.
func TestQRDBTenantBarrierInit(t *testing.T) {
	ctx := context.Background()

	// Not configured: fail closed.
	unconfigured := newTenantBarrierTestServer(t, nil)
	_, err := unconfigured.qrdbTenantBarrierInit(ctx, nil)
	var notCfg *QRDBTenantBarrierNotConfiguredError
	require.ErrorAs(t, err, &notCfg)

	// Unmarshal error: invalid protobuf bytes.
	svc := &mockTenantBarrierServicer{}
	server := newTenantBarrierTestServer(t, svc)
	_, err = server.qrdbTenantBarrierInit(ctx, []byte{0xff, 0xff, 0xff})
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)

	// Service error propagates.
	svc.initErr = errors.New("init failed")
	_, err = server.qrdbTenantBarrierInit(ctx, mustMarshal(t, &pb.TenantBarrierInitRequest{
		TenantId: "t1", Threshold: 2, Shares: 3, Strategy: "shamir",
	}))
	require.ErrorContains(t, err, "init failed")

	// Happy path: response marshaled with the facade result.
	svc.initErr = nil
	svc.initResp = &qrdbtransport.TenantBarrierInitResponse{
		TenantID: "t1", Strategy: "shamir", Shares: []string{"s1", "s2", "s3"}, Threshold: 2, TotalShares: 3,
	}
	out, err := server.qrdbTenantBarrierInit(ctx, mustMarshal(t, &pb.TenantBarrierInitRequest{
		TenantId: "t1", Passphrase: "", Threshold: 2, Shares: 3, Strategy: "shamir",
	}))
	require.NoError(t, err)
	var resp pb.TenantBarrierInitResponse
	require.NoError(t, proto.Unmarshal(out, &resp))
	require.Equal(t, "t1", resp.GetTenantId())
	require.Equal(t, "shamir", resp.GetStrategy())
	require.Equal(t, int32(2), resp.GetThreshold())
	require.Equal(t, int32(3), resp.GetTotalShares())
	require.Equal(t, 2, svc.gotThreshold)
	require.Equal(t, "shamir", svc.gotStrategy)
}

// TestQRDBTenantBarrierUnseal covers not-configured, unmarshal-error,
// service-error, and happy-path branches of the unseal handler.
func TestQRDBTenantBarrierUnseal(t *testing.T) {
	ctx := context.Background()

	unconfigured := newTenantBarrierTestServer(t, nil)
	_, err := unconfigured.qrdbTenantBarrierUnseal(ctx, nil)
	var notCfg *QRDBTenantBarrierNotConfiguredError
	require.ErrorAs(t, err, &notCfg)

	svc := &mockTenantBarrierServicer{}
	server := newTenantBarrierTestServer(t, svc)
	_, err = server.qrdbTenantBarrierUnseal(ctx, []byte{0xff, 0xff, 0xff})
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)

	svc.unsealErr = errors.New("unseal failed")
	_, err = server.qrdbTenantBarrierUnseal(ctx, mustMarshal(t, &pb.TenantBarrierUnsealRequest{
		TenantId: "t1", Key: []byte("k"),
	}))
	require.ErrorContains(t, err, "unseal failed")

	svc.unsealErr = nil
	out, err := server.qrdbTenantBarrierUnseal(ctx, mustMarshal(t, &pb.TenantBarrierUnsealRequest{
		TenantId: "t1", Key: []byte("key-material"),
	}))
	require.NoError(t, err)
	require.Nil(t, out)
	require.Equal(t, "t1", svc.gotTenantID)
	require.Equal(t, []byte("key-material"), svc.gotKey)
}

// TestQRDBTenantBarrierUnsealShare covers not-configured, unmarshal-error,
// service-error, and happy-path branches of the unseal-share handler.
func TestQRDBTenantBarrierUnsealShare(t *testing.T) {
	ctx := context.Background()

	unconfigured := newTenantBarrierTestServer(t, nil)
	_, err := unconfigured.qrdbTenantBarrierUnsealShare(ctx, nil)
	var notCfg *QRDBTenantBarrierNotConfiguredError
	require.ErrorAs(t, err, &notCfg)

	svc := &mockTenantBarrierServicer{}
	server := newTenantBarrierTestServer(t, svc)
	_, err = server.qrdbTenantBarrierUnsealShare(ctx, []byte{0xff, 0xff, 0xff})
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)

	svc.shareErr = errors.New("share rejected")
	_, err = server.qrdbTenantBarrierUnsealShare(ctx, mustMarshal(t, &pb.TenantBarrierUnsealShareRequest{
		TenantId: "t1", Share: "share-1",
	}))
	require.ErrorContains(t, err, "share rejected")

	svc.shareErr = nil
	svc.shareResp = &qrdbtransport.TenantBarrierUnsealProgress{
		TenantID: "t1", Required: 3, Submitted: 1, Complete: false, Sealed: true,
	}
	out, err := server.qrdbTenantBarrierUnsealShare(ctx, mustMarshal(t, &pb.TenantBarrierUnsealShareRequest{
		TenantId: "t1", Share: "share-1",
	}))
	require.NoError(t, err)
	var progress pb.TenantBarrierUnsealProgress
	require.NoError(t, proto.Unmarshal(out, &progress))
	require.Equal(t, int32(3), progress.GetRequired())
	require.Equal(t, int32(1), progress.GetSubmitted())
	require.False(t, progress.GetComplete())
	require.True(t, progress.GetSealed())
	require.Equal(t, "share-1", svc.gotShare)
}

// TestQRDBTenantBarrierSeal covers not-configured, unmarshal-error,
// service-error, and happy-path branches of the seal handler.
func TestQRDBTenantBarrierSeal(t *testing.T) {
	ctx := context.Background()

	unconfigured := newTenantBarrierTestServer(t, nil)
	_, err := unconfigured.qrdbTenantBarrierSeal(ctx, nil)
	var notCfg *QRDBTenantBarrierNotConfiguredError
	require.ErrorAs(t, err, &notCfg)

	svc := &mockTenantBarrierServicer{}
	server := newTenantBarrierTestServer(t, svc)
	_, err = server.qrdbTenantBarrierSeal(ctx, []byte{0xff, 0xff, 0xff})
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)

	svc.sealErr = errors.New("seal failed")
	_, err = server.qrdbTenantBarrierSeal(ctx, mustMarshal(t, &pb.TenantBarrierSealRequest{TenantId: "t1"}))
	require.ErrorContains(t, err, "seal failed")

	svc.sealErr = nil
	out, err := server.qrdbTenantBarrierSeal(ctx, mustMarshal(t, &pb.TenantBarrierSealRequest{TenantId: "t1"}))
	require.NoError(t, err)
	require.Nil(t, out)
	require.Equal(t, "t1", svc.gotTenantID)
}

// TestQRDBTenantBarrierStatus covers not-configured, unmarshal-error,
// service-error, and happy-path branches of the status handler.
func TestQRDBTenantBarrierStatus(t *testing.T) {
	ctx := context.Background()

	unconfigured := newTenantBarrierTestServer(t, nil)
	_, err := unconfigured.qrdbTenantBarrierStatus(ctx, nil)
	var notCfg *QRDBTenantBarrierNotConfiguredError
	require.ErrorAs(t, err, &notCfg)

	svc := &mockTenantBarrierServicer{}
	server := newTenantBarrierTestServer(t, svc)
	_, err = server.qrdbTenantBarrierStatus(ctx, []byte{0xff, 0xff, 0xff})
	var unmarshalErr *QRDBUnmarshalError
	require.ErrorAs(t, err, &unmarshalErr)

	svc.statusErr = errors.New("status failed")
	_, err = server.qrdbTenantBarrierStatus(ctx, mustMarshal(t, &pb.TenantBarrierStatusRequest{TenantId: "t1"}))
	require.ErrorContains(t, err, "status failed")

	svc.statusErr = nil
	svc.statusResp = &qrdbtransport.TenantBarrierStatusResponse{
		TenantID: "t1", Sealed: false, Initialized: true, Strategy: "shamir",
		HardwareBacked: false, ShamirThreshold: 2, ShamirTotal: 3,
	}
	out, err := server.qrdbTenantBarrierStatus(ctx, mustMarshal(t, &pb.TenantBarrierStatusRequest{TenantId: "t1"}))
	require.NoError(t, err)
	var resp pb.TenantBarrierStatusResponse
	require.NoError(t, proto.Unmarshal(out, &resp))
	require.Equal(t, "t1", resp.GetTenantId())
	require.True(t, resp.GetInitialized())
	require.Equal(t, "shamir", resp.GetStrategy())
	require.Equal(t, int32(2), resp.GetShamirThreshold())
	require.Equal(t, int32(3), resp.GetShamirTotal())
}
