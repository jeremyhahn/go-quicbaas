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

// transformResponse converts the raw MCP tool result to the expected SDK type
// for each Orchestrator method. For Inference the tool returns a JSON-encoded
// map; we re-encode and decode into InferenceResponse.
func (h *orchestratorMCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	switch method {
	case "Inference":
		return h.parseInferenceResponse(result)
	default:
		// Other Orchestrator methods do not need special handling.
		return result, nil
	}
}

// parseInferenceResponse converts the MCP tool result for
// quicbaas_orchestrator_inference into *orchestrator.InferenceResponse.
// The MCP tool serialises the result as a JSON string; we unmarshal it here.
func (h *orchestratorMCPHandler) parseInferenceResponse(result interface{}) (*orchestrator.InferenceResponse, error) {
	if result == nil {
		return nil, errors.New(errors.CodeInternal, "mcp.orchestrator.parseInferenceResponse",
			"nil result from MCP tool")
	}

	// Re-encode the result to JSON so we can decode into the typed struct.
	// The MCP tool may return a string (raw JSON) or a map[string]interface{}.
	var raw []byte
	var err error
	switch v := result.(type) {
	case string:
		raw = []byte(v)
	default:
		raw, err = json.Marshal(v)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"mcp.orchestrator.parseInferenceResponse", "failed to re-encode MCP result")
		}
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
