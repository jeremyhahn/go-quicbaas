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
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPOrchestratorService returns true if the service is an Orchestrator service.
func isMCPOrchestratorService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Orchestrator"
}

// orchestratorMCPHandler transforms MCP tool responses back into strongly-typed
// SDK response types for the Orchestrator service.
type orchestratorMCPHandler struct{}

// newOrchestratorMCPHandler creates a new orchestrator MCP response handler.
func newOrchestratorMCPHandler() *orchestratorMCPHandler {
	return &orchestratorMCPHandler{}
}

// orchestratorMCPTransform decodes one MCP tool result into its SDK type.
type orchestratorMCPTransform func(raw []byte) (interface{}, error)

// orchestratorMCPTransforms maps Orchestrator methods whose MCP tool result
// shape differs from the SDK response type to their decoder, for O(1)
// dispatch. Methods absent from the map need no transformation.
var orchestratorMCPTransforms = map[string]orchestratorMCPTransform{
	"PromptSession": decodeMCPPromptSession,
	"SessionStatus": decodeMCPSessionStatus,
	"CloseSession":  decodeMCPCloseSession,
	"ListSessions":  decodeMCPListSessions,
}

// transformResponse converts the raw MCP tool result to the expected SDK type
// for each Orchestrator method. The session tools serialize the service's own
// wire shape (content blocks, "state", ...), which shares no field names with
// the flat SDK response types, so those results are routed through the shared
// converters in the orchestrator client package. For Inference the tool
// returns a JSON-encoded map; we re-encode and decode into InferenceResponse.
func (h *orchestratorMCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	if method == "Inference" {
		return h.parseInferenceResponse(result)
	}

	transform, ok := orchestratorMCPTransforms[method]
	if !ok {
		// Other Orchestrator methods do not need special handling.
		return result, nil
	}

	raw, err := encodeMCPResult(result, "mcp.orchestrator."+method)
	if err != nil {
		return nil, err
	}
	return transform(raw)
}

// encodeMCPResult normalizes an MCP tool result into raw JSON bytes. The tool
// may return a string (raw JSON) or an already-decoded map.
func encodeMCPResult(result interface{}, op string) ([]byte, error) {
	if result == nil {
		return nil, errors.New(errors.CodeInternal, op, "nil result from MCP tool")
	}
	if s, ok := result.(string); ok {
		return []byte(s), nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to re-encode MCP result")
	}
	return raw, nil
}

// decodeMCPPromptSession decodes quicbaas_orchestrator_prompt output.
func decodeMCPPromptSession(raw []byte) (interface{}, error) {
	var wire orchestrator.PromptSessionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.PromptSession", "failed to decode MCP prompt result")
	}
	return wire.Response()
}

// decodeMCPSessionStatus decodes quicbaas_orchestrator_status output.
func decodeMCPSessionStatus(raw []byte) (interface{}, error) {
	var wire orchestrator.SessionStatusWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.SessionStatus", "failed to decode MCP session status result")
	}
	return wire.Response(), nil
}

// decodeMCPCloseSession decodes quicbaas_orchestrator_close output.
func decodeMCPCloseSession(raw []byte) (interface{}, error) {
	var wire orchestrator.CloseSessionWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.CloseSession", "failed to decode MCP close session result")
	}
	return wire.Response(), nil
}

// decodeMCPListSessions decodes quicbaas_orchestrator_list_sessions output.
func decodeMCPListSessions(raw []byte) (interface{}, error) {
	var wire orchestrator.ListSessionsWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.ListSessions", "failed to decode MCP list sessions result")
	}
	return wire.Response(), nil
}

// parseInferenceResponse converts the MCP tool result for
// quicbaas_orchestrator_inference into *orchestrator.InferenceResponse.
// The MCP tool serialises the result as a JSON string; we unmarshal it here.
func (h *orchestratorMCPHandler) parseInferenceResponse(result interface{}) (*orchestrator.InferenceResponse, error) {
	raw, err := encodeMCPResult(result, "mcp.orchestrator.parseInferenceResponse")
	if err != nil {
		return nil, err
	}

	// The MCP tool result structure wraps the response in a top-level object
	// with "content", "text", "model", and "stop_reason" fields.
	var toolResult struct {
		Content    []orchestrator.InferenceContentBlock `json:"content"`
		Text       string                               `json:"text"`
		Model      string                               `json:"model"`
		StopReason string                               `json:"stop_reason"`
	}
	if err := json.Unmarshal(raw, &toolResult); err != nil {
		// If the tool returned something we cannot parse, surface it as an error.
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.parseInferenceResponse", "failed to decode MCP inference result")
	}

	return &orchestrator.InferenceResponse{
		Content:    toolResult.Content,
		Model:      toolResult.Model,
		StopReason: toolResult.StopReason,
	}, nil
}
