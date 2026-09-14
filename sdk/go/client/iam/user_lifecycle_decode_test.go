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

package iam

import "testing"

// TestConvertToUser_DecodesLifecycleAndAuthMethods.
//
// Every map-backed path — Create, Get, Update, List — runs through this helper.
// While it did not read these fields the SDK returned a zero-value lifecycle
// even when the server sent one, so a caller could not tell a pending account
// from an active one, and a read-modify-write flow decided on state it never
// actually received.
func TestConvertToUser_DecodesLifecycleAndAuthMethods(t *testing.T) {
	user, ok := convertToUser(map[string]interface{}{
		"id":           "1001",
		"tenant_id":    "acme",
		"email":        "alice@acme.test",
		"name":         "Alice",
		"lifecycle":    "pending",
		"auth_methods": []interface{}{"webauthn", "piv"},
	})
	if !ok {
		t.Fatal("convertToUser did not decode the response")
	}
	if user.Lifecycle != "pending" {
		t.Errorf("Lifecycle = %q, want the value the server sent", user.Lifecycle)
	}
	if len(user.AuthMethods) != 2 || user.AuthMethods[0] != "webauthn" || user.AuthMethods[1] != "piv" {
		t.Errorf("AuthMethods = %v, want [webauthn piv]", user.AuthMethods)
	}
}

// TestConvertToUser_ToleratesAbsentAndMistypedFields: a response that omits them
// must decode, and a value of the wrong type must not panic or half-populate.
func TestConvertToUser_ToleratesAbsentAndMistypedFields(t *testing.T) {
	user, ok := convertToUser(map[string]interface{}{
		"id":           "1001",
		"lifecycle":    42,
		"auth_methods": "not-a-list",
	})
	if !ok {
		t.Fatal("convertToUser did not decode the response")
	}
	if user.Lifecycle != "" || user.AuthMethods != nil {
		t.Fatalf("mistyped fields must be ignored, got %q / %v", user.Lifecycle, user.AuthMethods)
	}
}
