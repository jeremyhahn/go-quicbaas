//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
)

// =============================================================================
// qrdb barrier dispatch handlers (qrdb wire format)
//
// These Server.qrdbBarrier* methods are the qrdb-ALPN wire-format handlers
// registered in buildQRDBHandlers. They were entirely uncovered. Each has
// three meaningful branches: facade-not-configured, payload-unmarshal-error,
// and success (delegating to the shared BarrierServicer facade).
// =============================================================================

// barrierDispatchHandlers returns every qrdbBarrier* handler that consumes a
// protobuf payload, paired with a name for error messages.
func barrierPayloadHandlers(s *Server) map[string]func(context.Context, []byte) ([]byte, error) {
	return map[string]func(context.Context, []byte) ([]byte, error){
		"Initialize":         s.qrdbBarrierInitialize,
		"Unseal":             s.qrdbBarrierUnseal,
		"ShamirInitialize":   s.qrdbBarrierShamirInitialize,
		"ShamirUnsealShare":  s.qrdbBarrierShamirUnsealShare,
		"ShamirUnsealShares": s.qrdbBarrierShamirUnsealShares,
		"Rekey":              s.qrdbBarrierRekey,
		"RecoveryGenerate":   s.qrdbBarrierRecoveryGenerate,
		"RecoveryRecover":    s.qrdbBarrierRecoveryRecover,
		"RootToken":          s.qrdbBarrierRootToken,
	}
}

// barrierNoPayloadHandlers returns the qrdbBarrier* handlers that ignore their
// payload (Seal, RecoveryDelete, RecoveryStatus).
func barrierNoPayloadHandlers(s *Server) map[string]func(context.Context, []byte) ([]byte, error) {
	return map[string]func(context.Context, []byte) ([]byte, error){
		"Seal":           s.qrdbBarrierSeal,
		"RecoveryDelete": s.qrdbBarrierRecoveryDelete,
		"RecoveryStatus": s.qrdbBarrierRecoveryStatus,
	}
}

// TestQRDBBarrierDispatch_NotConfigured verifies every barrier handler returns
// the typed not-configured error when no barrier facade is wired.
func TestQRDBBarrierDispatch_NotConfigured(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	ctx := context.Background()

	all := barrierPayloadHandlers(server)
	for name, h := range barrierNoPayloadHandlers(server) {
		all[name] = h
	}

	for name, h := range all {
		_, err := h(ctx, nil)
		require.Error(t, err, "%s must error when barrier not configured", name)
		var notCfg *QRDBBarrierNotConfiguredError
		assert.ErrorAs(t, err, &notCfg, "%s must return QRDBBarrierNotConfiguredError", name)
	}
}

// TestQRDBBarrierDispatch_UnmarshalErrors verifies the payload-consuming
// handlers reject malformed protobuf with the typed unmarshal error.
func TestQRDBBarrierDispatch_UnmarshalErrors(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	server.SetBarrierService(&mockBarrierServicer{})
	ctx := context.Background()
	bad := []byte{0xff, 0xff, 0xff, 0xff}

	for name, h := range barrierPayloadHandlers(server) {
		_, err := h(ctx, bad)
		require.Error(t, err, "%s must reject malformed payload", name)
		var unmarshalErr *QRDBUnmarshalError
		assert.ErrorAs(t, err, &unmarshalErr, "%s must return QRDBUnmarshalError", name)
	}
}

// TestQRDBBarrierDispatch_HappyPaths exercises the success branch of every
// barrier handler and verifies the marshalled responses.
func TestQRDBBarrierDispatch_HappyPaths(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	server.SetBarrierService(&mockBarrierServicer{})
	ctx := context.Background()

	_, err := server.qrdbBarrierInitialize(ctx, mustMarshal(t, &pb.BarrierInitializeRequest{Secret: "pw"}))
	require.NoError(t, err)

	_, err = server.qrdbBarrierUnseal(ctx, mustMarshal(t, &pb.BarrierUnsealRequest{Secret: "pw"}))
	require.NoError(t, err)

	_, err = server.qrdbBarrierSeal(ctx, nil)
	require.NoError(t, err)

	shamirOut, err := server.qrdbBarrierShamirInitialize(ctx, mustMarshal(t, &pb.BarrierShamirInitializeRequest{
		Secret: "pw", Threshold: 2, TotalShares: 3,
	}))
	require.NoError(t, err)
	var shamirResp pb.BarrierShamirInitializeResponse
	require.NoError(t, proto.Unmarshal(shamirOut, &shamirResp))
	assert.Equal(t, int32(2), shamirResp.GetThreshold())
	assert.Equal(t, int32(3), shamirResp.GetTotalShares())
	assert.Len(t, shamirResp.GetShares(), 3)

	shareOut, err := server.qrdbBarrierShamirUnsealShare(ctx, mustMarshal(t, &pb.BarrierShamirUnsealShareRequest{Share: "s1"}))
	require.NoError(t, err)
	var shareResp pb.BarrierShamirUnsealShareResponse
	require.NoError(t, proto.Unmarshal(shareOut, &shareResp))
	assert.Equal(t, int32(2), shareResp.GetRequired())
	assert.Equal(t, int32(1), shareResp.GetSubmitted())
	assert.False(t, shareResp.GetComplete())

	_, err = server.qrdbBarrierShamirUnsealShares(ctx, mustMarshal(t, &pb.BarrierShamirUnsealSharesRequest{Shares: []string{"s1", "s2"}}))
	require.NoError(t, err)

	rekeyOut, err := server.qrdbBarrierRekey(ctx, mustMarshal(t, &pb.BarrierRekeyRequest{Threshold: 2, Total: 2}))
	require.NoError(t, err)
	var rekeyResp pb.BarrierShamirInitializeResponse
	require.NoError(t, proto.Unmarshal(rekeyOut, &rekeyResp))
	assert.Len(t, rekeyResp.GetShares(), 2)

	recGenOut, err := server.qrdbBarrierRecoveryGenerate(ctx, mustMarshal(t, &pb.BarrierRecoveryGenerateRequest{Threshold: 2, Total: 2}))
	require.NoError(t, err)
	var recGenResp pb.BarrierRecoveryKeysResponse
	require.NoError(t, proto.Unmarshal(recGenOut, &recGenResp))
	assert.Len(t, recGenResp.GetKeys(), 2)

	_, err = server.qrdbBarrierRecoveryRecover(ctx, mustMarshal(t, &pb.BarrierRecoveryRecoverRequest{Keys: []string{"r1", "r2"}}))
	require.NoError(t, err)

	_, err = server.qrdbBarrierRecoveryDelete(ctx, nil)
	require.NoError(t, err)

	statusOut, err := server.qrdbBarrierRecoveryStatus(ctx, nil)
	require.NoError(t, err)
	var statusResp pb.BarrierRecoveryStatusResponse
	require.NoError(t, proto.Unmarshal(statusOut, &statusResp))
	assert.False(t, statusResp.GetHasRecoveryKeys())

	rootOut, err := server.qrdbBarrierRootToken(ctx, mustMarshal(t, &pb.BarrierRootTokenRequest{Shares: []string{"s1", "s2"}}))
	require.NoError(t, err)
	var rootResp pb.BarrierRootTokenResponse
	require.NoError(t, proto.Unmarshal(rootOut, &rootResp))
	assert.Equal(t, "deadbeef", rootResp.GetToken())
	assert.NotEmpty(t, rootResp.GetCreatedAt())
}

// TestQRDBBarrierDispatch_FacadeErrors verifies handlers surface the underlying
// facade error.
func TestQRDBBarrierDispatch_FacadeErrors(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	sentinel := errors.New("barrier boom")
	server.SetBarrierService(&mockBarrierServicer{err: sentinel})
	ctx := context.Background()

	cases := []struct {
		name   string
		invoke func() ([]byte, error)
	}{
		{name: "Initialize", invoke: func() ([]byte, error) {
			return server.qrdbBarrierInitialize(ctx, mustMarshal(t, &pb.BarrierInitializeRequest{Secret: "pw"}))
		}},
		{name: "Seal", invoke: func() ([]byte, error) { return server.qrdbBarrierSeal(ctx, nil) }},
		{name: "ShamirInitialize", invoke: func() ([]byte, error) {
			return server.qrdbBarrierShamirInitialize(ctx, mustMarshal(t, &pb.BarrierShamirInitializeRequest{Threshold: 2, TotalShares: 3}))
		}},
		{name: "ShamirUnsealShare", invoke: func() ([]byte, error) {
			return server.qrdbBarrierShamirUnsealShare(ctx, mustMarshal(t, &pb.BarrierShamirUnsealShareRequest{Share: "s1"}))
		}},
		{name: "Rekey", invoke: func() ([]byte, error) {
			return server.qrdbBarrierRekey(ctx, mustMarshal(t, &pb.BarrierRekeyRequest{Threshold: 2, Total: 2}))
		}},
		{name: "RecoveryGenerate", invoke: func() ([]byte, error) {
			return server.qrdbBarrierRecoveryGenerate(ctx, mustMarshal(t, &pb.BarrierRecoveryGenerateRequest{Threshold: 2, Total: 2}))
		}},
		{name: "RecoveryDelete", invoke: func() ([]byte, error) { return server.qrdbBarrierRecoveryDelete(ctx, nil) }},
		{name: "RecoveryStatus", invoke: func() ([]byte, error) { return server.qrdbBarrierRecoveryStatus(ctx, nil) }},
		{name: "RootToken", invoke: func() ([]byte, error) {
			return server.qrdbBarrierRootToken(ctx, mustMarshal(t, &pb.BarrierRootTokenRequest{Shares: []string{"s1"}}))
		}},
	}

	for _, tc := range cases {
		_, err := tc.invoke()
		require.Error(t, err, "%s must surface facade error", tc.name)
		assert.ErrorIs(t, err, sentinel, "%s must wrap the sentinel", tc.name)
	}
}

// =============================================================================
// qrdb ceremony dispatch handlers (qrdb wire format)
// =============================================================================

func ceremonyPayloadHandlers(s *Server) map[string]func(context.Context, []byte) ([]byte, error) {
	return map[string]func(context.Context, []byte) ([]byte, error){
		"Initialize":            s.qrdbCeremonyInitialize,
		"ClaimCertBegin":        s.qrdbCeremonyClaimCertBegin,
		"ClaimCertComplete":     s.qrdbCeremonyClaimCertComplete,
		"ClaimShare":            s.qrdbCeremonyClaimShare,
		"SignCSR":               s.qrdbCeremonySignCSR,
		"EnrollExternalOfficer": s.qrdbCeremonyEnrollExternalOfficer,
	}
}

// TestQRDBCeremonyDispatch_NotConfigured verifies every ceremony handler
// returns the typed not-configured error when no ceremony facade is wired.
func TestQRDBCeremonyDispatch_NotConfigured(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	ctx := context.Background()

	all := ceremonyPayloadHandlers(server)
	all["Status"] = server.qrdbCeremonyStatus

	for name, h := range all {
		_, err := h(ctx, nil)
		require.Error(t, err, "%s must error when ceremony not configured", name)
		var notCfg *QRDBCeremonyNotConfiguredError
		assert.ErrorAs(t, err, &notCfg, "%s must return QRDBCeremonyNotConfiguredError", name)
	}
}

// TestQRDBCeremonyDispatch_UnmarshalErrors verifies the payload-consuming
// ceremony handlers reject malformed protobuf.
func TestQRDBCeremonyDispatch_UnmarshalErrors(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	server.SetCeremonyService(&mockCeremonyServicer{})
	ctx := context.Background()
	bad := []byte{0xff, 0xff, 0xff, 0xff}

	for name, h := range ceremonyPayloadHandlers(server) {
		_, err := h(ctx, bad)
		require.Error(t, err, "%s must reject malformed payload", name)
		var unmarshalErr *QRDBUnmarshalError
		assert.ErrorAs(t, err, &unmarshalErr, "%s must return QRDBUnmarshalError", name)
	}
}

// TestQRDBCeremonyDispatch_HappyPaths exercises the success branch of every
// ceremony handler.
func TestQRDBCeremonyDispatch_HappyPaths(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	server.SetCeremonyService(&mockCeremonyServicer{})
	ctx := context.Background()

	initOut, err := server.qrdbCeremonyInitialize(ctx, mustMarshal(t, &pb.CeremonyInitializeRequest{
		SoPin:     "1234",
		UserPin:   "5678",
		Threshold: 2,
		Officers:  []*pb.CeremonyOfficer{{Username: "officer-a", CsrPem: "csr"}},
		Hostname:  "node1",
		Sans:      []string{"node1.local"},
	}))
	require.NoError(t, err)
	var initResp pb.CeremonyInitializeResponse
	require.NoError(t, proto.Unmarshal(initOut, &initResp))
	assert.Equal(t, "pin", initResp.GetSpkiPin())
	assert.Equal(t, int32(3), initResp.GetShareCount())

	statusOut, err := server.qrdbCeremonyStatus(ctx, nil)
	require.NoError(t, err)
	var statusResp pb.CeremonyStatusResponse
	require.NoError(t, proto.Unmarshal(statusOut, &statusResp))
	assert.Equal(t, "ready", statusResp.GetState())

	beginOut, err := server.qrdbCeremonyClaimCertBegin(ctx, mustMarshal(t, &pb.CeremonyClaimCertBeginRequest{Username: "officer-a"}))
	require.NoError(t, err)
	var beginResp pb.CeremonyClaimCertBeginResponse
	require.NoError(t, proto.Unmarshal(beginOut, &beginResp))
	assert.Equal(t, "abcd", beginResp.GetNonceHex())
	assert.NotEmpty(t, beginResp.GetExpiresAt())

	completeOut, err := server.qrdbCeremonyClaimCertComplete(ctx, mustMarshal(t, &pb.CeremonyClaimCertCompleteRequest{
		Username: "officer-a", NonceHex: "abcd", Signature: "sig",
	}))
	require.NoError(t, err)
	var completeResp pb.CeremonyClaimCertCompleteResponse
	require.NoError(t, proto.Unmarshal(completeOut, &completeResp))
	assert.Equal(t, "cert", completeResp.GetCertPem())

	shareOut, err := server.qrdbCeremonyClaimShare(ctx, mustMarshal(t, &pb.CeremonyClaimShareRequest{Username: "officer-a"}))
	require.NoError(t, err)
	var shareResp pb.CeremonyClaimShareResponse
	require.NoError(t, proto.Unmarshal(shareOut, &shareResp))
	assert.Equal(t, "share-value", shareResp.GetShare())

	signOut, err := server.qrdbCeremonySignCSR(ctx, mustMarshal(t, &pb.CeremonySignCSRRequest{
		Username: "officer-a", SoPin: "1234", CsrPem: "csr", Role: "admin",
	}))
	require.NoError(t, err)
	var signResp pb.CeremonySignCSRResponse
	require.NoError(t, proto.Unmarshal(signOut, &signResp))
	assert.Equal(t, "cert", signResp.GetCertPem())

	enrollOut, err := server.qrdbCeremonyEnrollExternalOfficer(ctx, mustMarshal(t, &pb.CeremonyEnrollExternalOfficerRequest{
		Username: "officer-b", CertPem: "cert", PubkeyPem: "pub", ShareIndex: 1, AuthFactor: "piv", ShareBytes: []byte("share"),
	}))
	require.NoError(t, err)
	var enrollResp pb.CeremonyEnrollExternalOfficerResponse
	require.NoError(t, proto.Unmarshal(enrollOut, &enrollResp))
	assert.Equal(t, "sealed", enrollResp.GetSealedShare())
}

// TestQRDBCeremonyDispatch_FacadeErrors verifies handlers surface the
// underlying ceremony facade error.
func TestQRDBCeremonyDispatch_FacadeErrors(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	sentinel := errors.New("ceremony boom")
	server.SetCeremonyService(&mockCeremonyServicer{err: sentinel})
	ctx := context.Background()

	cases := []struct {
		name   string
		invoke func() ([]byte, error)
	}{
		{name: "Initialize", invoke: func() ([]byte, error) {
			return server.qrdbCeremonyInitialize(ctx, mustMarshal(t, &pb.CeremonyInitializeRequest{Threshold: 2}))
		}},
		{name: "Status", invoke: func() ([]byte, error) { return server.qrdbCeremonyStatus(ctx, nil) }},
		{name: "ClaimCertBegin", invoke: func() ([]byte, error) {
			return server.qrdbCeremonyClaimCertBegin(ctx, mustMarshal(t, &pb.CeremonyClaimCertBeginRequest{Username: "a"}))
		}},
		{name: "ClaimCertComplete", invoke: func() ([]byte, error) {
			return server.qrdbCeremonyClaimCertComplete(ctx, mustMarshal(t, &pb.CeremonyClaimCertCompleteRequest{Username: "a"}))
		}},
		{name: "ClaimShare", invoke: func() ([]byte, error) {
			return server.qrdbCeremonyClaimShare(ctx, mustMarshal(t, &pb.CeremonyClaimShareRequest{Username: "a"}))
		}},
		{name: "SignCSR", invoke: func() ([]byte, error) {
			return server.qrdbCeremonySignCSR(ctx, mustMarshal(t, &pb.CeremonySignCSRRequest{Username: "a"}))
		}},
		{name: "EnrollExternalOfficer", invoke: func() ([]byte, error) {
			return server.qrdbCeremonyEnrollExternalOfficer(ctx, mustMarshal(t, &pb.CeremonyEnrollExternalOfficerRequest{Username: "a"}))
		}},
	}

	for _, tc := range cases {
		_, err := tc.invoke()
		require.Error(t, err, "%s must surface facade error", tc.name)
		assert.ErrorIs(t, err, sentinel, "%s must wrap the sentinel", tc.name)
	}
}
