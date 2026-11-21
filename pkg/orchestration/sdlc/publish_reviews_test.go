// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package sdlc

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// errReviewArtifactCause stands in for whatever the encoder refused.
var errReviewArtifactCause = errors.New("the encoder said no")

// testTurnAttempt is the attempt the fixtures below were published by; the
// artifact names are scoped to it, as the store's overwrite-by-name forces.
const testTurnAttempt = 2

// TestReviewRoundsArtifactPublishesEveryVerdict: the review rounds a delivery
// recorded reach the engine's job page, which reads them from this artifact
// and never from the Delivery — go-quicbaas#796.
func TestReviewRoundsArtifactPublishesEveryVerdict(t *testing.T) {
	t.Parallel()

	submitted := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	delivery := &Delivery{
		TaskID: "task-1",
		Reviews: []ReviewRecord{
			{Round: 1, Reviewer: "codex", Verdict: VerdictRequestChanges, HeadSHA: "a1", Findings: "add a test", SubmittedAt: submitted},
			{Round: 2, Reviewer: "codex", Verdict: VerdictApprove, HeadSHA: "b2", SubmittedAt: submitted.Add(time.Hour)},
		},
	}

	artifact, err := reviewRoundsArtifact(delivery, testTurnAttempt)
	if err != nil {
		t.Fatalf("reviewRoundsArtifact: %v", err)
	}
	if artifact == nil {
		t.Fatal("two recorded verdicts published no artifact")
	}
	wantName := orchestration.ArtifactNameForAttempt(orchestration.ArtifactNameReviews, testTurnAttempt)
	if artifact.Name != wantName || artifact.Kind != orchestration.ArtifactKindInline {
		t.Fatalf("artifact = %+v, want an inline reviews artifact", artifact)
	}
	if artifact.Size != int64(len(artifact.Content)) {
		t.Errorf("size = %d, want %d", artifact.Size, len(artifact.Content))
	}

	// The engine's own reader is the contract: what it decodes is what the
	// job page renders.
	decoded, err := orchestration.JobDeliveryFromArtifacts([]orchestration.Artifact{*artifact}, testTurnAttempt)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if len(decoded.Reviews) != 2 {
		t.Fatalf("rounds = %+v, want two", decoded.Reviews)
	}
	if decoded.Reviews[0].Verdict != VerdictRequestChanges.String() || decoded.Reviews[0].Findings != "add a test" {
		t.Errorf("first round = %+v", decoded.Reviews[0])
	}
	if decoded.Reviews[1].Verdict != VerdictApprove.String() || decoded.Reviews[1].Round != 2 {
		t.Errorf("second round = %+v", decoded.Reviews[1])
	}
	if decoded.Reviews[0].SubmittedAt != submitted.Format(time.RFC3339) {
		t.Errorf("submitted at = %q, want RFC 3339", decoded.Reviews[0].SubmittedAt)
	}
}

// TestReviewRoundsArtifactIsNilBeforeAnyReviewer: an empty rounds array reads
// as "reviewed, no findings", which is not the same as "not yet reviewed".
func TestReviewRoundsArtifactIsNilBeforeAnyReviewer(t *testing.T) {
	t.Parallel()

	artifact, err := reviewRoundsArtifact(&Delivery{TaskID: "task-1"}, testTurnAttempt)
	if err != nil {
		t.Fatalf("reviewRoundsArtifact: %v", err)
	}
	if artifact != nil {
		t.Errorf("artifact = %+v, want none before a reviewer has spoken", artifact)
	}
}

// TestReviewArtifactErrorNamesTheTask: the typed refusal fails the hook rather
// than dropping the rounds silently.
func TestReviewArtifactErrorNamesTheTask(t *testing.T) {
	t.Parallel()

	err := &ReviewArtifactError{TaskID: "task-9"}
	if got := err.Error(); got == "" || !strings.Contains(got, "task-9") {
		t.Errorf("error = %q, want it to name the task", got)
	}
	wrapped := &ReviewArtifactError{TaskID: "task-9", Cause: errReviewArtifactCause}
	if got := wrapped.Error(); !strings.Contains(got, errReviewArtifactCause.Error()) {
		t.Errorf("error = %q, want it to carry the cause", got)
	}
}
