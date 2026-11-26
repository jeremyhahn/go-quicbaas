package caadapter

import (
	"context"
	stderrors "errors"
	"testing"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
)

// TestXKMSCAServicer_SignCSRRefusesAttestation pins that a CSR carrying an
// attestation proof is refused, never signed as if the proof were absent:
// the facade has no attestation verifier, and go-xkms's XKMSService.SignCSR
// refuses a proof it cannot verify. Every transport (REST, QUIC, MCP, gRPC,
// Unix, embedded) reaches this servicer, so each one refuses it.
func TestXKMSCAServicer_SignCSRRefusesAttestation(t *testing.T) {
	ca := &countingCA{TrustedCA: testca.New(t, "xkmsca-attest-ca")}
	a := NewXKMSCAServicer(ca, "")
	csr := newTestCSR(t, "leaf.example.test")

	proofs := map[string]func(r *xkmstransport.SignCSRRequest){
		"format": func(r *xkmstransport.SignCSRRequest) { r.AttestationFormat = "tpm2" },
		"chain":  func(r *xkmstransport.SignCSRRequest) { r.AttestationChain = [][]byte{{0x30}} },
		"data":   func(r *xkmstransport.SignCSRRequest) { r.AttestationData = []byte("bogus") },
		"sig":    func(r *xkmstransport.SignCSRRequest) { r.AttestationSig = []byte("bogus") },
		"nonce":  func(r *xkmstransport.SignCSRRequest) { r.AttestationNonce = []byte("n") },
	}
	for name, set := range proofs {
		t.Run(name, func(t *testing.T) {
			req := &xkmstransport.SignCSRRequest{CSRPEM: csr}
			set(req)
			resp, err := a.SignCSR(context.Background(), req)
			if !stderrors.Is(err, ErrXKMSCAAttestationUnsupported) {
				t.Fatalf("expected ErrXKMSCAAttestationUnsupported, got %v", err)
			}
			if resp != nil {
				t.Fatalf("no certificate may be returned, got %#v", resp)
			}
		})
	}
	if n := ca.calls.Load(); n != 0 {
		t.Fatalf("the CA signed %d attested CSR(s) it could not verify", n)
	}

	// Without a proof the CSR is signed as before.
	if _, err := a.SignCSR(context.Background(), &xkmstransport.SignCSRRequest{CSRPEM: csr}); err != nil {
		t.Fatalf("sign without attestation: %v", err)
	}
}
