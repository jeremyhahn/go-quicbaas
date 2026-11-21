package sdlc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance/provenancetest"
)

func TestPublishImageProvenance(t *testing.T) {
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

	fake := provenancetest.NewRegistry()
	srv := fake.Serve()
	defer srv.Close()
	reg, err := provenance.NewRegistry(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	build := ImageBuild{
		Image:          "registry.example.com/team/app",
		Digest:         fake.AddImage("team/app"),
		BuilderID:      "https://factory.example.com",
		SourceURI:      "git+https://example.com/team/app",
		SourceRevision: strings.Repeat("a", 40),
		InvocationID:   "train-42",
		StartedOn:      time.Now().Add(-time.Minute),
		FinishedOn:     time.Now(),
	}
	ctx := context.Background()
	ref, err := PublishImageProvenance(ctx, reg, key, []*x509.Certificate{cert}, build)
	if err != nil {
		t.Fatalf("PublishImageProvenance: %v", err)
	}
	if !strings.HasPrefix(ref, "sha256:") {
		t.Fatalf("referrer digest %q", ref)
	}

	referrers, err := reg.Fetch(ctx, "team/app", build.Digest)
	if err != nil || len(referrers) != 1 {
		t.Fatalf("Fetch: %d referrers, %v", len(referrers), err)
	}
	signed := referrers[0]
	digest := sha256.Sum256(signed.Envelope.PAE())
	if !ecdsa.VerifyASN1(&key.PublicKey, digest[:], signed.Envelope.Signatures[0].Sig) {
		t.Fatal("the stored envelope must carry the factory's signature")
	}
	stmt, err := signed.Statement()
	if err != nil {
		t.Fatal(err)
	}
	pred, err := stmt.SLSA()
	if err != nil {
		t.Fatal(err)
	}
	if !stmt.Covers(build.Digest) || pred.RunDetails.Builder.ID != build.BuilderID ||
		pred.RunDetails.Metadata.InvocationID != "train-42" ||
		len(pred.BuildDefinition.ResolvedDependencies) != 1 ||
		pred.BuildDefinition.ResolvedDependencies[0].Digest["gitCommit"] != build.SourceRevision {
		t.Fatalf("provenance does not describe the build: %s", signed.Envelope.Payload)
	}

	missing := build
	missing.BuilderID = ""
	if _, err := PublishImageProvenance(ctx, reg, key, []*x509.Certificate{cert}, missing); err == nil {
		t.Fatal("provenance needs a builder ID")
	}
	unknown := build
	unknown.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := PublishImageProvenance(ctx, reg, key, []*x509.Certificate{cert}, unknown); err == nil {
		t.Fatal("provenance of an image the registry lacks must not publish")
	}
}
