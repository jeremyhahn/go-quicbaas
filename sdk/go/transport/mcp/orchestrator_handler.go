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
	"time"

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

// orchestratorMethodTools maps Orchestrator SDK methods to the MCP tool names
// the server actually registers. The server names session tools after the
// operation (quicbaas_orchestrator_prompt) while the SDK names the method
// (PromptSession), so the transport's generic service_method derivation would
// ask for tools that do not exist. Methods absent from this map fall through to
// that derivation, which is correct for the ones whose names already agree
// (ListSessions, Inference, the job and fleet surfaces).
var orchestratorMethodTools = map[string]string{
	"SpawnSession":  "quicbaas_orchestrator_spawn",
	"PromptSession": "quicbaas_orchestrator_prompt",
	"SessionStatus": "quicbaas_orchestrator_status",
	"CloseSession":  "quicbaas_orchestrator_close",

	"RotateSessionAccount": "quicbaas_orchestrator_rotate_account",

	"GetSessionHistory": "quicbaas_orchestrator_session_history",
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

	"RotateSessionAccount": decodeMCPRotateSessionAccount,

	"ListSessions":      decodeMCPListSessions,
	"GetSessionHistory": decodeMCPSessionHistory,
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

// decodeMCPSessionHistory decodes quicbaas_orchestrator_session_history output.
//
// This is how an MCP caller observes an asynchronous turn: MCP has no
// server-push channel, so instead of subscribing it pages the durable
// transcript from the sequence the prompt acknowledgement returned until the
// turn's terminal event appears.
func decodeMCPSessionHistory(raw []byte) (interface{}, error) {
	var wire sessionHistoryWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.GetSessionHistory", "failed to decode MCP session history result")
	}
	return wire.response()
}

// sessionHistoryWire mirrors the quicbaas_orchestrator_session_history tool
// result. The tool returns the stored SessionEvent objects inline, while the
// SDK exposes each one as an envelope with the event carried verbatim under
// PayloadJSON, so the events are re-encoded here rather than reshaped — the
// SDK deliberately does not model every event kind.
type sessionHistoryWire struct {
	SessionID string            `json:"session_id"`
	TenantID  string            `json:"tenant_id"`
	Events    []json.RawMessage `json:"events"`
	NextSeq   uint64            `json:"next_seq"`
}

// sessionEventHeader is the addressing subset lifted out of each stored event
// so an envelope can be built without interpreting the payload.
type sessionEventHeader struct {
	Sequence  uint64    `json:"sequence"`
	SessionID string    `json:"session_id"`
	TenantID  string    `json:"tenant_id"`
	Timestamp time.Time `json:"timestamp"`
}

// response converts the tool result into the SDK history response.
func (w *sessionHistoryWire) response() (*orchestrator.GetSessionHistoryResponse, error) {
	out := &orchestrator.GetSessionHistoryResponse{
		Events:  make([]*orchestrator.SessionEventEnvelope, 0, len(w.Events)),
		NextSeq: w.NextSeq,
	}
	for _, raw := range w.Events {
		var header sessionEventHeader
		if err := json.Unmarshal(raw, &header); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"mcp.orchestrator.GetSessionHistory", "failed to decode session event")
		}
		sessionID := header.SessionID
		if sessionID == "" {
			sessionID = w.SessionID
		}
		tenantID := header.TenantID
		if tenantID == "" {
			tenantID = w.TenantID
		}
		out.Events = append(out.Events, &orchestrator.SessionEventEnvelope{
			SessionID:   sessionID,
			TenantID:    tenantID,
			Sequence:    header.Sequence,
			Timestamp:   header.Timestamp,
			PayloadJSON: raw,
			FromReplay:  true,
		})
	}
	return out, nil
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

// decodeMCPRotateSessionAccount decodes quicbaas_orchestrator_rotate_account
// output. The tool result mirrors the SDK response field for field, so the
// decode is direct.
func decodeMCPRotateSessionAccount(raw []byte) (interface{}, error) {
	var resp orchestrator.RotateSessionAccountResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.RotateSessionAccount", "failed to decode MCP rotate account result")
	}
	return &resp, nil
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
