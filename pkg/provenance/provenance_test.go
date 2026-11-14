package provenance

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/provenance/provenancetest"
)

func testSigner(t *testing.T) (*ecdsa.PrivateKey, []*x509.Certificate) {
	t.Helper()
	// local-keygen-ok: stands in for a go-xkms image-signing key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "factory"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return key, []*x509.Certificate{cert}
}

func testPredicate() *SLSAProvenance {
	return &SLSAProvenance{
		BuildDefinition: BuildDefinition{
			BuildType:          BuildTypeSoftwareFactory,
			ExternalParameters: map[string]any{"source": "git+https://example.com/app@refs/heads/main"},
		},
		RunDetails: RunDetails{Builder: Builder{ID: "https://example.com/factory"}},
	}
}

func TestPAE(t *testing.T) {
	// The DSSE v1 specification's example.
	if got := string(PAE("http://example.com/HelloWorld", []byte("hello world"))); got != "DSSEv1 29 http://example.com/HelloWorld 11 hello world" {
		t.Fatalf("PAE = %q", got)
	}
}

func TestSignAndStatement(t *testing.T) {
	key, chain := testSigner(t)
	const image = "sha256:" + "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"
	stmt, err := NewImageStatement("registry.example.com/app", image, testPredicate())
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(stmt, key, chain)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(signed.Envelope.PAE())
	if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], signed.Envelope.Signatures[0].Sig) {
		t.Fatal("the signature must cover the SHA-256 of the PAE")
	}
	got, err := signed.Statement()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Covers(image) || got.Covers("sha256:"+strings.Repeat("0", 64)) {
		t.Fatal("the statement must cover exactly its subject")
	}
	if pred, err := got.SLSA(); err != nil || pred.RunDetails.Builder.ID != "https://example.com/factory" {
		t.Fatalf("SLSA predicate: %+v, %v", pred, err)
	}

	signed.Envelope.PayloadType = "text/plain"
	if _, err := signed.Statement(); err == nil {
		t.Fatal("a non in-toto payload must be refused")
	}
	if _, err := Sign(stmt, key, nil); err == nil {
		t.Fatal("signing needs the signer chain")
	}
	if _, err := NewImageStatement("app", "nodigest", testPredicate()); err == nil {
		t.Fatal("a subject needs an <alg>:<hex> digest")
	}
	bad := *stmt
	bad.PredicateType = "https://slsa.dev/provenance/v0.2"
	if err := bad.Validate(); err == nil {
		t.Fatal("only SLSA v1 provenance is accepted")
	}
}

func TestRegistryPushAndFetch(t *testing.T) {
	fake := provenancetest.NewRegistry()
	srv := fake.Serve()
	defer srv.Close()
	reg, err := NewRegistry(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	image := fake.AddImage("app")
	other := "sha256:" + strings.Repeat("e", 64)
	key, chain := testSigner(t)
	stmt, err := NewImageStatement("app", image, testPredicate())
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Sign(stmt, key, chain)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := reg.Push(ctx, "team/app", other, signed); err == nil {
		t.Fatal("pushing a referrer of an image the registry lacks must fail")
	}
	ref, err := reg.Push(ctx, "team/app", image, signed)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !strings.HasPrefix(ref, "sha256:") {
		t.Fatalf("referrer digest %q", ref)
	}

	got, err := reg.Fetch(ctx, "team/app", image)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 1 || !bytes.Equal(got[0].Envelope.Payload, signed.Envelope.Payload) ||
		len(got[0].SignerChainPEM) != 1 || !bytes.Equal(got[0].SignerChainPEM[0], signed.SignerChainPEM[0]) {
		t.Fatalf("fetched %+v, want the pushed referrer", got)
	}
	if none, err := reg.Fetch(ctx, "team/app", other); err != nil || len(none) != 0 {
		t.Fatalf("another image has no referrers, got %d (%v)", len(none), err)
	}

	// A blob the registry altered no longer matches its digest.
	fake.TamperBlobs("payloadType")
	if _, err := reg.Fetch(ctx, "team/app", image); err == nil {
		t.Fatal("a tampered envelope blob must be refused")
	}

	var regErr *RegistryError
	missing := httptest.NewServer(http.NotFoundHandler())
	defer missing.Close()
	broken, _ := NewRegistry(missing.URL, missing.Client())
	if _, err := broken.Fetch(ctx, "team/app", image); !errors.As(err, &regErr) {
		t.Fatalf("a registry error must be a RegistryError, got %v", err)
	}
	if _, err := NewRegistry("registry.example.com", nil); err == nil {
		t.Fatal("a relative registry URL must be refused")
	}
}

func TestRepositoryOf(t *testing.T) {
	for image, want := range map[string]string{
		"registry.example.com/team/app@sha256:ab": "team/app",
		"localhost:5000/app:1.0":                  "app",
		"localhost/app":                           "app",
		"team/app:latest":                         "team/app",
		"alpine":                                  "alpine",
	} {
		if got := RepositoryOf(image); got != want {
			t.Errorf("RepositoryOf(%q) = %q, want %q", image, got, want)
		}
	}
}

var _ crypto.Signer = (*ecdsa.PrivateKey)(nil)
