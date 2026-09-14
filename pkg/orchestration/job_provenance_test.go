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

package orchestration

import (
	"errors"
	"strings"
	"testing"
)

// =============================================================================
// JobDeliveryFromArtifacts
// =============================================================================

// pullRequestArtifact is the artifact a runner publishes for an opened pull
// request: kind url, the URL on Ref.Path, the number/state/branch in metadata.
func pullRequestArtifact(number, state, branch, url string) Artifact {
	return pullRequestArtifactForAttempt(testAttempt, number, state, branch, url)
}

// testAttempt is the attempt the single-attempt fixtures below belong to.
const testAttempt = 1

// pullRequestArtifactForAttempt is the same artifact, published by a named
// attempt — the scoping the store's overwrite-by-name forces.
func pullRequestArtifactForAttempt(attempt int, number, state, branch, url string) Artifact {
	return Artifact{
		Kind: ArtifactKindURL,
		Name: ArtifactNameForAttempt(ArtifactNamePullRequest, attempt),
		Ref:  ArtifactRef{Path: url},
		Metadata: map[string]string{
			artifactMetadataNumber: number,
			artifactMetadataState:  state,
			artifactMetadataBranch: branch,
		},
	}
}

func TestJobDeliveryFromArtifactsReadsThePublishedNames(t *testing.T) {
	artifacts := []Artifact{
		pullRequestArtifact("57", "merged", "issue/57-x", "http://forge/org/repo/pulls/57"),
		{
			Kind: ArtifactKindCommit,
			Name: ArtifactNameForAttempt(ArtifactNameHead, testAttempt),
			Ref:  ArtifactRef{SHA: "abc123", Branch: "issue/57-x"},
		},
		{
			Kind:    ArtifactKindInline,
			Name:    ArtifactNameForAttempt(ArtifactNameReviews, testAttempt),
			Content: []byte(`[{"round":1,"reviewer":"codex","verdict":"request_changes","head_sha":"abc123","findings":"needs a test"},{"round":2,"reviewer":"codex","verdict":"approve","head_sha":"def456"}]`),
		},
	}

	delivery, err := JobDeliveryFromArtifacts(artifacts, testAttempt)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery == nil {
		t.Fatal("a task carrying all three published names produced no delivery")
	}
	if delivery.PullRequestNumber != 57 {
		t.Errorf("pull request number = %d, want 57", delivery.PullRequestNumber)
	}
	if delivery.PullRequestURL != "http://forge/org/repo/pulls/57" {
		t.Errorf("pull request url = %q", delivery.PullRequestURL)
	}
	if delivery.PullRequestState != "merged" {
		t.Errorf("pull request state = %q, want merged", delivery.PullRequestState)
	}
	if delivery.Branch != "issue/57-x" {
		t.Errorf("branch = %q, want issue/57-x", delivery.Branch)
	}
	if delivery.HeadSHA != "abc123" {
		t.Errorf("head = %q, want abc123", delivery.HeadSHA)
	}
	if len(delivery.Reviews) != 2 {
		t.Fatalf("review rounds = %d, want 2", len(delivery.Reviews))
	}
	if delivery.Reviews[1].Verdict != "approve" || delivery.Reviews[1].Round != 2 {
		t.Errorf("second round = %+v, want round 2 approve", delivery.Reviews[1])
	}
}

func TestJobDeliveryFromArtifactsTakesTheBranchFromTheHeadWhenNoPullRequestNamesIt(t *testing.T) {
	// The direct-push carve-out: a change that landed without a pull request
	// still has a branch and a head, and the page must still show them.
	delivery, err := JobDeliveryFromArtifacts([]Artifact{{
		Kind: ArtifactKindCommit,
		Name: ArtifactNameForAttempt(ArtifactNameHead, testAttempt),
		Ref:  ArtifactRef{SHA: "deadbeef", Branch: "hotfix/x"},
	}}, testAttempt)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery == nil {
		t.Fatal("a carve-out delivery produced nothing")
	}
	if delivery.Branch != "hotfix/x" || delivery.HeadSHA != "deadbeef" {
		t.Errorf("delivery = %+v, want branch hotfix/x head deadbeef", delivery)
	}
	if delivery.PullRequestNumber != 0 || delivery.PullRequestURL != "" {
		t.Errorf("a carve-out delivery invented a pull request: %+v", delivery)
	}
}

func TestJobDeliveryFromArtifactsIgnoresArtifactsItDoesNotOwn(t *testing.T) {
	delivery, err := JobDeliveryFromArtifacts([]Artifact{
		{Kind: ArtifactKindURL, Name: "outcome_comment", Ref: ArtifactRef{Path: "http://forge/x"}},
		{Kind: ArtifactKindInline, Name: "notes", Content: []byte("hello")},
	}, testAttempt)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery != nil {
		t.Errorf("a task with no published delivery produced one: %+v", delivery)
	}
}

func TestJobDeliveryFromArtifactsIsNilForNoArtifacts(t *testing.T) {
	delivery, err := JobDeliveryFromArtifacts(nil, testAttempt)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery != nil {
		t.Errorf("nil artifacts produced a delivery: %+v", delivery)
	}
}

func TestJobDeliveryFromArtifactsRefusesAnUnreadableNumber(t *testing.T) {
	_, err := JobDeliveryFromArtifacts([]Artifact{
		pullRequestArtifact("fifty-seven", "open", "b", "http://forge/x"),
	}, testAttempt)
	if !errors.Is(err, ErrJobArtifactDecode) {
		t.Fatalf("error = %v, want ErrJobArtifactDecode", err)
	}
	var decode *JobArtifactDecodeError
	if !errors.As(err, &decode) || decode.Artifact != ArtifactNamePullRequest {
		t.Fatalf("error = %v, want a pull_request decode error", err)
	}
}

func TestJobDeliveryFromArtifactsRefusesUnreadableReviewRounds(t *testing.T) {
	// A refusal, not an omission: review rounds that exist and are not shown
	// is exactly the failure the job page was reported for.
	_, err := JobDeliveryFromArtifacts([]Artifact{{
		Kind:    ArtifactKindInline,
		Name:    ArtifactNameForAttempt(ArtifactNameReviews, testAttempt),
		Content: []byte(`{"not":"an array"}`),
	}}, testAttempt)
	var decode *JobArtifactDecodeError
	if !errors.As(err, &decode) || decode.Artifact != ArtifactNameReviews {
		t.Fatalf("error = %v, want a reviews decode error", err)
	}
}

func TestJobDeliveryFromArtifactsAcceptsAnEmptyReviewsBody(t *testing.T) {
	// An artifact with no bytes is a publication that had nothing to say,
	// which is not the same as one that could not be read.
	delivery, err := JobDeliveryFromArtifacts([]Artifact{
		{Kind: ArtifactKindInline, Name: ArtifactNameReviews},
		{Kind: ArtifactKindCommit, Name: ArtifactNameForAttempt(ArtifactNameHead, testAttempt), Ref: ArtifactRef{SHA: "a1"}},
	}, testAttempt)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery == nil || len(delivery.Reviews) != 0 {
		t.Fatalf("delivery = %+v, want no review rounds", delivery)
	}
}

func TestJobDeliveryEmpty(t *testing.T) {
	if !(*JobDelivery)(nil).Empty() {
		t.Error("a nil delivery is not empty")
	}
	if !(&JobDelivery{}).Empty() {
		t.Error("a zero delivery is not empty")
	}
	if (&JobDelivery{Branch: "b"}).Empty() {
		t.Error("a delivery naming a branch reports empty")
	}
}

// =============================================================================
// JobAttemptsOfTask
// =============================================================================

func TestJobAttemptsOfTaskNumbersTheHistoryAndMarksTheOpenAttempt(t *testing.T) {
	task := &Task{
		ID:           "task-1",
		Metadata:     map[string]string{attemptHistoryKey: "job-1,job-2"},
		SpawnedJobID: "job-3",
	}
	attempts := JobAttemptsOfTask(task, "job-3", 3)
	if len(attempts) != 3 {
		t.Fatalf("attempts = %+v, want three", attempts)
	}
	for i, want := range []string{"job-1", "job-2", "job-3"} {
		if attempts[i].JobID != want || attempts[i].Attempt != i+1 {
			t.Errorf("attempt %d = %+v, want %s at ordinal %d", i, attempts[i], want, i+1)
		}
	}
	if attempts[0].Current || attempts[1].Current || !attempts[2].Current {
		t.Errorf("the open attempt is not the one marked current: %+v", attempts)
	}
}

func TestJobAttemptsOfTaskMarksAConcludedAttemptTheReaderAskedFor(t *testing.T) {
	// Reading attempt 1 of a task now on attempt 3: the page must still say
	// which attempt this job was.
	task := &Task{ID: "task-1", Metadata: map[string]string{attemptHistoryKey: "job-1,job-2,job-3"}}
	attempts := JobAttemptsOfTask(task, "job-1", 1)
	if len(attempts) != 3 || !attempts[0].Current {
		t.Fatalf("attempts = %+v, want three with the first current", attempts)
	}
}

func TestJobAttemptsOfTaskPlacesAJobTheHistoryLost(t *testing.T) {
	// A status read after another attempt replaced this one, on a task whose
	// history the reaper never got to write: the job still knows it was
	// attempt 2 and must not render as attempt "—".
	task := &Task{ID: "task-1", Metadata: map[string]string{attemptHistoryKey: "job-1"}}
	attempts := JobAttemptsOfTask(task, "job-9", 2)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %+v, want two", attempts)
	}
	if attempts[1].JobID != "job-9" || attempts[1].Attempt != 2 || !attempts[1].Current {
		t.Errorf("second attempt = %+v, want job-9 at ordinal 2, current", attempts[1])
	}
}

func TestJobAttemptsOfTaskReplacesTheHistoryEntryAtTheSameOrdinal(t *testing.T) {
	task := &Task{ID: "task-1", Metadata: map[string]string{attemptHistoryKey: "job-1,job-2"}}
	attempts := JobAttemptsOfTask(task, "job-9", 2)
	if len(attempts) != 2 {
		t.Fatalf("attempts = %+v, want two", attempts)
	}
	if attempts[1].JobID != "job-9" || !attempts[1].Current {
		t.Errorf("ordinal 2 = %+v, want the reading job", attempts[1])
	}
}

func TestJobAttemptsOfTaskIsNilForNoTask(t *testing.T) {
	if got := JobAttemptsOfTask(nil, "job-1", 1); got != nil {
		t.Errorf("attempts = %+v, want nil for no task", got)
	}
}

func TestJobAttemptsOfTaskLeavesTheHistoryAloneForAnUnnumberedJob(t *testing.T) {
	// A job the task never recorded and that carries no ordinal of its own
	// has nothing to place: inventing an ordinal would be a lie.
	task := &Task{ID: "task-1", Metadata: map[string]string{attemptHistoryKey: "job-1"}}
	attempts := JobAttemptsOfTask(task, "job-9", 0)
	if len(attempts) != 1 || attempts[0].JobID != "job-1" || attempts[0].Current {
		t.Errorf("attempts = %+v, want the history alone and nothing current", attempts)
	}
}

// =============================================================================
// The errors
// =============================================================================

func TestJobArtifactDecodeErrorNamesTheArtifactAndField(t *testing.T) {
	err := &JobArtifactDecodeError{Artifact: "reviews", Field: "content", Message: "bad json"}
	msg := err.Error()
	for _, want := range []string{"reviews", "content", "bad json"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name %q", msg, want)
		}
	}
	if !errors.Is(err, ErrJobArtifactDecode) {
		t.Error("the error does not unwrap to its sentinel")
	}
}

// =============================================================================
// One task, two attempts, two deliveries
// =============================================================================

// TestJobDeliveryFromArtifactsIsPerAttempt is the property the whole page
// depends on. SubmitTaskArtifacts overwrites by name and a retried task keeps
// ONE record, so a flat artifact name had the newest attempt's pull request
// replace every earlier one — and an older job then showed work it never did.
func TestJobDeliveryFromArtifactsIsPerAttempt(t *testing.T) {
	// What the task accumulates once two attempts have published.
	artifacts := []Artifact{
		pullRequestArtifactForAttempt(1, "40", "closed", "issue/26-first", "http://forge/pulls/40"),
		{
			Kind: ArtifactKindCommit,
			Name: ArtifactNameForAttempt(ArtifactNameHead, 1),
			Ref:  ArtifactRef{SHA: "aaa111", Branch: "issue/26-first"},
		},
		pullRequestArtifactForAttempt(3, "57", "merged", "issue/26-third", "http://forge/pulls/57"),
		{
			Kind: ArtifactKindCommit,
			Name: ArtifactNameForAttempt(ArtifactNameHead, 3),
			Ref:  ArtifactRef{SHA: "ccc333", Branch: "issue/26-third"},
		},
		{
			Kind:    ArtifactKindInline,
			Name:    ArtifactNameForAttempt(ArtifactNameReviews, 3),
			Content: []byte(`[{"round":1,"reviewer":"codex","verdict":"approve","head_sha":"ccc333"}]`),
		},
	}

	first, err := JobDeliveryFromArtifacts(artifacts, 1)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts(attempt 1): %v", err)
	}
	if first == nil {
		t.Fatal("attempt 1 published a delivery and got none back")
	}
	if first.PullRequestNumber != 40 || first.HeadSHA != "aaa111" || first.Branch != "issue/26-first" {
		t.Errorf("attempt 1 = %+v, want its OWN pull request 40 on issue/26-first@aaa111", first)
	}
	if len(first.Reviews) != 0 {
		t.Errorf("attempt 1 = %+v, want no review rounds; the later attempt's are not its", first.Reviews)
	}

	third, err := JobDeliveryFromArtifacts(artifacts, 3)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts(attempt 3): %v", err)
	}
	if third == nil {
		t.Fatal("attempt 3 published a delivery and got none back")
	}
	if third.PullRequestNumber != 57 || third.HeadSHA != "ccc333" || third.Branch != "issue/26-third" {
		t.Errorf("attempt 3 = %+v, want pull request 57 on issue/26-third@ccc333", third)
	}
	if len(third.Reviews) != 1 || third.Reviews[0].Verdict != "approve" {
		t.Errorf("attempt 3 reviews = %+v, want its one approval", third.Reviews)
	}
}

// TestJobDeliveryFromArtifactsIsNilForAnAttemptThatPublishedNothing: an
// attempt that died before publishing must show ITS emptiness, never the
// neighbouring attempt's delivery.
func TestJobDeliveryFromArtifactsIsNilForAnAttemptThatPublishedNothing(t *testing.T) {
	artifacts := []Artifact{
		pullRequestArtifactForAttempt(3, "57", "merged", "issue/26", "http://forge/pulls/57"),
	}
	delivery, err := JobDeliveryFromArtifacts(artifacts, 2)
	if err != nil {
		t.Fatalf("JobDeliveryFromArtifacts: %v", err)
	}
	if delivery != nil {
		t.Errorf("attempt 2 published nothing but was handed %+v", delivery)
	}
}

// =============================================================================
// The attempt-scoped names
// =============================================================================

func TestArtifactNameForAttempt(t *testing.T) {
	if got := ArtifactNameForAttempt(ArtifactNamePullRequest, 3); got != "pull_request@a3" {
		t.Errorf("name = %q, want pull_request@a3", got)
	}
	// A job that is not an attempt at a task has nothing to be distinguished
	// from, so it keeps the bare name.
	if got := ArtifactNameForAttempt(ArtifactNameHead, 0); got != "head" {
		t.Errorf("name = %q, want head", got)
	}
	if got := ArtifactNameForAttempt(ArtifactNameHead, -1); got != "head" {
		t.Errorf("name = %q, want head for a negative attempt", got)
	}
}

func TestArtifactBaseName(t *testing.T) {
	if got := ArtifactBaseName("pull_request@a12"); got != "pull_request" {
		t.Errorf("base = %q, want pull_request", got)
	}
	if got := ArtifactBaseName("head"); got != "head" {
		t.Errorf("base = %q, want head", got)
	}
	// An artifact whose own name starts with the separator is not scoped.
	if got := ArtifactBaseName("@a3"); got != "@a3" {
		t.Errorf("base = %q, want the name unchanged", got)
	}
}
