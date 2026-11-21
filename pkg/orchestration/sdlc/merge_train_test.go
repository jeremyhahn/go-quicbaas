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
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Policy
// =============================================================================

func TestMergeTrainPolicy_DefaultsAndBounds(t *testing.T) {
	if got := (MergeTrainPolicy{}).Size(); got != DefaultMergeTrainMaxSize {
		t.Fatalf("zero policy size = %d, want the default %d", got, DefaultMergeTrainMaxSize)
	}
	if got := (MergeTrainPolicy{MaxSize: 3}).Size(); got != 3 {
		t.Fatalf("size = %d, want 3", got)
	}
	for _, size := range []int{0, 1, MaxMergeTrainSize} {
		if err := (MergeTrainPolicy{MaxSize: size}).Validate(); err != nil {
			t.Fatalf("max_size %d refused: %v", size, err)
		}
	}
	for _, size := range []int{-1, MaxMergeTrainSize + 1} {
		var invalid *InvalidInputError
		if err := (MergeTrainPolicy{MaxSize: size}).Validate(); !errors.As(err, &invalid) || invalid.Field != "merge_train.max_size" {
			t.Fatalf("max_size %d: want an InvalidInputError on merge_train.max_size, got %v", size, err)
		}
	}
}

// TestTrainEnabled_OnByDefaultForASelfReviewedProject pins the default the
// owner asked for, and the one case it cannot apply: a train lands only on a
// verdict from a reviewer profile that counts toward merge, so a project
// reviewed by people, or only advisorily, is not trained — and says why.
func TestTrainEnabled_OnByDefaultForASelfReviewedProject(t *testing.T) {
	binding := testBinding()
	if binding.TrainEnabled() || !strings.Contains(binding.TrainOffReason(), "counts toward merge") {
		t.Fatalf("a project with no reviewer of its own was trained, or not told why: %q", binding.TrainOffReason())
	}
	binding.Reviewer = trainTestReviewer()
	if !binding.TrainEnabled() || binding.TrainOffReason() != "" {
		t.Fatalf("the zero merge-train policy on a self-reviewed project did not train it: %q", binding.TrainOffReason())
	}
	binding.MergeTrain.Disabled = true
	if binding.TrainEnabled() || !strings.Contains(binding.TrainOffReason(), "turned off") {
		t.Fatalf("a disabled train still trained the project, or did not say so: %q", binding.TrainOffReason())
	}
	var nilBinding *ProjectBinding
	if nilBinding.TrainEnabled() || nilBinding.TrainOffReason() == "" {
		t.Fatal("a nil binding was trained")
	}
}

// TestTrainReviewer_IsTheFirstProfileThatCounts proves which reviewer judges a
// train: the default when it counts, otherwise the first counting alternate,
// and none — so no train — when every profile is advisory.
func TestTrainReviewer_IsTheFirstProfileThatCounts(t *testing.T) {
	no, yes := false, true
	binding := testBinding()
	binding.Reviewer = trainTestReviewer()
	if got, ok := binding.TrainReviewer(); !ok || !got.Default || !got.Counts {
		t.Fatalf("TrainReviewer = %+v, %v; want the counting default", got, ok)
	}

	binding.Reviewer.CountsTowardMerge = &no
	binding.Reviewers = []ReviewerConfig{
		{Name: "second-opinion", Agent: testReviewerAgent, Login: "advisor", TokenSecretRef: "advisor-token"},
		{Name: "gate", Agent: testReviewerAgent, Login: "gatekeeper", TokenSecretRef: "gate-token", CountsTowardMerge: &yes},
	}
	got, ok := binding.TrainReviewer()
	if !ok || got.Name != "gate" || got.Context() != DefaultReviewStatusContext {
		t.Fatalf("TrainReviewer = %+v, %v; want the counting alternate under the required context", got, ok)
	}
	if !binding.TrainEnabled() {
		t.Fatalf("a project with a counting alternate was not trained: %q", binding.TrainOffReason())
	}

	binding.Reviewers = binding.Reviewers[:1]
	if _, ok := binding.TrainReviewer(); ok {
		t.Fatal("an advisory-only project named a train reviewer")
	}
	if binding.TrainEnabled() {
		t.Fatal("an advisory-only project was trained")
	}
}

func TestProjectBinding_ValidateRefusesABadTrainBound(t *testing.T) {
	binding := testBinding()
	binding.MergeTrain.MaxSize = MaxMergeTrainSize + 1
	if err := binding.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("an out-of-range train size was bound: %v", err)
	}
}

func TestTrainBranchNames(t *testing.T) {
	if got := TrainBranch(7); got != "queue/7" {
		t.Fatalf("TrainBranch = %q", got)
	}
	if got := TrainProbeBranch(7, 2); got != "queue/7-2" {
		t.Fatalf("TrainProbeBranch = %q", got)
	}
	if !IsTrainBranch("queue/7-2") || IsTrainBranch("issue/7-queue") || IsTrainBranch("") {
		t.Fatal("IsTrainBranch misclassified a branch")
	}
}

// =============================================================================
// The planner
// =============================================================================

// TestMergeTrainObserve_FindsTheFirstRedChange drives the bisection against
// every culprit position of trains up to eight long, with a pipeline that is
// green exactly for the prefixes that stop before the culprit. Whatever the
// position, the planner must land the prefix before it, eject it, and spend no
// more probes than a bisection does.
func TestMergeTrainObserve_FindsTheFirstRedChange(t *testing.T) {
	for length := 1; length <= 8; length++ {
		for culprit := 0; culprit <= length; culprit++ {
			t.Run(strconv.Itoa(length)+"-cars-culprit-"+strconv.Itoa(culprit), func(t *testing.T) {
				train := planTrain(length)
				train.startProbe(length)
				var action trainAction
				probes := 1
				for {
					green := culprit == 0 || train.Current().Length < culprit
					action = train.observe(green)
					if action.kind != trainProbe {
						break
					}
					if action.probe <= train.Good || (train.Bad > 0 && action.probe >= train.Bad) {
						t.Fatalf("probe %d is not strictly between good=%d and bad=%d", action.probe, train.Good, train.Bad)
					}
					train.startProbe(action.probe)
					probes++
				}
				if action.kind != trainLand {
					t.Fatalf("action = %+v, want a landing", action)
				}
				if culprit == 0 {
					if action.land != length || action.eject != 0 || probes != 1 {
						t.Fatalf("a green train: action %+v after %d probes, want land=%d eject=0 in one", action, probes, length)
					}
					return
				}
				if action.land != culprit-1 || action.eject != culprit {
					t.Fatalf("culprit %d: action = %+v, want land=%d eject=%d", culprit, action, culprit-1, culprit)
				}
				if bound := bisectionBound(length); probes > bound {
					t.Fatalf("culprit %d of %d took %d probes, more than a bisection's %d", culprit, length, probes, bound)
				}
			})
		}
	}
}

// bisectionBound is how many probes finding one red change among n may take:
// the full train, then one per halving.
func bisectionBound(n int) int {
	probes := 1
	for span := n; span > 1; span = (span + 1) / 2 {
		probes++
	}
	return probes
}

// TestMergeTrainObserve_ARedSingleCarIsEjectedWithNothingLanded is the
// smallest red train: nothing was verified, so nothing lands.
func TestMergeTrainObserve_ARedSingleCarIsEjectedWithNothingLanded(t *testing.T) {
	train := planTrain(1)
	train.startProbe(1)
	action := train.observe(false)
	if action.kind != trainLand || action.land != 0 || action.eject != 1 {
		t.Fatalf("action = %+v, want land=0 eject=1", action)
	}
}

func TestMergeTrain_ProbeBranchesAndLookup(t *testing.T) {
	train := planTrain(3)
	train.startProbe(3)
	train.startProbe(1)
	if got := train.ProbeOf(3); got == nil || got.Branch != "queue/9" || got.HeadSHA != "commit-3" {
		t.Fatalf("full probe = %+v", got)
	}
	if got := train.ProbeOf(1); got == nil || got.Branch != "queue/9-1" || got.HeadSHA != "commit-1" {
		t.Fatalf("prefix probe = %+v", got)
	}
	if train.ProbeOf(2) != nil {
		t.Fatal("a prefix nobody tested has a probe")
	}
	if train.Current().Length != 1 {
		t.Fatal("the current probe is not the last started")
	}
	var none *MergeTrain
	if none.Current() != nil || none.ProbeOf(1) != nil {
		t.Fatal("a nil train answered a probe")
	}
}

// planTrain is a train of n cars whose commits are commit-1..commit-n.
func planTrain(n int) *MergeTrain {
	train := &MergeTrain{Number: 9, Target: "main", BaseSHA: "base", Phase: TrainTesting}
	for i := 1; i <= n; i++ {
		train.Cars = append(train.Cars, TrainCar{
			TaskID: "task-" + strconv.Itoa(i), PullRequest: 100 + i, Title: "change " + strconv.Itoa(i),
			HeadSHA: "head-" + strconv.Itoa(i), Commit: "commit-" + strconv.Itoa(i), Closes: []int{i},
		})
	}
	return train
}

// =============================================================================
// What the train writes
// =============================================================================

// TestTrainText_ClosesEveryIssueOnceAndNamesEveryHead is the contract the
// train's merge carries: it is what closes the issues, so every one must be
// named, and each change's approved head must be on the record.
func TestTrainText_ClosesEveryIssueOnceAndNamesEveryHead(t *testing.T) {
	train := planTrain(3)
	train.Cars[1].Closes = []int{2, 20}
	train.Cars[2].Closes = []int{20, 3}
	body := trainBody(train, 3)
	for _, want := range []string{"#101", "#102", "#103", "Closes #1", "Closes #2", "Closes #20", "Closes #3"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "Closes #20") != 1 {
		t.Fatalf("an issue two changes close is closed twice:\n%s", body)
	}
	message := trainCommitMessage(train.Cars[:2])
	if !strings.Contains(message, "change 1 (#101)") || strings.Contains(message, "#103") || strings.Contains(message, "Closes #3") {
		t.Fatalf("the commit message is not exactly the landed prefix:\n%s", message)
	}
	if got := trainTitle(train, 2); got != "Merge train #9 (first 2 of 3)" {
		t.Fatalf("prefix title = %q", got)
	}
	if got := trainTitle(train, 3); got != "Merge train #9" {
		t.Fatalf("full title = %q", got)
	}
	if !strings.Contains(body, "reviews this pull request as a whole") {
		t.Fatalf("the body does not say the train is reviewed before it lands:\n%s", body)
	}
}

// TestTrainRefusedComment_KeepsTheApprovalAndLinksTheReview proves what a
// change's pull request is told when the train carrying it is refused: which
// train, with which other changes, where the review is, that its own approval
// stands, how small the next train is, and what the reviewer found.
func TestTrainRefusedComment_KeepsTheApprovalAndLinksTheReview(t *testing.T) {
	train := planTrain(3)
	train.Land = 3
	trainPR := &ForgePullRequest{Number: 901, URL: "http://forge.test/acme/api/pulls/901"}
	comment := trainRefusedComment(train, trainPR, train.Cars[1], "the two caches disagree\non eviction", 2)
	for _, want := range []string{
		"Merge train #9", "#101 and #103", "#901 (http://forge.test/acme/api/pulls/901)",
		"own approval at `head-2` still stands", "at most 2 change(s)",
		"> the two caches disagree\n> on eviction", trainRefusedMarker + " train=9",
	} {
		if !strings.Contains(comment, want) {
			t.Fatalf("comment lacks %q:\n%s", want, comment)
		}
	}
	if strings.Contains(comment, "#102 ") {
		t.Fatalf("the comment names the change it is posted on as its own peer:\n%s", comment)
	}
	bare := trainRefusedComment(train, &ForgePullRequest{Number: 901}, train.Cars[0], "", 1)
	if strings.Contains(bare, "findings") || !strings.Contains(bare, "#901.") {
		t.Fatalf("a refusal with no findings and no URL rendered wrongly:\n%s", bare)
	}
}

// TestRefusedTrainSize_HalvesAndNeverReachesZero pins the bound a refused
// train's changes ride under next.
func TestRefusedTrainSize_HalvesAndNeverReachesZero(t *testing.T) {
	for refused, want := range map[int]int{0: 1, 1: 1, 2: 1, 3: 2, 4: 2, 5: 3, 25: 13} {
		if got := refusedTrainSize(refused); got != want {
			t.Fatalf("refusedTrainSize(%d) = %d, want %d", refused, got, want)
		}
	}
}

// TestCarPeers_NamesEveryOtherCar covers each shape of the list.
func TestCarPeers_NamesEveryOtherCar(t *testing.T) {
	cars := planTrain(4).Cars
	cases := []struct {
		length int
		except int
		want   string
	}{
		{1, 0, "nothing else"},
		{2, 0, "#102"},
		{3, 1, "#101 and #103"},
		{4, 2, "#101, #102 and #104"},
	}
	for _, tc := range cases {
		if got := carPeers(cars[:tc.length], cars[tc.except]); got != tc.want {
			t.Fatalf("carPeers(%d cars, except #%d) = %q, want %q", tc.length, cars[tc.except].PullRequest, got, tc.want)
		}
	}
}

func TestTrainEjectedComment_NamesTheReasonForEveryKind(t *testing.T) {
	for reason, description := range trainEjectDescriptions {
		comment := trainEjectedComment(TrainEjection{Reason: reason, Train: 4, Detail: "the detail", Findings: "what was found"})
		if !strings.Contains(comment, "merge train #4") || !strings.Contains(comment, description) || !strings.Contains(comment, "the detail") {
			t.Fatalf("%s: comment does not say what happened:\n%s", reason, comment)
		}
		// Only a reviewer's findings are written for a person; a red
		// pipeline's are log excerpts the worker reads.
		if shown := strings.Contains(comment, "> what was found"); shown != (reason == TrainEjectReviewRefused) {
			t.Fatalf("%s: findings shown = %v:\n%s", reason, shown, comment)
		}
	}
}

// =============================================================================
// Records
// =============================================================================

func TestTrainEntry_ValidateRefusesWhatATrainCannotCarry(t *testing.T) {
	valid := func() *TrainEntry {
		return &TrainEntry{ProjectID: testProject, TaskID: "task-1", PullRequest: 5, HeadSHA: "abc",
			Target: "main", Branch: "issue/5", State: TrainEntryQueued}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("a valid entry was refused: %v", err)
	}
	cases := map[string]func(*TrainEntry){
		"no pull request":           func(e *TrainEntry) { e.PullRequest = 0 },
		"no head":                   func(e *TrainEntry) { e.HeadSHA = " " },
		"no target":                 func(e *TrainEntry) { e.Target = "" },
		"no branch":                 func(e *TrainEntry) { e.Branch = "" },
		"unknown state":             func(e *TrainEntry) { e.State = "parked" },
		"ejected with no ejection":  func(e *TrainEntry) { e.State = TrainEntryEjected },
		"a task id with a slash":    func(e *TrainEntry) { e.TaskID = "a/b" },
		"a project id with a slash": func(e *TrainEntry) { e.ProjectID = "a/b" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			entry := valid()
			mutate(entry)
			if err := entry.Validate(); err == nil {
				t.Fatal("refused nothing")
			}
		})
	}
	var none *TrainEntry
	if err := none.Validate(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil entry: %v", err)
	}
}

// TestTrainStore_EntryWritesAreGuarded is the property the delivery and the
// train racing on one entry depend on: a write that lost is refused, not
// applied over the winner.
func TestTrainStore_EntryWritesAreGuarded(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	entry := &TrainEntry{ProjectID: testProject, TaskID: "task-1", PullRequest: 5, HeadSHA: "abc",
		Target: "main", Branch: "issue/5", State: TrainEntryQueued, EnqueuedAt: time.Now()}
	if err := store.SaveTrainEntry(ctx, testTenant, entry); err != nil {
		t.Fatalf("create: %v", err)
	}
	if entry.Version != 1 || entry.CreatedAt.IsZero() {
		t.Fatalf("created entry = version %d created %v", entry.Version, entry.CreatedAt)
	}
	stale := *entry
	entry.State = TrainEntryRiding
	entry.Train = 1
	if err := store.SaveTrainEntry(ctx, testTenant, entry); err != nil {
		t.Fatalf("update: %v", err)
	}
	stale.State = TrainEntryLanded
	stale.MergeSHA = "m"
	if err := store.SaveTrainEntry(ctx, testTenant, &stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a stale write was applied: %v", err)
	}
	fresh := &TrainEntry{ProjectID: testProject, TaskID: "task-1", PullRequest: 5, HeadSHA: "abc",
		Target: "main", Branch: "issue/5", State: TrainEntryQueued}
	if err := store.SaveTrainEntry(ctx, testTenant, fresh); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a create over an existing entry was applied: %v", err)
	}
	held, err := store.GetTrainEntry(ctx, testTenant, testProject, "task-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if held.State != TrainEntryRiding || held.Train != 1 {
		t.Fatalf("held = %+v", held)
	}
}

func TestTrainStore_ListIsOneProjectsAndDeleteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	for i, project := range []string{testProject, testProject, "other-project"} {
		entry := &TrainEntry{ProjectID: project, TaskID: "task-" + strconv.Itoa(i), PullRequest: 5 + i, HeadSHA: "abc",
			Target: "main", Branch: "b", State: TrainEntryQueued}
		if err := store.SaveTrainEntry(ctx, testTenant, entry); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.ListTrainEntries(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("listed %d entries, want the project's two", len(entries))
	}
	if err := store.DeleteTrainEntry(ctx, testTenant, testProject, "task-0"); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteTrainEntry(ctx, testTenant, testProject, "task-0"); err != nil {
		t.Fatalf("a second delete failed: %v", err)
	}
	if _, err := store.GetTrainEntry(ctx, testTenant, testProject, "task-0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted entry still read: %v", err)
	}
	if _, err := store.ListTrainEntries(ctx, testTenant, "bad/project"); err == nil {
		t.Fatal("a malformed project id was listed")
	}
}

func TestTrainStore_LedgerStartsEmptyAndIsGuarded(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	ledger, err := store.GetTrainLedger(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if ledger.Next != 1 || ledger.Active != nil || ledger.Version != 0 {
		t.Fatalf("a project that never trained has ledger %+v", ledger)
	}
	stale := *ledger
	ledger.Active = planTrain(1)
	ledger.Next = 2
	if err := store.SaveTrainLedger(ctx, testTenant, ledger); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if err := store.SaveTrainLedger(ctx, testTenant, &stale); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("a second job's stale ledger was written: %v", err)
	}
	held, err := store.GetTrainLedger(ctx, testTenant, testProject)
	if err != nil {
		t.Fatal(err)
	}
	if held.Next != 2 || held.Active == nil || held.Active.Number != 9 {
		t.Fatalf("held ledger = %+v", held)
	}
	if err := store.SaveTrainLedger(ctx, testTenant, &TrainLedger{ProjectID: "bad/id"}); err == nil {
		t.Fatal("a ledger with a malformed project was written")
	}
}

func TestOrderQueue_FirstEnqueuedFirstThenByNumber(t *testing.T) {
	at := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	entries := []*TrainEntry{
		{PullRequest: 9, EnqueuedAt: at.Add(time.Minute)},
		{PullRequest: 7, EnqueuedAt: at},
		{PullRequest: 3, EnqueuedAt: at},
	}
	orderQueue(entries)
	if entries[0].PullRequest != 3 || entries[1].PullRequest != 7 || entries[2].PullRequest != 9 {
		t.Fatalf("order = %d %d %d", entries[0].PullRequest, entries[1].PullRequest, entries[2].PullRequest)
	}
}

// trainTestReviewer is a self-reviewing project's reviewer.
func trainTestReviewer() ReviewerConfig {
	return ReviewerConfig{Agent: testReviewerAgent, Login: fakeReviewerLogin, TokenSecretRef: "reviewer-token"}
}
