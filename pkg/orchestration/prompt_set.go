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

import (
	"embed"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The workflow prompt set
// =============================================================================
//
// EVERY INSTRUCTION THE ENGINE SENDS AN AGENT IS A FILE UNDER
// promptdefaults/workflow, and this is the declaration of what each of them
// has to say. The markdown is the system tenant's content: it is embedded in
// the binary, seeded into a tenant when the tenant is created, and inherited
// by every tenant that has not overridden it.
//
// The builders elsewhere in this package are assemblers over this set. They
// still own the loops, the plurals, the fences and the JSON parsing, because
// those cannot be validated in human-edited markdown; what they no longer own
// is a single word of what an agent reads.

// SetWorkflow is the engine's own set: the lead, worker, reviewer, planner
// and validator prompts of a generic agent workflow.
const SetWorkflow prompts.SetKind = "workflow"

//go:embed promptdefaults/workflow
var workflowDefaultsFS embed.FS

// workflowDefaults is the embedded markdown, keyed by prompt name.
var workflowDefaults = prompts.MustDefaults(workflowDefaultsFS, "promptdefaults/workflow")

// workflowPrompts is the registered set. Registration validates every file
// against the declaration below, so markdown that dropped a hole the engine
// fills refuses to boot rather than reaching an agent.
var workflowPrompts = prompts.MustRegisterSet(workflowSetSpec())

// WorkflowPrompts returns the engine's registered prompt set.
func WorkflowPrompts() *prompts.Set { return workflowPrompts }

// workflowSetSpec declares the set.
func workflowSetSpec() prompts.SetSpec {
	return prompts.SetSpec{
		Kind:        SetWorkflow,
		Description: "the generic agent workflow: decomposition, work, review and validation",
		Prompts: []prompts.PromptSpec{
			{
				Name:        "worker_prompt",
				Description: "the whole instruction one work item is handed to a worker session",
				Default:     workflowDefaults["worker_prompt"],
				Variables: []prompts.Variable{
					{Name: "workspace", Description: "the worker_workspace prompt, empty when the item has no workspace"},
					{Name: "title", Description: "the work item's title", Required: true},
					{Name: "description", Description: "the worker_description prompt, empty when the item has none"},
					{Name: "project", Description: "the worker_project prompt, empty when the item names no project"},
					{Name: "tool_guidance", Description: "the backend's own tool guidance, empty when it publishes none"},
				},
			},
			{
				Name:        "worker_workspace",
				Description: "where a worker writes, for a work item that has a workspace",
				Default:     workflowDefaults["worker_workspace"],
				Variables: []prompts.Variable{
					{Name: "workspace_path", Description: "the absolute path of the workspace directory", Required: true},
				},
			},
			{
				Name:        "worker_description",
				Description: "the work item's description line",
				Default:     workflowDefaults["worker_description"],
				Variables: []prompts.Variable{
					{Name: "description", Description: "the work item's description", Required: true},
				},
			},
			{
				Name:        "worker_project",
				Description: "the work item's project line",
				Default:     workflowDefaults["worker_project"],
				Variables: []prompts.Variable{
					{Name: "project_id", Description: "the project the work item belongs to", Required: true},
				},
			},
			{
				Name:        "reviewer_prompt",
				Description: "what the reviewer is shown: the job's goal and every task's result",
				Default:     workflowDefaults["reviewer_prompt"],
				Variables: []prompts.Variable{
					{Name: "description", Description: "the job's own description", Required: true},
					{Name: "tasks", Description: "the reviewer_prompt_task prompts, one per task"},
				},
			},
			{
				Name:        "reviewer_prompt_task",
				Description: "one task's line in the review prompt",
				Default:     workflowDefaults["reviewer_prompt_task"],
				Variables: []prompts.Variable{
					{Name: "index", Description: "the task's position, from one", Required: true},
					{Name: "subject", Description: "the task's subject", Required: true},
					{Name: "status", Description: "the task's status", Required: true},
				},
			},
			{
				Name:        "reviewer_prompt_result",
				Description: "one task's output in the review prompt",
				Default:     workflowDefaults["reviewer_prompt_result"],
				Variables: []prompts.Variable{
					{Name: "output", Description: "what the task produced", Required: true},
				},
			},
			{
				Name:        "reviewer_system",
				Description: "the reviewer session's standing instructions",
				Default:     workflowDefaults["reviewer_system"],
				Markers: []prompts.Marker{
					{Literal: "approved", Reason: "parseReviewResponse reads the approved field out of the reply"},
					{Literal: "new_work_items", Reason: "parseReviewResponse reads new_work_items out of the reply"},
				},
			},
			{
				Name:        "planner_system",
				Description: "the planning session's standing instructions",
				Default:     workflowDefaults["planner_system"],
				Markers: []prompts.Marker{
					{Literal: "JSON", Reason: "the decomposition reply is parsed as JSON and nothing else"},
				},
			},
			{
				Name:        "decomposition_prompt",
				Description: "what a lead is asked to break a job into work items with",
				Default:     workflowDefaults["decomposition_prompt"],
				Variables: []prompts.Variable{
					{Name: "task", Description: "the job's own description", Required: true},
					{Name: "projects", Description: "the decomposition_projects prompt, empty when the job names none"},
					{Name: "backends", Description: "the decomposition_backends prompt, empty when none were published"},
				},
				Markers: []prompts.Marker{
					{Literal: "\"id\"", Reason: "parseWorkItems reads the id of every work item"},
					{Literal: "\"title\"", Reason: "parseWorkItems reads the title of every work item"},
					{Literal: "\"description\"", Reason: "parseWorkItems reads the description of every work item"},
				},
			},
			{
				Name:        "decomposition_projects",
				Description: "the projects a decomposition may target",
				Default:     workflowDefaults["decomposition_projects"],
				Variables: []prompts.Variable{
					{Name: "project_ids", Description: "the project identifiers, comma separated", Required: true},
				},
			},
			{
				Name:        "decomposition_backends",
				Description: "the backends a decomposition may assign work to",
				Default:     workflowDefaults["decomposition_backends"],
				Variables: []prompts.Variable{
					{Name: "backend_lines", Description: "the decomposition_backend prompts, one per backend", Required: true},
				},
			},
			{
				Name:        "decomposition_backend",
				Description: "one backend's line in the decomposition prompt",
				Default:     workflowDefaults["decomposition_backend"],
				Variables: []prompts.Variable{
					{Name: "name", Description: "the backend's registered name", Required: true},
					{Name: "type", Description: "the backend's type", Required: true},
					{Name: "capabilities", Description: "its capabilities, comma separated, or none", Required: true},
				},
			},
			{
				Name:        "validation_prompt",
				Description: "what an agent validator is asked to judge artifacts with",
				Default:     workflowDefaults["validation_prompt"],
				Variables: []prompts.Variable{
					{Name: "criterion", Description: "the criterion's name", Required: true},
					{Name: "config", Description: "the criterion's configuration", Required: true},
					{Name: "artifacts", Description: "the validation_artifact prompts, one per artifact"},
				},
				Markers: []prompts.Marker{
					{Literal: "passed", Reason: "validateWithAgent reads the passed field out of the verdict"},
				},
			},
			{
				Name:        "validation_artifact",
				Description: "one artifact handed to an agent validator",
				Default:     workflowDefaults["validation_artifact"],
				Variables: []prompts.Variable{
					{Name: "index", Description: "the artifact's position, from one", Required: true},
					{Name: "output", Description: "what the task produced", Required: true},
				},
			},
			{
				Name:        "validation_summary",
				Description: "the heading over the validation results shown to a reviewer",
				Default:     workflowDefaults["validation_summary"],
			},
			{
				Name:        "validation_summary_row",
				Description: "one validation result in that summary",
				Default:     workflowDefaults["validation_summary_row"],
				Variables: []prompts.Variable{
					{Name: "name", Description: "the criterion's name", Required: true},
					{Name: "verdict", Description: "PASSED or FAILED", Required: true},
					{Name: "message", Description: "what the validator said, empty when it said nothing"},
				},
			},
			{
				Name:        "capabilities_context",
				Description: "the heading over the backends a workflow makes available",
				Default:     workflowDefaults["capabilities_context"],
			},
			{
				Name:        "capabilities_agent",
				Description: "one role's line in the capabilities context",
				Default:     workflowDefaults["capabilities_agent"],
				Variables: []prompts.Variable{
					{Name: "role", Description: "the agent role", Required: true},
					{Name: "backend", Description: "the backend it runs on", Required: true},
					{Name: "capabilities", Description: "its capabilities clause, empty when it declares none"},
				},
			},
			{
				Name:        "memory_header",
				Description: "the heading over what previous attempts learned",
				Default:     workflowDefaults["memory_header"],
			},
			{
				Name:        "memory_worked",
				Description: "the heading over approaches that worked",
				Default:     workflowDefaults["memory_worked"],
			},
			{
				Name:        "memory_failed",
				Description: "the heading over approaches that failed",
				Default:     workflowDefaults["memory_failed"],
			},
			{
				Name:        "memory_failed_item",
				Description: "one failed approach and why it failed",
				Default:     workflowDefaults["memory_failed_item"],
				Variables: []prompts.Variable{
					{Name: "title", Description: "the work item, quoted", Required: true},
					{Name: "approach", Description: "what was tried", Required: true},
					{Name: "reason", Description: "why it failed", Required: true},
					{Name: "iteration", Description: "the iteration it failed on", Required: true},
				},
			},
			{
				Name:        "memory_constraints",
				Description: "the heading over constraints previous attempts discovered",
				Default:     workflowDefaults["memory_constraints"],
			},
			{
				Name:        "memory_feedback",
				Description: "the heading over what reviewers said",
				Default:     workflowDefaults["memory_feedback"],
			},
			{
				Name:        "resume_priming",
				Description: "what a fresh agent process is told before it is shown a replayed transcript",
				Default:     workflowDefaults["resume_priming"],
				Markers: []prompts.Marker{
					{Literal: "READY", Reason: "the resume handshake waits for the agent to answer READY"},
				},
			},
			{
				Name:        "resume_priming_omitted",
				Description: "how a replayed transcript says turns were dropped to fit the budget",
				Default:     workflowDefaults["resume_priming_omitted"],
				Variables: []prompts.Variable{
					{Name: "count", Description: "how many turns were dropped", Required: true},
				},
			},
			{
				Name:        "handshake",
				Description: "the throwaway first message that makes an agent CLI publish its init record",
				Default:     workflowDefaults["handshake"],
			},
			{
				Name:        charterLead,
				Description: "the standing instructions the lead session is spawned with",
				Default:     workflowDefaults[charterLead],
			},
			{
				Name:        charterWorker,
				Description: "the standing instructions every worker session is spawned with",
				Default:     workflowDefaults[charterWorker],
			},
			{
				Name:        charterReviewer,
				Description: "the standing instructions the reviewer session is spawned with",
				Default:     workflowDefaults[charterReviewer],
				Markers: []prompts.Marker{
					{Literal: "\"approved\"", Reason: "parseReviewResponse reads the approved field out of the reply"},
					{Literal: "\"feedback\"", Reason: "parseReviewResponse reads the feedback field out of the reply"},
					{Literal: "\"new_work_items\"", Reason: "parseReviewResponse reads new_work_items out of the reply"},
				},
			},
			{
				Name:        charterValidator,
				Description: "the standing instructions an agent validator is prompted with",
				Default:     workflowDefaults[charterValidator],
				Markers: []prompts.Marker{
					{Literal: "\"passed\"", Reason: "validateWithAgent reads the passed field out of the verdict"},
					{Literal: "\"message\"", Reason: "validateWithAgent reads the message field out of the verdict"},
				},
			},
		},
	}
}
