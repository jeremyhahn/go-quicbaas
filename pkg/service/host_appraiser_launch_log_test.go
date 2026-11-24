package service

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
)

// launchLogEvidence builds evidence whose launch log holds one anchored
// launch on PCR 16 and whose quoted PCR 16 is quoted.
func launchLogEvidence(t *testing.T, quoted []byte) *orchestration.HostEvidence {
	t.Helper()
	log := []orchestration.MeasurementLogEntry{
		{Sequence: 0, Kind: orchestration.MeasurementLogAnchor, Base: make([]byte, sha256.Size), PCR: 16, Bank: "sha256"},
	}
	return &orchestration.HostEvidence{
		PCRs:       map[uint32][]byte{16: quoted},
		PCRBank:    "sha256",
		LaunchPCRs: []uint32{16},
		LaunchLog:  log,
	}
}

func TestAppraiseLaunchLog(t *testing.T) {
	zero := make([]byte, sha256.Size)

	t.Run("NoLogBelowMeasuredTier", func(t *testing.T) {
		ev := launchLogEvidence(t, bytes.Repeat([]byte{1}, sha256.Size))
		ev.LaunchLog = nil
		if failure := appraiseLaunchLog(ev); failure != nil {
			t.Fatalf("evidence without a log must not be replayed, got %+v", failure)
		}
	})

	t.Run("ReplayedRegisterPasses", func(t *testing.T) {
		// An anchor-only log replays to its base.
		ev := launchLogEvidence(t, zero)
		ev.LaunchLog = chainedLog(t, ev.LaunchLog)
		if failure := appraiseLaunchLog(ev); failure != nil {
			t.Fatalf("unexpected failure %+v", failure)
		}
	})

	t.Run("DivergentRegisterIsForged", func(t *testing.T) {
		quoted := bytes.Repeat([]byte{7}, sha256.Size)
		ev := launchLogEvidence(t, quoted)
		ev.LaunchLog = chainedLog(t, ev.LaunchLog)
		failure := appraiseLaunchLog(ev)
		if failure == nil || failure.Check != AppraisalCheckLaunchLog || failure.Outcome != string(AppraisalForged) {
			t.Fatalf("expected a forged launch-log failure, got %+v", failure)
		}
		if len(failure.PCRs) != 1 || failure.PCRs[0].Index != 16 || !bytes.Equal(failure.PCRs[0].Quoted, quoted) {
			t.Fatalf("failure must name PCR 16, got %+v", failure.PCRs)
		}
	})

	t.Run("UnquotedLaunchRegisterIsForged", func(t *testing.T) {
		ev := launchLogEvidence(t, zero)
		ev.LaunchLog = chainedLog(t, ev.LaunchLog)
		delete(ev.PCRs, 16)
		if failure := appraiseLaunchLog(ev); failure == nil {
			t.Fatal("a launch register missing from the quote must fail")
		}
	})

	t.Run("BrokenChainIsForged", func(t *testing.T) {
		ev := launchLogEvidence(t, zero)
		if failure := appraiseLaunchLog(ev); failure == nil {
			t.Fatal("a log whose chain does not verify must fail")
		}
	})
}

// chainedLog numbers and chains entries the way the node's log does.
func chainedLog(t *testing.T, entries []orchestration.MeasurementLogEntry) []orchestration.MeasurementLogEntry {
	t.Helper()
	out := orchestration.ChainMeasurementLog(entries)
	if err := orchestration.VerifyMeasurementLog(out); err != nil {
		t.Fatalf("VerifyMeasurementLog: %v", err)
	}
	return out
}

// realLaunchLog is a log of one measured launch on PCR 16, with the
// register value the launch leaves behind.
func realLaunchLog(t *testing.T) ([]orchestration.MeasurementLogEntry, []byte) {
	t.Helper()
	spec := &orchestration.ContainerSpec{
		Image:      "alpine@sha256:" + strings.Repeat("01", sha256.Size),
		TrustLevel: orchestration.TrustLevelMeasured,
		TrustLabel: "launch-log",
	}
	policy := orchestration.LaunchPolicy{
		TrustLevel:      orchestration.TrustLevelMeasured,
		MeasurementPCR:  16,
		MeasurementBank: "sha256",
	}
	measurement, err := orchestration.LaunchMeasurementUnder(spec, policy)
	if err != nil {
		t.Fatalf("LaunchMeasurementUnder: %v", err)
	}
	configDigest, err := orchestration.ContainerConfigDigest(spec)
	if err != nil {
		t.Fatalf("ContainerConfigDigest: %v", err)
	}
	log := chainedLog(t, []orchestration.MeasurementLogEntry{
		{Kind: orchestration.MeasurementLogAnchor, Base: make([]byte, sha256.Size), PCR: 16, Bank: "sha256"},
		{
			Kind: orchestration.MeasurementLogLaunch, Image: spec.Image, ImageDigest: spec.ResolvedImageDigest(),
			ConfigDigest: configDigest, PolicyDigest: policy.Digest(), TrustLabel: spec.TrustLabel,
			PCR: 16, Bank: "sha256", Measurement: measurement,
		},
	})
	h := sha256.Sum256(measurement)
	return log, attestationtest.ExtendSHA256(make([]byte, sha256.Size), h[:])
}

func TestAppraiseHost_ForgedLaunchLogIsRecordedAsFailed(t *testing.T) {
	w, appraiser := newHostAppraiserWorld(t)
	log, launched := realLaunchLog(t)
	pcrs := map[uint32][]byte{16: launched}
	w.putReferenceValues(t, "launched", pcrs)
	results, err := NewAttestationResults(w.store)
	if err != nil {
		t.Fatalf("NewAttestationResults: %v", err)
	}
	appraise := func(t *testing.T, launchLog []orchestration.MeasurementLogEntry) (*orchestration.HostAppraisal, *AttestationResult) {
		t.Helper()
		ev := w.hostEvidence(t, pcrs, "launched")
		ev.PCRBank = "sha256"
		ev.LaunchPCRs = []uint32{16}
		ev.LaunchLog = launchLog
		appraisal, err := appraiser.AppraiseHost(w.ctx, ev)
		if err != nil {
			t.Fatalf("AppraiseHost: %v", err)
		}
		rec, found, err := results.Latest(w.ctx, w.ak.CertificatePEM())
		if err != nil || !found {
			t.Fatalf("Latest: found %v, err %v", found, err)
		}
		return appraisal, &rec.Result
	}
	launchCheck := func(result *AttestationResult) *AppraisalCheck {
		for i := range result.Checks {
			if result.Checks[i].Name == AppraisalCheckLaunchLog {
				return &result.Checks[i]
			}
		}
		return nil
	}

	t.Run("MatchingLogIsRecordedVerified", func(t *testing.T) {
		appraisal, stored := appraise(t, log)
		if !appraisal.Verified || !stored.Verified {
			t.Fatalf("appraisal %+v, stored %+v: want both verified", appraisal, stored)
		}
		if check := launchCheck(stored); check == nil || check.Outcome != AppraisalPass {
			t.Fatalf("stored checks %+v, want a passing launch-log check", stored.Checks)
		}
	})

	t.Run("ForgedLogIsRecordedFailed", func(t *testing.T) {
		// The producer clears a field and re-chains: the chain verifies, the
		// entry no longer matches its measurement.
		forged := append([]orchestration.MeasurementLogEntry(nil), log...)
		forged[1].TrustLabel = "someone-else"
		forged = chainedLog(t, forged)
		appraisal, stored := appraise(t, forged)
		if appraisal.Verified || !appraisal.CryptographicFailure {
			t.Fatalf("appraisal = %+v, want a cryptographic failure", appraisal)
		}
		if stored.Verified || !stored.CryptographicFailure {
			t.Fatalf("stored result = %+v, want it not verified", stored)
		}
		if check := launchCheck(stored); check == nil || check.Outcome != AppraisalForged {
			t.Fatalf("stored checks %+v, want a forged launch-log check", stored.Checks)
		}
		if failure := failureOf(appraisal, AppraisalCheckLaunchLog); failure == nil || failure.Outcome != string(AppraisalForged) {
			t.Fatalf("failures = %+v, want a forged launch log", appraisal.Failures)
		}
	})
}
