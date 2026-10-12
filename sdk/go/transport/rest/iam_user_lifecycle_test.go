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

package rest

import "testing"

// TestMapUserCreateRequest_ForwardsLifecycleAndAuthMethods.
//
// Dropping lifecycle here does not merely lose a field. The server treats an
// omitted lifecycle as `pending` and mints an activation invite, so a caller
// asking for `disabled` over REST would get a pending account with a LIVE
// invite, while the same SDK call over gRPC or embedded created a disabled one.
// Account state cannot depend on which wire carried the call.
func TestMapUserCreateRequest_ForwardsLifecycleAndAuthMethods(t *testing.T) {
	body := mapUserCreateRequest(map[string]interface{}{
		"email":        "alice@acme.test",
		"username":     "alice",
		"display_name": "Alice",
		"lifecycle":    "disabled",
		"auth_methods": []interface{}{"webauthn", "piv"},
	})
	if body["lifecycle"] != "disabled" {
		t.Errorf("lifecycle = %v, want it forwarded so the account is not created pending", body["lifecycle"])
	}
	methods, ok := body["auth_methods"].([]interface{})
	if !ok || len(methods) != 2 {
		t.Errorf("auth_methods = %v, want both forwarded", body["auth_methods"])
	}
}

// An omitted lifecycle stays omitted, so the server applies its own default
// (pending, with an invite) rather than receiving an empty string to interpret.
func TestMapUserCreateRequest_OmitsAnUnsetLifecycle(t *testing.T) {
	body := mapUserCreateRequest(map[string]interface{}{
		"email":    "alice@acme.test",
		"username": "alice",
	})
	if _, present := body["lifecycle"]; present {
		t.Errorf("lifecycle must be absent when unset, got %v", body["lifecycle"])
	}
	if _, present := body["auth_methods"]; present {
		t.Errorf("auth_methods must be absent when unset, got %v", body["auth_methods"])
	}
}

// TestMapUserCreateRequest_ForwardsInviteExpiry: a caller's invite lifetime
// must reach the server, or REST alone would issue the default.
func TestMapUserCreateRequest_ForwardsInviteExpiry(t *testing.T) {
	body := mapUserCreateRequest(map[string]interface{}{
		"email":               "alice@acme.test",
		"invite_expiry_hours": float64(48),
	})
	if body["invite_expiry_hours"] != 48 {
		t.Errorf("invite_expiry_hours = %v, want 48", body["invite_expiry_hours"])
	}

	unset := mapUserCreateRequest(map[string]interface{}{"email": "alice@acme.test"})
	if _, present := unset["invite_expiry_hours"]; present {
		t.Errorf("invite_expiry_hours must be absent when unset, got %v", unset["invite_expiry_hours"])
	}
}

// TestMapUserUpdateRequest_ForwardsLifecycleAndAuthMethods: an update that
// silently drops an account-state change answers 200 with nothing changed.
func TestMapUserUpdateRequest_ForwardsLifecycleAndAuthMethods(t *testing.T) {
	body := mapUserUpdateRequest(map[string]interface{}{
		"display_name": "Alice",
		"lifecycle":    "suspended",
		"auth_methods": []interface{}{"piv"},
	})
	if body["lifecycle"] != "suspended" {
		t.Errorf("lifecycle = %v, want it forwarded", body["lifecycle"])
	}
	if methods, ok := body["auth_methods"].([]interface{}); !ok || len(methods) != 1 {
		t.Errorf("auth_methods = %v, want it forwarded", body["auth_methods"])
	}

	empty := mapUserUpdateRequest(map[string]interface{}{"display_name": "Alice"})
	if _, present := empty["lifecycle"]; present {
		t.Errorf("lifecycle must be absent when unset, got %v", empty["lifecycle"])
	}
}
