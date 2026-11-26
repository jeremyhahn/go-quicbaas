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

package caadapter

import (
	"bytes"
	"context"
	"testing"

	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestXKMSCAServicer_IssueCertificateReturnsTheCAChain proves the issued
// certificate comes back with the CA chain the issuer reports: go-xkms puts
// the chain in CACertificatePEM and leaves ChainPEM empty.
func TestXKMSCAServicer_IssueCertificateReturnsTheCAChain(t *testing.T) {
	ca := testca.New(t, "chain-ca")
	s := NewXKMSCAServicer(ca, "")

	resp, err := s.IssueCertificate(context.Background(), &xkmstransport.IssueCertificateRequest{
		CommonName:   "leaf.example.test",
		ValidityDays: 1,
	})
	if err != nil {
		t.Fatalf("issue certificate: %v", err)
	}
	if len(resp.ChainPEM) == 0 {
		t.Fatal("IssueCertificate returned an empty chain")
	}
	bundle, err := ca.CABundle()
	if err != nil {
		t.Fatalf("CA bundle: %v", err)
	}
	if !bytes.Equal(resp.ChainPEM, bundle) {
		t.Fatalf("chain does not match the CA bundle:\n got %q\nwant %q", resp.ChainPEM, bundle)
	}
}
