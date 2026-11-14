package orchestration

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func measuredTestSpec(label string) *ContainerSpec {
	return &ContainerSpec{
		Image:           "alpine@" + testDigest,
		TrustLevel:      TrustLevelMeasured,
		TrustLabel:      label,
		ReferenceValues: "golden",
	}
}

func TestLaunchPolicyDigest(t *testing.T) {
	base := measuredTestSpec("prod")
	baseDigest, err := LaunchPolicyDigest(base)
	if err != nil {
		t.Fatalf("LaunchPolicyDigest: %v", err)
	}
	if !strings.HasPrefix(baseDigest, "sha256:") || len(baseDigest) != len("sha256:")+2*sha256.Size {
		t.Fatalf("policy digest %q is not a sha256 digest", baseDigest)
	}
	again, _ := LaunchPolicyDigest(measuredTestSpec("prod"))
	if again != baseDigest {
		t.Fatal("policy digest is not deterministic")
	}

	variants := map[string]*ContainerSpec{
		"DifferentLevel": {Image: base.Image, TrustLevel: TrustLevelAttested, TrustLabel: "prod", ReferenceValues: "golden"},
		"DifferentPCR":   {Image: base.Image, TrustLevel: TrustLevelMeasured, TrustLabel: "prod", ReferenceValues: "golden", MeasurementPCR: 12},
		"DifferentBank":  {Image: base.Image, TrustLevel: TrustLevelMeasured, TrustLabel: "prod", ReferenceValues: "golden", MeasurementBank: "sha384"},
	}
	baseMeasurement, err := LaunchMeasurement(base)
	if err != nil {
		t.Fatalf("LaunchMeasurement: %v", err)
	}
	for name, spec := range variants {
		t.Run(name, func(t *testing.T) {
			got, err := LaunchPolicyDigest(spec)
			if err != nil {
				t.Fatalf("LaunchPolicyDigest: %v", err)
			}
			if got == baseDigest {
				t.Fatalf("%s must change the policy digest", name)
			}
			measurement, err := LaunchMeasurement(spec)
			if err != nil {
				t.Fatalf("LaunchMeasurement: %v", err)
			}
			if bytes.Equal(measurement, baseMeasurement) {
				t.Fatalf("%s must change the launch measurement", name)
			}
		})
	}

	t.Run("MeasurementCarriesPolicyDigest", func(t *testing.T) {
		if !bytes.Contains(baseMeasurement, []byte(baseDigest)) {
			t.Fatal("the launch measurement must bind the policy digest")
		}
	})

	t.Run("NilSpecRejected", func(t *testing.T) {
		if _, err := LaunchPolicyDigest(nil); err == nil {
			t.Fatal("expected an error for a nil spec")
		}
	})
}

func TestTPMAttestingRuntime_MeasurementLog(t *testing.T) {
	reference := map[uint32][]byte{7: make([]byte, sha256.Size)}

	t.Run("EachMeasuredLaunchIsLoggedAndReplays", func(t *testing.T) {
		tpm := newFakeHostTPM()
		rt, _ := newAttestingTestRuntime(t, tpm, reference)

		specs := []*ContainerSpec{measuredTestSpec("a"), measuredTestSpec("b"), measuredTestSpec("c")}
		for _, spec := range specs {
			if _, err := rt.Run(t.Context(), spec); err != nil {
				t.Fatalf("Run: %v", err)
			}
		}

		entries := rt.MeasurementLog().Entries()
		if len(entries) != len(specs) {
			t.Fatalf("log has %d entries, want %d", len(entries), len(specs))
		}
		for i, spec := range specs {
			e := entries[i]
			configDigest, _ := ContainerConfigDigest(spec)
			policyDigest, _ := LaunchPolicyDigest(spec)
			measurement, _ := LaunchMeasurement(spec)
			if e.Sequence != uint64(i) || e.ImageDigest != testDigest ||
				e.ConfigDigest != configDigest || e.PolicyDigest != policyDigest ||
				e.TrustLabel != spec.TrustLabel || e.PCR != MeasurementPCRDefault ||
				e.Bank != DefaultMeasurementBank || !bytes.Equal(e.Measurement, measurement) {
				t.Fatalf("entry %d = %+v does not record the launch", i, e)
			}
		}

		got, err := ReplayMeasurementLog(entries, MeasurementPCRDefault, DefaultMeasurementBank)
		if err != nil {
			t.Fatalf("ReplayMeasurementLog: %v", err)
		}
		if want := tpm.pcr(uint32(MeasurementPCRDefault)); !bytes.Equal(got, want) {
			t.Fatalf("replay = %x, PCR holds %x", got, want)
		}
	})

	t.Run("FailedExtendLeavesNoEntry", func(t *testing.T) {
		tpm := newFakeHostTPM()
		tpm.extendErr = errors.New("pcr locked")
		rt, base := newAttestingTestRuntime(t, tpm, reference)

		if _, err := rt.Run(t.Context(), measuredTestSpec("a")); !errors.Is(err, ErrMeasurementFailed) {
			t.Fatalf("error %v does not wrap ErrMeasurementFailed", err)
		}
		if n := len(rt.MeasurementLog().Entries()); n != 0 {
			t.Fatalf("log has %d entries after a failed extend, want 0", n)
		}
		if base.launchCount() != 0 {
			t.Fatal("the container must not be launched when the measurement fails")
		}
	})

	t.Run("LowerTiersAreNotLogged", func(t *testing.T) {
		rt, _ := newAttestingTestRuntime(t, newFakeHostTPM(), reference)
		spec := measuredTestSpec("a")
		spec.TrustLevel = TrustLevelAttested
		if _, err := rt.Run(t.Context(), spec); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if n := len(rt.MeasurementLog().Entries()); n != 0 {
			t.Fatalf("log has %d entries below the measured tier, want 0", n)
		}
	})
}

func TestVerifyMeasurementLog(t *testing.T) {
	log := NewMeasurementLog()
	for _, label := range []string{"a", "b", "c"} {
		measurement, err := LaunchMeasurement(measuredTestSpec(label))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.record(MeasurementLogEntry{
			ImageDigest: testDigest, TrustLabel: label, PCR: MeasurementPCRDefault,
			Bank: DefaultMeasurementBank, Measurement: measurement,
		}, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	entries := log.Entries()
	if err := VerifyMeasurementLog(entries); err != nil {
		t.Fatalf("VerifyMeasurementLog: %v", err)
	}

	tamper := map[string]func([]MeasurementLogEntry) []MeasurementLogEntry{
		"AlteredMeasurement": func(e []MeasurementLogEntry) []MeasurementLogEntry { e[1].Measurement[0] ^= 0xFF; return e },
		"AlteredPolicy":      func(e []MeasurementLogEntry) []MeasurementLogEntry { e[0].PolicyDigest = "sha256:00"; return e },
		"DroppedEntry":       func(e []MeasurementLogEntry) []MeasurementLogEntry { return append(e[:1], e[2:]...) },
		"Reordered":          func(e []MeasurementLogEntry) []MeasurementLogEntry { e[0], e[1] = e[1], e[0]; return e },
	}
	for name, mutate := range tamper {
		t.Run(name, func(t *testing.T) {
			forged := mutate(log.Entries())
			var logErr *MeasurementLogError
			if err := VerifyMeasurementLog(forged); !errors.As(err, &logErr) {
				t.Fatalf("expected *MeasurementLogError, got %v", err)
			}
			if _, err := ReplayMeasurementLog(forged, MeasurementPCRDefault, DefaultMeasurementBank); err == nil {
				t.Fatal("replay must refuse a forged log")
			}
		})
	}

	t.Run("EntriesAreCopies", func(t *testing.T) {
		copied := log.Entries()
		copied[0].Measurement[0] ^= 0xFF
		if err := VerifyMeasurementLog(log.Entries()); err != nil {
			t.Fatalf("mutating a returned entry changed the log: %v", err)
		}
	})

	t.Run("ReplaySkipsOtherRegistersAndRejectsUnknownBank", func(t *testing.T) {
		got, err := ReplayMeasurementLog(entries, 12, DefaultMeasurementBank)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, make([]byte, sha256.Size)) {
			t.Fatal("a register no entry names replays to zero")
		}
		if _, err := ReplayMeasurementLog(entries, MeasurementPCRDefault, "sha1"); err == nil {
			t.Fatal("expected sha1 to be refused")
		}
	})
}
