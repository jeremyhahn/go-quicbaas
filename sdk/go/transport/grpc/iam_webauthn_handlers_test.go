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

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// webAuthnIAMServer records what the handler sent and answers with a populated
// response, so the request mapping and the response projection are both
// exercised end to end over a real gRPC connection.
type webAuthnIAMServer struct {
	iampb.UnimplementedIAMServiceServer

	gotList      *iampb.ListWebAuthnCredentialsRequest
	gotDelete    *iampb.DeleteWebAuthnCredentialRequest
	gotDeleteAll *iampb.DeleteWebAuthnCredentialsRequest
}

func (s *webAuthnIAMServer) ListWebAuthnCredentials(_ context.Context, req *iampb.ListWebAuthnCredentialsRequest) (*iampb.ListWebAuthnCredentialsResponse, error) {
	s.gotList = req
	return &iampb.ListWebAuthnCredentialsResponse{
		Credentials: []*iampb.WebAuthnCredential{{
			Id:              "AbCd01",
			Aaguid:          "aabbccdd",
			AttestationType: "packed",
			Transports:      []string{"usb", "nfc"},
			SignCount:       7,
			BackupEligible:  true,
			BackupState:     true,
			CloneWarning:    true,
		}},
		Total: 1,
	}, nil
}

func (s *webAuthnIAMServer) DeleteWebAuthnCredential(_ context.Context, req *iampb.DeleteWebAuthnCredentialRequest) (*iampb.DeleteWebAuthnCredentialResponse, error) {
	s.gotDelete = req
	return &iampb.DeleteWebAuthnCredentialResponse{Success: true}, nil
}

func (s *webAuthnIAMServer) DeleteWebAuthnCredentials(_ context.Context, req *iampb.DeleteWebAuthnCredentialsRequest) (*iampb.DeleteWebAuthnCredentialsResponse, error) {
	s.gotDeleteAll = req
	return &iampb.DeleteWebAuthnCredentialsResponse{Deleted: 3}, nil
}

func newWebAuthnHandler(t *testing.T) (*iamHandler, *webAuthnIAMServer) {
	t.Helper()
	backend := &webAuthnIAMServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, backend)
	})
	return newIAMHandler(srv.dial(t)), backend
}

// TestWebAuthnHandlerListOverGRPC asserts the credential verbs reach the
// IAMService RPCs over gRPC — the transport the CLI defaults to — and that the
// response is projected onto the same JSON field names the REST transport
// produces, so the SDK client decodes one contract either way.
func TestWebAuthnHandlerListOverGRPC(t *testing.T) {
	h, backend := newWebAuthnHandler(t)

	resp, err := h.handleRequest(context.Background(), iamWebAuthnServicePrefix, "ListWebAuthnCredentials",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
	if err != nil {
		t.Fatalf("handleRequest: %v", err)
	}
	if backend.gotList == nil || backend.gotList.TenantId != "t1" || backend.gotList.UserId != "u1" {
		t.Fatalf("server received %+v, want tenant t1 / user u1", backend.gotList)
	}

	out, ok := resp.(map[string]interface{})
	if !ok {
		t.Fatalf("response type = %T, want map", resp)
	}
	if out["total"] != 1 {
		t.Errorf("total = %v, want 1", out["total"])
	}
	credentials, ok := out["credentials"].([]map[string]interface{})
	if !ok || len(credentials) != 1 {
		t.Fatalf("credentials = %#v, want one entry", out["credentials"])
	}
	got := credentials[0]
	for key, want := range map[string]interface{}{
		"id":               "AbCd01",
		"aaguid":           "aabbccdd",
		"attestation_type": "packed",
		"sign_count":       uint32(7),
		"backup_eligible":  true,
		"backup_state":     true,
		"clone_warning":    true,
	} {
		if got[key] != want {
			t.Errorf("credential[%q] = %v, want %v", key, got[key], want)
		}
	}
	if _, ok := got["transports"]; !ok {
		t.Error("credential must carry its reported transports")
	}
	if _, leaked := got["public_key"]; leaked {
		t.Error("the wire projection must not carry key material")
	}
}

// TestWebAuthnHandlerDeleteOverGRPC drives both delete verbs and asserts the
// credential id and the deleted count survive the round trip.
func TestWebAuthnHandlerDeleteOverGRPC(t *testing.T) {
	h, backend := newWebAuthnHandler(t)
	ctx := context.Background()

	if _, err := h.handleRequest(ctx, iamWebAuthnServicePrefix, "DeleteWebAuthnCredential",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "credential_id": "AbCd01"}); err != nil {
		t.Fatalf("DeleteWebAuthnCredential: %v", err)
	}
	if backend.gotDelete == nil || backend.gotDelete.CredentialId != "AbCd01" {
		t.Fatalf("server received %+v, want credential AbCd01", backend.gotDelete)
	}

	resp, err := h.handleRequest(ctx, iamWebAuthnServicePrefix, "DeleteWebAuthnCredentials",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
	if err != nil {
		t.Fatalf("DeleteWebAuthnCredentials: %v", err)
	}
	if backend.gotDeleteAll == nil || backend.gotDeleteAll.UserId != "u1" {
		t.Fatalf("server received %+v, want user u1", backend.gotDeleteAll)
	}
	out, ok := resp.(map[string]interface{})
	if !ok || out["deleted"] != 3 {
		t.Errorf("deleted = %#v, want 3", resp)
	}
}

// TestWebAuthnHandlerRejectsUnknownMethod keeps an unrecognised verb an
// explicit unimplemented rather than a silent no-op.
func TestWebAuthnHandlerRejectsUnknownMethod(t *testing.T) {
	h, _ := newWebAuthnHandler(t)

	if _, err := h.handleRequest(context.Background(), iamWebAuthnServicePrefix, "Frobnicate",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}); err == nil {
		t.Error("expected an error for an unknown method")
	}
	if _, err := h.handleRequest(context.Background(), iamWebAuthnServicePrefix, "DeleteWebAuthnCredential",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}); err == nil {
		t.Error("expected an error when credential_id is missing")
	}
}
