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
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The triage runner, driven end to end
// =============================================================================
//
// THE JUDGEMENT IS NOT DOUBLED. Every test here drives a real agent reply
// through the real declaration parser and the real verdict table, and then
// asserts on what reached the forge. The doubles stand in for the transport
// (the agent service) and the forge API, never for the decision — a test that
// stubbed ClassifyTriage would prove only that the stub was called.

// triageRig is one triage run's collaborators.
type triageRig struct {
	t         *testing.T
	forge     *fakeForge
	store     *Store
	agents    *fakeAgents
	container *fakeContainer
	runner    *TriageRunner
	root      string
	binding   *ProjectBinding

	// signing is the agent the session's checkout authenticates against. It
	// is the rig's, so a test can ask which SCOPE triage requested — which is
	// the property that keeps triage and delivery from drifting apart again
	// (go-quicbaas#929).
	signing *fakeSigningAgent

	// project is the project the job runs against. A test replaces it to move
	// the repository onto another transport; nil takes the https fixture.
	project *orchestration.Project

	// tokens is the forge token reader the session's spawn resolves through,
	// so a test can take the credential away.
	tokens *fakeTokens

	auditMu sync.Mutex
	audits  []orchestration.JobAuditEvent

	// snapshots is every status the run PUBLISHED, which is the only place a
	// runner's Decorate is observable: the stall detector and the UI read
	// published snapshots, not the run's private fields.
	snapshotMu sync.Mutex
	snapshots  []orchestration.JobStatus
}

// snapshotsIn is every status the run published while it was in one state.
func (r *triageRig) snapshotsIn(state orchestration.JobState) []orchestration.JobStatus {
	r.snapshotMu.Lock()
	defer r.snapshotMu.Unlock()
	var in []orchestration.JobStatus
	for _, status := range r.snapshots {
		if status.State == state {
			in = append(in, status)
		}
	}
	return in
}

// newTriageRig builds a bound project whose forge holds the given issues.
func newTriageRig(t *testing.T) *triageRig {
	t.Helper()
	ctx := context.Background()
	store := newTestStore(t)
	binding := testBinding()
	if err := store.PutBinding(ctx, testTenant, binding); err != nil {
		t.Fatalf("put binding: %v", err)
	}
	forge := newFakeForge()
	forge.forgeType = binding.Forge.Type
	// The container is the only place triage's checkout can be made, so the
	// rig models one: a triage run with no container is a refusal, which is
	// its own test rather than the shape every other test runs in.
	container := &fakeContainer{checkout: filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)}
	root := testRoot(t)
	agents := &fakeAgents{container: container, host: executingHost{root: root}}
	signing := &fakeSigningAgent{}
	tokens := &fakeTokens{token: []byte("t0ken")}
	rig := &triageRig{
		t: t, forge: forge, store: store, agents: agents, container: container,
		root: root, binding: binding, signing: signing, tokens: tokens,
	}
	runner, err := NewTriageRunner(TriageRunnerConfig{
		Bindings:     store,
		Clusters:     store,
		Forges:       &fakeForges{forge: forge},
		Tokens:       tokens,
		Signing:      signing,
		CheckoutRoot: orchestration.WorkspaceCheckoutRoot,
		Guard:        testRepoGuard(t),
		SocketsRoot:  testSocketsRoot(t),
	})
	if err != nil {
		t.Fatalf("NewTriageRunner: %v", err)
	}
	rig.runner = runner
	return rig
}

// onSSH moves the project onto an ssh remote and pins the forge's host key on
// the binding, which is the shape the burn-in node runs: the remote triage
// could not clone at all before go-quicbaas#929.
func (r *triageRig) onSSH() {
	r.t.Helper()
	project := testOrchProject()
	project.Git.RepoURL = "ssh://git@forge.test:2222/acme/api.git"
	r.project = project
	r.binding.Forge.SSHHostKey = testSSHHostKey
	if err := r.store.PutBinding(context.Background(), testTenant, r.binding); err != nil {
		r.t.Fatalf("put binding: %v", err)
	}
}

// answers scripts the triage agent's reply for each turn, in order. A turn
// past the end of the list answers with the last entry, so a test that cares
// about one issue does not have to enumerate the rest.
func (r *triageRig) answers(replies ...string) {
	r.agents.mu.Lock()
	defer r.agents.mu.Unlock()
	r.agents.replyFn = func(turn int) *orchestration.PromptSessionResponse {
		if len(replies) == 0 {
			return nil
		}
		if turn > len(replies) {
			turn = len(replies)
		}
		return workerSaid(replies[turn-1])
	}
}

// run drives one triage job to completion.
func (r *triageRig) run() error {
	r.t.Helper()
	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		r.t.Fatal(err)
	}
	project := r.project
	if project == nil {
		project = testOrchProject()
	}
	project.Workspace = orchestration.ProjectWorkspace{Path: r.root}
	job := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:      "job-triage-1",
		Runner:     TriageRunnerName,
		Config:     &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, Attempt: 1},
		Project:    project,
		StartState: orchestration.JobStateInitializing,
		Agents:     r.agents,
		Logger:     logger.NewAdapter(base),
		AuditFn: func(eventType, details string) {
			r.auditMu.Lock()
			r.audits = append(r.audits, orchestration.JobAuditEvent{Type: eventType, Details: details})
			r.auditMu.Unlock()
		},
		PersistFn: func(status *orchestration.JobStatus) {
			r.snapshotMu.Lock()
			r.snapshots = append(r.snapshots, *status)
			r.snapshotMu.Unlock()
		},
	})
	return r.runner.Run(context.Background(), job)
}

// labels returns an issue's labels as the forge holds them.
func (r *triageRig) labels(number int) []string {
	return r.forge.issueLabels(number)
}

// comments returns the bodies of every comment on an issue.
func (r *triageRig) comments(number int) []string {
	var bodies []string
	for _, comment := range r.forge.commentsOn(number) {
		bodies = append(bodies, comment.Body)
	}
	return bodies
}

// putComment seeds a comment on an issue with a chosen creation instant, so
// a test can place triage's own question before or after the issue was last
// touched. It lives here rather than in the shared fixtures because only the
// triage tests need a comment whose timestamp they choose.
func (f *fakeForge) putComment(number int, body string, created time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextCommentID++
	f.comments[number] = append(f.comments[number], ForgeComment{
		ID: f.nextCommentID, Body: body, Author: "agent", Created: created,
	})
}

// hasLabel reports whether an issue carries a label.
func (r *triageRig) hasLabel(number int, label string) bool {
	for _, held := range r.labels(number) {
		if held == label {
			return true
		}
	}
	return false
}

// =============================================================================
// The three verdicts
// =============================================================================

// TestTriageRunner_ReadyAppliesTheTriggerLabel is the happy path: an issue the
// agent finds ready is admitted to the factory, and admitted by the ONE thing
// that admits it — the binding's trigger label.
func TestTriageRunner_ReadyAppliesTheTriggerLabel(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(101, "Parse returns an untyped error on malformed input")
	rig.answers("The objective is verifiable and scoped to one change.\n\nSDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rig.hasLabel(101, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("a ready issue carries the trigger label, got %v", rig.labels(101))
	}
	if comments := rig.comments(101); len(comments) != 0 {
		t.Fatalf("the ready verdict speaks through its label alone, got %v", comments)
	}
}

// TestTriageRunner_NeedsInfoLabelsAndNamesWhatIsMissing proves the
// needs-info verdict both labels and says exactly what it needs, in the
// agent's own words.
func TestTriageRunner_NeedsInfoLabelsAndNamesWhatIsMissing(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(102, "Fix the encoder")
	rig.answers("The issue does not say which of the three Encode overloads is affected.\n\n" +
		"SDLC-TRIAGE-QUESTION: Which of the three Encode overloads is affected?\nSDLC-TRIAGE: needs-info")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	needsInfo := rig.binding.Forge.Label(LabelNeedsInfo)
	if !rig.hasLabel(102, needsInfo) {
		t.Fatalf("a needs-info issue carries the needs-info label, got %v", rig.labels(102))
	}
	if rig.hasLabel(102, rig.binding.Forge.TriggerLabel) {
		t.Fatal("a needs-info issue must never carry the trigger label")
	}
	comments := rig.comments(102)
	if len(comments) != 1 {
		t.Fatalf("needs-info comments exactly once, got %d", len(comments))
	}
	if !strings.Contains(comments[0], "which of the three Encode overloads") {
		t.Fatalf("the comment must name what is missing, got:\n%s", comments[0])
	}
}

// TestTriageRunner_NotActionableCommentsAndLeavesItAlone proves the third
// verdict writes evidence and touches no label at all.
func TestTriageRunner_NotActionableCommentsAndLeavesItAlone(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(103, "Rewrite the storage layer in Rust")
	rig.answers("This is an architecture decision reserved for the owner.\n\nSDLC-TRIAGE: not-actionable")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if labels := rig.labels(103); len(labels) != 0 {
		t.Fatalf("not-actionable writes no label, got %v", labels)
	}
	comments := rig.comments(103)
	if len(comments) != 1 {
		t.Fatalf("not-actionable comments exactly once, got %d", len(comments))
	}
	if !strings.Contains(comments[0], "architecture decision reserved for the owner") {
		t.Fatalf("the comment must carry the evidence, got:\n%s", comments[0])
	}
}

// =============================================================================
// The two checks that earn their keep
// =============================================================================

// TestTriageRunner_AlreadyDoneWorkIsNeverReady is the go-truststrap case: an
// issue whose work is already present in the checkout must not reach a
// worker, and the evidence must reach the issue.
func TestTriageRunner_AlreadyDoneWorkIsNeverReady(t *testing.T) {
	tests := []struct {
		name  string
		reply string
	}{
		{
			name:  "reported as not-actionable",
			reply: "main already carries this: README.md:1 documents the new CLI.\n\nSDLC-TRIAGE: not-actionable",
		},
		{
			name: "reported as needs-info when the agent could not be certain",
			reply: "README.md:1 looks current, but the issue names no specific section.\n\n" +
				"SDLC-TRIAGE-QUESTION: Which README section is stale?\nSDLC-TRIAGE: needs-info",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newTriageRig(t)
			rig.forge.putIssue(104, "README is stale")
			rig.answers(tc.reply)

			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			if rig.hasLabel(104, rig.binding.Forge.TriggerLabel) {
				t.Fatal("work already on main must never be handed to a worker")
			}
			comments := rig.comments(104)
			if len(comments) != 1 {
				t.Fatalf("the evidence reaches the issue exactly once, got %d", len(comments))
			}
			if !strings.Contains(comments[0], "README.md:1") {
				t.Fatalf("the comment must carry the file-and-line evidence, got:\n%s", comments[0])
			}
		})
	}
}

// TestTriageRunner_DuplicateIsIdentifiedAndNamed is the six-identical-issues
// case reduced to two: the second issue is identified as a duplicate of the
// first, is not handed to a worker, and the comment says WHICH issue.
func TestTriageRunner_DuplicateIsIdentifiedAndNamed(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(25, "README.md is stale")
	rig.forge.putIssue(52, "README.md is stale")
	// The first issue is ready; the second is the same work.
	rig.answers(
		"A verifiable objective, scoped to one change.\n\nSDLC-TRIAGE: ready",
		"The same stale README as #25, word for word.\n\nSDLC-TRIAGE-DUPLICATE: #25\nSDLC-TRIAGE: duplicate",
	)

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rig.hasLabel(25, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("the first issue is the one that gets worked, got %v", rig.labels(25))
	}
	if rig.hasLabel(52, rig.binding.Forge.TriggerLabel) {
		t.Fatal("a duplicate must never be handed to a worker")
	}
	comments := rig.comments(52)
	if len(comments) != 1 {
		t.Fatalf("the duplicate is reported exactly once, got %d", len(comments))
	}
	if !strings.Contains(comments[0], "#25") {
		t.Fatalf("the comment must name the issue it duplicates, got:\n%s", comments[0])
	}
}

// TestTriageRunner_DuplicateCandidatesReachThePrompt proves the agent is
// actually GIVEN the other open issues. Without this the duplicate verdict
// would depend on the agent guessing, and the test above would pass against a
// prompt that named nothing.
func TestTriageRunner_DuplicateCandidatesReachThePrompt(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(25, "README.md is stale")
	rig.forge.putIssue(52, "README.md is stale")
	rig.answers("SDLC-TRIAGE: needs-info")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	prompts := rig.agents.promptTexts()
	if len(prompts) != 2 {
		t.Fatalf("both issues are triaged, got %d prompts", len(prompts))
	}
	// The prompt about #25 offers #52 as a candidate, and vice versa.
	if !strings.Contains(prompts[0], "#52") {
		t.Fatalf("the prompt for #25 must offer #52 as a duplicate candidate:\n%s", prompts[0])
	}
	if !strings.Contains(prompts[1], "#25") {
		t.Fatalf("the prompt for #52 must offer #25 as a duplicate candidate:\n%s", prompts[1])
	}
}

// =============================================================================
// Triage never closes an issue
// =============================================================================

// TestTriageRunner_NeverClosesAnIssue proves the property directly: whatever
// the agent says, every issue is still open and the only writes that reached
// the forge were labels and comments.
//
// The ForgeBackend interface has no close verb at all, so this is belt and
// braces — but the property is the one the owner named, and a test that
// asserts it will fail loudly the day somebody adds one and wires it here.
func TestTriageRunner_NeverClosesAnIssue(t *testing.T) {
	replies := []string{
		"SDLC-TRIAGE: ready",
		"SDLC-TRIAGE: needs-info",
		"main already has this.\n\nSDLC-TRIAGE: not-actionable",
		"SDLC-TRIAGE-DUPLICATE: #201\nSDLC-TRIAGE: duplicate",
		"Please close this, it is obsolete and should be deleted.\n\nSDLC-TRIAGE: not-actionable",
	}
	rig := newTriageRig(t)
	for i := range replies {
		rig.forge.putIssue(201+i, "issue")
	}
	rig.answers(replies...)

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	for i := range replies {
		number := 201 + i
		issue, err := rig.forge.GetIssue(context.Background(), testRepoRef, number)
		if err != nil {
			t.Fatalf("get issue %d: %v", number, err)
		}
		if issueClosed(issue) {
			t.Fatalf("issue #%d was closed; triage may only move labels", number)
		}
	}
}

// =============================================================================
// A reply the binary cannot read is never ready
// =============================================================================

// TestTriageRunner_AnUnreadableReplyIsNeverReady proves the safe direction:
// every way of failing to understand the agent leaves the issue for a person,
// and none of them applies the trigger label.
func TestTriageRunner_AnUnreadableReplyIsNeverReady(t *testing.T) {
	tests := []struct {
		name  string
		reply string
	}{
		{
			name:  "prose that sounds approving but declares nothing",
			reply: "This issue looks great and is absolutely ready to be worked on.",
		},
		{
			name:  "a code outside the vocabulary",
			reply: "SDLC-TRIAGE: probably-fine",
		},
		{
			name:  "a duplicate naming no issue",
			reply: "This duplicates something.\n\nSDLC-TRIAGE: duplicate",
		},
		{
			name:  "an empty reply",
			reply: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newTriageRig(t)
			rig.forge.putIssue(301, "some issue")
			rig.answers(tc.reply)

			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			if rig.hasLabel(301, rig.binding.Forge.TriggerLabel) {
				t.Fatalf("an unreadable verdict must never be read as ready, got %v", rig.labels(301))
			}
			if got := rig.comments(301); len(got) != 0 {
				t.Fatalf("a reply that is not a verdict writes nothing to the issue, got %v", got)
			}
			if got := rig.labels(301); len(got) != 0 {
				t.Fatalf("a reply that is not a verdict labels nothing, got %v", got)
			}
			if !rig.audited("sdlc:triage_unusable") {
				t.Fatal("setting a reply aside is recorded on the audit trail")
			}
		})
	}
}

// TestTriageRunner_AVerdictWithoutEvidenceIsNotAVerdict proves the charter's
// demand for evidence is enforced: a declaration with no prose, a duplicate
// of the issue itself, and a duplicate of an issue the pass never listed are
// all set aside unposted. Each is a shape the NPU model produced on go-luks2
// on 2026-09-21 and a person then had to undo.
func TestTriageRunner_AVerdictWithoutEvidenceIsNotAVerdict(t *testing.T) {
	tests := map[string]string{
		"a bare not-actionable":            "SDLC-TRIAGE: not-actionable",
		"a bare needs-info":                "SDLC-TRIAGE: needs-info",
		"a duplicate of itself":            "Same thing.\n\nSDLC-TRIAGE-DUPLICATE: #301\nSDLC-TRIAGE: duplicate",
		"a duplicate of an unlisted issue": "Same thing.\n\nSDLC-TRIAGE-DUPLICATE: #999\nSDLC-TRIAGE: duplicate",
	}
	for name, reply := range tests {
		t.Run(name, func(t *testing.T) {
			rig := newTriageRig(t)
			rig.forge.putIssue(301, "some issue")
			rig.answers(reply)
			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := rig.comments(301); len(got) != 0 {
				t.Fatalf("nothing is posted for a reply without evidence, got %v", got)
			}
			if got := rig.labels(301); len(got) != 0 {
				t.Fatalf("nothing is labelled for a reply without evidence, got %v", got)
			}
			details, ok := rig.auditDetails("sdlc:triage_unusable")
			if !ok || !strings.Contains(details, "issue=301") {
				t.Fatalf("the audit trail names the issue set aside, got %q", details)
			}
		})
	}
}

// TestTriageRunner_StopsAskingAfterTwoUnusableRepliesAtOneHead proves the
// bound: the same runner sets an issue aside twice at one head and then skips
// it, audited, until the head moves or somebody speaks.
func TestTriageRunner_StopsAskingAfterTwoUnusableRepliesAtOneHead(t *testing.T) {
	first := newTriageRig(t)
	first.forge.putIssue(303, "some issue")
	first.answers("SDLC-TRIAGE: not-actionable")
	if err := first.run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	second := newTriageRigOver(t, first)
	second.runner = first.runner
	second.answers("SDLC-TRIAGE: not-actionable")
	if err := second.run(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !second.audited("sdlc:triage_unusable") {
		t.Fatal("the second pass still asks, and sets the reply aside")
	}
	third := newTriageRigOver(t, first)
	third.runner = first.runner
	third.answers("This would have been a fine verdict.\n\nSDLC-TRIAGE: not-actionable")
	if err := third.run(); err != nil {
		t.Fatalf("third run: %v", err)
	}
	if third.audited("sdlc:triage_verdict") {
		t.Fatal("the third pass must not ask again at the same head")
	}
	if details, ok := third.auditDetails("sdlc:triage_skipped"); !ok || !strings.Contains(details, "issue=303") {
		t.Fatalf("the skip is audited with the issue, got %q", details)
	}
	if got := third.comments(303); len(got) != 0 {
		t.Fatalf("a skipped issue is not written to, got %v", got)
	}
}

// TestTriageRunner_MeasuredFactsReachThePrompt proves the deterministic half
// of the already-done check is done by the runner: the files and identifiers
// the issue names are probed in the session's checkout and the answers are in
// the prompt the model reads, audited.
func TestTriageRunner_MeasuredFactsReachThePrompt(t *testing.T) {
	rig := newTriageRig(t)
	rig.container.seedFile("pkg/luks2/kdf.go", "package luks2\nfunc DeriveKey() {}")
	rig.forge.putIssue(401, "kdf: `DeriveKey` in pkg/luks2/kdf.go leaks; see pkg/luks2/gone.go and `Vanished`")
	rig.answers("Grounded.\n\nSDLC-TRIAGE: not-actionable")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	rig.agents.mu.Lock()
	prompts := rig.agents.prompts
	rig.agents.mu.Unlock()
	if len(prompts) == 0 {
		t.Fatal("the issue was never prompted")
	}
	prompt := prompts[len(prompts)-1].Prompt
	for _, want := range []string{
		"## Repository facts, measured by the factory",
		"`pkg/luks2/kdf.go` — present",
		"`pkg/luks2/gone.go` — ABSENT",
		"`DeriveKey` — found",
		"`Vanished` — no match",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the prompt must carry %q:\n%s", want, prompt)
		}
	}
	if details, ok := rig.auditDetails("sdlc:triage_facts"); !ok || !strings.Contains(details, "paths=2") {
		t.Fatalf("the measurement is audited, got %q", details)
	}
}

// TestTriageRunner_AFailedMeasurementIsAuditedNotFatal proves a checkout that
// will not answer the probes leaves the issue judged without the section and
// says so on the audit trail.
func TestTriageRunner_AFailedMeasurementIsAuditedNotFatal(t *testing.T) {
	rig := newTriageRig(t) // no tree: ls-files and grep are refused
	rig.forge.putIssue(402, "fix pkg/luks2/kdf.go")
	rig.answers("Out of scope.\n\nSDLC-TRIAGE: not-actionable")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rig.audited("sdlc:triage_facts_failed") {
		t.Fatal("a failed measurement is audited")
	}
	if got := rig.comments(402); len(got) != 1 {
		t.Fatalf("the issue is still judged, got %v", got)
	}
	rig.agents.mu.Lock()
	prompt := rig.agents.prompts[len(rig.agents.prompts)-1].Prompt
	rig.agents.mu.Unlock()
	if strings.Contains(prompt, "Repository facts") {
		t.Fatal("no facts section is rendered when the measurement failed")
	}
}

// TestTriageRunner_AnUnusableReplyLeavesTheIssueForTheNextPass proves the
// point of not posting: the issue carries no word from triage, so the next
// pass judges it again -- and a usable reply then lands normally.
func TestTriageRunner_AnUnusableReplyLeavesTheIssueForTheNextPass(t *testing.T) {
	first := newTriageRig(t)
	first.forge.putIssue(302, "some issue")
	first.answers("SDLC-TRIAGE: not-actionable")
	if err := first.run(); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if got := first.comments(302); len(got) != 0 {
		t.Fatalf("the first pass must post nothing, got %v", got)
	}

	second := newTriageRigOver(t, first)
	second.answers("The change is out of scope for this repository.\n\nSDLC-TRIAGE: not-actionable")
	if err := second.run(); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if got := second.comments(302); len(got) != 1 || !strings.Contains(got[0], "out of scope") {
		t.Fatalf("the second pass judges the issue again and posts its verdict, got %v", got)
	}
}

// =============================================================================
// Which issues are candidates
// =============================================================================

// TestTriageRunner_SkipsIssuesAlreadyInTheFactory proves triage does not
// re-judge work that is already moving: an issue carrying the trigger label,
// or any of the module's own progress labels, is left alone.
func TestTriageRunner_SkipsIssuesAlreadyInTheFactory(t *testing.T) {
	rig := newTriageRig(t)
	binding := rig.binding
	rig.forge.putIssue(401, "already triggered", binding.Forge.TriggerLabel)
	rig.forge.putIssue(402, "already being worked", binding.Forge.Label(LabelInProgress))
	rig.forge.putIssue(403, "already in review", binding.Forge.Label(LabelInReview))
	rig.forge.putIssue(404, "fresh and untriaged")
	rig.answers("SDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 1 {
		t.Fatalf("only the untriaged issue is judged, got %d prompts", len(prompts))
	}
	if !rig.hasLabel(404, binding.Forge.TriggerLabel) {
		t.Fatalf("the fresh issue is the one triaged, got %v", rig.labels(404))
	}
}

// TestTriageRunner_SkipsClosedIssues proves a closed issue is not triaged:
// there is no work to admit and no person waiting on a verdict.
func TestTriageRunner_SkipsClosedIssues(t *testing.T) {
	rig := newTriageRig(t)
	issue := rig.forge.putIssue(405, "closed already")
	issue.State = "closed"
	rig.answers("SDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("a closed issue is not triaged, got %d prompts", len(prompts))
	}
}

// TestTriageRunner_NoCandidatesSpawnsNothing proves the cheap path is cheap:
// a project with nothing to triage must not pay for a container.
func TestTriageRunner_NoCandidatesSpawnsNothing(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(406, "already triggered", rig.binding.Forge.TriggerLabel)

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if spawns := rig.agents.sessionCount(); spawns != 0 {
		t.Fatalf("nothing to triage must spawn no session, got %d", spawns)
	}
}

// =============================================================================
// One session, closed
// =============================================================================

// TestTriageRunner_UsesOneSessionAndClosesIt proves the run holds exactly one
// session however many issues it judges, and gives it back.
func TestTriageRunner_UsesOneSessionAndClosesIt(t *testing.T) {
	rig := newTriageRig(t)
	for i := 0; i < 3; i++ {
		rig.forge.putIssue(501+i, "issue")
	}
	rig.answers("SDLC-TRIAGE: needs-info")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if spawns := rig.agents.sessionCount(); spawns != TriageSessionCount {
		t.Fatalf("triage holds %d session, got %d", TriageSessionCount, spawns)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 3 {
		t.Fatalf("one turn per issue, got %d", len(prompts))
	}
	rig.agents.mu.Lock()
	closed := len(rig.agents.closed)
	rig.agents.mu.Unlock()
	if closed != 1 {
		t.Fatalf("the session is given back, got %d closes", closed)
	}
}

// TestTriageRunner_OneIssueFailingDoesNotStopTheSweep proves a forge write
// that fails on one issue still lets the rest be triaged. The sweep is the
// only path the others have.
func TestTriageRunner_OneIssueFailingDoesNotStopTheSweep(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(601, "first")
	rig.forge.putIssue(602, "second")
	rig.forge.commentErr = errors.New("forge refused the comment")
	rig.answers(
		"SDLC-TRIAGE: needs-info",
		"SDLC-TRIAGE: ready",
	)

	err := rig.run()
	if err != nil {
		t.Fatalf("a per-issue failure is reported, not fatal: %v", err)
	}
	if !rig.hasLabel(602, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("the second issue is still triaged, got %v", rig.labels(602))
	}
}

// =============================================================================
// The runner's contract with the engine
// =============================================================================

// TestTriageRunner_IsRegisteredAsAJobRunner proves the runner satisfies the
// engine's seams, including the optional lease policy triage depends on.
func TestTriageRunner_IsRegisteredAsAJobRunner(t *testing.T) {
	rig := newTriageRig(t)
	var runner orchestration.JobRunner = rig.runner
	if runner.Name() != TriageRunnerName {
		t.Fatalf("name: want %q, got %q", TriageRunnerName, runner.Name())
	}
	if len(runner.States()) == 0 {
		t.Fatal("a runner must declare its states")
	}
	policy, ok := runner.(orchestration.JobLeasePolicy)
	if !ok {
		t.Fatal("triage must declare a lease policy; without it it queues behind workers")
	}
	if !policy.OffProjectBudget() {
		t.Fatal("triage must be off the project's worker budget")
	}
	subject := policy.LeaseSubject(&orchestration.JobConfig{ProjectID: "proj-1"})
	if subject != TriageLeaseSubject("proj-1") {
		t.Fatalf("lease subject: want %q, got %q", TriageLeaseSubject("proj-1"), subject)
	}
	// Two projects do not contend; two jobs over one project do.
	if TriageLeaseSubject("proj-1") == TriageLeaseSubject("proj-2") {
		t.Fatal("triage of two projects must not serialize")
	}
}

// TestTriageRunner_ValidateRefusesWhatItCannotDrive proves the refusals
// happen before any goroutine or lease exists.
func TestTriageRunner_ValidateRefusesWhatItCannotDrive(t *testing.T) {
	rig := newTriageRig(t)
	tests := []struct {
		name  string
		cfg   *orchestration.JobConfig
		field string
	}{
		{name: "a nil config", cfg: nil, field: "config"},
		{name: "no project", cfg: &orchestration.JobConfig{}, field: "project_id"},
		{
			name:  "a task-bound job",
			cfg:   &orchestration.JobConfig{ProjectID: "proj-1", TaskID: "task-1"},
			field: "task_id",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := rig.runner.Validate(context.Background(), tc.cfg)
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("want *InvalidInputError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("field: want %q, got %q", tc.field, invalid.Field)
			}
		})
	}
	if err := rig.runner.Validate(context.Background(), &orchestration.JobConfig{ProjectID: "proj-1"}); err != nil {
		t.Fatalf("a project-scoped triage job is valid: %v", err)
	}
}

// TestNewTriageRunner_RefusesAnIncompleteWiring proves every collaborator is
// required, named, and refused as a typed error.
func TestNewTriageRunner_RefusesAnIncompleteWiring(t *testing.T) {
	store := newTestStore(t)
	forges := &fakeForges{forge: newFakeForge()}
	tokens := &fakeTokens{token: []byte("t")}
	signing := &fakeSigningAgent{}
	sockets := testSocketsRoot(t)
	tests := []struct {
		name  string
		cfg   TriageRunnerConfig
		field string
	}{
		{
			name:  "no bindings",
			cfg:   TriageRunnerConfig{Forges: forges, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
			field: "bindings",
		},
		{
			name:  "no forges",
			cfg:   TriageRunnerConfig{Bindings: store, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
			field: "forges",
		},
		{
			name:  "no tokens",
			cfg:   TriageRunnerConfig{Bindings: store, Forges: forges, Clusters: store, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
			field: "tokens",
		},
		{
			// THE ONE go-quicbaas#929 ADDS. A triage runner built without an
			// ssh agent cannot authenticate a clone of an ssh remote, and the
			// only place that became visible was a container it had already
			// spawned. It is refused at construction instead.
			name:  "no signing agent",
			cfg:   TriageRunnerConfig{Bindings: store, Forges: forges, Clusters: store, Tokens: tokens, Guard: testRepoGuard(t), SocketsRoot: sockets},
			field: "signing",
		},
		{
			// Clustering is on by default, and a runner that could not record
			// a cluster would adopt its members one by one on the next pass.
			name:  "no cluster store",
			cfg:   TriageRunnerConfig{Bindings: store, Forges: forges, Tokens: tokens, Signing: signing, Guard: testRepoGuard(t), SocketsRoot: sockets},
			field: "clusters",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTriageRunner(tc.cfg)
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) {
				t.Fatalf("want *InvalidInputError, got %T: %v", err, err)
			}
			if invalid.Field != tc.field {
				t.Fatalf("field: want %q, got %q", tc.field, invalid.Field)
			}
		})
	}
}

// TestTriageRunner_RefusesAnUnboundProject proves a project with no binding
// is a typed refusal naming the project, not a nil dereference.
func TestTriageRunner_RefusesAnUnboundProject(t *testing.T) {
	rig := newTriageRig(t)
	if err := rig.store.DeleteBinding(context.Background(), testTenant, testProject); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
	err := rig.run()
	var unbound *TriageNotBoundError
	if !errors.As(err, &unbound) {
		t.Fatalf("want *TriageNotBoundError, got %T: %v", err, err)
	}
	if unbound.ProjectID != testProject {
		t.Fatalf("the refusal must name the project, got %q", unbound.ProjectID)
	}
}

// =============================================================================
// Re-triage after an answer
// =============================================================================
//
// The needs-info verdict is a QUESTION, and the point of asking is that
// somebody answers. These tests pin when the answer is noticed — without a
// person having to remove a label by hand, which is the manual step this
// whole capability exists to remove — and when it is not, because re-asking
// the same question on every periodic pass would be worse than not asking.

// TestTriageRunner_RetriagesANeedsInfoIssueThatWasAnswered proves an issue
// edited after triage asked its question is judged again.
func TestTriageRunner_RetriagesANeedsInfoIssueThatWasAnswered(t *testing.T) {
	rig := newTriageRig(t)
	needsInfo := rig.binding.Forge.Label(LabelNeedsInfo)
	issue := rig.forge.putIssue(701, "Fix the encoder", needsInfo)

	asked := time.Now().Add(-time.Hour)
	rig.forge.putComment(701, "Which overload?\n<!-- "+triageCommentMarker+" "+triageHeadMarker+fakeForgeHeadSHA+" -->", asked)
	// The reporter answered, in the thread, after the question. The answer is
	// a COMMENT and not a bumped timestamp on purpose: triage's own
	// needs-info label write advances the issue's Updated past its own
	// question, so a timestamp rule would read triage's second write as the
	// reporter's reply.
	rig.forge.putComment(701, "Encode(io.Writer).", asked.Add(time.Minute))
	issue.Updated = asked.Add(time.Minute)

	rig.answers("The reporter named Encode(io.Writer).\n\nSDLC-TRIAGE: ready")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rig.hasLabel(701, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("an answered issue is admitted, got %v", rig.labels(701))
	}
	if rig.hasLabel(701, needsInfo) {
		t.Fatalf("the answered question's label is cleared, got %v", rig.labels(701))
	}
}

// TestTriageRunner_LeavesAnUnansweredNeedsInfoIssueAlone proves the periodic
// pass does not re-ask a question nobody has answered.
func TestTriageRunner_LeavesAnUnansweredNeedsInfoIssueAlone(t *testing.T) {
	rig := newTriageRig(t)
	needsInfo := rig.binding.Forge.Label(LabelNeedsInfo)
	issue := rig.forge.putIssue(702, "Fix the encoder", needsInfo)

	asked := time.Now()
	rig.forge.putComment(702, "Which overload?\n<!-- "+triageCommentMarker+" "+triageHeadMarker+fakeForgeHeadSHA+" -->", asked)
	// Nothing happened since the question was asked.
	issue.Updated = asked.Add(-time.Minute)

	rig.answers("SDLC-TRIAGE: ready")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("an unanswered question is not re-asked, got %d prompts", len(prompts))
	}
	if len(rig.comments(702)) != 1 {
		t.Fatalf("no second copy of the question is posted, got %v", rig.comments(702))
	}
}

// TestTriageRunner_JudgesANeedsInfoIssueTriageNeverAsked proves a needs-info
// label something ELSE applied does not park the issue for ever: triage has
// nothing recorded to wait on, so it judges.
func TestTriageRunner_JudgesANeedsInfoIssueTriageNeverAsked(t *testing.T) {
	rig := newTriageRig(t)
	needsInfo := rig.binding.Forge.Label(LabelNeedsInfo)
	rig.forge.putIssue(703, "Fix the encoder", needsInfo)
	rig.forge.putComment(703, "a human comment with no triage marker", time.Now())

	rig.answers("Now specific enough.\n\nSDLC-TRIAGE: ready")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rig.hasLabel(703, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("the issue is judged, got %v", rig.labels(703))
	}
}

// =============================================================================
// The failure paths that write to the forge
// =============================================================================

// TestTriageRunner_ReportsAForgeWriteItCouldNotMake proves each label write
// that fails is reported against its issue rather than silently dropped,
// and — for the ready verdict — that a failure to clear needs-info does not
// then apply the trigger label. An issue that was both ready and blocked
// would be worked on while displaying that it is waiting for a person.
func TestTriageRunner_ReportsAForgeWriteItCouldNotMake(t *testing.T) {
	tests := []struct {
		name        string
		reply       string
		seedLabels  []string
		addLabelErr error
		removeErr   error
		wantTrigger bool
	}{
		{
			name:        "the trigger label is refused",
			reply:       "SDLC-TRIAGE: ready",
			addLabelErr: errors.New("forge refused the label"),
			wantTrigger: false,
		},
		{
			name:        "clearing needs-info is refused, so ready is not applied",
			reply:       "SDLC-TRIAGE: ready",
			seedLabels:  []string{"factory:needs-info"},
			removeErr:   errors.New("forge refused the removal"),
			wantTrigger: false,
		},
		{
			name:        "the needs-info label is refused after the comment landed",
			reply:       "The issue names no version.\n\nSDLC-TRIAGE-QUESTION: Which version is affected?\nSDLC-TRIAGE: needs-info",
			addLabelErr: errors.New("forge refused the label"),
			wantTrigger: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newTriageRig(t)
			issue := rig.forge.putIssue(801, "an issue", tc.seedLabels...)
			issue.Updated = time.Now()
			rig.forge.addLabelErr = tc.addLabelErr
			rig.forge.removeLabelErr = tc.removeErr
			rig.answers(tc.reply)

			// A per-issue failure is recorded, never fatal to the sweep.
			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			if got := rig.hasLabel(801, rig.binding.Forge.TriggerLabel); got != tc.wantTrigger {
				t.Fatalf("trigger label: want %v, got %v (labels %v)", tc.wantTrigger, got, rig.labels(801))
			}
			if !rig.audited("sdlc:triage_failed") {
				t.Fatal("a forge write that failed must reach the audit trail")
			}
		})
	}
}

// TestTriageRunner_RecordsEveryVerdictOnTheAuditTrail proves an operator can
// say what triage concluded about an issue without re-reading the forge.
func TestTriageRunner_RecordsEveryVerdictOnTheAuditTrail(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(901, "an issue")
	rig.answers("Evidence here.\n\nSDLC-TRIAGE: not-actionable")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	details, ok := rig.auditDetails("sdlc:triage_verdict")
	if !ok {
		t.Fatal("every verdict is recorded")
	}
	for _, want := range []string{"issue=901", "outcome=not_actionable", "disposition=not_actionable"} {
		if !strings.Contains(details, want) {
			t.Fatalf("the audit line must carry %q, got %q", want, details)
		}
	}
}

// TestTriageRunner_ReportsAnIssueWhoseDiscussionCannotBeRead proves a forge
// read failure is a typed, per-issue refusal naming the issue.
func TestTriageRunner_ReportsAnIssueWhoseDiscussionCannotBeRead(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(902, "an issue")
	rig.forge.listCommentsErr = errors.New("forge refused the listing")
	rig.answers("SDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("a per-issue read failure is not fatal: %v", err)
	}
	if rig.hasLabel(902, rig.binding.Forge.TriggerLabel) {
		t.Fatal("an issue that could not be read must not be marked ready")
	}
	if !rig.audited("sdlc:triage_failed") {
		t.Fatal("the failure must reach the audit trail")
	}
}

// TestTriageRunner_RefusesAProjectWithNoWorkspace proves the spawn's
// preconditions are typed refusals rather than a container started nowhere.
func TestTriageRunner_RefusesAProjectWithNoWorkspace(t *testing.T) {
	rig := newTriageRig(t)
	rig.agents.host.root = ""
	rig.forge.putIssue(903, "an issue")
	rig.answers("SDLC-TRIAGE: ready")

	err := rig.run()
	var unavailable *WorkspaceUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("want *WorkspaceUnavailableError, got %T: %v", err, err)
	}
}

// audited reports whether an audit event of this type was written.
func (r *triageRig) audited(eventType string) bool {
	_, ok := r.auditDetails(eventType)
	return ok
}

// auditDetails returns the details of the first audit event of a type.
func (r *triageRig) auditDetails(eventType string) (string, bool) {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	for _, event := range r.audits {
		if event.Type == eventType {
			return event.Details, true
		}
	}
	return "", false
}

// =============================================================================
// A verdict is not re-reached
// =============================================================================
//
// THIS IS THE PROPERTY THAT KEEPS TRIAGE CHEAP AND QUIET. Two of the three
// verdicts leave no label — not-actionable and duplicate comment and leave the
// issue alone by design — so nothing about the issue's LABELS says triage has
// already spoken. A sweep that read only labels would judge the same issue on
// every pass for ever: one model turn and one duplicate comment per issue per
// sweep, on every repository in the fleet. That is the exact pathology this
// runner exists to remove, so it is asserted rather than assumed.

// TestTriageRunner_ANotActionableVerdictIsNotReReached proves the second
// sweep over an issue triage already answered spends no turn and posts no
// second comment, even though the issue carries no label at all.
func TestTriageRunner_ANotActionableVerdictIsNotReReached(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(801, "Please make it better")
	rig.answers("No objective anybody could verify.\n\nSDLC-TRIAGE: not-actionable")

	if err := rig.run(); err != nil {
		t.Fatalf("first sweep: %v", err)
	}
	if got := rig.labels(801); len(got) != 0 {
		t.Fatalf("not-actionable applies no label, got %v", got)
	}
	first := rig.comments(801)
	if len(first) != 1 {
		t.Fatalf("the first sweep posts its evidence once, got %v", first)
	}

	// The same project, swept again with nothing changed.
	second := newTriageRigOver(t, rig)
	if err := second.run(); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if prompts := second.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("an answered issue costs no second turn, got %d prompts", len(prompts))
	}
	if got := second.comments(801); len(got) != 1 {
		t.Fatalf("no second copy of the verdict is posted, got %v", got)
	}
}

// TestTriageRunner_AReplyToATriagedIssueIsJudgedAgain is the other half: the
// rule must not park an issue for ever. Somebody answering — disputing the
// verdict, or answering the needs-info question — is new information, and the
// next sweep judges it.
func TestTriageRunner_AReplyToATriagedIssueIsJudgedAgain(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(802, "Please make it better")
	rig.answers("No objective anybody could verify.\n\nSDLC-TRIAGE: not-actionable")
	if err := rig.run(); err != nil {
		t.Fatalf("first sweep: %v", err)
	}

	// The reporter replies, after triage's verdict.
	rig.forge.putComment(802, "Concretely: Encode must return *EncodeError.", time.Now().Add(time.Minute))

	second := newTriageRigOver(t, rig)
	second.answers("Now verifiable.\n\nSDLC-TRIAGE: ready")
	if err := second.run(); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if !second.hasLabel(802, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("a reply is judged, got %v", second.labels(802))
	}
}

// newTriageRigOver builds a SECOND sweep over the first one's forge and
// store: a new job, a new session, the same repository state. It is how a
// periodic pass is modelled without re-seeding the forge, which would hide
// exactly the writes the first sweep made.
func newTriageRigOver(t *testing.T, first *triageRig) *triageRig {
	t.Helper()
	container := &fakeContainer{checkout: filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)}
	root := testRoot(t)
	agents := &fakeAgents{container: container, host: executingHost{root: root}}
	signing := &fakeSigningAgent{}
	runner, err := NewTriageRunner(TriageRunnerConfig{
		Bindings:     first.store,
		Clusters:     first.store,
		Forges:       &fakeForges{forge: first.forge},
		Tokens:       &fakeTokens{token: []byte("t0ken")},
		Signing:      signing,
		CheckoutRoot: orchestration.WorkspaceCheckoutRoot,
		Guard:        testRepoGuard(t),
		SocketsRoot:  testSocketsRoot(t),
	})
	if err != nil {
		t.Fatalf("NewTriageRunner: %v", err)
	}
	return &triageRig{
		t: t, forge: first.forge, store: first.store, agents: agents, container: container,
		runner: runner, root: root, binding: first.binding,
	}
}

// =============================================================================
// The session triage reads the repository through
// =============================================================================

// TestTriageRunner_ClonesTheRepositoryIntoTheSession proves the charter's
// promise is kept. The charter tells the agent it has "a read-only checkout of
// the project's repository at its default branch", and the FIRST question it
// is asked to answer is whether the change is already on that branch. Nothing
// else makes the checkout: the daemon does not, and the image's entrypoint is
// cleared at spawn, so a runner that did not drive the clone would hand the
// agent an empty directory and every already-done check would be a guess.
func TestTriageRunner_ClonesTheRepositoryIntoTheSession(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(811, "Parse returns an untyped error")
	rig.answers("SDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	clones := rig.container.clonesMade()
	if len(clones) == 0 {
		t.Fatal("triage made no checkout; the agent was asked to read a repository it does not have")
	}
	project := testOrchProject()
	wantDir := filepath.Join(orchestration.WorkspaceCheckoutRoot, project.Name)
	var found bool
	for _, clone := range clones {
		if clone.Dir == wantDir || strings.HasSuffix(clone.Dir, project.Name) {
			found = true
		}
	}
	if !found {
		t.Fatalf("the project was not cloned into the container's checkout root, got %+v", clones)
	}
	// And the clone is driven INSIDE the container, never on the daemon: the
	// fake refuses any directory outside the checkout root it was built with.
	if dirs := rig.container.dirsUsed(); len(dirs) == 0 {
		t.Fatal("no command ran in the container")
	}
}

// TestTriageRunner_RefusesAnUnsandboxedSession proves triage will not read
// forge-controlled text on the daemon. Anybody who can open an issue writes
// the body triage is about to prompt with, so a session the spawn brought up
// as a host process is refused before the first prompt — the same refusal the
// reviewer makes about a candidate change, for the same reason.
func TestTriageRunner_RefusesAnUnsandboxedSession(t *testing.T) {
	rig := newTriageRig(t)
	rig.agents.noContainer = true
	rig.forge.putIssue(812, "an issue whose body is written by anybody")
	rig.answers("SDLC-TRIAGE: ready")

	err := rig.run()
	var session *TriageSessionError
	if !errors.As(err, &session) {
		t.Fatalf("want *TriageSessionError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "not containerized") {
		t.Fatalf("the refusal must say why, got %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("nothing is prompted on an unsandboxed session, got %d prompts", len(prompts))
	}
	// The refusal still gives the session back.
	if len(rig.agents.closed) == 0 {
		t.Fatal("the refused session was leaked")
	}
}

// TestTriageRunner_ClosesItsSessionWhenTheJobIsCancelled proves the teardown
// does not inherit the cancellation that caused it. Every unplanned exit —
// shutdown, supersession, stall abort — arrives at the deferred teardown with
// the context already dead, and a close that used it would fail at once and
// leave the container running with nothing holding its id.
func TestTriageRunner_ClosesItsSessionWhenTheJobIsCancelled(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(813, "an issue")

	ctx, cancel := context.WithCancel(context.Background())
	// The cancellation lands while the agent is answering, which is where a
	// shutdown or a stall abort lands in production.
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) { cancel() }
	rig.answers("SDLC-TRIAGE: ready")

	base, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout", Format: "console"})
	if err != nil {
		t.Fatal(err)
	}
	project := testOrchProject()
	project.Workspace = orchestration.ProjectWorkspace{Path: rig.root}
	job := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:      "job-triage-cancel",
		Runner:     TriageRunnerName,
		Config:     &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject, Attempt: 1},
		Project:    project,
		StartState: orchestration.JobStateInitializing,
		Agents:     rig.agents,
		Logger:     logger.NewAdapter(base),
	})
	if err := rig.runner.Run(ctx, job); err == nil {
		t.Fatal("a cancelled run reports the cancellation")
	}
	if len(rig.agents.closed) == 0 {
		t.Fatal("the session outlived the cancelled job")
	}
}

// TestTriageRunner_PublishesItsSessionOnSnapshots proves the executing state's
// SessionBacked declaration is true of the snapshots as well as the spec. A
// session-backed state whose snapshots name no session gives the stall
// detector nothing to abort, so a long sweep is killed rather than addressed.
func TestTriageRunner_PublishesItsSessionOnSnapshots(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(814, "an issue")

	rig.answers("SDLC-TRIAGE: ready")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	executing := rig.snapshotsIn(orchestration.JobStateExecuting)
	if len(executing) == 0 {
		t.Fatal("the run published no executing status; this test can prove nothing")
	}
	for _, status := range executing {
		if len(status.SessionIDs) != 1 || status.SessionIDs[0] == "" {
			t.Fatalf("an executing snapshot names %v, want the one triage session", status.SessionIDs)
		}
	}
	// And the state really is declared session-backed, so the two agree.
	var backed bool
	for _, spec := range TriageRunnerStates() {
		if spec.State == orchestration.JobStateExecuting && spec.SessionBacked {
			backed = true
		}
	}
	if !backed {
		t.Fatal("executing is not declared session-backed")
	}
}

// TestTriageRunner_KeepsTheNarrowGrantInItsContainer is the negative half of
// go-quicbaas#927. A containerized spawn otherwise takes full control of its
// container — that is what a worker needs to build and test what it wrote — and
// triage must NOT: it reads the repository at its default branch and writes
// nothing, which is what its prompt tells it and what this keeps true. It
// states its own --permission-mode, and ApplyWorkerSpawnEnvironment leaves an
// already-stated grant alone.
func TestTriageRunner_KeepsTheNarrowGrantInItsContainer(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(801, "an issue to triage")
	rig.answers("read it\n\nSDLC-TRIAGE: needs-info")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	rig.agents.mu.Lock()
	spawns := append([]*orchestration.SpawnSessionRequest(nil), rig.agents.spawns...)
	rig.agents.mu.Unlock()
	if len(spawns) != 1 {
		t.Fatalf("triage spawned %d sessions, want 1", len(spawns))
	}
	// The container is the Agent's to decide — its image makes the spawn
	// sandboxed once the reference resolves — so what this runner owns is
	// that the spawn names the agent and states the narrow grant, which
	// ApplyWorkerSpawnEnvironment leaves alone on every candidate.
	if !spawns[0].AgentRef().Names(orchestration.AgentRefTo(testCoderAgent)) || spawns[0].Role != orchestration.AgentRoleTriage {
		t.Fatalf("triage did not spawn as the project's agent: %+v", spawns[0])
	}
	if !slices.Contains(spawns[0].ExtraArgs, orchestration.WorkerPermissionMode) {
		t.Fatalf("triage did not state the narrow grant: %v", spawns[0].ExtraArgs)
	}
	if slices.Contains(spawns[0].ExtraArgs, orchestration.WorkerFullControlFlag) {
		t.Fatalf("triage was handed full control of its container: %v", spawns[0].ExtraArgs)
	}
}

// =============================================================================
// The checkout's credentials — go-quicbaas#929
// =============================================================================

// TestTriageRunner_ClonesAnSSHRemoteItCanAuthenticate is the defect, reproduced
// and fixed.
//
// The burn-in node's project is on an ssh remote. Triage was handed no agent —
// deliberately, because it signs nothing — and an ssh clone is authenticated by
// nothing else, so every sweep spawned a container, ran `git clone`, collected
// "Permission denied (publickey)" and concluded: one wasted job every fifteen
// minutes, and a capability that had never once run.
//
// The container in this rig REQUIRES the credential (fakeContainer.authenticate),
// so this test fails on the code as it was and passes only when the checkout is
// authenticated in the same way the delivery runner's is.
func TestTriageRunner_ClonesAnSSHRemoteItCanAuthenticate(t *testing.T) {
	rig := newTriageRig(t)
	rig.onSSH()
	rig.forge.putIssue(101, "Parse returns an untyped error on malformed input")
	rig.answers("The objective is verifiable and scoped to one change.\n\nSDLC-TRIAGE: ready")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	clones := rig.container.clonesMade()
	if len(clones) != 1 || clones[0].URL != "ssh://git@forge.test:2222/acme/api.git" {
		t.Fatalf("clones = %+v, want the project cloned over ssh", clones)
	}
	// The verdict landed, which is only possible if the checkout the charter
	// promises was actually there to read.
	if !rig.hasLabel(101, rig.binding.Forge.TriggerLabel) {
		t.Fatalf("the verdict was not applied, labels = %v", rig.labels(101))
	}
	// And the credential came from the ONE seam, at the reader's scope.
	scopes := rig.signing.startedScopes()
	if len(scopes) != 1 || scopes[0] != SigningScopeCheckout {
		t.Fatalf("scopes = %v, want exactly one checkout-scoped agent", scopes)
	}
}

// TestTriageRunner_StopsItsCheckoutAgentBeforeItPromptsAnything is the posture
// the "no live signing socket beside untrusted issue text" rule asked for, kept
// while the clone is still able to authenticate.
//
// Triage reads issue text written by anybody who can open an issue. Nothing it
// does after the checkout reaches the forge over git — the verdicts are forge
// API calls the daemon makes — so the agent goes down first, and the socket the
// container holds for the rest of its life answers nothing.
func TestTriageRunner_StopsItsCheckoutAgentBeforeItPromptsAnything(t *testing.T) {
	rig := newTriageRig(t)
	rig.onSSH()
	rig.forge.putIssue(101, "Parse returns an untyped error on malformed input")
	rig.answers("SDLC-TRIAGE: ready")

	stoppedAtFirstPrompt := -1
	rig.agents.mu.Lock()
	rig.agents.onPrompt = func(*orchestration.PromptSessionRequest) {
		if stoppedAtFirstPrompt < 0 {
			stoppedAtFirstPrompt = rig.signing.stopCount()
		}
	}
	rig.agents.mu.Unlock()

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stoppedAtFirstPrompt != 1 {
		t.Fatalf("agents stopped before the first prompt = %d, want 1: the reader held a live agent while it read untrusted text",
			stoppedAtFirstPrompt)
	}
}

// TestTriageRunner_StopsItsCheckoutAgentEvenWhenTheCloneFails: the agent is a
// live socket on the daemon, so it goes down on the failure path too. A refusal
// that leaked one would leave the daemon holding an agent for a container that
// is already gone.
func TestTriageRunner_StopsItsCheckoutAgentEvenWhenTheCloneFails(t *testing.T) {
	rig := newTriageRig(t)
	rig.onSSH()
	rig.forge.putIssue(101, "Parse returns an untyped error")
	rig.container.cloneErr = errors.New("fatal: the forge refused the connection")

	if err := rig.run(); err == nil {
		t.Fatal("a checkout that could not be made was not a failure")
	}
	if rig.signing.stopCount() != 1 {
		t.Fatalf("agents stopped = %d, want the one this session started", rig.signing.stopCount())
	}
	if len(rig.agents.promptTexts()) != 0 {
		t.Error("a turn was spent on a session with no checkout")
	}
}

// TestTriageRunner_ProvesTheCheckoutBeforeItSpendsATurn: triage runs the same
// pre-flight the delivery runner and the reviewer run (go-quicbaas#914).
//
// The clone is idempotent by design — a resumed session is mounted through the
// directory its predecessor cloned into — so an attempt directory left holding
// a DIFFERENT repository is adopted without a word. Triage judges issues against
// what it reads; judging them against another repository is worse than not
// judging them, and a model asked to establish that by hand costs a turn.
func TestTriageRunner_ProvesTheCheckoutBeforeItSpendsATurn(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(101, "Parse returns an untyped error")
	// The mount already holds a repository, and it is somebody else's.
	checkout := filepath.Join(orchestration.WorkspaceCheckoutRoot, testOrchProject().Name)
	rig.container.seedRepo(checkout, "https://forge.test/acme/not-the-project.git")

	err := rig.run()
	if !errors.Is(err, ErrWorkspaceUnfit) {
		t.Fatalf("err = %v, want ErrWorkspaceUnfit", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("%d turn(s) were spent on a workspace nothing had proven", len(prompts))
	}
	if !rig.audited(auditPreflight) {
		t.Error("the pre-flight's probes were not recorded as evidence")
	}
}

// TestTriageRunner_ARefusedCredentialCostsNoContainer is the pre-flight
// go-quicbaas#914 asked for, applied to the credential: a spawn that holds
// nothing the remote would accept is refused on the DAEMON, so the sweep costs
// no container, no image pull and no agent tokens.
//
// It is the test that would have caught go-quicbaas#929. The old triage path
// reached `git clone` to find out it had no credential, which is one wasted job
// per sweep for ever; the fact was decidable from the spec all along.
func TestTriageRunner_ARefusedCredentialCostsNoContainer(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(101, "Parse returns an untyped error")
	// An https remote with no token: the credential helper would have nothing
	// to answer with and git would ask for a password on a terminal no
	// container has.
	rig.tokens.token = nil

	err := rig.run()
	if !errors.Is(err, orchestration.ErrCheckoutCredentialMissing) {
		t.Fatalf("err = %v, want ErrCheckoutCredentialMissing", err)
	}
	if got := rig.agents.sessionCount(); got != 0 {
		t.Fatalf("sessions = %d, want the sweep refused before any container started", got)
	}
	if execs := rig.container.commandsRun(); len(execs) != 0 {
		t.Fatalf("commands run in a container = %v, want none", execs)
	}
}

// triageRunFor builds a bare triageRun over a forge and binding, enough to
// exercise the verdict-application paths directly.
func triageRunFor(forge ForgeBackend, binding *ProjectBinding) *triageRun {
	return &triageRun{
		runner: &TriageRunner{clusters: newMemoryClusters()},
		job: orchestration.NewJobRun(orchestration.JobRunConfig{
			JobID: "job-triage-bare", Runner: TriageRunnerName,
			Config: &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject},
		}),
		forge: forge, binding: binding, repo: binding.Repo(),
	}
}

// TestApplyNotActionable_ClosesOnlyWhenBothConditionsHold is the gate on the
// one irreversible thing triage can do.
//
// BOTH SIDES ARE REQUIRED, and each guards a different mistake. Without the
// binding check, turning the feature on anywhere would let an agent close
// issues in a project that never asked. Without the verdict check, a project
// that opted in would have every not-actionable closed — including the ones
// that are out of scope or waiting on a person, which are exactly the ones a
// person still needs to see open.
func TestApplyNotActionable_ClosesOnlyWhenBothConditionsHold(t *testing.T) {
	cases := []struct {
		name      string
		mayClose  bool
		declared  bool
		wantClose bool
	}{
		{"binding allows, verdict asks", true, true, true},
		{"binding allows, verdict silent", true, false, false},
		{"binding forbids, verdict asks", false, true, false},
		{"neither", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			forge := newFakeForge()
			binding := testBinding()
			binding.TriageMayClose = c.mayClose
			issue := ForgeIssue{Number: 42, Title: "stale report"}
			forge.issues[42] = &issue

			run := triageRunFor(forge, binding)
			verdict := TriageVerdict{
				Outcome: TriageOutcomeNotActionable, Comments: true,
				Headline:            "No worker will be spawned for this issue.",
				ClosesAsAlreadyDone: c.declared,
			}
			if err := run.applyNotActionable(context.Background(), issue, verdict, "the work is on main at foo.go:12"); err != nil {
				t.Fatalf("applyNotActionable: %v", err)
			}

			closed := len(forge.closed) == 1 && forge.closed[0] == 42
			if closed != c.wantClose {
				t.Fatalf("closed=%v, want %v (may_close=%v declared=%v)", closed, c.wantClose, c.mayClose, c.declared)
			}
			// THE EVIDENCE IS POSTED EITHER WAY. A closed issue whose comment
			// never landed tells a person nothing about why.
			if len(forge.comments[42]) != 1 {
				t.Fatalf("the evidence comment must be posted in every case, got %d", len(forge.comments[42]))
			}
		})
	}
}

// TestApplyNotActionable_AFailedCloseIsReported keeps a refused close from
// passing as a completed triage: the comment says the issue is being closed,
// so a close that did not happen must surface.
func TestApplyNotActionable_AFailedCloseIsReported(t *testing.T) {
	forge := newFakeForge()
	forge.closeIssueErr = errors.New("forge refused")
	binding := testBinding()
	binding.TriageMayClose = true
	issue := ForgeIssue{Number: 7}
	forge.issues[7] = &issue

	run := triageRunFor(forge, binding)
	err := run.applyNotActionable(context.Background(), issue,
		TriageVerdict{
			Outcome: TriageOutcomeNotActionable, Comments: true,
			Headline: "No worker will be spawned for this issue.", ClosesAsAlreadyDone: true,
		}, "evidence")
	var verdictErr *TriageVerdictError
	if !errors.As(err, &verdictErr) {
		t.Fatalf("want TriageVerdictError, got %T: %v", err, err)
	}
	if verdictErr.Issue != 7 {
		t.Fatalf("the refusal must name the issue, got %d", verdictErr.Issue)
	}
}

// =============================================================================
// Which agent triage runs on
// =============================================================================

// triageRunWith is a run whose runner triages with policy.
func triageRunWith(policy TriageAgentPolicy, binding *ProjectBinding) *triageRun {
	return &triageRun{
		runner:  &TriageRunner{triageAgents: policy},
		job:     &orchestration.JobRun{TenantID: testTenant},
		binding: binding,
	}
}

// TestTriageAgent_TheNodeDefaultOutranksTheWorker is the regression that cost
// real money: with no node default, every project fell straight through to its
// worker, so a fleet writing code with a frontier model also re-read its whole
// backlog with that model on every sweep.
func TestTriageAgent_TheNodeDefaultOutranksTheWorker(t *testing.T) {
	run := triageRunWith(
		TriageAgentPolicy{Default: orchestration.AgentRefTo(testTriageAgent), Agents: testAgentCatalog()},
		&ProjectBinding{Worker: orchestration.AgentRefTo(testCoderAgent)},
	)
	agent, err := run.triageAgent(context.Background())
	if err != nil || agent.AgentID != testTriageAgent {
		t.Fatalf("triage took the worker over the node default: %+v, %v", agent, err)
	}
}

// TestTriageAgent_TheBindingOutranksTheNodeDefault keeps the node default a
// DEFAULT: a project that names its own triage agent still gets it.
func TestTriageAgent_TheBindingOutranksTheNodeDefault(t *testing.T) {
	run := triageRunWith(
		TriageAgentPolicy{Default: orchestration.AgentRefTo(testTriageAgent), Agents: testAgentCatalog()},
		&ProjectBinding{Triage: orchestration.AgentRefTo(testReviewerAgent), Worker: orchestration.AgentRefTo(testCoderAgent)},
	)
	if agent, err := run.triageAgent(context.Background()); err != nil || agent.AgentID != testReviewerAgent {
		t.Fatalf("the binding's own triage agent was ignored: %+v, %v", agent, err)
	}
}

// TestTriageAgent_NoDefaultStillFallsBackToTheWorker pins the behaviour an
// install with no locally served model keeps: empty config changes nothing.
func TestTriageAgent_NoDefaultStillFallsBackToTheWorker(t *testing.T) {
	run := triageRunWith(TriageAgentPolicy{}, &ProjectBinding{Worker: orchestration.AgentRefTo(testCoderAgent)})
	if agent, err := run.triageAgent(context.Background()); err != nil || agent.AgentID != testCoderAgent {
		t.Fatalf("an unset node default broke the worker fallback: %+v, %v", agent, err)
	}
}

// TestTriageAgent_ADefaultTheTenantDoesNotDefineIsPassedOver: the default is
// the node's and names one tenant's Agent; a tenant without it triages with
// its worker rather than failing every sweep.
func TestTriageAgent_ADefaultTheTenantDoesNotDefineIsPassedOver(t *testing.T) {
	run := triageRunWith(
		TriageAgentPolicy{Default: orchestration.AgentRefTo("elsewhere"), Agents: testAgentCatalog()},
		&ProjectBinding{Worker: orchestration.AgentRefTo(testCoderAgent)},
	)
	if agent, err := run.triageAgent(context.Background()); err != nil || agent.AgentID != testCoderAgent {
		t.Fatalf("an undefined default must fall through to the worker: %+v, %v", agent, err)
	}
	// A default that exists but is not a triage agent is passed over too.
	run = triageRunWith(
		TriageAgentPolicy{Default: orchestration.AgentRefTo(testQwenAgent), Agents: testAgentCatalog()},
		&ProjectBinding{Worker: orchestration.AgentRefTo(testCoderAgent)},
	)
	if agent, _ := run.triageAgent(context.Background()); agent.AgentID != testCoderAgent {
		t.Fatalf("a non-triage default must not be taken: %+v", agent)
	}
}

// TestTriageAgent_AnUnreadableCatalogIsAnError: the default cannot be judged,
// and guessing would spend a frontier model on a sweep that should be local.
func TestTriageAgent_AnUnreadableCatalogIsAnError(t *testing.T) {
	boom := errors.New("catalog unreadable")
	run := triageRunWith(
		TriageAgentPolicy{Default: orchestration.AgentRefTo(testTriageAgent), Agents: fixedAgents{err: boom}},
		&ProjectBinding{Worker: orchestration.AgentRefTo(testCoderAgent)},
	)
	if _, err := run.triageAgent(context.Background()); !errors.Is(err, boom) {
		t.Fatalf("an unreadable catalog = %v", err)
	}
}

// TestApplyNotActionable_AnOpenIssueNeverLeavesWithoutAState is the regression
// that idled the fleet.
//
// A not-actionable verdict used to comment and stop, leaving the issue open
// carrying NO factory label. Because the newest comment was then triage's own,
// spokenToSinceTriage ruled it not a candidate on every later pass: invisible
// to the factory and unmarked for a person. Measured on go-luks2 2026-09-16 —
// five issues in that state and 34 hours of a fleet triaging nothing.
func TestApplyNotActionable_AnOpenIssueNeverLeavesWithoutAState(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	binding.TriageMayClose = true
	issue := ForgeIssue{Number: 42, Title: "needs a ruling before a worker touches it"}
	forge.issues[42] = &issue

	run := triageRunFor(forge, binding)
	// Not closable: the verdict did not say the work is already done.
	verdict := TriageVerdict{
		Outcome:  TriageOutcomeNotActionable,
		Comments: true,
		Headline: "No worker will be spawned for this issue.",
	}
	if err := run.applyNotActionable(context.Background(), issue, verdict, "ext2 support is an owner call"); err != nil {
		t.Fatalf("applyNotActionable: %v", err)
	}
	if len(forge.closed) != 0 {
		t.Fatalf("closed %v, want the issue left open", forge.closed)
	}
	want := binding.Forge.Label(LabelEscalated)
	if want == "" {
		t.Fatal("the test binding displays no escalated label; the fixture no longer exercises this")
	}
	if len(forge.labelAdds) != 1 || forge.labelAdds[0] != want {
		t.Fatalf("labels added = %v, want exactly [%s] so a person can find it", forge.labelAdds, want)
	}
}

// TestApplyNotActionable_AClosedIssueIsNotAlsoEscalated keeps the two
// terminal states from both being applied: a closed issue carrying "a person
// must look" is a contradiction that puts it back in somebody's queue.
func TestApplyNotActionable_AClosedIssueIsNotAlsoEscalated(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	binding.TriageMayClose = true
	issue := ForgeIssue{Number: 7, Title: "already fixed on main"}
	forge.issues[7] = &issue

	run := triageRunFor(forge, binding)
	verdict := TriageVerdict{
		Outcome: TriageOutcomeNotActionable, Comments: true,
		Headline:            "No worker will be spawned for this issue.",
		ClosesAsAlreadyDone: true,
	}
	if err := run.applyNotActionable(context.Background(), issue, verdict, "fixed at foo.go:12"); err != nil {
		t.Fatalf("applyNotActionable: %v", err)
	}
	if len(forge.closed) != 1 || forge.closed[0] != 7 {
		t.Fatalf("closed = %v, want [7]", forge.closed)
	}
	if len(forge.labelAdds) != 0 {
		t.Fatalf("labels added = %v, want none on a closed issue", forge.labelAdds)
	}
}

// TestClearOtherDispositions_AFailedRemovalIsAuditedNotFatal proves a stale
// disposition label the forge refuses to remove is recorded on the audit trail
// rather than silently dropped.
func TestClearOtherDispositions_AFailedRemovalIsAuditedNotFatal(t *testing.T) {
	forge := newFakeForge()
	forge.removeLabelErr = errors.New("forge refused the removal")
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	escalated := binding.Forge.Label(LabelEscalated)
	issue := ForgeIssue{Number: 9, Title: "judged twice", Labels: []string{escalated}}
	forge.issues[9] = &issue

	var audits []orchestration.JobAuditEvent
	run := triageRunFor(forge, binding)
	run.job = orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID: "job-triage-audit", Runner: TriageRunnerName,
		Config: &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject},
		AuditFn: func(eventType, details string) {
			audits = append(audits, orchestration.JobAuditEvent{Type: eventType, Details: details})
		},
	})

	run.clearOtherDispositions(context.Background(), issue, LabelNeedsInfo)

	if len(audits) != 1 || audits[0].Type != "sdlc:triage_label_stale" {
		t.Fatalf("audits = %v, want one sdlc:triage_label_stale event", audits)
	}
	if !strings.Contains(audits[0].Details, "label="+escalated) ||
		!strings.Contains(audits[0].Details, "forge refused the removal") {
		t.Fatalf("the audit must name the label and the cause, got %q", audits[0].Details)
	}
}

// TestClearOtherDispositions_RemovesOnlyTheOtherHeldDisposition covers the
// success path: the kept disposition stays and nothing is audited.
func TestClearOtherDispositions_RemovesOnlyTheOtherHeldDisposition(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	escalated := binding.Forge.Label(LabelEscalated)
	needsInfo := binding.Forge.Label(LabelNeedsInfo)
	issue := ForgeIssue{Number: 10, Title: "judged twice", Labels: []string{escalated, needsInfo}}
	forge.issues[10] = &issue

	var audits []orchestration.JobAuditEvent
	run := triageRunFor(forge, binding)
	run.job = orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID: "job-triage-audit", Runner: TriageRunnerName,
		Config: &orchestration.JobConfig{TenantID: testTenant, ProjectID: testProject},
		AuditFn: func(eventType, details string) {
			audits = append(audits, orchestration.JobAuditEvent{Type: eventType, Details: details})
		},
	})

	run.clearOtherDispositions(context.Background(), issue, LabelNeedsInfo)

	if len(forge.labelRemovals) != 1 || forge.labelRemovals[0] != escalated {
		t.Fatalf("removals = %v, want [%s]", forge.labelRemovals, escalated)
	}
	if len(audits) != 0 {
		t.Fatalf("a clean removal audits nothing, got %v", audits)
	}
}

// TestIsCandidate_EscalatedIsNotACandidateEvenWhenAnswered pins #1060:
// escalated names a person as the next step, and a comment is not that person
// removing the label, so the issue is never re-judged while it carries it.
// (It was once exempt, so an answer re-judged it; that let a verdict promote
// an issue a person had been asked to rule on.)
func TestIsCandidate_EscalatedIsNotACandidateEvenWhenAnswered(t *testing.T) {
	forge := newFakeForge()
	binding := testBinding()
	binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	escalated := binding.Forge.Label(LabelEscalated)
	issue := &ForgeIssue{Number: 9, Title: "needs a ruling", Labels: []string{escalated}}
	forge.issues[9] = issue
	forge.comments[9] = []ForgeComment{
		{Created: time.Now().Add(-time.Minute), Body: "the verdict\n<!-- " + triageCommentMarker + " -->"},
		{Created: time.Now(), Body: "ruling: ext2 is out of scope"},
	}

	run := triageRunFor(forge, binding)
	run.discussions = map[int][]ForgeComment{}
	got, err := run.isCandidate(context.Background(), issue, binding.Forge.OwnedLabels())
	if err != nil {
		t.Fatalf("isCandidate: %v", err)
	}
	if got {
		t.Fatal("an escalated issue is a candidate; a verdict could clear a person's hold")
	}
}

// TestTriageVerdictHead reads back the commit a verdict recorded.
func TestTriageVerdictHead(t *testing.T) {
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"a stamped verdict": {
			"text\n<!-- " + triageCommentMarker + " " + triageHeadMarker + "abc123 -->\n", "abc123",
		},
		"no trailing space before the close": {
			"<!-- " + triageCommentMarker + " " + triageHeadMarker + "deadbeef-->", "deadbeef",
		},
		// Every verdict written before the head was recorded.
		"an unstamped verdict":  {"text\n<!-- " + triageCommentMarker + " -->\n", ""},
		"somebody else's words": {"I disagree with this", ""},
		"empty":                 {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := triageVerdictHead(tc.body); got != tc.want {
				t.Fatalf("triageVerdictHead = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHeadMovedSince_ReopensAParkedBacklog pins the rule that stopped the
// fleet converging on having nothing to do.
//
// A verdict of "blocked on another change" or "the prerequisite has not
// landed" is an answer about a TREE, and a merge is exactly the event that
// makes it wrong. Until this existed the only thing that could reopen a
// parked issue was a person commenting on it, so thirteen go-luks2 issues
// stayed parked while the factory merged a change to main and triage spun
// every fifteen minutes finding nothing.
func TestHeadMovedSince_ReopensAParkedBacklog(t *testing.T) {
	stamped := func(sha string) ForgeComment {
		return ForgeComment{Body: "verdict\n<!-- " + triageCommentMarker + " " + triageHeadMarker + sha + " -->"}
	}
	for name, tc := range map[string]struct {
		head    string
		verdict ForgeComment
		want    bool
	}{
		// A needs-info verdict names something only a person can supply, so a
		// merge says nothing about it and must not re-ask the question.
		"a needs-info question is not reopened by a merge": {"bbbb", ForgeComment{
			Body: "v\n<!-- " + triageCommentMarker + " " + triageHeadMarker + "aaaa " +
				triageVerdictMarker + string(TriageOutcomeNeedsInfo) + " -->"}, false},
		"a not-actionable verdict IS reopened by a merge": {"bbbb", ForgeComment{
			Body: "v\n<!-- " + triageCommentMarker + " " + triageHeadMarker + "aaaa " +
				triageVerdictMarker + string(TriageOutcomeNotActionable) + " -->"}, true},
		"the tree has not moved":         {"aaaa", stamped("aaaa"), false},
		"case does not decide it":        {"AAAA", stamped("aaaa"), false},
		"the tree moved":                 {"bbbb", stamped("aaaa"), true},
		"an unstamped verdict":           {"aaaa", ForgeComment{Body: "v\n<!-- " + triageCommentMarker + " -->"}, true},
		"a head this run could not read": {"", stamped("aaaa"), true},
	} {
		t.Run(name, func(t *testing.T) {
			run := &triageRun{headSHA: tc.head}
			if got := run.headMovedSince(tc.verdict); got != tc.want {
				t.Fatalf("headMovedSince = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTriageRunner_AMergeReopensAParkedIssue is the fleet-level defect this
// rule exists for.
//
// A parked issue is an answer about a TREE — "blocked on another change",
// "the prerequisite has not landed" — and a merge is exactly the event that
// makes it wrong. Before this, the only thing that could reopen one was a
// person commenting, so thirteen go-luks2 issues stayed parked while the
// factory merged to main, every project ran out of work, and triage spun
// every fifteen minutes finding nothing to do.
func TestTriageRunner_AMergeReopensAParkedIssue(t *testing.T) {
	rig := newTriageRig(t)
	// No escalated label: the default fixture displays none, so the verdict
	// leaves the issue unlabelled. An escalated issue is a hold (#1060) and a
	// merge does not reopen it; only a person removing the label does.
	issue := rig.forge.putIssue(703, "blocked on the dependency")

	asked := time.Now().Add(-time.Hour)
	rig.forge.putComment(703,
		"Blocked: the prerequisite has not landed.\n<!-- "+triageCommentMarker+" "+triageHeadMarker+fakeForgeHeadSHA+" -->",
		asked)
	issue.Updated = asked

	// While main is where the verdict left it, the verdict stands.
	rig.answers("SDLC-TRIAGE: ready")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) != 0 {
		t.Fatalf("an unchanged tree re-judges nothing, got %d prompts", len(prompts))
	}

	// The prerequisite merges: main moves, and the verdict is about a tree
	// that is no longer there.
	rig.forge.mu.Lock()
	rig.forge.branchSHA = "1111222233334444555566667777888899990000"
	rig.forge.mu.Unlock()

	rig.answers("SDLC-TRIAGE: ready")
	if err := rig.run(); err != nil {
		t.Fatalf("run after the merge: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) == 0 {
		t.Fatal("a merge must reopen a parked issue; the backlog stayed parked and the fleet ran dry")
	}
}

// TestTriageRunner_AVerdictReplacesThePreviousDisposition proves an issue
// carries one answer, not a pile of them.
//
// Each disposition only ever ADDED its label, so an issue judged escalated
// after being asked for information ended up carrying both — two
// contradictory answers, and an operator scanning the queue cannot tell which
// is current. Six of eleven open go-luks2 issues were in that state after one
// re-triage pass.
func TestTriageRunner_AVerdictReplacesThePreviousDisposition(t *testing.T) {
	rig := newTriageRig(t)
	// The default fixture declares no escalated label; this test is about two
	// dispositions colliding, so it needs both declared — and the runner
	// reloads the binding from the STORE, so the store is what must change.
	rig.binding.Forge.Labels[LabelEscalated] = "factory:escalated"
	if err := rig.store.PutBinding(context.Background(), testTenant, rig.binding); err != nil {
		t.Fatalf("put binding: %v", err)
	}
	escalated := rig.binding.Forge.Label(LabelEscalated)
	needsInfo := rig.binding.Forge.Label(LabelNeedsInfo)
	if escalated == "" || needsInfo == "" {
		t.Fatal("both disposition labels must be declared for this test to mean anything")
	}
	// Starts from needs-info: an escalated issue is a hold (#1060) and is not
	// re-judged, so the collision is reachable only in this direction.
	issue := rig.forge.putIssue(704, "asked for information", needsInfo)
	issue.Updated = time.Now()

	rig.answers("a person must rule on scope first\n\nSDLC-TRIAGE: not-actionable")
	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	labels := rig.forge.issues[704].Labels
	got := map[string]bool{}
	for _, l := range labels {
		got[l] = true
	}
	if !got[escalated] {
		t.Fatalf("the new verdict's label was not applied: %v", labels)
	}
	if got[needsInfo] {
		t.Fatalf("the superseded disposition still stands: %v", labels)
	}
}

// TestTriageRunner_AnIdenticalVerdictIsNotPostedAgain is go-quicbaas#1013's
// thread: every merge reopened the parked issue, triage reached the same
// "duplicate of #981" each time, and posted it again — sixteen identical
// comments. A merge still reopens the issue; a re-judgement that reaches the
// verdict triage already gave says nothing new and posts nothing. A different
// verdict — another duplicate target — is news, and is posted.
func TestTriageRunner_AnIdenticalVerdictIsNotPostedAgain(t *testing.T) {
	rig := newTriageRig(t)
	trigger := rig.binding.Forge.TriggerLabel
	rig.forge.putIssue(981, "agent-side workspaces in pkg/sdlc/agent.go", trigger)
	rig.forge.putIssue(982, "agent-side sockets in pkg/sdlc/agent.go", trigger)
	rig.forge.putIssue(1013, "relay the signing agent from pkg/sdlc/agent.go")
	duplicateOf := func(number string) string {
		return "The same work as " + number + ".\n\nSDLC-TRIAGE-DUPLICATE: " + number + "\nSDLC-TRIAGE: duplicate"
	}
	merge := func(sha string) {
		rig.forge.mu.Lock()
		rig.forge.branchSHA = sha
		rig.forge.mu.Unlock()
	}

	rig.answers(duplicateOf("#981"))
	if err := rig.run(); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if got := rig.comments(1013); len(got) != 1 || !strings.Contains(got[0], "#981") {
		t.Fatalf("first pass posted %v, want the one duplicate verdict", got)
	}

	merge("1111222233334444555566667777888899990000")
	rig.answers(duplicateOf("#981"))
	if err := rig.run(); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if prompts := rig.agents.promptTexts(); len(prompts) == 0 {
		t.Fatal("the merge did not reopen the issue at all")
	}
	if got := rig.comments(1013); len(got) != 1 {
		t.Fatalf("an identical verdict was posted again: %d comments", len(got))
	}

	merge("aaaabbbbccccddddeeeeffff0000111122223333")
	rig.answers(duplicateOf("#982"))
	if err := rig.run(); err != nil {
		t.Fatalf("third pass: %v", err)
	}
	if got := rig.comments(1013); len(got) != 2 || !strings.Contains(got[1], "#982") {
		t.Fatalf("a changed verdict was not posted: %v", got)
	}
}

// TestSameTriageVerdict_ComparesOutcomeAndHeadline covers the comparison on its
// own: the agent's prose around a verdict differs every time and does not make
// it a new one; the outcome or the headline does.
func TestSameTriageVerdict_ComparesOutcomeAndHeadline(t *testing.T) {
	verdict := TriageVerdict{Outcome: TriageOutcomeDuplicate, Comments: true, DuplicateOf: 981,
		Headline: "Triage found this already covered by another open issue."}
	posted := TriageComment(verdict, "some evidence") +
		"\n<!-- " + triageCommentMarker + " " + triageHeadMarker + "abc " + triageVerdictMarker + string(TriageOutcomeDuplicate) + " -->\n"

	if !sameTriageVerdict(posted, verdict, "other evidence, reworded") {
		t.Fatal("the same duplicate with different evidence was treated as new")
	}
	other := verdict
	other.DuplicateOf = 982
	if sameTriageVerdict(posted, other, "some evidence") {
		t.Fatal("a different duplicate target was treated as the same verdict")
	}
	ready := TriageVerdict{Outcome: TriageOutcomeReady, Comments: true, Headline: verdict.Headline}
	if sameTriageVerdict(posted, ready, "some evidence") {
		t.Fatal("a different outcome was treated as the same verdict")
	}
	if sameTriageVerdict("an unmarked human comment", verdict, "some evidence") {
		t.Fatal("a comment with no recorded verdict matched")
	}

	// Any other outcome is its body: a different question is a new verdict,
	// the same question re-wrapped is not.
	question := TriageVerdict{Outcome: TriageOutcomeNeedsInfo, Comments: true, Headline: "Triage needs more information."}
	asked := TriageComment(question, "Which of the three Encode overloads?") +
		"\n<!-- " + triageCommentMarker + " " + triageHeadMarker + "abc " + triageVerdictMarker + string(TriageOutcomeNeedsInfo) + " -->\n"
	if !sameTriageVerdict(asked, question, "Which of the three\n  Encode overloads?") {
		t.Fatal("the same question re-wrapped was treated as new")
	}
	if sameTriageVerdict(asked, question, "What should the threshold be?") {
		t.Fatal("a different needs-info question was treated as the same verdict")
	}

	older := ForgeComment{Body: "old <!-- " + triageCommentMarker + " -->", Created: time.Unix(100, 0)}
	newer := ForgeComment{Body: "new <!-- " + triageCommentMarker + " -->", Created: time.Unix(200, 0)}
	human := ForgeComment{Body: "a person", Created: time.Unix(300, 0)}
	if last, ok := lastTriageComment([]ForgeComment{newer, human, older}); !ok || last.Body != newer.Body {
		t.Fatalf("lastTriageComment = %+v, %v; want the newest triage comment", last, ok)
	}
	if _, ok := lastTriageComment([]ForgeComment{human}); ok {
		t.Fatal("a thread triage never spoke on has a last triage comment")
	}
}

// TestTriageRunner_ASuppressedVerdictRecordsTheHeadItJudged: a merge reopens the
// issue and triage reaches the verdict it already posted, so nothing is posted
// — and the head it judged is remembered, so the next sweep at that same head
// spends no model turn re-reaching it.
func TestTriageRunner_ASuppressedVerdictRecordsTheHeadItJudged(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(981, "agent-side workspaces in pkg/sdlc/agent.go", rig.binding.Forge.TriggerLabel)
	rig.forge.putIssue(1013, "relay the signing agent from pkg/sdlc/agent.go")
	duplicate := "The same work as #981.\n\nSDLC-TRIAGE-DUPLICATE: #981\nSDLC-TRIAGE: duplicate"

	rig.answers(duplicate)
	if err := rig.run(); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	rig.forge.mu.Lock()
	rig.forge.branchSHA = "1111222233334444555566667777888899990000"
	rig.forge.mu.Unlock()
	rig.answers(duplicate)
	if err := rig.run(); err != nil {
		t.Fatalf("pass after the merge: %v", err)
	}
	if got := rig.comments(1013); len(got) != 1 {
		t.Fatalf("the unchanged verdict was posted again: %d comments", len(got))
	}
	if !rig.audited("sdlc:triage_unchanged") {
		t.Fatal("the suppression was not audited")
	}

	turns := len(rig.agents.promptTexts())
	rig.answers(duplicate)
	if err := rig.run(); err != nil {
		t.Fatalf("pass at the same head: %v", err)
	}
	if got := len(rig.agents.promptTexts()); got != turns {
		t.Fatalf("a sweep at the head already judged spent %d more turn(s)", got-turns)
	}

	// A person speaking is still new information at that head.
	rig.forge.putComment(1013, "It is not a duplicate: #981 never touched the socket.", time.Now().Add(time.Hour))
	rig.answers(duplicate)
	if err := rig.run(); err != nil {
		t.Fatalf("pass after a comment: %v", err)
	}
	if got := len(rig.agents.promptTexts()); got == turns {
		t.Fatal("a human comment did not reopen the issue")
	}
}

// TestTriageRunner_TwoDifferentQuestionsAreBothPosted: needs-info asks one
// question, a person answers, and triage asks a different one. The second
// question is a new verdict — its headline is the same, its question is not —
// and it is posted.
func TestTriageRunner_TwoDifferentQuestionsAreBothPosted(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(40, "Encode is slow")

	rig.answers("The issue does not say which of the three Encode overloads is affected.\n\n" +
		"SDLC-TRIAGE-QUESTION: Which of the three Encode overloads is affected?\nSDLC-TRIAGE: needs-info")
	if err := rig.run(); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	rig.forge.putComment(40, "The streaming one.", time.Now().Add(time.Hour))
	rig.answers("The issue names no latency target.\n\n" +
		"SDLC-TRIAGE-QUESTION: What latency should the streaming Encode meet?\nSDLC-TRIAGE: needs-info")
	if err := rig.run(); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	var questions []string
	for _, comment := range rig.comments(40) {
		if strings.Contains(comment, triageCommentMarker) {
			questions = append(questions, comment)
		}
	}
	if len(questions) != 2 || !strings.Contains(questions[0], "overloads") || !strings.Contains(questions[1], "latency") {
		t.Fatalf("triage's questions = %q, want both, in order", questions)
	}
}

// triageSummaryReply is the shape go-quicbaas#826 and #851 were parked with:
// a review that restates the issue under "What is missing:" and asks its
// author nothing.
const triageSummaryReply = "## Review of Issue #826\n\n### Issue Summary\n\n" +
	"The issue asks for the scheduler to coalesce leases in pkg/scheduler/lease.go.\n\n" +
	"### What is missing:\n\nThe issue describes lease coalescing for the scheduler.\n\n"

func TestTriageRunner_ANeedsInfoThatAsksNothingIsNotAVerdict(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(826, "Coalesce scheduler leases")
	rig.answers(triageSummaryReply + "SDLC-TRIAGE: needs-info")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := rig.comments(826); len(got) != 0 {
		t.Fatalf("a needs-info that asks nothing is not posted, got %v", got)
	}
	if got := rig.labels(826); len(got) != 0 {
		t.Fatalf("a needs-info that asks nothing parks nothing, got %v", got)
	}
	details, ok := rig.auditDetails("sdlc:triage_unusable")
	if !ok || !strings.Contains(details, "issue=826") || !strings.Contains(details, "asks no question") {
		t.Fatalf("the audit trail names the issue and the reason, got %q", details)
	}
}

func TestTriageRunner_ANeedsInfoListsTheQuestionsItAsks(t *testing.T) {
	rig := newTriageRig(t)
	rig.forge.putIssue(851, "Coalesce scheduler leases")
	rig.answers(triageSummaryReply +
		"SDLC-TRIAGE-QUESTION: Should a coalesced lease keep the earliest or the latest deadline?\n" +
		"SDLC-TRIAGE: needs-info")

	if err := rig.run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rig.hasLabel(851, rig.binding.Forge.Label(LabelNeedsInfo)) {
		t.Fatalf("a needs-info that asks a question is parked, got %v", rig.labels(851))
	}
	comments := rig.comments(851)
	if len(comments) != 1 {
		t.Fatalf("needs-info comments exactly once, got %d", len(comments))
	}
	if !strings.Contains(comments[0], "1. Should a coalesced lease keep the earliest or the latest deadline?") {
		t.Fatalf("the comment must list the question, got:\n%s", comments[0])
	}
	if strings.Contains(comments[0], TriageQuestionMarker) {
		t.Fatalf("the marker itself means nothing to a reader, got:\n%s", comments[0])
	}
}

func TestTriageRunner_ADuplicateMustShareAPathOrIdentifier(t *testing.T) {
	const dice = "Add `dice.Attestation` to sdk/go/xkms/dice/dice.go."
	tests := []struct {
		name   string
		cited  string
		usable bool
	}{
		// go-quicbaas#1082 cited #894: a DICE SDK re-export against
		// scheduler lease coalescing, which share nothing.
		{name: "two issues sharing nothing", cited: "Coalesce leases in pkg/scheduler/lease.go via `LeaseCoalescer`."},
		{name: "a shared path", cited: "Export the attestation from sdk/go/xkms/dice/dice.go.", usable: true},
		{name: "a shared identifier", cited: "`dice.Attestation` is missing from the SDK.", usable: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rig := newTriageRig(t)
			rig.forge.putIssue(894, "the cited issue", rig.binding.Forge.TriggerLabel).Body = tc.cited
			rig.forge.putIssue(1082, "Re-export DICE attestation").Body = dice
			rig.answers("The same work as #894.\n\nSDLC-TRIAGE-DUPLICATE: #894\nSDLC-TRIAGE: duplicate")

			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			posted := len(rig.comments(1082)) == 1
			if posted != tc.usable {
				t.Fatalf("duplicate posted = %v, want %v: %v", posted, tc.usable, rig.comments(1082))
			}
			if tc.usable {
				return
			}
			details, ok := rig.auditDetails("sdlc:triage_unusable")
			if !ok || !strings.Contains(details, "no path or identifier in common") {
				t.Fatalf("the audit trail names why the duplicate was set aside, got %q", details)
			}
		})
	}
}

func TestTriageRunner_TheVerdictMarkerNamesModelAndPrompt(t *testing.T) {
	tests := map[string]struct {
		model, want string
	}{
		"a reported model":     {model: "glm 4.6", want: triageModelMarker + "glm_4.6"},
		"no model is reported": {want: triageModelMarker + triageUnknownModel},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rig := newTriageRig(t)
			rig.agents.model = tc.model
			rig.forge.putIssue(102, "Fix the encoder")
			rig.answers("The issue names no overload.\n\n" +
				"SDLC-TRIAGE-QUESTION: Which overload?\nSDLC-TRIAGE: needs-info")

			if err := rig.run(); err != nil {
				t.Fatalf("run: %v", err)
			}
			comments := rig.comments(102)
			if len(comments) != 1 {
				t.Fatalf("needs-info comments exactly once, got %d", len(comments))
			}
			posted := comments[0]
			if !strings.Contains(posted, " "+tc.want+" ") {
				t.Fatalf("the marker must carry %q, got:\n%s", tc.want, posted)
			}
			if !strings.Contains(posted, " "+triagePromptMarker+TriagePromptRevision+" -->") {
				t.Fatalf("the marker must carry the prompt revision, got:\n%s", posted)
			}
			if got := triageVerdictCode(posted); got != string(TriageOutcomeNeedsInfo) {
				t.Fatalf("triageVerdictCode = %q on the new marker", got)
			}
			if got := triageVerdictHead(posted); got != fakeForgeHeadSHA {
				t.Fatalf("triageVerdictHead = %q on the new marker, want %q", got, fakeForgeHeadSHA)
			}
		})
	}
}

// TestTriageRun_DecorateNamesTheTriageSessionAsTheLead: the triage session
// is the job's only session, so it is the job's lead. The job page badges
// and links the lead; a triage job that never named one rendered no badge
// (go-quicbaas#863).
func TestTriageRun_DecorateNamesTheTriageSessionAsTheLead(t *testing.T) {
	run := &triageRun{}
	run.decorate(nil)
	status := &orchestration.JobStatus{}
	run.decorate(status)
	if len(status.SessionIDs) != 0 || status.LeadSessionID != "" {
		t.Fatalf("a run with no session decorated one: %v lead=%q", status.SessionIDs, status.LeadSessionID)
	}
	run.sessionID = "triage-1"
	run.decorate(status)
	if len(status.SessionIDs) != 1 || status.SessionIDs[0] != "triage-1" {
		t.Fatalf("session ids = %v", status.SessionIDs)
	}
	if status.LeadSessionID != "triage-1" {
		t.Fatalf("lead session = %q, want the triage session", status.LeadSessionID)
	}
}
