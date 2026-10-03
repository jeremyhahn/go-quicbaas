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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// verifierExchange is one request the fake server received and its reply.
type verifierExchange struct {
	method, path, query string
	body                map[string]interface{}
}

func verifierServer(t *testing.T, got *verifierExchange, status int, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		got.body = nil
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &got.body); err != nil {
				t.Errorf("body is not JSON: %s", raw)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := io.WriteString(w, reply); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAttestationVerifierREST_Routes(t *testing.T) {
	cases := []struct {
		method    string
		req       interface{}
		reply     string
		wantVerb  string
		wantPath  string
		wantQuery string
		check     func(t *testing.T, out interface{}, body map[string]interface{})
	}{
		{"PutEndorsement", &attestationclient.PutEndorsementRequest{Name: "ak ca", Kind: "ak-root", CertificatePEM: []byte("pem")},
			`{"name":"ak ca","kind":"ak-root","created_at":"2026-10-01T12:00:00Z"}`, http.MethodPut, "/api/v1/attestation/endorsements/ak%20ca", "",
			func(t *testing.T, out interface{}, body map[string]interface{}) {
				if body["kind"] != "ak-root" || body["certificate_pem"] != "cGVt" || body["public_key_pem"] != nil {
					t.Fatalf("server received %v", body)
				}
				if out.(*attestationclient.Endorsement).CreatedAt == "" {
					t.Fatal("CreatedAt not decoded")
				}
			}},
		{"ListEndorsements", &attestationclient.ListEndorsementsRequest{Kind: "ak-root"},
			`{"endorsements":[{"name":"a","kind":"ak-root"}]}`, http.MethodGet, "/api/v1/attestation/endorsements", "kind=ak-root",
			func(t *testing.T, out interface{}, _ map[string]interface{}) {
				if len(out.(*attestationclient.ListEndorsementsResponse).Endorsements) != 1 {
					t.Fatalf("out = %+v", out)
				}
			}},
		{"DeleteEndorsement", &attestationclient.NameRequest{Name: "a"}, ``, http.MethodDelete, "/api/v1/attestation/endorsements/a", "", nil},
		{"PutReferenceValues", &attestationclient.PutReferenceValuesRequest{Name: "golden", SignedCoRIM: []byte{0xd2}},
			`{"name":"golden","manifest_id":"m","pcr_indices":[0,7],"stored_at":"2026-10-01T12:00:00Z"}`, http.MethodPut, "/api/v1/attestation/reference-values/golden", "",
			func(t *testing.T, out interface{}, body map[string]interface{}) {
				if body["signed_corim"] != "0g==" {
					t.Fatalf("server received %v", body)
				}
				if rv := out.(*attestationclient.ReferenceValues); len(rv.PCRIndices) != 2 || rv.ManifestID != "m" {
					t.Fatalf("out = %+v", rv)
				}
			}},
		{"GetReferenceValues", &attestationclient.NameRequest{Name: "golden"}, `{"name":"golden","signed_corim":"0g=="}`, http.MethodGet, "/api/v1/attestation/reference-values/golden", "",
			func(t *testing.T, out interface{}, _ map[string]interface{}) {
				if len(out.(*attestationclient.ReferenceValues).SignedCoRIM) != 1 {
					t.Fatalf("out = %+v", out)
				}
			}},
		{"ListReferenceValues", nil, `{"reference_values":[{"name":"golden"}]}`, http.MethodGet, "/api/v1/attestation/reference-values", "", nil},
		{"DeleteReferenceValues", &attestationclient.NameRequest{Name: "golden"}, ``, http.MethodDelete, "/api/v1/attestation/reference-values/golden", "", nil},
		{"AppraiseQuote", &attestationclient.AppraiseQuoteRequest{Quote: []byte("q"), PCRs: map[uint32][]byte{7: {1}}, ReferenceValues: "golden"},
			`{"verified":true,"checks":[{"name":"quote","outcome":"pass"}],"baseline_source":"signed"}`, http.MethodPost, "/api/v1/attestation/appraise-quote", "",
			func(t *testing.T, out interface{}, body map[string]interface{}) {
				if body["quote"] != "cQ==" || body["reference_values"] != "golden" || body["pcrs"].(map[string]interface{})["7"] != "AQ==" {
					t.Fatalf("server received %v", body)
				}
				if r := out.(*attestationclient.AttestationResult); !r.Verified || r.Checks[0].Outcome != "pass" {
					t.Fatalf("out = %+v", r)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			got := &verifierExchange{}
			tr := newTestTransport(t, verifierServer(t, got, http.StatusOK, tc.reply))
			defer tr.Close()
			out, err := tr.Request(context.Background(), attestationclient.ServiceName, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: %v", tc.method, err)
			}
			if got.method != tc.wantVerb || got.path != tc.wantPath || got.query != tc.wantQuery {
				t.Fatalf("server received %s %s?%s, want %s %s?%s", got.method, got.path, got.query, tc.wantVerb, tc.wantPath, tc.wantQuery)
			}
			if tc.check != nil {
				tc.check(t, out, got.body)
			}
		})
	}
}

func TestAttestationVerifierREST_Errors(t *testing.T) {
	got := &verifierExchange{}
	tr := newTestTransport(t, verifierServer(t, got, http.StatusNotFound, `{"error":"reference values not found"}`))
	defer tr.Close()
	ctx := context.Background()
	if _, err := tr.Request(ctx, attestationclient.ServiceName, "GetReferenceValues", &attestationclient.NameRequest{Name: "x"}); !sdkerrors.IsNotFound(err) {
		t.Fatalf("404 = %v", err)
	}
	for _, method := range []string{"PutEndorsement", "ListEndorsements", "DeleteEndorsement", "PutReferenceValues", "GetReferenceValues", "DeleteReferenceValues", "AppraiseQuote"} {
		if _, err := tr.Request(ctx, attestationclient.ServiceName, method, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type = %v", method, err)
		}
	}
}
