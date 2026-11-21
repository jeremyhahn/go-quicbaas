package workerimage

import (
	"context"
	"crypto"
	"crypto/x509"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// Provenance publishes the software factory's signed SLSA provenance of
// every image a pass pushes, as a DSSE referrer of the pushed digest
// (sdlc.PublishImageProvenance). A measured launch admits a signed image
// only when such a referrer verifies, so a worker image the factory pushed
// without one could not run on the measured tier.
type Provenance struct {
	// Publisher stores the referrer in the registry the image was pushed
	// to; *provenance.Registry implements it over the OCI referrers API.
	Publisher sdlc.ProvenancePublisher

	// Signer is the factory's image-signing key (a go-xkms key) and Chain
	// its certificate chain, leaf first. The chain must end at an
	// image-signer endorsement of every tenant that launches the image.
	Signer crypto.Signer
	Chain  []*x509.Certificate

	// BuilderID is the SLSA builder ID of this factory.
	BuilderID string

	// SourceURI names the source the image is built from; empty records
	// none.
	SourceURI string
}

func (p *Provenance) validate(registry string) error {
	switch {
	case registry == "":
		return &InvalidConfigError{Field: "provenance", Message: "a provenance referrer needs a registry to push it to"}
	case p.Publisher == nil:
		return &InvalidConfigError{Field: "provenance.publisher", Message: "required"}
	case p.Signer == nil:
		return &InvalidConfigError{Field: "provenance.signer", Message: "required"}
	case len(p.Chain) == 0:
		return &InvalidConfigError{Field: "provenance.chain", Message: "the signer's certificate chain is required"}
	case strings.TrimSpace(p.BuilderID) == "":
		return &InvalidConfigError{Field: "provenance.builder_id", Message: "required"}
	}
	return nil
}

// publishProvenance signs and publishes the provenance of the image pushed
// to remote ("registry:tag") at digest. Without Config.Provenance it does
// nothing.
func (r *Refresher) publishProvenance(ctx context.Context, remote, digest string) error {
	p := r.cfg.Provenance
	if p == nil {
		return nil
	}
	ref, err := sdlc.PublishImageProvenance(ctx, p.Publisher, p.Signer, p.Chain, sdlc.ImageBuild{
		Image:        repositoryOf(remote),
		Digest:       digest,
		BuilderID:    p.BuilderID,
		SourceURI:    p.SourceURI,
		InvocationID: remote,
		FinishedOn:   r.now(),
	})
	if err != nil {
		return err
	}
	r.deps.Logger.Info("worker image: published provenance", "reference", remote, "digest", digest, "referrer", ref)
	return nil
}
