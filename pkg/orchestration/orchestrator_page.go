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

import "context"

// =============================================================================
// Paged Listings
//
// EACH PAGED VERB IS ITS UNPAGED VERB PLUS ApplyFacetedPage, and deliberately
// little more. The tenant checks and the store reads have one implementation,
// so a narrowing that works unpaged cannot stop working paged; the page is the
// last thing applied to the answer, over the FILTERED set, which is what makes
// Total the count the caller asked about.
//
// THE FILTER IS SPLIT IN TWO AND THAT IS THE ONLY SUBTLETY HERE. The BASE half
// narrows the store listing; the FACETED half runs inside ApplyFacetedPage,
// after the counts are taken, because a filter bar's counts must show what
// each unselected choice WOULD yield. See page_facets.go for which half each
// field belongs to.
//
// The search and the ordering run here, in the service, and not in a
// transport: seven handlers each cutting their own page would be seven
// slightly different contracts within a release.
// =============================================================================

// ListJobsPage returns one page of the tenant's matching jobs, with the facet
// counts for its state, runner, project and agent, each row carrying the
// source of the task it is an attempt at.
//
// THE SOURCE IS RESOLVED AFTER THE CUT, which is the one place this verb is
// more than its unpaged twin plus ApplyFacetedPage. The projection costs a
// task read per distinct task, so it must be paid for the twenty-five rows
// being answered and not for the two thousand the tenant owns; doing it
// before the cut would make the page's cost the collection's. It runs after
// the facet counts for the same reason it runs after the page: the counts are
// taken over the matching set, which is the thing too big to project. See
// resolveJobSources.
func (s *agentServiceImpl) ListJobsPage(ctx context.Context, tenantID string, filter *JobFilter, page *Page) (*PageResult[*JobStatus], error) {
	jobs, err := s.ListJobs(ctx, tenantID, filter.FacetBase())
	if err != nil {
		return nil, err
	}
	result, err := ApplyFacetedPage(jobs, page, jobPageSpec, jobFacetSpec, filter.MatchesFacets)
	if err != nil {
		return nil, err
	}
	s.resolveJobSources(ctx, tenantID, result.Items)
	return result, nil
}

// ListTasksPage returns one page of the tenant's matching tasks, with the
// facet counts for its status, project and agent.
func (s *agentServiceImpl) ListTasksPage(ctx context.Context, tenantID string, filter *TaskFilter, page *Page) (*PageResult[*Task], error) {
	tasks, err := s.ListTasks(ctx, tenantID, filter.FacetBase())
	if err != nil {
		return nil, err
	}
	s.resolveTaskAgents(ctx, tenantID, filter, page, tasks)
	return ApplyFacetedPage(tasks, page, taskPageSpec, taskFacetSpec, filter.MatchesFacets)
}

// resolveTaskAgents fills in each task's latest-attempt agent, which the task
// record does not carry (see TaskFilter.AgentID).
//
// IT RESOLVES OVER THE WHOLE MATCHING SET, NOT THE PAGE, because the agent is
// both a facet and a filter: counts taken over a page would say "3" about a
// tenant with two hundred, and a narrowing applied to a page would hide the
// matches that fell off it. The cost is one pass over the tenant's jobs,
// however many tasks name one — see resolveJobFacts.
//
// IT IS SKIPPED WHEN NOBODY WILL READ IT. A nil page is an internal caller
// asking for the raw collection — the dispatcher's queue sweep, a CLI listing
// — which renders no filter bar and no agent column. Charging it for a job
// scan would make every dispatch pass pay for a UI it does not draw.
func (s *agentServiceImpl) resolveTaskAgents(ctx context.Context, tenantID string, filter *TaskFilter, page *Page, tasks []*Task) {
	wanted := filter != nil && filter.AgentID != ""
	if page == nil && !wanted {
		return
	}
	jobIDs := make([]string, 0, len(tasks))
	seen := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		jobID := task.LatestAttemptJobID()
		if jobID == "" {
			continue
		}
		if _, repeated := seen[jobID]; repeated {
			continue
		}
		seen[jobID] = struct{}{}
		jobIDs = append(jobIDs, jobID)
	}
	if len(jobIDs) == 0 {
		return
	}
	facts := s.resolveJobFacts(ctx, tenantID, jobIDs)
	for _, task := range tasks {
		task.AgentID = facts[task.LatestAttemptJobID()].AgentID
	}
}

// ListAgentsPage returns one page of the tenant's matching agents, with the
// facet counts for its status and served backends.
func (s *agentServiceImpl) ListAgentsPage(ctx context.Context, tenantID string, filter *AgentFilter, page *Page) (*PageResult[*AgentRegistration], error) {
	agents, err := s.ListAgents(ctx, tenantID, filter.FacetBase())
	if err != nil {
		return nil, err
	}
	return ApplyFacetedPage(agents, page, agentPageSpec, agentFacetSpec, filter.MatchesFacets)
}

// ListWorkflowsPage returns one page of the context tenant's matching
// workflow definitions, with the facet counts for their project and mode.
func (s *agentServiceImpl) ListWorkflowsPage(ctx context.Context, filter *WorkflowFilter, page *Page) (*PageResult[*WorkflowDefinition], error) {
	workflows, err := s.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return ApplyFacetedPage(workflows, page, workflowPageSpec, workflowFacetSpec, filter.Matches)
}
