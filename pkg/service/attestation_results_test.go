package service

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestAttestationResults_RecordsEveryAppraisal proves AppraiseQuote records
// its result in the go-qrdb attestation results, latest per attestation key
// and per tenant, replacing go-trusted-platform's attestation store.
func TestAttestationResults_RecordsEveryAppraisal(t *testing.T) {
	w := newVerifierWorld(t)
	w.endorse(t)
	results, err := NewAttestationResults(w.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := results.Latest(w.ctx, w.ak.CertificatePEM()); err != nil || found {
		t.Fatalf("before any appraisal: found=%v err=%v", found, err)
	}

	nonce := []byte("results-nonce")
	w.appraise(t, w.pcrs, nonce, nonce)
	rec, found, err := results.Latest(w.ctx, w.ak.CertificatePEM())
	if err != nil || !found || !rec.Result.Verified || rec.Subject != AttestationSubject(w.ak.CertificatePEM()) {
		t.Fatalf("Latest = %+v, %v, %v", rec, found, err)
	}

	// A later failing appraisal replaces the latest result.
	w.appraise(t, w.pcrs, nonce, []byte("other-nonce"))
	if rec, _, _ = results.Latest(w.ctx, w.ak.CertificatePEM()); rec.Result.Verified || !rec.Result.CryptographicFailure {
		t.Fatalf("the latest result must be the forged appraisal: %+v", rec.Result)
	}
	list, err := results.List(w.ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("List = %+v, %v", list, err)
	}

	other := tenantctx.WithTenant(context.Background(), "globex")
	if list, err := results.List(other); err != nil || len(list) != 0 {
		t.Fatalf("another tenant sees the results: %+v, %v", list, err)
	}
}

// failingPutStore fails every write.
type failingPutStore struct{ AttestationStore }

func (failingPutStore) Put(context.Context, string, []byte) error { return errUnavailableStore }

var errUnavailableStore = context.DeadlineExceeded

func TestAttestationResults_UnrecordedAppraisalIsAnError(t *testing.T) {
	w := newVerifierWorld(t)
	w.endorse(t)
	svc := NewAttestationService(nil, nil, WithAttestationStore(failingPutStore{w.store}))
	quote, err := attestationtest.SignQuote(w.ak, w.pcrs, []byte("n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppraiseQuote(w.ctx, &AppraiseQuoteRequest{
		AKCertificatePEM: w.ak.CertificatePEM(), Quote: quote.Attest, Signature: quote.Signature,
		Nonce: []byte("n"), PCRs: w.pcrs, ReferenceValues: "golden",
	}); err == nil {
		t.Fatal("an appraisal whose result was not recorded must not be returned")
	}
	if _, err := NewAttestationResults(nil); err != ErrAttestationResultsConfig {
		t.Fatalf("nil store: %v", err)
	}
}
