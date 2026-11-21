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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// attentionRulingActor is the identity every owner ruling is posted under —
// a bot identity, never the operator's own name, so a forge issue's history
// reads "the factory recorded an owner ruling" the same way regardless of
// who happened to be signed in when they gave it.
const attentionRulingActor = "Owner ruling (via QuicBaaS)"

// AttentionTaskService is the attention inbox's whole view of the task
// engine: it pages blocked tasks and drives the three verbs a resolution may
// call. orchestration.AgentService satisfies it.
type AttentionTaskService interface {
	GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error)
	ListTasksPage(ctx context.Context, tenantID string, filter *orchestration.TaskFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.Task], error)
	ReadmitTask(ctx context.Context, tenantID, taskID string) error
	CloseBlockedTask(ctx context.Context, tenantID, taskID string, result *orchestration.TaskResult) error
	BumpTaskReviewRoundBudget(ctx context.Context, tenantID, taskID string) (int, error)
}

var _ AttentionTaskService = (orchestration.AgentService)(nil)

// AttentionFleetReader lists fleet nodes, for the agent_offline alert.
// orchestration.AgentService satisfies it.
type AttentionFleetReader interface {
	ListFleetNodesPage(ctx context.Context, tenantID string, filter *orchestration.FleetNodeFilter, page *orchestration.Page) (*orchestration.PageResult[*orchestration.FleetNode], error)
}

var _ AttentionFleetReader = (orchestration.AgentService)(nil)

// AttentionAccountReader lists pooled provider accounts, for the
// account_error ("pool credential stale") alert. orchestration.AccountStore
// satisfies it.
type AttentionAccountReader interface {
	ListByTenant(ctx context.Context, tenantID string, provider orchestration.AccountProvider, projectID string) ([]*orchestration.Account, error)
}

// AttentionBarrierStatus is what AttentionBarrierReader answers: whether the
// tenant has a barrier of its own at all, whether it is sealed, and its scope
// name (the tenant id, for the item's id).
//
// INITIALIZED IS PART OF THE ANSWER because qrdb reports a tenant with no
// per-tenant barrier as Sealed=true, Initialized=false: absence is not an
// error there. Such a tenant's data runs under the system barrier, so its
// "sealed" says nothing about whether anything can run.
type AttentionBarrierStatus struct {
	Scope       string
	Initialized bool
	Sealed      bool
}

// AttentionBarrierReader reports the CALLING TENANT's own barrier state, for
// the barrier_sealed alert — never the system barrier: a sealed system
// barrier takes the whole API down, so the inbox could not render an item
// for it even if it tried (owner ruling, go-quicbaas#1020). Left unwired,
// the alert simply never fires — an honest degrade, not a missing feature,
// until a caller has a concrete tenant barrier status source to hand it (see
// docs/software-factory).
type AttentionBarrierReader interface {
	BarrierStatus(ctx context.Context, tenantID string) (AttentionBarrierStatus, error)
}

// AttentionServiceConfig wires the attention inbox.
type AttentionServiceConfig struct {
	// Tasks pages blocked tasks and drives readmit/complete/bump. Required.
	Tasks AttentionTaskService

	// Bindings resolves the forge a project's tasks are commented on.
	// Required.
	Bindings BindingReader

	// Forges builds the forge adapter a binding names. Required.
	Forges ForgeResolver

	// Projects resolves a task's project display name. Required.
	Projects ProjectReader

	// Fleet lists fleet nodes for the agent_offline alert. Optional; nil
	// omits the alert kind entirely rather than reporting it empty.
	Fleet AttentionFleetReader

	// Accounts lists pooled provider accounts for the account_error alert.
	// Optional, same as Fleet.
	Accounts AttentionAccountReader

	// Barrier reports the calling tenant's own barrier state for the
	// barrier_sealed alert. Optional, same as Fleet.
	Barrier AttentionBarrierReader

	// Now is the clock ages and rulings are stamped with; nil takes
	// time.Now.
	Now func() time.Time

	// Rulers are the node-configured forge logins whose comment on a parked
	// issue flags the item "ruling posted" — the same policy the unblock
	// sweep readmits on. See RulerPolicy.
	Rulers RulerPolicy
}

// AttentionService lists everything only an operator can clear and drives
// the resolutions the inbox offers.
type AttentionService struct {
	tasks    AttentionTaskService
	bindings BindingReader
	forges   ForgeResolver
	projects ProjectReader
	fleet    AttentionFleetReader
	accounts AttentionAccountReader
	barrier  AttentionBarrierReader
	now      func() time.Time
	rulers   RulerPolicy
}

// NewAttentionService wires the service; Tasks, Bindings, Forges and
// Projects are required, the alert sources are not.
func NewAttentionService(cfg AttentionServiceConfig) (*AttentionService, error) {
	switch {
	case cfg.Tasks == nil:
		return nil, &InvalidInputError{Field: "tasks", Message: "the inbox lists blocked tasks and drives their resolution through the engine's own verbs"}
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "the forge a ruling is posted to lives on the project's binding"}
	case cfg.Forges == nil:
		return nil, &InvalidInputError{Field: "forges", Message: "a ruling is posted through the forge adapter"}
	case cfg.Projects == nil:
		return nil, &InvalidInputError{Field: "projects", Message: "the inbox names the project a parked task belongs to"}
	}
	if err := cfg.Rulers.Validate(); err != nil {
		return nil, err
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &AttentionService{
		tasks: cfg.Tasks, bindings: cfg.Bindings, forges: cfg.Forges, projects: cfg.Projects,
		fleet: cfg.Fleet, accounts: cfg.Accounts, barrier: cfg.Barrier, now: now,
		rulers: cfg.Rulers,
	}, nil
}

// List returns one page of the tenant's attention items: every blocked task,
// plus whatever environmental alerts the wired sources report, faceted by
// project and kind.
func (s *AttentionService) List(ctx context.Context, tenantID string, filter AttentionFilter, page *orchestration.Page) (*orchestration.PageResult[*AttentionItem], error) {
	if tenantID == "" {
		return nil, &InvalidInputError{Field: "tenant_id", Message: "the inbox is tenant-scoped"}
	}
	if filter.Kind != "" && !filter.Kind.Valid() {
		return nil, &InvalidInputError{Field: "kind", Message: "unknown attention kind " + string(filter.Kind)}
	}

	items, tasks, err := s.collect(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}

	keep := func(item *AttentionItem) bool {
		if filter.ProjectID != "" && item.ProjectID != filter.ProjectID {
			return false
		}
		if filter.Kind != "" && item.Kind != filter.Kind {
			return false
		}
		return true
	}
	result, err := orchestration.ApplyFacetedPage(items, page, attentionPageSpec, attentionFacetSpec, keep)
	if err != nil {
		return nil, err
	}
	// Rulings are read for the returned page only: each costs a forge round
	// trip, and the page is what the operator is looking at.
	s.markRulings(ctx, tenantID, result.Items, tasks)
	return result, nil
}

// markRulings sets RulingPosted on every parked-task item whose issue
// carries an authorized ruler's comment newer than the park.
//
// A FORGE THAT CANNOT BE READ LEAVES THE FLAG UNSET rather than failing the
// listing: the flag is a hint layered over an inbox whose whole purpose is to
// render when things are going wrong, and an unreachable forge must not take
// every parked task off the screen with it.
func (s *AttentionService) markRulings(ctx context.Context, tenantID string, items []*AttentionItem, tasks map[string]*orchestration.Task) {
	type projectForge struct {
		forge   ForgeBackend
		binding *ProjectBinding
		ok      bool
	}
	forges := map[string]projectForge{}
	for _, item := range items {
		task := tasks[item.ID]
		if item.Kind != AttentionParkedTask || task == nil {
			continue
		}
		repo, number, err := issueOfTask(task)
		if err != nil {
			continue
		}
		pf, seen := forges[task.ProjectID]
		if !seen {
			binding, bindErr := s.bindings.GetBinding(ctx, tenantID, task.ProjectID)
			if bindErr == nil {
				forge, forgeErr := s.forges.ForgeFor(ctx, tenantID, binding.Forge)
				pf = projectForge{forge: forge, binding: binding, ok: forgeErr == nil && forge != nil}
			}
			forges[task.ProjectID] = pf
		}
		if !pf.ok || ensureTaskSourceRepoMatchesBinding(task, pf.binding, repo) != nil {
			continue
		}
		comments, err := pf.forge.ListIssueComments(ctx, repo, number)
		if err != nil {
			continue
		}
		_, item.RulingPosted = findRuling(task, comments, rulerSet(pf.binding, s.rulers.For(tenantID)))
	}
}

// collect builds every attention item this tenant carries, unfiltered: the
// facet counts need to see the whole set before a chosen facet narrows it
// (see ApplyFacetedPage's ordering contract).
//
// It also returns the blocked tasks by id, for markRulings.
func (s *AttentionService) collect(ctx context.Context, tenantID string, filter AttentionFilter) ([]*AttentionItem, map[string]*orchestration.Task, error) {
	var items []*AttentionItem

	tasks, err := s.tasks.ListTasksPage(ctx, tenantID,
		&orchestration.TaskFilter{Status: orchestration.TaskStatusBlocked}, nil)
	if err != nil {
		return nil, nil, err
	}
	names := map[string]string{}
	byID := make(map[string]*orchestration.Task, len(tasks.Items))
	for _, task := range tasks.Items {
		byID[task.ID] = task
		items = append(items, s.taskItem(ctx, tenantID, task, names))
	}
	idle, err := s.projectIdleAlerts(ctx, tenantID, tasks.Items, names)
	if err != nil {
		return nil, nil, err
	}
	items = append(items, idle...)

	if s.fleet != nil {
		alerts, err := s.agentOfflineAlerts(ctx, tenantID)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, alerts...)
	}
	if s.accounts != nil {
		alerts, err := s.accountErrorAlerts(ctx, tenantID)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, alerts...)
	}
	if s.barrier != nil {
		alert, err := s.barrierSealedAlert(ctx, tenantID)
		if err != nil {
			return nil, nil, err
		}
		if alert != nil {
			items = append(items, alert)
		}
	}
	return items, byID, nil
}

// taskItem renders one blocked task as an attention item. names caches
// project id -> display name across one List call.
func (s *AttentionService) taskItem(ctx context.Context, tenantID string, task *orchestration.Task, names map[string]string) *AttentionItem {
	item := &AttentionItem{
		ID:              task.ID,
		Kind:            AttentionParkedTask,
		ProjectID:       task.ProjectID,
		Subject:         task.Subject,
		BlockKind:       task.BlockKind,
		CreatedAt:       task.CreatedAt,
		StatusChangedAt: task.StatusChangedAt,
		Attempts:        task.ChargedAttempts(),
		Actions:         []AttentionAction{AttentionAnswer, AttentionReadmit, AttentionCloseAsDone, AttentionNote},
	}

	if task.ProjectID != "" {
		item.ProjectName = s.projectName(ctx, tenantID, task.ProjectID, names)
	}
	if repo, number, err := issueOfTask(task); err == nil {
		item.IssueRef = IssueRefString(repo, number)
		if binding, bindErr := s.bindings.GetBinding(ctx, tenantID, task.ProjectID); bindErr == nil {
			item.IssueURL = issueURL(binding.Forge.BaseURL, repo, number)
		}
	}

	// blockedCondition ALWAYS RETURNS A DETAIL, even when the reason carries
	// no typed marker: a park this module composed but never routed through
	// ComposeBlockedReason (label_reconcile's operator-hold prose, for
	// instance) still has words a person needs read, so the ask is
	// extracted from detail regardless of ok — only Condition depends on it.
	condition, detail, ok := blockedCondition(task.BlockedReason)
	if ok {
		item.Condition = condition.Kind
	}
	ask := ExtractAsk(detail)
	item.Question, item.Recommendation, item.Truncated = ask.Question, ask.Recommendation, ask.Truncated
	if item.Recommendation != "" {
		item.Actions = append([]AttentionAction{AttentionApproveRecommendation}, item.Actions...)
	}
	if task.BlockKind == orchestration.TaskBlockKindEscalated {
		item.Actions = append(item.Actions, AttentionAllowAnotherRound)
	}
	return item
}

// projectName resolves a project's display name, caching per id, and falls
// back to the id itself when the project cannot be read — a project a task
// still names but that has since been deleted must not blank the row.
func (s *AttentionService) projectName(ctx context.Context, tenantID, projectID string, names map[string]string) string {
	if name, cached := names[projectID]; cached {
		return name
	}
	name := projectID
	if project, err := s.projects.Get(ctx, tenantID, projectID); err == nil && project != nil && project.Name != "" {
		name = project.Name
	}
	names[projectID] = name
	return name
}

// =============================================================================
// Environmental alerts
// =============================================================================

// agentOfflineAlerts reports every fleet node the engine considers offline
// or erroring: work routed to it will not run until a person repairs it.
func (s *AttentionService) agentOfflineAlerts(ctx context.Context, tenantID string) ([]*AttentionItem, error) {
	var alerts []*AttentionItem
	for _, status := range []orchestration.FleetNodeStatus{orchestration.FleetNodeStatusOffline, orchestration.FleetNodeStatusError} {
		page, err := s.fleet.ListFleetNodesPage(ctx, tenantID, &orchestration.FleetNodeFilter{Status: status}, nil)
		if errors.Is(err, orchestration.ErrFleetRegistryRequired) {
			// A node with no fleet registry configured (an embedded,
			// single-process deployment, some test rigs) has simply never
			// had a fleet to go offline. Degrading here is what keeps this
			// OPTIONAL alert from taking the whole inbox down with it.
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, agent := range page.Items {
			alerts = append(alerts, &AttentionItem{
				ID:      "agent_offline:" + agent.ID,
				Kind:    AttentionFleetNodeOffline,
				Subject: "Fleet node " + agent.Name + " is " + string(agent.Status),
				Message: "Agent " + agent.Name + " (" + agent.Host + ") is " + string(agent.Status) +
					"; work routed to it will not run until it is repaired or removed from the fleet.",
				Severity:        "critical",
				StatusChangedAt: agent.LastSeen,
			})
		}
	}
	return alerts, nil
}

// accountErrorAlerts reports every pooled provider account whose last
// credential refresh failed — the "pool credential stale" alert.
func (s *AttentionService) accountErrorAlerts(ctx context.Context, tenantID string) ([]*AttentionItem, error) {
	accounts, err := s.accounts.ListByTenant(ctx, tenantID, "", "")
	if err != nil {
		return nil, err
	}
	var alerts []*AttentionItem
	for _, account := range accounts {
		if account.Status != orchestration.AccountStatusError {
			continue
		}
		message := "The pooled " + string(account.Provider) + " account " + account.DisplayName +
			" failed its last credential refresh; sessions resolving it cannot get a token until it is fixed."
		subject := "Pooled account " + account.DisplayName + " needs a credential refresh"
		// A LOGIN THE PROVIDER REJECTED is fixed by a person logging the
		// account in again, not by a refresh, so the item says that.
		if strings.HasPrefix(account.LastError, orchestration.AccountReloginNeededPrefix) {
			message = "The provider rejected the login of the pooled " + string(account.Provider) + " account " +
				account.DisplayName + "; it is out of selection until someone logs it in again and stores the new credential." +
				" Reviews that can only run on it are held, not dispatched, and resume on the next sweep once it is usable."
			subject = "Pooled account " + account.DisplayName + " needs a re-login"
		}
		if account.LastError != "" {
			message += " Last error: " + account.LastError
		}
		alerts = append(alerts, &AttentionItem{
			ID:        "account_error:" + account.ID,
			Kind:      AttentionAccountError,
			ProjectID: account.ProjectID,
			Subject:   subject,
			Message:   message,
			Severity:  "critical",
		})
	}
	return alerts, nil
}

// projectIdleStatuses are the task states that mean a project still has work
// the factory may do: queued, or being done.
var projectIdleStatuses = []orchestration.TaskStatus{
	orchestration.TaskStatusPending,
	orchestration.TaskStatusClaimed,
	orchestration.TaskStatusInProgress,
}

// projectIdleAlerts reports every autonomous, unparked project that holds
// parked work and nothing pending or in progress: the factory is idle there
// until somebody rules. blocked is the tenant's parked tasks, already read
// for the inbox; names is the listing's project-name cache.
//
// A PARKED PROJECT IS NOT IDLE, it is closed: its operator stopped intake on
// purpose, and an alert saying so would be noise. A project that is not
// autonomous runs nothing on its own, so nothing about it is stalled either.
func (s *AttentionService) projectIdleAlerts(ctx context.Context, tenantID string, blocked []*orchestration.Task, names map[string]string) ([]*AttentionItem, error) {
	parked := map[string]int{}
	for _, task := range blocked {
		if task.ProjectID != "" {
			parked[task.ProjectID]++
		}
	}
	if len(parked) == 0 {
		return nil, nil
	}
	active, err := s.tasks.ListTasksPage(ctx, tenantID, &orchestration.TaskFilter{Statuses: projectIdleStatuses}, nil)
	if err != nil {
		return nil, err
	}
	for _, task := range active.Items {
		delete(parked, task.ProjectID)
	}
	projectIDs := make([]string, 0, len(parked))
	for projectID := range parked {
		projectIDs = append(projectIDs, projectID)
	}
	sort.Strings(projectIDs)

	var alerts []*AttentionItem
	for _, projectID := range projectIDs {
		project, err := s.projects.Get(ctx, tenantID, projectID)
		if err != nil || project == nil {
			// A project a parked task still names but that cannot be read —
			// deleted since, most often — has no factory to be idle.
			continue
		}
		if !project.Autonomous || project.Parked {
			continue
		}
		name := s.projectName(ctx, tenantID, projectID, names)
		count := strconv.Itoa(parked[projectID])
		alerts = append(alerts, &AttentionItem{
			ID:          "project_idle:" + projectID,
			Kind:        AttentionProjectIdle,
			ProjectID:   projectID,
			ProjectName: name,
			Subject:     "Project " + name + " is idle: " + count + " parked item(s) wait on you",
			Message: "The factory has nothing pending or in progress for " + name + ", and " + count +
				" parked item(s) wait on a ruling. It stays idle until one of them is resolved.",
			Severity: "warning",
		})
	}
	return alerts, nil
}

// barrierSealedAlert reports the calling tenant's own storage barrier when
// it is sealed; nil when it is not (there is nothing to surface) or when
// Barrier is unwired. It never reports the system barrier — a sealed system
// barrier takes the whole API down before this inbox could render anything.
func (s *AttentionService) barrierSealedAlert(ctx context.Context, tenantID string) (*AttentionItem, error) {
	status, err := s.barrier.BarrierStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	// NO BARRIER IS NOT A SEALED BARRIER. Only an initialized barrier that is
	// sealed stops the tenant's data operations; alerting on the other would
	// be a permanent false critical, and one of those teaches an operator to
	// stop reading the inbox.
	if !status.Initialized || !status.Sealed {
		return nil, nil
	}
	scope := status.Scope
	if scope == "" {
		scope = tenantID
	}
	return &AttentionItem{
		ID:       "barrier_sealed:" + scope,
		Kind:     AttentionBarrierSealed,
		Subject:  "Your barrier is sealed",
		Message:  "No data operation can run for this tenant until its barrier is unsealed.",
		Severity: "critical",
	}, nil
}

// =============================================================================
// Resolve
// =============================================================================

// AttentionResolution is what an operator decided about one parked task.
type AttentionResolution struct {
	// Action is the resolution chosen; must be a member of the closed set.
	Action AttentionAction

	// Answer is the operator's free-text ruling. Required for
	// AttentionAnswer and AttentionNote; posted as the evidence for
	// AttentionCloseAsDone when given; ignored otherwise.
	Answer string

	// ActorID identifies who resolved it, for the audit record. Not posted
	// to the forge — the forge comment always carries the bot identity
	// (attentionRulingActor) so the issue's history reads the same
	// regardless of which operator gave the ruling.
	ActorID string
}

// Resolve applies one operator decision to a blocked task: the ruling is
// posted to the forge issue FIRST, and only once that succeeds does the task
// move — AttentionCloseAsDone completes it, AttentionNote leaves it where it
// is, and every other action hands the issue back to the factory and
// readmits it. A note alone may also be posted on a live task that is not
// parked (see resolvable).
// This ordering is deliberate: a task that moved before its ruling reached
// the forge would leave the issue silently stuck at "blocked" while the
// factory had already acted on it, which is exactly the state the inbox
// exists to make visible instead of hiding.
func (s *AttentionService) Resolve(ctx context.Context, tenantID, taskID string, resolution AttentionResolution) (*AttentionItem, error) {
	switch {
	case tenantID == "":
		return nil, &InvalidInputError{Field: "tenant_id", Message: "resolving an item is tenant-scoped"}
	case taskID == "":
		return nil, &InvalidInputError{Field: "task_id", Message: "resolve names the task it resolves"}
	case !resolution.Action.Valid():
		return nil, &InvalidInputError{Field: "action", Message: "unknown attention action " + string(resolution.Action)}
	case resolution.Action == AttentionAnswer && strings.TrimSpace(resolution.Answer) == "":
		return nil, &InvalidInputError{Field: "answer", Message: "the answer action carries no free text to post"}
	case resolution.Action == AttentionNote && strings.TrimSpace(resolution.Answer) == "":
		return nil, &InvalidInputError{Field: "answer", Message: "the note action carries no free text to post"}
	}

	task, err := s.tasks.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return nil, err
	}
	if !resolvable(resolution.Action, task.Status) {
		return nil, &orchestration.TaskInvalidStateError{TaskID: taskID, CurrentState: task.Status, TargetState: orchestration.TaskStatusBlocked}
	}

	names := map[string]string{}
	item := s.taskItem(ctx, tenantID, task, names)

	if err := s.postRuling(ctx, tenantID, task, item, resolution); err != nil {
		return nil, err
	}

	settle, ok := attentionSettlers[resolution.Action]
	if !ok {
		settle = (*AttentionService).readmit
	}
	if err := settle(s, ctx, tenantID, task, item, resolution); err != nil {
		return nil, err
	}
	return item, nil
}

// notableStatuses are the task states a note may be posted on: every state
// in which the task can still act on it. A note moves nothing, so it is not
// confined to parked work. A scope change for a queued or running task
// reaches the issue, and the worker re-reads the issue's discussion at
// dispatch (runnerRun.freshContract). A finished task has no next turn to
// read it.
var notableStatuses = map[orchestration.TaskStatus]bool{
	orchestration.TaskStatusPending:    true,
	orchestration.TaskStatusClaimed:    true,
	orchestration.TaskStatusInProgress: true,
	orchestration.TaskStatusBlocked:    true,
}

// resolvable reports whether action may resolve a task in status. A note
// reaches any live task; every other action moves the task out of the
// inbox, so it applies only to a parked (blocked) one.
func resolvable(action AttentionAction, status orchestration.TaskStatus) bool {
	if action == AttentionNote {
		return notableStatuses[status]
	}
	return status == orchestration.TaskStatusBlocked
}

// attentionSettler moves a task once its ruling is on the forge.
type attentionSettler func(s *AttentionService, ctx context.Context, tenantID string, task *orchestration.Task, item *AttentionItem, resolution AttentionResolution) error

// attentionSettlers are the actions that do something other than readmit;
// every action not named here readmits (AttentionService.readmit).
var attentionSettlers = map[AttentionAction]attentionSettler{
	AttentionCloseAsDone:       (*AttentionService).closeAsDone,
	AttentionAllowAnotherRound: (*AttentionService).allowAnotherRound,
	AttentionNote:              (*AttentionService).keepParked,
}

// readmit hands the issue back to the factory and returns the task to the
// queue.
func (s *AttentionService) readmit(ctx context.Context, tenantID string, task *orchestration.Task, _ *AttentionItem, _ AttentionResolution) error {
	if err := s.handBack(ctx, tenantID, task); err != nil {
		return err
	}
	return s.tasks.ReadmitTask(ctx, tenantID, task.ID)
}

// allowAnotherRound raises the task's review-round budget by one, then
// readmits it.
func (s *AttentionService) allowAnotherRound(ctx context.Context, tenantID string, task *orchestration.Task, item *AttentionItem, resolution AttentionResolution) error {
	if _, err := s.tasks.BumpTaskReviewRoundBudget(ctx, tenantID, task.ID); err != nil {
		return err
	}
	return s.readmit(ctx, tenantID, task, item, resolution)
}

// keepParked is the note's settlement: the note is already on the forge, and
// the task stays exactly where it is.
func (*AttentionService) keepParked(context.Context, string, *orchestration.Task, *AttentionItem, AttentionResolution) error {
	return nil
}

// handBack gives a readmitted task's issue back to the factory: the trigger
// label goes on and every park label comes off (see parkLabels).
//
// THE LABELS MOVE BEFORE THE TASK DOES. A ruling that readmitted the task and
// left needs-info, escalated or the hold on the issue would leave a task
// running against an issue that still says a person owns it — and while it
// waits for dispatch, triage would read the ruling as a fresh comment on a
// needs-info issue and judge it again. The trigger label is the display
// state of queued work, and triage never re-judges a triggered issue.
//
// THE TRIGGER GOES ON FIRST, as in Hooks.moveLabelsOn: an add that lands and
// a remove that does not leaves an over-labelled issue a retry converges; the
// other order can leave one carrying nothing the module can find.
//
// A task with no forge issue has no labels to move and is not refused for it.
func (s *AttentionService) handBack(ctx context.Context, tenantID string, task *orchestration.Task) error {
	repo, number, err := issueOfTask(task)
	if err != nil {
		return nil
	}
	binding, err := s.bindings.GetBinding(ctx, tenantID, task.ProjectID)
	if err != nil {
		return err
	}
	if err := ensureTaskSourceRepoMatchesBinding(task, binding, repo); err != nil {
		return err
	}
	forge, err := s.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return err
	}
	issue, err := forge.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	if issue == nil {
		return &NotFoundError{Entity: "issue", TenantID: tenantID, ID: IssueRefString(repo, number)}
	}
	return handIssueBack(ctx, forge, binding, repo, issue)
}

// handIssueBack puts the trigger label on issue and takes every park label
// off, trigger first (see AttentionService.handBack). It is the one label move
// every operator verb that gives an issue back to the factory makes.
func handIssueBack(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, repo RepoRef, issue *ForgeIssue) error {
	if trigger := strings.TrimSpace(binding.Forge.TriggerLabel); trigger != "" && heldBy(issue, trigger) == "" {
		if err := forge.AddIssueLabels(ctx, repo, issue.Number, []string{trigger}); err != nil {
			return err
		}
	}
	return stripParkLabels(ctx, forge, binding, repo, issue)
}

// stripParkLabels takes every park label issue carries off it (see
// parkLabels). Both ways an operator ends a park use it: handing the issue
// back to the factory, and closing it as done.
func stripParkLabels(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, repo RepoRef, issue *ForgeIssue) error {
	return removeParkLabels(binding, issue.Labels, func(label string) error {
		return forge.RemoveIssueLabel(ctx, repo, issue.Number, label)
	})
}

// removeParkLabels calls remove with every park label in labels, spelled as
// the forge spells it, stopping at the first refusal. It is the one rule for
// which labels a park leaves, whichever object — issue or pull request —
// carries them.
func removeParkLabels(binding *ProjectBinding, labels []string, remove func(label string) error) error {
	for _, park := range parkLabels(binding) {
		held := heldIn(labels, park.label)
		if held == "" {
			continue
		}
		if err := remove(held); err != nil {
			return err
		}
	}
	return nil
}

// retireIssue closes a finished issue: the park labels come off FIRST, then
// the issue closes. A closed issue still carrying escalated or task:blocked
// would say a person owns work nobody will ever do (X19). In this order a
// label that will not come off leaves the issue open and the task blocked,
// so the operator's retry converges.
func retireIssue(ctx context.Context, forge ForgeBackend, binding *ProjectBinding, repo RepoRef, number int) error {
	issue, err := forge.GetIssue(ctx, repo, number)
	if err != nil {
		return err
	}
	if issue == nil {
		return &NotFoundError{Entity: "issue", ID: IssueRefString(repo, number)}
	}
	if err := stripParkLabels(ctx, forge, binding, repo, issue); err != nil {
		return err
	}
	return forge.CloseIssue(ctx, repo, number)
}

// rulingText renders the comment body an operator's resolution posts to the
// forge issue.
//
// parked says whether the task is blocked. Only the note reads it: a note
// leaves the task where it is, and a queued or running task is not parked.
func rulingText(action AttentionAction, item *AttentionItem, answer string, parked bool) string {
	var b strings.Builder
	// The factory marker keeps this comment from being read back as a fresh
	// owner ruling by the unblock sweep, whichever account posted it.
	b.WriteString(factoryCommentMarker + "ruling task=" + item.ID + " -->\n")
	b.WriteString("**" + attentionRulingActor + "**\n\n")
	switch action {
	case AttentionApproveRecommendation:
		b.WriteString("Approved the recommendation above")
		if item.Recommendation != "" {
			b.WriteString(":\n\n> " + strings.ReplaceAll(item.Recommendation, "\n", "\n> "))
		}
		b.WriteString("\n\nThe task is readmitted to the queue.")
	case AttentionAnswer:
		b.WriteString(strings.TrimSpace(answer) + "\n\nThe task is readmitted to the queue.")
	case AttentionAllowAnotherRound:
		// The operator's optional text scopes the granted round; it goes in
		// the same comment, so the worker reads one ruling, not two.
		if scope := strings.TrimSpace(answer); scope != "" {
			b.WriteString(scope + "\n\n")
		}
		b.WriteString("One more review round is granted for this task; it is readmitted to the queue.")
	case AttentionCloseAsDone:
		// The operator's evidence for "done" belongs on the issue people
		// read, not only in the task record.
		if evidence := strings.TrimSpace(answer); evidence != "" {
			b.WriteString(evidence + "\n\n")
		}
		b.WriteString("No further work is needed. The issue is closed and the task is marked done.")
	case AttentionReadmit:
		b.WriteString("The task is readmitted to the queue.")
	case AttentionNote:
		b.WriteString(strings.TrimSpace(answer) + "\n\n")
		if parked {
			b.WriteString("The task stays parked.")
		} else {
			b.WriteString("The task keeps its place; its next turn reads this note.")
		}
	}
	return b.String()
}

// postRuling posts the resolution's comment to the task's forge issue. A
// task with no recorded forge issue (pre-provenance, or a task this build's
// TaskSource never adopted from one) posts nothing and is not refused for it
// — Resolve still moves the task, exactly as a readmit through any other
// surface would.
func (s *AttentionService) postRuling(ctx context.Context, tenantID string, task *orchestration.Task, item *AttentionItem, resolution AttentionResolution) error {
	repo, number, err := issueOfTask(task)
	if err != nil {
		return nil
	}
	binding, err := s.bindings.GetBinding(ctx, tenantID, task.ProjectID)
	if err != nil {
		return err
	}
	if err := ensureTaskSourceRepoMatchesBinding(task, binding, repo); err != nil {
		return err
	}
	forge, err := s.forges.ForgeFor(ctx, tenantID, binding.Forge)
	if err != nil {
		return err
	}
	body := rulingText(resolution.Action, item, resolution.Answer, task.Status == orchestration.TaskStatusBlocked)
	if _, err := forge.CommentOnIssue(ctx, repo, number, body); err != nil {
		return err
	}
	if resolution.Action == AttentionCloseAsDone {
		return retireIssue(ctx, forge, binding, repo, number)
	}
	return nil
}

// closeAsDone concludes the task without readmitting it: the operator has
// decided no further work is needed. CloseBlockedTask is the engine's one
// blocked -> completed edge; CompleteTask refuses a blocked task by design.
func (s *AttentionService) closeAsDone(ctx context.Context, tenantID string, task *orchestration.Task, item *AttentionItem, resolution AttentionResolution) error {
	output := "Closed by an owner ruling from the attention inbox: no further work is needed."
	if strings.TrimSpace(resolution.Answer) != "" {
		output = strings.TrimSpace(resolution.Answer)
	}
	return s.tasks.CloseBlockedTask(ctx, tenantID, task.ID, &orchestration.TaskResult{Output: output})
}

// issueURL builds the browsable link for a forge issue: the Gitea/GitHub
// path convention every adapter this build registers shares
// ({base}/{owner}/{repo}/issues/{number}), from the binding's forge API
// root. See ForgeConfig.BaseURL.
func issueURL(baseURL string, repo RepoRef, number int) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/" + repo.Owner + "/" + repo.Repo + "/issues/" + strconv.Itoa(number)
}
