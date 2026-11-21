package sdlc

import (
	"context"
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/provenance"
)

// ImageBuild describes one container image the software factory built.
type ImageBuild struct {
	// Image is the image name without tag or digest
	// ("registry.example.com/team/app").
	Image string
	// Digest pins the built image ("sha256:<hex>").
	Digest string
	// BuilderID is the SLSA builder ID of the factory that ran the build.
	BuilderID string
	// SourceURI and SourceRevision name the source the image was built from.
	SourceURI      string
	SourceRevision string
	// InvocationID identifies the build run.
	InvocationID string
	StartedOn    time.Time
	FinishedOn   time.Time
}

// ProvenancePublisher stores signed provenance as a referrer of an image.
// *provenance.Registry implements it over the OCI referrers API.
type ProvenancePublisher interface {
	Push(ctx context.Context, repository, imageDigest string, signed *provenance.Signed) (string, error)
}

// ImageProvenance returns the in-toto statement of SLSA v1 provenance for
// build.
func ImageProvenance(build ImageBuild) (*provenance.Statement, error) {
	if build.Image == "" || build.BuilderID == "" {
		return nil, errors.New("sdlc: image provenance needs the image name and the builder ID")
	}
	params := map[string]any{"image": build.Image}
	var deps []provenance.ResourceDescriptor
	if build.SourceURI != "" {
		params["source"] = build.SourceURI
		dep := provenance.ResourceDescriptor{URI: build.SourceURI}
		if build.SourceRevision != "" {
			dep.Digest = map[string]string{"gitCommit": build.SourceRevision}
		}
		deps = append(deps, dep)
	}
	meta := provenance.BuildMetadata{InvocationID: build.InvocationID}
	if !build.StartedOn.IsZero() {
		started := build.StartedOn.UTC()
		meta.StartedOn = &started
	}
	if !build.FinishedOn.IsZero() {
		finished := build.FinishedOn.UTC()
		meta.FinishedOn = &finished
	}
	return provenance.NewImageStatement(build.Image, build.Digest, &provenance.SLSAProvenance{
		BuildDefinition: provenance.BuildDefinition{
			BuildType:            provenance.BuildTypeSoftwareFactory,
			ExternalParameters:   params,
			ResolvedDependencies: deps,
		},
		RunDetails: provenance.RunDetails{Builder: provenance.Builder{ID: build.BuilderID}, Metadata: meta},
	})
}

// PublishImageProvenance signs the SLSA provenance of build as a DSSE
// envelope with signer, the factory's go-xkms image-signing key whose
// certificate chain (leaf first) is chain, and stores it with pub as a
// referrer of the image. It returns the referrer's digest. A measured
// launch admits a signed image only when such a referrer verifies.
func PublishImageProvenance(ctx context.Context, pub ProvenancePublisher, signer crypto.Signer, chain []*x509.Certificate, build ImageBuild) (string, error) {
	if pub == nil {
		return "", errors.New("sdlc: a provenance publisher is required")
	}
	stmt, err := ImageProvenance(build)
	if err != nil {
		return "", err
	}
	signed, err := provenance.Sign(stmt, signer, chain)
	if err != nil {
		return "", err
	}
	ref, err := pub.Push(ctx, provenance.RepositoryOf(build.Image), build.Digest, signed)
	if err != nil {
		return "", fmt.Errorf("sdlc: publish provenance of %s@%s: %w", build.Image, build.Digest, err)
	}
	return ref, nil
}
