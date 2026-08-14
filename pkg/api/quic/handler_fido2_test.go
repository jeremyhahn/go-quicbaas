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

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// errQUICFIDO2 drives error propagation through the FIDO2 handlers.
var errQUICFIDO2 = errors.New("fido2: boom")

// fakeFIDO2Service records its input (including the tenant scope) and
// replays a fixed response.
type fakeFIDO2Service struct {
	failNext bool

	lastTenant     string
	lastBeginReg   *xkmstransport.BeginRegistrationRequest
	lastFinishReg  *xkmstransport.FinishRegistrationRequest
	lastBeginAuth  *xkmstransport.BeginAuthenticationRequest
	lastFinishAuth *xkmstransport.FinishAuthenticationRequest
}

func (f *fakeFIDO2Service) BeginRegistration(_ context.Context, tenantID string, req *xkmstransport.BeginRegistrationRequest) (*xkmstransport.BeginRegistrationResponse, error) {
	f.lastTenant = tenantID
	f.lastBeginReg = req
	if f.failNext {
		return nil, errQUICFIDO2
	}
	return &xkmstransport.BeginRegistrationResponse{SessionID: "sess-1", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Service) FinishRegistration(_ context.Context, tenantID string, req *xkmstransport.FinishRegistrationRequest) (*xkmstransport.FinishRegistrationResponse, error) {
	f.lastTenant = tenantID
	f.lastFinishReg = req
	if f.failNext {
		return nil, errQUICFIDO2
	}
	return &xkmstransport.FinishRegistrationResponse{Success: true, CredentialID: "cred-1", Token: "tok"}, nil
}

func (f *fakeFIDO2Service) BeginAuthentication(_ context.Context, tenantID string, req *xkmstransport.BeginAuthenticationRequest) (*xkmstransport.BeginAuthenticationResponse, error) {
	f.lastTenant = tenantID
	f.lastBeginAuth = req
	if f.failNext {
		return nil, errQUICFIDO2
	}
	return &xkmstransport.BeginAuthenticationResponse{SessionID: "sess-2", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Service) FinishAuthentication(_ context.Context, tenantID string, req *xkmstransport.FinishAuthenticationRequest) (*xkmstransport.FinishAuthenticationResponse, error) {
	f.lastTenant = tenantID
	f.lastFinishAuth = req
	if f.failNext {
		return nil, errQUICFIDO2
	}
	return &xkmstransport.FinishAuthenticationResponse{Success: true, Token: "tok", Username: req.Username}, nil
}

// newFIDO2TestHandler builds a QUIC Handler whose FIDO2() returns the
// supplied service.
func newFIDO2TestHandler(t *testing.T, svc service.FIDO2Service) *Handler {
	t.Helper()
	return NewHandler(&mockService{fido2Svc: svc}, nil, nil, nil, createCertificateTestLogger())
}

// fido2Request builds a wire request carrying the sub-op byte followed by
// the JSON body (tenant_id injected alongside the xkms fields), exactly as
// the SDK QUIC transport encodes it.
func fido2Request(t *testing.T, subOp byte, body map[string]interface{}) *Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	full := append([]byte{subOp}, payload...)
	return &Request{Opcode: OpOrchestrator, Payload: full}
}

func TestHandler_FIDO2Dispatch_BeginRegistration(t *testing.T) {
	fake := &fakeFIDO2Service{}
	h := newFIDO2TestHandler(t, fake)

	resp := h.handleOrchestratorDispatch(context.Background(),
		fido2Request(t, SubOpFIDO2BeginRegistration, map[string]interface{}{
			"tenant_id":    "tenant-a",
			"username":     "alice@example.com",
			"display_name": "Alice",
		}))
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	var decoded xkmstransport.BeginRegistrationResponse
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.SessionID != "sess-1" || decoded.OptionsJSON == "" {
		t.Fatalf("unexpected response: %+v", decoded)
	}
	if fake.lastTenant != "tenant-a" {
		t.Fatalf("tenant = %q, want tenant-a", fake.lastTenant)
	}
	if fake.lastBeginReg == nil || fake.lastBeginReg.Username != "alice@example.com" || fake.lastBeginReg.DisplayName != "Alice" {
		t.Fatalf("request did not reach the service: %+v", fake.lastBeginReg)
	}
}

func TestHandler_FIDO2Dispatch_FinishRegistration(t *testing.T) {
	fake := &fakeFIDO2Service{}
	h := newFIDO2TestHandler(t, fake)

	resp := h.handleOrchestratorDispatch(context.Background(),
		fido2Request(t, SubOpFIDO2FinishRegistration, map[string]interface{}{
			"tenant_id":        "tenant-a",
			"session_id":       "sess-1",
			"attestation_json": `{"id":"cred"}`,
		}))
	if resp.Status != StatusOK {
		t.Fatalf("status = %v, payload = %s", resp.Status, resp.Payload)
	}

	var decoded xkmstransport.FinishRegistrationResponse
	if err := json.Unmarshal(resp.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Success || decoded.CredentialID != "cred-1" {
		t.Fatalf("unexpected response: %+v", decoded)
	}
	if fake.lastFinishReg == nil || fake.lastFinishReg.SessionID != "sess-1" {
		t.Fatalf("request did not reach the service: %+v", fake.lastFinishReg)
	}
}

func TestHandler_FIDO2Dispatch_AuthenticationCeremony(t *testing.T) {
	fake := &fakeFIDO2Service{}
	h := newFIDO2TestHandler(t, fake)

	begin := h.handleOrchestratorDispatch(context.Background(),
		fido2Request(t, SubOpFIDO2BeginAuthentication, map[string]interface{}{
			"tenant_id": "tenant-b",
			"username":  "bob@example.com",
		}))
	if begin.Status != StatusOK {
		t.Fatalf("begin: status = %v payload = %s", begin.Status, begin.Payload)
	}
	if fake.lastBeginAuth == nil || fake.lastBeginAuth.Username != "bob@example.com" {
		t.Fatalf("begin request did not reach the service: %+v", fake.lastBeginAuth)
	}

	finish := h.handleOrchestratorDispatch(context.Background(),
		fido2Request(t, SubOpFIDO2FinishAuthentication, map[string]interface{}{
			"tenant_id":      "tenant-b",
			"session_id":     "sess-2",
			"username":       "bob@example.com",
			"assertion_json": `{"id":"cred"}`,
		}))
	if finish.Status != StatusOK {
		t.Fatalf("finish: status = %v payload = %s", finish.Status, finish.Payload)
	}

	var decoded xkmstransport.FinishAuthenticationResponse
	if err := json.Unmarshal(finish.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !decoded.Success || decoded.Token != "tok" || decoded.Username != "bob@example.com" {
		t.Fatalf("unexpected response: %+v", decoded)
	}
	if fake.lastFinishAuth == nil || fake.lastFinishAuth.AssertionJSON == "" {
		t.Fatalf("finish request did not reach the service: %+v", fake.lastFinishAuth)
	}
}

func TestHandler_FIDO2Dispatch_ServiceErrorPropagates(t *testing.T) {
	fake := &fakeFIDO2Service{failNext: true}
	h := newFIDO2TestHandler(t, fake)

	resp := h.handleOrchestratorDispatch(context.Background(),
		fido2Request(t, SubOpFIDO2BeginRegistration, map[string]interface{}{
			"tenant_id": "tenant-a",
			"username":  "alice@example.com",
		}))
	if resp.Status == StatusOK {
		t.Fatalf("expected error status, got OK")
	}
}

func TestHandler_FIDO2Dispatch_NilServiceUnavailable(t *testing.T) {
	h := newFIDO2TestHandler(t, nil)

	resp := h.handleOrchestratorDispatch(context.Background(),
		fido2Request(t, SubOpFIDO2BeginAuthentication, map[string]interface{}{
			"tenant_id": "tenant-a",
			"username":  "alice@example.com",
		}))
	if resp.Status == StatusOK {
		t.Fatalf("expected error status for nil FIDO2 service, got OK")
	}
}

// TestFIDO2PublicSubOperations pins the pre-auth boundary: ONLY the two
// authentication-ceremony sub-ops bypass authentication, and only on the
// OpOrchestrator dispatch byte.
func TestFIDO2PublicSubOperations(t *testing.T) {
	if !isPublicSubOperation(OpOrchestrator, []byte{SubOpFIDO2BeginAuthentication}) {
		t.Fatal("BeginAuthentication must be pre-auth")
	}
	if !isPublicSubOperation(OpOrchestrator, []byte{SubOpFIDO2FinishAuthentication}) {
		t.Fatal("FinishAuthentication must be pre-auth")
	}
	if isPublicSubOperation(OpOrchestrator, []byte{SubOpFIDO2BeginRegistration}) {
		t.Fatal("BeginRegistration must NOT be pre-auth")
	}
	if isPublicSubOperation(OpOrchestrator, []byte{SubOpFIDO2FinishRegistration}) {
		t.Fatal("FinishRegistration must NOT be pre-auth")
	}
	if isPublicSubOperation(OpOrchestrator, []byte{SubOpPIVListSlots}) {
		t.Fatal("PIV verbs must NOT be pre-auth")
	}
	if isPublicSubOperation(OpOrchestrator, nil) {
		t.Fatal("empty payload must NOT be pre-auth")
	}
	if isPublicSubOperation(OpBarrier, []byte{SubOpFIDO2BeginAuthentication}) {
		t.Fatal("other dispatch bytes must NOT be pre-auth")
	}
}

// TestFIDO2SubOpPermissions pins the registration verbs to
// user_authfactors:add and keeps the authentication verbs OUT of the table
// so they fail closed if they ever reach the authz resolver.
func TestFIDO2SubOpPermissions(t *testing.T) {
	for _, subOp := range []byte{SubOpFIDO2BeginRegistration, SubOpFIDO2FinishRegistration} {
		perm, ok := fido2SubOpPermissions[subOp]
		if !ok {
			t.Fatalf("registration sub-op 0x%02X has no permission mapping", subOp)
		}
		if perm.resource != "user_authfactors" || perm.action != "add" {
			t.Fatalf("registration sub-op 0x%02X resolves %q:%q, want user_authfactors:add",
				subOp, perm.resource, perm.action)
		}
	}
	for _, subOp := range []byte{SubOpFIDO2BeginAuthentication, SubOpFIDO2FinishAuthentication} {
		if _, ok := fido2SubOpPermissions[subOp]; ok {
			t.Fatalf("authentication sub-op 0x%02X must not be in the permission table", subOp)
		}
		if _, _, ok := getQRDBPermission(OpOrchestrator, []byte{subOp}); ok {
			t.Fatalf("authentication sub-op 0x%02X must fail closed in getQRDBPermission", subOp)
		}
	}
}
