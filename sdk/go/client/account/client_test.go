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

package account

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport records the last call and returns a scripted response.
type mockTransport struct {
	service string
	method  string
	request interface{}

	response interface{}
	err      error
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	m.service, m.method, m.request = service, method, req
	return m.response, m.err
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, nil
}

func (m *mockTransport) Conn() interface{} { return nil }
func (m *mockTransport) Close() error      { return nil }
func (m *mockTransport) Healthy() bool     { return true }

func sampleInfo() *AccountInfo {
	return &AccountInfo{ID: "acct-1", TenantID: "tenant-1", Provider: ProviderClaude, Status: StatusActive}
}

func sampleCredential() *Credential {
	return &Credential{AccessToken: "SECRET-ACCESS-TOKEN", RefreshToken: "SECRET-REFRESH-TOKEN"}
}

// =============================================================================
// Construction
// =============================================================================

func TestNewClient(t *testing.T) {
	if NewClient(&mockTransport{}) == nil {
		t.Error("expected a non-nil client")
	}
	if NewClient(nil) != nil {
		t.Error("expected a nil client for a nil transport")
	}
}

// =============================================================================
// Happy paths — every verb reaches the transport with the right method name
// =============================================================================

func TestClient_VerbsDispatchToTransport(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		response interface{}
		call     func(c Client) error
	}{
		{
			name:     "Create",
			method:   "Create",
			response: sampleInfo(),
			call: func(c Client) error {
				_, err := c.Create(context.Background(), &CreateAccountRequest{
					TenantID: "tenant-1", Provider: ProviderClaude, Credential: sampleCredential(),
				})
				return err
			},
		},
		{
			name:     "Get",
			method:   "Get",
			response: sampleInfo(),
			call: func(c Client) error {
				_, err := c.Get(context.Background(), "tenant-1", "acct-1")
				return err
			},
		},
		{
			name:     "List",
			method:   "List",
			response: &ListAccountsResponse{Accounts: []*AccountInfo{sampleInfo()}, Total: 1},
			call: func(c Client) error {
				_, err := c.List(context.Background(), &ListAccountsRequest{TenantID: "tenant-1"})
				return err
			},
		},
		{
			name:     "Update",
			method:   "Update",
			response: sampleInfo(),
			call: func(c Client) error {
				_, err := c.Update(context.Background(), &UpdateAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"})
				return err
			},
		},
		{
			name:     "Delete",
			method:   "Delete",
			response: &DeleteAccountResponse{AccountID: "acct-1", Deleted: true},
			call: func(c Client) error {
				_, err := c.Delete(context.Background(), "tenant-1", "acct-1")
				return err
			},
		},
		{
			name:     "RotateCredential",
			method:   "RotateCredential",
			response: &AccountAckResponse{AccountID: "acct-1"},
			call: func(c Client) error {
				_, err := c.RotateCredential(context.Background(), &RotateCredentialRequest{
					TenantID: "tenant-1", AccountID: "acct-1", Credential: sampleCredential(),
				})
				return err
			},
		},
		{
			name:     "Revoke",
			method:   "Revoke",
			response: sampleInfo(),
			call: func(c Client) error {
				_, err := c.Revoke(context.Background(), &RevokeAccountRequest{TenantID: "tenant-1", AccountID: "acct-1"})
				return err
			},
		},
		{
			name:     "SetDefault",
			method:   "SetDefault",
			response: &AccountAckResponse{AccountID: "acct-1"},
			call: func(c Client) error {
				_, err := c.SetDefault(context.Background(), "tenant-1", "acct-1")
				return err
			},
		},
		{
			name:     "Usage",
			method:   "Usage",
			response: &UsageReport{TotalTokens: 42},
			call: func(c Client) error {
				_, err := c.Usage(context.Background(), "tenant-1", "acct-1")
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mt := &mockTransport{response: tc.response}
			if err := tc.call(NewClient(mt)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mt.service != serviceName {
				t.Errorf("service = %q, want %q", mt.service, serviceName)
			}
			if mt.method != tc.method {
				t.Errorf("method = %q, want %q", mt.method, tc.method)
			}
		})
	}
}

func TestClient_ListEmptyResponseIsNotAnError(t *testing.T) {
	c := NewClient(&mockTransport{response: nil})
	resp, err := c.List(context.Background(), &ListAccountsRequest{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || len(resp.Accounts) != 0 {
		t.Fatalf("expected an empty list, got %+v", resp)
	}
}

func TestClient_DeleteNilResponseIsSuccess(t *testing.T) {
	c := NewClient(&mockTransport{response: nil})
	resp, err := c.Delete(context.Background(), "tenant-1", "acct-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || !resp.Deleted {
		t.Fatalf("expected a deleted acknowledgement, got %+v", resp)
	}
}

// =============================================================================
// Invalid input
// =============================================================================

func TestClient_InvalidInput(t *testing.T) {
	c := NewClient(&mockTransport{response: sampleInfo()})

	cases := []struct {
		name string
		call func() error
	}{
		{"create nil request", func() error { _, err := c.Create(context.Background(), nil); return err }},
		{"create no tenant", func() error {
			_, err := c.Create(context.Background(), &CreateAccountRequest{Provider: ProviderClaude, Credential: sampleCredential()})
			return err
		}},
		{"create no provider", func() error {
			_, err := c.Create(context.Background(), &CreateAccountRequest{TenantID: "t", Credential: sampleCredential()})
			return err
		}},
		{"create no credential and no secret ref", func() error {
			_, err := c.Create(context.Background(), &CreateAccountRequest{TenantID: "t", Provider: ProviderClaude})
			return err
		}},
		{"get no tenant", func() error { _, err := c.Get(context.Background(), "", "acct-1"); return err }},
		{"get no account", func() error { _, err := c.Get(context.Background(), "tenant-1", ""); return err }},
		{"list nil request", func() error { _, err := c.List(context.Background(), nil); return err }},
		{"list no tenant", func() error { _, err := c.List(context.Background(), &ListAccountsRequest{}); return err }},
		{"update nil request", func() error { _, err := c.Update(context.Background(), nil); return err }},
		{"update no account", func() error {
			_, err := c.Update(context.Background(), &UpdateAccountRequest{TenantID: "t"})
			return err
		}},
		{"delete no account", func() error { _, err := c.Delete(context.Background(), "tenant-1", ""); return err }},
		{"rotate nil request", func() error { _, err := c.RotateCredential(context.Background(), nil); return err }},
		{"rotate no credential", func() error {
			_, err := c.RotateCredential(context.Background(), &RotateCredentialRequest{TenantID: "t", AccountID: "a"})
			return err
		}},
		{"revoke nil request", func() error { _, err := c.Revoke(context.Background(), nil); return err }},
		{"revoke no account", func() error {
			_, err := c.Revoke(context.Background(), &RevokeAccountRequest{TenantID: "t"})
			return err
		}},
		{"set-default no account", func() error { _, err := c.SetDefault(context.Background(), "tenant-1", ""); return err }},
		{"usage no tenant", func() error { _, err := c.Usage(context.Background(), "", "acct-1"); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Fatal("expected an invalid-input error")
			}
		})
	}
}

// TestClient_NilContextRejected pins the nil-context guard every SDK client
// applies; a nil context would otherwise panic deep in the transport.
func TestClient_NilContextRejected(t *testing.T) {
	c := NewClient(&mockTransport{response: sampleInfo()})
	//nolint:staticcheck // deliberately passing a nil context
	if _, err := c.Get(nil, "tenant-1", "acct-1"); err == nil {
		t.Fatal("expected an error for a nil context")
	}
}

// TestClient_WrongResponseTypeIsAnError proves a transport that returns the
// wrong shape surfaces an error instead of a nil-typed value the caller would
// dereference.
func TestClient_WrongResponseTypeIsAnError(t *testing.T) {
	c := NewClient(&mockTransport{response: "not-an-account"})
	if _, err := c.Get(context.Background(), "tenant-1", "acct-1"); err == nil {
		t.Fatal("expected an internal error for a mistyped response")
	}
	if _, err := c.List(context.Background(), &ListAccountsRequest{TenantID: "t"}); err == nil {
		t.Fatal("expected an internal error for a mistyped list response")
	}
	if _, err := c.SetDefault(context.Background(), "tenant-1", "acct-1"); err == nil {
		t.Fatal("expected an internal error for a mistyped ack response")
	}
	if _, err := c.Usage(context.Background(), "tenant-1", "acct-1"); err == nil {
		t.Fatal("expected an internal error for a mistyped usage response")
	}
	if _, err := c.Delete(context.Background(), "tenant-1", "acct-1"); err == nil {
		t.Fatal("expected an internal error for a mistyped delete response")
	}
}

// TestClient_TransportErrorsPropagate proves a transport failure surfaces
// unchanged rather than being swallowed into a nil result.
func TestClient_TransportErrorsPropagate(t *testing.T) {
	boom := errors.NewInternal("test", "boom")
	c := NewClient(&mockTransport{err: boom})

	calls := map[string]func() error{
		"Create": func() error {
			_, err := c.Create(context.Background(), &CreateAccountRequest{
				TenantID: "t", Provider: ProviderClaude, Credential: sampleCredential(),
			})
			return err
		},
		"Get":  func() error { _, err := c.Get(context.Background(), "t", "a"); return err },
		"List": func() error { _, err := c.List(context.Background(), &ListAccountsRequest{TenantID: "t"}); return err },
		"Update": func() error {
			_, err := c.Update(context.Background(), &UpdateAccountRequest{TenantID: "t", AccountID: "a"})
			return err
		},
		"Delete": func() error { _, err := c.Delete(context.Background(), "t", "a"); return err },
		"RotateCredential": func() error {
			_, err := c.RotateCredential(context.Background(), &RotateCredentialRequest{
				TenantID: "t", AccountID: "a", Credential: sampleCredential(),
			})
			return err
		},
		"Revoke": func() error {
			_, err := c.Revoke(context.Background(), &RevokeAccountRequest{TenantID: "t", AccountID: "a"})
			return err
		},
		"SetDefault": func() error { _, err := c.SetDefault(context.Background(), "t", "a"); return err },
		"Usage":      func() error { _, err := c.Usage(context.Background(), "t", "a"); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s swallowed the transport error", name)
		}
	}
}

// TestClient_NilResponsesAreErrors covers the "server answered nothing" paths
// for the verbs that require a body.
func TestClient_NilResponsesAreErrors(t *testing.T) {
	c := NewClient(&mockTransport{response: nil})
	if _, err := c.Get(context.Background(), "t", "a"); err == nil {
		t.Error("Get accepted an empty response")
	}
	if _, err := c.SetDefault(context.Background(), "t", "a"); err == nil {
		t.Error("SetDefault accepted an empty response")
	}
	if _, err := c.Usage(context.Background(), "t", "a"); err == nil {
		t.Error("Usage accepted an empty response")
	}
}

// TestClient_NilContextRejectedOnEveryVerb pins the nil-context guard across
// the whole surface, not just Get.
func TestClient_NilContextRejectedOnEveryVerb(t *testing.T) {
	c := NewClient(&mockTransport{response: sampleInfo()})
	//nolint:staticcheck // deliberately passing a nil context
	var nilCtx context.Context

	calls := map[string]func() error{
		"Create": func() error { _, err := c.Create(nilCtx, &CreateAccountRequest{}); return err },
		"List":   func() error { _, err := c.List(nilCtx, &ListAccountsRequest{}); return err },
		"Update": func() error { _, err := c.Update(nilCtx, &UpdateAccountRequest{}); return err },
		"Delete": func() error { _, err := c.Delete(nilCtx, "t", "a"); return err },
		"RotateCredential": func() error {
			_, err := c.RotateCredential(nilCtx, &RotateCredentialRequest{})
			return err
		},
		"Revoke":     func() error { _, err := c.Revoke(nilCtx, &RevokeAccountRequest{}); return err },
		"SetDefault": func() error { _, err := c.SetDefault(nilCtx, "t", "a"); return err },
		"Usage":      func() error { _, err := c.Usage(nilCtx, "t", "a"); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil {
			t.Errorf("%s accepted a nil context", name)
		}
	}
}

// =============================================================================
// Credential leak guards
// =============================================================================

// TestResponseTypesHaveNoCredentialField is the structural guard: no response
// type may be, or transitively contain, the request-only Credential shape. A
// refactor that adds one breaks here even if no transport populates it.
func TestResponseTypesHaveNoCredentialField(t *testing.T) {
	credentialType := reflect.TypeOf(Credential{})
	responseTypes := []reflect.Type{
		reflect.TypeOf(AccountInfo{}),
		reflect.TypeOf(ListAccountsResponse{}),
		reflect.TypeOf(DeleteAccountResponse{}),
		reflect.TypeOf(AccountAckResponse{}),
		reflect.TypeOf(UsageReport{}),
	}
	for _, rt := range responseTypes {
		if reachesType(rt, credentialType, make(map[reflect.Type]bool)) {
			t.Errorf("response type %s reaches the credential shape %s", rt, credentialType)
		}
	}
}

// reachesType reports whether rt is, or transitively contains, target.
func reachesType(rt, target reflect.Type, seen map[reflect.Type]bool) bool {
	for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		rt = rt.Elem()
	}
	if rt == target {
		return true
	}
	if rt.Kind() != reflect.Struct || seen[rt] {
		return false
	}
	seen[rt] = true
	for i := 0; i < rt.NumField(); i++ {
		if reachesType(rt.Field(i).Type, target, seen) {
			return true
		}
	}
	return false
}

// TestAccountInfoSerializesNoCredential is the behavioral guard: an
// AccountInfo built from a server response must serialize without any
// credential-shaped key, so a caller logging it cannot leak one.
func TestAccountInfoSerializesNoCredential(t *testing.T) {
	encoded, err := json.Marshal(sampleInfo())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoCredentialKeys(t, encoded)
}

// TestClient_CreateBySecretRef proves a create that names a tenant secret
// instead of an inline credential passes client validation and reaches the
// transport with the reference.
func TestClient_CreateBySecretRef(t *testing.T) {
	mt := &mockTransport{response: sampleInfo()}
	_, err := NewClient(mt).Create(context.Background(), &CreateAccountRequest{
		TenantID: "tenant-1", Provider: ProviderClaude, CredentialSecretRef: "agents/claude/claude1/oauth_token",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	req, ok := mt.request.(*CreateAccountRequest)
	if !ok || req.CredentialSecretRef != "agents/claude/claude1/oauth_token" || req.Credential != nil {
		t.Fatalf("transport request = %#v, want the reference and no inline credential", mt.request)
	}
}

// TestAccountInfoWithSecretRefSerializesOnlyTheName proves a reference-sourced
// account reports the secret's NAME and nothing credential-shaped besides.
func TestAccountInfoWithSecretRefSerializesOnlyTheName(t *testing.T) {
	info := sampleInfo()
	info.CredentialSecretRef = "agents/codex/reviewer/auth_json"
	info.CredentialSecretFile = ".codex/auth.json"
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoCredentialKeys(t, encoded)
	if !strings.Contains(string(encoded), `"credential_secret_ref":"agents/codex/reviewer/auth_json"`) {
		t.Errorf("reference name missing from %s", encoded)
	}
}

// nonSecretCredentialKeys are the metadata field names that contain the word
// "credential" without carrying any. Two are derived from a timestamp mirrored
// onto the account's non-secret record — an expiry is not credential material —
// and two NAME the tenant secret a reference-sourced account reads (a secret's
// name is not its value). The guards below match on JSON KEYS with these
// excluded, rather than scanning the whole document for the substring and
// tripping over its own vocabulary.
var nonSecretCredentialKeys = map[string]bool{
	"credential_expires_at":  true,
	"credential_health":      true,
	"credential_secret_ref":  true,
	"credential_secret_file": true,
}

// assertNoCredentialKeys fails when encoded carries a key that looks like
// credential material.
func assertNoCredentialKeys(t *testing.T, encoded []byte) {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for key := range decoded {
		if nonSecretCredentialKeys[key] {
			continue
		}
		for _, marker := range []string{"credential", "access_token", "refresh_token", "env", "files", "secret", "token"} {
			if strings.Contains(key, marker) {
				t.Errorf("serialized a credential-shaped key %q: %s", key, encoded)
			}
		}
	}
}

// =============================================================================
// Materialize — the one verb whose response carries secret material
// =============================================================================

// TestClient_Materialize_DispatchesAndDecodes covers the happy path: the verb
// reaches the transport under its own method name and hands back the released
// envelope unchanged.
func TestClient_Materialize_DispatchesAndDecodes(t *testing.T) {
	tr := &mockTransport{response: &MaterializedCredential{
		AccountID: "acct-1",
		Provider:  ProviderClaude,
		Credential: &ReleasedCredential{
			AccessToken:       "RELEASED",
			AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		},
	}}
	c := NewClient(tr)

	out, err := c.Materialize(context.Background(), &MaterializeCredentialRequest{
		TenantID: "tenant-1", AccountID: "acct-1", ProjectID: "project-x",
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if tr.method != "Materialize" {
		t.Errorf("dispatched method %q, want Materialize", tr.method)
	}
	if out.Credential == nil || out.Credential.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Error("released envelope lost the material a spawn needs")
	}
	sent, ok := tr.request.(*MaterializeCredentialRequest)
	if !ok {
		t.Fatalf("transport received %T, want *MaterializeCredentialRequest", tr.request)
	}
	if sent.ProjectID != "project-x" {
		t.Errorf("transport received project %q, want project-x", sent.ProjectID)
	}
}

// TestClient_Materialize_RejectsBadInput covers the validation path for each
// way a caller can under-specify the request, plus the two protocol faults a
// server can produce.
func TestClient_Materialize_RejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		ctx      context.Context
		req      *MaterializeCredentialRequest
		response interface{}
	}{
		{name: "nil context", ctx: nil, req: &MaterializeCredentialRequest{TenantID: "t", AccountID: "a"}},
		{name: "nil request", ctx: context.Background(), req: nil},
		{name: "no tenant", ctx: context.Background(), req: &MaterializeCredentialRequest{AccountID: "a"}},
		{name: "no account", ctx: context.Background(), req: &MaterializeCredentialRequest{TenantID: "t"}},
		{
			name: "server returned nothing",
			ctx:  context.Background(),
			req:  &MaterializeCredentialRequest{TenantID: "t", AccountID: "a"},
		},
		{
			name:     "server returned the wrong shape",
			ctx:      context.Background(),
			req:      &MaterializeCredentialRequest{TenantID: "t", AccountID: "a"},
			response: sampleInfo(),
		},
		{
			name:     "server released no envelope",
			ctx:      context.Background(),
			req:      &MaterializeCredentialRequest{TenantID: "t", AccountID: "a"},
			response: &MaterializedCredential{AccountID: "acct-1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(&mockTransport{response: tc.response})
			//nolint:staticcheck // SA1012: passing a nil context is the case under test.
			if _, err := c.Materialize(tc.ctx, tc.req); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// TestMaterializedCredential_SerializesNoRefreshGrant is the leak guard for the
// release shape: a daemon receives the short-lived half of a credential and
// nothing that would let it renew on its own.
func TestMaterializedCredential_SerializesNoRefreshGrant(t *testing.T) {
	encoded, err := json.Marshal(&MaterializedCredential{
		AccountID: "acct-1",
		Credential: &ReleasedCredential{
			AccessToken:       "RELEASED",
			AccessTokenEnvKey: "K",
			Scopes:            []string{"a"},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"refresh_token", "token_endpoint", "client_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("released shape carries %q", forbidden)
		}
	}
}

// TestReleasedCredentialIsNotTheRequestCredential proves the release shape is a
// DISTINCT type from the request-only Credential, which is what keeps
// TestResponseTypesHaveNoCredentialField meaning what it says.
func TestReleasedCredentialIsNotTheRequestCredential(t *testing.T) {
	if reflect.TypeOf(ReleasedCredential{}) == reflect.TypeOf(Credential{}) {
		t.Fatal("the release shape is an alias of the request-only credential shape")
	}
	if reachesType(reflect.TypeOf(MaterializedCredential{}), reflect.TypeOf(Credential{}), make(map[reflect.Type]bool)) {
		t.Fatal("the release response reaches the request-only credential shape")
	}
}
