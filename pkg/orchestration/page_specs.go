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

package orchestration

// =============================================================================
// Per-Endpoint Page Specifications
//
// THE ALLOW-LISTS ARE THE API. `sort` names a field from the map below and
// nothing else, and `q` searches the fields the Text extractor returns and
// nothing else, so the documented contract in docs/software-factory/api.md and
// the code that serves it are one artifact rather than two that drift.
//
// The specs are built once at init rather than per request: a listing of 2,000
// jobs must not pay for seven map literals to answer a page of 25.
// =============================================================================

// jobPageSpec orders and searches a tenant's jobs. The natural order is
// newest first: an operator opening the job list wants the run that just
// happened, not the first one ever recorded.
var jobPageSpec = PageSpec[*JobStatus]{
	Sorts: map[string]func(a, b *JobStatus) int{
		"job_id":      func(a, b *JobStatus) int { return compareStrings(a.JobID, b.JobID) },
		"state":       func(a, b *JobStatus) int { return compareStrings(string(a.State), string(b.State)) },
		"runner":      func(a, b *JobStatus) int { return compareStrings(jobStatusRunner(a), jobStatusRunner(b)) },
		"task_id":     func(a, b *JobStatus) int { return compareStrings(jobStatusTaskID(a), jobStatusTaskID(b)) },
		"agent_id":    func(a, b *JobStatus) int { return compareStrings(a.AgentID, b.AgentID) },
		"description": func(a, b *JobStatus) int { return compareStrings(a.Description, b.Description) },
		"started_at":  func(a, b *JobStatus) int { return compareInt64(a.StartedAt, b.StartedAt) },
		"updated_at":  func(a, b *JobStatus) int { return compareInt64(a.UpdatedAt, b.UpdatedAt) },
	},
	DefaultSort:       "started_at",
	DefaultDescending: true,
	Text: func(job *JobStatus) []string {
		return []string{job.JobID, job.Description, jobStatusRunner(job), string(job.State), jobStatusTaskID(job)}
	},
}

// JobPageSpec returns how the jobs listing is ordered and searched.
func JobPageSpec() PageSpec[*JobStatus] { return jobPageSpec }

// taskPageSpec orders and searches a tenant's tasks, newest first.
var taskPageSpec = PageSpec[*Task]{
	Sorts: map[string]func(a, b *Task) int{
		"id":         func(a, b *Task) int { return compareStrings(a.ID, b.ID) },
		"subject":    func(a, b *Task) int { return compareStrings(a.Subject, b.Subject) },
		"status":     func(a, b *Task) int { return compareStrings(string(a.Status), string(b.Status)) },
		"project_id": func(a, b *Task) int { return compareStrings(a.ProjectID, b.ProjectID) },
		"attempts":   func(a, b *Task) int { return compareInt64(int64(a.Attempts), int64(b.Attempts)) },
		"agent_id":   func(a, b *Task) int { return compareStrings(a.AgentID, b.AgentID) },
		"created_at": func(a, b *Task) int { return compareInt64(a.CreatedAt, b.CreatedAt) },
		"updated_at": func(a, b *Task) int { return compareInt64(a.UpdatedAt, b.UpdatedAt) },
	},
	DefaultSort:       "created_at",
	DefaultDescending: true,
	Text: func(task *Task) []string {
		return []string{task.ID, task.Subject, string(task.Status), task.ProjectID}
	},
}

// TaskPageSpec returns how the tasks listing is ordered and searched.
func TaskPageSpec() PageSpec[*Task] { return taskPageSpec }

// sessionPageSpec orders and searches the session listing.
//
// ITS NATURAL ORDER IS OLDEST FIRST, unlike every other timestamped listing
// here. The sessions listing has always answered in creation order and
// callers read it that way; flipping the default under them would be a
// behaviour change wearing a paging change's clothes. A UI that wants the
// newest asks for it: sort=created_at:desc.
var sessionPageSpec = PageSpec[*SessionInfo]{
	Sorts: map[string]func(a, b *SessionInfo) int{
		"session_id":    func(a, b *SessionInfo) int { return compareStrings(a.SessionID, b.SessionID) },
		"backend":       func(a, b *SessionInfo) int { return compareStrings(a.Backend, b.Backend) },
		"model":         func(a, b *SessionInfo) int { return compareStrings(a.Model, b.Model) },
		"state":         func(a, b *SessionInfo) int { return compareStrings(string(a.State), string(b.State)) },
		"project_id":    func(a, b *SessionInfo) int { return compareStrings(a.ProjectID, b.ProjectID) },
		"runner":        func(a, b *SessionInfo) int { return compareStrings(a.Runner, b.Runner) },
		"agent_id":      func(a, b *SessionInfo) int { return compareStrings(a.AgentID, b.AgentID) },
		"created_at":    func(a, b *SessionInfo) int { return compareInt64(a.CreatedAt, b.CreatedAt) },
		"last_activity": func(a, b *SessionInfo) int { return compareInt64(a.LastActivity, b.LastActivity) },
	},
	DefaultSort: "created_at",
	Text: func(session *SessionInfo) []string {
		return []string{session.SessionID, session.Backend, session.Model, string(session.State)}
	},
}

// SessionPageSpec returns how the sessions listing is ordered and searched.
func SessionPageSpec() PageSpec[*SessionInfo] { return sessionPageSpec }

// fleetNodePageSpec orders and searches the fleet registry. The natural order is
// the most recently registered daemon first, which is the one an operator who
// just brought a host up is looking for.
var fleetNodePageSpec = PageSpec[*FleetNode]{
	Sorts: map[string]func(a, b *FleetNode) int{
		"id":            func(a, b *FleetNode) int { return compareStrings(a.ID, b.ID) },
		"name":          func(a, b *FleetNode) int { return compareStrings(a.Name, b.Name) },
		"host":          func(a, b *FleetNode) int { return compareStrings(a.Host, b.Host) },
		"backend":       func(a, b *FleetNode) int { return compareStrings(a.Backend, b.Backend) },
		"status":        func(a, b *FleetNode) int { return compareStrings(string(a.Status), string(b.Status)) },
		"last_seen":     func(a, b *FleetNode) int { return compareInt64(a.LastSeen, b.LastSeen) },
		"registered_at": func(a, b *FleetNode) int { return compareInt64(a.RegisteredAt, b.RegisteredAt) },
	},
	DefaultSort:       "registered_at",
	DefaultDescending: true,
	// The search covers every name the agent is MATCHED on, not just its
	// primary backend: a daemon serving claude, codex and opencode carries
	// the other two in Capabilities, and a search for "codex" that missed it
	// would contradict what dispatch does with the same record.
	Text: func(agent *FleetNode) []string {
		text := make([]string, 0, len(agent.Capabilities)+3)
		text = append(text, agent.ID, agent.Name, agent.Backend)
		return append(text, agent.Capabilities...)
	},
}

// FleetNodePageSpec returns how the agents listing is ordered and searched.
func FleetNodePageSpec() PageSpec[*FleetNode] { return fleetNodePageSpec }

// workflowPageSpec orders and searches registered workflow definitions.
//
// ITS NATURAL ORDER IS BY NAME, not by time: a workflow definition carries no
// timestamp, and the listing has always been sorted by name. The name is also
// the identifier, which is why `q` matching "id" and matching "name" are the
// same match here.
var workflowPageSpec = PageSpec[*WorkflowDefinition]{
	Sorts: map[string]func(a, b *WorkflowDefinition) int{
		"name":       func(a, b *WorkflowDefinition) int { return compareStrings(a.Name, b.Name) },
		"mode":       func(a, b *WorkflowDefinition) int { return compareStrings(string(a.Mode), string(b.Mode)) },
		"project_id": func(a, b *WorkflowDefinition) int { return compareStrings(a.ProjectID, b.ProjectID) },
	},
	DefaultSort: "name",
	Text: func(workflow *WorkflowDefinition) []string {
		return []string{workflow.Name}
	},
}

// WorkflowPageSpec returns how the workflows listing is ordered and searched.
func WorkflowPageSpec() PageSpec[*WorkflowDefinition] { return workflowPageSpec }

// projectPageSpec orders and searches the project registry, newest first.
var projectPageSpec = PageSpec[*Project]{
	Sorts: map[string]func(a, b *Project) int{
		"id":         func(a, b *Project) int { return compareStrings(a.ID, b.ID) },
		"name":       func(a, b *Project) int { return compareStrings(a.Name, b.Name) },
		"autonomous": func(a, b *Project) int { return compareBool(a.Autonomous, b.Autonomous) },
		"created_at": func(a, b *Project) int { return a.CreatedAt.Compare(b.CreatedAt) },
		"updated_at": func(a, b *Project) int { return a.UpdatedAt.Compare(b.UpdatedAt) },
	},
	DefaultSort:       "created_at",
	DefaultDescending: true,
	Text: func(project *Project) []string {
		return []string{project.ID, project.Name}
	},
}

// ProjectPageSpec returns how the projects listing is ordered and searched.
func ProjectPageSpec() PageSpec[*Project] { return projectPageSpec }

// accountPageSpec orders and searches agent accounts, newest first.
var accountPageSpec = PageSpec[*Account]{
	Sorts: map[string]func(a, b *Account) int{
		"id":           func(a, b *Account) int { return compareStrings(a.ID, b.ID) },
		"display_name": func(a, b *Account) int { return compareStrings(a.DisplayName, b.DisplayName) },
		"provider":     func(a, b *Account) int { return compareStrings(string(a.Provider), string(b.Provider)) },
		"status":       func(a, b *Account) int { return compareStrings(string(a.Status), string(b.Status)) },
		"created_at":   func(a, b *Account) int { return a.CreatedAt.Compare(b.CreatedAt) },
		"updated_at":   func(a, b *Account) int { return a.UpdatedAt.Compare(b.UpdatedAt) },
	},
	DefaultSort:       "created_at",
	DefaultDescending: true,
	Text: func(account *Account) []string {
		return []string{account.ID, account.DisplayName, string(account.Provider)}
	},
}

// AccountPageSpec returns how the accounts listing is ordered and searched.
func AccountPageSpec() PageSpec[*Account] { return accountPageSpec }
