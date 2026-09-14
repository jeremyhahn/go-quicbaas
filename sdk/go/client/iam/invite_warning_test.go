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

// TestConvertToActivationResponse_CarriesWarning proves the client surfaces the
// activation warning instead of dropping it.
//
// The server sets it when an activation write reported an error on a user it
// then found active — the ceremony succeeded and the invite was spent, but the
// operator should hear about the irregularity. A client that discards the field
// turns a reportable event into silence, which is how #271's invite token went
// missing.
func TestConvertToActivationResponse_CarriesWarning(t *testing.T) {
	got, err := convertToActivationResponse(map[string]interface{}{
		"user_id": "u1",
		"message": "activation successful",
		"warning": "activation write reported an error but the account is active",
	})
	if err != nil {
		t.Fatalf("convertToActivationResponse: %v", err)
	}
	if got.Warning == "" {
		t.Error("the activation warning was dropped by the client")
	}
	if got.UserID != "u1" || got.Message != "activation successful" {
		t.Errorf("unexpected response: %+v", got)
	}
}

// TestConvertToActivationResponse_NoWarningLeavesFieldEmpty proves an ordinary
// activation carries no warning, so its presence is a reliable signal.
func TestConvertToActivationResponse_NoWarningLeavesFieldEmpty(t *testing.T) {
	got, err := convertToActivationResponse(map[string]interface{}{
		"user_id": "u1",
		"message": "activation successful",
	})
	if err != nil {
		t.Fatalf("convertToActivationResponse: %v", err)
	}
	if got.Warning != "" {
		t.Errorf("Warning set on a clean activation: %q", got.Warning)
	}
}
