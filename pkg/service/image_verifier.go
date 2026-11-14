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
	if err := v.svc.verifyImageSigner(ctx, sig.SignerChainPEM, sig.SignatureAlgorithm, sig.Signature, []byte(sig.Digest)); err != nil {
		return fmt.Errorf("signature over %s: %w", sig.Digest, err)
	}
	return nil
}

// verifyImageSigner checks that chainPEM (leaf first) terminates at one of
// the tenant's image-signer endorsements and that its leaf signed data,
// with the go-xkms attestation verifier. algName is the signature
// algorithm's name; empty takes the leaf's.
func (s *attestationService) verifyImageSigner(ctx context.Context, chainPEM [][]byte, algName string, signature, data []byte) error {
	signers, err := s.loadEndorsements(ctx, s.store, EndorsementImageSigner)
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

	chain, err := parseCertChainPEM(chainPEM, "ImageVerifier.verifyImageSigner")
	if err != nil {
		return err
	}
	leaf := chain[0]
	sigAlg := leaf.SignatureAlgorithm
	if algName != "" {
		parsed, ok := signatureAlgorithmByName[algName]
		if !ok {
			return fmt.Errorf("unsupported signature algorithm %q", algName)
		}
		sigAlg = parsed
	}
	stmt := &xkmsattestation.AttestationStatement{
		Format:                "image-signature",
		AttestingKeyAlgorithm: leaf.PublicKeyAlgorithm,
		AttestingKeyPublic:    leaf.PublicKey,
		Signature:             signature,
		SignatureAlgorithm:    sigAlg,
		CertificateChain:      chain,
		AttestationData:       data,
	}
	verifier := xkmsattestation.NewVerifier(roots)
	opts := xkmsattestation.DefaultVerifyOptions()
	opts.TrustedRoots = roots
	if err := verifier.VerifyChainAnchored(stmt, opts); err != nil {
		return fmt.Errorf("signer chain: %w", err)
	}
	if err := verifier.VerifySignature(stmt, opts); err != nil {
		return err
	}
	return nil
}
