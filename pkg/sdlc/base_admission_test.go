// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"testing"
	"time"
)

func baseCheck(name, status, conclusion string, now time.Time) ForgeCheckRun {
	run := ForgeCheckRun{ID: name, Name: name, Status: status, Conclusion: conclusion}
	if status == "completed" {
		run.Completed = now
	}
	return run
}

func TestAdmitOnBase_NotRedAdmitsEverything(t *testing.T) {
	for _, ci := range []CIState{CISuccess, CIPending, CINone} {
		base := BaseState{Branch: "main", HeadSHA: "abc", CI: ci}
		if base.Red() {
			t.Fatalf("%s read as red", ci)
		}
		if !admitOnBase(base, nil, DefaultMainFixLabel) || !admitOnBase(base, []string{"feature"}, DefaultMainFixLabel) {
			t.Fatalf("a %s base refused an issue", ci)
		}
	}
}

func TestAdmitOnBase_RedAdmitsOnlyMainFix(t *testing.T) {
	base := BaseState{Branch: "main", HeadSHA: "abc", CI: CIFailure}
	if !base.Red() {
		t.Fatal("a failed base is not red")
	}
	if admitOnBase(base, nil, DefaultMainFixLabel) || admitOnBase(base, []string{"task:ready", "bug"}, DefaultMainFixLabel) {
		t.Fatal("a red base admitted an issue without the main-fix label")
	}
	if !admitOnBase(base, []string{"task:ready", "main-fix"}, DefaultMainFixLabel) {
		t.Fatal("a red base refused a main-fix issue")
	}
}

func TestAdmitOnBase_LabelMatchIgnoresCaseAndSpaces(t *testing.T) {
	base := BaseState{CI: CIFailure}
	for _, label := range []string{"MAIN-FIX", " Main-Fix ", "main-fix\t"} {
		if !admitOnBase(base, []string{label}, " main-fix ") {
			t.Fatalf("label %q not matched", label)
		}
	}
}

func TestReadBaseState_FailedCheckOnHeadIsRed(t *testing.T) {
	now := time.Now().UTC()
	forge := newFakeForge()
	forge.checks[fakeForgeHeadSHA] = []ForgeCheckRun{
		baseCheck("lint", "completed", "success", now),
		baseCheck("test", "completed", "failure", now),
	}
	base, err := readBaseState(context.Background(), forge, testBinding().Repo(), "main", now)
	if err != nil {
		t.Fatal(err)
	}
	if base.CI != CIFailure || !base.Red() || base.HeadSHA != fakeForgeHeadSHA || base.Branch != "main" {
		t.Fatalf("base = %+v, want red on %s", base, fakeForgeHeadSHA)
	}
}

func TestReadBaseState_SuccessPendingAndNoChecks(t *testing.T) {
	now := time.Now().UTC()
	cases := map[CIState][]ForgeCheckRun{
		CISuccess: {baseCheck("test", "completed", "success", now)},
		CIPending: {baseCheck("test", "in_progress", "", now)},
		CINone:    nil,
	}
	for want, runs := range cases {
		forge := newFakeForge()
		if runs != nil {
			forge.checks[fakeForgeHeadSHA] = runs
		}
		base, err := readBaseState(context.Background(), forge, testBinding().Repo(), "main", now)
		if err != nil {
			t.Fatal(err)
		}
		if base.CI != want || base.Red() {
			t.Fatalf("base = %+v, want %s and not red", base, want)
		}
	}
}

func TestReadBaseState_CheckOnAnotherSHADoesNotCount(t *testing.T) {
	now := time.Now().UTC()
	forge := newFakeForge()
	forge.checks["0ther000000000000000000000000000000000000"] = []ForgeCheckRun{baseCheck("test", "completed", "failure", now)}
	base, err := readBaseState(context.Background(), forge, testBinding().Repo(), "main", now)
	if err != nil {
		t.Fatal(err)
	}
	if base.CI != CINone || base.Red() {
		t.Fatalf("base = %+v, want none: the failure is on another commit", base)
	}
}

func TestReadBaseState_BlankBranchReadsMain(t *testing.T) {
	forge := newFakeForge()
	forge.branches = map[string]string{}
	forge.branches["main"] = "a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1a1"
	forge.onlyKnownBranches = true
	base, err := readBaseState(context.Background(), forge, testBinding().Repo(), "  ", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if base.Branch != FallbackDefaultBranch || base.HeadSHA != forge.branches["main"] {
		t.Fatalf("base = %+v, want the head of main", base)
	}
}

func TestReadBaseState_ForgeErrorsAreReturned(t *testing.T) {
	boom := errors.New("forge down")
	forge := newFakeForge()
	forge.getBranchErr = boom
	if _, err := readBaseState(context.Background(), forge, testBinding().Repo(), "main", time.Now()); !errors.Is(err, boom) {
		t.Fatalf("GetBranch error = %v, want %v", err, boom)
	}
	forge = newFakeForge()
	forge.listChecksErr = boom
	if _, err := readBaseState(context.Background(), forge, testBinding().Repo(), "main", time.Now()); !errors.Is(err, boom) {
		t.Fatalf("ListCheckRuns error = %v, want %v", err, boom)
	}
}
