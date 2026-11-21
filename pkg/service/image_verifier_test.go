package service

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const imageTestDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// signImage signs digest as an image signer issued by ca. The signer chain
// runs leaf to root, as the go-xkms anchored chain check requires.
func signImage(t *testing.T, ca *attestationtest.Authority, digest string) *orchestration.ImageSignature {
	t.Helper()
	cert, key, err := attestationtest.SignerUnder(ca)
	if err != nil {
		t.Fatal(err)
	}
	hashed := sha256.Sum256([]byte(digest))
	sig, err := key.Sign(rand.Reader, hashed[:], crypto.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	return &orchestration.ImageSignature{
		Digest:    digest,
		Signature: sig,
		SignerChainPEM: [][]byte{
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
			ca.CertificatePEM(),
		},
	}
}

func TestImageVerifier(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	imageCA, err := attestationtest.NewAuthority("Image Signing CA")
	if err != nil {
		t.Fatal(err)
	}
	otherCA, err := attestationtest.NewAuthority("Other CA")
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewImageVerifier(store)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("NoEndorsementIsRefused", func(t *testing.T) {
		if err := verifier.VerifyImageSignature(ctx, signImage(t, imageCA, imageTestDigest)); err == nil {
			t.Fatal("a signature must not verify before an image-signer is endorsed")
		}
	})

	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "images", Kind: EndorsementImageSigner, CertificatePEM: imageCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}

	t.Run("EndorsedSignerVerifies", func(t *testing.T) {
		if err := verifier.VerifyImageSignature(ctx, signImage(t, imageCA, imageTestDigest)); err != nil {
			t.Fatalf("VerifyImageSignature: %v", err)
		}
	})

	t.Run("UnendorsedSignerIsRefused", func(t *testing.T) {
		if err := verifier.VerifyImageSignature(ctx, signImage(t, otherCA, imageTestDigest)); err == nil {
			t.Fatal("a signer outside the image-signer endorsements must be refused")
		}
	})

	t.Run("SignatureOverAnotherDigestIsRefused", func(t *testing.T) {
		sig := signImage(t, imageCA, imageTestDigest)
		sig.Digest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		if err := verifier.VerifyImageSignature(ctx, sig); err == nil {
			t.Fatal("a signature over another digest must be refused")
		}
	})

	t.Run("OtherTenantsEndorsementDoesNotApply", func(t *testing.T) {
		other := tenantctx.WithTenant(context.Background(), "globex")
		if err := verifier.VerifyImageSignature(other, signImage(t, imageCA, imageTestDigest)); err == nil {
			t.Fatal("one tenant's image-signer must not endorse another tenant's launches")
		}
	})

	t.Run("NilSignatureIsRefused", func(t *testing.T) {
		if err := verifier.VerifyImageSignature(ctx, nil); err == nil {
			t.Fatal("a nil signature must be refused")
		}
	})

	t.Run("ImageSignerMustBeACertificate", func(t *testing.T) {
		pub, err := x509.MarshalPKIXPublicKey(imageCA.Certificate.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		req := &PutEndorsementRequest{Name: "key", Kind: EndorsementImageSigner,
			PublicKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})}
		if _, err := svc.PutEndorsement(ctx, req); err == nil {
			t.Fatal("an image-signer endorsement must be a CA certificate")
		}
	})

	t.Run("NeedsStore", func(t *testing.T) {
		if _, err := NewImageVerifier(nil); err != ErrImageVerifierNoStore {
			t.Fatalf("NewImageVerifier(nil) error = %v, want ErrImageVerifierNoStore", err)
		}
	})
}
