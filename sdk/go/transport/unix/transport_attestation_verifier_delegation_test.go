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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
)

// TestTransport_AttestationVerifier_Delegates proves the Unix transport
// routes every Verifier verb through the gRPC attestation handler. With no
// server listening each call fails on connection; failing as unexposed or as
// a rejected request type would mean the dispatch never saw it. The gRPC
// handler tests prove the conversion against a recording server.
func TestTransport_AttestationVerifier_Delegates(t *testing.T) {
	cases := map[string]interface{}{
		"PutEndorsement":        &attestationclient.PutEndorsementRequest{Name: "a", Kind: "ak-root", CertificatePEM: []byte("pem")},
		"ListEndorsements":      &attestationclient.ListEndorsementsRequest{},
		"DeleteEndorsement":     &attestationclient.NameRequest{Name: "a"},
		"PutReferenceValues":    &attestationclient.PutReferenceValuesRequest{Name: "g", SignedCoRIM: []byte{1}},
		"GetReferenceValues":    &attestationclient.NameRequest{Name: "g"},
		"ListReferenceValues":   &attestationclient.ListEndorsementsRequest{},
		"DeleteReferenceValues": &attestationclient.NameRequest{Name: "g"},
		"AppraiseQuote":         &attestationclient.AppraiseQuoteRequest{ReferenceValues: "g"},
	}
	for method, req := range cases {
		t.Run(method, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() {
				if err := tr.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, err := tr.Request(ctx, attestationclient.ServiceName, method, req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not exposed by AttestationService") || strings.Contains(msg, "invalid request type") {
				t.Fatalf("Verifier %s was not dispatched to the gRPC handler: %v", method, err)
			}
		})
	}
}
