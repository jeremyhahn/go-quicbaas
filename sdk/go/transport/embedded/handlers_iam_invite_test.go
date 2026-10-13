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

package embedded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubInviteActivation records the scope each verb receives.
type stubInviteActivation struct {
	gotTenant, gotToken string
	gotCredential       map[string]interface{}
	gotCert             []byte
	meta                *service.InviteMetadata
	activation          *service.InviteActivation
	err                 error
}

func (s *stubInviteActivation) GetInvite(_ context.Context, tenantID, token string) (*service.InviteMetadata, error) {
	s.gotTenant, s.gotToken = tenantID, token
	return s.meta, s.err
}

func (s *stubInviteActivation) ActivateFIDO2Begin(_ context.Context, tenantID, token string) (map[string]interface{}, error) {
	s.gotTenant, s.gotToken = tenantID, token
	if s.err != nil {
		return nil, s.err
	}
	return map[string]interface{}{"publicKey": "opts"}, nil
}

func (s *stubInviteActivation) ActivateFIDO2Finish(_ context.Context, tenantID, token string, credential map[string]interface{}) (*service.InviteActivation, error) {
	s.gotTenant, s.gotToken, s.gotCredential = tenantID, token, credential
	return s.activation, s.err
}

func (s *stubInviteActivation) ActivatePIV(_ context.Context, tenantID, token string, certPEM []byte) (*service.InviteActivation, error) {
	s.gotTenant, s.gotToken, s.gotCert = tenantID, token, certPEM
	return s.activation, s.err
}

// mockQuicBaaSForInvite exposes the activation service through the same
// structural seam the production embedded transport uses.
type mockQuicBaaSForInvite struct {
	service.QuicBaaS
	activation service.InviteActivationService
}

func (m *mockQuicBaaSForInvite) InviteActivation() service.InviteActivationService {
	return m.activation
}

// inviteEnvelope mirrors the SDK invite client's request structs; the handlers
// extract the fields by name via reflection.
type inviteEnvelope struct {
	TenantID   string
	Token      string
	Credential map[string]interface{}
	CertPEM    string
}

// TestEmbeddedInviteDispatchRegistered guards the parity gap this change
// closes: the SDK invite client dispatches on quicbaas.IAM.Invite, which the
// embedded transport answered with "unknown method".
func TestEmbeddedInviteDispatchRegistered(t *testing.T) {
	for _, key := range []string{
		"quicbaas.IAM.Invite/GetInvite",
		"quicbaas.IAM.Invite/ActivateFIDO2Begin",
		"quicbaas.IAM.Invite/ActivateFIDO2Finish",
		"quicbaas.IAM.Invite/ActivatePIV",
	} {
		if _, ok := unaryHandlers[key]; !ok {
			t.Errorf("embedded transport does not dispatch %s", key)
		}
	}
}

func TestEmbeddedInviteGet(t *testing.T) {
	expires := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	stub := &stubInviteActivation{meta: &service.InviteMetadata{
		UserID: "u1", Email: "a@example.com", AuthMethods: []string{"fido2"}, ExpiresAt: expires,
	}}
	svc := &mockQuicBaaSForInvite{activation: stub}

	resp, err := handleIAMInviteGet(context.Background(), svc, &inviteEnvelope{TenantID: "t1", Token: "tok"})
	if err != nil {
		t.Fatalf("GetInvite: %v", err)
	}
	if stub.gotTenant != "t1" || stub.gotToken != "tok" {
		t.Errorf("service called with %s/%s, want t1/tok", stub.gotTenant, stub.gotToken)
	}
	out, ok := resp.(map[string]interface{})
	if !ok {
		t.Fatalf("response type = %T, want map", resp)
	}
	if out["user_id"] != "u1" || out["email"] != "a@example.com" || out["expires_at"] != expires {
		t.Errorf("response = %#v, want the stub's metadata", out)
	}
}

func TestEmbeddedInviteFIDO2(t *testing.T) {
	stub := &stubInviteActivation{activation: &service.InviteActivation{UserID: "u1", Warning: "w"}}
	svc := &mockQuicBaaSForInvite{activation: stub}
	ctx := context.Background()

	opts, err := handleIAMInviteFIDO2Begin(ctx, svc, &inviteEnvelope{TenantID: "t1", Token: "tok"})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if m, ok := opts.(map[string]interface{}); !ok || m["publicKey"] != "opts" {
		t.Errorf("begin = %#v, want the service's options", opts)
	}

	credential := map[string]interface{}{"id": "c"}
	resp, err := handleIAMInviteFIDO2Finish(ctx, svc,
		&inviteEnvelope{TenantID: "t1", Token: "tok", Credential: credential})
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if stub.gotCredential["id"] != "c" {
		t.Errorf("credential = %#v, want the request's", stub.gotCredential)
	}
	out := resp.(map[string]interface{})
	if out["user_id"] != "u1" || out["message"] != "activation successful" || out["warning"] != "w" {
		t.Errorf("finish = %#v, want the activation result", out)
	}
}

func TestEmbeddedInvitePIV(t *testing.T) {
	stub := &stubInviteActivation{activation: &service.InviteActivation{UserID: "u1"}}
	svc := &mockQuicBaaSForInvite{activation: stub}

	resp, err := handleIAMInvitePIV(context.Background(), svc,
		&inviteEnvelope{TenantID: "t1", Token: "tok", CertPEM: "PEM"})
	if err != nil {
		t.Fatalf("PIV: %v", err)
	}
	if string(stub.gotCert) != "PEM" {
		t.Errorf("cert = %q, want PEM", stub.gotCert)
	}
	out := resp.(map[string]interface{})
	if out["message"] != "PIV activation successful" {
		t.Errorf("message = %v", out["message"])
	}
	if _, ok := out["warning"]; ok {
		t.Error("an activation without a warning must not carry one")
	}
}

func TestEmbeddedInviteUnconfigured(t *testing.T) {
	for name, svc := range map[string]service.QuicBaaS{
		"NoProvider": &mockQuicBaaSForWebAuthn{},
		"NilService": &mockQuicBaaSForInvite{},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := handleIAMInviteGet(context.Background(), svc,
				&inviteEnvelope{TenantID: "t1", Token: "tok"}); err == nil {
				t.Error("expected an error when invite activation is not configured")
			}
		})
	}
}

// TestEmbeddedInvitePropagatesError keeps a rejected invite a failure on
// every verb rather than an empty success.
func TestEmbeddedInvitePropagatesError(t *testing.T) {
	wantErr := errors.New("invite unusable")
	svc := &mockQuicBaaSForInvite{activation: &stubInviteActivation{err: wantErr}}
	req := &inviteEnvelope{TenantID: "t1", Token: "tok"}

	for name, handler := range map[string]func(context.Context, service.QuicBaaS, interface{}) (interface{}, error){
		"Get":    handleIAMInviteGet,
		"Begin":  handleIAMInviteFIDO2Begin,
		"Finish": handleIAMInviteFIDO2Finish,
		"PIV":    handleIAMInvitePIV,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := handler(context.Background(), svc, req); !errors.Is(err, wantErr) {
				t.Errorf("error = %v, want the service failure", err)
			}
		})
	}
}

func TestIAMRequestMapField(t *testing.T) {
	if m, ok := iamRequestMapField(&inviteEnvelope{Credential: map[string]interface{}{"k": 1}}, "Credential"); !ok || m["k"] != 1 {
		t.Errorf("map field = %v, %v", m, ok)
	}
	for name, req := range map[string]interface{}{
		"NilPointer": (*inviteEnvelope)(nil),
		"NotStruct":  "x",
		"WrongKind":  &inviteEnvelope{},
	} {
		field := "Credential"
		if name == "WrongKind" {
			field = "Token"
		}
		if _, ok := iamRequestMapField(req, field); ok {
			t.Errorf("%s: expected no map field", name)
		}
	}
}
