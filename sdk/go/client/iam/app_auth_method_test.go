// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

import (
	"encoding/json"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// authMethodField decodes v's JSON and returns its token_endpoint_auth_method
// value and whether the key is present at all.
func authMethodField(t *testing.T, v interface{}) (interface{}, bool) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := fields["token_endpoint_auth_method"]
	return got, ok
}

// TestRegisterAppRequest_SendsEveryAuthMethod proves client_secret_basic, the
// enum's zero value, is sent on register like every other method (#1473).
func TestRegisterAppRequest_SendsEveryAuthMethod(t *testing.T) {
	for _, method := range []TokenEndpointAuthMethod{
		TokenEndpointAuthClientSecretBasic,
		TokenEndpointAuthClientSecretPost,
		TokenEndpointAuthPrivateKeyJWT,
	} {
		t.Run(method.String(), func(t *testing.T) {
			got, ok := authMethodField(t, RegisterAppRequest{Name: "app", TokenEndpointAuthMethod: method})
			if !ok {
				t.Fatal("token_endpoint_auth_method was dropped from the register body")
			}
			if got != method.String() {
				t.Fatalf("token_endpoint_auth_method = %v, want %q", got, method.String())
			}
		})
	}
}

// TestUpdateAppRequest_AuthMethodPresence proves an update names
// client_secret_basic when it is set and leaves the key out when it is not.
func TestUpdateAppRequest_AuthMethodPresence(t *testing.T) {
	basic := TokenEndpointAuthClientSecretBasic
	got, ok := authMethodField(t, UpdateAppRequest{TokenEndpointAuthMethod: &basic})
	if !ok || got != "client_secret_basic" {
		t.Fatalf("set: token_endpoint_auth_method = %v (present %v), want client_secret_basic", got, ok)
	}

	if got, ok := authMethodField(t, UpdateAppRequest{Name: "renamed"}); ok {
		t.Fatalf("unset: token_endpoint_auth_method = %v, want the key absent", got)
	}
}

// TestApplication_AuthMethodRoundTrip proves a decoded app on
// client_secret_basic serialises the field and decodes back to the same
// method.
func TestApplication_AuthMethodRoundTrip(t *testing.T) {
	for _, method := range []TokenEndpointAuthMethod{
		TokenEndpointAuthClientSecretBasic,
		TokenEndpointAuthClientSecretPost,
	} {
		t.Run(method.String(), func(t *testing.T) {
			app := Application{ClientID: "c1", TokenEndpointAuthMethod: method}
			got, ok := authMethodField(t, app)
			if !ok || got != method.String() {
				t.Fatalf("token_endpoint_auth_method = %v (present %v), want %q", got, ok, method.String())
			}

			raw, err := json.Marshal(app)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back Application
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if back.TokenEndpointAuthMethod != method {
				t.Fatalf("round trip = %v, want %v", back.TokenEndpointAuthMethod, method)
			}
		})
	}
}

// TestApplication_InvalidAuthMethodIsRefused proves a method that is neither a
// string nor a number is refused with the typed invalid-input error.
func TestApplication_InvalidAuthMethodIsRefused(t *testing.T) {
	var app Application
	err := json.Unmarshal([]byte(`{"client_id":"c1","token_endpoint_auth_method":true}`), &app)
	if !errors.IsInvalidInput(err) {
		t.Fatalf("err = %v, want an invalid-input error", err)
	}
}
