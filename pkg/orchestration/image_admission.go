package orchestration

import (
	"context"
	"errors"
	"fmt"
)

// =============================================================================
// Launch admission: default deny
// =============================================================================
//
// A measured launch runs only an image whose signature verifies and that
// carries software-factory provenance. The signature is ImageSignature on
// the spec; an ImageVerifier (the Verifier's, built on go-xkms) judges it
// against the image-signer endorsements, and a ProvenanceVerifier checks
// the image's signed DSSE provenance referrer, and the image must be a
// release listed in the signed ClusterManifest. An
// image without a signature runs only under an allow-unsigned grant from
// the runtime's UnsignedImagePolicy. The grant's name is folded into the
// launch policy, so it changes the policy digest, the launch measurement and
// the PCR, and it is recorded in the node measurement log: a launch under
// allow-unsigned can never pass for a signed one.

// ImageVerifier verifies the detached signature of a pinned image digest.
// It returns nil only when the signature verifies under a signer the
// verifier trusts.
type ImageVerifier interface {
	VerifyImageSignature(ctx context.Context, sig *ImageSignature) error
}

// ProvenanceVerifier checks that a signed image carries software-factory
// provenance: a signed DSSE in-toto statement, stored as a registry
// referrer of the image, whose SLSA provenance covers the image digest. It
// returns nil only when such provenance verifies.
type ProvenanceVerifier interface {
	VerifyImageProvenance(ctx context.Context, spec *ContainerSpec) error
}

// UnsignedImagePolicy decides whether a spec may launch an unsigned image.
// It returns the name of the grant that admits the spec (for example the
// tenant or service the operator granted it to) and true, or false to
// refuse. The grant name is recorded in the measurement log.
type UnsignedImagePolicy interface {
	AllowUnsigned(ctx context.Context, spec *ContainerSpec) (grant string, ok bool)
}

// UnsignedImageGrants is an UnsignedImagePolicy keyed by trust label: a
// spec whose TrustLabel names a grant may launch an unsigned image under it.
type UnsignedImageGrants map[string]string

// AllowUnsigned implements UnsignedImagePolicy.
func (g UnsignedImageGrants) AllowUnsigned(_ context.Context, spec *ContainerSpec) (string, bool) {
	if spec == nil || spec.TrustLabel == "" {
		return "", false
	}
	grant, ok := g[spec.TrustLabel]
	if !ok || grant == "" {
		return "", false
	}
	return grant, true
}

// TPMAttestingOption configures a TPMAttestingRuntime.
type TPMAttestingOption func(*TPMAttestingRuntime)

// WithImageVerifier sets the verifier a measured launch's image signature
// is checked with. Without one, no signed image can be admitted.
func WithImageVerifier(v ImageVerifier) TPMAttestingOption {
	return func(r *TPMAttestingRuntime) { r.images = v }
}

// WithProvenanceVerifier sets the verifier a measured launch's image
// provenance is checked with. Without one, no signed image can be admitted.
func WithProvenanceVerifier(v ProvenanceVerifier) TPMAttestingOption {
	return func(r *TPMAttestingRuntime) { r.provenance = v }
}

// WithUnsignedImagePolicy sets the policy that may grant an unsigned image
// a measured launch. Without one, every unsigned image is refused.
func WithUnsignedImagePolicy(p UnsignedImagePolicy) TPMAttestingOption {
	return func(r *TPMAttestingRuntime) { r.unsigned = p }
}

// ErrImageUnsigned is returned when a measured launch names an image with
// no signature and no allow-unsigned grant admits it.
var ErrImageUnsigned = errors.New("image is unsigned and no allow-unsigned grant admits it")

// ErrImageSignatureRejected is returned when a measured launch's image
// signature does not verify.
var ErrImageSignatureRejected = errors.New("image signature rejected")

// ImageUnsignedError reports an unsigned image refused at launch.
type ImageUnsignedError struct {
	Image       string
	ImageDigest string
}

func (e *ImageUnsignedError) Error() string {
	return fmt.Sprintf("launch of %s (%s) refused: %v", e.Image, e.ImageDigest, ErrImageUnsigned)
}

// Unwrap returns ErrImageUnsigned.
func (e *ImageUnsignedError) Unwrap() error { return ErrImageUnsigned }

// ImageSignatureRejectedError reports an image signature that failed
// verification, or that could not be verified because no ImageVerifier is
// configured.
type ImageSignatureRejectedError struct {
	Image       string
	ImageDigest string
	Cause       error
}

func (e *ImageSignatureRejectedError) Error() string {
	return fmt.Sprintf("launch of %s (%s) refused: %v: %v", e.Image, e.ImageDigest, ErrImageSignatureRejected, e.Cause)
}

// Unwrap returns ErrImageSignatureRejected and the cause.
func (e *ImageSignatureRejectedError) Unwrap() []error {
	return []error{ErrImageSignatureRejected, e.Cause}
}

// ErrImageProvenance is returned when a measured launch's signed image
// carries no software-factory provenance that verifies.
var ErrImageProvenance = errors.New("image provenance missing or rejected")

// ImageProvenanceError reports a signed image refused for its provenance.
type ImageProvenanceError struct {
	Image       string
	ImageDigest string
	Cause       error
}

func (e *ImageProvenanceError) Error() string {
	return fmt.Sprintf("launch of %s (%s) refused: %v: %v", e.Image, e.ImageDigest, ErrImageProvenance, e.Cause)
}

// Unwrap returns ErrImageProvenance and the cause.
func (e *ImageProvenanceError) Unwrap() []error {
	return []error{ErrImageProvenance, e.Cause}
}

// errNoImageVerifier is the cause when a signed image meets a runtime with
// no ImageVerifier.
var errNoImageVerifier = errors.New("no image verifier is configured")

// errNoProvenanceVerifier is the cause when a signed image meets a runtime
// with no ProvenanceVerifier.
var errNoProvenanceVerifier = errors.New("no provenance verifier is configured")

// ClusterManifest is the signed cluster manifest a measured launch checks:
// the list of service releases (image and digest) the cluster may run.
// CheckRelease returns nil only when the spec's pinned image is listed in a
// manifest whose signature verifies.
type ClusterManifest interface {
	CheckRelease(ctx context.Context, spec *ContainerSpec) error
}

// WithClusterManifest sets the signed cluster manifest a measured launch's
// image must be listed in. Without one, no signed image can be admitted.
func WithClusterManifest(m ClusterManifest) TPMAttestingOption {
	return func(r *TPMAttestingRuntime) { r.manifest = m }
}

// ErrReleaseNotListed is returned when a measured launch's signed image is
// not a release listed in the signed cluster manifest.
var ErrReleaseNotListed = errors.New("image is not a release listed in the signed cluster manifest")

// ReleaseNotListedError reports a signed image refused because the signed
// cluster manifest does not list it.
type ReleaseNotListedError struct {
	Image       string
	ImageDigest string
	Cause       error
}

func (e *ReleaseNotListedError) Error() string {
	return fmt.Sprintf("launch of %s (%s) refused: %v: %v", e.Image, e.ImageDigest, ErrReleaseNotListed, e.Cause)
}

// Unwrap returns ErrReleaseNotListed and the cause.
func (e *ReleaseNotListedError) Unwrap() []error {
	return []error{ErrReleaseNotListed, e.Cause}
}

// errNoClusterManifest is the cause when a signed image meets a runtime with
// no ClusterManifest.
var errNoClusterManifest = errors.New("no signed cluster manifest is configured")

// admitImage applies default deny to a measured launch and returns the
// launch policy it runs under: the spec's policy, plus the allow-unsigned
// grant when one admitted an unsigned image.
func (r *TPMAttestingRuntime) admitImage(ctx context.Context, spec *ContainerSpec) (LaunchPolicy, error) {
	policy, err := LaunchPolicyOf(spec)
	if err != nil {
		return LaunchPolicy{}, err
	}
	digest := spec.ResolvedImageDigest()
	if spec.ImageSignature != nil {
		cause := errNoImageVerifier
		if r.images != nil {
			cause = r.images.VerifyImageSignature(ctx, spec.ImageSignature)
		}
		if cause != nil {
			return LaunchPolicy{}, &ImageSignatureRejectedError{Image: spec.Image, ImageDigest: digest, Cause: cause}
		}
		cause = errNoProvenanceVerifier
		if r.provenance != nil {
			cause = r.provenance.VerifyImageProvenance(ctx, spec)
		}
		if cause != nil {
			return LaunchPolicy{}, &ImageProvenanceError{Image: spec.Image, ImageDigest: digest, Cause: cause}
		}
		cause = errNoClusterManifest
		if r.manifest != nil {
			cause = r.manifest.CheckRelease(ctx, spec)
		}
		if cause != nil {
			return LaunchPolicy{}, &ReleaseNotListedError{Image: spec.Image, ImageDigest: digest, Cause: cause}
		}
		return policy, nil
	}
	if r.unsigned != nil {
		if grant, ok := r.unsigned.AllowUnsigned(ctx, spec); ok {
			policy.AllowUnsigned = grant
			r.logger.Warnf("measured launch: AUDIT allow-unsigned grant %q admits unsigned image %s (trust label %q)",
				grant, spec.PinnedImageReference(), spec.TrustLabel)
			return policy, nil
		}
	}
	return LaunchPolicy{}, &ImageUnsignedError{Image: spec.Image, ImageDigest: digest}
}
