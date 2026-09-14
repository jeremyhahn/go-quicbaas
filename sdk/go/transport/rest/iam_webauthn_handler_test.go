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

import (
	"context"
	"net/http"
	"testing"
)

// TestWebAuthnHandlerPaths pins the URL each credential verb builds. The routes
// are user-nested, so a wrong path would silently address another resource
// rather than fail.
func TestWebAuthnHandlerPaths(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        map[string]interface{}
		wantMethod string
		wantPath   string
	}{
		{"List", "ListWebAuthnCredentials",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodGet, "/api/v1/tenants/t1/users/u1/webauthn/credentials"},
		{"DeleteOne", "DeleteWebAuthnCredential",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "credential_id": "AbCd01"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/webauthn/credentials/AbCd01"},
		{"DeleteAll", "DeleteWebAuthnCredentials",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"},
			http.MethodDelete, "/api/v1/tenants/t1/users/u1/webauthn/credentials"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, iamWebAuthnServicePrefix, tc.method, tc.req); err != nil {
				t.Fatalf("Request: %v", err)
			}
			if rec.gotMethod != tc.wantMethod {
				t.Errorf("HTTP method = %s, want %s", rec.gotMethod, tc.wantMethod)
			}
			if rec.gotPath != tc.wantPath {
				t.Errorf("path = %s, want %s", rec.gotPath, tc.wantPath)
			}
		})
	}
}

// TestWebAuthnHandlerEscapesCredentialID guards the one path segment that comes
// from operator input: a credential id carrying a slash must not be able to
// steer the request at a different route.
func TestWebAuthnHandlerEscapesCredentialID(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)

	_, err := tr.Request(context.Background(), iamWebAuthnServicePrefix, "DeleteWebAuthnCredential",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "credential_id": "a/../../b"})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	const want = "/api/v1/tenants/t1/users/u1/webauthn/credentials/a%2F..%2F..%2Fb"
	if rec.gotRawPath != want {
		t.Errorf("raw path = %s, want %s", rec.gotRawPath, want)
	}
}

// TestWebAuthnHandlerCarriesBase64URLCredentialIDVerbatim is the SDK half of
// the go-quicbaas#574 contract. A WebAuthn credential id is unpadded base64url
// — A-Za-z0-9, '-' and '_' — every character of which is URL-unreserved, so the
// id the listing published must reach the server byte-identical rather than
// percent-encoded. An escaping change here would reach the server as a
// different string and answer 404 for a credential the user owns, which is
// exactly the failure #574 reported.
func TestWebAuthnHandlerCarriesBase64URLCredentialIDVerbatim(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)

	// Every character class in the base64url alphabet, including both
	// characters std base64 would have spelled differently.
	const credentialID = "gDb-Nrg7zuv7VzffWjMl4I-F_aZ09XyZ"

	_, err := tr.Request(context.Background(), iamWebAuthnServicePrefix, "DeleteWebAuthnCredential",
		map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "credential_id": credentialID})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	want := "/api/v1/tenants/t1/users/u1/webauthn/credentials/" + credentialID
	if rec.gotPath != want {
		t.Errorf("path = %s, want %s", rec.gotPath, want)
	}
	if rec.gotRawPath != "" && rec.gotRawPath != want {
		t.Errorf("raw path = %s, want the id unescaped (%s)", rec.gotRawPath, want)
	}
}

// TestWebAuthnHandlerValidation fails closed on a missing scope rather than
// building a URL with an empty segment, which would address the collection
// instead of the intended user.
func TestWebAuthnHandlerValidation(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    map[string]interface{}
	}{
		{"MissingTenant", "ListWebAuthnCredentials", map[string]interface{}{"user_id": "u1"}},
		{"MissingUser", "ListWebAuthnCredentials", map[string]interface{}{"tenant_id": "t1"}},
		{"MissingCredential", "DeleteWebAuthnCredential",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, iamWebAuthnServicePrefix, tc.method, tc.req); err == nil {
				t.Error("expected an error for an incomplete request")
			}
		})
	}

	t.Run("UnsupportedMethod", func(t *testing.T) {
		_, err := tr.Request(ctx, iamWebAuthnServicePrefix, "Frobnicate",
			map[string]interface{}{"tenant_id": "t1", "user_id": "u1"})
		if err == nil {
			t.Error("expected an error for an unknown method")
		}
	})
}
