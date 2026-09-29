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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fakeAccountAvailability answers whether a reviewer's Agent can draw a usable
// pooled account, and records what it was asked.
type fakeAccountAvailability struct {
	answer orchestration.AccountAvailability
	err    error
	asked  []orchestration.AgentRef
	roles  []orchestration.AgentRole
}

func (f *fakeAccountAvailability) AgentAccountAvailability(_ context.Context, _, _ string, ref orchestration.AgentRef, role orchestration.AgentRole) (orchestration.AccountAvailability, error) {
	f.asked = append(f.asked, ref)
	f.roles = append(f.roles, role)
	return f.answer, f.err
}

// TestReviewDispatcher_NoUsableReviewerAccountStartsNoJob: a reviewer whose
// every account is errored, revoked or spent would only fail at spawn, so no
// review job is created; once an account is healthy the next sweep dispatches.
func TestReviewDispatcher_NoUsableReviewerAccountStartsNoJob(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	availability := &fakeAccountAvailability{answer: orchestration.AccountAvailability{
		Usable: false, Reason: "re-login needed: 401 Unauthorized",
	}}
	rig.dispatcher.SetAccountAvailability(availability)

	started, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
	if err != nil || started {
		t.Fatalf("TriggerReview = (%v, %v), want no job and no error while no reviewer account is usable", started, err)
	}
	if jobs := rig.jobs.requests(); len(jobs) != 0 {
		t.Fatalf("started %d review job(s) against a reviewer with no usable account", len(jobs))
	}
	if len(availability.asked) != 1 || availability.asked[0] != orchestration.AgentRefTo(testReviewerAgent) ||
		availability.roles[0] != orchestration.AgentRoleReview {
		t.Fatalf("availability asked %v as %v, want the seat's Agent in the review role", availability.asked, availability.roles)
	}

	availability.answer = orchestration.AccountAvailability{Usable: true}
	started, err = rig.dispatcher.TriggerReview(context.Background(), prRequest(41))
	if err != nil || !started || len(rig.jobs.requests()) != 1 {
		t.Fatalf("TriggerReview after recovery = (%v, %v) with %d job(s), want the review dispatched", started, err, len(rig.jobs.requests()))
	}
}

// TestReviewDispatcher_AnUnreadablePoolIsAnError: an availability that could
// not be established is returned, not read as usable or as empty.
func TestReviewDispatcher_AnUnreadablePoolIsAnError(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	boom := errors.New("account store unavailable")
	rig.dispatcher.SetAccountAvailability(&fakeAccountAvailability{err: boom})

	if _, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41)); !errors.Is(err, boom) {
		t.Fatalf("TriggerReview error = %v, want the availability failure", err)
	}
	if jobs := rig.jobs.requests(); len(jobs) != 0 {
		t.Fatalf("started %d job(s) on an unreadable pool", len(jobs))
	}
}

// TestReviewDispatcher_AHeldReviewSaysWhyOnThePullRequest: the reason is
// posted once as a pending review status on the head, again only when it
// changes, and never while the reviewer is usable.
func TestReviewDispatcher_AHeldReviewSaysWhyOnThePullRequest(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	availability := &fakeAccountAvailability{answer: orchestration.AccountAvailability{
		Reason: "no usable codex account: re-login needed on 1",
	}}
	rig.dispatcher.SetAccountAvailability(availability)
	ctx := context.Background()

	for range 2 {
		if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(41)); err != nil {
			t.Fatalf("TriggerReview: %v", err)
		}
	}
	if len(rig.forge.statuses) != 1 {
		t.Fatalf("statuses = %+v, want exactly one for an unchanged reason", rig.forge.statuses)
	}
	posted := rig.forge.statuses[0]
	if posted.Status != CheckPending || posted.Description != "no usable codex account: re-login needed on 1" ||
		posted.Context != DefaultReviewStatusContext || rig.forge.statusHeads[0] != dispatchHead {
		t.Fatalf("status = %+v on %s, want a pending review status carrying the reason on the head", posted, rig.forge.statusHeads[0])
	}

	availability.answer.Reason = "no usable codex account: 1 revoked"
	if _, err := rig.dispatcher.TriggerReview(ctx, prRequest(41)); err != nil {
		t.Fatalf("TriggerReview: %v", err)
	}
	if len(rig.forge.statuses) != 2 || rig.forge.statuses[1].Description != "no usable codex account: 1 revoked" {
		t.Fatalf("statuses = %+v, want the changed reason posted", rig.forge.statuses)
	}
}

// TestReviewDispatcher_AHeldReviewWhoseStatusFailsIsAnError: the reason that
// could not reach the pull request is reported, and still no job starts.
func TestReviewDispatcher_AHeldReviewWhoseStatusFailsIsAnError(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	rig.dispatcher.SetAccountAvailability(&fakeAccountAvailability{answer: orchestration.AccountAvailability{Reason: "x"}})
	rig.forge.statusErr = errors.New("forge refused the status")

	if _, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41)); err == nil {
		t.Fatal("TriggerReview succeeded, want the status failure")
	}
	if len(rig.jobs.requests()) != 0 {
		t.Fatal("a held review started a job")
	}
}

// TestReviewDispatcher_AHoldNeverOverwritesAVerdict: a head that already
// carries the reviewer's verdict keeps it; the hold is not posted over it.
func TestReviewDispatcher_AHoldNeverOverwritesAVerdict(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	rig.setChecks(dispatchHead, greenRun(DefaultReviewStatusContext, dispatchHead))
	rig.dispatcher.SetAccountAvailability(&fakeAccountAvailability{answer: orchestration.AccountAvailability{Reason: "x"}})

	if _, err := rig.dispatcher.TriggerReview(context.Background(), prRequest(41)); err != nil {
		t.Fatalf("TriggerReview: %v", err)
	}
	if len(rig.forge.statuses) != 0 {
		t.Fatalf("statuses = %+v, a hold replaced the head's verdict", rig.forge.statuses)
	}
}
