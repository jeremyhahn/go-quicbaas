package service

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"fmt"
	"time"

	xkmsattestation "github.com/jeremyhahn/go-xkms/pkg/attestation"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// ErrImageVerifierNoStore is returned by NewImageVerifier without a store.
var ErrImageVerifierNoStore = stderrors.New("image verifier: an attestation store is required")

// imageVerifier is the Verifier's orchestration.ImageVerifier: it judges a
// container image signature against the tenant's image-signer
// endorsements with the go-xkms attestation verifier.
type imageVerifier struct {
	svc *attestationService
}

// NewImageVerifier returns the orchestration.ImageVerifier a measured
// launch checks image signatures with. The signer chain of a signature
// must terminate at an image-signer endorsement in store, and the leaf must
// have signed the image digest; there is no trust on first use.
func NewImageVerifier(store AttestationStore, opts ...AttestationOption) (orchestration.ImageVerifier, error) {
	if store == nil {
		return nil, ErrImageVerifierNoStore
	}
	svc := &attestationService{now: time.Now}
	for _, opt := range opts {
		opt(svc)
	}
	svc.store = store
	return &imageVerifier{svc: svc}, nil
}

// VerifyImageSignature implements orchestration.ImageVerifier.
func (v *imageVerifier) VerifyImageSignature(ctx context.Context, sig *orchestration.ImageSignature) error {
	if sig == nil {
		return stderrors.New("image signature is required")
	}
	if err := sig.Validate(); err != nil {
		return err
	}
	signers, err := v.svc.loadEndorsements(ctx, v.svc.store, EndorsementImageSigner)
	if err != nil {
		return err
	}
	var roots []*x509.Certificate
	for _, signer := range signers {
		if len(signer.CertificatePEM) == 0 {
			continue
		}
		cert, err := parseCertificatePEM(signer.CertificatePEM)
		if err != nil {
			continue
		}
		roots = append(roots, cert)
	}
	if len(roots) == 0 {
		return stderrors.New("no image-signer endorsement is stored; image signatures are never trusted on first use")
	}

	chain, err := parseCertChainPEM(sig.SignerChainPEM, "ImageVerifier.VerifyImageSignature")
	if err != nil {
		return err
	}
	leaf := chain[0]
	sigAlg := leaf.SignatureAlgorithm
	if sig.SignatureAlgorithm != "" {
		parsed, ok := signatureAlgorithmByName[sig.SignatureAlgorithm]
		if !ok {
			return fmt.Errorf("unsupported signature algorithm %q", sig.SignatureAlgorithm)
		}
		sigAlg = parsed
	}
	stmt := &xkmsattestation.AttestationStatement{
		Format:                "image-signature",
		AttestingKeyAlgorithm: leaf.PublicKeyAlgorithm,
		AttestingKeyPublic:    leaf.PublicKey,
		Signature:             sig.Signature,
		SignatureAlgorithm:    sigAlg,
		CertificateChain:      chain,
		AttestationData:       []byte(sig.Digest),
	}
	verifier := xkmsattestation.NewVerifier(roots)
	opts := xkmsattestation.DefaultVerifyOptions()
	opts.TrustedRoots = roots
	if err := verifier.VerifyChainAnchored(stmt, opts); err != nil {
		return fmt.Errorf("signer chain: %w", err)
	}
	if err := verifier.VerifySignature(stmt, opts); err != nil {
		return fmt.Errorf("signature over %s: %w", sig.Digest, err)
	}
	return nil
}
