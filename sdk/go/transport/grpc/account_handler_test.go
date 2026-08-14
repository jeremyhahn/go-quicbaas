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
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestIsAccountServiceGRPC pins the routing predicate. The gRPC transport's
// dispatch chain is prefix-based, so a predicate that also matched
// "quicbaas.Auth" would hijack the auth surface.
func TestIsAccountServiceGRPC(t *testing.T) {
	if !isAccountService("quicbaas.Account") {
		t.Error(`isAccountService("quicbaas.Account") = false`)
	}
	for _, name := range []string{"quicbaas.Auth", "quicbaas.Orchestrator", "quicbaas.KV"} {
		if isAccountService(name) {
			t.Errorf("isAccountService(%q) = true", name)
		}
		if isAuthService(name) && isAccountService(name) {
			t.Errorf("%q matches both the auth and account predicates", name)
		}
	}
	// The reverse hijack matters too: Account must not be claimed by Auth.
	if isAuthService("quicbaas.Account") {
		t.Error("the auth predicate claims quicbaas.Account")
	}
}

// TestAccountMethodMapIsComplete pins the dispatch table against the SDK
// client's method set. A missing entry answers "unknown account method"
// instead of reaching the server.
func TestAccountMethodMapIsComplete(t *testing.T) {
	for _, method := range []string{
		"Create", "Get", "List", "Update", "Delete",
		"RotateCredential", "Revoke", "SetDefault", "Usage",
	} {
		if _, ok := accountMethodMap[method]; !ok {
			t.Errorf("account method %q has no gRPC handler", method)
		}
	}
	if len(accountMethodMap) != 9 {
		t.Errorf("dispatch table has %d entries, want 9", len(accountMethodMap))
	}
}

// TestAccountHandlerRejectsUnknownMethod covers the dispatcher's error path.
func TestAccountHandlerRejectsUnknownMethod(t *testing.T) {
	h := newAccountHandler(nil)
	if _, err := h.handleRequest(context.Background(), "quicbaas.Account", "Teleport", nil); err == nil {
		t.Fatal("expected an error for an unknown account method")
	}
}

// TestAccountHandlersRejectWrongRequestType proves each handler rejects a
// wrong-typed request before it touches the (nil) connection, so a type
// mismatch is a clean error rather than a nil-pointer panic.
func TestAccountHandlersRejectWrongRequestType(t *testing.T) {
	for method, handler := range accountMethodMap {
		if _, err := handler(context.Background(), nil, "not-a-request"); err == nil {
			t.Errorf("%s accepted a wrong-typed request", method)
		}
	}
}

// =============================================================================
// Converters
// =============================================================================

func TestAccountCredentialToProto(t *testing.T) {
	if accountCredentialToProto(nil) != nil {
		t.Fatal("nil credential must convert to nil")
	}
	expires := time.Now().Add(time.Hour).UTC()
	out := accountCredentialToProto(&account.Credential{
		AccessToken:       "SECRET-ACCESS-TOKEN",
		RefreshToken:      "SECRET-REFRESH-TOKEN",
		TokenEndpoint:     "https://auth.example.test/token",
		ClientID:          "client-abc",
		Scopes:            []string{"a", "b"},
		AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
		Env:               map[string]string{"K": "V"},
		Files:             map[string][]byte{"f": []byte("bytes")},
		ExpiresAt:         expires,
	})
	if out.AccessToken != "SECRET-ACCESS-TOKEN" || out.RefreshToken != "SECRET-REFRESH-TOKEN" ||
		out.ClientId != "client-abc" || out.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" ||
		len(out.Scopes) != 2 || out.Env["K"] != "V" || string(out.Files["f"]) != "bytes" {
		t.Fatalf("credential fields lost in conversion: %+v", out)
	}
	if out.ExpiresAtUnix != expires.Unix() {
		t.Errorf("expires_at = %d, want %d", out.ExpiresAtUnix, expires.Unix())
	}

	// A zero expiry must stay 0 rather than becoming a negative epoch offset.
	zero := accountCredentialToProto(&account.Credential{AccessToken: "t"})
	if zero.ExpiresAtUnix != 0 {
		t.Errorf("zero expiry serialized as %d, want 0", zero.ExpiresAtUnix)
	}
}

func TestAccountFromProto(t *testing.T) {
	if accountFromProto(nil) != nil {
		t.Fatal("nil account must convert to nil")
	}
	created := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	out := accountFromProto(&pb.Account{
		Id: "acct-1", TenantId: "tenant-1", ProjectId: "proj-1",
		Provider: "claude", DisplayName: "alice-work", IsDefault: true,
		Status: "active", RefreshPolicy: "oauth2",
		CreatedAtUnix: created.Unix(), LastError: "boom",
	})
	if out.ID != "acct-1" || out.Provider != account.ProviderClaude ||
		out.Status != account.StatusActive || out.RefreshPolicy != account.RefreshOAuth2 ||
		!out.Default || out.LastError != "boom" {
		t.Fatalf("account fields lost in conversion: %+v", out)
	}
	if !out.CreatedAt.Equal(created) {
		t.Errorf("created_at = %s, want %s", out.CreatedAt, created)
	}
	// Unset timestamps must stay zero, not 1970-01-01.
	if !out.UpdatedAt.IsZero() || !out.LastUsedAt.IsZero() || !out.LastRefreshedAt.IsZero() {
		t.Errorf("unset timestamps became epoch values: %+v", out)
	}
}

// =============================================================================
// Credential leak guard
// =============================================================================

// TestAccountGRPCSDKTypesCarryNoCredential is the structural guard on the
// client side: no SDK type the account handlers RETURN may reach the
// request-only credential shape.
func TestAccountGRPCSDKTypesCarryNoCredential(t *testing.T) {
	credentialType := reflect.TypeOf(account.Credential{})
	for _, rt := range []reflect.Type{
		reflect.TypeOf(account.AccountInfo{}),
		reflect.TypeOf(account.ListAccountsResponse{}),
		reflect.TypeOf(account.DeleteAccountResponse{}),
		reflect.TypeOf(account.AccountAckResponse{}),
		reflect.TypeOf(account.UsageReport{}),
	} {
		if grpcReachesType(rt, credentialType, make(map[reflect.Type]bool)) {
			t.Errorf("returned type %s reaches the credential shape", rt)
		}
	}

	// And behaviorally: a proto account carrying hostile extra data still
	// converts to an AccountInfo that serializes no credential keys.
	converted := accountFromProto(&pb.Account{Id: "acct-1", LastError: "SECRET-ACCESS-TOKEN"})
	converted.LastError = "" // LastError is operator text, not credential material
	encoded, err := json.Marshal(converted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertNoCredentialKeys(t, encoded)
}

// nonSecretCredentialKeys are the two metadata field names that contain the
// word "credential" without carrying any. Both are derived from a timestamp
// mirrored onto the account's non-secret record — an expiry is not credential
// material — so the guards below match on JSON KEYS with these two excluded,
// rather than scanning the whole document for the substring and tripping over
// its own vocabulary.
var nonSecretCredentialKeys = map[string]bool{
	"credential_expires_at": true,
	"credential_health":     true,
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

// grpcReachesType reports whether rt is, or transitively contains, target.
func grpcReachesType(rt, target reflect.Type, seen map[reflect.Type]bool) bool {
	for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice ||
		rt.Kind() == reflect.Array || rt.Kind() == reflect.Map {
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
		if grpcReachesType(rt.Field(i).Type, target, seen) {
			return true
		}
	}
	return false
}
