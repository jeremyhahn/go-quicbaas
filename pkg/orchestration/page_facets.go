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

package orchestration

import "strconv"

// =============================================================================
// Per-Endpoint Facets
//
// A FACET KEY AND ITS FILTER ARE ONE THING, so they are written side by side.
// Every key counted below is a narrowing the same endpoint accepts under the
// same name: `facets.status` counts what `?statuses=` would select,
// `facets.agent_id` counts what `?agent_id=` would. A key with no filter would
// be a number the operator cannot act on, and a filter with no key would be a
// choice the bar cannot offer.
//
// THE FACETED NARROWINGS ARE APPLIED HERE, NOT IN THE STORE READ, because the
// counts must see the records they remove — see ApplyFacetedPage. Each
// resource therefore splits its filter in two: the BASE half narrows the store
// listing (a tenant's tasks, one task's attempts), and the FACET half runs as
// the keep predicate. Only the facet half is counted.
// =============================================================================

// taskFacetSpec counts a tenant's tasks by status, project and agent.
var taskFacetSpec = FacetSpec[*Task]{
	Values: map[string]func(*Task) []string{
		"status":     func(task *Task) []string { return []string{string(task.Status)} },
		"project_id": func(task *Task) []string { return []string{task.ProjectID} },
		"agent_id":   func(task *Task) []string { return []string{task.AgentID} },
	},
}

// TaskFacetSpec returns the facets the tasks listing counts.
func TaskFacetSpec() FacetSpec[*Task] { return taskFacetSpec }

// FacetBase returns a copy of the filter with the FACETED narrowings removed,
// for the store listing the counts are taken over. A nil filter has no
// narrowings to remove.
//
// Status — the singular, legacy field — stays in the base half. It is not the
// facet: the UI sends `statuses`, and a caller that sets both is asking for
// the intersection, which composing a base narrowing with a faceted one is.
func (f *TaskFilter) FacetBase() *TaskFilter {
	if f == nil {
		return nil
	}
	base := *f
	base.Statuses = nil
	// A source is reserved per project, so with a source set the project is
	// part of the source's key and stays on the base (see TaskFilter.SourceKind).
	if f.Source() == nil {
		base.ProjectID = ""
	}
	base.AgentID = ""
	return &base
}

// MatchesFacets reports whether task satisfies the filter's faceted half.
func (f *TaskFilter) MatchesFacets(task *Task) bool {
	if f == nil || task == nil {
		return task != nil
	}
	return matchesAny(task.Status, f.Statuses) &&
		matchesFilterValue(task.ProjectID, f.ProjectID) &&
		matchesFilterValue(task.AgentID, f.AgentID)
}

// jobFacetSpec counts a tenant's jobs by state, runner, project and agent.
var jobFacetSpec = FacetSpec[*JobStatus]{
	Values: map[string]func(*JobStatus) []string{
		"state":      func(job *JobStatus) []string { return []string{string(job.State)} },
		"runner":     func(job *JobStatus) []string { return []string{jobStatusRunner(job)} },
		"project_id": func(job *JobStatus) []string { return []string{jobStatusProjectID(job)} },
		"agent_id":   func(job *JobStatus) []string { return []string{job.AgentID} },
	},
}

// JobFacetSpec returns the facets the jobs listing counts.
func JobFacetSpec() FacetSpec[*JobStatus] { return jobFacetSpec }

// FacetBase returns a copy of the filter narrowed to its base half — the task
// binding and the explicit id list, which no facet counts.
func (f *JobFilter) FacetBase() *JobFilter {
	if f == nil {
		return nil
	}
	return &JobFilter{TaskID: f.TaskID, JobIDs: f.JobIDs}
}

// MatchesFacets reports whether status satisfies the filter's faceted half.
func (f *JobFilter) MatchesFacets(status *JobStatus) bool {
	if status == nil {
		return false
	}
	if f == nil {
		return true
	}
	return matchesAny(status.State, f.Statuses) &&
		matchesFilterValue(jobStatusRunner(status), f.Runner) &&
		matchesFilterValue(jobStatusProjectID(status), f.ProjectID) &&
		matchesFilterValue(status.AgentID, f.AgentID)
}

// sessionFacetSpec counts the session listing by backend, state, role and
// agent.
var sessionFacetSpec = FacetSpec[*SessionInfo]{
	Values: map[string]func(*SessionInfo) []string{
		"backend":  func(session *SessionInfo) []string { return []string{session.Backend} },
		"state":    func(session *SessionInfo) []string { return []string{string(session.State)} },
		"runner":   func(session *SessionInfo) []string { return []string{session.Runner} },
		"agent_id": func(session *SessionInfo) []string { return []string{session.AgentID} },
	},
}

// SessionFacetSpec returns the facets the sessions listing counts.
func SessionFacetSpec() FacetSpec[*SessionInfo] { return sessionFacetSpec }

// MatchesSessionFacets reports whether session satisfies the faceted half of
// a sessions request: the backend, the state, the role and the agent.
//
// ALL FOUR ARE NARROWED HERE AND NOWHERE ELSE, over the merged listing. The
// backend and the state used to be narrowed inside the walk that builds it,
// which is a page earlier and two code paths — the live map and the durable
// records — that came to disagree about the state a session was in (#792).
// They are counted facets now, so they must run after the counts anyway, and
// running them once over the merged answer is what makes the state this
// matches the state the caller is shown.
func MatchesSessionFacets(req *ListSessionsRequest, session *SessionInfo) bool {
	if session == nil {
		return false
	}
	if req == nil {
		return true
	}
	return matchesFilterValue(session.Backend, req.Backend) &&
		matchesFilterValue(string(session.State), string(req.Status)) &&
		matchesFilterValue(session.Runner, req.Runner) &&
		matchesFilterValue(session.AgentID, req.AgentID)
}

// fleetNodeFacetSpec counts the fleet registry by status and served backend.
var fleetNodeFacetSpec = FacetSpec[*FleetNode]{
	Values: map[string]func(*FleetNode) []string{
		"status": func(agent *FleetNode) []string { return []string{string(agent.Status)} },
		// EVERY BACKEND THE DAEMON SERVES, not just its primary: the filter
		// matches on membership (FleetNodeServesBackend), so counting only
		// Backend would print a zero beside a choice that selects the
		// daemon.
		"backend": func(agent *FleetNode) []string {
			backends := make([]string, 0, len(agent.Capabilities)+1)
			backends = append(backends, agent.Backend)
			return append(backends, agent.Capabilities...)
		},
	},
}

// FleetNodeFacetSpec returns the facets the agents listing counts.
func FleetNodeFacetSpec() FacetSpec[*FleetNode] { return fleetNodeFacetSpec }

// FacetBase returns a copy of the filter narrowed to its base half — host,
// type and model, which no facet counts.
func (f *FleetNodeFilter) FacetBase() *FleetNodeFilter {
	if f == nil {
		return nil
	}
	return &FleetNodeFilter{Host: f.Host, NodeType: f.NodeType, Model: f.Model, Limit: f.Limit}
}

// MatchesFacets reports whether agent satisfies the filter's faceted half.
func (f *FleetNodeFilter) MatchesFacets(agent *FleetNode) bool {
	if agent == nil {
		return false
	}
	if f == nil {
		return true
	}
	if f.Status != "" && agent.Status != f.Status {
		return false
	}
	return f.Backend == "" || FleetNodeServesBackend(agent, f.Backend)
}

// WorkflowFilter narrows a workflow-definition listing.
//
// Both fields are faceted: a definition carries no timestamp and no owner, so
// the project it is scoped to and the mode it runs in are the only questions
// a filter bar can ask of it.
type WorkflowFilter struct {
	// ProjectID restricts the listing to one project's workflows. A
	// definition scoped to no project matches no project id.
	ProjectID string `json:"project_id,omitempty"`

	// Mode restricts the listing to one execution mode.
	Mode WorkflowMode `json:"mode,omitempty"`
}

// workflowFacetSpec counts registered workflows by project and mode.
var workflowFacetSpec = FacetSpec[*WorkflowDefinition]{
	Values: map[string]func(*WorkflowDefinition) []string{
		"project_id": func(workflow *WorkflowDefinition) []string { return []string{workflow.ProjectID} },
		"mode":       func(workflow *WorkflowDefinition) []string { return []string{string(workflow.Mode)} },
	},
}

// WorkflowFacetSpec returns the facets the workflows listing counts.
func WorkflowFacetSpec() FacetSpec[*WorkflowDefinition] { return workflowFacetSpec }

// Matches reports whether definition satisfies the filter. A nil filter, and
// a filter with no field set, match every definition.
func (f *WorkflowFilter) Matches(definition *WorkflowDefinition) bool {
	if definition == nil {
		return false
	}
	if f == nil {
		return true
	}
	return matchesFilterValue(definition.ProjectID, f.ProjectID) &&
		matchesFilterValue(string(definition.Mode), string(f.Mode))
}

// projectFacetSpec counts the project registry by whether autonomous
// development is on.
//
// THE VALUES ARE THE STRINGS THE FILTER SENDS. `?autonomous=` is a tri-state
// boolean parameter (ParseOptionalBool), so the counts are keyed "true" and
// "false" rather than by a rendering a caller would have to guess at.
var projectFacetSpec = FacetSpec[*Project]{
	Values: map[string]func(*Project) []string{
		"autonomous": func(project *Project) []string {
			return []string{strconv.FormatBool(project.Autonomous)}
		},
	},
}

// ProjectFacetSpec returns the facets the projects listing counts.
func ProjectFacetSpec() FacetSpec[*Project] { return projectFacetSpec }

// MatchesProjectAutonomy reports whether project satisfies a tri-state
// `autonomous` filter. A nil want is "the caller did not ask" and matches
// every project — NOT false, which matches only the ones with it off.
func MatchesProjectAutonomy(project *Project, want *bool) bool {
	if project == nil {
		return false
	}
	return want == nil || project.Autonomous == *want
}

// accountFacetSpec counts agent accounts by provider and lifecycle status.
var accountFacetSpec = FacetSpec[*Account]{
	Values: map[string]func(*Account) []string{
		"provider": func(account *Account) []string { return []string{string(account.Provider)} },
		"status":   func(account *Account) []string { return []string{string(account.Status)} },
	},
}

// AccountFacetSpec returns the facets the accounts listing counts.
func AccountFacetSpec() FacetSpec[*Account] { return accountFacetSpec }

// MatchesAccountFacets reports whether account satisfies the faceted half of
// an accounts listing: the provider and the status. Both empty match every
// account.
func MatchesAccountFacets(account *Account, provider AccountProvider, status AccountStatus) bool {
	if account == nil {
		return false
	}
	return matchesFilterValue(string(account.Provider), string(provider)) &&
		matchesFilterValue(string(account.Status), string(status))
}
