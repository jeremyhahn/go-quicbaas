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

// Package mcp — credential management tools exposed over MCP.
//
// The qrdb MCP transport (sdk/go/transport/mcp) calls tools named
// "qrdb_credential_*" with a JSON-text response shape. To keep that surface
// reachable through quicbaasd's MCP listener, these tools mirror the qrdb
// tool names and response payloads exactly. Each tool delegates to a
// CredentialServicer wired by lifecycle via SetCredentialService.

package mcp

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// CredentialServiceUnavailableError is returned by every credential MCP
// tool when the credential service has not been wired by the lifecycle
// layer. Mirrors the BarrierServiceUnavailableError convention so callers
// can type-assert and surface a stable error message.
type CredentialServiceUnavailableError struct{}

// Error implements the error interface.
func (*CredentialServiceUnavailableError) Error() string {
	return "credential service not configured"
}

// CredentialNameRequiredError is returned by credential MCP tools whose
// schema lists "name" as required but the caller omitted it.
type CredentialNameRequiredError struct{}

// Error implements the error interface.
func (*CredentialNameRequiredError) Error() string {
	return "name is required"
}

// CredentialValueRequiredError is returned by credential MCP tools whose
// schema lists "value" as required but the caller omitted it.
type CredentialValueRequiredError struct{}

// Error implements the error interface.
func (*CredentialValueRequiredError) Error() string {
	return "value is required"
}

// CredentialNewValueRequiredError is returned by qrdb_credential_rotate
// when the caller omits the new_value argument.
type CredentialNewValueRequiredError struct{}

// Error implements the error interface.
func (*CredentialNewValueRequiredError) Error() string {
	return "new_value is required"
}

// =============================================================================
// Tool registration
// =============================================================================

// registerCredentialTools adds every qrdb_credential_* tool definition to
// the registry. The tools are dispatched via isCredentialTool /
// executeCredentialTool, mirroring the barrier/ceremony tool wiring.
func (r *ToolRegistry) registerCredentialTools() {
	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_submit",
		Description: "Submit a named credential value, notifying any blocked retrievers for the manual strategy.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":  map[string]interface{}{"type": "string", "description": "Credential name"},
				"value": map[string]interface{}{"type": "string", "description": "Credential value"},
			},
			"required": []string{"name", "value"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_strategy",
		Description: "Return the active credential management strategy name.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_delete",
		Description: "Delete a named credential and all of its non-current historical versions.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Credential name"},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_list",
		Description: "List the sorted set of stored credential names. Values are never returned.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_rotate",
		Description: "Atomically rotate a credential to a new value, optionally preserving the previous version for a grace period.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":                 map[string]interface{}{"type": "string", "description": "Credential name"},
				"new_value":            map[string]interface{}{"type": "string", "description": "New credential value"},
				"grace_period_seconds": map[string]interface{}{"type": "integer", "description": "Seconds the previous version remains retrievable (0 = no grace)"},
			},
			"required": []string{"name", "new_value"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_retrieve_version",
		Description: "Retrieve a specific historical version of a credential. Version 0 means the current version.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":    map[string]interface{}{"type": "string", "description": "Credential name"},
				"version": map[string]interface{}{"type": "integer", "description": "Version number (0 = current)"},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_list_versions",
		Description: "Return the version history (current + non-expired grace versions) for a credential. Values are never returned.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Credential name"},
			},
			"required": []string{"name"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_credential_gc_expired",
		Description: "Reap any historical versions of a credential whose grace period has elapsed.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{"type": "string", "description": "Credential name"},
			},
			"required": []string{"name"},
		},
	})
}

// =============================================================================
// Membership + dispatch
// =============================================================================

// credentialToolNames carries every qrdb_credential_* tool name handled
// by executeCredentialTool. Membership is used by isCredentialTool to
// route dispatch from the registry's default branch.
var credentialToolNames = map[string]struct{}{
	"qrdb_credential_submit":           {},
	"qrdb_credential_strategy":         {},
	"qrdb_credential_delete":           {},
	"qrdb_credential_list":             {},
	"qrdb_credential_rotate":           {},
	"qrdb_credential_retrieve_version": {},
	"qrdb_credential_list_versions":    {},
	"qrdb_credential_gc_expired":       {},
}

// isCredentialTool reports whether the given tool name belongs to the
// credential management surface owned by executeCredentialTool.
func isCredentialTool(name string) bool {
	_, ok := credentialToolNames[name]
	return ok
}

// credentialToolFunc is the signature of a per-tool dispatcher. The
// dispatch table below keeps executeCredentialTool branch-free.
type credentialToolFunc func(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error)

// credentialToolDispatch maps tool name -> handler. Lookup is O(1).
var credentialToolDispatch = map[string]credentialToolFunc{
	"qrdb_credential_submit":           executeCredentialSubmit,
	"qrdb_credential_strategy":         executeCredentialStrategy,
	"qrdb_credential_delete":           executeCredentialDelete,
	"qrdb_credential_list":             executeCredentialList,
	"qrdb_credential_rotate":           executeCredentialRotate,
	"qrdb_credential_retrieve_version": executeCredentialRetrieveVersion,
	"qrdb_credential_list_versions":    executeCredentialListVersions,
	"qrdb_credential_gc_expired":       executeCredentialGCExpired,
}

// executeCredentialTool dispatches a credential tool by name. Mirrors the
// barrier/ceremony tool wiring: the credential service is read from the
// registry field without locking — lifecycle wiring is single-shot before
// the listener starts accepting traffic, so no race exists in practice.
func (r *ToolRegistry) executeCredentialTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.credentialSvc
	if svc == nil {
		return nil, &CredentialServiceUnavailableError{}
	}
	handler, ok := credentialToolDispatch[name]
	if !ok {
		// Should be unreachable when isCredentialTool gated the call.
		return nil, &CredentialServiceUnavailableError{}
	}
	return handler(ctx, svc, args)
}

// =============================================================================
// Per-tool handlers
// =============================================================================

func executeCredentialSubmit(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, &CredentialNameRequiredError{}
	}
	value := getString(args, "value")
	if value == "" {
		return nil, &CredentialValueRequiredError{}
	}
	if err := svc.CredentialSubmit(ctx, name, []byte(value)); err != nil {
		return nil, err
	}
	return "credential submitted", nil
}

func executeCredentialStrategy(_ context.Context, svc qrdbsdk.ServiceCredentialServicer, _ map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"strategy": svc.CredentialStrategy(),
	}, nil
}

func executeCredentialDelete(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, &CredentialNameRequiredError{}
	}
	if err := svc.CredentialDelete(ctx, name); err != nil {
		return nil, err
	}
	return "credential deleted", nil
}

func executeCredentialList(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, _ map[string]interface{}) (interface{}, error) {
	names, err := svc.CredentialList(ctx)
	if err != nil {
		return nil, err
	}
	if names == nil {
		names = []string{}
	}
	return map[string]interface{}{"names": names}, nil
}

func executeCredentialRotate(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, &CredentialNameRequiredError{}
	}
	newValue := getString(args, "new_value")
	if newValue == "" {
		return nil, &CredentialNewValueRequiredError{}
	}
	graceSecs := getInt64(args, "grace_period_seconds")
	result, err := svc.CredentialRotate(ctx, name, qrdbsdk.RotateOptions{
		NewValue:    []byte(newValue),
		GracePeriod: time.Duration(graceSecs) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"name":             result.Name,
		"previous_version": result.PreviousVersion,
		"new_version":      result.NewVersion,
	}
	if !result.GraceUntil.IsZero() {
		out["grace_until_unix"] = result.GraceUntil.Unix()
	}
	return out, nil
}

func executeCredentialRetrieveVersion(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, &CredentialNameRequiredError{}
	}
	version := getUint64(args, "version")
	buf, err := svc.CredentialRetrieveVersionBuffer(ctx, name, version)
	if err != nil {
		return nil, err
	}
	defer buf.Destroy()
	var value string
	if err := buf.WithBytes(func(b []byte) error {
		//secure:unwiped wire-boundary the MCP tool result is a JSON string, the one place the credential leaves protected memory.
		value = string(b)
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"value": value}, nil
}

func executeCredentialListVersions(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, &CredentialNameRequiredError{}
	}
	versions, err := svc.CredentialListVersions(ctx, name)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"name": name,
	}
	entries := make([]map[string]interface{}, 0, len(versions))
	for _, v := range versions {
		entry := map[string]interface{}{
			"version":    v.Version,
			"is_current": v.IsCurrent,
		}
		if !v.CreatedAt.IsZero() {
			entry["created_at_unix"] = v.CreatedAt.Unix()
		}
		if !v.GraceUntil.IsZero() {
			entry["grace_until_unix"] = v.GraceUntil.Unix()
		}
		entries = append(entries, entry)
	}
	out["versions"] = entries
	return out, nil
}

func executeCredentialGCExpired(ctx context.Context, svc qrdbsdk.ServiceCredentialServicer, args map[string]interface{}) (interface{}, error) {
	name := getString(args, "name")
	if name == "" {
		return nil, &CredentialNameRequiredError{}
	}
	reaped, err := svc.CredentialGCExpired(ctx, name)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"reaped": reaped}, nil
}

// =============================================================================
// Wiring
// =============================================================================

// SetCredentialService installs the qrdb credential management facade
// used by the credential MCP tools. Lifecycle wiring calls this once the
// credential service is online. Passing nil disables the surface
// (every tool returns CredentialServiceUnavailableError).
func (r *ToolRegistry) SetCredentialService(svc qrdbsdk.ServiceCredentialServicer) {
	r.credentialSvc = svc
}
