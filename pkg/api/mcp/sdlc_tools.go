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
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// SDLC module tools — forge bindings and deliveries
// =============================================================================

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
		"worker": map[string]interface{}{
			"type":        "object",
			"description": "The agent that writes the code",
			"properties": map[string]interface{}{
				"backend": map[string]interface{}{"type": "string", "description": "The ACP agent backend, e.g. claude"},
				"model":   map[string]interface{}{"type": "string", "description": "The model it runs; empty takes the backend default"},
				"image":   map[string]interface{}{"type": "string", "description": "The container image the worker runs in; naming one requires the sandbox"},
			},
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
		"ranking": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"default_priority":    map[string]interface{}{"type": "integer"},
				"analyzer_name":       map[string]interface{}{"type": "string"},
				"declared_dependents": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
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
		Name:        "sdlc_forges",
		Description: "List the forge platforms this build can talk to, so an unavailable platform can be shown disabled rather than hidden.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
}

// sdlcTools dispatches the module's tools.
var sdlcTools = map[string]func(*ToolRegistry, context.Context, map[string]interface{}) (interface{}, error){
	"sdlc_bind":        (*ToolRegistry).executeSDLCBind,
	"sdlc_unbind":      (*ToolRegistry).executeSDLCUnbind,
	"sdlc_binding":     (*ToolRegistry).executeSDLCBinding,
	"sdlc_delivery":    (*ToolRegistry).executeSDLCDelivery,
	"sdlc_forges":      (*ToolRegistry).executeSDLCForges,
	"sdlc_onboard":     (*ToolRegistry).executeSDLCOnboard,
	"sdlc_conformance": (*ToolRegistry).executeSDLCConformance,
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
	return svc.Bind(ctx, getString(args, "tenant_id"), &binding)
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

// executeSDLCConformance answers sdlc_conformance and changes nothing.
func (r *ToolRegistry) executeSDLCConformance(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, err := r.getSDLCService()
	if err != nil {
		return nil, err
	}
	return svc.Conformance(ctx, getString(args, "tenant_id"), getString(args, "project_id"))
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
