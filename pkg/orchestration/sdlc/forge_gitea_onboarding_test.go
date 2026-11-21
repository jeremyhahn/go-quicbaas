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
	"reflect"
	"strconv"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
)

// =============================================================================
// Labels
// =============================================================================

// TestGiteaEnsureLabels_CreatesOnlyWhatIsMissing is the provisioning property:
// a repository that already carries part of the vocabulary gets the rest and
// nothing else.
func TestGiteaEnsureLabels_CreatesOnlyWhatIsMissing(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutLabel("factory:blocked", "111111", "a human wrote this")

	specs := []LabelSpec{
		{Name: "factory:blocked", Color: "d93f0b", Description: "the module's text"},
		{Name: "factory:in-progress", Color: "fbca04"},
		{Name: "factory:merged"},
	}
	if err := backend.EnsureLabels(context.Background(), testRepo, specs); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}

	want := []string{"factory:blocked", "factory:in-progress", "factory:merged"}
	if got := gitea.LabelNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
	if created := gitea.Calls("POST /labels"); created != 2 {
		t.Errorf("created %d labels, want 2; the existing one must not be written again", created)
	}
}

// TestGiteaEnsureLabels_NeverTouchesAnExistingLabel is the non-destruction
// property, and it is the one that must never regress.
//
// Labels carry history: every past pull request and every saved search refers
// to them by name, and their colour is somebody's choice. A provisioning pass
// that "corrected" either would rewrite a repository's record of itself to
// make a config file tidier.
func TestGiteaEnsureLabels_NeverTouchesAnExistingLabel(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutLabel("factory:blocked", "ff00ff", "the operator's own words")

	specs := []LabelSpec{{Name: "factory:blocked", Color: "d93f0b", Description: "the module's words"}}
	if err := backend.EnsureLabels(context.Background(), testRepo, specs); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}

	held, ok := gitea.Label("factory:blocked")
	if !ok {
		t.Fatal("the label was destroyed")
	}
	if held.Color != "ff00ff" {
		t.Errorf("colour = %q, want ff00ff unchanged; provisioning recoloured somebody else's label", held.Color)
	}
	if held.Description != "the operator's own words" {
		t.Errorf("description = %q, want it unchanged", held.Description)
	}
	if wrote := gitea.Calls("POST /labels"); wrote != 0 {
		t.Errorf("wrote %d labels, want 0", wrote)
	}
}

// TestGiteaEnsureLabels_IsIdempotent proves a second pass is a no-op.
func TestGiteaEnsureLabels_IsIdempotent(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	specs := []LabelSpec{{Name: "factory:in-progress"}, {Name: "factory:merged"}}

	for round := 1; round <= 2; round++ {
		if err := backend.EnsureLabels(context.Background(), testRepo, specs); err != nil {
			t.Fatalf("round %d: EnsureLabels: %v", round, err)
		}
	}
	if got := gitea.LabelNames(); len(got) != 2 {
		t.Fatalf("labels = %v, want exactly the two specs", got)
	}
	if created := gitea.Calls("POST /labels"); created != 2 {
		t.Errorf("created %d labels over two passes, want 2", created)
	}
}

// TestGiteaEnsureLabels_MatchesExistingLabelsCaseInsensitively stops a
// duplicate nothing can tell apart from being created.
func TestGiteaEnsureLabels_MatchesExistingLabelsCaseInsensitively(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutLabel("Factory:Blocked", "d93f0b", "")

	if err := backend.EnsureLabels(context.Background(), testRepo, []LabelSpec{{Name: "factory:blocked"}}); err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	if got := gitea.LabelNames(); len(got) != 1 {
		t.Fatalf("labels = %v, want one; a case variant was created as a duplicate", got)
	}
}

// TestGiteaEnsureLabels_RefusesABlankName is the bad-input half: a spec the
// forge could not create fails here with the reason, not at the forge with a
// 422.
func TestGiteaEnsureLabels_RefusesABlankName(t *testing.T) {
	backend, gitea := newStatefulGitea(t)

	err := backend.EnsureLabels(context.Background(), testRepo, []LabelSpec{{Name: "  "}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if reads := gitea.Calls("GET /labels"); reads != 0 {
		t.Errorf("the forge was read %d times for a spec that could never be valid", reads)
	}
}

// TestGiteaEnsureLabels_ReportsAWriteThatDidNotTakeEffect covers the
// read-back.
//
// Gitea has answered 200 for label writes that did nothing — the case this
// adapter's read-back rule was written for. It cannot be staged against a
// live instance or against the stateful fixture, both of which store what
// they accept, so it is staged here: a creation that reports success over a
// listing that never grows.
func TestGiteaEnsureLabels_ReportsAWriteThatDidNotTakeEffect(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, []map[string]any{})
	fixture.respondJSON("POST /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusCreated,
		map[string]any{"id": 1, "name": "factory:blocked"})

	err := backend.EnsureLabels(context.Background(), testRepo, []LabelSpec{{Name: "factory:blocked"}})
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied; a 200 for a write that did nothing read as success", err)
	}
}

// TestGiteaLabels_FollowsPages proves the listing does not stop at the first
// page. A prefix would report a label the repository plainly defines as
// missing and send an operator to fix a vocabulary that is already correct.
func TestGiteaLabels_FollowsPages(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	for i := range giteafake.DefaultPageSize + 3 {
		gitea.PutLabel("label-"+strconv.Itoa(i), "ededed", "")
	}

	labels, err := backend.Labels(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if len(labels) != giteafake.DefaultPageSize+3 {
		t.Fatalf("read %d labels, want %d; the listing stopped at a page boundary",
			len(labels), giteafake.DefaultPageSize+3)
	}
}

// =============================================================================
// Webhooks
// =============================================================================

// TestGiteaWebhooks_ReadsWhatIsConfigured covers the read the conformance pass
// depends on, including the inactive case that looks present in a listing and
// delivers nothing.
func TestGiteaWebhooks_ReadsWhatIsConfigured(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutHook("https://node-a.test/api/v1/sdlc/webhook/p1", true, "issues")
	gitea.PutHook("https://node-b.test/api/v1/sdlc/webhook/p1", false, "issues")

	hooks, err := backend.Webhooks(context.Background(), testRepo)
	if err != nil {
		t.Fatalf("Webhooks: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("read %d deliveries, want 2", len(hooks))
	}
	if !hooks[0].Active || hooks[1].Active {
		t.Errorf("active flags = %v/%v, want true/false", hooks[0].Active, hooks[1].Active)
	}
	if hooks[0].ID == "" {
		t.Error("a delivery came back without an identifier")
	}
}

// TestGiteaDeleteWebhook_RemovesEveryMatchAndLeavesTheRest is offboarding: the
// project's delivery goes, another project's stays.
func TestGiteaDeleteWebhook_RemovesEveryMatchAndLeavesTheRest(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	mine := "https://node.test/api/v1/sdlc/webhook/p1"
	gitea.PutHook(mine, true, "issues")
	gitea.PutHook(mine, true, "issues") // a duplicate an earlier retry left
	gitea.PutHook("https://node.test/api/v1/sdlc/webhook/p2", true, "issues")

	if err := backend.DeleteWebhook(context.Background(), testRepo, mine); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	want := []string{"https://node.test/api/v1/sdlc/webhook/p2"}
	if got := gitea.HookURLs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("hooks = %v, want %v", got, want)
	}
}

// TestGiteaDeleteWebhook_IsSafeToRepeat: removing a delivery that is not there
// is the desired state, reached by someone else.
func TestGiteaDeleteWebhook_IsSafeToRepeat(t *testing.T) {
	backend, gitea := newStatefulGitea(t)

	if err := backend.DeleteWebhook(context.Background(), testRepo, "https://node.test/api/v1/sdlc/webhook/p1"); err != nil {
		t.Fatalf("DeleteWebhook on an empty repository: %v", err)
	}
	if deletes := gitea.Calls("DELETE /hooks/1"); deletes != 0 {
		t.Errorf("issued %d deletes for a delivery that does not exist", deletes)
	}
}

// TestGiteaDeleteWebhook_RefusesAnEmptyURL is the bad-input half, and it
// guards something worse than a bad request: an empty URL matched against
// every hook's config would delete a repository's whole webhook set.
func TestGiteaDeleteWebhook_RefusesAnEmptyURL(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutHook("https://node.test/api/v1/sdlc/webhook/p1", true, "issues")

	if err := backend.DeleteWebhook(context.Background(), testRepo, "  "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
	if len(gitea.HookURLs()) != 1 {
		t.Error("an empty URL removed a delivery")
	}
}

// TestGiteaEnsureWebhook_IsIdempotentAgainstAStatefulForge proves the property
// the canned-response suite cannot: two passes leave ONE delivery.
func TestGiteaEnsureWebhook_IsIdempotentAgainstAStatefulForge(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	spec := WebhookSpec{
		URL:    "https://node.test/api/v1/sdlc/webhook/p1",
		Events: []string{"issues", "pull_request"},
	}

	for round := 1; round <= 2; round++ {
		if err := backend.EnsureWebhook(context.Background(), testRepo, spec, []byte("s3cret")); err != nil {
			t.Fatalf("round %d: EnsureWebhook: %v", round, err)
		}
	}
	if got := gitea.HookURLs(); len(got) != 1 {
		t.Fatalf("hooks = %v, want exactly one", got)
	}
	if created := gitea.Calls("POST /hooks"); created != 1 {
		t.Errorf("created %d deliveries over two passes, want 1", created)
	}
}

// =============================================================================
// Repository tree
// =============================================================================

// TestGiteaRepoFiles_ListsADirectory covers the stale-workflow read.
func TestGiteaRepoFiles_ListsADirectory(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutDir(".github/workflows", "ci.yml", "automerge.yml")

	files, err := backend.RepoFiles(context.Background(), testRepo, "main", ".github/workflows")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v, want 2", files)
	}
	if files[0].Path != ".github/workflows/ci.yml" {
		t.Errorf("path = %q, want the full repository path", files[0].Path)
	}
}

// TestGiteaRepoFiles_AnAbsentDirectoryIsTheDesiredState is the case the whole
// check exists for. A repository that has no .github/workflows at all has
// reached the goal, and must not be the one the check errors on.
func TestGiteaRepoFiles_AnAbsentDirectoryIsTheDesiredState(t *testing.T) {
	backend, _ := newStatefulGitea(t)

	files, err := backend.RepoFiles(context.Background(), testRepo, "", ".github/workflows")
	if err != nil {
		t.Fatalf("an absent directory reported an error: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %+v, want none", files)
	}
}

// TestGiteaRepoFiles_ReportsAForgeFailure separates "the directory is not
// there" from "the forge could not be read" — a distinction the conformance
// report turns into two different findings.
func TestGiteaRepoFiles_ReportsAForgeFailure(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutDir(".github/workflows", "ci.yml")
	gitea.FailOnce("GET /contents/.github/workflows")

	if _, err := backend.RepoFiles(context.Background(), testRepo, "", ".github/workflows"); !errors.Is(err, ErrBackend) {
		t.Fatalf("error = %v, want ErrBackend", err)
	}
}

// =============================================================================
// Branch protection
// =============================================================================

// TestGiteaBranchProtection_ReadsTheRequiredContexts covers the read the merge
// gate's conformance depends on.
func TestGiteaBranchProtection_ReadsTheRequiredContexts(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutProtection("main", true, "CI / CI Gate (pull_request)", DefaultReviewStatusContext)

	protection, err := backend.BranchProtection(context.Background(), testRepo, "main")
	if err != nil {
		t.Fatalf("BranchProtection: %v", err)
	}
	if protection == nil {
		t.Fatal("a protected branch came back unprotected")
	}
	if !protection.RequiresContext(DefaultReviewStatusContext) {
		t.Errorf("contexts = %v, want %s among them", protection.RequiredContexts, DefaultReviewStatusContext)
	}
}

// TestGiteaBranchProtection_AnUnprotectedBranchIsNilNotAnError: "this branch
// is unprotected" is a conformance finding to report, and a caller made to
// pick it out of a NotFoundError would get it wrong.
func TestGiteaBranchProtection_AnUnprotectedBranchIsNilNotAnError(t *testing.T) {
	backend, _ := newStatefulGitea(t)

	protection, err := backend.BranchProtection(context.Background(), testRepo, "main")
	if err != nil {
		t.Fatalf("an unprotected branch reported an error: %v", err)
	}
	if protection != nil {
		t.Fatalf("protection = %+v, want nil", protection)
	}
}

// TestGiteaBranchProtection_RefusesAnEmptyBranch is the bad-input half.
func TestGiteaBranchProtection_RefusesAnEmptyBranch(t *testing.T) {
	backend, _ := newStatefulGitea(t)

	if _, err := backend.BranchProtection(context.Background(), testRepo, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

// TestRequiresContext_IgnoresARuleWithChecksSwitchedOff: a rule that lists a
// context it does not enforce demands nothing, and reading it as enforced
// would report a repository gated when it is not.
func TestRequiresContext_IgnoresARuleWithChecksSwitchedOff(t *testing.T) {
	cases := map[string]struct {
		protection *ForgeBranchProtection
		want       bool
	}{
		"nil":            {nil, false},
		"checks off":     {&ForgeBranchProtection{RequiredContexts: []string{"quicbaas/review"}}, false},
		"checks on":      {&ForgeBranchProtection{RequireStatusChecks: true, RequiredContexts: []string{"quicbaas/review"}}, true},
		"case insensiti": {&ForgeBranchProtection{RequireStatusChecks: true, RequiredContexts: []string{"QuicBaaS/Review"}}, true},
		"absent":         {&ForgeBranchProtection{RequireStatusChecks: true, RequiredContexts: []string{"ci"}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.protection.RequiresContext("quicbaas/review"); got != tc.want {
				t.Errorf("RequiresContext = %v, want %v", got, tc.want)
			}
		})
	}
}

// =============================================================================
// The failure paths
// =============================================================================

// TestGiteaOnboarding_ReportsAForgeThatCannotBeRead: a conformance pass has
// to be able to tell "the repository is in this state" from "the forge could
// not be read", and every read here must therefore fail loudly rather than
// answer empty.
func TestGiteaOnboarding_ReportsAForgeThatCannotBeRead(t *testing.T) {
	cases := map[string]struct {
		stage string
		call  func(*GiteaBackend) error
	}{
		"labels": {"GET /labels", func(b *GiteaBackend) error {
			_, err := b.Labels(context.Background(), testRepo)
			return err
		}},
		"webhooks": {"GET /hooks", func(b *GiteaBackend) error {
			_, err := b.Webhooks(context.Background(), testRepo)
			return err
		}},
		"branch protection": {"GET /branch_protections/main", func(b *GiteaBackend) error {
			_, err := b.BranchProtection(context.Background(), testRepo, "main")
			return err
		}},
		"ensure labels": {"GET /labels", func(b *GiteaBackend) error {
			return b.EnsureLabels(context.Background(), testRepo, []LabelSpec{{Name: "x"}})
		}},
		"delete webhook": {"GET /hooks", func(b *GiteaBackend) error {
			return b.DeleteWebhook(context.Background(), testRepo, "https://node.test/hook")
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			backend, gitea := newStatefulGitea(t)
			gitea.PutProtection("main", true, DefaultReviewStatusContext)
			gitea.FailOnce(tc.stage)

			if err := tc.call(backend); !errors.Is(err, ErrBackend) {
				t.Fatalf("error = %v, want ErrBackend", err)
			}
		})
	}
}

// TestGiteaDeleteWebhook_ReportsADeletionThatDidNotTakeEffect covers the
// read-back on the offboarding path: a delivery the forge said it removed and
// did not would keep reaching a node that no longer drives the project.
func TestGiteaDeleteWebhook_ReportsADeletionThatDidNotTakeEffect(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	hook := map[string]any{"id": 7, "active": true, "config": map[string]string{"url": "https://node.test/hook"}}
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks", http.StatusOK, []any{hook})
	fixture.respondJSON("DELETE /api/v1/repos/jeremyhahn/go-quicbaas/hooks/7", http.StatusNoContent, nil)

	err := backend.DeleteWebhook(context.Background(), testRepo, "https://node.test/hook")
	if !errors.Is(err, ErrForgeWriteNotApplied) {
		t.Fatalf("error = %v, want ErrForgeWriteNotApplied", err)
	}
}

// TestGiteaRepoFiles_AnsweredAsAFileRatherThanADirectory: the contents route
// answers an OBJECT for a file, and a caller that named one must get that
// entry back rather than a decode failure.
func TestGiteaRepoFiles_AnsweredAsAFileRatherThanADirectory(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutFile(".github/workflows/automerge.yml")

	files, err := backend.RepoFiles(context.Background(), testRepo, "", ".github/workflows/automerge.yml")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	if len(files) != 1 || files[0].Path != ".github/workflows/automerge.yml" || files[0].IsDir {
		t.Fatalf("files = %+v, want the one file", files)
	}
}

// TestGiteaRepoFiles_EscapesEachPathSegment: escaping the whole path would
// turn its separators into %2F and address a file whose name contains
// slashes, which no repository has.
func TestGiteaRepoFiles_EscapesEachPathSegment(t *testing.T) {
	backend, gitea := newStatefulGitea(t)
	gitea.PutDir("a dir/sub", "x.yml")

	files, err := backend.RepoFiles(context.Background(), testRepo, "", "/a dir/sub/")
	if err != nil {
		t.Fatalf("RepoFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "x.yml" {
		t.Fatalf("files = %+v, want the directory's one entry", files)
	}
	// The server saw ONE path segment per separator, with the space escaped
	// on the wire and decoded back here. Escaping the whole path would have
	// sent %2F and addressed a file nobody has.
	if served := gitea.Calls("GET /contents/a dir/sub"); served != 1 {
		t.Fatalf("the path did not arrive as its own segments; calls = %d", served)
	}
	// The repository root is addressable as an empty path.
	gitea.PutDir("", "README.md")
	if _, err := backend.RepoFiles(context.Background(), testRepo, "main", ""); err != nil {
		t.Fatalf("RepoFiles at the root: %v", err)
	}
}

// TestGiteaLabels_RefusesAListingItCouldNotFinish: a short list with a nil
// error is indistinguishable from a complete one, and a caller would read a
// label the repository defines as missing and create a duplicate.
func TestGiteaLabels_RefusesAListingItCouldNotFinish(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// Every page is full, so the listing never ends.
	page := make([]giteaLabel, giteafake.DefaultPageSize)
	for i := range page {
		page[i] = giteaLabel{ID: int64(i + 1), Name: "label-" + strconv.Itoa(i)}
	}
	fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/labels", http.StatusOK, page)

	_, err := backend.Labels(context.Background(), testRepo)
	if !errors.Is(err, ErrIncompleteResponse) {
		t.Fatalf("error = %v, want ErrIncompleteResponse", err)
	}
}

// TestGiteaDeleteWebhook_TreatsARaceAsTheDesiredState: a delivery another
// actor removed between the listing and the delete has reached the state this
// call wanted, by someone else's hand.
func TestGiteaDeleteWebhook_TreatsARaceAsTheDesiredState(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	hook := map[string]any{"id": 7, "active": true, "config": map[string]string{"url": "https://node.test/hook"}}
	listed := 0
	fixture.handlers["GET /api/v1/repos/jeremyhahn/go-quicbaas/hooks"] = func(w http.ResponseWriter, _ *http.Request) {
		listed++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// The first listing sees the hook; by the second it is gone.
		payload := []any{hook}
		if listed > 1 {
			payload = []any{}
		}
		_ = json.NewEncoder(w).Encode(payload)
	}
	fixture.respondJSON("DELETE /api/v1/repos/jeremyhahn/go-quicbaas/hooks/7", http.StatusNotFound,
		map[string]any{"message": "not found"})

	if err := backend.DeleteWebhook(context.Background(), testRepo, "https://node.test/hook"); err != nil {
		t.Fatalf("a delivery removed by someone else was reported as a failure: %v", err)
	}
}

// TestGiteaBranchProtection_NamesTheBranchWhenTheForgeDoesNot: Gitea has
// answered a rule with neither branch_name nor rule_name, and a report whose
// subject is empty names nothing an operator can act on.
func TestGiteaBranchProtection_NamesTheBranchWhenTheForgeDoesNot(t *testing.T) {
	cases := map[string]map[string]any{
		"rule_name only": {"rule_name": "release/*", "enable_status_check": true},
		"neither":        {"enable_status_check": true},
	}
	wants := map[string]string{"rule_name only": "release/*", "neither": "main"}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			backend, fixture := newGiteaFixture(t)
			fixture.respondJSON("GET /api/v1/repos/jeremyhahn/go-quicbaas/branch_protections/main", http.StatusOK, payload)

			protection, err := backend.BranchProtection(context.Background(), testRepo, "main")
			if err != nil {
				t.Fatalf("BranchProtection: %v", err)
			}
			if protection.Branch != wants[name] {
				t.Errorf("Branch = %q, want %q", protection.Branch, wants[name])
			}
		})
	}
}

// TestGiteaDeleteWebhook_ARepositoryThatIsGoneHoldsNoDelivery.
//
// Offboarding refuses when the webhook cannot be removed, so that the
// binding is never deleted while a live delivery survives it. A repository
// somebody deleted on the forge would otherwise be trapped: there is nothing
// to remove and nothing ever could be, and the project could never be
// unbound. A forge that is merely DOWN answers 5xx and is still refused,
// which is the case worth retrying.
func TestGiteaDeleteWebhook_ARepositoryThatIsGoneHoldsNoDelivery(t *testing.T) {
	backend, fixture := newGiteaFixture(t)
	// No handler for the hooks route: the stub answers 404, as a forge does
	// for a repository that is not there.
	_ = fixture

	if err := backend.DeleteWebhook(context.Background(), testRepo, "https://node.test/hook"); err != nil {
		t.Fatalf("a deleted repository trapped the unbind: %v", err)
	}
}
