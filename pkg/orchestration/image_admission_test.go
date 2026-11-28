package orchestration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
)

// testImageSignature is signature material for testDigest. The test
// runtimes verify it with acceptingImageVerifier; the Verifier's real
// go-xkms check is tested in pkg/service.
func testImageSignature() *ImageSignature {
	return &ImageSignature{
		Digest:         testDigest,
		Signature:      []byte{0x01},
		SignerChainPEM: [][]byte{{0x01}},
	}
}

// acceptingImageVerifier admits every signature over testDigest.
type acceptingImageVerifier struct{}

func (acceptingImageVerifier) VerifyImageSignature(_ context.Context, sig *ImageSignature) error {
	if sig == nil || sig.Digest != testDigest {
		return errors.New("signature does not cover the test digest")
	}
	return nil
}

// acceptingProvenanceVerifier admits provenance for testDigest.
type acceptingProvenanceVerifier struct{}

func (acceptingProvenanceVerifier) VerifyImageProvenance(_ context.Context, spec *ContainerSpec) error {
	if spec.ResolvedImageDigest() != testDigest {
		return errors.New("no provenance covers the image")
	}
	return nil
}

// rejectingProvenanceVerifier finds no provenance.
type rejectingProvenanceVerifier struct{}

func (rejectingProvenanceVerifier) VerifyImageProvenance(context.Context, *ContainerSpec) error {
	return errors.New("no provenance referrer")
}

// listingManifest lists testDigest as a release.
type listingManifest struct{}

func (listingManifest) CheckRelease(_ context.Context, spec *ContainerSpec) error {
	if spec.ResolvedImageDigest() != testDigest {
		return errors.New("the release is not listed")
	}
	return nil
}

// emptyManifest lists no release.
type emptyManifest struct{}

func (emptyManifest) CheckRelease(context.Context, *ContainerSpec) error {
	return errors.New("the release is not listed")
}

// rejectingImageVerifier refuses every signature.
type rejectingImageVerifier struct{}

func (rejectingImageVerifier) VerifyImageSignature(context.Context, *ImageSignature) error {
	return errors.New("signer is not endorsed")
}

func TestTPMAttestingRuntime_DefaultDeny(t *testing.T) {
	reference := map[uint32][]byte{7: make([]byte, sha256.Size)}
	unsignedSpec := func(label string) *ContainerSpec {
		spec := measuredTestSpec(label)
		spec.ImageSignature = nil
		return spec
	}
	// refused asserts a launch was refused before anything was measured.
	refused := func(t *testing.T, rt *TPMAttestingRuntime, base *recordingRuntime, tpm *fakeHostTPM, before []byte) {
		t.Helper()
		if n := len(rt.MeasurementLog().Entries()); n != 0 {
			t.Fatalf("log has %d entries after a refused launch, want 0", n)
		}
		if base.launchCount() != 0 {
			t.Fatal("a refused image must not be launched")
		}
		if got := tpm.pcr(uint32(MeasurementPCRDefault)); !bytes.Equal(got, before) {
			t.Fatal("a refused launch must not extend the measurement PCR")
		}
	}

	t.Run("SignedImageIsVerifiedAndLaunched", func(t *testing.T) {
		tpm := newFakeHostTPM()
		rt, base := newAttestingTestRuntime(t, tpm, measuredReference(t, measuredTestSpec("a")))
		if _, err := rt.Run(t.Context(), measuredTestSpec("a")); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if base.launchCount() != 1 {
			t.Fatal("a verified image must be launched")
		}
		entries := rt.MeasurementLog().Entries()
		if last := entries[len(entries)-1]; last.AllowUnsigned != "" {
			t.Fatalf("a signed launch must not record a grant, got %q", last.AllowUnsigned)
		}
	})

	t.Run("UnsignedImageIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		_, err := rt.Run(t.Context(), unsignedSpec("a"))
		var unsigned *ImageUnsignedError
		if !errors.Is(err, ErrImageUnsigned) || !errors.As(err, &unsigned) || unsigned.ImageDigest != testDigest {
			t.Fatalf("error %v is not an ImageUnsignedError for %s", err, testDigest)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("InteractiveUnsignedImageIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		if _, err := rt.RunInteractive(t.Context(), unsignedSpec("a")); !errors.Is(err, ErrImageUnsigned) {
			t.Fatalf("error %v does not wrap ErrImageUnsigned", err)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("RejectedSignatureIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		WithImageVerifier(rejectingImageVerifier{})(rt)
		if _, err := rt.Run(t.Context(), measuredTestSpec("a")); !errors.Is(err, ErrImageSignatureRejected) {
			t.Fatalf("error %v does not wrap ErrImageSignatureRejected", err)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("SignedImageWithoutVerifierIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		rt.images = nil
		if _, err := rt.Run(t.Context(), measuredTestSpec("a")); !errors.Is(err, ErrImageSignatureRejected) {
			t.Fatalf("error %v does not wrap ErrImageSignatureRejected", err)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("SignedImageWithoutProvenanceIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		WithProvenanceVerifier(rejectingProvenanceVerifier{})(rt)
		_, err := rt.Run(t.Context(), measuredTestSpec("a"))
		var provErr *ImageProvenanceError
		if !errors.Is(err, ErrImageProvenance) || !errors.As(err, &provErr) || provErr.ImageDigest != testDigest {
			t.Fatalf("error %v is not an ImageProvenanceError for %s", err, testDigest)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("SignedImageWithoutProvenanceVerifierIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		rt.provenance = nil
		if _, err := rt.Run(t.Context(), measuredTestSpec("a")); !errors.Is(err, ErrImageProvenance) {
			t.Fatalf("error %v does not wrap ErrImageProvenance", err)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("SignedImageNotInClusterManifestIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		WithClusterManifest(emptyManifest{})(rt)
		_, err := rt.Run(t.Context(), measuredTestSpec("a"))
		var listErr *ReleaseNotListedError
		if !errors.Is(err, ErrReleaseNotListed) || !errors.As(err, &listErr) || listErr.ImageDigest != testDigest {
			t.Fatalf("error %v is not a ReleaseNotListedError for %s", err, testDigest)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("SignedImageWithoutClusterManifestIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		rt.manifest = nil
		if _, err := rt.Run(t.Context(), measuredTestSpec("a")); !errors.Is(err, ErrReleaseNotListed) {
			t.Fatalf("error %v does not wrap ErrReleaseNotListed", err)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("UngrantedLabelIsRefused", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		WithUnsignedImagePolicy(UnsignedImageGrants{"legacy": "tenant-a/legacy-ci"})(rt)
		if _, err := rt.Run(t.Context(), unsignedSpec("other")); !errors.Is(err, ErrImageUnsigned) {
			t.Fatalf("error %v does not wrap ErrImageUnsigned", err)
		}
		refused(t, rt, base, tpm, before)
	})

	t.Run("AllowUnsignedAdmitsAuditsAndLogs", func(t *testing.T) {
		tpm := newFakeHostTPM()
		before := tpm.pcr(uint32(MeasurementPCRDefault))
		const grant = "tenant-a/legacy-ci"
		spec := unsignedSpec("legacy")
		policy, err := LaunchPolicyOf(spec)
		if err != nil {
			t.Fatal(err)
		}
		signedDigest := policy.Digest()
		policy.AllowUnsigned = grant
		granted, err := LaunchMeasurementUnder(spec, policy)
		if err != nil {
			t.Fatal(err)
		}
		// The Verifier compares equal sets, so the reference values carry
		// the launch register the granted launch produces.
		rt, base := newAttestingTestRuntime(t, tpm, map[uint32][]byte{
			7:                             make([]byte, sha256.Size),
			uint32(MeasurementPCRDefault): expectedExtend(before, granted),
		})
		WithUnsignedImagePolicy(UnsignedImageGrants{"legacy": grant})(rt)

		if _, err := rt.Run(t.Context(), spec); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if base.launchCount() != 1 {
			t.Fatal("a granted unsigned image must be launched")
		}

		entries := rt.MeasurementLog().Entries()
		launch := entries[len(entries)-1]
		if launch.Kind != MeasurementLogLaunch || launch.AllowUnsigned != grant {
			t.Fatalf("log must record the grant on the launch, got kind %q grant %q", launch.Kind, launch.AllowUnsigned)
		}

		if launch.PolicyDigest != policy.Digest() || launch.PolicyDigest == signedDigest {
			t.Fatalf("policy digest %s must commit to the grant (signed %s)", launch.PolicyDigest, signedDigest)
		}

		// The PCR commits to the grant: it is the extend of the measurement
		// under the granted policy, not of the spec's signed measurement.
		signed, err := LaunchMeasurement(spec)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(granted, signed) {
			t.Fatal("the granted measurement must differ from the signed one")
		}
		if got := tpm.pcr(uint32(MeasurementPCRDefault)); !bytes.Equal(got, expectedExtend(before, granted)) {
			t.Fatal("the measurement PCR must hold the extend of the granted measurement")
		}
		if replayed, err := ReplayMeasurementLog(entries, MeasurementPCRDefault, "sha256"); err != nil ||
			!bytes.Equal(replayed, tpm.pcr(uint32(MeasurementPCRDefault))) {
			t.Fatalf("the log must replay to the PCR (err %v)", err)
		}

		// The grant is chained: dropping it from the record breaks the log.
		entries[len(entries)-1].AllowUnsigned = ""
		if err := VerifyMeasurementLog(entries); err == nil {
			t.Fatal("erasing the grant from the log must break its chain")
		}
	})

	t.Run("LowerTiersAreNotGated", func(t *testing.T) {
		tpm := newFakeHostTPM()
		rt, base := newAttestingTestRuntime(t, tpm, reference)
		spec := unsignedSpec("a")
		spec.TrustLevel = TrustLevelAttested
		if _, err := rt.Run(t.Context(), spec); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if base.launchCount() != 1 {
			t.Fatal("a launch below the measured tier is not gated by image signatures")
		}
	})
}

func TestUnsignedImageGrants(t *testing.T) {
	grants := UnsignedImageGrants{"legacy": "tenant-a/legacy-ci", "empty": ""}
	if grant, ok := grants.AllowUnsigned(t.Context(), &ContainerSpec{TrustLabel: "legacy"}); !ok || grant != "tenant-a/legacy-ci" {
		t.Fatalf("legacy label: got %q %v", grant, ok)
	}
	for name, spec := range map[string]*ContainerSpec{
		"Nil":        nil,
		"NoLabel":    {},
		"Ungranted":  {TrustLabel: "other"},
		"EmptyGrant": {TrustLabel: "empty"},
	} {
		if _, ok := grants.AllowUnsigned(t.Context(), spec); ok {
			t.Errorf("%s: must not be granted", name)
		}
	}
}

func TestAttest_AllowUnsignedLaunchReportsRecordedMeasurement(t *testing.T) {
	const grant = "tenant-a/legacy-ci"
	spec := measuredTestSpec("legacy")
	spec.ImageSignature = nil
	granted, err := LaunchMeasurementUnder(spec, measuredTestPolicy(spec, grant))
	if err != nil {
		t.Fatal(err)
	}
	signed, err := LaunchMeasurement(spec)
	if err != nil {
		t.Fatal(err)
	}

	tpm := newFakeHostTPM()
	rt, _ := newAttestingTestRuntime(t, tpm, map[uint32][]byte{
		7:                             make([]byte, sha256.Size),
		uint32(MeasurementPCRDefault): expectedExtend(make([]byte, sha256.Size), granted),
	})
	WithUnsignedImagePolicy(UnsignedImageGrants{"legacy": grant})(rt)
	if _, err := rt.Run(t.Context(), spec); err != nil {
		t.Fatalf("Run: %v", err)
	}

	evidence, err := rt.Attest(t.Context(), spec, "container-1")
	if err != nil {
		t.Fatalf("Attest: the Verifier must accept the evidence: %v", err)
	}
	entries := rt.MeasurementLog().Entries()
	recorded := entries[len(entries)-1].Measurement
	if !bytes.Equal(evidence.LaunchMeasurement, recorded) {
		t.Fatal("Attest must report the measurement the log recorded")
	}
	if bytes.Equal(evidence.LaunchMeasurement, signed) {
		t.Fatal("an allow-unsigned launch's measurement must differ from the spec's signed one")
	}
}

func TestAttest_MeasuredSpecNeverRunIsUnavailable(t *testing.T) {
	spec := measuredTestSpec("never-run")
	// The measurement register is untouched and the reference values say
	// so, so the host appraises; only the log lacks a launch of this spec.
	rt, _ := newAttestingTestRuntime(t, newFakeHostTPM(), map[uint32][]byte{
		7:                             make([]byte, sha256.Size),
		uint32(MeasurementPCRDefault): make([]byte, sha256.Size),
	})
	_, err := rt.Attest(t.Context(), spec, "container-1")
	var unavailable *AttestationUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Attest = %v, want *AttestationUnavailableError", err)
	}
}
