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
	"sort"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// The merge train — approved changes verified together, landed together
// =============================================================================
//
// WHY IT EXISTS. A project whose target branch refuses an out-of-date pull
// request (Gitea's block_on_outdated_branch) makes every merge stale every
// other open pull request, and each of them then merges the target in and runs
// its whole pipeline again — about an hour a time. Merging one at a time makes
// n approved changes cost n pipelines AFTER they were already green.
//
// WHAT A TRAIN IS. Approved pull requests enter the project's queue instead of
// being merged. A train takes up to MaxSize of them in queue order, builds one
// branch — the target plus each change merged on top, in order — and CI runs
// ONCE on that branch through a pull request of its own. Green: the train's
// pull request lands through the forge's ordinary merge, so every rule of the
// target's protection is enforced on exactly the commit that lands, and each
// change's own pull request is closed naming where it landed. Red: the train
// is bisected over its prefixes to find the first change that turns it red;
// that change is EJECTED back to its worker with the failure, the verified
// prefix before it lands, and the rest go back to the queue.
//
// THE FORGE SEAM IS NOT WIDENED FOR IT. Everything above is branches, pull
// requests, check runs, commit statuses and a merge call — verbs every forge
// adapter already has — so the train behaves the same on every platform.
// ForgeCapabilities.NativeMergeQueue records which platforms could do this
// themselves, for a later change to use.
//
// THIS FILE IS THE MODEL AND THE PLANNER: the records the train persists and
// the pure function that decides what a probe's verdict means. The runner
// (merge_train_runner.go) carries the decisions out; nothing here talks to a
// forge or a container.

// DefaultMergeTrainMaxSize is how many changes one train carries when the
// binding says nothing.
//
// FIVE, because a red train is bisected: five cars cost at most three more
// pipelines to find the culprit, and a train much longer than that spends
// more finding one bad change than the batching saved.
const DefaultMergeTrainMaxSize = 5

// MaxMergeTrainSize bounds what a binding may configure.
const MaxMergeTrainSize = 25

// MergeTrainPolicy is a project's merge-train configuration.
//
// ON BY DEFAULT, which is why the switch is Disabled rather than Enabled: the
// zero value a binding written before trains existed carries is the default,
// and the default is the train.
type MergeTrainPolicy struct {
	// Disabled turns the train off; approved pull requests are then merged
	// one at a time, as before trains existed.
	Disabled bool `json:"disabled,omitempty"`

	// MaxSize bounds how many changes one train carries. Zero takes
	// DefaultMergeTrainMaxSize.
	MaxSize int `json:"max_size,omitempty"`
}

// Size is the configured bound or the default.
func (p MergeTrainPolicy) Size() int {
	if p.MaxSize <= 0 {
		return DefaultMergeTrainMaxSize
	}
	return p.MaxSize
}

// Validate refuses a bound outside 0..MaxMergeTrainSize.
func (p MergeTrainPolicy) Validate() error {
	if p.MaxSize < 0 || p.MaxSize > MaxMergeTrainSize {
		return &InvalidInputError{
			Field:   "merge_train.max_size",
			Message: "must be between 1 and " + strconv.Itoa(MaxMergeTrainSize) + ", or zero for the default",
		}
	}
	return nil
}

// TrainEnabled reports whether this project's approved pull requests land
// through the merge train.
func (b *ProjectBinding) TrainEnabled() bool {
	return b != nil && b.TrainOffReason() == ""
}

// TrainOffReason says why this project's approved pull requests merge one at
// a time, and is empty when they ride the train.
//
// A PROJECT WITH NO REVIEWER PROFILE THAT COUNTS TOWARD MERGE IS NOT TRAINED.
// The train's pull request carries the union of changes that were each
// approved at their own heads, and it lands only on a genuine verdict of its
// own, on its own head, from a reviewer whose verdict the target's protection
// counts (see TrainReviewer). A project reviewed by people, or only by
// advisory profiles, has no such reviewer to ask, so its changes merge one at
// a time exactly as they did before trains existed.
func (b *ProjectBinding) TrainOffReason() string {
	switch {
	case b == nil:
		return "the project is not bound"
	case b.MergeTrain.Disabled:
		return "the project's merge train is turned off"
	}
	if _, ok := b.TrainReviewer(); !ok {
		return "the project has no reviewer profile that counts toward merge, and a merge train's pull request is reviewed by one"
	}
	return ""
}

// TrainReviewer is the reviewer profile that reviews this project's merge
// trains: the first profile, in binding order, whose verdict counts toward
// merge. The default profile comes first, so a project whose default counts —
// the ordinary case — has its trains reviewed by the same reviewer, and
// dispatched by the same floor, as every other pull request it opens.
func (b *ProjectBinding) TrainReviewer() (ReviewerProfile, bool) {
	if b == nil {
		return ReviewerProfile{}, false
	}
	for _, profile := range b.ReviewerProfiles() {
		if profile.Counts {
			return profile, true
		}
	}
	return ReviewerProfile{}, false
}

// TrainBranchPrefix names every branch a train builds. The review runner
// reviews a pull request from one only while its train is waiting on that
// verdict (see IsTrainBranch and the review runner's skipDecision): a prefix
// the train is still bisecting will never land, and reviewing it would be a
// bill for nothing.
const TrainBranchPrefix = "queue/"

// IsTrainBranch reports whether a branch is one a merge train built.
func IsTrainBranch(branch string) bool {
	return strings.HasPrefix(strings.TrimSpace(branch), TrainBranchPrefix)
}

// TrainBranch names train n's branch.
func TrainBranch(number int) string { return TrainBranchPrefix + strconv.Itoa(number) }

// TrainProbeBranch names the branch that tests the first length changes of
// train n. The full train is tested on TrainBranch itself.
func TrainProbeBranch(number, length int) string {
	return TrainBranch(number) + "-" + strconv.Itoa(length)
}

// =============================================================================
// Queue entries
// =============================================================================

// TrainEntryState is where one approved pull request stands in its project's
// queue.
type TrainEntryState string

const (
	// TrainEntryQueued is waiting for a train.
	TrainEntryQueued TrainEntryState = "queued"

	// TrainEntryRiding is in the train that is building, testing or landing.
	TrainEntryRiding TrainEntryState = "riding"

	// TrainEntryLanded reached the target inside a train.
	TrainEntryLanded TrainEntryState = "landed"

	// TrainEntryEjected was put out of a train; Ejection says why.
	TrainEntryEjected TrainEntryState = "ejected"
)

// TrainEjectReason says why a change was put out of a train.
type TrainEjectReason string

const (
	// TrainEjectChecksFailed is a change whose addition turned the train red.
	TrainEjectChecksFailed TrainEjectReason = "checks_failed"

	// TrainEjectConflict is a change that no longer merges onto the train.
	TrainEjectConflict TrainEjectReason = "conflict"

	// TrainEjectHeadMoved is a change whose pull request moved past the head
	// that was approved and queued.
	TrainEjectHeadMoved TrainEjectReason = "head_moved"

	// TrainEjectClosed is a change whose pull request is no longer open.
	TrainEjectClosed TrainEjectReason = "closed"

	// TrainEjectUnreviewed is a change whose head no longer carries the
	// reviewer's approval when the train is about to land.
	TrainEjectUnreviewed TrainEjectReason = "unreviewed"

	// TrainEjectReviewRefused is the one change of a train whose reviewer
	// requested changes on it: the train carried that change alone onto the
	// target, so the findings can be about nothing else.
	TrainEjectReviewRefused TrainEjectReason = "review_refused"

	// TrainEjectUnsigned is a change carrying a commit no key the project
	// trusts signed. The factory never rewrites a pushed branch to re-sign
	// it, so the change waits for a person rather than for another train.
	TrainEjectUnsigned TrainEjectReason = "unsigned"
)

// TrainEjection is the account of one ejection, for the worker who owns the
// change and for the person reading its pull request.
type TrainEjection struct {
	Reason TrainEjectReason `json:"reason"`

	// Train is the train the change was ejected from.
	Train int `json:"train"`

	// Detail is one line a person reads beside the reason.
	Detail string `json:"detail,omitempty"`

	// Findings is what the worker is told: on a checks_failed ejection the
	// red pipeline of the smallest prefix of the train that includes this
	// change, rendered as the gate renders its own red; on a review_refused
	// one the findings the train's reviewer wrote.
	Findings string `json:"findings,omitempty"`
}

// TrainEntry is one approved pull request in its project's queue.
//
// IT IS KEYED BY THE TASK, because a task has one delivery and one pull
// request at a time: a change re-approved on a new head replaces its entry
// rather than queueing twice.
type TrainEntry struct {
	// ID is the DAO entity id, derived from ProjectID and TaskID.
	ID uint64 `json:"-"`

	ProjectID string `json:"project_id"`
	TaskID    string `json:"task_id"`

	// PullRequest, Title and Branch identify the change's own pull request.
	PullRequest int    `json:"pull_request"`
	Title       string `json:"title"`
	Branch      string `json:"branch"`

	// Target is the branch the change lands on. A train carries changes for
	// one target only.
	Target string `json:"target"`

	// HeadSHA is the head the review gate approved. The train merges exactly
	// this commit and ejects the change if its pull request has moved.
	HeadSHA string `json:"head_sha"`

	// Closes are the issues the change closes: its own, and every issue
	// clustered into its task.
	Closes []int `json:"closes,omitempty"`

	State TrainEntryState `json:"state"`

	// Train is the train the entry rides or rode, zero while it waits.
	Train int `json:"train,omitempty"`

	// MaxTrain bounds the next train this change rides, zero for no bound
	// beyond the project's. It is set when a train carrying the change was
	// refused by its reviewer: the change keeps its own approval and its place
	// in the queue, and the next train is half the size, so a refused
	// combination is judged again in smaller parts rather than as the same
	// union. A new head replaces the entry, and the bound with it.
	MaxTrain int `json:"max_train,omitempty"`

	// MergeSHA and TrainPullRequest record where a landed change landed: the
	// commit on the target, and the train pull request that carried it.
	MergeSHA         string `json:"merge_sha,omitempty"`
	TrainPullRequest int    `json:"train_pull_request,omitempty"`

	// Ejection is set on an ejected entry.
	Ejection *TrainEjection `json:"ejection,omitempty"`

	// EnqueuedAt orders the queue.
	EnqueuedAt time.Time `json:"enqueued_at"`

	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID implements the DAO entity contract.
func (e *TrainEntry) EntityID() uint64 { return e.ID }

// SetEntityID implements the DAO entity contract.
func (e *TrainEntry) SetEntityID(id uint64) { e.ID = id }

// GetVersion implements the DAO's Versioned contract.
func (e *TrainEntry) GetVersion() int64 { return e.Version }

// SetVersion implements the DAO's Versioned contract.
func (e *TrainEntry) SetVersion(version int64) { e.Version = version }

// Validate refuses an entry the train could not carry.
func (e *TrainEntry) Validate() error {
	if e == nil {
		return &InvalidInputError{Field: "entry", Message: "must not be nil"}
	}
	if err := validateKeySegment("project_id", e.ProjectID); err != nil {
		return err
	}
	if err := validateKeySegment("task_id", e.TaskID); err != nil {
		return err
	}
	switch {
	case e.PullRequest <= 0:
		return &InvalidInputError{Field: "entry.pull_request", Message: "a queued change is an open pull request"}
	case strings.TrimSpace(e.HeadSHA) == "":
		return &InvalidInputError{Field: "entry.head_sha", Message: "a queued change is the head that was approved"}
	case strings.TrimSpace(e.Target) == "":
		return &InvalidInputError{Field: "entry.target", Message: "a queued change names the branch it lands on"}
	case strings.TrimSpace(e.Branch) == "":
		return &InvalidInputError{Field: "entry.branch", Message: "a queued change names the branch it is offered from"}
	}
	if _, ok := trainEntryStates[e.State]; !ok {
		return &InvalidInputError{Field: "entry.state", Message: "unknown state " + strconv.Quote(string(e.State))}
	}
	if e.State == TrainEntryEjected && e.Ejection == nil {
		return &InvalidInputError{Field: "entry.ejection", Message: "an ejected change says why it was ejected"}
	}
	return nil
}

// trainEntryStates is the closed set of entry states.
var trainEntryStates = map[TrainEntryState]struct{}{
	TrainEntryQueued: {}, TrainEntryRiding: {}, TrainEntryLanded: {}, TrainEntryEjected: {},
}

// trainEntryKey is the identity an entry's entity id derives from.
func trainEntryKey(projectID, taskID string) string { return projectID + "\x00" + taskID }

// orderQueue sorts entries into queue order: first enqueued first, ties broken
// by pull request number so the order is total and every node agrees on it.
func orderQueue(entries []*TrainEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if !entries[i].EnqueuedAt.Equal(entries[j].EnqueuedAt) {
			return entries[i].EnqueuedAt.Before(entries[j].EnqueuedAt)
		}
		return entries[i].PullRequest < entries[j].PullRequest
	})
}

// =============================================================================
// Trains
// =============================================================================

// TrainPhase is where one train stands.
type TrainPhase string

const (
	// TrainBuilding is a train whose branch is being built in a container.
	TrainBuilding TrainPhase = "building"

	// TrainTesting is a train waiting on CI for one of its prefixes.
	TrainTesting TrainPhase = "testing"

	// TrainReviewing is a train whose verified prefix is waiting on the
	// project's reviewer: its pipeline is green, and it lands only on a
	// genuine verdict on its own head.
	TrainReviewing TrainPhase = "reviewing"

	// TrainLanding is a train whose verified prefix is being landed.
	TrainLanding TrainPhase = "landing"
)

// TrainCar is one change riding a train.
type TrainCar struct {
	TaskID      string `json:"task_id"`
	PullRequest int    `json:"pull_request"`
	Title       string `json:"title"`
	Branch      string `json:"branch"`
	HeadSHA     string `json:"head_sha"`
	Closes      []int  `json:"closes,omitempty"`

	// Commit is the train's commit once this change is merged on top of every
	// car before it: the head of the prefix that ends here.
	Commit string `json:"commit,omitempty"`
}

// TrainProbe is one prefix of the train under test.
type TrainProbe struct {
	// Length is how many cars the prefix carries.
	Length int `json:"length"`

	// Branch and PullRequest are where its pipeline runs: a pull request is
	// what the target's CI runs on.
	Branch      string `json:"branch,omitempty"`
	PullRequest int    `json:"pull_request,omitempty"`

	// HeadSHA is the prefix's commit — Cars[Length-1].Commit.
	HeadSHA string `json:"head_sha,omitempty"`

	// Reruns counts the void reds this probe re-ran, and RerunAt is when the
	// last was asked for: a red that finished before it is the run being
	// replaced, not an answer.
	Reruns  int       `json:"reruns,omitempty"`
	RerunAt time.Time `json:"rerun_at,omitempty"`

	// Verdict is "green" or "red" once the pipeline settled, empty before.
	Verdict string `json:"verdict,omitempty"`

	// ReviewRequestedAt is when the train last asked the project's reviewer
	// for a verdict on this prefix, zero before it did. ReviewJobID is the
	// review job that ask started, and ReviewAsks counts the asks the engine
	// accepted for this head. Together they keep a train resumed while it
	// waits from asking again, and let it ask again — bounded by
	// MaxTrainReviewAsks — when that job ended with no verdict.
	ReviewRequestedAt time.Time `json:"review_requested_at,omitempty"`
	ReviewJobID       string    `json:"review_job_id,omitempty"`
	ReviewAsks        int       `json:"review_asks,omitempty"`

	// EnvironmentSuspicion is set on a red that looked like the environment's
	// (see environmentalRed) but was not retried — the train's one retry was
	// spent, or the forge cannot re-run — so its ejection says why.
	EnvironmentSuspicion string `json:"environment_suspicion,omitempty"`
}

// MaxTrainReviewAsks bounds how many review jobs a train starts for one head:
// the first ask and the re-asks after jobs that ended with no verdict. A
// train must never wait for ever on a review that is not coming, and a
// reviewer that fails three times on one head is failing on something a
// fourth job will not fix.
const MaxTrainReviewAsks = 3

// Probe verdicts.
const (
	probeGreen = "green"
	probeRed   = "red"
)

// MergeTrain is one train.
type MergeTrain struct {
	Number int        `json:"number"`
	Target string     `json:"target"`
	Phase  TrainPhase `json:"phase"`

	// BaseSHA is the target commit the train was built on. The train lands
	// only while the target is still there.
	BaseSHA string `json:"base_sha,omitempty"`

	Cars []TrainCar `json:"cars"`

	// Good is the longest prefix known green, zero when none is. Bad is the
	// shortest prefix known red, zero when none is. The culprit is found when
	// Bad == Good+1.
	Good int `json:"good,omitempty"`
	Bad  int `json:"bad,omitempty"`

	// BadFindings is the red pipeline of the Bad prefix, which is what the
	// culprit is ejected with.
	BadFindings string `json:"bad_findings,omitempty"`

	// Probes are every prefix tested, the current one last.
	Probes []TrainProbe `json:"probes,omitempty"`

	// Land and Eject are the planner's decision once the pipeline has
	// answered: the verified prefix that lands, and the 1-based car ejected
	// after it (zero for none). They are recorded before the review and the
	// landing start, so a train interrupted in either finishes the same
	// decision.
	Land  int `json:"land,omitempty"`
	Eject int `json:"eject,omitempty"`

	// EnvironmentRetried is set once the train re-ran a red that looked like
	// the environment's rather than its cars' (merge_train_environment.go),
	// and EnvironmentSuspicion is why it did. A train retries so once; a
	// second red ejects as any red does.
	EnvironmentRetried   bool   `json:"environment_retried,omitempty"`
	EnvironmentSuspicion string `json:"environment_suspicion,omitempty"`

	StartedAt time.Time `json:"started_at"`
}

// Current is the probe under test, nil before the first.
func (t *MergeTrain) Current() *TrainProbe {
	if t == nil || len(t.Probes) == 0 {
		return nil
	}
	return &t.Probes[len(t.Probes)-1]
}

// ProbeOf returns the probe that tested the first length cars, nil when none
// did.
func (t *MergeTrain) ProbeOf(length int) *TrainProbe {
	if t == nil {
		return nil
	}
	for i := range t.Probes {
		if t.Probes[i].Length == length {
			return &t.Probes[i]
		}
	}
	return nil
}

// startProbe begins testing the first length cars.
func (t *MergeTrain) startProbe(length int) {
	probe := TrainProbe{Length: length, HeadSHA: t.Cars[length-1].Commit}
	if length == len(t.Cars) {
		probe.Branch = TrainBranch(t.Number)
	} else {
		probe.Branch = TrainProbeBranch(t.Number, length)
	}
	t.Probes = append(t.Probes, probe)
}

// trainActionKind is what the planner decides next.
type trainActionKind int

const (
	// trainProbe tests another prefix.
	trainProbe trainActionKind = iota + 1

	// trainLand lands a verified prefix, ejecting at most one car after it.
	trainLand
)

// trainAction is the planner's decision.
type trainAction struct {
	kind trainActionKind

	// probe is the prefix length to test next (trainProbe).
	probe int

	// land is the verified prefix length to land (trainLand); zero lands
	// nothing.
	land int

	// eject is the 1-based car to eject (trainLand); zero ejects nothing.
	eject int
}

// observe records the current probe's settled verdict and decides what the
// train does next.
//
// IT IS A BISECTION OVER PREFIXES, and every prefix it lands was verified. A
// green prefix raises Good; a red one lowers Bad. While the two are more than
// one apart the midpoint is tested — it lies strictly between them, so every
// probe teaches something. When they meet, car Bad is the first change whose
// addition turned the train red: it is ejected, the Good prefix (which CI
// verified on this exact commit) lands, and the cars after Bad — never
// verified without it — go back to the queue.
//
// A FULLY GREEN TRAIN IS THE COMMON CASE and needs one probe: Bad is zero, so
// the first green lands everything.
func (t *MergeTrain) observe(green bool) trainAction {
	probe := t.Current()
	if green {
		probe.Verdict = probeGreen
		if probe.Length > t.Good {
			t.Good = probe.Length
		}
		if t.Bad == 0 {
			return trainAction{kind: trainLand, land: t.Good}
		}
	} else {
		probe.Verdict = probeRed
		if t.Bad == 0 || probe.Length < t.Bad {
			t.Bad = probe.Length
		}
	}
	if t.Bad == t.Good+1 {
		return trainAction{kind: trainLand, land: t.Good, eject: t.Bad}
	}
	return trainAction{kind: trainProbe, probe: t.Good + (t.Bad-t.Good)/2}
}

// TrainLedger is a project's train record: the next train number and the
// train in flight.
//
// ONE RECORD PER PROJECT, written under a version guard, which is what keeps
// two nodes from running two trains for one project even if both took the
// job: the second write of a train loses.
type TrainLedger struct {
	// ID is the DAO entity id, derived from ProjectID.
	ID uint64 `json:"-"`

	ProjectID string `json:"project_id"`

	// Next is the number the next train takes. Numbers are never reused, so a
	// train's branch name is never pushed twice.
	Next int `json:"next"`

	// Active is the train in flight, nil between trains.
	Active *MergeTrain `json:"active,omitempty"`

	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID implements the DAO entity contract.
func (l *TrainLedger) EntityID() uint64 { return l.ID }

// SetEntityID implements the DAO entity contract.
func (l *TrainLedger) SetEntityID(id uint64) { l.ID = id }

// GetVersion implements the DAO's Versioned contract.
func (l *TrainLedger) GetVersion() int64 { return l.Version }

// SetVersion implements the DAO's Versioned contract.
func (l *TrainLedger) SetVersion(version int64) { l.Version = version }

// Validate refuses a ledger with no project.
func (l *TrainLedger) Validate() error {
	if l == nil {
		return &InvalidInputError{Field: "ledger", Message: "must not be nil"}
	}
	return validateKeySegment("project_id", l.ProjectID)
}

// trainLedgerKey is the identity a ledger's entity id derives from.
func trainLedgerKey(projectID string) string { return "merge-train\x00" + projectID }

// =============================================================================
// What the train writes for people to read
// =============================================================================

// trainTitle is the train pull request's title.
func trainTitle(t *MergeTrain, length int) string {
	title := "Merge train #" + strconv.Itoa(t.Number)
	if length < len(t.Cars) {
		title += " (first " + strconv.Itoa(length) + " of " + strconv.Itoa(len(t.Cars)) + ")"
	}
	return title
}

// trainBody is the train pull request's body: which changes it carries, each
// at the head that was approved, and the issues it closes.
//
// THE CLOSING LINES ARE THE CONTRACT. The train's merge is what puts every
// change on the target, so it is the train that closes their issues, exactly
// as each change's own merge would have.
func trainBody(t *MergeTrain, length int) string {
	var b strings.Builder
	b.WriteString("Merge train #" + strconv.Itoa(t.Number) + " onto `" + t.Target + "`, built by the Software Factory.\n\n")
	b.WriteString("Each change below was approved by the project's reviewer at the head named, and is merged here on top of every change before it. " +
		"The project's reviewer reviews this pull request as a whole before it lands:\n\n")
	for _, car := range t.Cars[:length] {
		b.WriteString("- #" + strconv.Itoa(car.PullRequest) + " " + strings.TrimSpace(car.Title) +
			" at `" + shortSHA(car.HeadSHA) + "`\n")
	}
	if closes := trainCloses(t.Cars[:length]); len(closes) > 0 {
		b.WriteString("\n")
		for _, issue := range closes {
			b.WriteString(closesLine(issue) + "\n")
		}
	}
	return b.String()
}

// trainCommitMessage is the message the train lands under: every change's
// title, and every issue it closes.
func trainCommitMessage(cars []TrainCar) string {
	var b strings.Builder
	for _, car := range cars {
		b.WriteString("* " + strings.TrimSpace(car.Title) + " (#" + strconv.Itoa(car.PullRequest) + ")\n")
	}
	if closes := trainCloses(cars); len(closes) > 0 {
		b.WriteString("\n")
		for _, issue := range closes {
			b.WriteString(closesLine(issue) + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// trainCloses is every issue the cars close, in order, each once.
func trainCloses(cars []TrainCar) []int {
	seen := map[int]bool{}
	var closes []int
	for _, car := range cars {
		for _, issue := range car.Closes {
			if issue > 0 && !seen[issue] {
				seen[issue] = true
				closes = append(closes, issue)
			}
		}
	}
	return closes
}

// trainLandedMarker tags the comment a landed change's pull request is closed
// with, so a Complete that is retried finds its own comment rather than
// posting a second one.
const trainLandedMarker = "quicbaas-sdlc:train-landed"

// trainLandedComment is posted on a change's own pull request when the train
// carrying it lands.
func trainLandedComment(entry *TrainEntry) string {
	return "Landed on `" + entry.Target + "` in " + shortSHA(entry.MergeSHA) + " by merge train #" +
		strconv.Itoa(entry.Train) + " (#" + strconv.Itoa(entry.TrainPullRequest) + "). This pull request is " +
		"closed rather than merged because its change reached the target inside the train.\n" +
		"<!-- " + trainLandedMarker + " train=" + strconv.Itoa(entry.Train) + " -->\n"
}

// trainEjectedComment is posted on a change's own pull request when a train
// ejects it.
func trainEjectedComment(ejection TrainEjection) string {
	var b strings.Builder
	b.WriteString("Ejected from merge train #" + strconv.Itoa(ejection.Train) + ": " + trainEjectDescriptions[ejection.Reason] + ".")
	if detail := strings.TrimSpace(ejection.Detail); detail != "" {
		b.WriteString("\n\n" + detail)
	}
	if findings := strings.TrimSpace(ejection.Findings); findings != "" && trainEjectFindingsShown[ejection.Reason] {
		b.WriteString("\n\nThe reviewer's findings:\n\n" + quoteReason(truncateForPrompt(findings, maxTrainFindingsBytes)))
	}
	b.WriteString("\n\n" + trainEjectNextStep(ejection.Reason) + "\n")
	return b.String()
}

// trainEjectNextSteps say what happens to an ejected change when it is not
// the usual trip back to its worker.
var trainEjectNextSteps = map[TrainEjectReason]string{
	TrainEjectUnsigned: "The change is held for a person: its commit must be signed by a key the project trusts, " +
		"or the key that signed it recorded in the project's signing history. It rejoins the queue once it is " +
		"readmitted.",
}

// trainEjectNextStep is the closing line of an ejection comment.
func trainEjectNextStep(reason TrainEjectReason) string {
	if step, ok := trainEjectNextSteps[reason]; ok {
		return step
	}
	return "The factory sends the change back to its worker; it rejoins the queue once it is approved again."
}

// trainEjectFindingsShown are the ejection reasons whose findings are written
// for a person and so are quoted on the pull request as well as handed to the
// worker. A red pipeline's findings are log excerpts, which the worker reads
// and the pull request's own checks already link to.
var trainEjectFindingsShown = map[TrainEjectReason]bool{TrainEjectReviewRefused: true}

// maxTrainFindingsBytes bounds the review findings one comment quotes. The
// train pull request the comment links to carries them whole.
const maxTrainFindingsBytes = 8000

// trainRefusedMarker tags the comment a change's pull request gets when the
// train carrying it was refused by its reviewer, so a refusal resumed after
// the comment does not post it twice.
const trainRefusedMarker = "quicbaas-sdlc:train-refused"

// trainRefusedComment is posted on the pull request of every change a refused
// train carried and sent back to the queue.
//
// IT SAYS WHAT STILL STANDS as plainly as what was found: the change's own
// approval, at its own head, is untouched, and it is the COMBINATION the
// reviewer refused. The link is to the train's pull request, where the review
// was posted on the head it judged.
func trainRefusedComment(t *MergeTrain, trainPR *ForgePullRequest, car TrainCar, findings string, maxTrain int) string {
	var b strings.Builder
	b.WriteString("Merge train #" + strconv.Itoa(t.Number) + " carried this change onto `" + t.Target + "` together with " +
		carPeers(t.Cars[:t.Land], car) + ", and the project's reviewer requested changes on the combination: " +
		trainLink(trainPR) + ".\n\n")
	b.WriteString("This change's own approval at `" + shortSHA(car.HeadSHA) + "` still stands, and it is back in the merge queue " +
		"in its place. The next train that carries it holds at most " + strconv.Itoa(maxTrain) +
		" change(s), so the combination is judged again in smaller parts.\n")
	if findings = strings.TrimSpace(findings); findings != "" {
		b.WriteString("\nThe reviewer's findings on the train:\n\n" + quoteReason(truncateForPrompt(findings, maxTrainFindingsBytes)))
	}
	b.WriteString("<!-- " + trainRefusedMarker + " train=" + strconv.Itoa(t.Number) + " -->\n")
	return b.String()
}

// trainUnreviewedMarker tags the comment a change's pull request gets when
// the train carrying it was abandoned because its review never produced a
// verdict.
const trainUnreviewedMarker = "quicbaas-sdlc:train-unreviewed"

// trainUnreviewedComment is posted on the pull request of every change a train
// carried when the train's review produced no verdict after every ask.
func trainUnreviewedComment(t *MergeTrain, trainPR *ForgePullRequest, car TrainCar, asks int) string {
	return "Merge train #" + strconv.Itoa(t.Number) + " carried this change onto `" + t.Target + "`, and its review " +
		"never produced a verdict: " + strconv.Itoa(asks) + " review job(s) of " + trainLink(trainPR) +
		" ended without approving or requesting changes. Nothing was found wrong with this change.\n\n" +
		"Its own approval at `" + shortSHA(car.HeadSHA) + "` still stands, and it is back in the merge queue in its place.\n" +
		"<!-- " + trainUnreviewedMarker + " train=" + strconv.Itoa(t.Number) + " -->\n"
}

// carPeers names every car but one, as "#101 and #103".
func carPeers(cars []TrainCar, except TrainCar) string {
	peers := make([]string, 0, len(cars))
	for _, car := range cars {
		if car.PullRequest != except.PullRequest {
			peers = append(peers, "#"+strconv.Itoa(car.PullRequest))
		}
	}
	switch len(peers) {
	case 0:
		return "nothing else"
	case 1:
		return peers[0]
	default:
		return strings.Join(peers[:len(peers)-1], ", ") + " and " + peers[len(peers)-1]
	}
}

// trainLink names a train's pull request, with its address when the forge
// gave one.
func trainLink(pr *ForgePullRequest) string {
	link := "#" + strconv.Itoa(pr.Number)
	if url := strings.TrimSpace(pr.URL); url != "" {
		link += " (" + url + ")"
	}
	return link
}

// refusedTrainSize is the bound a refused train's changes ride the next train
// under: half the refused combination, rounded up, so the two halves are each
// judged on their own.
func refusedTrainSize(refused int) int {
	return max(1, (refused+1)/2)
}

// trainEjectDescriptions are the one-line accounts of each ejection reason.
var trainEjectDescriptions = map[TrainEjectReason]string{
	TrainEjectChecksFailed: "the train's pipeline went red when this change was added on top of the changes before it",
	TrainEjectConflict:     "this change no longer merges onto the target and the changes queued before it",
	TrainEjectHeadMoved:    "the pull request moved past the head that was approved",
	TrainEjectClosed:       "the pull request is no longer open",
	TrainEjectUnreviewed:   "the head no longer carries the reviewer's approval",
	TrainEjectReviewRefused: "the train carried this change alone onto the target, and the project's reviewer " +
		"requested changes on it",
	TrainEjectUnsigned: "the change carries a commit no key this project trusts signed, and the factory never " +
		"rewrites a pushed branch to re-sign it",
}
