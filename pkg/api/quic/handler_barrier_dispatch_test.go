//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock BarrierServicer
// =============================================================================

type mockBarrierServicer struct {
	err error // when non-nil every method returns it

	statusInfo   *service.BarrierStatusInfo
	shamirInfo   *service.ShamirInitInfo
	progress     *service.UnsealShareProgress
	recoveryInfo *service.RecoveryKeysInfo
	hasRecovery  bool
	rootToken    *service.RootTokenInfo
}

func (m *mockBarrierServicer) BarrierStatus(context.Context) (*service.BarrierStatusInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.statusInfo != nil {
		return m.statusInfo, nil
	}
	return &service.BarrierStatusInfo{Sealed: true, Strategy: "shamir", InitializedAt: time.Now()}, nil
}

func (m *mockBarrierServicer) BarrierInitialize(context.Context, string) error { return m.err }
func (m *mockBarrierServicer) BarrierUnseal(context.Context, string) error     { return m.err }
func (m *mockBarrierServicer) BarrierSeal(context.Context) error               { return m.err }

func (m *mockBarrierServicer) BarrierInitializeShamir(context.Context, string, int, int) (*service.ShamirInitInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.shamirInfo != nil {
		return m.shamirInfo, nil
	}
	return &service.ShamirInitInfo{Threshold: 2, TotalShares: 3, ShareCount: 3}, nil
}

func (m *mockBarrierServicer) BarrierUnsealWithShare(context.Context, string) (*service.UnsealShareProgress, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.progress != nil {
		return m.progress, nil
	}
	return &service.UnsealShareProgress{Required: 2, Submitted: 1, Complete: false}, nil
}

func (m *mockBarrierServicer) BarrierUnsealWithShares(context.Context, []string) error { return m.err }

func (m *mockBarrierServicer) BarrierRekey(context.Context, int, int) (*service.ShamirInitInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.ShamirInitInfo{Threshold: 2, TotalShares: 2, ShareCount: 2}, nil
}

func (m *mockBarrierServicer) BarrierGenerateRecoveryKeys(context.Context, int, int) (*service.RecoveryKeysInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.recoveryInfo != nil {
		return m.recoveryInfo, nil
	}
	return &service.RecoveryKeysInfo{Threshold: 2, Total: 2, ShareCount: 2}, nil
}

func (m *mockBarrierServicer) BarrierRecoverWithKeys(context.Context, []string) error { return m.err }
func (m *mockBarrierServicer) BarrierDeleteRecoveryKeys(context.Context) error        { return m.err }

func (m *mockBarrierServicer) BarrierHasRecoveryKeys(context.Context) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.hasRecovery, nil
}

func (m *mockBarrierServicer) BarrierGenerateRootToken(context.Context, []string) (*service.RootTokenInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.rootToken != nil {
		return m.rootToken, nil
	}
	return &service.RootTokenInfo{Token: "deadbeef", CreatedAt: time.Now()}, nil
}

func newBarrierHandler(t *testing.T, svc service.BarrierServicer) *Handler {
	t.Helper()
	return NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithBarrierService(svc))
}

func barrierRequest(subOp byte, body []byte) *Request {
	return &Request{Opcode: OpBarrier, Payload: append([]byte{subOp}, body...)}
}

// =============================================================================
// Happy paths
// =============================================================================

func TestBarrierDispatch_HappyPaths(t *testing.T) {
	handler := newBarrierHandler(t, &mockBarrierServicer{hasRecovery: true})
	ctx := context.Background()

	enc := func(v interface{}) []byte {
		b, err := EncodePayload(v)
		require.NoError(t, err)
		return b
	}

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"status", SubOpBarrierStatus, nil},
		{"initialize", SubOpBarrierInitialize, enc(&barrierInitializeWireRequest{Secret: "pw"})},
		{"unseal", SubOpBarrierUnseal, enc(&barrierUnsealWireRequest{Secret: "pw"})},
		{"seal", SubOpBarrierSeal, nil},
		{"shamir-init", SubOpBarrierShamirInitialize, enc(&barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 2, TotalShares: 3})},
		{"shamir-unseal-share", SubOpBarrierShamirUnsealShare, enc(&barrierShamirUnsealShareWireRequest{Share: "s1"})},
		{"shamir-unseal-shares", SubOpBarrierShamirUnsealShares, enc(&barrierShamirUnsealSharesWireRequest{Shares: []string{"s1", "s2"}})},
		{"rekey", SubOpBarrierRekey, enc(&barrierRekeyWireRequest{Threshold: 2, Total: 2})},
		{"recovery-generate", SubOpBarrierRecoveryGenerate, enc(&barrierRecoveryGenerateWireRequest{Threshold: 2, Total: 2})},
		{"recovery-recover", SubOpBarrierRecoveryRecover, enc(&barrierRecoveryRecoverWireRequest{Keys: []string{"r1", "r2"}})},
		{"recovery-delete", SubOpBarrierRecoveryDelete, nil},
		{"recovery-status", SubOpBarrierRecoveryStatus, nil},
		{"root-token", SubOpBarrierRootToken, enc(&barrierRootTokenWireRequest{Shares: []string{"s1", "s2"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, barrierRequest(tc.subOp, tc.body))
			assert.Equal(t, StatusOK, resp.Status, "expected OK, got status %d", resp.Status)
		})
	}
}

// =============================================================================
// Service-error paths
// =============================================================================

func TestBarrierDispatch_ServiceErrors(t *testing.T) {
	handler := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("barrier boom")})
	ctx := context.Background()

	enc := func(v interface{}) []byte {
		b, err := EncodePayload(v)
		require.NoError(t, err)
		return b
	}

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"status", SubOpBarrierStatus, nil},
		{"initialize", SubOpBarrierInitialize, enc(&barrierInitializeWireRequest{Secret: "pw"})},
		{"unseal", SubOpBarrierUnseal, enc(&barrierUnsealWireRequest{Secret: "pw"})},
		{"seal", SubOpBarrierSeal, nil},
		{"shamir-init", SubOpBarrierShamirInitialize, enc(&barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 2, TotalShares: 3})},
		{"shamir-unseal-share", SubOpBarrierShamirUnsealShare, enc(&barrierShamirUnsealShareWireRequest{Share: "s1"})},
		{"shamir-unseal-shares", SubOpBarrierShamirUnsealShares, enc(&barrierShamirUnsealSharesWireRequest{Shares: []string{"s1"}})},
		{"rekey", SubOpBarrierRekey, enc(&barrierRekeyWireRequest{Threshold: 2, Total: 2})},
		{"recovery-generate", SubOpBarrierRecoveryGenerate, enc(&barrierRecoveryGenerateWireRequest{Threshold: 2, Total: 2})},
		{"recovery-recover", SubOpBarrierRecoveryRecover, enc(&barrierRecoveryRecoverWireRequest{Keys: []string{"r1"}})},
		{"recovery-delete", SubOpBarrierRecoveryDelete, nil},
		{"recovery-status", SubOpBarrierRecoveryStatus, nil},
		{"root-token", SubOpBarrierRootToken, enc(&barrierRootTokenWireRequest{Shares: []string{"s1"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, barrierRequest(tc.subOp, tc.body))
			assert.NotEqual(t, StatusOK, resp.Status, "expected error status")
		})
	}
}

// =============================================================================
// Decode-error paths (malformed JSON body)
// =============================================================================

func TestBarrierDispatch_DecodeErrors(t *testing.T) {
	handler := newBarrierHandler(t, &mockBarrierServicer{})
	ctx := context.Background()
	bad := []byte("{not-json")

	subOps := []byte{
		SubOpBarrierInitialize,
		SubOpBarrierUnseal,
		SubOpBarrierShamirInitialize,
		SubOpBarrierShamirUnsealShare,
		SubOpBarrierShamirUnsealShares,
		SubOpBarrierRekey,
		SubOpBarrierRecoveryGenerate,
		SubOpBarrierRecoveryRecover,
		SubOpBarrierRootToken,
	}
	for _, subOp := range subOps {
		resp := handler.HandleRequest(ctx, barrierRequest(subOp, bad))
		assert.NotEqual(t, StatusOK, resp.Status, "subOp %d should reject malformed body", subOp)
	}
}

// =============================================================================
// Validation errors
// =============================================================================

func TestBarrierDispatch_ValidationErrors(t *testing.T) {
	handler := newBarrierHandler(t, &mockBarrierServicer{})
	ctx := context.Background()
	enc := func(v interface{}) []byte {
		b, err := EncodePayload(v)
		require.NoError(t, err)
		return b
	}

	// shamir initialize: threshold <= 0
	resp := handler.HandleRequest(ctx, barrierRequest(SubOpBarrierShamirInitialize,
		enc(&barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 0, TotalShares: 3})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// shamir initialize: total_shares <= 0
	resp = handler.HandleRequest(ctx, barrierRequest(SubOpBarrierShamirInitialize,
		enc(&barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 2, TotalShares: 0})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// shamir initialize: threshold > total_shares
	resp = handler.HandleRequest(ctx, barrierRequest(SubOpBarrierShamirInitialize,
		enc(&barrierShamirInitializeWireRequest{Secret: "pw", Threshold: 5, TotalShares: 3})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// shamir unseal share: empty share
	resp = handler.HandleRequest(ctx, barrierRequest(SubOpBarrierShamirUnsealShare,
		enc(&barrierShamirUnsealShareWireRequest{Share: ""})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// shamir unseal shares: empty list
	resp = handler.HandleRequest(ctx, barrierRequest(SubOpBarrierShamirUnsealShares,
		enc(&barrierShamirUnsealSharesWireRequest{Shares: nil})))
	assert.NotEqual(t, StatusOK, resp.Status)
}
