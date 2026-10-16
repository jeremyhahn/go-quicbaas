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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// promptAddressSchema is the input every prompt tool shares: the prompt and
// the scope it is addressed at.
func promptAddressSchema(withName bool) map[string]interface{} {
	properties := map[string]interface{}{
		"tenant_id": map[string]interface{}{"type": "string", "description": "Tenant ID (defaults to the caller's own)"},
		"scope": map[string]interface{}{
			"type":        "string",
			"enum":        []string{"tenant", "project", "system"},
			"description": "The layer addressed; system is read-only. Defaults to tenant, or project when project_id is set",
		},
		"project_id": map[string]interface{}{"type": "string", "description": "The project, for the project scope"},
		"set":        map[string]interface{}{"type": "string", "description": "The prompt set"},
	}
	required := []string{"set"}
	if withName {
		properties["name"] = map[string]interface{}{"type": "string", "description": "The prompt's name within the set"}
		required = append(required, "name")
	}
	return map[string]interface{}{"type": "object", "properties": properties, "required": required}
}

// withPromptProperties adds properties to a prompt address schema and marks
// the listed ones required.
func withPromptProperties(schema map[string]interface{}, properties map[string]interface{}, required ...string) map[string]interface{} {
	props, _ := schema["properties"].(map[string]interface{})
	for name, property := range properties {
		props[name] = property
	}
	need, _ := schema["required"].([]string)
	schema["required"] = append(need, required...)
	return schema
}

func (r *ToolRegistry) registerPromptTools() {
	expected := map[string]interface{}{"type": "integer", "description": "The record's current version; 0 when none exists at this scope"}
	note := map[string]interface{}{"type": "string", "description": "Why the change is made; kept in the history"}
	r.addTool(ToolDefinition{
		Name:        "prompt_list",
		Description: "List a prompt set's prompts at one scope: whether each is overridden there, its version and the body a job there runs with.",
		InputSchema: promptAddressSchema(false),
	})
	r.addTool(ToolDefinition{
		Name:        "prompt_get",
		Description: "Read the record overridden at one scope, or one of its revisions by version. Fails not-found when nothing is overridden there.",
		InputSchema: withPromptProperties(promptAddressSchema(true), map[string]interface{}{
			"version": map[string]interface{}{"type": "integer", "description": "A revision; omit for the current record"},
		}),
	})
	r.addTool(ToolDefinition{
		Name:        "prompt_effective",
		Description: "Resolve the body a job at one scope runs with, the layer it came from and the whole resolution chain.",
		InputSchema: promptAddressSchema(true),
	})
	r.addTool(ToolDefinition{
		Name:        "prompt_diff",
		Description: "Diff the body a job at one scope runs with against the body its parent scope resolves to.",
		InputSchema: promptAddressSchema(true),
	})
	r.addTool(ToolDefinition{
		Name:        "prompt_history",
		Description: "List a record's revisions: version, hash, author, note and time. Bodies are read with prompt_get and a version.",
		InputSchema: promptAddressSchema(true),
	})
	r.addTool(ToolDefinition{
		Name: "prompt_put",
		Description: "Override a prompt at the tenant or project scope. The body must keep every marker the prompt declares; " +
			"expected_version guards against a concurrent edit.",
		InputSchema: withPromptProperties(promptAddressSchema(true), map[string]interface{}{
			"content":          map[string]interface{}{"type": "string", "description": "The new body"},
			"note":             note,
			"expected_version": expected,
		}, "content", "note"),
	})
	r.addTool(ToolDefinition{
		Name:        "prompt_rollback",
		Description: "Write a new revision carrying an earlier revision's body.",
		InputSchema: withPromptProperties(promptAddressSchema(true), map[string]interface{}{
			"to_version":       map[string]interface{}{"type": "integer", "description": "The revision whose body is restored"},
			"note":             note,
			"expected_version": expected,
		}, "to_version", "expected_version"),
	})
	r.addTool(ToolDefinition{
		Name:        "prompt_reset",
		Description: "Make a scope follow its parent again; the history is kept.",
		InputSchema: withPromptProperties(promptAddressSchema(true), map[string]interface{}{
			"note":             note,
			"expected_version": expected,
		}, "expected_version"),
	})
}

var promptTools = map[string]func(*ToolRegistry, context.Context, map[string]interface{}) (interface{}, error){
	"prompt_list":      (*ToolRegistry).executePromptList,
	"prompt_get":       (*ToolRegistry).executePromptGet,
	"prompt_effective": (*ToolRegistry).executePromptEffective,
	"prompt_diff":      (*ToolRegistry).executePromptDiff,
	"prompt_history":   (*ToolRegistry).executePromptHistory,
	"prompt_put":       (*ToolRegistry).executePromptPut,
	"prompt_rollback":  (*ToolRegistry).executePromptRollback,
	"prompt_reset":     (*ToolRegistry).executePromptReset,
}

// isPromptTool reports whether name is one of the prompt tools.
func isPromptTool(name string) bool {
	_, ok := promptTools[name]
	return ok
}

// executePromptTool dispatches one of the prompt tools.
func (r *ToolRegistry) executePromptTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if h, ok := promptTools[name]; ok {
		return h(r, ctx, args)
	}
	return nil, api.NewUnknownToolError(name)
}

// promptTarget resolves the mounted prompt service and the prompt a call
// names, in the caller's own tenant; naming another tenant is refused.
func (r *ToolRegistry) promptTarget(ctx context.Context, args map[string]interface{}) (*service.PromptService, service.PromptAddress, error) {
	svc := r.service.Prompts()
	if svc == nil {
		return nil, service.PromptAddress{}, service.ErrPromptsUnavailable
	}
	tenantID, err := tenantctx.Target(ctx, "mcp.Prompts", getString(args, "tenant_id"))
	if err != nil {
		return nil, service.PromptAddress{}, err
	}
	return svc, service.PromptAddress{
		TenantID:  tenantID,
		Scope:     getString(args, "scope"),
		ProjectID: getString(args, "project_id"),
		Set:       getString(args, "set"),
		Name:      getString(args, "name"),
	}, nil
}

func (r *ToolRegistry) executePromptList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	return svc.List(ctx, &service.ListPromptsRequest{
		TenantID: addr.TenantID, Scope: addr.Scope, ProjectID: addr.ProjectID, Set: addr.Set,
	})
}

func (r *ToolRegistry) executePromptGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	ints := newIntArgReader(args)
	version := ints.Int64("version")
	if err := ints.Err(); err != nil {
		return nil, err
	}
	return svc.Get(ctx, &service.GetPromptRequest{PromptAddress: addr, Version: version})
}

func (r *ToolRegistry) executePromptEffective(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	return svc.Effective(ctx, &addr)
}

func (r *ToolRegistry) executePromptDiff(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	return svc.Diff(ctx, &addr)
}

func (r *ToolRegistry) executePromptHistory(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	return svc.History(ctx, &addr)
}

func (r *ToolRegistry) executePromptPut(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	ints := newIntArgReader(args)
	expected := ints.Int64("expected_version")
	if err := ints.Err(); err != nil {
		return nil, err
	}
	return svc.Put(ctx, &service.PutPromptRequest{
		PromptAddress: addr, Content: getString(args, "content"), Note: getString(args, "note"), ExpectedVersion: expected,
	})
}

func (r *ToolRegistry) executePromptRollback(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	ints := newIntArgReader(args)
	toVersion := ints.Int64("to_version")
	expected := ints.Int64("expected_version")
	if err := ints.Err(); err != nil {
		return nil, err
	}
	return svc.Rollback(ctx, &service.RollbackPromptRequest{
		PromptAddress: addr, ToVersion: toVersion, Note: getString(args, "note"), ExpectedVersion: expected,
	})
}

func (r *ToolRegistry) executePromptReset(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	svc, addr, err := r.promptTarget(ctx, args)
	if err != nil {
		return nil, err
	}
	ints := newIntArgReader(args)
	expected := ints.Int64("expected_version")
	if err := ints.Err(); err != nil {
		return nil, err
	}
	return svc.Reset(ctx, &service.ResetPromptRequest{
		PromptAddress: addr, Note: getString(args, "note"), ExpectedVersion: expected,
	})
}
