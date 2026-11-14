package service

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"testing"

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
