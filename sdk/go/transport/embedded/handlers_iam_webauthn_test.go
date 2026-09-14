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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubWebAuthnCredentials records the scope each verb was called with and
// returns canned results, so the embedded transport's request extraction and
// response shaping are asserted without a live credential store.
type stubWebAuthnCredentials struct {
	gotTenant string
	gotUser   string
	gotCredID string

	list    []iam.WebAuthnCredentialSummary
	deleted int
	err     error
}

func (s *stubWebAuthnCredentials) ListCredentials(_ context.Context, tenantID, userID string) ([]iam.WebAuthnCredentialSummary, error) {
	s.gotTenant, s.gotUser = tenantID, userID
	return s.list, s.err
}

func (s *stubWebAuthnCredentials) DeleteCredential(_ context.Context, tenantID, userID, credentialID string) error {
	s.gotTenant, s.gotUser, s.gotCredID = tenantID, userID, credentialID
	return s.err
}

func (s *stubWebAuthnCredentials) DeleteCredentials(_ context.Context, tenantID, userID string) (int, error) {
	s.gotTenant, s.gotUser = tenantID, userID
	return s.deleted, s.err
}

// mockQuicBaaSForWebAuthn exposes the credential service through the same
// structural seam the production embedded transport uses.
type mockQuicBaaSForWebAuthn struct {
	service.QuicBaaS
	creds service.WebAuthnCredentialService
}

func (m *mockQuicBaaSForWebAuthn) WebAuthnCredentials() service.WebAuthnCredentialService {
	return m.creds
}

// webAuthnListEnvelope mirrors the SDK client's request struct; the handler
// extracts the fields by name via reflection.
type webAuthnListEnvelope struct {
	TenantID string
	UserID   string
}

// webAuthnDeleteEnvelope mirrors the SDK client's single-delete request.
type webAuthnDeleteEnvelope struct {
	TenantID     string
	UserID       string
	CredentialID string
}

// TestEmbeddedWebAuthnDispatchRegistered is the guard for the defect this
// change fixes: the SDK client dispatches these verbs on quicbaas.IAM.WebAuthn,
// and before they were registered here the embedded transport answered
// "unknown method" while REST worked.
func TestEmbeddedWebAuthnDispatchRegistered(t *testing.T) {
	for _, key := range []string{
		"quicbaas.IAM.WebAuthn/ListWebAuthnCredentials",
		"quicbaas.IAM.WebAuthn/DeleteWebAuthnCredential",
		"quicbaas.IAM.WebAuthn/DeleteWebAuthnCredentials",
	} {
		if _, ok := unaryHandlers[key]; !ok {
			t.Errorf("embedded transport does not dispatch %s", key)
		}
	}
}

// TestEmbeddedWebAuthnList asserts the scope reaches the service and the
// response carries the {"credentials", "total"} shape the SDK client decodes —
// the same contract REST and gRPC produce.
func TestEmbeddedWebAuthnList(t *testing.T) {
	stub := &stubWebAuthnCredentials{list: []iam.WebAuthnCredentialSummary{{
		ID: "AbCd01", AAGUID: "aabbccdd", AttestationType: "packed",
		Transports: []string{"usb"}, SignCount: 7, BackupEligible: true,
	}}}
	svc := &mockQuicBaaSForWebAuthn{creds: stub}

	resp, err := handleIAMWebAuthnCredentialsList(context.Background(), svc,
		&webAuthnListEnvelope{TenantID: "t1", UserID: "u1"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if stub.gotTenant != "t1" || stub.gotUser != "u1" {
		t.Errorf("service called with %s/%s, want t1/u1", stub.gotTenant, stub.gotUser)
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
	if credentials[0]["id"] != "AbCd01" || credentials[0]["aaguid"] != "aabbccdd" {
		t.Errorf("credential = %#v, want the stub's identity fields", credentials[0])
	}
	if _, leaked := credentials[0]["public_key"]; leaked {
		t.Error("the embedded projection must not carry key material")
	}
}

// TestEmbeddedWebAuthnDelete drives both delete verbs and asserts the
// credential id and the deleted count survive the round trip.
func TestEmbeddedWebAuthnDelete(t *testing.T) {
	stub := &stubWebAuthnCredentials{deleted: 2}
	svc := &mockQuicBaaSForWebAuthn{creds: stub}
	ctx := context.Background()

	if _, err := handleIAMWebAuthnCredentialDelete(ctx, svc,
		&webAuthnDeleteEnvelope{TenantID: "t1", UserID: "u1", CredentialID: "AbCd01"}); err != nil {
		t.Fatalf("delete one: %v", err)
	}
	if stub.gotCredID != "AbCd01" {
		t.Errorf("credential id = %q, want AbCd01", stub.gotCredID)
	}

	resp, err := handleIAMWebAuthnCredentialsDeleteAll(ctx, svc,
		&webAuthnListEnvelope{TenantID: "t1", UserID: "u1"})
	if err != nil {
		t.Fatalf("delete all: %v", err)
	}
	out, ok := resp.(map[string]interface{})
	if !ok || out["deleted"] != 2 {
		t.Errorf("deleted = %#v, want 2", resp)
	}
}

// TestEmbeddedWebAuthnUnconfigured keeps a node without WebAuthn honest: the
// verbs report not-configured rather than reporting an empty credential list,
// which an operator would read as "nothing is blocking this account".
func TestEmbeddedWebAuthnUnconfigured(t *testing.T) {
	svc := &mockQuicBaaSForWebAuthn{creds: nil}

	if _, err := handleIAMWebAuthnCredentialsList(context.Background(), svc,
		&webAuthnListEnvelope{TenantID: "t1", UserID: "u1"}); err == nil {
		t.Error("expected an error when WebAuthn is not configured")
	}
}

// TestEmbeddedWebAuthnPropagatesError keeps a store failure a failure rather
// than an empty result.
func TestEmbeddedWebAuthnPropagatesError(t *testing.T) {
	wantErr := errors.New("credential store unreachable")
	svc := &mockQuicBaaSForWebAuthn{creds: &stubWebAuthnCredentials{err: wantErr}}

	_, err := handleIAMWebAuthnCredentialsList(context.Background(), svc,
		&webAuthnListEnvelope{TenantID: "t1", UserID: "u1"})
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want the store failure", err)
	}
}
