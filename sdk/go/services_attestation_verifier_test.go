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

package quicbaas

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
)

// TestAttestationVerifier_EmbeddedEndToEnd drives Security().Attestation()'s
// Verifier verbs through the embedded transport into the real service: the
// path an in-process platform control plane takes.
func TestAttestationVerifier_EmbeddedEndToEnd(t *testing.T) {
	store := attestationtest.NewMemoryStore()
	tr, err := embedded.New(&testutil.MockQuicBaaS{
		AttestationSvc: service.NewAttestationService(nil, nil, service.WithAttestationStore(store)),
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := newAttestationService(tr)
	ctx := tenantctx.WithTenant(context.Background(), "acme")

	akCA, err := attestationtest.NewAuthority("AK CA")
	if err != nil {
		t.Fatal(err)
	}
	rvCA, err := attestationtest.NewAuthority("RV CA")
	if err != nil {
		t.Fatal(err)
	}
	signerCert, signerKey, err := attestationtest.SignerUnder(rvCA)
	if err != nil {
		t.Fatal(err)
	}
	ak, err := attestationtest.IssueAttestationKey(akCA, true)
	if err != nil {
		t.Fatal(err)
	}
	pcrs := attestationtest.PCRValues(0, 2, 7)
	corim, err := attestationtest.SignCoRIM(pcrs, signerKey, []*x509.Certificate{signerCert})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.PutEndorsement(ctx, &AttestationPutEndorsementRequest{Name: "ak-ca", Kind: AttestationEndorsementAKRoot, CertificatePEM: akCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	if _, err := svc.PutEndorsement(ctx, &AttestationPutEndorsementRequest{Name: "rv-ca", Kind: AttestationEndorsementReferenceSigner, CertificatePEM: rvCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	listed, err := svc.ListEndorsements(ctx, "")
	if err != nil || len(listed.Endorsements) != 2 || listed.Endorsements[0].CreatedAt == "" {
		t.Fatalf("ListEndorsements = %+v, %v", listed, err)
	}
	rv, err := svc.PutReferenceValues(ctx, "golden", corim)
	if err != nil || len(rv.PCRIndices) != 3 {
		t.Fatalf("PutReferenceValues = %+v, %v", rv, err)
	}
	got, err := svc.GetReferenceValues(ctx, "golden")
	if err != nil || string(got.SignedCoRIM) != string(corim) {
		t.Fatalf("GetReferenceValues = %+v, %v", got, err)
	}
	all, err := svc.ListReferenceValues(ctx)
	if err != nil || len(all.ReferenceValues) != 1 {
		t.Fatalf("ListReferenceValues = %+v, %v", all, err)
	}

	quote, err := attestationtest.SignQuote(ak, pcrs, []byte("nonce"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.AppraiseQuote(ctx, &AttestationAppraiseQuoteRequest{
		AKCertificatePEM: ak.CertificatePEM(), Quote: quote.Attest, Signature: quote.Signature,
		Nonce: []byte("nonce"), PCRs: pcrs, ReferenceValues: "golden",
	})
	if err != nil {
		t.Fatalf("AppraiseQuote: %v", err)
	}
	if !result.Verified || result.BaselineSource != "signed" || result.AppraisedAt == "" || len(result.Checks) != 4 {
		t.Fatalf("result = %+v, want a verified, signed-baseline appraisal", result)
	}

	if err := svc.DeleteReferenceValues(ctx, "golden"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEndorsement(ctx, "ak-ca"); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteEndorsement(ctx, "ak-ca"); err == nil {
		t.Fatal("deleting a missing endorsement must fail")
	}
}

func TestAttestationVerifier_ClientValidation(t *testing.T) {
	svc := newAttestationService(nil)
	ctx := context.Background()
	//nolint:staticcheck // a nil context is the case under test
	var nilCtx context.Context
	for name, err := range map[string]error{
		"put nil":         func() error { _, err := svc.PutEndorsement(ctx, nil); return err }(),
		"put no name":     func() error { _, err := svc.PutEndorsement(ctx, &AttestationPutEndorsementRequest{}); return err }(),
		"delete no name":  svc.DeleteEndorsement(ctx, ""),
		"delete nil ctx":  svc.DeleteEndorsement(nilCtx, "x"),
		"put rv no name":  func() error { _, err := svc.PutReferenceValues(ctx, "", []byte{1}); return err }(),
		"put rv no body":  func() error { _, err := svc.PutReferenceValues(ctx, "x", nil); return err }(),
		"get rv no name":  func() error { _, err := svc.GetReferenceValues(ctx, ""); return err }(),
		"del rv no name":  svc.DeleteReferenceValues(ctx, ""),
		"del rv nil ctx":  svc.DeleteReferenceValues(nilCtx, "x"),
		"appraise nil":    func() error { _, err := svc.AppraiseQuote(ctx, nil); return err }(),
		"appraise no ref": func() error { _, err := svc.AppraiseQuote(ctx, &AttestationAppraiseQuoteRequest{}); return err }(),
		"list nil ctx":    func() error { _, err := svc.ListReferenceValues(nilCtx); return err }(),
	} {
		if !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s = %v, want invalid input before any round trip", name, err)
		}
	}
}

func TestAttestationVerifier_BadResponseType(t *testing.T) {
	svc := newAttestationService(&mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return "not a typed response", nil
	}})
	_, err := svc.ListReferenceValues(context.Background())
	if sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Fatalf("bad response = %v, want internal", err)
	}
	failing := newAttestationService(&mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, stderrors.New("down")
	}})
	if _, err := failing.ListEndorsements(context.Background(), ""); err == nil {
		t.Fatal("a transport failure must surface")
	}
}
