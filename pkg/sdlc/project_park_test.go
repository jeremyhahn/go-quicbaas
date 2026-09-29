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
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// A parked project: deliveries acknowledged, intake refused, in flight served
// =============================================================================

// parkedRefusal is the refusal the engine's poller nudge gives a parked
// project.
func parkedRefusal() error {
	return &orchestration.ProjectParkedError{TenantID: testTenant, ProjectID: testProject, Work: "adoption of new work"}
}

func TestWebhookIngest_AParkedProjectIsAcknowledgedAndNotTriaged(t *testing.T) {
	rig := newWebhookRig(t)
	rig.nudger.err = parkedRefusal()
	triage := &fakeNudger{}
	rig.ingestor.SetTriageNudger(triage)

	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("a parked project's delivery must be acknowledged, got %v", err)
	}
	if result.Nudged || !errors.Is(result.Parked, orchestration.ErrProjectParked) {
		t.Fatalf("result = %+v, want the park recorded and no nudge", result)
	}
	if len(triage.nudges) != 0 || result.Triaged {
		t.Fatalf("a parked project was triaged: %v", triage.nudges)
	}
}

func TestWebhookIngest_AParkedProjectStillReviewsWorkInFlight(t *testing.T) {
	rig := newWebhookRig(t)
	rig.nudger.err = parkedRefusal()
	rig.reviews.started = true
	rig.forge.parseEvent = &ForgeEvent{Kind: EventPullRequestOpened, Repo: testRepoRef, PullRequestNumber: 7}

	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !result.ReviewStarted || len(rig.reviews.requests()) != 1 {
		t.Fatalf("a parked project's open pull request was not reviewed: %+v", result)
	}
}

func TestWebhookIngest_ANudgeFailureThatIsNotAParkStillFails(t *testing.T) {
	rig := newWebhookRig(t)
	rig.nudger.err = errors.New("the poller is down")

	if _, err := rig.ingestor.Ingest(context.Background(), delivery()); !errors.Is(err, ErrBackend) {
		t.Fatalf("err = %v, want the backend failure so the forge retries", err)
	}
}

func TestWebhookHTTP_AParkedProjectIsAcceptedAndLogged(t *testing.T) {
	rig := newWebhookRig(t)
	rig.nudger.err = parkedRefusal()
	h, seen := newWebhookHTTP(t, rig, 64)

	rec := post(h, testProject, `{"action":"opened"}`, "sig")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("a parked project's delivery: %d %s", rec.Code, rec.Body.String())
	}
	var accepted webhookAcceptedResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &accepted); err != nil || !accepted.Accepted || accepted.Nudged || !accepted.Parked {
		t.Fatalf("body = %s, want accepted, parked, not nudged", rec.Body.String())
	}
	if len(*seen) != 1 || !errors.Is((*seen)[0], orchestration.ErrProjectParked) {
		t.Fatalf("the operator log = %v, want the park refusal once", *seen)
	}
}

func TestTriageScheduler_AParkedProjectIsRefusedTyped(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)
	starter.err = parkedRefusal()

	err := scheduler.TriageProject(context.Background(), testTenant, testProject)
	if !errors.Is(err, orchestration.ErrProjectParked) {
		t.Fatalf("err = %v, want the park refusal", err)
	}
	if errors.Is(err, ErrBackend) {
		t.Fatalf("a closed intake was reported as a broken backend: %v", err)
	}
}

func TestTriageSweep_AParkedProjectIsNotAFailedPass(t *testing.T) {
	scheduler, starter, _ := newTriageScheduler(t)
	starter.err = parkedRefusal()

	swept, err := scheduler.Sweep(context.Background(), &fakeTenants{ids: []string{testTenant}})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept != 0 {
		t.Errorf("swept = %d, want the parked project not counted as triaged", swept)
	}
	if failures := scheduler.SweepFailures(); len(failures) != 0 {
		t.Fatalf("a parked project was reported as a failure: %v", failures)
	}
}
