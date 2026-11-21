package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"slices"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
)

// ProvenanceSource finds the provenance referrers of an image.
// *provenance.Registry implements it over the OCI referrers API.
type ProvenanceSource interface {
	Fetch(ctx context.Context, repository, imageDigest string) ([]*provenance.Signed, error)
}

// ErrProvenanceVerifierConfig is returned by NewProvenanceVerifier without
// a store or a source.
var ErrProvenanceVerifierConfig = stderrors.New("provenance verifier: an attestation store and a provenance source are required")

// provenanceVerifier is the Verifier's orchestration.ProvenanceVerifier.
type provenanceVerifier struct {
	svc      *attestationService
	source   ProvenanceSource
	builders []string
}

// NewProvenanceVerifier returns the orchestration.ProvenanceVerifier a
// measured launch checks provenance with. An image passes when one of its
// provenance referrers in source is a DSSE envelope signed by a chain that
// terminates at the tenant's image-signer endorsements (checked with the
// go-xkms attestation verifier) and carries an in-toto statement whose SLSA
// v1 provenance covers the image digest. builders, when not empty, lists
// the SLSA builder IDs accepted; empty accepts any builder the endorsed
// signer vouches for.
func NewProvenanceVerifier(store AttestationStore, source ProvenanceSource, builders []string, opts ...AttestationOption) (orchestration.ProvenanceVerifier, error) {
	if store == nil || source == nil {
		return nil, ErrProvenanceVerifierConfig
	}
	svc := &attestationService{now: time.Now}
	for _, opt := range opts {
		opt(svc)
	}
	svc.store = store
	return &provenanceVerifier{svc: svc, source: source, builders: slices.Clone(builders)}, nil
}

// VerifyImageProvenance implements orchestration.ProvenanceVerifier.
func (v *provenanceVerifier) VerifyImageProvenance(ctx context.Context, spec *orchestration.ContainerSpec) error {
	if spec == nil {
		return stderrors.New("a container spec is required")
	}
	digest := spec.ResolvedImageDigest()
	if digest == "" {
		return stderrors.New("the image is not pinned to a digest")
	}
	referrers, err := v.source.Fetch(ctx, provenance.RepositoryOf(spec.Image), digest)
	if err != nil {
		return fmt.Errorf("find provenance of %s: %w", digest, err)
	}
	if len(referrers) == 0 {
		return fmt.Errorf("%s has no provenance referrer", digest)
	}
	var errs []error
	for _, signed := range referrers {
		err := v.verify(ctx, signed, digest)
		if err == nil {
			return nil
		}
		errs = append(errs, err)
	}
	return fmt.Errorf("no provenance referrer of %s verifies: %w", digest, stderrors.Join(errs...))
}

func (v *provenanceVerifier) verify(ctx context.Context, signed *provenance.Signed, digest string) error {
	if signed == nil || len(signed.Envelope.Signatures) == 0 {
		return provenance.ErrUnsigned
	}
	pae := signed.Envelope.PAE()
	var sigErr error
	for _, sig := range signed.Envelope.Signatures {
		if sigErr = v.svc.verifyImageSigner(ctx, signed.SignerChainPEM, "", sig.Sig, pae); sigErr == nil {
			break
		}
	}
	if sigErr != nil {
		return fmt.Errorf("envelope signature: %w", sigErr)
	}
	stmt, err := signed.Statement()
	if err != nil {
		return err
	}
	if !stmt.Covers(digest) {
		return fmt.Errorf("the provenance does not cover %s", digest)
	}
	if len(v.builders) > 0 {
		pred, err := stmt.SLSA()
		if err != nil {
			return err
		}
		if !slices.Contains(v.builders, pred.RunDetails.Builder.ID) {
			return fmt.Errorf("builder %q is not an accepted builder", pred.RunDetails.Builder.ID)
		}
	}
	return nil
}
