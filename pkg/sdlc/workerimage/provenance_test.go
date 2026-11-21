package workerimage

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
)

// fakeProvenancePublisher records the referrers a pass publishes.
type fakeProvenancePublisher struct {
	err    error
	pushes []publishedProvenance
}

type publishedProvenance struct {
	repository, digest string
	signed             *provenance.Signed
}

func (f *fakeProvenancePublisher) Push(_ context.Context, repository, imageDigest string, signed *provenance.Signed) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	f.pushes = append(f.pushes, publishedProvenance{repository: repository, digest: imageDigest, signed: signed})
	return "sha256:referrer", nil
}

// factorySigner returns a key and self-signed certificate standing in for
// the factory's go-xkms image-signing key.
func factorySigner(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	// local-keygen-ok: stands in for the factory's go-xkms image-signing key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "factory image signer"},
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
	return key, cert
}

func withProvenance(t *testing.T, cfg *Config) (*fakeProvenancePublisher, *ecdsa.PrivateKey) {
	t.Helper()
	withRegistry(cfg)
	key, cert := factorySigner(t)
	pub := &fakeProvenancePublisher{}
	cfg.Provenance = &Provenance{
		Publisher: pub,
		Signer:    key,
		Chain:     []*x509.Certificate{cert},
		BuilderID: "https://factory.example.com",
		SourceURI: "git+https://example.com/jeremyhahn/go-quicbaas",
	}
	return pub, key
}

// TestRefresh_PublishesTheFactoryProvenanceOfThePushedDigest proves a pass
// that pushes a worker image publishes the factory's signed provenance of
// that digest, so the image passes a measured launch's provenance check.
func TestRefresh_PublishesTheFactoryProvenanceOfThePushedDigest(t *testing.T) {
	cfg := testConfig()
	pub, key := withProvenance(t, &cfg)
	h := newHarness(t, cfg)

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome != OutcomePromoted {
		t.Fatalf("outcome = %s (%s)", report.Outcome, report.Error)
	}
	if len(pub.pushes) != 1 {
		t.Fatalf("provenance referrers published = %d, want 1", len(pub.pushes))
	}
	got := pub.pushes[0]
	if got.repository != provenance.RepositoryOf(cfg.Registry) || got.digest != testDigest {
		t.Fatalf("referrer of %s@%s, want %s@%s", got.repository, got.digest, provenance.RepositoryOf(cfg.Registry), testDigest)
	}
	sum := sha256.Sum256(got.signed.Envelope.PAE())
	if !ecdsa.VerifyASN1(&key.PublicKey, sum[:], got.signed.Envelope.Signatures[0].Sig) {
		t.Fatal("the referrer must carry the factory's signature")
	}
	stmt, err := got.signed.Statement()
	if err != nil {
		t.Fatal(err)
	}
	pred, err := stmt.SLSA()
	if err != nil {
		t.Fatal(err)
	}
	if !stmt.Covers(testDigest) || pred.RunDetails.Builder.ID != "https://factory.example.com" {
		t.Fatalf("provenance does not describe the pushed image: %s", got.signed.Envelope.Payload)
	}
}

// TestRefresh_AFailedProvenancePublishPromotesNothing proves an image whose
// provenance could not be published is not bound: a measured launch would
// refuse it.
func TestRefresh_AFailedProvenancePublishPromotesNothing(t *testing.T) {
	cfg := testConfig()
	pub, _ := withProvenance(t, &cfg)
	pub.err = errors.New("referrers API unavailable")
	h := newHarness(t, cfg)
	before := h.agents.image("acme", "coder")

	report := h.r.Refresh(t.Context(), TriggerManual)

	if report.Outcome == OutcomePromoted {
		t.Fatal("a pass whose provenance publish failed must not promote")
	}
	if got := h.agents.image("acme", "coder"); got != before {
		t.Fatalf("worker image = %q, want it left at %q", got, before)
	}
}

func TestNewRefresher_ProvenanceIsValidated(t *testing.T) {
	_, cert := factorySigner(t)
	key, _ := factorySigner(t)
	full := func() *Provenance {
		return &Provenance{Publisher: &fakeProvenancePublisher{}, Signer: key,
			Chain: []*x509.Certificate{cert}, BuilderID: "https://factory.example.com"}
	}
	cases := map[string]func(*Config){
		"no registry":  func(c *Config) { c.Registry = "" },
		"no publisher": func(c *Config) { c.Provenance.Publisher = nil },
		"no signer":    func(c *Config) { c.Provenance.Signer = nil },
		"no chain":     func(c *Config) { c.Provenance.Chain = nil },
		"no builder":   func(c *Config) { c.Provenance.BuilderID = " " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			withRegistry(&cfg)
			cfg.Provenance = full()
			mutate(&cfg)
			var invalid *InvalidConfigError
			if err := cfg.validate(); !errors.As(err, &invalid) {
				t.Fatalf("validate = %v, want an InvalidConfigError", err)
			}
		})
	}
	cfg := testConfig()
	withRegistry(&cfg)
	cfg.Provenance = full()
	if err := cfg.validate(); err != nil {
		t.Fatalf("a complete provenance config must validate: %v", err)
	}
}
