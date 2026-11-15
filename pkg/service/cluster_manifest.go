package service

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"time"

	"github.com/opencontainers/go-digest"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
)

// PayloadTypeClusterManifest is the DSSE payload type of a signed cluster
// manifest.
const PayloadTypeClusterManifest = "application/vnd.quicbaas.cluster-manifest.v1+json"

const (
	clusterManifestSegment = "manifest/"
	clusterManifestCurrent = "current"
)

// Release is one service release a cluster manifest lists: an image (name
// without tag or digest) pinned to a digest.
type Release struct {
	Service string `json:"service,omitempty"`
	Image   string `json:"image"`
	Digest  string `json:"digest"`
}

// ClusterManifest is the list of service releases a cluster may run. It is
// signed as a DSSE envelope by a signer endorsed as manifest-signer, and
// each new manifest must carry a higher Version than the stored one.
type ClusterManifest struct {
	Cluster  string    `json:"cluster,omitempty"`
	Version  uint64    `json:"version"`
	IssuedAt time.Time `json:"issuedAt"`
	Releases []Release `json:"releases"`
}

// Validate checks that the manifest has a version and that each release
// names an image and a well-formed digest.
func (m *ClusterManifest) Validate() error {
	if m.Version == 0 {
		return stderrors.New("cluster manifest: the version must be positive")
	}
	for i, r := range m.Releases {
		if r.Image == "" {
			return fmt.Errorf("cluster manifest: release %d names no image", i)
		}
		if _, err := digest.Parse(r.Digest); err != nil {
			return fmt.Errorf("cluster manifest: release %d digest %q: %w", i, r.Digest, err)
		}
	}
	return nil
}

// Lists reports whether a release of image's repository is pinned to
// imageDigest. Registry hosts, tags and digests on image are ignored.
func (m *ClusterManifest) Lists(image, imageDigest string) bool {
	repo := provenance.RepositoryOf(image)
	for _, r := range m.Releases {
		if r.Digest == imageDigest && provenance.RepositoryOf(r.Image) == repo {
			return true
		}
	}
	return false
}

// SignClusterManifest signs m as a DSSE envelope with signer, a go-xkms
// key whose certificate chain (leaf first) is chain.
func SignClusterManifest(m *ClusterManifest, signer crypto.Signer, chain []*x509.Certificate) (*provenance.Signed, error) {
	if m == nil {
		return nil, stderrors.New("cluster manifest: a manifest is required")
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("cluster manifest: encode: %w", err)
	}
	return provenance.SignPayload(PayloadTypeClusterManifest, payload, signer, chain)
}

var (
	// ErrClusterManifestConfig is returned by NewClusterManifests without a
	// store.
	ErrClusterManifestConfig = stderrors.New("cluster manifests: an attestation store is required")

	// ErrClusterManifestRollback is returned when a manifest's version is
	// not higher than the stored manifest's.
	ErrClusterManifestRollback = stderrors.New("cluster manifest version does not advance the stored manifest")

	// ErrNoClusterManifest is returned when the tenant has no cluster
	// manifest stored.
	ErrNoClusterManifest = stderrors.New("no signed cluster manifest is stored")
)

// ClusterManifests stores the signed cluster manifest in the Verifier's
// go-qrdb store, one current manifest per tenant, and is the
// orchestration.ClusterManifest a measured launch checks. The signature is
// verified with the go-xkms attestation verifier against the tenant's
// manifest-signer endorsements when the manifest is stored and again each
// time it is read, so a record altered in the store is refused.
type ClusterManifests struct {
	svc *attestationService
}

var _ orchestration.ClusterManifest = (*ClusterManifests)(nil)

// NewClusterManifests returns the cluster manifests over store.
func NewClusterManifests(store AttestationStore, opts ...AttestationOption) (*ClusterManifests, error) {
	if store == nil {
		return nil, ErrClusterManifestConfig
	}
	svc := &attestationService{now: time.Now}
	for _, opt := range opts {
		opt(svc)
	}
	svc.store = store
	return &ClusterManifests{svc: svc}, nil
}

// Put verifies signed and stores it as the tenant's current manifest. The
// manifest's version must be higher than the stored one's.
func (c *ClusterManifests) Put(ctx context.Context, signed *provenance.Signed) (*ClusterManifest, error) {
	m, err := c.verify(ctx, signed)
	if err != nil {
		return nil, err
	}
	current, err := c.Current(ctx)
	if err != nil && !stderrors.Is(err, ErrNoClusterManifest) {
		return nil, err
	}
	if current != nil && m.Version <= current.Version {
		return nil, fmt.Errorf("%w: version %d, stored %d", ErrClusterManifestRollback, m.Version, current.Version)
	}
	key, err := c.key(ctx)
	if err != nil {
		return nil, err
	}
	record, err := json.Marshal(signed)
	if err != nil {
		return nil, err
	}
	if err := c.svc.store.Put(ctx, key, record); err != nil {
		return nil, err
	}
	return m, nil
}

// Current returns the tenant's stored manifest after verifying its
// signature, or ErrNoClusterManifest.
func (c *ClusterManifests) Current(ctx context.Context) (*ClusterManifest, error) {
	key, err := c.key(ctx)
	if err != nil {
		return nil, err
	}
	record, found, err := c.svc.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrNoClusterManifest
	}
	var signed provenance.Signed
	if err := json.Unmarshal(record, &signed); err != nil {
		return nil, fmt.Errorf("cluster manifest: decode stored record: %w", err)
	}
	return c.verify(ctx, &signed)
}

// CheckRelease implements orchestration.ClusterManifest: the spec's pinned
// image must be a release listed in the tenant's verified manifest.
func (c *ClusterManifests) CheckRelease(ctx context.Context, spec *orchestration.ContainerSpec) error {
	if spec == nil {
		return stderrors.New("a container spec is required")
	}
	imageDigest := spec.ResolvedImageDigest()
	if imageDigest == "" {
		return stderrors.New("the image is not pinned to a digest")
	}
	m, err := c.Current(ctx)
	if err != nil {
		return err
	}
	if !m.Lists(spec.Image, imageDigest) {
		return fmt.Errorf("cluster manifest version %d does not list %s@%s", m.Version, provenance.RepositoryOf(spec.Image), imageDigest)
	}
	return nil
}

func (c *ClusterManifests) key(ctx context.Context) (string, error) {
	prefix, err := attestationPrefix(ctx, clusterManifestSegment)
	if err != nil {
		return "", err
	}
	return prefix + clusterManifestCurrent, nil
}

// verify checks that signed is a cluster manifest signed by a chain that
// terminates at the tenant's manifest-signer endorsements, and returns it.
func (c *ClusterManifests) verify(ctx context.Context, signed *provenance.Signed) (*ClusterManifest, error) {
	if signed == nil || len(signed.Envelope.Signatures) == 0 {
		return nil, provenance.ErrUnsigned
	}
	if signed.Envelope.PayloadType != PayloadTypeClusterManifest {
		return nil, fmt.Errorf("cluster manifest: payload type %q is not %q", signed.Envelope.PayloadType, PayloadTypeClusterManifest)
	}
	pae := signed.Envelope.PAE()
	var sigErr error
	for _, sig := range signed.Envelope.Signatures {
		if sigErr = c.svc.verifyEndorsedSigner(ctx, EndorsementManifestSigner, signed.SignerChainPEM, "", sig.Sig, pae); sigErr == nil {
			break
		}
	}
	if sigErr != nil {
		return nil, fmt.Errorf("cluster manifest signature: %w", sigErr)
	}
	var m ClusterManifest
	if err := json.Unmarshal(signed.Envelope.Payload, &m); err != nil {
		return nil, fmt.Errorf("cluster manifest: decode: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
