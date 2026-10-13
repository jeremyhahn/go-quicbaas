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
	"encoding/json"
	"net/http"
	"testing"
)

// TestInviteHandlerPaths routes each invite verb onto the server's public
// tenant-scoped invite route, carrying the body that route reads.
func TestInviteHandlerPaths(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	const base = "/api/v1/iam/t1/invites/tok"
	cases := []struct {
		name       string
		method     string
		req        map[string]interface{}
		wantMethod string
		wantPath   string
		wantBody   map[string]interface{}
	}{
		{"GetInvite", "GetInvite",
			map[string]interface{}{"tenant_id": "t1", "token": "tok"},
			http.MethodGet, base, nil},
		{"FIDO2Begin", "ActivateFIDO2Begin",
			map[string]interface{}{"tenant_id": "t1", "token": "tok"},
			http.MethodPost, base + "/activate/fido2/begin", nil},
		{"FIDO2Finish", "ActivateFIDO2Finish",
			map[string]interface{}{"tenant_id": "t1", "token": "tok", "credential": map[string]interface{}{"id": "c"}},
			http.MethodPost, base + "/activate/fido2/finish",
			map[string]interface{}{"credential": map[string]interface{}{"id": "c"}}},
		{"PIV", "ActivatePIV",
			map[string]interface{}{"tenant_id": "t1", "token": "tok", "cert_pem": "PEM"},
			http.MethodPost, base + "/activate/piv",
			map[string]interface{}{"cert_pem": "PEM"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, iamInviteServicePrefix, tc.method, tc.req); err != nil {
				t.Fatalf("Request: %v", err)
			}
			if rec.gotMethod != tc.wantMethod || rec.gotPath != tc.wantPath {
				t.Errorf("got %s %s, want %s %s", rec.gotMethod, rec.gotPath, tc.wantMethod, tc.wantPath)
			}
			if tc.wantBody == nil {
				return
			}
			var body map[string]interface{}
			if err := json.Unmarshal(rec.gotBody, &body); err != nil {
				t.Fatalf("decode body %q: %v", rec.gotBody, err)
			}
			want, err := json.Marshal(tc.wantBody)
			if err != nil {
				t.Fatalf("marshal want: %v", err)
			}
			got, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal got: %v", err)
			}
			if string(got) != string(want) {
				t.Errorf("body = %s, want %s", got, want)
			}
		})
	}
}

// TestInviteHandlerEscapesToken keeps a token carrying a slash inside its own
// path segment rather than letting it address another route.
func TestInviteHandlerEscapesToken(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)

	_, err := tr.Request(context.Background(), iamInviteServicePrefix, "GetInvite",
		map[string]interface{}{"tenant_id": "t1", "token": "a/../b"})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	const want = "/api/v1/iam/t1/invites/a%2F..%2Fb"
	if rec.gotRawPath != want {
		t.Errorf("raw path = %s, want %s", rec.gotRawPath, want)
	}
}

// TestInviteHandlerValidation fails closed on a missing scope rather than
// building a URL with an empty segment.
func TestInviteHandlerValidation(t *testing.T) {
	rec := newIAMRecorder(t)
	tr := newTestTransport(t, rec.server)
	ctx := context.Background()

	cases := map[string]map[string]interface{}{
		"MissingTenant": {"token": "tok"},
		"MissingToken":  {"tenant_id": "t1"},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := tr.Request(ctx, iamInviteServicePrefix, "GetInvite", req); err == nil {
				t.Error("expected an error for an incomplete request")
			}
		})
	}

	t.Run("UnsupportedMethod", func(t *testing.T) {
		_, err := tr.Request(ctx, iamInviteServicePrefix, "Frobnicate",
			map[string]interface{}{"tenant_id": "t1", "token": "tok"})
		if err == nil {
			t.Error("expected an error for an unsupported method")
		}
	})
}
