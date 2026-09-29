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

// triageVerdictFixture is a needs-info verdict as triage posts it: headline,
// evidence, and the machine marker line.
const triageVerdictFixture = "Triage: this issue needs information before a worker should touch it.\n\n" +
	"Which key size should the new signer default to, 2048 or 4096?\n" +
	"<!-- " + triageCommentMarker + " head=abc123 verdict=needs_info -->\n"

// specsByRef indexes a poll by provenance ref.
func specsByRef(specs []orchestration.TaskSourceSpec) map[string]orchestration.TaskSourceSpec {
	byRef := make(map[string]orchestration.TaskSourceSpec, len(specs))
	for _, spec := range specs {
		byRef[spec.Source.Ref] = spec
	}
	return byRef
}

// TestIssueSource_ANeedsInfoIssueIsOfferedParkedWithTriagesQuestion is X4: an
// issue triage labelled needs-info, with no task behind it, is offered as a
// task adopted parked, carrying triage's question where the inbox reads it.
func TestIssueSource_ANeedsInfoIssueIsOfferedParkedWithTriagesQuestion(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(40, "Pick the key size", "factory:needs-info")
	forge.comments[40] = []ForgeComment{{Body: triageVerdictFixture, Author: "agent", Created: time.Unix(100, 0)}}

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	spec, ok := specsByRef(specs)["acme/api#40"]
	if !ok || len(specs) != 1 {
		t.Fatalf("specs = %+v, want exactly the needs-info issue", specs)
	}
	if spec.Park == nil || spec.Park.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Fatalf("park = %+v, want a needs_info park", spec.Park)
	}
	if spec.Runner != JobRunnerName || spec.Subject != "Pick the key size" || !strings.Contains(spec.Description, "body of 40") {
		t.Fatalf("spec = %+v", spec)
	}
	if err := spec.Validate(TaskSourceKindForgeIssue); err != nil {
		t.Fatalf("the engine would refuse the spec: %v", err)
	}
	condition, err := ParseUnblockCondition(spec.Park.Message)
	if err != nil || condition.Kind != UnblockOwnerRuling {
		t.Fatalf("park reason %q: condition %+v, %v; want owner_ruling", spec.Park.Message, condition, err)
	}
	if strings.Contains(spec.Park.Message, triageCommentMarker) {
		t.Errorf("the triage marker leaked into the park reason: %q", spec.Park.Message)
	}
	_, detail, _ := blockedCondition(spec.Park.Message)
	if ask := ExtractAsk(detail); !strings.Contains(ask.Question, "2048 or 4096") {
		t.Errorf("the inbox would read question %q, want triage's question", ask.Question)
	}
}

// TestIssueSource_AHeldIssueIsOfferedParkedNamingItsLabel covers an issue
// triage never spoke on: the portfolio hold alone parks it, and the reason
// names the label.
func TestIssueSource_AHeldIssueIsOfferedParkedNamingItsLabel(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(41, "Lead-driven change", HoldLabel)

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	spec, ok := specsByRef(specs)["acme/api#41"]
	if !ok || spec.Park == nil || spec.Park.Kind != orchestration.TaskBlockKindBlocked {
		t.Fatalf("specs = %+v, want the held issue parked blocked", specs)
	}
	if !strings.Contains(spec.Park.Message, HoldLabel) {
		t.Errorf("park reason %q does not name the hold", spec.Park.Message)
	}
}

// TestIssueSource_ParkedListingSkipsWhatItMustNot: a closed issue, a
// triggered one (the triggered listing owns it), and an issue carrying two
// park labels offered once, under the stronger one.
func TestIssueSource_ParkedListingSkipsWhatItMustNot(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	closed := forge.putIssue(50, "Closed", "factory:needs-info")
	closed.State = "closed"
	forge.putIssue(51, "Triggered and asked", "task:ready", "factory:needs-info")
	forge.putIssue(52, "Two labels", "factory:blocked", "factory:needs-info")

	specs, err := source.Poll(context.Background(), testOrchProject())
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	byRef := specsByRef(specs)
	if _, ok := byRef["acme/api#50"]; ok {
		t.Error("a closed issue was offered")
	}
	counted := 0
	for _, spec := range specs {
		if spec.Source.Ref == "acme/api#52" {
			counted++
		}
	}
	if counted != 1 || byRef["acme/api#52"].Park.Kind != orchestration.TaskBlockKindNeedsInfo {
		t.Errorf("issue 52 offered %d time(s) as %+v, want once as needs_info", counted, byRef["acme/api#52"].Park)
	}
	for _, spec := range specs {
		if spec.Source.Ref == "acme/api#51" && spec.Park != nil && strings.Contains(spec.Park.Message, "records no question") {
			t.Error("a triggered issue was offered by the parked listing")
		}
	}
}

// TestIssueSource_ParkedListingReportsAnUnreadableDiscussionAsAnOutage is the
// error side: a thread that cannot be read fails the poll as unavailable,
// never offers a park with no question.
func TestIssueSource_ParkedListingReportsAnUnreadableDiscussionAsAnOutage(t *testing.T) {
	source, forge, _ := newIssueSourceRig(t)
	forge.putIssue(60, "Pick the key size", "factory:needs-info")
	forge.listCommentsErr = backendError("ListIssueComments", errors.New("502"))

	_, err := source.Poll(context.Background(), testOrchProject())
	var unavailable *orchestration.TaskSourceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Poll = %v, want a TaskSourceUnavailableError", err)
	}
}

// TestParkLabels_SkipUnconfiguredAndDuplicateLabels pins the label set.
func TestParkLabels_SkipUnconfiguredAndDuplicateLabels(t *testing.T) {
	binding := testBinding()
	binding.Forge.Labels[LabelBlocked] = HoldLabel // a binding displaying the hold as its own blocked label
	labels := parkLabels(binding)
	var names []string
	for _, park := range labels {
		names = append(names, park.label)
	}
	if strings.Join(names, ",") != "factory:needs-info,"+HoldLabel {
		t.Fatalf("park labels = %v, want needs-info then the hold once (escalated is unconfigured)", names)
	}
	if _, ok := issueParkLabel(binding, &ForgeIssue{Labels: []string{"bug"}}); ok {
		t.Error("an issue with no park label reported one")
	}
}
