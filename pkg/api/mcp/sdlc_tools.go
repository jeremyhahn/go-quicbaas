//go:build mcp

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

package mcp

import (
	"context"
	"encoding/json"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// SDLC module tools — forge bindings and deliveries
// =============================================================================

// sdlcReviewerSchema is the JSON schema of one reviewer profile.
var sdlcReviewerSchema = map[string]interface{}{
	"type":        "object",
	"description": "A reviewer profile: the agent that reviews pull requests, as its own forge identity.",
	"properties": map[string]interface{}{
		"name":                map[string]interface{}{"type": "string", "description": "The profile's name; required on alternates, optional on the default (reads as \"default\")"},
		"counts_toward_merge": map[string]interface{}{"type": "boolean", "description": "Whether the profile's verdicts can land a change; unset is true on the default and false on an alternate"},
		"agent_id":            map[string]interface{}{"type": "string", "description": "The tenant Agent the seat reviews as; it must declare the review role and supplies the backend, model and image"},
		"login":               map[string]interface{}{"type": "string", "description": "The forge account the reviewer posts as; distinct per profile"},
		"token_secret_ref":    map[string]interface{}{"type": "string", "description": "The tenant secret holding this profile's forge token; distinct per profile"},
		"status_context":      map[string]interface{}{"type": "string", "description": "The commit status context; empty takes the role's default"},
		"endpoint":            map[string]interface{}{"type": "string", "description": "The http(s) root of the model server an opencode reviewer on a local model (ollama/<name>) runs against, as its container reaches it; required for such a reviewer, refused otherwise"},
		"host_affinity":       map[string]interface{}{"type": "string", "description": "The fleet node the reviewer must run on; strict — an offline node refuses the review"},
		"prompt_append":       map[string]interface{}{"type": "string", "description": "Prompt material appended after the reviewer charter for every review this seat runs; at most 64 KiB"},
	},
}

// sdlcBindingSchema is the JSON schema of a ProjectBinding argument.
var sdlcBindingSchema = map[string]interface{}{
	"type":        "object",
	"description": "The project binding: the forge repository, the SDLC strategy, the worker, verification, the review policy and ranking.",
	"properties": map[string]interface{}{
		"project_id": map[string]interface{}{"type": "string"},
		"forge": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"type":               map[string]interface{}{"type": "string", "description": "Forge type (gitea)"},
				"base_url":           map[string]interface{}{"type": "string"},
				"owner":              map[string]interface{}{"type": "string"},
				"repo":               map[string]interface{}{"type": "string"},
				"labels":             map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
				"trigger_label":      map[string]interface{}{"type": "string", "description": "The label that hands an issue to the Software Factory"},
				"webhook_secret_ref": map[string]interface{}{"type": "string"},
				"token_secret_ref":   map[string]interface{}{"type": "string"},
				"ssh_host_key": map[string]interface{}{
					"type":        "string",
					"description": "The forge's pinned SSH host public key, one OpenSSH public-key line; required when the project is cloned over ssh",
				},
			},
		},
		"strategy": map[string]interface{}{
			"type":        "string",
			"description": "The registered SDLC the project follows: github-flow, trunk-based or gitflow",
		},
		"worker": agentRefSchema("The Agent or pool that writes the code; every Agent it names declares the coding role."),
		"worker_prompt_append": map[string]interface{}{
			"type":        "string",
			"description": "Prompt material appended after the worker charter for every worker session of this project; at most 64 KiB",
		},
		"triage": agentRefSchema("The Agent or pool that triages the project's open issues; unset takes the node's default triage Agent, then the worker."),
		"triage_prompt_append": map[string]interface{}{
			"type":        "string",
			"description": "Prompt material appended after the triage charter for every triage session of this project; at most 64 KiB",
		},
		"verification": map[string]interface{}{
			"type":        "array",
			"description": "The commands that must pass before the work is offered for review, in order",
			"items":       map[string]interface{}{"type": "string"},
		},
		"max_review_rounds": map[string]interface{}{
			"type":        "integer",
			"description": "How many times review may send the work back before escalation; zero takes the default",
		},
		"review": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"required_approvals":           map[string]interface{}{"type": "integer"},
				"require_independent_reviewer": map[string]interface{}{"type": "boolean"},
				"require_green_checks":         map[string]interface{}{"type": "boolean"},
				"checks_settle_window":         map[string]interface{}{"type": "integer", "description": "nanoseconds; zero takes the default"},
				"reviewers":                    map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{"type": "string"}},
			},
		},
		"reviewer": sdlcReviewerSchema,
		"reviewers": map[string]interface{}{
			"type":        "array",
			"description": "Alternate reviewer profiles, chosen per pull request by an operator override; advisory unless counts_toward_merge is true",
			"items":       sdlcReviewerSchema,
		},
		"ranking": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"default_priority":    map[string]interface{}{"type": "integer"},
				"analyzer_name":       map[string]interface{}{"type": "string"},
				"declared_dependents": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
		},
		"merge_train": map[string]interface{}{
			"type":        "object",
			"description": "The merge train: approved pull requests land together, CI running once per train. On by default for a self-reviewed project.",
			"properties": map[string]interface{}{
				"disabled": map[string]interface{}{"type": "boolean", "description": "Merge approved pull requests one at a time instead"},
				"max_size": map[string]interface{}{"type": "integer", "description": "Changes per train; zero takes the default (5)"},
			},
		},
		"rulers": map[string]interface{}{
			"type":        "array",
			"description": "Forge logins, beyond the repository owner and the node's configured rulers, whose comment on a parked issue is an owner ruling. Each must be the owner or a collaborator of the repository (see sdlc_collaborators).",
			"items":       map[string]interface{}{"type": "string"},
		},
		"clustering": map[string]interface{}{
			"type":        "object",
			"description": "Issue clustering: triage adopts related ready issues as one task and one pull request. On by default.",
			"properties": map[string]interface{}{
				"disabled": map[string]interface{}{"type": "boolean", "description": "Make every ready issue its own task"},
				"max_size": map[string]interface{}{"type": "integer", "description": "Issues per task, the anchor included; zero takes the default (4)"},
			},
		},
	},
}

// registerSDLCTools registers the SDLC module's tools.
func (r *ToolRegistry) registerSDLCTools() {
	tenant := map[string]interface{}{"type": "string", "description": "Tenant ID (defaults to the caller's own)"}
	r.addTool(ToolDefinition{
		Name:        "sdlc_bind",
		Description: "Attach a forge binding to an orchestration project so that issues carrying the binding's trigger label become tasks the Software Factory branches, implements, reviews and merges.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string", "description": "The orchestration project"},
				"binding":    sdlcBindingSchema,
				"paused": map[string]interface{}{
					"type":        "boolean",
					"description": "Bind without switching the project's autonomy on; it never turns autonomy off",
				},
			},
			"required": []string{"project_id", "binding"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        "sdlc_unbind",
		Description: "Detach a project's forge binding.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"project_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        "sdlc_binding",
		Description: "Read a project's forge binding.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"project_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        "sdlc_delivery",
		Description: "Read what a task's attempt delivered: branch, head, pull request, reviews and checks.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": tenant,
				"task_id":   map[string]interface{}{"type": "string"},
			},
			"required": []string{"task_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "sdlc_onboard",
		Description: "Provision a bound project's repository on its forge — the webhook that delivers to this node and the label vocabulary the binding declares. " +
			"Idempotent: a repository already in the desired state is left alone, and one whose webhook was deleted by hand gets it back. " +
			"This is the repair for a conformance finding; sdlc_conformance only reports.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"project_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "sdlc_project_onboard",
		Description: "Onboard a forge repository like an existing bound project: create its project and webhook secret, bind it and provision its forge. " +
			"Idempotent: every step checks before it acts, so a second run reports every step unchanged. " +
			"What it may not do itself — a missing forge token or signing key, an unprotected branch, a stale workflow — it reports as a finding with its remedy.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": tenant,
				"repo":      map[string]interface{}{"type": "string", "description": "The repository's name on the template's forge and owner"},
				"like":      map[string]interface{}{"type": "string", "description": "The ID of the bound project the repository is onboarded like"},
				"max_concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "The new project's concurrent job limit; zero takes the template's",
				},
				"paused": map[string]interface{}{
					"type":        "boolean",
					"description": "Bind without switching the new project's autonomy on",
				},
				"preflight": map[string]interface{}{
					"type":        "boolean",
					"description": "Run the verification commands once on the default branch in the worker image; autonomy is switched on only when they pass",
				},
			},
			"required": []string{"repo", "like"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "sdlc_conformance",
		Description: "Report whether a project is correctly onboarded and, if not, exactly what is wrong: a missing or misdirected webhook, a label the repository does not define, " +
			"an orchestration workflow still committed to the repository, or a protected branch that does not require the status contexts quicbaas writes. " +
			"Read-only: it changes nothing.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"project_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "sdlc_open_work",
		Description: "List a bound project's open work on its forge: the open pull requests the project's agent opened (number, title, head branch, CI state, review state, age) " +
			"and the open issues carrying a label the factory writes (number, title, labels, age). Read-only: it changes nothing.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"project_id"},
		},
	})
	pullRequest := func() map[string]interface{} {
		return map[string]interface{}{
			"tenant_id":    tenant,
			"project_id":   map[string]interface{}{"type": "string"},
			"pull_request": map[string]interface{}{"type": "integer", "description": "The pull request number"},
		}
	}
	r.addTool(ToolDefinition{
		Name:        "sdlc_reviewer",
		Description: "Report which reviewer profile reviews a pull request: the operator's override when one is set, the binding's default otherwise, and whether its verdict counts toward merge.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": pullRequest(),
			"required":   []string{"project_id", "pull_request"},
		},
	})
	setProps := pullRequest()
	setProps["profile"] = map[string]interface{}{"type": "string", "description": "A reviewer profile the project's binding carries"}
	r.addTool(ToolDefinition{
		Name: "sdlc_reviewer_set",
		Description: "Choose which of the binding's reviewer profiles reviews one pull request. An operator verb: nothing a forge sends can set it. " +
			"It never cancels anything: a review already running finishes, and every new review dispatch uses the chosen profile. " +
			"An advisory profile's verdict is shown and never counted toward merge.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": setProps,
			"required":   []string{"project_id", "pull_request", "profile"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        "sdlc_reviewer_clear",
		Description: "Clear a pull request's reviewer override, so the binding's default reviewer reviews it again.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": pullRequest(),
			"required":   []string{"project_id", "pull_request"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "sdlc_issue_create",
		Description: "File an issue on a bound project's repository through the binding's own forge token, e.g. to split an issue into follow-ups. " +
			"Labels must already exist on the repository (task:ready hands the issue straight to the factory); parent links the issue it was split out of. Audited.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
				"title":      map[string]interface{}{"type": "string", "description": "One line, at most 255 characters"},
				"body":       map[string]interface{}{"type": "string"},
				"labels":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"parent":     map[string]interface{}{"type": "integer", "description": "The issue this one was split out of; omit for none"},
			},
			"required": []string{"project_id", "title"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        "sdlc_collaborators",
		Description: "List the accounts that may rule on a bound project's repository: its owner, then every collaborator. The candidates for a binding's rulers. Read-only.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":  tenant,
				"project_id": map[string]interface{}{"type": "string"},
			},
			"required": []string{"project_id"},
		},
	})
	r.addTool(ToolDefinition{
		Name: "sdlc_adopt_issue",
		Description: "Hand one forge issue back to the Software Factory: create its task, take its holds off (needs-info, escalated, blocked, the portfolio hold) and put the trigger label on. " +
			"Name an open pull request to resume it: the task works on that pull request's own head branch, never a new one cut from the issue title. " +
			"A ruling is appended to the task's contract and posted on the issue. Refused while a live task still holds the issue — readmit that one instead.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id":    tenant,
				"project_id":   map[string]interface{}{"type": "string"},
				"issue":        map[string]interface{}{"type": "integer", "description": "The issue number"},
				"pull_request": map[string]interface{}{"type": "integer", "description": "An open pull request the task resumes, by number"},
				"ruling":       map[string]interface{}{"type": "string", "description": "The operator's instruction to the worker"},
			},
			"required": []string{"project_id", "issue"},
		},
	})
	r.addTool(ToolDefinition{
		Name:        "sdlc_forges",
		Description: "List the forge platforms this build can talk to, so an unavailable platform can be shown disabled rather than hidden.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
	r.addTool(ToolDefinition{
		Name: "quicbaas_sdlc_worker_image",
		Description: "Report the Software Factory worker image refresh on this node: whether its loop is serving, whether a pass is running or queued, " +
			"and what the last pass did — the npm versions it worked towards, the candidate it built, every probe and smoke of its gate, the reference " +
			"bindings were rebound to and any binding it could not rebind. Read-only; system operators only.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
	r.addTool(ToolDefinition{
		Name: "quicbaas_sdlc_worker_image_refresh",
		Description: "Queue a full worker image refresh pass now: rebuild the worker image if the npm registry has newer Claude Code or Codex CLIs than the bound " +
			"image carries, gate it, publish it and rebind every binding naming a stale managed image. Returns at once; poll quicbaas_sdlc_worker_image for the outcome. " +
			"System administrators only.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
}

// sdlcTools dispatches the module's tools.
var sdlcTools = map[string]func(*ToolRegistry, context.Context, map[string]interface{}) (interface{}, error){
	"sdlc_bind":            (*ToolRegistry).executeSDLCBind,
	"sdlc_unbind":          (*ToolRegistry).executeSDLCUnbind,
	"sdlc_binding":         (*ToolRegistry).executeSDLCBinding,
	"sdlc_delivery":        (*ToolRegistry).executeSDLCDelivery,
	"sdlc_forges":          (*ToolRegistry).executeSDLCForges,
	"sdlc_onboard":         (*ToolRegistry).executeSDLCOnboard,
	"sdlc_project_onboard": (*ToolRegistry).executeSDLCProjectOnboard,
	"sdlc_conformance":     (*ToolRegistry).executeSDLCConformance,
	"sdlc_open_work":       (*ToolRegistry).executeSDLCOpenWork,

	"sdlc_reviewer":                      (*ToolRegistry).executeSDLCReviewer,
	"sdlc_reviewer_set":                  (*ToolRegistry).executeSDLCReviewerSet,
	"sdlc_reviewer_clear":                (*ToolRegistry).executeSDLCReviewerClear,
	"sdlc_issue_create":                  (*ToolRegistry).executeSDLCIssueCreate,
	"sdlc_collaborators":                 (*ToolRegistry).executeSDLCCollaborators,
	"sdlc_adopt_issue":                   (*ToolRegistry).executeSDLCAdoptIssue,
	"quicbaas_sdlc_worker_image":         (*ToolRegistry).executeSDLCWorkerImage,
	"quicbaas_sdlc_worker_image_refresh": (*ToolRegistry).executeSDLCWorkerImageRefresh,
}

// isSDLCTool reports whether name is one of the module's tools.
func isSDLCTool(name string) bool {
	_, ok := sdlcTools[name]
	return ok
}

// executeSDLCTool dispatches one of the module's tools.
func (r *ToolRegistry) executeSDLCTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if h, ok := sdlcTools[name]; ok {
		return h(r, ctx, args)
	}
	return nil, api.NewUnknownToolError(name)
}

// getSDLCService resolves the module's binding service.
func (r *ToolRegistry) getSDLCService() (*service.SDLCService, error) {
	svc := r.service.SDLC()
	if svc == nil {
		return nil, api.NewServiceUnavailableError("sdlc")
	}
	return svc, nil
}

func (r *ToolRegistry) executeSDLCBind(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	raw, ok := args["binding"]
	if !ok || raw == nil {
		return nil, &sdlc.InvalidInputError{Field: "binding", Message: "is required"}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, &sdlc.InvalidInputError{Field: "binding", Message: err.Error()}
	}
	var binding sdlc.ProjectBinding
	if err := json.Unmarshal(data, &binding); err != nil {
		return nil, &sdlc.InvalidInputError{Field: "binding", Message: err.Error()}
	}
	if binding.ProjectID == "" {
		binding.ProjectID = getString(args, "project_id")
	}
	paused := false
	if raw, present := args["paused"]; present {
		if paused, ok = raw.(bool); !ok {
			return nil, &sdlc.InvalidInputError{Field: "paused", Message: "must be a boolean"}
		}
	}
	return svc.Bind(ctx, getString(args, "tenant_id"), &binding, service.BindOptions{LeavePaused: paused})
}

func (r *ToolRegistry) executeSDLCUnbind(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	projectID := getString(args, "project_id")
	if err := svc.Unbind(ctx, getString(args, "tenant_id"), projectID); err != nil {
		return nil, err
	}
	return map[string]interface{}{"project_id": projectID, "unbound": true}, nil
}

func (r *ToolRegistry) executeSDLCBinding(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.Binding(ctx, getString(args, "tenant_id"), getString(args, "project_id"))
}

func (r *ToolRegistry) executeSDLCDelivery(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.Delivery(ctx, getString(args, "tenant_id"), getString(args, "task_id"))
}

// executeSDLCOnboard answers sdlc_onboard: the explicit repair verb.
func (r *ToolRegistry) executeSDLCOnboard(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.Onboard(ctx, getString(args, "tenant_id"), getString(args, "project_id"))
}

// executeSDLCProjectOnboard answers sdlc_project_onboard: a repository
// onboarded like a bound project.
func (r *ToolRegistry) executeSDLCProjectOnboard(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil, &sdlc.InvalidInputError{Field: "arguments", Message: err.Error()}
	}
	var req sdlc.RepoOnboardRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &sdlc.InvalidInputError{Field: "arguments", Message: err.Error()}
	}
	return svc.OnboardRepo(ctx, getString(args, "tenant_id"), req)
}

// executeSDLCConformance answers sdlc_conformance and changes nothing.
func (r *ToolRegistry) executeSDLCConformance(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.Conformance(ctx, getString(args, "tenant_id"), getString(args, "project_id"))
}

// executeSDLCOpenWork answers sdlc_open_work and changes nothing.
func (r *ToolRegistry) executeSDLCOpenWork(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.OpenWork(ctx, getString(args, "tenant_id"), getString(args, "project_id"))
}

// executeSDLCForges answers sdlc_forges: the forge adapters this build
// carries.
//
// BUILD capability, not tenant data, so no tenant is derived — matching the
// REST, gRPC and embedded handlers. It is still gated on the module running,
// so a node without it answers unavailable rather than an empty list.
func (r *ToolRegistry) executeSDLCForges(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	if _, err := r.getSDLCService(); err != nil {
		return nil, err
	}
	// RegisteredForgeTypes returns its types sorted, so two reads serialize
	// identically without a second sort here.
	registered := sdlc.RegisteredForgeTypes()
	forges := make([]string, 0, len(registered))
	for _, forgeType := range registered {
		forges = append(forges, string(forgeType))
	}
	return map[string]interface{}{"forges": forges, "count": len(forges)}, nil
}

// executeSDLCReviewer answers sdlc_reviewer: which profile reviews one pull
// request. It changes nothing.
func (r *ToolRegistry) executeSDLCReviewer(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.ReviewerSelection(ctx, getString(args, "tenant_id"), getString(args, "project_id"), getInt(args, "pull_request"))
}

// executeSDLCReviewerSet answers sdlc_reviewer_set: an operator choosing the
// reviewer profile for one pull request.
func (r *ToolRegistry) executeSDLCReviewerSet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.SetReviewerOverride(ctx, getString(args, "tenant_id"), getString(args, "project_id"),
		getInt(args, "pull_request"), getString(args, "profile"))
}

// executeSDLCReviewerClear answers sdlc_reviewer_clear: the binding's default
// reviews the pull request again.
func (r *ToolRegistry) executeSDLCReviewerClear(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.ClearReviewerOverride(ctx, getString(args, "tenant_id"), getString(args, "project_id"), getInt(args, "pull_request"))
}

// executeSDLCAdoptIssue answers sdlc_adopt_issue: the issue's task is created
// and the issue handed back to the factory.
func (r *ToolRegistry) executeSDLCAdoptIssue(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.AdoptIssue(ctx, getString(args, "tenant_id"), sdlc.AdoptIssueRequest{
		ProjectID:   getString(args, "project_id"),
		Issue:       getInt(args, "issue"),
		PullRequest: getInt(args, "pull_request"),
		Ruling:      getString(args, "ruling"),
	})
}

// executeSDLCWorkerImage answers quicbaas_sdlc_worker_image: the refresh loop's state
// and its last pass. A host verb, so no tenant is derived.
func (r *ToolRegistry) executeSDLCWorkerImage(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.WorkerImageStatus(ctx)
}

// executeSDLCWorkerImageRefresh answers quicbaas_sdlc_worker_image_refresh: queue a
// pass and report the status it left behind.
func (r *ToolRegistry) executeSDLCWorkerImageRefresh(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.RefreshWorkerImage(ctx)
}

// executeSDLCIssueCreate answers sdlc_issue_create: an operator filing an
// issue on the project's bound repository.
func (r *ToolRegistry) executeSDLCIssueCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	labels, err := sdlcStringList(args, "labels")
	if err != nil {
		return nil, err
	}
	return svc.FileIssue(ctx, getString(args, "tenant_id"), getString(args, "project_id"), sdlc.IssueFiling{
		Title: getString(args, "title"), Body: getString(args, "body"), Labels: labels, Parent: getInt(args, "parent"),
	})
}

// executeSDLCCollaborators answers sdlc_collaborators and changes nothing.
func (r *ToolRegistry) executeSDLCCollaborators(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	users, err := svc.Collaborators(ctx, getString(args, "tenant_id"), getString(args, "project_id"))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"users": users, "count": len(users)}, nil
}

// sdlcStringList reads an optional list of strings, refusing any other shape
// rather than dropping what it cannot read.
func sdlcStringList(args map[string]interface{}, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, &sdlc.InvalidInputError{Field: key, Message: "must be a list of strings"}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, &sdlc.InvalidInputError{Field: key, Message: "must be a list of strings"}
		}
		out = append(out, value)
	}
	return out, nil
}
