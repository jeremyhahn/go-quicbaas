package service

import (
	"crypto"
	"crypto/x509"
	stderrors "errors"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
)

func goldenFrom(pcrs map[uint32][]byte) *GoldenValues {
	gv := &GoldenValues{ID: "gtp-golden-1", Vendor: "acme", Model: "dl385"}
	for idx, v := range pcrs {
		gv.PCRs = append(gv.PCRs, GoldenPCR{Index: idx, Hash: crypto.SHA256, Value: v})
	}
	return gv
}

// TestImportGoldenValues_AppraisalUsesTheImport proves golden values moved
// from go-trusted-platform are stored as signed reference values in go-xkms
// and that AppraiseQuote appraises evidence against them.
func TestImportGoldenValues_AppraisalUsesTheImport(t *testing.T) {
	w := newVerifierWorld(t)
	if _, err := w.svc.PutEndorsement(w.ctx, &PutEndorsementRequest{Name: "ak-ca", Kind: EndorsementAKRoot, CertificatePEM: w.akCA.CertificatePEM()}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.svc.PutEndorsement(w.ctx, &PutEndorsementRequest{Name: "rv-ca", Kind: EndorsementReferenceSigner, CertificatePEM: w.rvCA.CertificatePEM()}); err != nil {
		t.Fatal(err)
	}
	rv, err := ImportGoldenValues(w.ctx, w.svc, "golden", goldenFrom(w.pcrs), w.signerKey, []*x509.Certificate{w.signerCert})
	if err != nil {
		t.Fatalf("ImportGoldenValues: %v", err)
	}
	if rv.ManifestID != "gtp-golden-1" || rv.Profile != GoldenValuesProfile || !reflect.DeepEqual(rv.PCRIndices, []uint32{0, 4, 7}) {
		t.Fatalf("stored reference values = %+v", rv)
	}
	if got, err := w.svc.GetReferenceValues(w.ctx, &GetReferenceValuesRequest{Name: "golden"}); err != nil || got.ManifestID != "gtp-golden-1" {
		t.Fatalf("GetReferenceValues = %+v, %v", got, err)
	}

	nonce := []byte("golden-nonce")
	if result := w.appraise(t, w.pcrs, nonce, nonce); !result.Verified || outcomeOf(result, AppraisalCheckBaseline) != AppraisalPass || result.BaselineSource != BaselineSourceSigned {
		t.Fatalf("genuine evidence against the imported values = %+v", result)
	}
	divergent := attestationtest.PCRValues(0, 4, 7)
	divergent[4] = append([]byte(nil), divergent[4]...)
	divergent[4][0] ^= 0xff
	if result := w.appraise(t, divergent, nonce, nonce); result.Verified || outcomeOf(result, AppraisalCheckBaseline) == AppraisalPass {
		t.Fatalf("a PCR off the imported golden value must fail the baseline: %+v", result)
	}
}

func TestImportGoldenValues_UnendorsedSignerIsRefused(t *testing.T) {
	w := newVerifierWorld(t)
	if _, err := w.svc.PutEndorsement(w.ctx, &PutEndorsementRequest{Name: "rv-ca", Kind: EndorsementReferenceSigner, CertificatePEM: w.rvCA.CertificatePEM()}); err != nil {
		t.Fatal(err)
	}
	rogue, err := attestationtest.NewAuthority("Rogue CA")
	if err != nil {
		t.Fatal(err)
	}
	cert, key, err := attestationtest.SignerUnder(rogue)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ImportGoldenValues(w.ctx, w.svc, "golden", goldenFrom(w.pcrs), key, []*x509.Certificate{cert}); err == nil {
		t.Fatal("golden values signed outside the reference-signer endorsements must be refused")
	}
	if _, err := w.svc.GetReferenceValues(w.ctx, &GetReferenceValuesRequest{Name: "golden"}); err == nil {
		t.Fatal("a refused import must store nothing")
	}
}

func TestSignGoldenValues_Rejects(t *testing.T) {
	w := newVerifierWorld(t)
	chain := []*x509.Certificate{w.signerCert}
	cases := map[string]*GoldenValues{
		"nil":       nil,
		"no id":     {PCRs: []GoldenPCR{{Index: 0, Hash: crypto.SHA256, Value: make([]byte, 32)}}},
		"no pcrs":   {ID: "x"},
		"sha1 bank": {ID: "x", PCRs: []GoldenPCR{{Index: 0, Hash: crypto.SHA1, Value: make([]byte, 20)}}},
		"short":     {ID: "x", PCRs: []GoldenPCR{{Index: 0, Hash: crypto.SHA256, Value: make([]byte, 20)}}},
	}
	for name, gv := range cases {
		if _, err := SignGoldenValues(gv, w.signerKey, chain); !stderrors.Is(err, ErrGoldenValues) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := SignGoldenValues(goldenFrom(w.pcrs), nil, chain); !stderrors.Is(err, ErrGoldenValues) {
		t.Errorf("nil signer: %v", err)
	}
	if _, err := ImportGoldenValues(w.ctx, nil, "golden", goldenFrom(w.pcrs), w.signerKey, chain); !stderrors.Is(err, ErrGoldenValues) {
		t.Errorf("nil service: %v", err)
	}
}
