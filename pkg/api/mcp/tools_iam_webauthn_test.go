//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/logger"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubWebAuthnCredentials records the scope each tool was invoked with.
type stubWebAuthnCredentials struct {
	gotTenant string
	gotUser   uint64
	gotCredID string

	list    []iam.WebAuthnCredentialSummary
	deleted int
	err     error
}

func (s *stubWebAuthnCredentials) ListCredentials(_ context.Context, tenantID string, userID uint64) ([]iam.WebAuthnCredentialSummary, error) {
	s.gotTenant, s.gotUser = tenantID, userID
	return s.list, s.err
}

func (s *stubWebAuthnCredentials) DeleteCredential(_ context.Context, tenantID string, userID uint64, credentialID string) error {
	s.gotTenant, s.gotUser, s.gotCredID = tenantID, userID, credentialID
	return s.err
}

func (s *stubWebAuthnCredentials) DeleteCredentials(_ context.Context, tenantID string, userID uint64) (int, error) {
	s.gotTenant, s.gotUser = tenantID, userID
	return s.deleted, s.err
}

// webauthnQuicBaaSWrapper exposes the credential service through the same
// structural seam the production MCP tools use.
type webauthnQuicBaaSWrapper struct {
	*MockQuicBaaS
	creds service.WebAuthnCredentialService
}

func (m *webauthnQuicBaaSWrapper) WebAuthnCredentials() service.WebAuthnCredentialService {
	return m.creds
}

func newWebAuthnTestRegistry(creds service.WebAuthnCredentialService) *ToolRegistry {
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	return NewToolRegistry(&webauthnQuicBaaSWrapper{MockQuicBaaS: &MockQuicBaaS{}, creds: creds}, log)
}

// TestWebAuthnToolsRegistered is the guard for the defect this change fixes:
// the SDK's MCP transport maps the credential verbs onto these tool names, and
// a tool the server never registers is invisible in tools/list.
func TestWebAuthnToolsRegistered(t *testing.T) {
	reg := newWebAuthnTestRegistry(&stubWebAuthnCredentials{})
	registered := map[string]bool{}
	for _, tool := range reg.ListTools() {
		registered[tool.Name] = true
	}
	for _, name := range []string{
		"quicbaas_iam_webauthn_credentials_list",
		"quicbaas_iam_webauthn_credential_delete",
		"quicbaas_iam_webauthn_credentials_delete_all",
	} {
		if !registered[name] {
			t.Errorf("MCP tool %s is not registered", name)
		}
	}
}

// TestWebAuthnToolsExecute drives each tool through the registry's dispatch and
// asserts the scope reaches the shared credential service.
func TestWebAuthnToolsExecute(t *testing.T) {
	stub := &stubWebAuthnCredentials{
		list:    []iam.WebAuthnCredentialSummary{{ID: "AbCd01", AAGUID: "aabbccdd"}},
		deleted: 2,
	}
	reg := newWebAuthnTestRegistry(stub)
	ctx := context.Background()

	if _, err := reg.executeIAMWebAuthnCredentialsList(ctx,
		map[string]interface{}{"tenant_id": "t1", "user_id": "30001"}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if stub.gotTenant != "t1" || stub.gotUser != 30001 {
		t.Errorf("list called with %s/%d, want t1/30001", stub.gotTenant, stub.gotUser)
	}

	if _, err := reg.executeIAMWebAuthnCredentialDelete(ctx,
		map[string]interface{}{"tenant_id": "t1", "user_id": "30001", "credential_id": "AbCd01"}); err != nil {
		t.Fatalf("delete one: %v", err)
	}
	if stub.gotCredID != "AbCd01" {
		t.Errorf("credential id = %q, want AbCd01", stub.gotCredID)
	}

	if _, err := reg.executeIAMWebAuthnCredentialsDeleteAll(ctx,
		map[string]interface{}{"tenant_id": "t1", "user_id": "30001"}); err != nil {
		t.Fatalf("delete all: %v", err)
	}
}

// TestWebAuthnToolsValidateScope fails closed on a missing scope rather than
// operating on an empty tenant or user.
func TestWebAuthnToolsValidateScope(t *testing.T) {
	reg := newWebAuthnTestRegistry(&stubWebAuthnCredentials{})
	ctx := context.Background()

	if _, err := reg.executeIAMWebAuthnCredentialsList(ctx,
		map[string]interface{}{"user_id": "30001"}); err == nil {
		t.Error("expected an error when tenant_id is missing")
	}
	if _, err := reg.executeIAMWebAuthnCredentialsList(ctx,
		map[string]interface{}{"tenant_id": "t1"}); err == nil {
		t.Error("expected an error when user_id is missing")
	}
	if _, err := reg.executeIAMWebAuthnCredentialDelete(ctx,
		map[string]interface{}{"tenant_id": "t1", "user_id": "30001"}); err == nil {
		t.Error("expected an error when credential_id is missing")
	}
}

// TestWebAuthnToolsUnconfigured keeps a node without WebAuthn honest: the tools
// report unavailable rather than an empty credential list, which an operator
// would read as "nothing is blocking this account".
func TestWebAuthnToolsUnconfigured(t *testing.T) {
	reg := newWebAuthnTestRegistry(nil)

	_, err := reg.executeIAMWebAuthnCredentialsList(context.Background(),
		map[string]interface{}{"tenant_id": "t1", "user_id": "30001"})
	var unavailable *WebAuthnCredentialUnavailableError
	if !errors.As(err, &unavailable) {
		t.Errorf("error = %v, want *WebAuthnCredentialUnavailableError", err)
	}
}
