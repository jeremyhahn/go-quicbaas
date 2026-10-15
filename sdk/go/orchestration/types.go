// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package orchestration re-exports the container-trust surface of
// go-quicbaas/pkg/orchestration as type aliases, so a consumer bound to the
// SDK-only boundary can change imports from:
//
//	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
//
// to:
//
//	"github.com/jeremyhahn/go-quicbaas/sdk/go/orchestration"
//
// without any type conversion. It adds NO behavior: every identifier here is
// an alias, a constant copy, or a variable bound to the upstream value. A
// value produced by pkg/orchestration is the same value here, so errors.Is,
// errors.As and type assertions work across the boundary unchanged.
//
// # Coverage
//
// The three trust tiers of the standalone trusted-agent model and the seams
// a Tier-3 consumer (go-trusted-platform: device registry, scheduled
// re-attestation, quarantine, golden-PCR policy) fills:
//
//   - The trust axis — TrustLevel, TrustPolicy, TrustCapabilities,
//     ResolveTrustLevel and TrustLevelViolationError.
//   - Tier 0, image identity pinning — ContainerSpec, ContainerMount,
//     ImageSignature, ValidateImageDigest, SplitImageReference and the
//     deterministic ContainerConfigDigest.
//   - Tiers 1 and 2, host TPM attestation and measured launch — HostTPM,
//     AttestingContainerRuntime, ContainerAttestation, LaunchMeasurement and
//     the reference TPMAttestingRuntime.
//   - The runtime seam itself — ContainerRuntime, InteractiveProcess and the
//     default exec-backed ExecContainerRuntime.
//   - Every typed error and Err* sentinel those paths return.
//
// # Completeness contract
//
// Closure, not totality: every exported pkg/orchestration identifier
// reachable from an alias declared here — through an interface method
// signature, an exported struct field, or a constructor parameter — is itself
// aliased here. A consumer that can name a type can therefore always name
// every argument it must supply. The wider pkg/orchestration surface
// (sessions, jobs, workflows, agent backends) is deliberately out of scope;
// it is server-side orchestration, not container trust.
//
// The compile-time fidelity assertions at the bottom of this file break the
// build when an upstream signature drifts away from what is documented here.
package orchestration

import (
	xkmstpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	pkgorch "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Trust axis
// =============================================================================

// TrustLevel selects the integrity tier required of an agent execution on a
// per-request basis. It is the trust-axis counterpart of SandboxMode.
type TrustLevel = pkgorch.TrustLevel

// Trust levels, lowest tier first. TrustLevelDefault is the zero value and
// defers to the server's TrustPolicy.
const (
	// TrustLevelDefault lets the server resolve the effective level from its
	// configured TrustPolicy and any tenant override.
	TrustLevelDefault = pkgorch.TrustLevelDefault

	// TrustLevelNone requires no integrity guarantee.
	TrustLevelNone = pkgorch.TrustLevelNone

	// TrustLevelPinned is Tier 0: launch and verify by immutable content
	// digest. Requires no hardware.
	TrustLevelPinned = pkgorch.TrustLevelPinned

	// TrustLevelAttested is Tier 1: Tier 0 plus a host TPM quote verified
	// against the expected PCR set.
	TrustLevelAttested = pkgorch.TrustLevelAttested

	// TrustLevelMeasured is Tier 2: Tier 1 plus measured launch.
	TrustLevelMeasured = pkgorch.TrustLevelMeasured
)

// ParseTrustLevel decodes a string (as produced by TrustLevel.String) back
// into a TrustLevel. Empty string maps to TrustLevelDefault.
var ParseTrustLevel = pkgorch.ParseTrustLevel

// TrustPolicy is the server-side policy governing how TrustLevelDefault
// resolves and which explicit levels are permitted.
type TrustPolicy = pkgorch.TrustPolicy

// Trust policies. TrustPolicyOptIn is the zero value.
const (
	// TrustPolicyOptIn resolves DEFAULT to TrustLevelNone and rejects
	// nothing.
	TrustPolicyOptIn = pkgorch.TrustPolicyOptIn

	// TrustPolicyPinned resolves DEFAULT to TrustLevelPinned and forbids
	// TrustLevelNone.
	TrustPolicyPinned = pkgorch.TrustPolicyPinned

	// TrustPolicyAttested resolves DEFAULT to TrustLevelAttested and forbids
	// anything below it.
	TrustPolicyAttested = pkgorch.TrustPolicyAttested

	// TrustPolicyMeasured resolves DEFAULT to TrustLevelMeasured and forbids
	// anything below it.
	TrustPolicyMeasured = pkgorch.TrustPolicyMeasured
)

// ParseTrustPolicy decodes a string (as produced by TrustPolicy.String) back
// into a TrustPolicy. Empty string maps to TrustPolicyOptIn.
var ParseTrustPolicy = pkgorch.ParseTrustPolicy

// TrustCapabilities describes what the target host can actually prove. The
// zero value advertises no capability, so a required tier fails closed.
type TrustCapabilities = pkgorch.TrustCapabilities

// ResolveTrustLevel resolves the effective trust level for a request given the
// server policy, an optional tenant override, and the host's capabilities.
// Resolution is deny-by-default in both directions.
var ResolveTrustLevel = pkgorch.ResolveTrustLevel

// TrustLevelViolationError signals that a requested TrustLevel cannot be
// satisfied under the active TrustPolicy or on the target host.
type TrustLevelViolationError = pkgorch.TrustLevelViolationError

// =============================================================================
// Tier 0 — container spec, image identity and configuration digest
// =============================================================================

// ContainerSpec defines a container to run, including the trust fields. Every
// trust field is inert at its zero value.
type ContainerSpec = pkgorch.ContainerSpec

// ContainerMount defines a mount between host and container. Its Type field
// names the mount kind and participates in ContainerConfigDigest, so a
// consumer does not need to smuggle the kind through a label to have it
// measured.
type ContainerMount = pkgorch.ContainerMount

// ContainerUlimit is one per-process resource limit inside a container
// (ContainerSpec.Ulimits, emitted as --ulimit name=soft:hard). Ulimits
// participate in ContainerConfigDigest, sorted by name.
type ContainerUlimit = pkgorch.ContainerUlimit

// Mount kinds accepted by ContainerMount.Type. The empty string keeps the
// untyped short form (-v source:target[:ro]).
const (
	// MountTypeBind mounts a host path into the container.
	MountTypeBind = pkgorch.MountTypeBind

	// MountTypeVolume mounts a runtime-managed named volume.
	MountTypeVolume = pkgorch.MountTypeVolume

	// MountTypeTmpfs mounts a kernel tmpfs, which has no host source.
	MountTypeTmpfs = pkgorch.MountTypeTmpfs
)

// ImageSignature carries detached endorsement material for a pinned image
// digest: a signature over the canonical digest string plus the signer chain.
type ImageSignature = pkgorch.ImageSignature

// DigestAlgorithmSHA256 is the only OCI content-digest algorithm the trust
// layer accepts.
const DigestAlgorithmSHA256 = pkgorch.DigestAlgorithmSHA256

// ValidateImageDigest checks that digest is a canonical OCI SHA-256 content
// digest of the form "sha256:<64 lowercase hex characters>".
var ValidateImageDigest = pkgorch.ValidateImageDigest

// SplitImageReference splits an OCI image reference into its repository part
// and its content digest, reporting whether the reference was pinned.
var SplitImageReference = pkgorch.SplitImageReference

// ImageRepository returns the repository portion of an image reference with
// any content digest removed. The tag, when present, is preserved.
var ImageRepository = pkgorch.ImageRepository

// ContainerConfigDigest returns a deterministic SHA-256 digest over the parts
// of a spec that determine what the container executes, what it can see and
// what it is allowed to do — including ContainerSpec.Privileged, CapAdd,
// CapDrop, PidsLimit, Ulimits and each mount's Type. It is the value extended into a PCR at
// measured launch, so it must be reproducible across launches of the same
// approved configuration; capability lists are sorted before hashing so their
// order is not significant.
var ContainerConfigDigest = pkgorch.ContainerConfigDigest

// =============================================================================
// Tiers 1 and 2 — host attestation and measured launch
// =============================================================================

// MaxPCRIndex is the highest PCR index a TCG PC Client TPM exposes.
const MaxPCRIndex = pkgorch.MaxPCRIndex

// MeasurementPCRDefault is the PCR index used for measured launch when a spec
// does not name one. PCR 16 is the TCG PC Client "debug" register.
const MeasurementPCRDefault = pkgorch.MeasurementPCRDefault

// DefaultMeasurementBank is the PCR bank used for measured launch when a spec
// does not name one.
const DefaultMeasurementBank = pkgorch.DefaultMeasurementBank

// HostTPM is the narrow TPM capability the attesting container runtime needs:
// Quote, ReadPCRs and ExtendPCR. It is satisfied as-is by go-xkms *tpm2.TPM2
// and by its software simulator.
type HostTPM = pkgorch.HostTPM

// TPMQuote is the go-xkms TPM 2.0 quote returned by HostTPM.Quote. Re-exported
// so a consumer can implement HostTPM without importing go-xkms directly.
type TPMQuote = xkmstpm2.Quote

// PCRBank is a go-xkms PCR bank returned by HostTPM.ReadPCRs.
type PCRBank = xkmstpm2.PCRBank

// PCR is a single go-xkms PCR index/value pair carried in a PCRBank.
type PCR = xkmstpm2.PCR

// ContainerAttestation is the self-contained evidence produced by
// AttestingContainerRuntime.Attest: quote, nonce, PCR values and the identity
// of what was launched.
type ContainerAttestation = pkgorch.ContainerAttestation

// LaunchMeasurement returns the byte string extended into the measurement PCR
// for a spec, binding image digest, configuration digest and trust label.
var LaunchMeasurement = pkgorch.LaunchMeasurement

// AttestingContainerRuntime is the trusted-computing seam: a ContainerRuntime
// that can additionally prove what it launched and where. Callers detect it
// with a type assertion rather than requiring every runtime to carry a TPM.
type AttestingContainerRuntime = pkgorch.AttestingContainerRuntime

// TPMAttestingRuntime is the standalone Tier 1 / Tier 2 runtime: it delegates
// container operations to a base runtime and adds host attestation and
// measured launch by composing go-xkms pkg/tpm2.
type TPMAttestingRuntime = pkgorch.TPMAttestingRuntime

// NewTPMAttestingRuntime wraps base with TPM-backed attestation. base supplies
// Tier 0, tpm supplies Tiers 1 and 2, and appraiser is the Verifier the host
// quote is appraised by (service.NewHostAppraiser); all four arguments are
// required.
var NewTPMAttestingRuntime = pkgorch.NewTPMAttestingRuntime

// HostAppraiser is the seam through which a TPMAttestingRuntime hands bound
// host evidence to the Verifier, which appraises it against reference values
// stored by name.
type HostAppraiser = pkgorch.HostAppraiser

// HostEvidence is the bound host evidence handed to a HostAppraiser.
type HostEvidence = pkgorch.HostEvidence

// HostAppraisal is the Verifier's verdict on a HostEvidence.
type HostAppraisal = pkgorch.HostAppraisal

// HostAppraisalFailure is one appraisal check that did not pass.
type HostAppraisalFailure = pkgorch.HostAppraisalFailure

// PCRDivergence is one PCR whose quoted value the reference values do not
// admit.
type PCRDivergence = pkgorch.PCRDivergence

// =============================================================================
// Container runtime seam
// =============================================================================

// ContainerRuntime abstracts OCI container operations. It covers Tier 0 and
// nothing above it; attestation lives on AttestingContainerRuntime.
type ContainerRuntime = pkgorch.ContainerRuntime

// InteractiveProcess is a running interactive container with stdio pipes,
// returned by ContainerRuntime.RunInteractive.
type InteractiveProcess = pkgorch.InteractiveProcess

// ExecSpec is one command run inside a container that is already running:
// the argument vector, an environment added to the container's own, the
// working directory inside the container, and stdin.
type ExecSpec = pkgorch.ExecSpec

// ExecResult is what one command did. A non-zero ExitCode is the command's
// answer, not an error — an error from Exec means it could not be run.
type ExecResult = pkgorch.ExecResult

// ExecContainerRuntime is the default runtime; it shells out to docker or
// podman and implements Tier 0.
type ExecContainerRuntime = pkgorch.ExecContainerRuntime

// Container runtime command names accepted by NewExecContainerRuntimeWithCommand.
const (
	// RuntimeDocker is the docker container runtime command.
	RuntimeDocker = pkgorch.RuntimeDocker

	// RuntimePodman is the podman container runtime command.
	RuntimePodman = pkgorch.RuntimePodman
)

// NewExecContainerRuntime creates a runtime that auto-detects docker or
// podman, preferring docker.
var NewExecContainerRuntime = pkgorch.NewExecContainerRuntime

// NewExecContainerRuntimeWithCommand creates a runtime bound to a specific
// container runtime command.
var NewExecContainerRuntimeWithCommand = pkgorch.NewExecContainerRuntimeWithCommand

// Logger is the structured logger the container runtime constructors require.
// Re-exported so a consumer can name the parameter without importing
// go-quicbaas/pkg/interfaces.
type Logger = interfaces.Logger

// =============================================================================
// Typed errors and sentinels
// =============================================================================

// Trust-layer sentinels. Values are shared with pkg/orchestration, so
// errors.Is matches errors raised anywhere in the stack.
var (
	// ErrImageNotPinned indicates a spec requires a content-digest pin but
	// supplies only a mutable tag.
	ErrImageNotPinned = pkgorch.ErrImageNotPinned

	// ErrImageDigestMismatch indicates the image a runtime resolved does not
	// match the pinned content digest.
	ErrImageDigestMismatch = pkgorch.ErrImageDigestMismatch

	// ErrImageDigestUnresolved indicates the runtime could not determine the
	// content digest of an image.
	ErrImageDigestUnresolved = pkgorch.ErrImageDigestUnresolved

	// ErrAttestationUnavailable indicates the attestation capability a trust
	// tier requires is not present on this host.
	ErrAttestationUnavailable = pkgorch.ErrAttestationUnavailable

	// ErrPCRMismatch indicates a quoted PCR does not match its expected value.
	ErrPCRMismatch = pkgorch.ErrPCRMismatch

	// ErrHostNotVerified indicates the Verifier did not verify the host
	// evidence against the named reference values.
	ErrHostNotVerified = pkgorch.ErrHostNotVerified

	// ErrMeasurementFailed indicates the measured-launch PCR extension failed.
	ErrMeasurementFailed = pkgorch.ErrMeasurementFailed

	// ErrQuoteNotBound indicates the PCR values read back from the TPM could
	// not be bound to the signed quote body.
	ErrQuoteNotBound = pkgorch.ErrQuoteNotBound

	// ErrQuotedPCRDigestMismatch indicates the PCR digest inside the signed
	// quote does not match a recomputation over the reported PCR values.
	ErrQuotedPCRDigestMismatch = pkgorch.ErrQuotedPCRDigestMismatch

	// ErrPCRBankUnavailable indicates the PCR bank a verification step needs
	// is not among the banks the TPM reported.
	ErrPCRBankUnavailable = pkgorch.ErrPCRBankUnavailable

	// ErrQuoteFailed indicates the host TPM refused or failed to produce a
	// quote.
	ErrQuoteFailed = pkgorch.ErrQuoteFailed
)

// Container runtime sentinels.
var (
	// ErrContainerRuntimeNotAvailable indicates no docker or podman binary
	// was found.
	ErrContainerRuntimeNotAvailable = pkgorch.ErrContainerRuntimeNotAvailable

	// ErrContainerStart indicates a failure to start a container.
	ErrContainerStart = pkgorch.ErrContainerStart

	// ErrContainerStop indicates a failure to stop a container.
	ErrContainerStop = pkgorch.ErrContainerStop

	// ErrContainerNotFound indicates the container does not exist.
	ErrContainerNotFound = pkgorch.ErrContainerNotFound

	// ErrContainerImagePull indicates the image could not be pulled.
	ErrContainerImagePull = pkgorch.ErrContainerImagePull

	// ErrContainerInteractiveStart indicates a failure to start an
	// interactive container.
	ErrContainerInteractiveStart = pkgorch.ErrContainerInteractiveStart

	// ErrContainerExec indicates a command could not be RUN inside a running
	// container. A command that ran and exited non-zero is not this error;
	// its status is on ExecResult.
	ErrContainerExec = pkgorch.ErrContainerExec
)

// ErrInvalidInput is the sentinel wrapped by InputValidationError, which every
// validating entry point in this package returns.
var ErrInvalidInput = pkgorch.ErrInvalidInput

// ImageNotPinnedError indicates a spec requires a content-digest pin but the
// image reference carries only a mutable tag.
type ImageNotPinnedError = pkgorch.ImageNotPinnedError

// ImageDigestMismatchError indicates the content digest resolved for an image
// does not match the digest the spec pinned.
type ImageDigestMismatchError = pkgorch.ImageDigestMismatchError

// ImageDigestUnresolvedError indicates the runtime could not determine the
// content digest of an image, so the pin cannot be verified.
type ImageDigestUnresolvedError = pkgorch.ImageDigestUnresolvedError

// ContainerImageDigestMismatchError indicates a container was created from an
// image other than the one its spec pinned.
type ContainerImageDigestMismatchError = pkgorch.ContainerImageDigestMismatchError

// AttestationUnavailableError indicates the attestation capability a trust
// tier requires is not present.
type AttestationUnavailableError = pkgorch.AttestationUnavailableError

// HostAppraisalError indicates the Verifier did not verify the host evidence
// against the named reference values. It carries the failing checks and, for
// a divergent PCR, its expected and quoted digests.
type HostAppraisalError = pkgorch.HostAppraisalError

// ContainerMeasurementError indicates the measured-launch PCR extension
// failed, so the container was not started.
type ContainerMeasurementError = pkgorch.ContainerMeasurementError

// QuoteBindingError indicates the PCR values read from the TPM could not be
// tied to the signed quote body, so they are not evidence.
type QuoteBindingError = pkgorch.QuoteBindingError

// QuotedPCRDigestMismatchError indicates the pcrDigest inside the signed quote
// disagrees with a recomputation over the PCR values the TPM reported.
type QuotedPCRDigestMismatchError = pkgorch.QuotedPCRDigestMismatchError

// PCRBankUnavailableError indicates the PCR bank a verification step requires
// is not one the TPM reported.
type PCRBankUnavailableError = pkgorch.PCRBankUnavailableError

// ContainerQuoteError indicates the host TPM refused or failed to produce a
// quote.
type ContainerQuoteError = pkgorch.ContainerQuoteError

// ContainerExecError indicates a command could not be run inside a running
// container: the runtime refused, the container was unreachable, or the
// context ended before the command answered.
type ContainerExecError = pkgorch.ContainerExecError

// ContainerRuntimeNotAvailableError indicates no docker or podman binary was
// found.
type ContainerRuntimeNotAvailableError = pkgorch.ContainerRuntimeNotAvailableError

// ContainerStartError indicates a failure to start a container.
type ContainerStartError = pkgorch.ContainerStartError

// ContainerStopError indicates a failure to stop a container.
type ContainerStopError = pkgorch.ContainerStopError

// ContainerNotFoundError indicates the container does not exist.
type ContainerNotFoundError = pkgorch.ContainerNotFoundError

// ContainerImagePullError indicates the image could not be pulled.
type ContainerImagePullError = pkgorch.ContainerImagePullError

// ContainerInteractiveStartError indicates a failure to start an interactive
// container.
type ContainerInteractiveStartError = pkgorch.ContainerInteractiveStartError

// InputValidationError indicates invalid input was provided. It is the
// concrete type behind most validation failures in this package.
type InputValidationError = pkgorch.InputValidationError

// CommandError wraps the stderr output from a failed container runtime
// command. It appears as the Cause of ImageDigestUnresolvedError.
type CommandError = pkgorch.CommandError

// =============================================================================
// Compile-time fidelity assertions
//
// These break the build when an upstream type or signature drifts away from
// what this package documents, which is the whole point of an alias package:
// a silent upstream change must not silently change the SDK contract.
// =============================================================================

// Interfaces alias in both directions — an alias, unlike a defined type, is
// mutually assignable with its target.
var (
	_ pkgorch.ContainerRuntime          = (ContainerRuntime)(nil)
	_ ContainerRuntime                  = (pkgorch.ContainerRuntime)(nil)
	_ pkgorch.AttestingContainerRuntime = (AttestingContainerRuntime)(nil)
	_ AttestingContainerRuntime         = (pkgorch.AttestingContainerRuntime)(nil)
	_ pkgorch.HostTPM                   = (HostTPM)(nil)
	_ HostTPM                           = (pkgorch.HostTPM)(nil)
	_ pkgorch.HostAppraiser             = (HostAppraiser)(nil)
	_ HostAppraiser                     = (pkgorch.HostAppraiser)(nil)
	_ interfaces.Logger                 = (Logger)(nil)
	_ Logger                            = (interfaces.Logger)(nil)
)

// Structs alias in both directions.
var (
	_ *pkgorch.ContainerSpec            = (*ContainerSpec)(nil)
	_ *ContainerSpec                    = (*pkgorch.ContainerSpec)(nil)
	_ *pkgorch.ContainerMount           = (*ContainerMount)(nil)
	_ *ContainerMount                   = (*pkgorch.ContainerMount)(nil)
	_ *pkgorch.ContainerUlimit          = (*ContainerUlimit)(nil)
	_ *ContainerUlimit                  = (*pkgorch.ContainerUlimit)(nil)
	_ *pkgorch.ImageSignature           = (*ImageSignature)(nil)
	_ *pkgorch.ContainerAttestation     = (*ContainerAttestation)(nil)
	_ *ContainerAttestation             = (*pkgorch.ContainerAttestation)(nil)
	_ *pkgorch.InteractiveProcess       = (*InteractiveProcess)(nil)
	_ *pkgorch.ExecSpec                 = (*ExecSpec)(nil)
	_ *ExecSpec                         = (*pkgorch.ExecSpec)(nil)
	_ *pkgorch.ExecResult               = (*ExecResult)(nil)
	_ *ExecResult                       = (*pkgorch.ExecResult)(nil)
	_ *pkgorch.TPMAttestingRuntime      = (*TPMAttestingRuntime)(nil)
	_ *pkgorch.ExecContainerRuntime     = (*ExecContainerRuntime)(nil)
	_ *pkgorch.TrustCapabilities        = (*TrustCapabilities)(nil)
	_ pkgorch.TrustLevel                = TrustLevelMeasured
	_ pkgorch.TrustPolicy               = TrustPolicyMeasured
	_ *pkgorch.TrustLevelViolationError = (*TrustLevelViolationError)(nil)
)

// The reference implementations still satisfy the seams they are exported for.
var (
	_ AttestingContainerRuntime = (*TPMAttestingRuntime)(nil)
	_ ContainerRuntime          = (*ExecContainerRuntime)(nil)
)

// Function signatures. A var alias silently absorbs an upstream signature
// change; these assertions do not.
var (
	_ func(string) (TrustLevel, error)                                                     = ParseTrustLevel
	_ func(string) (TrustPolicy, error)                                                    = ParseTrustPolicy
	_ func(TrustLevel, TrustPolicy, *TrustPolicy, TrustCapabilities) (TrustLevel, error)   = ResolveTrustLevel
	_ func(string) error                                                                   = ValidateImageDigest
	_ func(string) (string, string, bool)                                                  = SplitImageReference
	_ func(string) string                                                                  = ImageRepository
	_ func(*ContainerSpec) (string, error)                                                 = ContainerConfigDigest
	_ func(*ContainerSpec) ([]byte, error)                                                 = LaunchMeasurement
	_ func(ContainerRuntime, HostTPM, HostAppraiser, Logger) (*TPMAttestingRuntime, error) = NewTPMAttestingRuntime
	_ func(Logger) (*ExecContainerRuntime, error)                                          = NewExecContainerRuntime
	_ func(string, Logger) (*ExecContainerRuntime, error)                                  = NewExecContainerRuntimeWithCommand
)

// Errors alias in both directions, so errors.As with either spelling matches
// the same concrete type.
var (
	_ error = (*ContainerExecError)(nil)
	_ error = (*ImageNotPinnedError)(nil)
	_ error = (*ImageDigestMismatchError)(nil)
	_ error = (*ImageDigestUnresolvedError)(nil)
	_ error = (*ContainerImageDigestMismatchError)(nil)
	_ error = (*AttestationUnavailableError)(nil)
	_ error = (*HostAppraisalError)(nil)
	_ error = (*ContainerMeasurementError)(nil)
	_ error = (*ContainerQuoteError)(nil)
	_ error = (*QuoteBindingError)(nil)
	_ error = (*QuotedPCRDigestMismatchError)(nil)
	_ error = (*PCRBankUnavailableError)(nil)
	_ error = (*ContainerRuntimeNotAvailableError)(nil)
	_ error = (*ContainerStartError)(nil)
	_ error = (*ContainerStopError)(nil)
	_ error = (*ContainerNotFoundError)(nil)
	_ error = (*ContainerImagePullError)(nil)
	_ error = (*ContainerInteractiveStartError)(nil)
	_ error = (*InputValidationError)(nil)
	_ error = (*CommandError)(nil)
	_ error = (*TrustLevelViolationError)(nil)

	_ *pkgorch.ImageNotPinnedError               = (*ImageNotPinnedError)(nil)
	_ *pkgorch.ImageDigestMismatchError          = (*ImageDigestMismatchError)(nil)
	_ *pkgorch.ImageDigestUnresolvedError        = (*ImageDigestUnresolvedError)(nil)
	_ *pkgorch.ContainerImageDigestMismatchError = (*ContainerImageDigestMismatchError)(nil)
	_ *pkgorch.AttestationUnavailableError       = (*AttestationUnavailableError)(nil)
	_ *pkgorch.HostAppraisalError                = (*HostAppraisalError)(nil)
	_ *pkgorch.ContainerMeasurementError         = (*ContainerMeasurementError)(nil)
	_ *pkgorch.ContainerQuoteError               = (*ContainerQuoteError)(nil)
	_ *pkgorch.InputValidationError              = (*InputValidationError)(nil)
	_ *pkgorch.CommandError                      = (*CommandError)(nil)
)

// go-xkms types named by HostTPM.
var (
	_ xkmstpm2.Quote   = TPMQuote{}
	_ xkmstpm2.PCRBank = PCRBank{}
	_ xkmstpm2.PCR     = PCR{}
)
