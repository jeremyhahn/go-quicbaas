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

package orchestration_test

import (
	"errors"
	"testing"

	pkgorch "github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	sdkorch "github.com/jeremyhahn/go-quicbaas/sdk/go/orchestration"
)

const testDigest = "sha256:" +
	"0000000000000000000000000000000000000000000000000000000000000001"

// TestSpecCrossesBoundaryUnconverted is the property the whole package exists
// for: a spec built with SDK type names is the same type pkg/orchestration
// declares, so it can be handed to upstream code with no conversion.
func TestSpecCrossesBoundaryUnconverted(t *testing.T) {
	spec := &sdkorch.ContainerSpec{
		Image:       "registry.example.com/agent:v1",
		ImageDigest: testDigest,
		TrustLevel:  sdkorch.TrustLevelPinned,
		TrustLabel:  "approved-agent",
		Mounts: []sdkorch.ContainerMount{
			{Source: "/host/work", Target: "/work", ReadOnly: true},
		},
	}

	var upstream *pkgorch.ContainerSpec = spec
	if err := upstream.Validate(); err != nil {
		t.Fatalf("Validate() on a pinned spec: %v", err)
	}

	want := "registry.example.com/agent:v1@" + testDigest
	if got := spec.PinnedImageReference(); got != want {
		t.Errorf("PinnedImageReference() = %q, want %q", got, want)
	}
	if !spec.IsPinned() {
		t.Error("IsPinned() = false, want true for a digest-pinned spec")
	}
	if got := spec.EffectiveMeasurementPCR(); got != sdkorch.MeasurementPCRDefault {
		t.Errorf("EffectiveMeasurementPCR() = %d, want %d", got, sdkorch.MeasurementPCRDefault)
	}
	if got := spec.EffectiveMeasurementBank(); got != sdkorch.DefaultMeasurementBank {
		t.Errorf("EffectiveMeasurementBank() = %q, want %q", got, sdkorch.DefaultMeasurementBank)
	}
}

// TestConfigDigestMatchesUpstream proves the aliased function is the upstream
// function, not a re-derivation that could drift: a measurement taken through
// the SDK must equal one taken through pkg/orchestration.
func TestConfigDigestMatchesUpstream(t *testing.T) {
	spec := &sdkorch.ContainerSpec{
		Image:       "registry.example.com/agent",
		ImageDigest: testDigest,
		Env:         map[string]string{"B": "2", "A": "1"},
		TrustLabel:  "approved-agent",
	}

	viaSDK, err := sdkorch.ContainerConfigDigest(spec)
	if err != nil {
		t.Fatalf("sdk ContainerConfigDigest: %v", err)
	}
	viaPkg, err := pkgorch.ContainerConfigDigest(spec)
	if err != nil {
		t.Fatalf("pkg ContainerConfigDigest: %v", err)
	}
	if viaSDK != viaPkg {
		t.Fatalf("config digest diverged: sdk %q, pkg %q", viaSDK, viaPkg)
	}

	measurement, err := sdkorch.LaunchMeasurement(spec)
	if err != nil {
		t.Fatalf("LaunchMeasurement: %v", err)
	}
	if len(measurement) == 0 {
		t.Error("LaunchMeasurement returned an empty measurement")
	}
}

// TestTrustResolutionRoundTrip exercises the trust axis through the aliases,
// covering both the permitted and the denied path.
func TestTrustResolutionRoundTrip(t *testing.T) {
	level, err := sdkorch.ParseTrustLevel("measured")
	if err != nil {
		t.Fatalf("ParseTrustLevel(measured): %v", err)
	}
	if level != sdkorch.TrustLevelMeasured {
		t.Fatalf("ParseTrustLevel(measured) = %v, want %v", level, sdkorch.TrustLevelMeasured)
	}
	if got := level.String(); got != "measured" {
		t.Errorf("TrustLevel.String() = %q, want %q", got, "measured")
	}

	policy, err := sdkorch.ParseTrustPolicy("attested")
	if err != nil {
		t.Fatalf("ParseTrustPolicy(attested): %v", err)
	}

	caps := sdkorch.TrustCapabilities{DigestPinning: true, HostTPM: true, MeasuredLaunch: true}
	resolved, err := sdkorch.ResolveTrustLevel(sdkorch.TrustLevelDefault, policy, nil, caps)
	if err != nil {
		t.Fatalf("ResolveTrustLevel on a fully capable host: %v", err)
	}
	if resolved != sdkorch.TrustLevelAttested {
		t.Errorf("resolved = %v, want %v", resolved, sdkorch.TrustLevelAttested)
	}

	// A host with no TPM cannot satisfy the policy floor and must be denied
	// rather than silently downgraded.
	_, err = sdkorch.ResolveTrustLevel(
		sdkorch.TrustLevelAttested, policy, nil, sdkorch.TrustCapabilities{DigestPinning: true})
	var violation *sdkorch.TrustLevelViolationError
	if !errors.As(err, &violation) {
		t.Fatalf("ResolveTrustLevel on a TPM-less host = %v, want *TrustLevelViolationError", err)
	}
	if violation.Requested != sdkorch.TrustLevelAttested {
		t.Errorf("violation.Requested = %v, want %v", violation.Requested, sdkorch.TrustLevelAttested)
	}
}

// TestErrorIdentityAcrossBoundary confirms the aliases share sentinel values
// and concrete types with upstream, so errors.Is and errors.As match errors
// raised anywhere in the stack regardless of which spelling the caller used.
func TestErrorIdentityAcrossBoundary(t *testing.T) {
	unpinned := &sdkorch.ContainerSpec{
		Image:      "registry.example.com/agent:latest",
		TrustLevel: sdkorch.TrustLevelPinned,
	}
	err := unpinned.Validate()
	if err == nil {
		t.Fatal("Validate() on an unpinned Tier 0 spec = nil, want an error")
	}
	if !errors.Is(err, sdkorch.ErrImageNotPinned) {
		t.Errorf("errors.Is(err, sdk.ErrImageNotPinned) = false for %v", err)
	}
	if !errors.Is(err, pkgorch.ErrImageNotPinned) {
		t.Errorf("errors.Is(err, pkg.ErrImageNotPinned) = false for %v", err)
	}

	var notPinned *pkgorch.ImageNotPinnedError
	if !errors.As(err, &notPinned) {
		t.Fatalf("errors.As to *pkgorch.ImageNotPinnedError failed for %v", err)
	}
	if notPinned.TrustLevel != sdkorch.TrustLevelPinned {
		t.Errorf("TrustLevel = %v, want %v", notPinned.TrustLevel, sdkorch.TrustLevelPinned)
	}

	// Invalid input travels the same way.
	var invalid *sdkorch.InputValidationError
	if got := sdkorch.ValidateImageDigest("sha256:nothex"); !errors.As(got, &invalid) {
		t.Fatalf("ValidateImageDigest(bad) = %v, want *InputValidationError", got)
	}
	if invalid.Field != "image_digest" {
		t.Errorf("InputValidationError.Field = %q, want %q", invalid.Field, "image_digest")
	}
	if err := sdkorch.ValidateImageDigest(testDigest); err != nil {
		t.Errorf("ValidateImageDigest(valid) = %v, want nil", err)
	}
}

// TestPrivilegeFieldsCrossBoundaryAndMeasure is the reason a consumer no longer
// needs a label carrier for privilege: the fields are on the shared spec type,
// and they change the measurement taken through either spelling.
func TestPrivilegeFieldsCrossBoundaryAndMeasure(t *testing.T) {
	safe := &sdkorch.ContainerSpec{Image: "agent", ImageDigest: testDigest}
	privileged := &sdkorch.ContainerSpec{
		Image:       "agent",
		ImageDigest: testDigest,
		Privileged:  true,
		CapAdd:      []string{"CAP_SYS_ADMIN"},
		CapDrop:     []string{"ALL"},
		Mounts: []sdkorch.ContainerMount{
			{Target: "/scratch", Type: sdkorch.MountTypeTmpfs},
		},
	}

	// The spec is the upstream type, so upstream validation and measurement
	// see the new fields without conversion.
	var upstream *pkgorch.ContainerSpec = privileged
	if err := upstream.Validate(); err != nil {
		t.Fatalf("Validate() on a privileged spec: %v", err)
	}

	safeDigest, err := sdkorch.ContainerConfigDigest(safe)
	if err != nil {
		t.Fatalf("ContainerConfigDigest(safe): %v", err)
	}
	privilegedDigest, err := pkgorch.ContainerConfigDigest(privileged)
	if err != nil {
		t.Fatalf("ContainerConfigDigest(privileged): %v", err)
	}
	if safeDigest == privilegedDigest {
		t.Fatal("a privileged spec must not measure the same as a safe one")
	}

	if sdkorch.MountTypeBind != pkgorch.MountTypeBind ||
		sdkorch.MountTypeVolume != pkgorch.MountTypeVolume ||
		sdkorch.MountTypeTmpfs != pkgorch.MountTypeTmpfs {
		t.Fatal("mount kind constants diverged from upstream")
	}
}

// TestUlimitsCrossBoundaryAndMeasure: a consumer can raise a container's
// locked-memory limit through the SDK spelling, the upstream validation refuses
// a limit the runtime would, and the limit changes the measurement.
func TestUlimitsCrossBoundaryAndMeasure(t *testing.T) {
	base := &sdkorch.ContainerSpec{Image: "agent", ImageDigest: testDigest}
	memlock := &sdkorch.ContainerSpec{
		Image:       "agent",
		ImageDigest: testDigest,
		Ulimits:     []sdkorch.ContainerUlimit{{Name: "memlock", Soft: 4 << 30, Hard: 4 << 30}},
	}
	if err := memlock.Validate(); err != nil {
		t.Fatalf("Validate() on a memlock spec: %v", err)
	}
	baseDigest, err := sdkorch.ContainerConfigDigest(base)
	if err != nil {
		t.Fatalf("ContainerConfigDigest(base): %v", err)
	}
	memlockDigest, err := sdkorch.ContainerConfigDigest(memlock)
	if err != nil {
		t.Fatalf("ContainerConfigDigest(memlock): %v", err)
	}
	if baseDigest == memlockDigest {
		t.Fatal("a raised memlock limit must change the measurement")
	}

	invalid := &sdkorch.ContainerSpec{
		Image:   "agent",
		Ulimits: []sdkorch.ContainerUlimit{{Name: "memlock", Soft: 2, Hard: 1}},
	}
	if err := invalid.Validate(); !errors.Is(err, pkgorch.ErrInvalidInput) {
		t.Fatalf("Validate() with soft above hard = %v, want ErrInvalidInput", err)
	}
}

// TestQuoteBindingErrorsCrossBoundary keeps the quote-binding failures usable
// through the SDK spelling: a consumer must be able to tell "the host drifted"
// from "the evidence does not hang together".
func TestQuoteBindingErrorsCrossBoundary(t *testing.T) {
	var mismatch error = &sdkorch.QuotedPCRDigestMismatchError{
		Image:  "agent",
		Bank:   "sha256",
		Signed: []byte{0x01},
		Cause:  errors.New("reported PCR values do not hash to the signed PCR digest"),
	}
	if !errors.Is(mismatch, sdkorch.ErrQuotedPCRDigestMismatch) {
		t.Error("errors.Is(err, sdk.ErrQuotedPCRDigestMismatch) = false")
	}
	if !errors.Is(mismatch, pkgorch.ErrQuoteNotBound) {
		t.Error("errors.Is(err, pkg.ErrQuoteNotBound) = false")
	}
	var upstreamMismatch *pkgorch.QuotedPCRDigestMismatchError
	if !errors.As(mismatch, &upstreamMismatch) {
		t.Fatal("errors.As to *pkgorch.QuotedPCRDigestMismatchError failed")
	}

	var binding error = &sdkorch.QuoteBindingError{Image: "agent", Reason: "stale nonce"}
	if !errors.Is(binding, pkgorch.ErrQuoteNotBound) {
		t.Error("errors.Is(binding, pkg.ErrQuoteNotBound) = false")
	}

	var unavailable error = &sdkorch.PCRBankUnavailableError{Requested: "sha384"}
	if !errors.Is(unavailable, sdkorch.ErrPCRBankUnavailable) {
		t.Error("errors.Is(unavailable, sdk.ErrPCRBankUnavailable) = false")
	}
}

// TestSplitImageReference covers both branches of the pinned/unpinned split.
func TestSplitImageReference(t *testing.T) {
	repo, digest, pinned := sdkorch.SplitImageReference("alpine@" + testDigest)
	if repo != "alpine" || digest != testDigest || !pinned {
		t.Errorf("SplitImageReference(pinned) = (%q, %q, %v)", repo, digest, pinned)
	}

	repo, digest, pinned = sdkorch.SplitImageReference("alpine:3.20")
	if repo != "alpine:3.20" || digest != "" || pinned {
		t.Errorf("SplitImageReference(tagged) = (%q, %q, %v)", repo, digest, pinned)
	}
	if got := sdkorch.ImageRepository("alpine@" + testDigest); got != "alpine" {
		t.Errorf("ImageRepository = %q, want %q", got, "alpine")
	}
}

// TestConstructorsRejectNilDependencies is the error-path counterpart of the
// constructor aliases: a nil dependency must fail closed rather than produce a
// runtime that advertises tiers it cannot satisfy.
func TestConstructorsRejectNilDependencies(t *testing.T) {
	if _, err := sdkorch.NewTPMAttestingRuntime(nil, nil, nil); err == nil {
		t.Error("NewTPMAttestingRuntime(nil, nil, nil) = nil error, want a validation error")
	}
	if _, err := sdkorch.NewExecContainerRuntimeWithCommand("", nil); err == nil {
		t.Error("NewExecContainerRuntimeWithCommand(\"\", nil) = nil error, want a validation error")
	}
}
