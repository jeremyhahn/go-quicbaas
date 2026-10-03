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
	"testing"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestAttestationVerifierMCP_PutEndorsementAndAppraise(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_attestation_put_endorsement",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["name"] != "rv" || args["kind"] != "reference-signer" || args["public_key_pem"] != "-----BEGIN PUBLIC KEY-----" || args["certificate_pem"] != nil {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{"name": "rv", "kind": "reference-signer", "public_key_pem": "-----BEGIN PUBLIC KEY-----", "created_at": "2026-10-01T12:00:00Z"},
	})
	out, err := tr.Request(context.Background(), attestationclient.ServiceName, "PutEndorsement",
		&attestationclient.PutEndorsementRequest{Name: "rv", Kind: "reference-signer", PublicKeyPEM: []byte("-----BEGIN PUBLIC KEY-----")})
	cleanup()
	if err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	if e := out.(*attestationclient.Endorsement); string(e.PublicKeyPEM) != "-----BEGIN PUBLIC KEY-----" || e.CreatedAt == "" {
		t.Fatalf("out = %+v", e)
	}

	tr, cleanup = newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_attestation_appraise_quote",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			pcrs, _ := args["pcrs"].(map[string]interface{})
			if args["quote"] != "cQ==" || args["nonce"] != "bg==" || args["event_log"] != "bG9n" || args["ak_certificate_pem"] != "akpem" || pcrs["7"] != "AQ==" || args["reference_values"] != "golden" {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{
			"verified": false, "cryptographic_failure": true, "baseline_source": "signed",
			"checks": []interface{}{map[string]interface{}{"name": "baseline", "outcome": "forged", "reason": "PCR [7] diverges"}},
		},
	})
	defer cleanup()
	out, err = tr.Request(context.Background(), attestationclient.ServiceName, "AppraiseQuote", &attestationclient.AppraiseQuoteRequest{
		AKCertificatePEM: []byte("akpem"), Quote: []byte("q"), Signature: []byte("s"), Nonce: []byte("n"),
		PCRs: map[uint32][]byte{7: {1}}, EventLog: []byte("log"), ReferenceValues: "golden",
	})
	if err != nil {
		t.Fatalf("AppraiseQuote: %v", err)
	}
	r := out.(*attestationclient.AttestationResult)
	if r.Verified || !r.CryptographicFailure || len(r.Checks) != 1 || r.Checks[0].Outcome != "forged" {
		t.Fatalf("out = %+v", r)
	}
}

func TestAttestationVerifierMCP_ReferenceValuesAndLists(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_attestation_put_reference_values",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["name"] != "golden" || args["signed_corim"] != "0g==" {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{"name": "golden", "pcr_indices": []interface{}{0.0, 7.0}, "signed_corim": "0g=="},
	})
	out, err := tr.Request(context.Background(), attestationclient.ServiceName, "PutReferenceValues", &attestationclient.PutReferenceValuesRequest{Name: "golden", SignedCoRIM: []byte{0xd2}})
	cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if rv := out.(*attestationclient.ReferenceValues); len(rv.PCRIndices) != 2 || rv.PCRIndices[1] != 7 || len(rv.SignedCoRIM) != 1 {
		t.Fatalf("out = %+v", rv)
	}

	for _, tc := range []struct {
		method, tool string
		req          interface{}
		result       map[string]interface{}
	}{
		{"ListEndorsements", "quicbaas_attestation_list_endorsements", &attestationclient.ListEndorsementsRequest{Kind: "ak-root"},
			map[string]interface{}{"endorsements": []interface{}{map[string]interface{}{"name": "a", "kind": "ak-root"}}}},
		{"ListReferenceValues", "quicbaas_attestation_list_reference_values", nil,
			map[string]interface{}{"reference_values": []interface{}{map[string]interface{}{"name": "golden"}}}},
		{"GetReferenceValues", "quicbaas_attestation_get_reference_values", &attestationclient.NameRequest{Name: "golden"},
			map[string]interface{}{"name": "golden"}},
		{"DeleteEndorsement", "quicbaas_attestation_delete_endorsement", &attestationclient.NameRequest{Name: "a"},
			map[string]interface{}{"deleted": true}},
		{"DeleteReferenceValues", "quicbaas_attestation_delete_reference_values", &attestationclient.NameRequest{Name: "golden"},
			map[string]interface{}{"deleted": true}},
	} {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: tc.tool, result: tc.result})
		if _, err := tr.Request(context.Background(), attestationclient.ServiceName, tc.method, tc.req); err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		cleanup()
	}

	for _, method := range []string{"PutEndorsement", "ListEndorsements", "DeleteEndorsement", "PutReferenceValues", "GetReferenceValues", "DeleteReferenceValues", "AppraiseQuote"} {
		if _, err := verifierRequestTransforms[method]("wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type = %v", method, err)
		}
	}
}
