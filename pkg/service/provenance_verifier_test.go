package service

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	stderrors "errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance/provenancetest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeProvenanceSource serves fixed referrers for one repository.
type fakeProvenanceSource struct {
	repository string
	referrers  map[string][]*provenance.Signed
	err        error
}

func (f *fakeProvenanceSource) Fetch(_ context.Context, repository, imageDigest string) ([]*provenance.Signed, error) {
	if f.err != nil {
		return nil, f.err
	}
	if repository != f.repository {
		return nil, nil
	}
	return f.referrers[imageDigest], nil
}

// signProvenance signs SLSA provenance for digest by builder, as a factory
// signer issued by ca.
func signProvenance(t *testing.T, ca *attestationtest.Authority, digest, builder string) *provenance.Signed {
	t.Helper()
	cert, key, err := attestationtest.SignerUnder(ca)
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := provenance.NewImageStatement("registry.example.com/team/app", digest, &provenance.SLSAProvenance{
		BuildDefinition: provenance.BuildDefinition{BuildType: provenance.BuildTypeSoftwareFactory},
		RunDetails:      provenance.RunDetails{Builder: provenance.Builder{ID: builder}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := provenance.Sign(stmt, key, []*x509.Certificate{cert, ca.Certificate})
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// newP384Authority returns a CA whose P-384 key signs the certificates it
// issues with ECDSA-SHA384, so a leaf's SignatureAlgorithm names the
// issuer's algorithm, not the one its own key signs DSSE envelopes with.
func newP384Authority(t *testing.T, commonName string) *attestationtest.Authority {
	t.Helper()
	// local-keygen-ok: models an external image or manifest signing CA; the Verifier only verifies under it.
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	cert := createTestCertificate(t, tmpl, tmpl, &key.PublicKey, key)
	if cert.SignatureAlgorithm != x509.ECDSAWithSHA384 {
		t.Fatalf("CA certificate signed with %s, want ECDSA-SHA384", cert.SignatureAlgorithm)
	}
	return &attestationtest.Authority{Certificate: cert, Key: key}
}

// dsseLeafUnder issues a signing certificate for key under ca and returns
// the chain a DSSE envelope carries.
func dsseLeafUnder(t *testing.T, ca *attestationtest.Authority, key crypto.Signer) []*x509.Certificate {
	t.Helper()
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "dsse signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	return []*x509.Certificate{createTestCertificate(t, tmpl, ca.Certificate, key.Public(), ca.Key), ca.Certificate}
}

func createTestCertificate(t *testing.T, tmpl, parent *x509.Certificate, pub crypto.PublicKey, signer crypto.Signer) *x509.Certificate {
	t.Helper()
	// local-keygen-ok: issues test certificates for the external signers above.
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

// newP256Key and newRSAKey return DSSE signing keys for the tests.
func newP256Key(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	// local-keygen-ok: models an external image or manifest signing key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func newRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	// local-keygen-ok: models an external image or manifest signing key.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// resignOverSHA384 replaces signed's signature with one key makes over
// SHA-384 of the PAE, which the DSSE contract (SHA-256) does not admit.
func resignOverSHA384(t *testing.T, signed *provenance.Signed, key crypto.Signer) {
	t.Helper()
	digest := sha512.Sum384(signed.Envelope.PAE())
	sig, err := key.Sign(rand.Reader, digest[:], crypto.SHA384)
	if err != nil {
		t.Fatal(err)
	}
	signed.Envelope.Signatures[0].Sig = sig
}

func signProvenanceWith(t *testing.T, key crypto.Signer, chain []*x509.Certificate, digest, builder string) *provenance.Signed {
	t.Helper()
	stmt, err := provenance.NewImageStatement("registry.example.com/team/app", digest, &provenance.SLSAProvenance{
		BuildDefinition: provenance.BuildDefinition{BuildType: provenance.BuildTypeSoftwareFactory},
		RunDetails:      provenance.RunDetails{Builder: provenance.Builder{ID: builder}},
	})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := provenance.Sign(stmt, key, chain)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

// TestProvenanceVerifier_DSSESigningAlgorithm checks that a DSSE envelope
// is verified with the algorithm its leaf key signs with (SHA-256 over the
// PAE), not the algorithm the leaf's issuer signed the certificate with.
func TestProvenanceVerifier_DSSESigningAlgorithm(t *testing.T) {
	const factory = "https://factory.example.com"
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	ca := newP384Authority(t, "P-384 Image Signing CA")
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "images", Kind: EndorsementImageSigner, CertificatePEM: ca.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	spec := &orchestration.ContainerSpec{Image: "registry.example.com/team/app@" + imageTestDigest}
	verify := func(t *testing.T, signed *provenance.Signed) error {
		t.Helper()
		source := &fakeProvenanceSource{repository: "team/app", referrers: map[string][]*provenance.Signed{imageTestDigest: {signed}}}
		v, err := NewProvenanceVerifier(store, source, []string{factory})
		if err != nil {
			t.Fatal(err)
		}
		return v.VerifyImageProvenance(ctx, spec)
	}

	t.Run("P256LeafUnderP384CAVerifies", func(t *testing.T) {
		key := newP256Key(t)
		if err := verify(t, signProvenanceWith(t, key, dsseLeafUnder(t, ca, key), imageTestDigest, factory)); err != nil {
			t.Fatalf("VerifyImageProvenance: %v", err)
		}
	})

	t.Run("RSALeafUnderECDSACAVerifies", func(t *testing.T) {
		key := newRSAKey(t)
		if err := verify(t, signProvenanceWith(t, key, dsseLeafUnder(t, ca, key), imageTestDigest, factory)); err != nil {
			t.Fatalf("VerifyImageProvenance: %v", err)
		}
	})

	t.Run("SHA384OverPAEIsRefused", func(t *testing.T) {
		key := newP256Key(t)
		signed := signProvenanceWith(t, key, dsseLeafUnder(t, ca, key), imageTestDigest, factory)
		resignOverSHA384(t, signed, key)
		if err := verify(t, signed); err == nil {
			t.Fatal("a signature over SHA-384 of the PAE is not a DSSE signature and must be refused")
		}
	})
}

func TestDSSESignatureAlgorithm(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  crypto.Signer
		want x509.SignatureAlgorithm
	}{
		{"ECDSA", newP256Key(t), x509.ECDSAWithSHA256},
		{"RSA", newRSAKey(t), x509.SHA256WithRSA},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := dsseLeafUnder(t, newP384Authority(t, "CA"), tc.key)[0]
			got, err := dsseSignatureAlgorithm(leaf)
			if err != nil || got != tc.want {
				t.Fatalf("dsseSignatureAlgorithm = %s, %v; want %s", got, err, tc.want)
			}
		})
	}
	t.Run("OtherKeyAlgorithmIsNamed", func(t *testing.T) {
		// local-keygen-ok: models a signer key type DSSE verification does not accept.
		pub, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		leaf := &x509.Certificate{PublicKeyAlgorithm: x509.Ed25519, PublicKey: pub}
		if _, err := dsseSignatureAlgorithm(leaf); err == nil || !strings.Contains(err.Error(), "Ed25519") {
			t.Fatalf("an Ed25519 signer must be refused by name: %v", err)
		}
	})
}

func TestProvenanceVerifier(t *testing.T) {
	const factory = "https://factory.example.com"
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
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "images", Kind: EndorsementImageSigner, CertificatePEM: imageCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	spec := &orchestration.ContainerSpec{Image: "registry.example.com/team/app@" + imageTestDigest}
	verify := func(t *testing.T, builders []string, referrers ...*provenance.Signed) error {
		t.Helper()
		source := &fakeProvenanceSource{repository: "team/app", referrers: map[string][]*provenance.Signed{imageTestDigest: referrers}}
		v, err := NewProvenanceVerifier(store, source, builders)
		if err != nil {
			t.Fatal(err)
		}
		return v.VerifyImageProvenance(ctx, spec)
	}

	t.Run("EndorsedFactoryProvenanceVerifies", func(t *testing.T) {
		if err := verify(t, []string{factory}, signProvenance(t, imageCA, imageTestDigest, factory)); err != nil {
			t.Fatalf("VerifyImageProvenance: %v", err)
		}
	})

	t.Run("NoReferrerIsRefused", func(t *testing.T) {
		if err := verify(t, nil); err == nil {
			t.Fatal("an image without provenance must be refused")
		}
	})

	t.Run("UnendorsedSignerIsRefused", func(t *testing.T) {
		if err := verify(t, nil, signProvenance(t, otherCA, imageTestDigest, factory)); err == nil {
			t.Fatal("provenance signed outside the image-signer endorsements must be refused")
		}
	})

	t.Run("ProvenanceOfAnotherImageIsRefused", func(t *testing.T) {
		other := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
		if err := verify(t, nil, signProvenance(t, imageCA, other, factory)); err == nil {
			t.Fatal("provenance must cover the launched digest")
		}
	})

	t.Run("TamperedPayloadIsRefused", func(t *testing.T) {
		signed := signProvenance(t, imageCA, imageTestDigest, factory)
		forged := signProvenance(t, imageCA, imageTestDigest, "https://elsewhere.example.com")
		signed.Envelope.Payload = forged.Envelope.Payload
		if err := verify(t, []string{"https://elsewhere.example.com"}, signed); err == nil {
			t.Fatal("a payload the signature does not cover must be refused")
		}
	})

	t.Run("UnacceptedBuilderIsRefused", func(t *testing.T) {
		if err := verify(t, []string{factory}, signProvenance(t, imageCA, imageTestDigest, "https://laptop.example.com")); err == nil {
			t.Fatal("provenance from a builder outside the list must be refused")
		}
	})

	t.Run("OneGoodReferrerSuffices", func(t *testing.T) {
		bad := signProvenance(t, otherCA, imageTestDigest, factory)
		if err := verify(t, nil, bad, signProvenance(t, imageCA, imageTestDigest, factory)); err != nil {
			t.Fatalf("VerifyImageProvenance: %v", err)
		}
	})

	t.Run("OtherTenantIsRefused", func(t *testing.T) {
		source := &fakeProvenanceSource{repository: "team/app", referrers: map[string][]*provenance.Signed{
			imageTestDigest: {signProvenance(t, imageCA, imageTestDigest, factory)},
		}}
		v, err := NewProvenanceVerifier(store, source, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := v.VerifyImageProvenance(tenantctx.WithTenant(context.Background(), "globex"), spec); err == nil {
			t.Fatal("one tenant's image-signer must not vouch for another tenant's launches")
		}
	})

	t.Run("SourceErrorIsRefused", func(t *testing.T) {
		boom := stderrors.New("registry down")
		v, err := NewProvenanceVerifier(store, &fakeProvenanceSource{err: boom}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := v.VerifyImageProvenance(ctx, spec); !stderrors.Is(err, boom) {
			t.Fatalf("error %v does not wrap the source error", err)
		}
	})

	t.Run("NeedsStoreAndSource", func(t *testing.T) {
		if _, err := NewProvenanceVerifier(nil, &fakeProvenanceSource{}, nil); err != ErrProvenanceVerifierConfig {
			t.Fatalf("nil store: %v", err)
		}
		if _, err := NewProvenanceVerifier(store, nil, nil); err != ErrProvenanceVerifierConfig {
			t.Fatalf("nil source: %v", err)
		}
	})
}

// TestProvenanceVerifier_RegistryReferrer runs the Verifier against a
// provenance referrer pushed to an OCI registry over HTTP.
func TestProvenanceVerifier_RegistryReferrer(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	imageCA, err := attestationtest.NewAuthority("Image Signing CA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "images", Kind: EndorsementImageSigner, CertificatePEM: imageCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	fake := provenancetest.NewRegistry()
	srv := fake.Serve()
	defer srv.Close()
	reg, err := provenance.NewRegistry(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	image := fake.AddImage("team/app")
	spec := &orchestration.ContainerSpec{Image: "registry.example.com/team/app@" + image}
	verifier, err := NewProvenanceVerifier(store, reg, []string{"https://factory.example.com"})
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyImageProvenance(ctx, spec); err == nil {
		t.Fatal("an image with no referrer must be refused")
	}
	signed := signProvenance(t, imageCA, image, "https://factory.example.com")
	if _, err := reg.Push(ctx, "team/app", image, signed); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := verifier.VerifyImageProvenance(ctx, spec); err != nil {
		t.Fatalf("VerifyImageProvenance: %v", err)
	}
	fake.TamperBlobs("payloadType")
	if err := verifier.VerifyImageProvenance(ctx, spec); err == nil {
		t.Fatal("a referrer the registry altered must be refused")
	}
}
