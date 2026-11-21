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
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDelivery_ReviewRecordedDedupesByExternalIDThenIdentity(t *testing.T) {
	d := &Delivery{Reviews: []ReviewRecord{
		{Reviewer: "codex", Verdict: VerdictApprove, HeadSHA: "h1", ExternalID: "7"},
		{Reviewer: "alice", Verdict: VerdictRequestChanges, HeadSHA: "h1"},
	}}
	if !d.ReviewRecorded(ReviewRecord{Reviewer: "someone-else", Verdict: VerdictError, HeadSHA: "h9", ExternalID: "7"}) {
		t.Fatal("a submission with a recorded external id is not recorded")
	}
	if d.ReviewRecorded(ReviewRecord{Reviewer: "codex", Verdict: VerdictApprove, HeadSHA: "h1", ExternalID: "8"}) {
		t.Fatal("a new external id is recorded")
	}
	if !d.ReviewRecorded(ReviewRecord{Reviewer: "alice", Verdict: VerdictRequestChanges, HeadSHA: "h1"}) {
		t.Fatal("an id-less submission with the same reviewer/head/verdict is not recorded")
	}
	if d.ReviewRecorded(ReviewRecord{Reviewer: "alice", Verdict: VerdictApprove, HeadSHA: "h1"}) {
		t.Fatal("a different verdict from the same reviewer is recorded")
	}
}

func TestDelivery_SubjectAndValidate(t *testing.T) {
	d := &Delivery{TaskID: testTask, Attempt: 1, Branch: "issue/1-x"}
	if got := d.Subject(testRepoRef); got != "acme/api@issue/1-x" {
		t.Fatalf("Subject without PR = %q", got)
	}
	d.PullRequest = DeliveryPullRequest{Number: 12}
	if got := d.Subject(testRepoRef); got != "acme/api#12" {
		t.Fatalf("Subject with PR = %q", got)
	}
	if !d.PullRequest.Opened() {
		t.Fatal("Opened")
	}
	if err := d.Validate(); err != nil {
		t.Fatalf("valid delivery refused: %v", err)
	}
	for name, bad := range map[string]*Delivery{
		"nil":     nil,
		"task":    {Attempt: 1, Branch: "b"},
		"attempt": {TaskID: testTask, Branch: "b"},
		"branch":  {TaskID: testTask, Attempt: 1},
	} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalidInput) && !errors.Is(err, ErrInvalidKeySegment) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
	if got := d.GetVersion(); got != 0 {
		t.Fatalf("GetVersion = %d", got)
	}
	d.SetVersion(4)
	if d.Version != 4 {
		t.Fatal("SetVersion")
	}
	d.SetEntityID(9)
	if d.EntityID() != 9 {
		t.Fatal("entity id")
	}
}

func TestCheckStatus_Finished(t *testing.T) {
	for status, want := range map[CheckStatus]bool{CheckPending: false, CheckSuccess: true, CheckFailure: true, CheckCancelled: true, "weird": false} {
		if status.Finished() != want {
			t.Fatalf("%s.Finished() = %v", status, status.Finished())
		}
	}
	if !(ReviewRecord{HeadSHA: "h"}).AppliesTo("h") || (ReviewRecord{}).AppliesTo("") {
		t.Fatal("AppliesTo")
	}
}

func TestKeys_SegmentsAndDigests(t *testing.T) {
	for _, bad := range []string{"", "a/b", ".", "..", "a\\b", "a\x00b", "a/../b", "./a"} {
		if err := validateKeySegment("f", bad); !errors.Is(err, ErrInvalidKeySegment) {
			t.Fatalf("%q accepted: %v", bad, err)
		}
	}
	if SegmentHash("a") == SegmentHash("b") || len(SegmentHash("a")) != 64 {
		t.Fatal("SegmentHash")
	}
	if entityIDFor(testTask) != entityIDFor(testTask) || entityIDFor(testTask) == entityIDFor("other") || entityIDFor("") == 0 {
		t.Fatal("entityIDFor must be deterministic, distinct and never zero")
	}
	key, err := WebhookRouteKey("p1")
	if err != nil || !strings.HasPrefix(key, "__sys:sdlc/webhookroute/") || !strings.HasSuffix(key, "/p1") {
		t.Fatalf("WebhookRouteKey = %q, %v", key, err)
	}
	if _, err := WebhookRouteKey("a/b"); !errors.Is(err, ErrInvalidKeySegment) {
		t.Fatalf("slash accepted: %v", err)
	}
	var seg *KeySegmentError
	if err := validateKeySegment("tenant", ""); !errors.As(err, &seg) || seg.Field != "tenant" || !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("KeySegmentError shape: %v", err)
	}
}

func TestPullRequestStateAndVerdictStrings(t *testing.T) {
	if PullRequestMerged.String() != "merged" || VerdictRequestChanges.String() != "request_changes" || CheckFailure.String() != "failure" {
		t.Fatal("String()")
	}
	_ = time.Now()
}
