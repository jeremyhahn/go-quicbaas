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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A car with an untrusted commit is ejected, never looped
// =============================================================================
//
// Measured on merge train #1 of go-luks2 (2026-09-25): the push verification
// refused a car's pre-rotation commit, the whole train was abandoned as a
// failure "no one change's", its cars went back to the queue, and the next
// train failed identically — for ever, with no attention item.

// carLogLine is the verification log line for the car's range.
const carLogCommand = "log " + verifySignedFormat + " HEAD..abc123"

// signingScriptedBuilder is a scripted builder whose project signs with the
// test key and trusts retiredSigningKey until testRetiredAt.
func signingScriptedBuilder(git *scriptedGit) *containerTrainBuilder {
	builder := newScriptedBuilder(git)
	builder.signing = &SigningSession{PublicKey: testSigningKey}
	builder.publisher.signing = builder.signing
	builder.project.Git.Signing.Retired = []orchestration.RetiredSigningKey{
		{PublicKey: retiredSigningKey, RetiredAt: testRetiredAt},
	}
	return builder
}

func TestTrainBuild_ACarWithAnUntrustedCommitIsEjectedBeforeItIsMerged(t *testing.T) {
	stranger := outgoingCommit{SHA: "0badc0ffee", Status: "U", Signer: "SHA256:stranger"}
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		carLogCommand:          {Stdout: stranger.logLine() + "\n"},
	}}
	_, ejection, err := signingScriptedBuilder(git).Add(buildCar)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if ejection == nil || ejection.Reason != TrainEjectUnsigned {
		t.Fatalf("ejection = %+v, want unsigned", ejection)
	}
	for _, want := range []string{"0badc0f", `"U"`, "SHA256:stranger"} {
		if !strings.Contains(ejection.Detail, want) {
			t.Fatalf("detail %q does not name %q", ejection.Detail, want)
		}
	}
	if git.ranLine("merge") {
		t.Fatal("a car with an untrusted commit was merged onto the train")
	}
}

func TestTrainBuild_ACarSignedByTheRetiredKeyBeforeItsRetirementRides(t *testing.T) {
	pre := outgoingCommit{SHA: "df8ae49c", Status: "G", Signer: retiredSigningFingerprint, Time: testRetiredAt.Add(-72 * time.Hour)}
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		"rev-parse HEAD":       {Stdout: "merged1\n"},
		carLogCommand:          {Stdout: pre.logLine() + "\n"},
	}}
	commit, ejection, err := signingScriptedBuilder(git).Add(buildCar)
	if err != nil || ejection != nil || commit != "merged1" {
		t.Fatalf("Add = %q %+v %v, want the car merged", commit, ejection, err)
	}
}

func TestTrainBuild_ACarWhoseCommitsGitCannotListIsAnError(t *testing.T) {
	git := &scriptedGit{answers: map[string]*orchestration.ExecResult{
		"rev-parse FETCH_HEAD": {Stdout: "abc123\n"},
		carLogCommand:          {ExitCode: 128, Stderr: "fatal: bad revision"},
	}}
	_, ejection, err := signingScriptedBuilder(git).Add(buildCar)
	var cmdErr *PublishCommandError
	if !errors.As(err, &cmdErr) || ejection != nil {
		t.Fatalf("Add = %+v %v, want the listing failure reported, not an ejection", ejection, err)
	}
}

// unsignIt is a train ejecting the change for an untrusted commit.
func unsignIt(entry *TrainEntry) {
	entry.State = TrainEntryEjected
	entry.Train = 1
	entry.Ejection = &TrainEjection{Reason: TrainEjectUnsigned, Train: 1,
		Detail: "Commit `df8ae49` is not signed by a key this project trusts for it."}
}

// TestTrainedDelivery_AnUnsignedEjectionIsHeldForAPerson: the delivery does
// not go back to the gate, which would approve the same head for the next
// train to eject again; it parks, escalated, naming the commit.
func TestTrainedDelivery_AnUnsignedEjectionIsHeldForAPerson(t *testing.T) {
	rig, _ := trainedRunnerRig(t, unsignIt)
	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("err = %v, want the attempt parked", err)
	}
	parked := job.Parked()
	if parked == nil || parked.Kind != orchestration.TaskBlockKindEscalated || parked.FaultClass != orchestration.FaultUntrustedSigner || !strings.Contains(parked.Message, "df8ae49") {
		t.Fatalf("parked = %+v, want escalated naming the commit", parked)
	}
	if ejections := rig.auditDetails(auditTrainEjected); len(ejections) != 1 || !strings.Contains(ejections[0], "unsigned") {
		t.Fatalf("ejection audits = %v", ejections)
	}
}

func TestTrainEjectedComment_AnUnsignedChangeIsHeldNotSentBack(t *testing.T) {
	comment := trainEjectedComment(TrainEjection{Reason: TrainEjectUnsigned, Train: 2, Detail: "Commit `abc` ..."})
	if !strings.Contains(comment, "held for a person") || strings.Contains(comment, "sends the change back to its worker") {
		t.Fatalf("comment:\n%s", comment)
	}
	if got := trainEjectNextStep(TrainEjectConflict); !strings.Contains(got, "back to its worker") {
		t.Fatalf("a conflict's next step = %q", got)
	}
}
