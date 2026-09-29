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
	"strconv"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// parkedAt is the instant every fixture park happened.
var parkedAt = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

// parkedForRuling seeds a task parked on an owner question and returns it.
func (f *sweepFixture) parkedForRuling(kind orchestration.TaskBlockKind) *orchestration.Task {
	task := f.blocked("## What I need from you\n\nShould the codec keep the v1 field order?")
	f.tasks.mu.Lock()
	task.BlockKind = kind
	task.StatusChangedAt = parkedAt.UnixNano()
	f.tasks.mu.Unlock()
	return task
}

// comment appends a comment to an issue as the forge would store it.
func (f *fakeForge) comment(number int, id int64, author, body string, created time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.comments[number] = append(f.comments[number], ForgeComment{ID: id, Author: author, Body: body, Created: created})
}

// issueNumber is the forge issue a fixture task was adopted from.
func issueNumber(t *testing.T, task *orchestration.Task) int {
	t.Helper()
	_, number, err := issueOfTask(task)
	if err != nil {
		t.Fatalf("issueOfTask: %v", err)
	}
	return number
}

// storedTask reads a task back from the fake store.
func (f *sweepFixture) storedTask(id string) *orchestration.Task {
	f.tasks.mu.Lock()
	defer f.tasks.mu.Unlock()
	for _, task := range f.tasks.tasks {
		if task.ID == id {
			copied := *task
			return &copied
		}
	}
	return nil
}

func TestSweep_AnAuthorizedRulerReadmitsAParkedTask(t *testing.T) {
	cases := map[string]struct {
		kind   orchestration.TaskBlockKind
		author string
	}{
		"the repository owner answers a question":      {orchestration.TaskBlockKindNeedsInfo, fixtureOwner},
		"the company agent rules on an escalation":     {orchestration.TaskBlockKindEscalated, "agent-company"},
		"a login is matched regardless of letter case": {orchestration.TaskBlockKindNeedsInfo, "Agent-Company"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSweepFixture(t)
			task := f.parkedForRuling(tc.kind)
			f.forge.comment(issueNumber(t, task), 501, tc.author, "Keep the v1 order.", parkedAt.Add(time.Minute))

			finding := onlyFinding(t, f.sweep(t))

			if finding.Verdict != UnblockReleased || finding.Kind != UnblockOwnerRuling {
				t.Fatalf("finding = %+v, want released on an owner ruling", finding)
			}
			if got := f.tasks.readmitted(); len(got) != 1 || got[0] != task.ID {
				t.Fatalf("readmitted = %v, want [%s]", got, task.ID)
			}
			audit := f.storedTask(task.ID).OutputArtifacts
			if len(audit) != 1 || audit[0].Name != rulingArtifactName(501) ||
				audit[0].Metadata["ruler"] != tc.author || audit[0].Metadata["comment_id"] != "501" {
				t.Fatalf("audit = %+v, want one owner_ruling artifact naming %s and comment 501", audit, tc.author)
			}
		})
	}
}

func TestSweep_ACommentThatIsNotARulingLeavesTheTaskParked(t *testing.T) {
	cases := map[string]struct {
		kind    orchestration.TaskBlockKind
		author  string
		body    string
		created time.Time
	}{
		"the project's own agent": {
			orchestration.TaskBlockKindNeedsInfo, "agent-api", "I think the answer is yes.", parkedAt.Add(time.Minute),
		},
		"a worker or reviewer bot": {
			orchestration.TaskBlockKindEscalated, "codex", "LGTM", parkedAt.Add(time.Minute),
		},
		"the factory's own status comment, whoever posted it": {
			orchestration.TaskBlockKindNeedsInfo, fixtureOwner, "<!-- quicbaas-sdlc:blocked task=x kind=needs_info -->\nparked", parkedAt.Add(time.Minute),
		},
		"an owner comment older than the park": {
			orchestration.TaskBlockKindNeedsInfo, fixtureOwner, "Earlier context.", parkedAt.Add(-time.Minute),
		},
		"an owner comment on an operator hold": {
			orchestration.TaskBlockKindBlocked, fixtureOwner, "Looking at it.", parkedAt.Add(time.Minute),
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSweepFixture(t)
			task := f.parkedForRuling(tc.kind)
			f.forge.comment(issueNumber(t, task), 502, tc.author, tc.body, tc.created)

			finding := onlyFinding(t, f.sweep(t))

			if finding.Verdict == UnblockReleased {
				t.Fatalf("finding = %+v: a comment that is not a ruling readmitted the task", finding)
			}
			if got := f.tasks.readmitted(); len(got) != 0 {
				t.Fatalf("readmitted = %v, want none", got)
			}
		})
	}
}

// TestSweep_OneRulingReadmitsOnce is the idempotency requirement: repeated
// comments readmit the task once, and a ruling that already readmitted a
// task does not readmit it again when the task parks a second time.
func TestSweep_OneRulingReadmitsOnce(t *testing.T) {
	f := newSweepFixture(t)
	task := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	number := issueNumber(t, task)
	f.forge.comment(number, 601, fixtureOwner, "Keep the v1 order.", parkedAt.Add(time.Minute))
	f.forge.comment(number, 602, "agent-company", "Relaying: keep the v1 order.", parkedAt.Add(2*time.Minute))

	onlyFinding(t, f.sweep(t))
	if got := f.tasks.readmitted(); len(got) != 1 {
		t.Fatalf("readmitted = %v after two ruling comments, want exactly one readmission", got)
	}

	// The next pass finds nothing parked.
	if report := f.sweep(t); len(report.Findings) != 0 {
		t.Fatalf("second pass = %+v, want no blocked task left to examine", report.Findings)
	}

	// The task parks again, and the park is stamped BEFORE the old comments
	// (a skewed forge clock) — only the audit artifact can tell that comment
	// 602 is already spent.
	f.tasks.conclude(task.ID, orchestration.TaskBlockKindNeedsInfo, "## What I need from you\n\nAnother question.")
	f.tasks.mu.Lock()
	for _, stored := range f.tasks.tasks {
		if stored.ID == task.ID {
			stored.StatusChangedAt = parkedAt.UnixNano()
		}
	}
	f.tasks.mu.Unlock()

	finding := onlyFinding(t, f.sweep(t))
	if finding.Verdict == UnblockReleased {
		t.Fatalf("finding = %+v: a spent ruling readmitted the task a second time", finding)
	}
	if got := f.tasks.readmitted(); len(got) != 1 {
		t.Fatalf("readmitted = %v, want still exactly one", got)
	}
}

func TestSweep_AnUnreadableCommentListLeavesTheTaskParkedAndIsReported(t *testing.T) {
	f := newSweepFixture(t)
	f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	f.forge.listCommentsErr = errors.New("forge down")

	finding := onlyFinding(t, f.sweep(t))

	if finding.Verdict == UnblockReleased || len(f.tasks.readmitted()) != 0 {
		t.Fatalf("finding = %+v: an unreadable forge released a task", finding)
	}
	if failures := f.sweeper.SweepFailures(); len(failures) != 1 {
		t.Fatalf("failures = %v, want the comment-listing failure reported", failures)
	}
}

func TestReadmitOnRuling_ReadmitsOnlyTheParkedTaskOfThatIssue(t *testing.T) {
	f := newSweepFixture(t)
	answered := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	other := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	f.forge.comment(issueNumber(t, answered), 701, fixtureOwner, "Yes.", parkedAt.Add(time.Minute))

	readmitted, err := f.sweeper.ReadmitOnRuling(context.Background(), testTenant, testProject, issueNumber(t, answered))
	if err != nil || !readmitted {
		t.Fatalf("ReadmitOnRuling = %v, %v; want a readmission", readmitted, err)
	}
	if got := f.tasks.readmitted(); len(got) != 1 || got[0] != answered.ID {
		t.Fatalf("readmitted = %v, want only %s (not %s)", got, answered.ID, other.ID)
	}
}

func TestReadmitOnRuling_ATaskThatIsNotParkedIsLeftAlone(t *testing.T) {
	f := newSweepFixture(t)
	task := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	f.tasks.mu.Lock()
	for _, stored := range f.tasks.tasks {
		stored.Status = orchestration.TaskStatusInProgress
	}
	f.tasks.mu.Unlock()
	f.forge.comment(issueNumber(t, task), 801, fixtureOwner, "Yes.", parkedAt.Add(time.Minute))

	readmitted, err := f.sweeper.ReadmitOnRuling(context.Background(), testTenant, testProject, issueNumber(t, task))
	if err != nil || readmitted {
		t.Fatalf("ReadmitOnRuling = %v, %v; want nothing for a running task", readmitted, err)
	}
	if got := f.tasks.readmitted(); len(got) != 0 {
		t.Fatalf("readmitted = %v, want none", got)
	}
}

func TestReadmitOnRuling_RefusesAnIncompleteAddress(t *testing.T) {
	f := newSweepFixture(t)
	var invalid *InvalidInputError
	if _, err := f.sweeper.ReadmitOnRuling(context.Background(), testTenant, testProject, 0); !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidInputError", err)
	}
}

func TestNewUnblockSweeper_ConfiguredRulersReplaceTheDefault(t *testing.T) {
	f := newSweepFixture(t)
	sweeper, err := NewUnblockSweeper(UnblockSweeperConfig{
		Tasks: f.tasks, Bindings: &fakeBindings{binding: testBinding()}, Forges: &fakeForges{forge: f.forge},
		Rulers: RulerPolicy{Default: []string{}},
	})
	if err != nil {
		t.Fatalf("NewUnblockSweeper: %v", err)
	}
	f.sweeper = sweeper
	task := f.parkedForRuling(orchestration.TaskBlockKindNeedsInfo)
	f.forge.comment(issueNumber(t, task), 901, "agent-company", "Yes.", parkedAt.Add(time.Minute))

	if finding := onlyFinding(t, f.sweep(t)); finding.Verdict == UnblockReleased {
		t.Fatalf("finding = %+v: a login outside the configured rulers readmitted the task", finding)
	}
}

// TestRuling_TheWebhookAndTheSweepOnTheRealEngineReadmitOnce drives the
// ruling path through the real AgentService and task store: a verified
// issue-comment delivery readmits the task at once, and the sweep that
// follows finds the same ruling and readmits nothing more.
func TestRuling_TheWebhookAndTheSweepOnTheRealEngineReadmitOnce(t *testing.T) {
	rig := newEngineRig(t)
	task := testTaskRecord(25)
	if err := rig.tasks.CreateTask(rig.ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := rig.svc.BlockTask(rig.ctx, testTenant, task.ID, orchestration.TaskBlockKindNeedsInfo, "## What I need from you\n\nWhich order?"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	rig.hooks.forge.comment(25, 1001, fixtureOwner, "Keep the v1 order.", time.Now().Add(time.Minute))

	readmitted, err := rig.sweeper.ReadmitOnRuling(context.Background(), testTenant, testProject, 25)
	if err != nil || !readmitted {
		t.Fatalf("ReadmitOnRuling = %v, %v; want a readmission", readmitted, err)
	}
	got, err := rig.tasks.GetTask(rig.ctx, testTenant, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != orchestration.TaskStatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
	var audited bool
	for _, artifact := range got.OutputArtifacts {
		if artifact.Name == rulingArtifactName(1001) && artifact.Metadata["ruler"] == fixtureOwner {
			audited = true
		}
	}
	if !audited {
		t.Fatalf("artifacts = %+v, want the ruling audited", got.OutputArtifacts)
	}

	again, err := rig.sweeper.ReadmitOnRuling(context.Background(), testTenant, testProject, 25)
	if err != nil || again {
		t.Fatalf("a repeated delivery = %v, %v; want nothing further", again, err)
	}
	report, err := rig.sweeper.SweepProject(rig.ctx, testTenant, testProject)
	if err != nil {
		t.Fatalf("SweepProject: %v", err)
	}
	if len(report.Released()) != 0 {
		t.Fatalf("sweep released %+v after the webhook already had", report.Released())
	}
}

// =============================================================================
// Webhook
// =============================================================================

type fakeRulings struct {
	calls []string
	err   error
}

func (r *fakeRulings) ReadmitOnRuling(_ context.Context, tenantID, projectID string, issue int) (bool, error) {
	r.calls = append(r.calls, tenantID+"/"+projectID+"#"+strconv.Itoa(issue))
	return r.err == nil, r.err
}

func TestWebhookIngest_AnIssueCommentIsExaminedAsARuling(t *testing.T) {
	rig := newWebhookRig(t)
	rulings := &fakeRulings{}
	rig.ingestor.SetRulingReadmitter(rulings)
	rig.forge.parseEvent = &ForgeEvent{Kind: EventIssueCommented, Repo: testRepoRef, IssueNumber: 24}

	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !result.RulingReadmitted || len(rulings.calls) != 1 || rulings.calls[0] != testTenant+"/"+testProject+"#24" {
		t.Fatalf("result = %+v calls = %v, want issue 24 examined and readmitted", result, rulings.calls)
	}
	if !result.Nudged {
		t.Error("an issue comment must still nudge the poller")
	}

	// Any other delivery is not a ruling.
	rig.forge.parseEvent = &ForgeEvent{Kind: EventIssueEdited, Repo: testRepoRef, IssueNumber: 24}
	if _, err := rig.ingestor.Ingest(context.Background(), delivery()); err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(rulings.calls) != 1 {
		t.Fatalf("calls = %v: an issue edit was examined as a ruling", rulings.calls)
	}
}

func TestWebhookIngest_ARulingFailureDoesNotFailTheDelivery(t *testing.T) {
	rig := newWebhookRig(t)
	rig.ingestor.SetRulingReadmitter(&fakeRulings{err: errors.New("forge down")})
	rig.forge.parseEvent = &ForgeEvent{Kind: EventIssueCommented, Repo: testRepoRef, IssueNumber: 24}

	result, err := rig.ingestor.Ingest(context.Background(), delivery())
	if err != nil {
		t.Fatalf("Ingest failed the delivery on a ruling error: %v", err)
	}
	if result.RulingReadmitted || result.RulingError == nil {
		t.Fatalf("result = %+v, want the ruling error carried and nothing readmitted", result)
	}
}

// =============================================================================
// Inbox flag
// =============================================================================

func TestAttentionService_List_FlagsARulingPosted(t *testing.T) {
	answered := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindEscalated)
	answered.StatusChangedAt = parkedAt.UnixNano()
	waiting := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindNeedsInfo)
	waiting.ID = "task-waiting"
	waiting.StatusChangedAt = parkedAt.UnixNano()
	waiting.Source = &orchestration.TaskSourceRef{Kind: TaskSourceKindForgeIssue, Ref: IssueRefString(testRepoRef, 78)}
	f := newAttentionFixture(t, answered, waiting)
	f.forge.comment(77, 1101, fixtureOwner, "Go with the recommendation.", parkedAt.Add(time.Minute))
	f.forge.comment(78, 1102, "agent-api", "Still waiting.", parkedAt.Add(time.Minute))

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	flags := map[string]bool{}
	for _, item := range page.Items {
		flags[item.ID] = item.RulingPosted
	}
	if !flags[answered.ID] || flags[waiting.ID] {
		t.Fatalf("ruling_posted = %v, want only %s flagged", flags, answered.ID)
	}
}

func TestAttentionService_List_AnUnreadableForgeLeavesTheFlagUnsetAndTheListingIntact(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindEscalated)
	f := newAttentionFixture(t, task)
	f.forge.listCommentsErr = errors.New("forge down")

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{}, nil)
	if err != nil {
		t.Fatalf("List failed on an unreadable forge: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].RulingPosted {
		t.Fatalf("items = %+v, want the task listed and not flagged", page.Items)
	}
}

func TestRulingText_CarriesTheFactoryMarker(t *testing.T) {
	body := rulingText(AttentionReadmit, &AttentionItem{ID: "task-1"}, "", true)
	if isRuling(ForgeComment{Author: fixtureOwner, Body: body}, rulerSet(testBinding(), nil)) {
		t.Fatal("the inbox's own ruling comment reads back as a fresh owner ruling")
	}
}
