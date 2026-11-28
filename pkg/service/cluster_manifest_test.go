package service

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// signManifest signs a manifest of version listing releases, as a
// manifest signer issued by ca.
func signManifest(t *testing.T, ca *attestationtest.Authority, version uint64, releases ...Release) *provenance.Signed {
	t.Helper()
	cert, key, err := attestationtest.SignerUnder(ca)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := SignClusterManifest(&ClusterManifest{Cluster: "prod", Version: version, IssuedAt: time.Now().UTC(), Releases: releases},
		key, []*x509.Certificate{cert, ca.Certificate})
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestClusterManifests(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	manifestCA, err := attestationtest.NewAuthority("Cluster Manifest CA")
	if err != nil {
		t.Fatal(err)
	}
	imageCA, err := attestationtest.NewAuthority("Image Signing CA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "manifests", Kind: EndorsementManifestSigner, CertificatePEM: manifestCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "images", Kind: EndorsementImageSigner, CertificatePEM: imageCA.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	manifests, err := NewClusterManifests(store)
	if err != nil {
		t.Fatal(err)
	}
	spec := &orchestration.ContainerSpec{Image: "registry.example.com/team/app@" + imageTestDigest}
	other := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	app := Release{Service: "app", Image: "registry.example.com/team/app", Digest: imageTestDigest}

	if err := manifests.CheckRelease(ctx, spec); !stderrors.Is(err, ErrNoClusterManifest) {
		t.Fatalf("with no manifest stored: %v", err)
	}

	t.Run("UnendorsedSignerIsRefused", func(t *testing.T) {
		if _, err := manifests.Put(ctx, signManifest(t, imageCA, 1, app)); err == nil {
			t.Fatal("a manifest signed outside the manifest-signer endorsements must be refused")
		}
	})

	t.Run("ListedReleaseIsAdmitted", func(t *testing.T) {
		m, err := manifests.Put(ctx, signManifest(t, manifestCA, 1, app))
		if err != nil || m.Version != 1 {
			t.Fatalf("Put: %+v, %v", m, err)
		}
		if err := manifests.CheckRelease(ctx, spec); err != nil {
			t.Fatalf("CheckRelease: %v", err)
		}
		tagged := &orchestration.ContainerSpec{Image: "other.example.com:5000/team/app:v1", ImageDigest: imageTestDigest}
		if err := manifests.CheckRelease(ctx, tagged); err != nil {
			t.Fatalf("CheckRelease ignores host and tag: %v", err)
		}
	})

	t.Run("UnlistedDigestIsRefused", func(t *testing.T) {
		if err := manifests.CheckRelease(ctx, &orchestration.ContainerSpec{Image: "registry.example.com/team/app@" + other}); err == nil {
			t.Fatal("a digest the manifest does not list must be refused")
		}
		if err := manifests.CheckRelease(ctx, &orchestration.ContainerSpec{Image: "registry.example.com/team/other@" + imageTestDigest}); err == nil {
			t.Fatal("a listed digest under another repository must be refused")
		}
	})

	t.Run("RollbackIsRefused", func(t *testing.T) {
		if _, err := manifests.Put(ctx, signManifest(t, manifestCA, 1)); !stderrors.Is(err, ErrClusterManifestRollback) {
			t.Fatalf("an equal version must be refused: %v", err)
		}
		if err := manifests.CheckRelease(ctx, spec); err != nil {
			t.Fatalf("a refused manifest must not replace the stored one: %v", err)
		}
	})

	t.Run("NewVersionReplaces", func(t *testing.T) {
		if _, err := manifests.Put(ctx, signManifest(t, manifestCA, 2, Release{Image: "registry.example.com/team/app", Digest: other})); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if err := manifests.CheckRelease(ctx, spec); err == nil {
			t.Fatal("a release dropped from the manifest must be refused")
		}
		if err := manifests.CheckRelease(ctx, &orchestration.ContainerSpec{Image: "registry.example.com/team/app@" + other}); err != nil {
			t.Fatalf("CheckRelease: %v", err)
		}
	})

	t.Run("TamperedRecordIsRefused", func(t *testing.T) {
		key, err := manifests.key(ctx)
		if err != nil {
			t.Fatal(err)
		}
		record, _, _ := store.Get(ctx, key)
		var signed provenance.Signed
		if err := json.Unmarshal(record, &signed); err != nil {
			t.Fatal(err)
		}
		forged := signManifest(t, imageCA, 3, app)
		signed.Envelope.Payload = forged.Envelope.Payload
		record, _ = json.Marshal(&signed)
		if err := store.Put(ctx, key, record); err != nil {
			t.Fatal(err)
		}
		if err := manifests.CheckRelease(ctx, spec); err == nil {
			t.Fatal("a stored manifest whose payload the signature does not cover must be refused")
		}
	})

	t.Run("OtherTenantHasNoManifest", func(t *testing.T) {
		if err := manifests.CheckRelease(tenantctx.WithTenant(context.Background(), "globex"), spec); !stderrors.Is(err, ErrNoClusterManifest) {
			t.Fatalf("one tenant's manifest must not list another tenant's launches: %v", err)
		}
	})

	t.Run("WrongPayloadTypeIsRefused", func(t *testing.T) {
		prov := signProvenance(t, manifestCA, imageTestDigest, "https://factory.example.com")
		if _, err := manifests.Put(ctx, prov); err == nil {
			t.Fatal("a provenance envelope is not a cluster manifest")
		}
	})

	t.Run("InvalidManifestIsNotSigned", func(t *testing.T) {
		cert, key, err := attestationtest.SignerUnder(manifestCA)
		if err != nil {
			t.Fatal(err)
		}
		bad := []*ClusterManifest{{}, {Version: 1, Releases: []Release{{Digest: imageTestDigest}}}, {Version: 1, Releases: []Release{{Image: "a", Digest: "latest"}}}}
		for _, m := range bad {
			if _, err := SignClusterManifest(m, key, []*x509.Certificate{cert}); err == nil {
				t.Fatalf("manifest %+v must not sign", m)
			}
		}
	})

	t.Run("NeedsStore", func(t *testing.T) {
		if _, err := NewClusterManifests(nil); err != ErrClusterManifestConfig {
			t.Fatalf("nil store: %v", err)
		}
	})
}

// TestClusterManifests_ManifestSignerIsACA refuses a bare key as a
// manifest-signer endorsement.
func TestClusterManifests_ManifestSignerIsACA(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	svc := NewAttestationService(nil, nil, WithAttestationStore(attestationtest.NewMemoryStore()))
	ca, err := attestationtest.NewAuthority("Cluster Manifest CA")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "m", Kind: EndorsementManifestSigner, PublicKeyPEM: ca.CertificatePEM()}); err == nil {
		t.Fatal("a manifest-signer endorsement must be a CA certificate")
	}
}

// TestClusterManifests_DSSESigningAlgorithm checks that a manifest is
// verified with the algorithm its leaf key signs with (SHA-256 over the
// PAE), not the algorithm the leaf's issuer signed the certificate with.
func TestClusterManifests_DSSESigningAlgorithm(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	store := attestationtest.NewMemoryStore()
	svc := NewAttestationService(nil, nil, WithAttestationStore(store))
	ca := newP384Authority(t, "P-384 Cluster Manifest CA")
	if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "manifests", Kind: EndorsementManifestSigner, CertificatePEM: ca.CertificatePEM()}); err != nil {
		t.Fatalf("PutEndorsement: %v", err)
	}
	manifests, err := NewClusterManifests(store)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(t *testing.T, key crypto.Signer, version uint64) *provenance.Signed {
		t.Helper()
		signed, err := SignClusterManifest(&ClusterManifest{Cluster: "prod", Version: version, IssuedAt: time.Now().UTC(),
			Releases: []Release{{Service: "app", Image: "registry.example.com/team/app", Digest: imageTestDigest}}},
			key, dsseLeafUnder(t, ca, key))
		if err != nil {
			t.Fatal(err)
		}
		return signed
	}

	t.Run("P256LeafUnderP384CAVerifies", func(t *testing.T) {
		if _, err := manifests.Put(ctx, sign(t, newP256Key(t), 1)); err != nil {
			t.Fatalf("Put: %v", err)
		}
		if _, err := manifests.Current(ctx); err != nil {
			t.Fatalf("Current: %v", err)
		}
	})

	t.Run("RSALeafUnderECDSACAVerifies", func(t *testing.T) {
		if _, err := manifests.Put(ctx, sign(t, newRSAKey(t), 2)); err != nil {
			t.Fatalf("Put: %v", err)
		}
	})

	t.Run("SHA384OverPAEIsRefused", func(t *testing.T) {
		key := newP256Key(t)
		signed := sign(t, key, 3)
		resignOverSHA384(t, signed, key)
		if _, err := manifests.Put(ctx, signed); err == nil {
			t.Fatal("a signature over SHA-384 of the PAE is not a DSSE signature and must be refused")
		}
		if m, err := manifests.Current(ctx); err != nil || m.Version != 2 {
			t.Fatalf("a refused manifest must not replace the stored one: %+v, %v", m, err)
		}
	})
}

// plainStore is an AttestationStore without the conditional writes.
type plainStore struct{ AttestationStore }

// losingStore is an atomic store whose conditional writes always lose, as
// if a concurrent writer replaced the record each time.
type losingStore struct {
	*attestationtest.MemoryStore
	attempts atomic.Int32
}

func (s *losingStore) PutIfAbsent(context.Context, string, []byte) (bool, error) {
	s.attempts.Add(1)
	return false, nil
}

func (s *losingStore) CompareAndSwap(context.Context, string, []byte, []byte) (bool, error) {
	s.attempts.Add(1)
	return false, nil
}

// TestClusterManifests_CompareAndSwap checks that a manifest is replaced
// only by a conditional write, so concurrent writers cannot roll it back.
func TestClusterManifests_CompareAndSwap(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "acme")
	manifestCA, err := attestationtest.NewAuthority("Cluster Manifest CA")
	if err != nil {
		t.Fatal(err)
	}
	endorse := func(t *testing.T, store AttestationStore) {
		t.Helper()
		svc := NewAttestationService(nil, nil, WithAttestationStore(store))
		if _, err := svc.PutEndorsement(ctx, &PutEndorsementRequest{Name: "manifests", Kind: EndorsementManifestSigner, CertificatePEM: manifestCA.CertificatePEM()}); err != nil {
			t.Fatalf("PutEndorsement: %v", err)
		}
	}
	app := Release{Service: "app", Image: "registry.example.com/team/app", Digest: imageTestDigest}

	t.Run("ConcurrentPutsNeverRollBack", func(t *testing.T) {
		store := attestationtest.NewMemoryStore()
		endorse(t, store)
		manifests, err := NewClusterManifests(store)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manifests.Put(ctx, signManifest(t, manifestCA, 1, app)); err != nil {
			t.Fatalf("Put v1: %v", err)
		}
		const writers = 16
		signed := make([]*provenance.Signed, writers)
		for i := range signed {
			signed[i] = signManifest(t, manifestCA, uint64(i+2), app)
		}
		errs := make([]error, writers)
		var wg sync.WaitGroup
		for i := range signed {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = manifests.Put(ctx, signed[i])
			}(i)
		}
		wg.Wait()
		for i, err := range errs {
			if err != nil && !stderrors.Is(err, ErrClusterManifestRollback) {
				t.Errorf("Put v%d: %v", i+2, err)
			}
		}
		m, err := manifests.Current(ctx)
		if err != nil || m.Version != writers+1 {
			t.Fatalf("Current: %+v, %v; want version %d", m, err, writers+1)
		}
	})

	t.Run("LostConditionalWritesFailAfterTheBound", func(t *testing.T) {
		store := &losingStore{MemoryStore: attestationtest.NewMemoryStore()}
		endorse(t, store)
		manifests, err := NewClusterManifests(store)
		if err != nil {
			t.Fatal(err)
		}
		_, err = manifests.Put(ctx, signManifest(t, manifestCA, 1, app))
		if err == nil || !strings.Contains(err.Error(), "lost the conditional write") {
			t.Fatalf("Put must fail once its conditional writes keep losing: %v", err)
		}
		if got := store.attempts.Load(); got != clusterManifestPutAttempts {
			t.Fatalf("conditional writes tried %d times, want %d", got, clusterManifestPutAttempts)
		}
		if _, err := manifests.Current(ctx); !stderrors.Is(err, ErrNoClusterManifest) {
			t.Fatalf("a lost Put must not write: %v", err)
		}
	})

	t.Run("NeedsAnAtomicStore", func(t *testing.T) {
		if _, err := NewClusterManifests(plainStore{attestationtest.NewMemoryStore()}); err != ErrClusterManifestConfig {
			t.Fatalf("a store without conditional writes: %v", err)
		}
	})
}
