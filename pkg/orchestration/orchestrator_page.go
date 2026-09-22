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
// EACH PAGED VERB IS ITS UNPAGED VERB PLUS ApplyPage, and deliberately
// nothing more. The filter, the tenant checks and the store reads have one
// implementation, so a narrowing that works unpaged cannot stop working
// paged; the page is the last thing applied to the answer, over the FILTERED
// set, which is what makes Total the count the caller asked about.
//
// The search and the ordering run here, in the service, and not in a
// transport: seven handlers each cutting their own page would be seven
// slightly different contracts within a release.
// =============================================================================

// ListJobsPage returns one page of the tenant's matching jobs.
func (s *agentServiceImpl) ListJobsPage(ctx context.Context, tenantID string, filter *JobFilter, page *Page) (*PageResult[*JobStatus], error) {
	jobs, err := s.ListJobs(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return ApplyPage(jobs, page, jobPageSpec)
}

// ListTasksPage returns one page of the tenant's matching tasks.
func (s *agentServiceImpl) ListTasksPage(ctx context.Context, tenantID string, filter *TaskFilter, page *Page) (*PageResult[*Task], error) {
	tasks, err := s.ListTasks(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return ApplyPage(tasks, page, taskPageSpec)
}

// ListAgentsPage returns one page of the tenant's matching agents.
func (s *agentServiceImpl) ListAgentsPage(ctx context.Context, tenantID string, filter *AgentFilter, page *Page) (*PageResult[*AgentRegistration], error) {
	agents, err := s.ListAgents(ctx, tenantID, filter)
	if err != nil {
		return nil, err
	}
	return ApplyPage(agents, page, agentPageSpec)
}

// ListWorkflowsPage returns one page of the context tenant's workflow
// definitions.
func (s *agentServiceImpl) ListWorkflowsPage(ctx context.Context, page *Page) (*PageResult[*WorkflowDefinition], error) {
	workflows, err := s.ListWorkflows(ctx)
	if err != nil {
		return nil, err
	}
	return ApplyPage(workflows, page, workflowPageSpec)
}
