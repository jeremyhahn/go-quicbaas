//go:build codec_json

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

// selectBackupSeat binds a second, counting seat and records an operator's
// override of pull request 41 to it.
func (r *dispatchRig) selectBackupSeat(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	binding, err := r.store.GetBinding(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	binding.Reviewers = []ReviewerConfig{{Name: "backup", Agent: testBackupReviewerAgent, Login: "backup-bot",
		TokenSecretRef: "backup-tok", CountsTowardMerge: boolPtr(true)}}
	if err := r.store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatal(err)
	}
	override := &ReviewerOverride{ProjectID: testProject, PullRequest: 41, Profile: "backup", BindingEpoch: binding.Epoch}
	if err := r.store.PutReviewerOverride(ctx, testTenant, override); err != nil {
		t.Fatal(err)
	}
}

// TestReviewDispatcher_RedispatchReviewStartsTheSelectedSeat: with no review
// running, switching reviewer starts one now, by the seat just selected.
func TestReviewDispatcher_RedispatchReviewStartsTheSelectedSeat(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	rig.selectBackupSeat(t)

	started, err := rig.dispatcher.RedispatchReview(context.Background(), testTenant, testProject, 41)
	if err != nil || !started {
		t.Fatalf("RedispatchReview = %v, %v; want a review started", started, err)
	}
	reqs := rig.jobs.requests()
	if len(reqs) != 1 {
		t.Fatalf("started %d jobs, want 1", len(reqs))
	}
	if reqs[0].Params[ParamPullRequest] != "41" || reqs[0].Params[ParamReviewerProfile] != "backup" {
		t.Fatalf("params = %v, want pull request 41 reviewed by the selected seat", reqs[0].Params)
	}
	if reqs[0].Lead.AgentID != testBackupReviewerAgent {
		t.Fatalf("lead = %+v, want the selected seat's Agent", reqs[0].Lead)
	}
}

// TestReviewDispatcher_RedispatchReviewNeverDisplacesARunningReview is owner
// ruling 2026-09-25: switching reviewer cancels nothing. A review holding the
// pull request's lease runs on; the switch answers "not started" rather than
// an error, and the next dispatch is by the new seat.
func TestReviewDispatcher_RedispatchReviewNeverDisplacesARunningReview(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	rig.selectBackupSeat(t)
	rig.jobs.err = &orchestration.JobLeaseTakenError{JobID: "new", HeldBy: "running-review"}

	started, err := rig.dispatcher.RedispatchReview(context.Background(), testTenant, testProject, 41)
	if err != nil || started {
		t.Fatalf("RedispatchReview = %v, %v; want not started and no error while a review runs", started, err)
	}
}

func TestReviewDispatcher_RedispatchReviewReportsWhatItCouldNotDo(t *testing.T) {
	rig := newDispatchRig(t)
	rig.ungate(t)
	if _, err := rig.dispatcher.RedispatchReview(context.Background(), "", testProject, 41); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no tenant = %v, want invalid input", err)
	}
	if _, err := rig.dispatcher.RedispatchReview(context.Background(), testTenant, testProject, -1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a negative pull request = %v, want invalid input", err)
	}
	boom := errors.New("engine down")
	rig.jobs.err = boom
	if _, err := rig.dispatcher.RedispatchReview(context.Background(), testTenant, testProject, 41); !errors.Is(err, boom) {
		t.Fatalf("an engine failure = %v, want it reported", err)
	}
}
