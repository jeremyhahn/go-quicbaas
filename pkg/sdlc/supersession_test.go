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
	"strconv"
	"strings"
	"testing"
	"time"
)

// observed is the fixed clock every record in this file is stamped with, so a
// test asserting order asserts the order the code chose rather than the order
// the machine happened to produce.
var observed = time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

// cancellableForge is the fake forge with Gitea's missing capability declared,
// which is what a platform WITH a cancel route looks like to this code.
func cancellableForge() *fakeForge {
	forge := newFakeForge()
	forge.caps.CancelCheckRuns = true
	return forge
}

// deliveryOn is a delivery sitting on head with the given checks recorded.
func deliveryOn(head string, checks ...CheckRecord) *Delivery {
	return &Delivery{TaskID: testTask, ProjectID: testProject, Attempt: 1, HeadSHA: head, Checks: checks}
}

// =============================================================================
// supersedeChecks — the observation
// =============================================================================

// TestSupersedeChecks_RecordsEveryDisplacedRunAndWhatBecameOfIt is the whole
// of the observation half: which runs a moved head displaces, and what the
// engine was able to do about each.
func TestSupersedeChecks_RecordsEveryDisplacedRunAndWhatBecameOfIt(t *testing.T) {
	cases := map[string]struct {
		forge    *fakeForge
		delivery *Delivery
		newHead  string

		want          []SupersessionRecord
		wantCancelled []string
	}{
		"a finished run is recorded and nothing is cancelled": {
			forge:    cancellableForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckSuccess}),
			newHead:  "h2",
			want: []SupersessionRecord{{
				Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
				Status: CheckSuccess, Disposition: SupersessionFinished, ObservedAt: observed,
			}},
		},
		"an in-flight run on a forge with no cancel route is an orphan": {
			forge:    newFakeForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending}),
			newHead:  "h2",
			want: []SupersessionRecord{{
				Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
				Status: CheckPending, Disposition: SupersessionOrphaned,
				Detail: "gitea publishes no route to cancel a running check", ObservedAt: observed,
			}},
		},
		"an in-flight run on a forge that can cancel is cancelled": {
			forge:    cancellableForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending}),
			newHead:  "h2",
			want: []SupersessionRecord{{
				Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
				Status: CheckPending, Disposition: SupersessionCancelled, ObservedAt: observed,
			}},
			wantCancelled: []string{"7"},
		},
		"an in-flight run with no addressable id cannot be cancelled": {
			forge:    cancellableForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", HeadSHA: "h1", Status: CheckPending}),
			newHead:  "h2",
			want: []SupersessionRecord{{
				Name: "ci", HeadSHA: "h1", SupersededBy: "h2",
				Status: CheckPending, Disposition: SupersessionOrphaned,
				Detail: "the forge reported no run id to cancel", ObservedAt: observed,
			}},
		},
		"every displaced context is recorded, not only the first": {
			forge: newFakeForge(),
			delivery: deliveryOn("h1",
				CheckRecord{Name: "build", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending},
				CheckRecord{Name: "test", ForgeRunID: "8", HeadSHA: "h1", Status: CheckFailure},
			),
			newHead: "h2",
			want: []SupersessionRecord{
				{
					Name: "build", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
					Status: CheckPending, Disposition: SupersessionOrphaned,
					Detail: "gitea publishes no route to cancel a running check", ObservedAt: observed,
				},
				{
					Name: "test", ForgeRunID: "8", HeadSHA: "h1", SupersededBy: "h2",
					Status: CheckFailure, Disposition: SupersessionFinished, ObservedAt: observed,
				},
			},
		},
		"a check bound to some other head is not this displacement": {
			forge:    newFakeForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h0", Status: CheckPending}),
			newHead:  "h2",
			want:     nil,
		},
		"a head that did not move supersedes nothing": {
			forge:    newFakeForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending}),
			newHead:  "h1",
			want:     nil,
		},
		"a delivery with no head yet has nothing to supersede": {
			forge:    newFakeForge(),
			delivery: deliveryOn("", CheckRecord{Name: "ci", ForgeRunID: "7", Status: CheckPending}),
			newHead:  "h2",
			want:     nil,
		},
		"an empty new head is not a displacement": {
			forge:    newFakeForge(),
			delivery: deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending}),
			newHead:  "   ",
			want:     nil,
		},
		"a nil delivery answers nothing rather than panicking": {
			forge:    newFakeForge(),
			delivery: nil,
			newHead:  "h2",
			want:     nil,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := supersedeChecks(context.Background(), tc.forge, testRepoRef, tc.delivery, tc.newHead, observed)
			assertRecords(t, got, tc.want)
			cancelled := tc.forge.cancelledRuns()
			if len(cancelled) != len(tc.wantCancelled) {
				t.Fatalf("cancelled = %v, want %v", cancelled, tc.wantCancelled)
			}
			for i := range cancelled {
				if cancelled[i] != tc.wantCancelled[i] {
					t.Fatalf("cancelled = %v, want %v", cancelled, tc.wantCancelled)
				}
			}
		})
	}
}

// TestSupersedeChecks_ARefusedCancelIsAnOrphanAndNotAFailure is the bad-input
// half, and it is the one that matters: the caller is a review round that has
// already read the forge and is about to judge a head. A courtesy cancel the
// forge refused must leave a recorded orphan, never a failed round.
func TestSupersedeChecks_ARefusedCancelIsAnOrphanAndNotAFailure(t *testing.T) {
	forge := cancellableForge()
	forge.cancelErr = errors.New("502 bad gateway")
	delivery := deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending})

	got := supersedeChecks(context.Background(), forge, testRepoRef, delivery, "h2", observed)
	if len(got) != 1 {
		t.Fatalf("records = %+v, want the displacement recorded despite the refusal", got)
	}
	if got[0].Disposition != SupersessionOrphaned {
		t.Fatalf("disposition = %s, want orphaned: the run is still in flight", got[0].Disposition)
	}
	if !strings.Contains(got[0].Detail, "502 bad gateway") {
		t.Fatalf("the detail does not carry why the cancel failed: %q", got[0].Detail)
	}
}

// TestSupersedeChecks_ANilForgeIsTreatedAsUncancellable: the record still has
// to be made. Losing the displacement because no adapter was handed in would
// be the exact silence this whole file exists to end.
func TestSupersedeChecks_ANilForgeIsTreatedAsUncancellable(t *testing.T) {
	delivery := deliveryOn("h1", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", Status: CheckPending})
	got := supersedeChecks(context.Background(), nil, testRepoRef, delivery, "h2", observed)
	if len(got) != 1 || got[0].Disposition != SupersessionOrphaned {
		t.Fatalf("records = %+v, want one orphan", got)
	}
	if got[0].Detail != "this forge publishes no route to cancel a running check" {
		t.Fatalf("detail = %q", got[0].Detail)
	}
}

// TestSupersedeChecks_RefusesToCancelARunItAlreadyDisplaced is the stale-rerun
// guard applied to the ENGINE, which is the only code here that acts on a run
// by id.
//
// A forge that numbers runs per repository rather than per commit — Gitea's
// commit statuses do — hands back an id this delivery has already displaced
// once. Cancelling it a second time stops the run on the CURRENT head: exactly
// the failure two lanes committed by hand on 2026-09-12, committed instead by
// the engine and on every round.
func TestSupersedeChecks_RefusesToCancelARunItAlreadyDisplaced(t *testing.T) {
	forge := cancellableForge()
	delivery := deliveryOn("h2", CheckRecord{Name: "ci", ForgeRunID: "7", HeadSHA: "h2", Status: CheckPending})
	// h1 already displaced run 7, and the forge handed the same id back on h2.
	delivery.RecordSupersession([]SupersessionRecord{{
		Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
		Status: CheckPending, Disposition: SupersessionCancelled, ObservedAt: observed,
	}})

	got := supersedeChecks(context.Background(), forge, testRepoRef, delivery, "h3", observed)
	if len(got) != 1 {
		t.Fatalf("records = %+v, want the displacement recorded", got)
	}
	if got[0].Disposition != SupersessionOrphaned {
		t.Fatalf("disposition = %s, want orphaned: the engine refused to cancel it", got[0].Disposition)
	}
	if !strings.Contains(got[0].Detail, "already recorded as superseded") {
		t.Fatalf("the detail does not say why the cancel was refused: %q", got[0].Detail)
	}
	if cancelled := forge.cancelledRuns(); len(cancelled) != 0 {
		t.Fatalf("the engine re-ran a cancel on a displaced id: %v", cancelled)
	}
}

// TestSupersedeChecks_CancelsARunItHasNotDisplacedBefore is the success half of
// the same guard: the refusal must be specific to an id already recorded, not a
// blanket refusal that would make the cancel capability dead again.
func TestSupersedeChecks_CancelsARunItHasNotDisplacedBefore(t *testing.T) {
	forge := cancellableForge()
	delivery := deliveryOn("h2", CheckRecord{Name: "ci", ForgeRunID: "8", HeadSHA: "h2", Status: CheckPending})
	delivery.RecordSupersession([]SupersessionRecord{{
		Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
		Status: CheckPending, Disposition: SupersessionCancelled, ObservedAt: observed,
	}})

	got := supersedeChecks(context.Background(), forge, testRepoRef, delivery, "h3", observed)
	if len(got) != 1 || got[0].Disposition != SupersessionCancelled {
		t.Fatalf("records = %+v, want a fresh run id cancelled", got)
	}
	if cancelled := forge.cancelledRuns(); len(cancelled) != 1 || cancelled[0] != "8" {
		t.Fatalf("cancelled = %v, want the run that had not been displaced before", cancelled)
	}
}

// =============================================================================
// The record on the Delivery
// =============================================================================

// TestDeliveryRecordSupersession_DeduplicatesSoAReReadCannotDouble is the
// property the version-conflict retry depends on: record re-reads the delivery
// and re-applies the fold, so folding the same displacement twice must add
// nothing and report no change — otherwise the guarded write never converges.
func TestDeliveryRecordSupersession_DeduplicatesSoAReReadCannotDouble(t *testing.T) {
	delivery := deliveryOn("h1")
	records := []SupersessionRecord{{
		Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2",
		Status: CheckPending, Disposition: SupersessionOrphaned, ObservedAt: observed,
	}}
	if !delivery.RecordSupersession(records) {
		t.Fatal("the first fold reported no change")
	}
	if delivery.RecordSupersession(records) {
		t.Fatal("re-applying the same displacement reported a change; the guarded write would never converge")
	}
	if len(delivery.SupersededRuns) != 1 {
		t.Fatalf("superseded runs = %+v, want one", delivery.SupersededRuns)
	}
	// The SAME run id displaced by a LATER head is a different displacement.
	later := []SupersessionRecord{{
		Name: "ci", ForgeRunID: "7", HeadSHA: "h2", SupersededBy: "h3",
		Status: CheckPending, Disposition: SupersessionOrphaned, ObservedAt: observed.Add(time.Minute),
	}}
	if !delivery.RecordSupersession(later) {
		t.Fatal("a displacement by a later head was mistaken for the one already recorded")
	}
	if len(delivery.SupersededRuns) != 2 {
		t.Fatalf("superseded runs = %+v, want two", delivery.SupersededRuns)
	}
}

// TestDeliveryRecordSupersession_RefusesWhatIsNotADisplacement is the
// bad-input half. A record missing either head names no displacement at all,
// and writing it would put a row on the delivery that the stale-rerun guard
// can never match against anything.
func TestDeliveryRecordSupersession_RefusesWhatIsNotADisplacement(t *testing.T) {
	cases := map[string]struct {
		delivery *Delivery
		records  []SupersessionRecord
	}{
		"no records":         {deliveryOn("h1"), nil},
		"nil delivery":       {nil, []SupersessionRecord{{HeadSHA: "h1", SupersededBy: "h2"}}},
		"no displaced head":  {deliveryOn("h1"), []SupersessionRecord{{Name: "ci", SupersededBy: "h2"}}},
		"no displacing head": {deliveryOn("h1"), []SupersessionRecord{{Name: "ci", HeadSHA: "h1"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if tc.delivery.RecordSupersession(tc.records) {
				t.Fatal("reported a change for something that is not a displacement")
			}
			if tc.delivery != nil && len(tc.delivery.SupersededRuns) != 0 {
				t.Fatalf("superseded runs = %+v, want none", tc.delivery.SupersededRuns)
			}
		})
	}
}

// TestDeliveryRecordSupersession_KeepsTheNewestWithinTheBound: the list is
// bounded because it rides on every guarded write of the Delivery, and the
// NEWEST are the ones kept — the run a reader just found on the forge is the
// one they are about to re-run.
func TestDeliveryRecordSupersession_KeepsTheNewestWithinTheBound(t *testing.T) {
	delivery := deliveryOn("h1")
	overflow := MaxSupersededRuns + 5
	for i := 0; i < overflow; i++ {
		head := "h" + strconv.Itoa(i)
		if !delivery.RecordSupersession([]SupersessionRecord{{
			Name: "ci", ForgeRunID: strconv.Itoa(i), HeadSHA: head, SupersededBy: head + "-next",
			Status: CheckPending, Disposition: SupersessionOrphaned, ObservedAt: observed,
		}}) {
			t.Fatalf("displacement %d reported no change", i)
		}
	}
	if len(delivery.SupersededRuns) != MaxSupersededRuns {
		t.Fatalf("superseded runs = %d, want the bound %d", len(delivery.SupersededRuns), MaxSupersededRuns)
	}
	first := delivery.SupersededRuns[0]
	if first.ForgeRunID != strconv.Itoa(overflow-MaxSupersededRuns) {
		t.Fatalf("oldest kept run = %q, want %q: the bound evicted the wrong end",
			first.ForgeRunID, strconv.Itoa(overflow-MaxSupersededRuns))
	}
	last := delivery.SupersededRuns[len(delivery.SupersededRuns)-1]
	if last.ForgeRunID != strconv.Itoa(overflow-1) {
		t.Fatalf("newest kept run = %q, want the last recorded", last.ForgeRunID)
	}
}

// TestDeliverySupersededRun_IsTheStaleRerunGuard: the lookup a caller makes
// before re-running a CI run, which is the question nobody could ask before
// the record existed.
func TestDeliverySupersededRun_IsTheStaleRerunGuard(t *testing.T) {
	delivery := deliveryOn("h3")
	delivery.RecordSupersession([]SupersessionRecord{
		{Name: "ci", ForgeRunID: "7", HeadSHA: "h1", SupersededBy: "h2", Disposition: SupersessionOrphaned, ObservedAt: observed},
		{Name: "ci", ForgeRunID: "7", HeadSHA: "h2", SupersededBy: "h3", Disposition: SupersessionCancelled, ObservedAt: observed.Add(time.Minute)},
	})

	record, known := delivery.SupersededRun("7")
	if !known {
		t.Fatal("the guard does not know a run the delivery recorded as displaced")
	}
	if record.SupersededBy != "h3" {
		t.Fatalf("superseded by %q, want the newest displacement h3", record.SupersededBy)
	}

	for _, id := range []string{"", "   ", "9"} {
		if _, known := delivery.SupersededRun(id); known {
			t.Fatalf("the guard claimed to know run %q", id)
		}
	}
	var absent *Delivery
	if _, known := absent.SupersededRun("7"); known {
		t.Fatal("a nil delivery claimed to know a displaced run")
	}
}

// TestDeliveryOrphanedRuns_NamesOnlyWhatStillHoldsCapacity: an orphan is the
// one a person must be told about without going to look, newest first.
func TestDeliveryOrphanedRuns_NamesOnlyWhatStillHoldsCapacity(t *testing.T) {
	delivery := deliveryOn("h4")
	delivery.RecordSupersession([]SupersessionRecord{
		{Name: "build", ForgeRunID: "1", HeadSHA: "h1", SupersededBy: "h2", Disposition: SupersessionFinished, ObservedAt: observed},
		{Name: "test", ForgeRunID: "2", HeadSHA: "h1", SupersededBy: "h2", Disposition: SupersessionOrphaned, ObservedAt: observed},
		{Name: "lint", ForgeRunID: "3", HeadSHA: "h2", SupersededBy: "h3", Disposition: SupersessionCancelled, ObservedAt: observed.Add(time.Minute)},
		{Name: "test", ForgeRunID: "4", HeadSHA: "h3", SupersededBy: "h4", Disposition: SupersessionOrphaned, ObservedAt: observed.Add(2 * time.Minute)},
	})
	orphans := delivery.OrphanedRuns()
	if len(orphans) != 2 {
		t.Fatalf("orphans = %+v, want the two that still hold a runner", orphans)
	}
	if orphans[0].ForgeRunID != "4" || orphans[1].ForgeRunID != "2" {
		t.Fatalf("orphans are not newest first: %+v", orphans)
	}

	var absent *Delivery
	if got := absent.OrphanedRuns(); len(got) != 0 {
		t.Fatalf("a nil delivery answered %+v", got)
	}
	clean := deliveryOn("h1")
	clean.RecordSupersession([]SupersessionRecord{
		{Name: "ci", ForgeRunID: "1", HeadSHA: "h1", SupersededBy: "h2", Disposition: SupersessionFinished, ObservedAt: observed},
	})
	if got := clean.OrphanedRuns(); len(got) != 0 {
		t.Fatalf("a finished displacement was reported as an orphan: %+v", got)
	}
}

// TestSupersessionDisposition_OnlyAnOrphanLeaks pins the one predicate the
// surfacing reads. A cancelled run gave its capacity back and a finished one
// never held any past its own end.
func TestSupersessionDisposition_OnlyAnOrphanLeaks(t *testing.T) {
	cases := map[SupersessionDisposition]bool{
		SupersessionFinished:            false,
		SupersessionCancelled:           false,
		SupersessionOrphaned:            true,
		SupersessionDisposition("what"): false,
	}
	for disposition, leaks := range cases {
		if got := disposition.Leaks(); got != leaks {
			t.Errorf("%s.Leaks() = %v, want %v", disposition, got, leaks)
		}
		if disposition.String() != string(disposition) {
			t.Errorf("String() = %q", disposition.String())
		}
	}
}

// TestSupersessionRecord_DescribeNamesTheRunAndWhyItSurvived: what a person
// reads has to be enough to FIND the run and enough to know not to touch it.
func TestSupersessionRecord_DescribeNamesTheRunAndWhyItSurvived(t *testing.T) {
	orphan := SupersessionRecord{
		Name: "ci", ForgeRunID: "4231", HeadSHA: "0123456789abcdef", SupersededBy: "fedcba9876543210",
		Status: CheckPending, Disposition: SupersessionOrphaned, Detail: "gitea publishes no route to cancel a running check",
	}
	line := orphan.Describe()
	for _, want := range []string{"`ci`", "run `4231`", "0123456", "orphaned", "no route to cancel"} {
		if !strings.Contains(line, want) {
			t.Errorf("Describe() is missing %q:\n%s", want, line)
		}
	}
	// A record with nothing extra to say says nothing extra.
	bare := SupersessionRecord{Name: "ci", HeadSHA: "h1", SupersededBy: "h2", Disposition: SupersessionFinished}
	line = bare.Describe()
	if strings.Contains(line, "run `") || strings.HasSuffix(line, ": ") {
		t.Errorf("Describe() invented detail it does not have:\n%s", line)
	}
	if !strings.Contains(line, "finished") {
		t.Errorf("Describe() dropped the disposition:\n%s", line)
	}
}

// TestSupersessionRecord_IdentityDistinguishesContextsOnOneHead: a forge whose
// checks carry no run id — Gitea's commit statuses very nearly are that —
// would otherwise collapse every context of one displacement into one record.
func TestSupersessionRecord_IdentityDistinguishesContextsOnOneHead(t *testing.T) {
	build := SupersessionRecord{Name: "build", HeadSHA: "h1", SupersededBy: "h2"}
	test := SupersessionRecord{Name: "test", HeadSHA: "h1", SupersededBy: "h2"}
	if build.Identity() == test.Identity() {
		t.Fatalf("two contexts share one identity: %q", build.Identity())
	}
	same := SupersessionRecord{Name: "build", HeadSHA: "h1", SupersededBy: "h2", Disposition: SupersessionCancelled}
	if build.Identity() != same.Identity() {
		t.Fatal("the same displacement observed twice has two identities; the retry would double it")
	}
}

// =============================================================================
// Through the review validator — the only producer in production
// =============================================================================

// TestReviewValidator_RecordsTheDisplacementTheHeadMoveWouldHaveErased is the
// end-to-end claim. delivery.Checks holds the LATEST head only, so the moment
// the validator observes a moved head the previous head's runs exist nowhere
// else; this asserts they reach the Delivery before that write lands.
func TestReviewValidator_RecordsTheDisplacementTheHeadMoveWouldHaveErased(t *testing.T) {
	rig := newReviewRig(t)
	ctx := context.Background()

	// The attempt's first round left a run still in flight on h1.
	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	delivery.Checks = []CheckRecord{{Name: "ci", ForgeRunID: "run-h1", HeadSHA: "h1", Status: CheckPending}}
	if err := rig.store.UpdateDelivery(ctx, testTenant, delivery); err != nil {
		t.Fatal(err)
	}

	// The worker pushed; the reviewer and the pipeline both answered on h2.
	rig.forge.prs[100].HeadSHA = "h2"
	rig.approveBy("codex", "h2", 2)
	rig.greenChecks("h2")

	result, err := rig.validator.Validate(ctx, testJob(1), rig.criterion)
	if err != nil || !result.Passed {
		t.Fatalf("result = %+v, %v", result, err)
	}

	stored, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if stored.HeadSHA != "h2" {
		t.Fatalf("head = %q, want the moved head h2", stored.HeadSHA)
	}
	if len(stored.Checks) != 1 || stored.Checks[0].HeadSHA != "h2" {
		t.Fatalf("checks = %+v, want only the new head's: this is the write that erases the old", stored.Checks)
	}
	record, known := stored.SupersededRun("run-h1")
	if !known {
		t.Fatalf("the displaced run was not recorded; superseded runs = %+v", stored.SupersededRuns)
	}
	if record.HeadSHA != "h1" || record.SupersededBy != "h2" {
		t.Fatalf("record = %+v, want h1 displaced by h2", record)
	}
	// Gitea publishes no cancel route, so the run is an orphan rather than a
	// cancellation the engine would otherwise have claimed.
	if record.Disposition != SupersessionOrphaned {
		t.Fatalf("disposition = %s, want orphaned on a forge that cannot cancel", record.Disposition)
	}
	if len(rig.forge.cancelledRuns()) != 0 {
		t.Fatalf("a cancel was attempted on a forge that declares it cannot: %v", rig.forge.cancelledRuns())
	}
}

// TestReviewValidator_CancelsTheDisplacedRunWhereTheForgeCan is the other half
// of the capability branch, and the reason CancelCheckRun is declared at all:
// on a platform with a cancel route the capacity comes back and the record
// says so.
func TestReviewValidator_CancelsTheDisplacedRunWhereTheForgeCan(t *testing.T) {
	rig := newReviewRig(t)
	rig.forge.caps.CancelCheckRuns = true
	ctx := context.Background()

	delivery, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	delivery.Checks = []CheckRecord{{Name: "ci", ForgeRunID: "run-h1", HeadSHA: "h1", Status: CheckPending}}
	if err := rig.store.UpdateDelivery(ctx, testTenant, delivery); err != nil {
		t.Fatal(err)
	}
	rig.forge.prs[100].HeadSHA = "h2"
	rig.approveBy("codex", "h2", 2)
	rig.greenChecks("h2")

	if _, err := rig.validator.Validate(ctx, testJob(1), rig.criterion); err != nil {
		t.Fatal(err)
	}
	cancelled := rig.forge.cancelledRuns()
	if len(cancelled) != 1 || cancelled[0] != "run-h1" {
		t.Fatalf("cancelled = %v, want the displaced run", cancelled)
	}
	stored, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	record, known := stored.SupersededRun("run-h1")
	if !known || record.Disposition != SupersessionCancelled {
		t.Fatalf("record = %+v (known=%v), want a recorded cancellation", record, known)
	}
	if len(stored.OrphanedRuns()) != 0 {
		t.Fatalf("a cancelled run is still reported as leaking capacity: %+v", stored.OrphanedRuns())
	}
}

// TestReviewValidator_AHeadThatDidNotMoveRecordsNoDisplacement is the
// bad-input half at the seam: the ordinary polling round must not manufacture
// a displacement every time it re-reads a pull request that has not changed.
func TestReviewValidator_AHeadThatDidNotMoveRecordsNoDisplacement(t *testing.T) {
	rig := newReviewRig(t)
	ctx := context.Background()
	rig.approveBy("codex", "h1", 1)
	rig.greenChecks("h1")

	if _, err := rig.validator.Validate(ctx, testJob(1), rig.criterion); err != nil {
		t.Fatal(err)
	}
	stored, err := rig.store.GetDelivery(ctx, testTenant, testTask)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.SupersededRuns) != 0 {
		t.Fatalf("a stationary head recorded %+v", stored.SupersededRuns)
	}
}

// =============================================================================
// Surfacing — the record has to reach the person who would re-run it
// =============================================================================

// TestRenderOutcome_NamesTheOrphanedRunsSoNobodyReRunsThem: the record on the
// Delivery is only worth having if it reaches the reader. This is the line the
// two lanes of 2026-09-12 did not have.
func TestRenderOutcome_NamesTheOrphanedRunsSoNobodyReRunsThem(t *testing.T) {
	delivery := deliveryOn("h3")
	delivery.PullRequest = DeliveryPullRequest{Number: 100, State: PullRequestOpen, URL: "http://forge.test/acme/api/pulls/100"}
	delivery.RecordSupersession([]SupersessionRecord{
		{Name: "ci", ForgeRunID: "run-h1", HeadSHA: "h1", SupersededBy: "h2", Status: CheckPending,
			Disposition: SupersessionOrphaned, Detail: "gitea publishes no route to cancel a running check", ObservedAt: observed},
		{Name: "ci", ForgeRunID: "run-h2", HeadSHA: "h2", SupersededBy: "h3", Status: CheckSuccess,
			Disposition: SupersessionFinished, ObservedAt: observed.Add(time.Minute)},
	})

	body := renderOutcome(outcomeReport{
		binding: testBinding(), task: testTaskRecord(25), attempt: 1,
		kind: OutcomeEscalated, message: "review did not converge", iterations: 3, delivery: delivery,
	})
	if !strings.Contains(body, "Orphaned CI runs (1") {
		t.Fatalf("the comment does not count the orphans:\n%s", body)
	}
	if !strings.Contains(body, "run `run-h1`") {
		t.Fatalf("the comment does not name the orphan:\n%s", body)
	}
	if !strings.Contains(body, "do not re-run") {
		t.Fatalf("the comment does not say what not to do:\n%s", body)
	}
	if strings.Contains(body, "run-h2") {
		t.Fatalf("a finished displacement was reported as an orphan:\n%s", body)
	}
}

// TestRenderOutcome_SaysNothingAboutSupersessionWhenThereIsNone: a line that
// appears on every park stops being read. The absence is the assertion.
func TestRenderOutcome_SaysNothingAboutSupersessionWhenThereIsNone(t *testing.T) {
	for name, delivery := range map[string]*Delivery{
		"no delivery at all": nil,
		"no displacements":   deliveryOn("h1"),
		"nothing leaking": func() *Delivery {
			d := deliveryOn("h2")
			d.RecordSupersession([]SupersessionRecord{{Name: "ci", ForgeRunID: "1", HeadSHA: "h1",
				SupersededBy: "h2", Disposition: SupersessionCancelled, ObservedAt: observed}})
			return d
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			body := renderOutcome(outcomeReport{
				binding: testBinding(), task: testTaskRecord(25), attempt: 1,
				kind: OutcomeEscalated, message: "review did not converge", delivery: delivery,
			})
			if strings.Contains(body, "Orphaned CI runs") {
				t.Fatalf("an empty supersession record produced a line:\n%s", body)
			}
		})
	}
}

// assertRecords compares what supersedeChecks answered against what the case
// expects, field by field, so a failure names the field rather than dumping
// two structs.
func assertRecords(t *testing.T, got, want []SupersessionRecord) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("records = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
