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
// Mock CeremonyServicer
// =============================================================================

type mockCeremonyServicer struct {
	err error // when non-nil every method returns it
}

func (m *mockCeremonyServicer) CeremonyInitialize(context.Context, *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.CeremonyInitInfo{
		SPKIPin: "pin", CACertPEM: []byte("ca"), TLSCertPEM: []byte("tls"),
		SOCertPEM: []byte("so"), SOKeyPEM: []byte("sokey"),
		State: "initialized", ShareCount: 3,
	}, nil
}

func (m *mockCeremonyServicer) CeremonyStatus(context.Context) (*service.CeremonyStatusInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.CeremonyStatusInfo{State: "ready", SPKIPin: "pin"}, nil
}

func (m *mockCeremonyServicer) CeremonyBeginClaimCert(context.Context, string) (*service.CeremonyChallenge, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.CeremonyChallenge{NonceHex: "abcd", ExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (m *mockCeremonyServicer) CeremonyCompleteClaimCert(context.Context, string, string, string) (*service.CeremonyClaimCertResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.CeremonyClaimCertResult{CertPEM: []byte("cert"), CACertPEM: []byte("ca")}, nil
}

func (m *mockCeremonyServicer) CeremonyClaimShare(context.Context, string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return "share-value", nil
}

func (m *mockCeremonyServicer) CeremonySignCSR(context.Context, string, string, string, string) (*service.CeremonySignCSRResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.CeremonySignCSRResult{CertPEM: []byte("cert")}, nil
}

func (m *mockCeremonyServicer) CeremonyEnrollExternalOfficer(context.Context, *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.CeremonyEnrollExternalOfficerResponse{SealedShare: "sealed"}, nil
}

func newCeremonyHandler(t *testing.T, svc service.CeremonyServicer) *Handler {
	t.Helper()
	return NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithCeremonyService(svc))
}

func ceremonyRequest(subOp byte, body []byte) *Request {
	return &Request{Opcode: OpCeremony, Payload: append([]byte{subOp}, body...)}
}

func encMust(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := EncodePayload(v)
	require.NoError(t, err)
	return b
}

// =============================================================================
// Happy paths
// =============================================================================

func TestCeremonyDispatch_HappyPaths(t *testing.T) {
	handler := newCeremonyHandler(t, &mockCeremonyServicer{})
	ctx := context.Background()

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"initialize", SubOpCeremonyInitialize, encMust(t, &ceremonyInitializeWireRequest{SOPin: "1234", UserPin: "5678", Threshold: 2})},
		{"status", SubOpCeremonyStatus, nil},
		{"claim-cert-begin", SubOpCeremonyClaimCertBegin, encMust(t, &ceremonyClaimCertBeginWireRequest{Username: "alice"})},
		{"claim-cert-complete", SubOpCeremonyClaimCertComplete, encMust(t, &ceremonyClaimCertCompleteWireRequest{Username: "alice", NonceHex: "ab", Signature: "sig"})},
		{"claim-share", SubOpCeremonyClaimShare, encMust(t, &ceremonyClaimShareWireRequest{Username: "alice"})},
		{"sign-csr", SubOpCeremonySignCSR, encMust(t, &ceremonySignCSRWireRequest{Username: "alice", CSRPEM: "csr", Role: "officer"})},
		{"list-officers", SubOpCeremonyListOfficers, nil},
		{"enroll-external", SubOpCeremonyEnrollExternalOfficer, encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "bob", CertPEM: "cert", AuthFactor: "piv"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, ceremonyRequest(tc.subOp, tc.body))
			assert.Equal(t, StatusOK, resp.Status, "expected OK, got status %d", resp.Status)
		})
	}
}

// =============================================================================
// Service-error paths
// =============================================================================

func TestCeremonyDispatch_ServiceErrors(t *testing.T) {
	handler := newCeremonyHandler(t, &mockCeremonyServicer{err: errors.New("ceremony boom")})
	ctx := context.Background()

	cases := []struct {
		name  string
		subOp byte
		body  []byte
	}{
		{"initialize", SubOpCeremonyInitialize, encMust(t, &ceremonyInitializeWireRequest{SOPin: "1234", Threshold: 2})},
		{"status", SubOpCeremonyStatus, nil},
		{"claim-cert-begin", SubOpCeremonyClaimCertBegin, encMust(t, &ceremonyClaimCertBeginWireRequest{Username: "alice"})},
		{"claim-cert-complete", SubOpCeremonyClaimCertComplete, encMust(t, &ceremonyClaimCertCompleteWireRequest{Username: "alice", NonceHex: "ab", Signature: "sig"})},
		{"claim-share", SubOpCeremonyClaimShare, encMust(t, &ceremonyClaimShareWireRequest{Username: "alice"})},
		{"sign-csr", SubOpCeremonySignCSR, encMust(t, &ceremonySignCSRWireRequest{Username: "alice", CSRPEM: "csr"})},
		{"enroll-external", SubOpCeremonyEnrollExternalOfficer, encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "bob", CertPEM: "cert", AuthFactor: "piv"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := handler.HandleRequest(ctx, ceremonyRequest(tc.subOp, tc.body))
			assert.NotEqual(t, StatusOK, resp.Status)
		})
	}
}

// =============================================================================
// Decode-error paths
// =============================================================================

func TestCeremonyDispatch_DecodeErrors(t *testing.T) {
	handler := newCeremonyHandler(t, &mockCeremonyServicer{})
	ctx := context.Background()
	bad := []byte("{bad")

	subOps := []byte{
		SubOpCeremonyInitialize,
		SubOpCeremonyClaimCertBegin,
		SubOpCeremonyClaimCertComplete,
		SubOpCeremonyClaimShare,
		SubOpCeremonySignCSR,
		SubOpCeremonyEnrollExternalOfficer,
	}
	for _, subOp := range subOps {
		resp := handler.HandleRequest(ctx, ceremonyRequest(subOp, bad))
		assert.NotEqual(t, StatusOK, resp.Status, "subOp %d should reject malformed body", subOp)
	}
}

// =============================================================================
// Validation errors
// =============================================================================

func TestCeremonyDispatch_ValidationErrors(t *testing.T) {
	handler := newCeremonyHandler(t, &mockCeremonyServicer{})
	ctx := context.Background()

	// claim-cert-begin: missing username
	resp := handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyClaimCertBegin,
		encMust(t, &ceremonyClaimCertBeginWireRequest{})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// claim-cert-complete: missing nonce
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyClaimCertComplete,
		encMust(t, &ceremonyClaimCertCompleteWireRequest{Username: "alice"})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// claim-cert-complete: missing signature
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyClaimCertComplete,
		encMust(t, &ceremonyClaimCertCompleteWireRequest{Username: "alice", NonceHex: "ab"})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// claim-share: missing username
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyClaimShare,
		encMust(t, &ceremonyClaimShareWireRequest{})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// sign-csr: missing csr_pem
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonySignCSR,
		encMust(t, &ceremonySignCSRWireRequest{Username: "alice"})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// enroll-external: missing username
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyEnrollExternalOfficer,
		encMust(t, &ceremonyEnrollExternalOfficerWireRequest{CertPEM: "c", AuthFactor: "piv"})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// enroll-external: neither cert_pem nor pubkey_pem
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyEnrollExternalOfficer,
		encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "bob", AuthFactor: "piv"})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// enroll-external: both cert_pem and pubkey_pem
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyEnrollExternalOfficer,
		encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "bob", CertPEM: "c", PubKeyPEM: "p", AuthFactor: "piv"})))
	assert.NotEqual(t, StatusOK, resp.Status)

	// enroll-external: missing auth_factor
	resp = handler.HandleRequest(ctx, ceremonyRequest(SubOpCeremonyEnrollExternalOfficer,
		encMust(t, &ceremonyEnrollExternalOfficerWireRequest{Username: "bob", CertPEM: "c"})))
	assert.NotEqual(t, StatusOK, resp.Status)
}
