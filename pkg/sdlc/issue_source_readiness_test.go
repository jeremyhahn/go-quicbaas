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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The gate at the source
// =============================================================================
//
// readiness_test.go covers the evaluator. What is covered here is that
// IssueSource.Poll ASKS it, and turns each verdict into the right spec — the
// difference between a readiness gate that exists and one that is enforced.

// staticLiveWork answers the ownership predicate from a fixed set.
type staticLiveWork struct {
	refs map[string]struct{}
	err  error
}

func (s *staticLiveWork) LiveRefs(context.Context, string, time.Time) (map[string]struct{}, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.refs, nil
}

// TestIssueSource_ParksAnIssueWaitingOnAnUpstream is the park-and-resume property
// at the source: the spec carries the park, so the engine creates the task
// BLOCKED in one write and no sweep can start it in the meantime.
func TestIssueSource_ParksAnIssueWaitingOnAnUpstream(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	upstream := forge.putIssue(41, "the upstream work")
	upstream.State = "open"
	issue := forge.putIssue(42, "Consume it", "task:ready")
	issue.Body = "Do the thing.\n\nBlocked by: #41\n"

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %+v, want the blocked issue offered as a parked spec", specs)
	}
	spec := specs[0]
	if spec.Park == nil {
		t.Fatal("a blocked issue must be offered WITH its park, so the task is created blocked in one write")
	}
	if spec.Park.Kind != orchestration.TaskBlockKindBlocked {
		t.Fatalf("Park.Kind = %s, want blocked", spec.Park.Kind)
	}
	condition, err := ParseUnblockCondition(spec.Park.Message)
	if err != nil {
		t.Fatalf("the park must carry a condition the sweep can check: %v (%q)", err, spec.Park.Message)
	}
	if condition.Kind != UnblockForgeIssueState || condition.Subject != "acme/api#41" || condition.Target != "closed" {
		t.Fatalf("condition = %+v", condition)
	}
	// The contract is still rendered, so a readmitted task has work to hand a
	// worker rather than a stub to be refilled later.
	if !strings.Contains(spec.Description, "Blocked by: #41") {
		t.Fatalf("a parked spec still carries its contract: %q", spec.Description)
	}
	if err := spec.Validate(TaskSourceKindForgeIssue); err != nil {
		t.Fatalf("the engine would refuse the spec: %v", err)
	}

	// Resume: once the upstream closes, the same issue is offered unparked.
	upstream.State = "closed"
	specs, err = source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll after the upstream closed: %v", err)
	}
	if len(specs) != 1 || specs[0].Park != nil {
		t.Fatalf("once the dependency is satisfied the spec must carry no park: %+v", specs)
	}
}

// TestIssueSource_MergedButUnreleasedStillParks: the upstream issue is closed and
// the consumer pins a version, so the tag decides. This is the issue's
// merged-is-not-released acceptance check at the source.
func TestIssueSource_MergedButUnreleasedStillParks(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	merged := forge.putIssue(7, "the upstream fix")
	merged.State = "closed"
	issue := forge.putIssue(42, "Consume the fix", "task:ready")
	issue.Body = "Blocked by: acme/api#7\nRequires release: acme/api@v0.9.5-alpha\n"

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Park == nil {
		t.Fatalf("merged but untagged must still park: %+v", specs)
	}
	condition, err := ParseUnblockCondition(specs[0].Park.Message)
	if err != nil {
		t.Fatalf("ParseUnblockCondition: %v", err)
	}
	if condition.Kind != UnblockReleaseTag || condition.Target != "v0.9.5-alpha" {
		t.Fatalf("the park must wait on the TAG, not on the closed issue: %+v", condition)
	}

	forge.putTag(RepoRef{Owner: "acme", Repo: "api"}, "v0.9.5-alpha", "deadbeef")
	specs, err = source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll after the tag: %v", err)
	}
	if len(specs) != 1 || specs[0].Park != nil {
		t.Fatalf("once the tag exists the consumer may proceed: %+v", specs)
	}
}

// TestIssueSource_RefusesACycleAsAHumanPark: a cycle becomes a task parked for a
// person, never a running one and never a retry.
func TestIssueSource_RefusesACycleAsAHumanPark(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	first := forge.putIssue(1, "one half", "task:ready")
	first.Body = "Blocked by: #2\n"
	second := forge.putIssue(2, "the other half")
	second.Body = "Blocked by: #1\n"

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Park == nil {
		t.Fatalf("a cycle must park: %+v", specs)
	}
	if specs[0].Park.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("Park.Kind = %s, want needs_info: only a person can break a cycle", specs[0].Park.Kind)
	}
	if !strings.Contains(specs[0].Park.Message, "cycle") {
		t.Fatalf("the park must say what is wrong: %q", specs[0].Park.Message)
	}
	if strings.Contains(specs[0].Park.Message, "unblock[") {
		t.Fatal("a cycle has no checkable condition; encoding one would have the sweep evaluate a diagnosis nobody made")
	}
}

// TestIssueSource_AnIssueOwnedByALiveSessionIsNotOffered is the issue's
// double-adoption acceptance check at the source.
func TestIssueSource_AnIssueOwnedByALiveSessionIsNotOffered(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	forge.putIssue(26, "Add the other thing", "task:ready")
	source.SetLiveWorkReader(&staticLiveWork{refs: map[string]struct{}{"acme/api#25": {}}})

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Source.Ref != "acme/api#26" {
		t.Fatalf("specs = %+v, want only the unowned issue", specs)
	}
}

// TestIssueSource_WithNoLiveWorkReaderThePredicatePasses: the reader is wired
// later in the daemon's start-up than the source is built, and a deployment that
// has been adopting issues correctly must not stop because it is absent.
func TestIssueSource_WithNoLiveWorkReaderThePredicatePasses(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %+v, want the issue offered", specs)
	}
	// A nil reader is ignored rather than replacing a live one.
	source.SetLiveWorkReader(&staticLiveWork{refs: map[string]struct{}{"acme/api#25": {}}})
	source.SetLiveWorkReader(nil)
	specs, err = source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 0 {
		t.Fatalf("SetLiveWorkReader(nil) must not clear the reader that was wired: %+v", specs)
	}
}

// TestIssueSource_AnUnreadableLeaseStoreIsAnOutage: the ownership predicate's
// evidence being unreadable must stop the sweep rather than adopt work another
// session may already hold.
func TestIssueSource_AnUnreadableLeaseStoreIsAnOutage(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(25, "Add the thing", "task:ready")
	source.SetLiveWorkReader(&staticLiveWork{err: backendError("List", errors.New("kv down"))})

	_, err := source.Poll(context.Background(), testOrchProject())
	var unavailable *orchestration.TaskSourceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("an unreadable lease store must be reported as an outage, got %v", err)
	}
}

// TestIssueSource_RefusesAnIssueWithNoAcceptanceCriteria: opt-in, and when it is
// on an under-specified issue is not offered at all.
func TestIssueSource_RefusesAnIssueWithNoAcceptanceCriteria(t *testing.T) {
	source, forge, store := newIssueSourceRig(t)
	binding := testBinding()
	binding.Readiness.RequireAcceptanceCriteria = true
	if err := store.PutBinding(context.Background(), testTenant, binding); err != nil {
		t.Fatalf("PutBinding: %v", err)
	}
	bare := forge.putIssue(1, "Make it faster", "task:ready")
	bare.Body = "no criteria here"
	stated := forge.putIssue(2, "Make it correct", "task:ready")
	stated.Body = "## Verification\n- make test\n"

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Source.Ref != "acme/api#2" {
		t.Fatalf("specs = %+v, want only the issue that states what done is", specs)
	}
}

// TestIssueSource_SetClockIsHonoured: the clock feeds the readiness evaluator and
// the live-work read, so a test can place both at a fixed instant.
func TestIssueSource_SetClockIsHonoured(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(1, "x", "task:ready")
	source.SetClock(func() time.Time { return readinessClock })
	if !source.now().Equal(readinessClock) {
		t.Fatal("SetClock must replace the source's clock")
	}
	source.SetClock(nil)
	if !source.now().Equal(readinessClock) {
		t.Fatal("SetClock(nil) must not clear the clock that was set")
	}
	if _, err := source.Poll(context.Background(), testOrchProject()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
}

// TestIssueSource_AnUndecidableIssueIsNotOffered: a dangling dependency reference
// is undecidable for that issue only, and its neighbours are still judged.
func TestIssueSource_AnUndecidableIssueIsNotOffered(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	dangling := forge.putIssue(1, "dangling", "task:ready")
	dangling.Body = "Blocked by: #9999\n"
	forge.putIssue(2, "fine", "task:ready")

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(specs) != 1 || specs[0].Source.Ref != "acme/api#2" {
		t.Fatalf("specs = %+v, want only the decidable issue", specs)
	}
}
