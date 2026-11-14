package service

import (
	stderrors "errors"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// MeasuredLaunchConfig is what a host supplies for measured container
// launches: its TPM (go-trusted-platform implements HostTPM) and the
// runtime the launches run on. The Verifier side — appraisal, image
// signatures, provenance and the cluster manifest — is wired by
// NewMeasuredLaunchRuntime over the Verifier's own store.
type MeasuredLaunchConfig struct {
	// TPM records and extends each launch measurement. Required.
	TPM orchestration.HostTPM

	// Runtime is the container runtime the launches run on. Nil selects
	// the host's Docker or Podman (orchestration.NewExecContainerRuntime).
	Runtime orchestration.ContainerRuntime

	// Provenance fetches the software-factory provenance referrers of an
	// image. Nil refuses every signed launch for want of provenance.
	Provenance ProvenanceSource

	// Builders are the builder IDs whose provenance is accepted. Empty
	// accepts any builder the provenance signer vouches for.
	Builders []string

	// Unsigned is the audited allow-unsigned policy. Nil is default deny:
	// every unsigned image is refused.
	Unsigned orchestration.UnsignedImagePolicy
}

// ErrMeasuredLaunchConfig is returned by NewMeasuredLaunchRuntime when the
// configuration lacks a TPM, a store or a logger.
var ErrMeasuredLaunchConfig = stderrors.New("measured launch: a TPM, an attestation store and a logger are required")

// ErrMeasuredLaunchWithAttestor is returned when a service is built with
// both WithMeasuredLaunch and WithContainerAttestor: each fills the one
// container attestor seam.
var ErrMeasuredLaunchWithAttestor = stderrors.New("measured launch and a container attestor are exclusive")

// resolveContainerAttestor returns the attestor behind AttestContainer:
// the measured-launch runtime over store when WithMeasuredLaunch was set,
// else the application's own attestor (or none).
func (b *serviceBuilder) resolveContainerAttestor(store AttestationStore, logger interfaces.Logger) (ContainerAttestor, error) {
	if b.measuredLaunch == nil {
		return b.containerAttestor, nil
	}
	if b.containerAttestor != nil {
		return nil, ErrMeasuredLaunchWithAttestor
	}
	rt, err := NewMeasuredLaunchRuntime(store, *b.measuredLaunch, logger)
	if err != nil {
		return nil, err
	}
	return NewRuntimeContainerAttestor(rt), nil
}

// NewMeasuredLaunchRuntime returns the measured-launch runtime for a host:
// every launch is measured into the node measurement log and extended
// through cfg.TPM, quotes are appraised by the Verifier over store
// (NewHostAppraiser), and a launch is admitted only when its image
// signature verifies (NewImageVerifier), its provenance verifies
// (NewProvenanceVerifier) and the signed cluster manifest lists it
// (NewClusterManifests), or when cfg.Unsigned grants an audited exception.
func NewMeasuredLaunchRuntime(store AttestationStore, cfg MeasuredLaunchConfig, logger interfaces.Logger, opts ...AttestationOption) (*orchestration.TPMAttestingRuntime, error) {
	if store == nil || cfg.TPM == nil || logger == nil {
		return nil, ErrMeasuredLaunchConfig
	}
	base := cfg.Runtime
	if base == nil {
		rt, err := orchestration.NewExecContainerRuntime(logger)
		if err != nil {
			return nil, fmt.Errorf("measured launch: container runtime: %w", err)
		}
		base = rt
	}
	appraiser, err := NewHostAppraiser(store, opts...)
	if err != nil {
		return nil, err
	}
	images, err := NewImageVerifier(store, opts...)
	if err != nil {
		return nil, err
	}
	manifests, err := NewClusterManifests(store, opts...)
	if err != nil {
		return nil, err
	}
	runtimeOpts := []orchestration.TPMAttestingOption{
		orchestration.WithImageVerifier(images),
		orchestration.WithClusterManifest(manifests),
	}
	if cfg.Provenance != nil {
		prov, err := NewProvenanceVerifier(store, cfg.Provenance, cfg.Builders, opts...)
		if err != nil {
			return nil, err
		}
		runtimeOpts = append(runtimeOpts, orchestration.WithProvenanceVerifier(prov))
	}
	if cfg.Unsigned != nil {
		runtimeOpts = append(runtimeOpts, orchestration.WithUnsignedImagePolicy(cfg.Unsigned))
	}
	return orchestration.NewTPMAttestingRuntime(base, cfg.TPM, appraiser, logger, runtimeOpts...)
}
