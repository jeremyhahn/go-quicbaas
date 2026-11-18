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
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func TestTypedErrors_NameTheirSubjectAndUnwrap(t *testing.T) {
	cases := []struct {
		err      error
		sentinel error
		contains string
	}{
		{&NotFoundError{Entity: "binding", TenantID: "t", ID: "p"}, ErrNotFound, "binding p not found in tenant t"},
		{&AlreadyExistsError{Entity: "binding", TenantID: "t", ID: "p"}, ErrAlreadyExists, "already exists"},
		{&VersionConflictError{Entity: "delivery", TenantID: "t", ID: "x", Expected: 4}, ErrVersionConflict, "expected version 4"},
		{&InvalidInputError{Field: "f", Message: "m"}, ErrInvalidInput, "invalid f: m"},
		{&RepoURLRefusedError{RepoURL: "u", Reason: "r"}, ErrInvalidInput, "refused: r"},
		{&TriggerLabelCollisionError{Key: LabelMerged, Label: "x"}, ErrInvalidInput, "display label for merged"},
		{&StrategyNotRegisteredError{Name: "n"}, ErrStrategyNotRegistered, "no strategy registered under n"},
		{&StrategyNotRegisteredError{Name: "", Available: []string{"github-flow", "gitflow"}}, ErrStrategyNotRegistered, `under ""; available: github-flow, gitflow`},
		{&ForgeNotRegisteredError{Type: "gitlab"}, ErrForgeNotRegistered, "gitlab"},
		{&ForgeNotRegisteredError{Type: "gitlab", Available: []ForgeType{ForgeGitea}}, ErrForgeNotRegistered, "available: gitea"},
		{&ForgeUnsupportedError{Type: "gitea", Capability: "logs"}, ErrForgeUnsupported, "does not support logs"},
		{&ForgeWriteNotAppliedError{Type: "gitea", Operation: "merge", Subject: "s", Want: "a", Got: "b"}, ErrForgeWriteNotApplied, "want a, got b"},
		{&ForgeWriteNotAppliedError{Type: "gitea", Operation: "merge", Subject: "s"}, ErrForgeWriteNotApplied, "did not take effect"},
		{&ForgeNotReadyError{Type: "gitea", Operation: "merge", Subject: "s", Detail: "d"}, ErrForgeNotReady, "merge s: d"},
		{&ForgeNotReadyError{Type: "gitea", Operation: "merge", Subject: "s"}, ErrForgeNotReady, "merge s"},
		{&WebhookUnverifiedError{Type: "gitea", Reason: "hmac"}, ErrWebhookUnverified, "hmac"},
		{&WebhookSecretMissingError{ProjectID: "p", Reason: "r"}, ErrWebhookSecretMissing, "project p"},
		{&WebhookRepoMismatchError{ProjectID: "p", Configured: RepoRef{Owner: "a", Repo: "b"}, Delivered: RepoRef{Owner: "c", Repo: "d"}}, ErrWebhookRepoMismatch, "a/b"},
		{&TaskSourceRepoMismatchError{TaskID: "t", ProjectID: "p", Binding: RepoRef{Owner: "a", Repo: "b"}, Source: RepoRef{Owner: "c", Repo: "d"}}, ErrTaskSourceRepoMismatch, "task t in project p is bound to a/b"},
		{&MergeNotReadyExhaustedError{Subject: "s", Attempts: 2, Last: errors.New("x")}, ErrMergeNotReadyExhausted, "2 attempt(s): x"},
		{&MergeNotReadyExhaustedError{Subject: "s", Attempts: 2}, ErrMergeNotReadyExhausted, "2 attempt(s)"},
		{&HeadMovedError{Subject: "s", Reviewed: "a", Current: "b"}, ErrHeadMoved, "from reviewed a to b"},
		{&MergeConflictError{Subject: "s", Base: "main", Head: "abc", Attempts: 6}, ErrMergeConflict, "s at abc conflicts with its base main"},
		{&NoChangesError{TaskID: "t", WorkDir: "/w"}, ErrNoChanges, "task t produced no changes in /w"},
		{&TaskNotForgeIssueError{TaskID: "t", Kind: "operator"}, ErrTaskNotForgeIssue, `"operator"`},
		{&IncompleteResponseError{Op: "VerifyToken", Missing: "identity"}, ErrIncompleteResponse, "returned no identity"},
		{&IncompleteResponseError{Op: "VerifyToken", Missing: "identity"}, ErrBackend, "returned no identity"},
		{&WebhookRouteConflictError{ProjectID: "p"}, ErrWebhookRouteConflict, "project p"},
		{&SurfacingUndeliveredError{IssueNumber: 3, Attempts: 2}, nil, "issue #3"},
		{&KeySegmentError{Field: "f", Value: "v", Message: "m"}, ErrInvalidKeySegment, "f: m"},
		{backendError("op", errors.New("boom")), ErrBackend, "op: boom"},
		{&UnblockConditionError{Reason: "r", Detail: "d", Sentinel: ErrUnblockConditionAbsent}, ErrUnblockConditionAbsent, "d"},
		{&UnknownRunnerStateError{State: "decomposing"}, ErrRunner, `no state "decomposing"`},
		{&UnknownPostMergeStepError{Kind: "tag"}, ErrRunner, `post-merge step "tag"`},
		{&WorkerSessionError{TaskID: "t", Message: "no session"}, ErrRunner, "task t: no session"},
		{&WorkspaceUnavailableError{ProjectID: "p"}, ErrRunner, "project p has no workspace"},
		{&WorkspaceUnavailableError{ProjectID: "p", Cause: errors.New("mkdir refused")}, ErrRunner, "mkdir refused"},
		{&WorkspaceUnavailableError{ProjectID: "p", Cause: os.ErrPermission}, os.ErrPermission, "project p"},
		{&EscalatedError{TaskID: "t", Reason: "rounds spent"}, ErrRunner, "task t escalated: rounds spent"},
		{&ReviewVerdictUnreadableError{Reason: "no declaration"}, ErrReview, "no declaration"},
		{&ReviewerNotConfiguredError{ProjectID: "p"}, ErrReview, `project "p" binds no reviewer`},
		{&ReviewerNotIndependentError{ProjectID: "p", Family: "anthropic"}, ErrReview, `both "anthropic"`},
		{&ReviewSessionError{PullRequest: 41, Message: "no session"}, ErrReview, "pull request #41: no session"},
	}
	for _, tc := range cases {
		if !strings.Contains(tc.err.Error(), tc.contains) {
			t.Fatalf("%T: %q lacks %q", tc.err, tc.err.Error(), tc.contains)
		}
		if tc.sentinel != nil && !errors.Is(tc.err, tc.sentinel) {
			t.Fatalf("%T does not unwrap to %v", tc.err, tc.sentinel)
		}
	}
	if UnblockOwnerRuling.String() != "owner_ruling" {
		t.Fatal("UnblockKind.String")
	}
}

func TestIssueTriggered_EmptyTriggerNeverMatches(t *testing.T) {
	binding := testBinding()
	binding.Forge.TriggerLabel = ""
	if issueTriggered(binding, &ForgeIssue{Labels: []string{""}}) {
		t.Fatal("an empty trigger matched")
	}
}

// conflictOnce refuses the first update after moving the record.
type conflictOnce struct {
	*Store
	moved bool
}

func (c *conflictOnce) UpdateDelivery(ctx context.Context, tenantID string, delivery *Delivery) error {
	if !c.moved {
		c.moved = true
		other, err := c.Store.GetDelivery(ctx, tenantID, delivery.TaskID)
		if err != nil {
			return err
		}
		other.Reviews = append(other.Reviews, ReviewRecord{Reviewer: "racer", Verdict: VerdictApprove, HeadSHA: "x"})
		if err := c.Store.UpdateDelivery(ctx, tenantID, other); err != nil {
			return err
		}
	}
	return c.Store.UpdateDelivery(ctx, tenantID, delivery)
}

// alwaysConflicts never lets a guarded write through.
type alwaysConflicts struct {
	*Store
}

func (c *alwaysConflicts) UpdateDelivery(_ context.Context, tenantID string, delivery *Delivery) error {
	return &VersionConflictError{Entity: deliveryEntityName, TenantID: tenantID, ID: delivery.TaskID, Expected: delivery.Version}
}

func TestHooks_UpdateDeliveryReappliesOnConflictAndGivesUp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.PutDelivery(ctx, testTenant, &Delivery{TaskID: testTask, Attempt: 1, Branch: "b"}); err != nil {
		t.Fatal(err)
	}
	job := testJob(1)
	h := &Hooks{deliveries: &conflictOnce{Store: store}}
	d, _ := store.GetDelivery(ctx, testTenant, testTask)
	if err := h.updateDelivery(ctx, job, d, func(d *Delivery) { d.HeadSHA = "mine" }); err != nil {
		t.Fatalf("conflict not re-applied: %v", err)
	}
	if d.HeadSHA != "mine" || len(d.Reviews) != 1 || d.Reviews[0].Reviewer != "racer" {
		t.Fatalf("merged record = %+v", d)
	}
	stuck := &Hooks{deliveries: &alwaysConflicts{Store: store}}
	if err := stuck.updateDelivery(ctx, job, d, func(*Delivery) {}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a permanent conflict must surface: %v", err)
	}
}

func TestReviewValidator_RecordGivesUpOnAPermanentConflict(t *testing.T) {
	rig := newReviewRig(t)
	rig.approveBy("codex", "h1", 1)
	validator, err := NewReviewValidator(&alwaysConflicts{Store: rig.store}, rig.store, &fakeForges{forge: rig.forge}, testAgentCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validator.Validate(context.Background(), testJob(1), rig.criterion); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("permanent conflict: %v", err)
	}
}

func TestBlockReasonRendering_DefaultKindLabel(t *testing.T) {
	body := renderBlocked(sdlcPrompts, testBinding(), testTaskRecord(1), 1, orchestration.BlockReason{Kind: orchestration.TaskBlockKindBlocked, Message: "waiting on infra"}, nil)
	if !strings.Contains(body, "`task:ready` → `factory:blocked`") || !strings.Contains(body, "waits on an external condition") {
		t.Fatalf("blocked render:\n%s", body)
	}
}
