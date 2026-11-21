package service

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
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
