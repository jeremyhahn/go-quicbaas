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

package orchestration

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

// =============================================================================
// Tier 0 — Image Identity Pinning
// =============================================================================
//
// Tier 0 is the no-TPM tier of the standalone trusted-agent model. It answers
// exactly one question: "is the agent running the bytes we approved?" It does
// so by pinning the container to an immutable content digest
// (repo@sha256:<hex>) rather than a mutable tag, and by verifying after launch
// that the container the runtime actually created resolves to that same image.
//
// Tier 0 requires no hardware and no host cooperation, so it is the only tier
// the default ExecContainerRuntime implements. Tiers 1 and 2 (host TPM quote
// and measured launch) live on the attesting runtime in
// container_attestation.go.

const (
	// DigestAlgorithmSHA256 is the only OCI content-digest algorithm the
	// trust layer accepts. OCI permits others in principle; accepting only
	// SHA-256 keeps the comparison surface small and matches every registry
	// in practice.
	DigestAlgorithmSHA256 = "sha256"

	// digestPrefixSHA256 is the canonical "<algorithm>:" prefix of a
	// SHA-256 OCI content digest.
	digestPrefixSHA256 = DigestAlgorithmSHA256 + ":"

	// digestHexLenSHA256 is the number of hex characters in a SHA-256
	// digest.
	digestHexLenSHA256 = 64
)

// ImageSignature carries detached endorsement material for a pinned image
// digest: a signature over the canonical digest string produced by a signer
// whose certificate chain is supplied alongside it.
//
// Verification is delegated to the go-xkms attestation Verifier (see
// pkg/service/attestation_service.go) rather than reimplemented here or
// shelled out to an external tool. The signed payload is the ASCII digest
// string ("sha256:<hex>"), so an endorsement is portable across registries
// and mirrors: it binds the approving key to image content, not to a
// repository name.
//
// This is deliberately NOT registry-native (cosign / sigstore) signature
// verification. That requires a registry client and a Rekor transparency-log
// client, neither of which is available in-repo; adding either belongs in a
// separate change rather than behind a stub here.
type ImageSignature struct {
	// Digest is the image content digest the signature covers, in
	// canonical "sha256:<hex>" form. It must equal the spec's resolved
	// ImageDigest.
	Digest string `json:"digest"`

	// Signature is the raw signature bytes over the ASCII Digest string.
	Signature []byte `json:"signature"`

	// SignerChainPEM is the PEM-encoded signer certificate chain, leaf
	// first. The leaf's public key verifies Signature; the remainder
	// chains to a trust anchor configured on the verifying service.
	SignerChainPEM [][]byte `json:"signer_chain_pem"`

	// SignatureAlgorithm names the x509.SignatureAlgorithm used, e.g.
	// "ECDSA-SHA256" or "SHA256-RSA". An empty value asks the verifier to
	// infer the algorithm from the signer certificate.
	SignatureAlgorithm string `json:"signature_algorithm,omitempty"`
}

// Validate reports whether the signature carries the minimum material
// required for verification.
func (s *ImageSignature) Validate() error {
	if s == nil {
		return &InputValidationError{Field: "image_signature", Message: "cannot be nil"}
	}
	if err := ValidateImageDigest(s.Digest); err != nil {
		return err
	}
	if len(s.Signature) == 0 {
		return &InputValidationError{Field: "image_signature.signature", Message: "cannot be empty"}
	}
	if len(s.SignerChainPEM) == 0 {
		return &InputValidationError{
			Field:   "image_signature.signer_chain_pem",
			Message: "cannot be empty",
		}
	}
	return nil
}

// ValidateImageDigest checks that digest is a canonical OCI SHA-256 content
// digest of the form "sha256:<64 lowercase hex characters>".
func ValidateImageDigest(digest string) error {
	if digest == "" {
		return &InputValidationError{Field: "image_digest", Message: "cannot be empty"}
	}
	if !strings.HasPrefix(digest, digestPrefixSHA256) {
		return &InputValidationError{
			Field:   "image_digest",
			Message: "must be prefixed with " + digestPrefixSHA256,
		}
	}
	hexPart := digest[len(digestPrefixSHA256):]
	if len(hexPart) != digestHexLenSHA256 {
		return &InputValidationError{
			Field:   "image_digest",
			Message: "sha256 digest must be 64 hex characters",
		}
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return &InputValidationError{
			Field:   "image_digest",
			Message: "sha256 digest must be lowercase hexadecimal",
		}
	}
	if strings.ToLower(hexPart) != hexPart {
		return &InputValidationError{
			Field:   "image_digest",
			Message: "sha256 digest must be lowercase hexadecimal",
		}
	}
	return nil
}

// SplitImageReference splits an OCI image reference into its repository part
// and its content digest. "alpine@sha256:ab..." yields ("alpine",
// "sha256:ab...", true); a tag-only reference such as "alpine:3.20" yields
// ("alpine:3.20", "", false).
//
// The split is performed on the last '@' so digests embedded in references
// that themselves contain '@' (which registries do not produce, but callers
// may construct) resolve to the trailing digest.
func SplitImageReference(image string) (repository, digest string, pinned bool) {
	idx := strings.LastIndex(image, "@")
	if idx < 0 {
		return image, "", false
	}
	return image[:idx], image[idx+1:], true
}

// ImageRepository returns the repository portion of an image reference with
// any content digest removed. The tag, when present, is preserved: a
// registry resolves "repo:tag@sha256:..." by digest and ignores the tag, and
// keeping it makes the emitted command line self-documenting.
func ImageRepository(image string) string {
	repository, _, _ := SplitImageReference(image)
	return repository
}

// ResolvedImageDigest returns the content digest a spec is pinned to: the
// explicit ImageDigest field when set, otherwise a digest embedded in the
// Image reference. It returns the empty string when the spec is not pinned.
func (s *ContainerSpec) ResolvedImageDigest() string {
	if s == nil {
		return ""
	}
	if s.ImageDigest != "" {
		return s.ImageDigest
	}
	_, digest, pinned := SplitImageReference(s.Image)
	if pinned {
		return digest
	}
	return ""
}

// PinnedImageReference returns the image reference the container runtime
// should be invoked with. When the spec resolves to a content digest, the
// reference is rewritten to "repository@sha256:<hex>" so the runtime
// resolves the image by content rather than by a mutable tag. Unpinned specs
// return the reference unchanged, preserving today's behavior exactly.
func (s *ContainerSpec) PinnedImageReference() string {
	if s == nil {
		return ""
	}
	digest := s.ResolvedImageDigest()
	if digest == "" {
		return s.Image
	}
	return ImageRepository(s.Image) + "@" + digest
}

// IsPinned reports whether the spec resolves to an immutable content digest.
func (s *ContainerSpec) IsPinned() bool {
	return s.ResolvedImageDigest() != ""
}

// validateTrust checks the Tier 0 / Tier 1 / Tier 2 trust fields of a spec.
// It is called from Validate and is a no-op for zero-valued trust fields, so
// specs written before the trust dimension existed validate unchanged.
func (s *ContainerSpec) validateTrust() error {
	if s.ImageDigest != "" {
		if err := ValidateImageDigest(s.ImageDigest); err != nil {
			return err
		}
	}

	// A digest embedded in the image reference and an explicit ImageDigest
	// must agree; disagreement is an operator error that would otherwise
	// silently resolve in favour of one of the two.
	if _, embedded, pinned := SplitImageReference(s.Image); pinned {
		if err := ValidateImageDigest(embedded); err != nil {
			return err
		}
		if s.ImageDigest != "" && s.ImageDigest != embedded {
			return &ImageDigestMismatchError{
				Image:    s.Image,
				Expected: s.ImageDigest,
				Actual:   embedded,
			}
		}
	}

	if !s.TrustLevel.IsValid() {
		return &InputValidationError{
			Field:   "trust_level",
			Message: "must be one of: default, none, pinned, attested, measured",
		}
	}

	// Every tier at or above Pinned rests on image identity, so an
	// unpinned spec cannot satisfy any of them. Rejecting here — rather
	// than at launch — keeps the failure deterministic and offline.
	if s.TrustLevel.RequiresDigestPin() && !s.IsPinned() {
		return &ImageNotPinnedError{Image: s.Image, TrustLevel: s.TrustLevel}
	}

	if s.ImageSignature != nil {
		if err := s.ImageSignature.Validate(); err != nil {
			return err
		}
		if digest := s.ResolvedImageDigest(); digest != "" && s.ImageSignature.Digest != digest {
			return &ImageDigestMismatchError{
				Image:    s.Image,
				Expected: digest,
				Actual:   s.ImageSignature.Digest,
			}
		}
	}

	for index, value := range s.ExpectedPCRs {
		if index > MaxPCRIndex {
			return &InputValidationError{
				Field:   "expected_pcrs",
				Message: "PCR index must be in the range 0-" + intToStr(MaxPCRIndex),
			}
		}
		if len(value) == 0 {
			return &InputValidationError{
				Field:   "expected_pcrs",
				Message: "PCR digest cannot be empty",
			}
		}
	}

	if s.TrustLevel.RequiresHostTPM() && len(s.ExpectedPCRs) == 0 {
		return &InputValidationError{
			Field:   "expected_pcrs",
			Message: "at least one expected PCR is required for trust level " + s.TrustLevel.String(),
		}
	}

	if s.MeasurementPCR < 0 || s.MeasurementPCR > MaxPCRIndex {
		return &InputValidationError{
			Field:   "measurement_pcr",
			Message: "must be in the range 0-" + intToStr(MaxPCRIndex),
		}
	}

	if s.MeasurementBank != "" && !isSupportedPCRBank(s.MeasurementBank) {
		return &InputValidationError{
			Field:   "measurement_bank",
			Message: "must be one of: sha1, sha256, sha384, sha512",
		}
	}

	return nil
}

// MaxPCRIndex is the highest PCR index a TCG PC Client TPM exposes.
const MaxPCRIndex = 23

// DefaultMeasurementBank is the PCR bank used for measured launch when a
// spec does not name one. SHA-256 is the mandatory bank on every TPM 2.0
// PC Client platform.
const DefaultMeasurementBank = "sha256"

// supportedPCRBankNames lists, in ascending digest-size order, the PCR bank
// hash algorithms the measured launch and quote-verification paths accept.
// They match the banks go-xkms tpm2 reads, extends and can parse; the order is
// fixed so every derived lookup is deterministic.
var supportedPCRBankNames = []string{"sha1", "sha256", "sha384", "sha512"}

// supportedPCRBanks is the membership view of supportedPCRBankNames, derived
// rather than restated so the two can never drift.
var supportedPCRBanks = func() map[string]struct{} {
	banks := make(map[string]struct{}, len(supportedPCRBankNames))
	for _, name := range supportedPCRBankNames {
		banks[name] = struct{}{}
	}
	return banks
}()

// isSupportedPCRBank reports whether bank names a PCR bank the measured
// launch path can extend.
func isSupportedPCRBank(bank string) bool {
	_, ok := supportedPCRBanks[bank]
	return ok
}

// EffectiveMeasurementBank returns the PCR bank the spec's measured launch
// extends, defaulting to SHA-256.
func (s *ContainerSpec) EffectiveMeasurementBank() string {
	if s == nil || s.MeasurementBank == "" {
		return DefaultMeasurementBank
	}
	return s.MeasurementBank
}

// =============================================================================
// Deterministic Configuration Digest
// =============================================================================

// ContainerConfigDigest returns a deterministic SHA-256 digest over the parts
// of a spec that determine what the container executes, what it can see, and
// what it is allowed to do: the pinned image reference, entrypoint, command,
// args, environment, mounts (including each mount's kind), ports, working
// directory, network, user, GPU passthrough, auto-remove, labels, the
// privileged flag and the capability add/drop sets.
//
// Privilege is measured for the same reason image content is: a container
// flipped to Privileged, or granted CAP_SYS_ADMIN, is a different workload
// from the one an operator approved, and a digest that ignored the difference
// would certify the safe configuration while the dangerous one ran. Capability
// lists are sorted before hashing, so two specs that grant the same set in a
// different order measure identically — and ExecContainerRuntime emits them in
// that same sorted order, so the measured spec and the launched container
// cannot disagree.
//
// RESOURCE LIMITS ARE MEASURED for the same reason privilege is. A container
// whose memory limit was removed, or whose tmpfs workspace was resized, is not
// the workload an operator approved: an unbounded tmpfs can consume the host's
// memory, and a limit lifted off a measured configuration is a denial-of-
// service dimension that a digest ignoring MemoryBytes would certify as
// unchanged. See ContainerSpec.MemoryBytes and ContainerMount.TmpfsSizeBytes.
//
// Determinism is the whole point: this digest is extended into a PCR at
// measured launch, so two launches of the same approved configuration MUST
// produce the same value. Every map is therefore serialized in sorted key
// order and every field is length-prefixed, so no combination of values can
// alias another (a "A=1,B=2" env pair cannot collide with a single "A=1,B=2"
// key). Go map iteration order is randomized per-process, so a naive range
// over Env or Labels here would silently make measurements non-reproducible;
// TestContainerConfigDigest_DeterministicAcrossMapOrdering guards that.
//
// Deliberately excluded:
//   - Name: a per-instance identifier, frequently randomly generated.
//   - The trust-verification metadata itself (TrustLevel, ExpectedPCRs,
//     ImageSignature, MeasurementPCR/Bank). Those describe how the launch is
//     checked, not what is launched, and including them would make the
//     measurement depend on the policy that verifies it.
//
// TrustLabel IS included: it is an operator-assigned part of the approved
// identity, and folding it in lets one image+config serve two distinct
// measured identities.
func ContainerConfigDigest(spec *ContainerSpec) (string, error) {
	if spec == nil {
		return "", &InputValidationError{Field: "spec", Message: "cannot be nil"}
	}

	h := sha256.New()
	w := &canonicalWriter{h: h}

	w.field("image", spec.PinnedImageReference())
	w.field("digest", spec.ResolvedImageDigest())
	w.field("workdir", spec.WorkDir)
	w.field("network", spec.Network)
	w.field("user", spec.User)
	w.field("trust_label", spec.TrustLabel)
	w.flag("auto_remove", spec.AutoRemove)
	w.flag("gpu_enabled", spec.GPUEnabled)
	w.flag("privileged", spec.Privileged)
	w.number("memory_bytes", spec.MemoryBytes)
	w.number("pids_limit", spec.PidsLimit)
	w.sortedSlice("cap_add", spec.CapAdd)
	w.sortedSlice("cap_drop", spec.CapDrop)
	ulimits := sortedUlimits(spec.Ulimits)
	w.count("ulimits", len(ulimits))
	for _, u := range ulimits {
		w.field("ulimit.name", u.Name)
		w.number("ulimit.soft", u.Soft)
		w.number("ulimit.hard", u.Hard)
	}
	w.slice("entrypoint", spec.Entrypoint)
	w.slice("command", spec.Command)
	w.slice("args", spec.Args)
	w.slice("ports", spec.Ports)
	w.stringMap("env", spec.Env)
	w.stringMap("labels", spec.Labels)

	// Mounts are order-significant on the command line, so they are hashed
	// in declaration order rather than sorted.
	w.count("mounts", len(spec.Mounts))
	for _, m := range spec.Mounts {
		w.field("mount.source", m.Source)
		w.field("mount.target", m.Target)
		w.field("mount.type", m.Type)
		w.flag("mount.readonly", m.ReadOnly)
		w.number("mount.tmpfs_size", m.TmpfsSizeBytes)
	}

	return digestPrefixSHA256 + hex.EncodeToString(h.Sum(nil)), nil
}

// canonicalWriter serializes spec fields into a hash in an unambiguous,
// length-prefixed encoding. Every write is (keyLen, key, valueLen, value) so
// no rearrangement of field names and values can produce the same byte
// stream.
type canonicalWriter struct {
	h interface{ Write([]byte) (int, error) }
}

// write emits a length-prefixed byte string. hash.Hash never returns an
// error from Write (documented in the hash package), so the return is
// discarded deliberately rather than propagated through every call site.
func (w *canonicalWriter) write(b []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(b)))
	_, _ = w.h.Write(length[:])
	_, _ = w.h.Write(b)
}

// field emits a named string value.
func (w *canonicalWriter) field(key, value string) {
	w.write([]byte(key))
	w.write([]byte(value))
}

// flag emits a named boolean value.
func (w *canonicalWriter) flag(key string, value bool) {
	w.write([]byte(key))
	if value {
		w.write([]byte{1})
		return
	}
	w.write([]byte{0})
}

// number emits a named 64-bit integer value in fixed-width big-endian form,
// so a resource limit is measured as a number rather than as its decimal
// spelling: 0 and a limit that was never set cannot be told apart by the
// caller, and both must measure the same.
func (w *canonicalWriter) number(key string, value int64) {
	w.write([]byte(key))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(value))
	w.write(buf[:])
}

// count emits a named element count, so a sequence's length is bound into
// the stream independently of its contents.
func (w *canonicalWriter) count(key string, n int) {
	w.write([]byte(key))
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(n))
	w.write(buf[:])
}

// slice emits a named ordered string sequence.
func (w *canonicalWriter) slice(key string, values []string) {
	w.count(key, len(values))
	for _, v := range values {
		w.write([]byte(v))
	}
}

// sortedSlice emits a named string sequence in sorted order. It is for values
// whose meaning is a set rather than a sequence -- a capability grant, say --
// so that listing the same members in a different order yields the same
// digest. The caller's slice is left untouched.
func (w *canonicalWriter) sortedSlice(key string, values []string) {
	w.slice(key, sortedCopy(values))
}

// sortedCopy returns a sorted copy of values, leaving the input alone. It is
// shared by the digest and by the runtime's argument builder so the measured
// ordering and the launched ordering are produced by the same rule.
func sortedCopy(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	sort.Strings(out)
	return out
}

// stringMap emits a named map in sorted key order, which is what makes the
// digest independent of Go's randomized map iteration order.
func (w *canonicalWriter) stringMap(key string, m map[string]string) {
	w.count(key, len(m))
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.write([]byte(k))
		w.write([]byte(m[k]))
	}
}

// =============================================================================
// Typed Trust Errors
// =============================================================================

// Sentinel errors for the container trust layer.
var (
	// ErrImageNotPinned indicates a spec requires a content-digest pin but
	// supplies only a mutable tag.
	ErrImageNotPinned = errors.New("container trust: image is not pinned to a content digest")

	// ErrImageDigestMismatch indicates the image a runtime resolved does
	// not match the pinned content digest.
	ErrImageDigestMismatch = errors.New("container trust: image digest mismatch")

	// ErrImageDigestUnresolved indicates the runtime could not determine
	// the content digest of an image.
	ErrImageDigestUnresolved = errors.New("container trust: image digest could not be resolved")

	// ErrAttestationUnavailable indicates the attestation capability
	// required by a trust tier is not present on this host.
	ErrAttestationUnavailable = errors.New("container trust: attestation unavailable")

	// ErrPCRMismatch indicates a quoted PCR does not match its expected
	// value.
	ErrPCRMismatch = errors.New("container trust: PCR mismatch")

	// ErrMeasurementFailed indicates the measured-launch PCR extension
	// failed.
	ErrMeasurementFailed = errors.New("container trust: measurement failed")

	// ErrQuoteFailed indicates the host TPM refused or failed to produce a
	// quote.
	ErrQuoteFailed = errors.New("container trust: quote failed")

	// ErrQuoteNotBound indicates the PCR values read back from the TPM could
	// not be bound to the signed quote body, so nothing they say is evidence.
	ErrQuoteNotBound = errors.New("container trust: PCR values are not bound to the signed quote")

	// ErrQuotedPCRDigestMismatch indicates the PCR digest inside the signed
	// quote does not match a recomputation over the PCR values the TPM
	// reported, meaning the two disagree about the state of the host.
	ErrQuotedPCRDigestMismatch = errors.New("container trust: quoted PCR digest mismatch")

	// ErrPCRBankUnavailable indicates the PCR bank a verification step needs
	// is not among the banks the TPM reported.
	ErrPCRBankUnavailable = errors.New("container trust: PCR bank unavailable")
)

// ImageNotPinnedError indicates a spec requires a content-digest pin but the
// image reference carries only a mutable tag.
type ImageNotPinnedError struct {
	Image      string
	TrustLevel TrustLevel
}

// Error implements the error interface.
func (e *ImageNotPinnedError) Error() string {
	return ErrImageNotPinned.Error() + ": image " + e.Image +
		" must be referenced as repository@sha256:<hex> for trust level " + e.TrustLevel.String()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ImageNotPinnedError) Unwrap() error {
	return ErrImageNotPinned
}

// ImageDigestMismatchError indicates the content digest a runtime resolved
// for an image does not match the digest the spec pinned.
type ImageDigestMismatchError struct {
	Image    string
	Expected string
	Actual   string
}

// Error implements the error interface.
func (e *ImageDigestMismatchError) Error() string {
	return ErrImageDigestMismatch.Error() + ": image " + e.Image +
		" expected " + e.Expected + " but resolved " + e.Actual
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ImageDigestMismatchError) Unwrap() error {
	return ErrImageDigestMismatch
}

// ImageDigestUnresolvedError indicates the runtime could not determine the
// content digest of an image, so the pin cannot be verified.
type ImageDigestUnresolvedError struct {
	Image string
	Cause error
}

// Error implements the error interface.
func (e *ImageDigestUnresolvedError) Error() string {
	msg := ErrImageDigestUnresolved.Error() + ": " + e.Image
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is support.
func (e *ImageDigestUnresolvedError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrImageDigestUnresolved}
	}
	return []error{ErrImageDigestUnresolved, e.Cause}
}
