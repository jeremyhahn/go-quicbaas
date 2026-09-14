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
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// IssueSource — labelled forge issues become Tasks
// =============================================================================
//
// The engine's task-source poller asks this source, per bound project, what
// work the forge holds. The answer is every OPEN issue carrying the
// binding's trigger label — the explicit per-issue hand-over — as a
// TaskSourceSpec whose provenance is the issue. Dedupe is not this source's
// concern: the task store's guarded source index refuses a second adoption
// of one (kind, ref), so the same spec may be returned on every sweep.

// The runner every task this source adopts is driven by: the module's own,
// never the engine's lead-driven one. It is declared on the spec so the
// runner reaches the Task record and a requeue re-dispatches under the same
// runner that first ran it.

// TaskSourceName is the source's registry name.
const TaskSourceName = "forge_issue"

// TaskSourceKindForgeIssue is the provenance kind of every task this source
// emits.
const TaskSourceKindForgeIssue orchestration.TaskSourceKind = "forge_issue"

// DefaultIssuePageLimit bounds one project sweep's issue listing.
const DefaultIssuePageLimit = 50

// Metadata keys stamped on every task the source emits.
const (
	MetadataIssueNumber = "issue_number"
	MetadataRepository  = "repository"
	MetadataForge       = "forge"
)

// IssueSource is the forge_issue TaskSource.
type IssueSource struct {
	bindings  BindingReader
	forges    ForgeResolver
	ranker    *Ranker
	readiness *ReadinessEvaluator
	live      LiveWorkReader
	pageLimit int
	now       func() time.Time
}

// NewIssueSource builds the source; pageLimit ≤ 0 takes the default.
//
// The readiness gate runs with no live-work reader until SetLiveWorkReader
// supplies one — see that method for why the predicate then answers "not owned"
// rather than refusing to run.
func NewIssueSource(bindings BindingReader, forges ForgeResolver, pageLimit int) (*IssueSource, error) {
	if bindings == nil {
		return nil, &InvalidInputError{Field: "bindings", Message: "the source reads the trigger label from the binding"}
	}
	if forges == nil {
		return nil, &InvalidInputError{Field: "forges", Message: "the source lists issues through the forge adapter"}
	}
	ranker, err := NewRanker(bindings)
	if err != nil {
		return nil, err
	}
	if pageLimit <= 0 {
		pageLimit = DefaultIssuePageLimit
	}
	return &IssueSource{
		bindings:  bindings,
		forges:    forges,
		ranker:    ranker,
		readiness: NewReadinessEvaluator(time.Now),
		pageLimit: pageLimit,
		now:       time.Now,
	}, nil
}

// SetLiveWorkReader supplies the reader the ownership predicate consults.
//
// SEPARATE FROM CONSTRUCTION BECAUSE THE LEASE STORE IS NOT BUILT YET. The
// source is registered while the module wires itself; the engine's job lease
// store is built later in the daemon's own start-up, and an ordering that
// required it here would mean registering the source after the poller that
// sweeps it.
//
// WITH NO READER THE PREDICATE PASSES, and that is a deliberate weakening rather
// than a silent one. The duplicate-adoption it guards against is already refused
// for the ordinary case by the task store's guarded source index — one task per
// (kind, ref) per project — so the predicate's unique contribution is the
// CROSS-PROJECT case. Refusing to poll at all without it would stop a deployment
// that has been adopting issues correctly for months.
// A NIL ARGUMENT IS IGNORED rather than clearing a reader already wired. The
// method's only caller is a boot path that tests a handle before calling it, so a
// nil arriving here is a bug upstream — and the cheapest reading of it, "the
// caller meant to disable the predicate", is precisely the one that silently
// weakens a safety check nobody then notices is off.
func (s *IssueSource) SetLiveWorkReader(live LiveWorkReader) {
	if live == nil {
		return
	}
	s.live = live
}

// SetClock replaces the source's clock. For tests that must age a lease or a
// check without waiting for it.
func (s *IssueSource) SetClock(now func() time.Time) {
	if now == nil {
		return
	}
	s.now = now
	s.readiness = NewReadinessEvaluator(now)
}

// Name implements orchestration.TaskSource.
func (s *IssueSource) Name() string { return TaskSourceName }

// Poll implements orchestration.TaskSource: every open, triggered issue of
// the project's bound repository, as a task spec. A project with no binding
// has no work here and answers nothing.
func (s *IssueSource) Poll(ctx context.Context, project *orchestration.Project) ([]orchestration.TaskSourceSpec, error) {
	if project == nil {
		return nil, &InvalidInputError{Field: "project", Message: "must not be nil"}
	}
	binding, err := s.bindings.GetBinding(ctx, project.TenantID, project.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	forge, err := s.forges.ForgeFor(ctx, project.TenantID, binding.Forge)
	if err != nil {
		return nil, err
	}
	if forge == nil {
		return nil, NewForgeNotRegisteredError(binding.Forge.Type)
	}
	repo := binding.Repo()
	issues, err := forge.ListIssues(ctx, repo, IssueFilter{
		State:  "open",
		Labels: []string{binding.Forge.TriggerLabel},
		Limit:  s.pageLimit,
	})
	if err != nil {
		return nil, unavailable(err)
	}
	inputs, err := s.ranker.Inputs(ctx, project.TenantID, binding)
	if err != nil {
		return nil, err
	}
	priority := inputs.Computed()

	// The fleet's live work is read ONCE per project sweep, not once per issue.
	// It is the same answer for every issue in the sweep, and a per-issue read
	// would be one lease listing per labelled issue on every tick.
	scope := ReadinessScope{Binding: binding, Forge: forge, Repo: repo}
	if s.live != nil {
		liveRefs, liveErr := s.live.LiveRefs(ctx, project.TenantID, s.now())
		if liveErr != nil {
			return nil, unavailable(liveErr)
		}
		scope.LiveRefs = liveRefs
	}

	specs := make([]orchestration.TaskSourceSpec, 0, len(issues))
	for i := range issues {
		issue := &issues[i]
		finding, readyErr := s.readiness.Evaluate(ctx, scope, issue)
		if readyErr != nil {
			return nil, unavailable(readyErr)
		}
		if !finding.Verdict.Adoptable() {
			continue
		}
		comments, commentsErr := forge.ListIssueComments(ctx, repo, issue.Number)
		if commentsErr != nil {
			return nil, unavailable(commentsErr)
		}
		contract, contractErr := IssueContract(IssueContractSpec{
			Repo: repo, IssueNumber: issue.Number, Title: issue.Title, Body: issue.Body, Comments: comments,
		})
		if contractErr != nil {
			return nil, contractErr
		}
		park, parkErr := issueSpecPark(finding)
		if parkErr != nil {
			return nil, parkErr
		}
		specs = append(specs, orchestration.TaskSourceSpec{
			Source: orchestration.TaskSourceRef{
				Kind: TaskSourceKindForgeIssue,
				Ref:  IssueRefString(repo, issue.Number),
				URL:  issue.URL,
			},
			Subject:     strings.TrimSpace(issue.Title),
			Description: contract,
			Priority:    priority,
			Runner:      JobRunnerName,
			Park:        park,
			Metadata: map[string]string{
				MetadataIssueNumber: strconv.Itoa(issue.Number),
				MetadataRepository:  repo.String(),
				MetadataForge:       string(binding.Forge.Type),
			},
		})
	}
	return specs, nil
}

// issueSpecPark renders a readiness finding as the park a spec carries, and nil
// for a finding that is ready to run.
//
// THE PARK RIDES THE SPEC so the task is created blocked in ONE write. The
// alternative — adopt, then block — leaves the task pending and auto-spawning
// for as long as the two writes take, which is exactly long enough for the
// dispatcher to start the job the park exists to prevent. See
// orchestration.CreateTaskRequest.Park.
func issueSpecPark(finding ReadinessFinding) (*orchestration.BlockReason, error) {
	if finding.Verdict == ReadinessAdopt {
		return nil, nil
	}
	reason, err := finding.ParkReason()
	if err != nil {
		return nil, err
	}
	return &orchestration.BlockReason{Kind: finding.ParkKind(), Message: reason}, nil
}

// unavailable classifies a forge failure so the poller reports the outage
// once rather than on every sweep.
func unavailable(err error) error {
	if errors.Is(err, ErrBackend) {
		return &orchestration.TaskSourceUnavailableError{Source: TaskSourceName, Cause: err}
	}
	return err
}

// issueTriggered reports whether the issue carries the trigger label,
// compared exactly and case-sensitively: labels are records, not prose.
func issueTriggered(binding *ProjectBinding, issue *ForgeIssue) bool {
	trigger := binding.Forge.TriggerLabel
	if trigger == "" {
		return false
	}
	for _, label := range issue.Labels {
		if label == trigger {
			return true
		}
	}
	return false
}

// issueClosed reports whether the forge says the issue is closed.
func issueClosed(issue *ForgeIssue) bool {
	return strings.EqualFold(issue.State, "closed")
}

// IssueRefString renders the provenance ref of an issue: owner/repo#number.
func IssueRefString(repo RepoRef, number int) string {
	return repo.String() + "#" + strconv.Itoa(number)
}

// ParseIssueRef recovers the repository and issue number from a provenance
// ref this source emitted.
func ParseIssueRef(ref string) (RepoRef, int, error) {
	path, num, ok := strings.Cut(ref, "#")
	if !ok {
		return RepoRef{}, 0, &InvalidInputError{Field: "source.ref", Message: "expected owner/repo#number, got " + strconv.Quote(ref)}
	}
	owner, repo, ok := strings.Cut(path, "/")
	if !ok || owner == "" || repo == "" {
		return RepoRef{}, 0, &InvalidInputError{Field: "source.ref", Message: "expected owner/repo#number, got " + strconv.Quote(ref)}
	}
	number, err := strconv.Atoi(num)
	if err != nil || number <= 0 {
		return RepoRef{}, 0, &InvalidInputError{Field: "source.ref", Message: "issue number in " + strconv.Quote(ref) + " is not a positive integer"}
	}
	return RepoRef{Owner: owner, Repo: repo}, number, nil
}

// issueOfTask recovers the issue a task was adopted from.
func issueOfTask(task *orchestration.Task) (RepoRef, int, error) {
	if task == nil || task.Source == nil {
		return RepoRef{}, 0, &TaskNotForgeIssueError{TaskID: taskID(task), Kind: ""}
	}
	if task.Source.Kind != TaskSourceKindForgeIssue {
		return RepoRef{}, 0, &TaskNotForgeIssueError{TaskID: task.ID, Kind: string(task.Source.Kind)}
	}
	return ParseIssueRef(task.Source.Ref)
}

func taskID(task *orchestration.Task) string {
	if task == nil {
		return ""
	}
	return task.ID
}
