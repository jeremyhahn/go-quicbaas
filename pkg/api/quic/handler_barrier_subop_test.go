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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-sub-op handler tests — handler_barrier.go
//
// Each of the 13 SubOpBarrier* sub-handlers is tested for:
//   (1) success path: correct JSON wire response shape
//   (2) service-error path: error status returned
//   (3) nil barrierService: StatusUnavailable
//   (4) malformed JSON body: error status (for decode-body handlers)
//
// The mockBarrierServicer defined in handler_barrier_dispatch_test.go is
// reused here; barrierRequest/newBarrierHandler helpers are also reused.
// =============================================================================

// decodeJSON is a helper that unmarshals resp.Payload into v and fails the
// test if resp.Status != StatusOK or unmarshalling fails.
func decodeBarrierJSON(t *testing.T, resp *Response, v interface{}) {
	t.Helper()
	require.Equal(t, StatusOK, resp.Status, "expected StatusOK, got status 0x%02X payload=%q", resp.Status, resp.Payload)
	require.NoError(t, json.Unmarshal(resp.Payload, v))
}

// nilBarrierHandler builds a Handler with no barrier service installed.
func nilBarrierHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
}

// badJSON is a payload that will fail JSON decode.
var badJSON = []byte("{not:json")

// =============================================================================
// SubOpBarrierStatus
// =============================================================================

func TestBarrierSubOp_Status_Success(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := newBarrierHandler(t, &mockBarrierServicer{
		statusInfo: &service.BarrierStatusInfo{
			Sealed:         false,
			Strategy:       "shamir",
			HardwareBacked: true,
			InitializedAt:  ts,
		},
	})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierStatus, nil))

	var got barrierStatusWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.False(t, got.Sealed)
	assert.Equal(t, "shamir", got.Strategy)
	assert.True(t, got.HardwareBacked)
	assert.Equal(t, ts.UTC().Format("2006-01-02T15:04:05Z07:00"), got.InitializedAt)
}

func TestBarrierSubOp_Status_ServiceError(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("status boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierStatus, nil))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_Status_NilService(t *testing.T) {
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierStatus, nil))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

// =============================================================================
// SubOpBarrierInitialize
// =============================================================================

func TestBarrierSubOp_Initialize_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierInitializeWireRequest{Secret: "s3cr3t"})
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierInitialize, enc))
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Payload, "initialize returns no body on success")
}

func TestBarrierSubOp_Initialize_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierInitializeWireRequest{Secret: "x"})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("init boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierInitialize, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_Initialize_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierInitializeWireRequest{Secret: "x"})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierInitialize, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_Initialize_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierInitialize, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierUnseal
// =============================================================================

func TestBarrierSubOp_Unseal_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierUnsealWireRequest{Secret: "pass"})
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierUnseal, enc))
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Payload)
}

func TestBarrierSubOp_Unseal_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierUnsealWireRequest{Secret: "x"})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("unseal boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierUnseal, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_Unseal_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierUnsealWireRequest{Secret: "x"})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierUnseal, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_Unseal_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierUnseal, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierSeal
// =============================================================================

func TestBarrierSubOp_Seal_Success(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierSeal, nil))
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Payload)
}

func TestBarrierSubOp_Seal_ServiceError(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("seal boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierSeal, nil))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_Seal_NilService(t *testing.T) {
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierSeal, nil))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

// =============================================================================
// SubOpBarrierShamirInitialize
// =============================================================================

func TestBarrierSubOp_ShamirInitialize_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirInitializeWireRequest{
		Secret: "s", Threshold: 3, TotalShares: 5,
	})
	h := newBarrierHandler(t, &mockBarrierServicer{
		shamirInfo: &service.ShamirInitInfo{Threshold: 3, TotalShares: 5, ShareCount: 5},
	})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirInitialize, enc))

	var got shamirInitWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.Equal(t, 3, got.Threshold)
	assert.Equal(t, 5, got.TotalShares)
	assert.Equal(t, 5, got.ShareCount)
	// Share-free contract: no share material in the response struct.
}

func TestBarrierSubOp_ShamirInitialize_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirInitializeWireRequest{Secret: "s", Threshold: 2, TotalShares: 3})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("shamir-init boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirInitialize, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_ShamirInitialize_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirInitializeWireRequest{Secret: "s", Threshold: 2, TotalShares: 3})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirInitialize, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_ShamirInitialize_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirInitialize, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierShamirUnsealShare
// =============================================================================

func TestBarrierSubOp_ShamirUnsealShare_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirUnsealShareWireRequest{Share: "abc"})
	h := newBarrierHandler(t, &mockBarrierServicer{
		progress: &service.UnsealShareProgress{Required: 3, Submitted: 1, Complete: false},
	})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShare, enc))

	var got unsealShareProgressWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.Equal(t, 3, got.Required)
	assert.Equal(t, 1, got.Submitted)
	assert.False(t, got.Complete)
}

func TestBarrierSubOp_ShamirUnsealShare_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirUnsealShareWireRequest{Share: "abc"})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("share boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShare, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_ShamirUnsealShare_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirUnsealShareWireRequest{Share: "abc"})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShare, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_ShamirUnsealShare_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShare, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierShamirUnsealShares
// =============================================================================

func TestBarrierSubOp_ShamirUnsealShares_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirUnsealSharesWireRequest{Shares: []string{"a", "b", "c"}})
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShares, enc))
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Payload)
}

func TestBarrierSubOp_ShamirUnsealShares_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirUnsealSharesWireRequest{Shares: []string{"a"}})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("shares boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShares, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_ShamirUnsealShares_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierShamirUnsealSharesWireRequest{Shares: []string{"a"}})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShares, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_ShamirUnsealShares_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierShamirUnsealShares, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierRekey
// =============================================================================

func TestBarrierSubOp_Rekey_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRekeyWireRequest{Threshold: 2, Total: 4})
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRekey, enc))

	var got shamirInitWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.Equal(t, 2, got.Threshold)
	assert.Equal(t, 2, got.TotalShares) // mock returns fixed values
	assert.Equal(t, 2, got.ShareCount)
}

func TestBarrierSubOp_Rekey_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRekeyWireRequest{Threshold: 2, Total: 3})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("rekey boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRekey, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_Rekey_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRekeyWireRequest{Threshold: 2, Total: 3})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRekey, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_Rekey_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRekey, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierRecoveryGenerate
// =============================================================================

func TestBarrierSubOp_RecoveryGenerate_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRecoveryGenerateWireRequest{Threshold: 2, Total: 3})
	h := newBarrierHandler(t, &mockBarrierServicer{
		recoveryInfo: &service.RecoveryKeysInfo{Threshold: 2, Total: 3, ShareCount: 3},
	})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryGenerate, enc))

	var got recoveryKeysWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.Equal(t, 2, got.Threshold)
	assert.Equal(t, 3, got.Total)
	assert.Equal(t, 3, got.ShareCount)
	// Share-free contract: no key material in the response struct.
}

func TestBarrierSubOp_RecoveryGenerate_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRecoveryGenerateWireRequest{Threshold: 2, Total: 3})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("recovery-gen boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryGenerate, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_RecoveryGenerate_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRecoveryGenerateWireRequest{Threshold: 2, Total: 3})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryGenerate, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_RecoveryGenerate_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryGenerate, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierRecoveryRecover
// =============================================================================

func TestBarrierSubOp_RecoveryRecover_Success(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRecoveryRecoverWireRequest{Keys: []string{"k1", "k2"}})
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryRecover, enc))
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Payload)
}

func TestBarrierSubOp_RecoveryRecover_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRecoveryRecoverWireRequest{Keys: []string{"k1"}})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("recovery-recover boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryRecover, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_RecoveryRecover_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRecoveryRecoverWireRequest{Keys: []string{"k1"}})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryRecover, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_RecoveryRecover_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryRecover, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// SubOpBarrierRecoveryDelete
// =============================================================================

func TestBarrierSubOp_RecoveryDelete_Success(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryDelete, nil))
	assert.Equal(t, StatusOK, resp.Status)
	assert.Empty(t, resp.Payload)
}

func TestBarrierSubOp_RecoveryDelete_ServiceError(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("recovery-delete boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryDelete, nil))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_RecoveryDelete_NilService(t *testing.T) {
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryDelete, nil))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

// =============================================================================
// SubOpBarrierRecoveryStatus
// =============================================================================

func TestBarrierSubOp_RecoveryStatus_Success(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{hasRecovery: true})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryStatus, nil))

	var got recoveryStatusWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.True(t, got.HasRecoveryKeys)
}

func TestBarrierSubOp_RecoveryStatus_ServiceError(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("recovery-status boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryStatus, nil))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_RecoveryStatus_NilService(t *testing.T) {
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRecoveryStatus, nil))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

// =============================================================================
// SubOpBarrierRootToken
// =============================================================================

func TestBarrierSubOp_RootToken_Success(t *testing.T) {
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	enc := mustEncodePayload(t, &barrierRootTokenWireRequest{Shares: []string{"s1", "s2"}})
	h := newBarrierHandler(t, &mockBarrierServicer{
		rootToken: &service.RootTokenInfo{Token: "cafebabe", CreatedAt: ts},
	})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRootToken, enc))

	var got rootTokenWireResponse
	decodeBarrierJSON(t, resp, &got)
	assert.Equal(t, "cafebabe", got.Token)
	assert.Equal(t, ts.UTC().Format("2006-01-02T15:04:05Z07:00"), got.CreatedAt)
}

func TestBarrierSubOp_RootToken_ServiceError(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRootTokenWireRequest{Shares: []string{"s1"}})
	h := newBarrierHandler(t, &mockBarrierServicer{err: errors.New("root-token boom")})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRootToken, enc))
	assert.NotEqual(t, StatusOK, resp.Status)
}

func TestBarrierSubOp_RootToken_NilService(t *testing.T) {
	enc := mustEncodePayload(t, &barrierRootTokenWireRequest{Shares: []string{"s1"}})
	resp := nilBarrierHandler(t).handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRootToken, enc))
	assert.Equal(t, StatusUnavailable, resp.Status)
}

func TestBarrierSubOp_RootToken_BadJSON(t *testing.T) {
	h := newBarrierHandler(t, &mockBarrierServicer{})
	resp := h.handleBarrierDispatch(context.Background(), barrierRequest(SubOpBarrierRootToken, badJSON))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// Helpers
// =============================================================================

// mustEncodePayload encodes v as JSON for QUIC wire format and fails the test
// on error.
func mustEncodePayload(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := EncodePayload(v)
	require.NoError(t, err)
	return b
}
