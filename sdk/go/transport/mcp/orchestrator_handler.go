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

	"GetSessionHistory":      "quicbaas_orchestrator_session_history",
	"GetSessionConversation": "quicbaas_orchestrator_session_conversation",
	"GetSpendReport":         "quicbaas_orchestrator_cost",
	"GetTenantSpendPolicy":   "quicbaas_orchestrator_spend_policy_get",
	"SetTenantSpendPolicy":   "quicbaas_orchestrator_spend_policy_set",

	// Task lifecycle tools are named after the noun and verb
	// (quicbaas_orchestrator_task_block), not the RPC (BlockTask).
	"BlockTask":   "quicbaas_orchestrator_task_block",
	"ReadmitTask": "quicbaas_orchestrator_task_readmit",
	"DeleteTask":  "quicbaas_orchestrator_task_delete",

	// A tenant's runtime cost settings (go-quicbaas#1058).
	"GetCostSettings": "quicbaas_orchestrator_cost_settings_get",
	"SetCostSettings": "quicbaas_orchestrator_cost_settings_set",
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

	"ListSessions":           decodeMCPListSessions,
	"GetSessionHistory":      decodeMCPSessionHistory,
	"GetSessionConversation": decodeMCPSessionConversation,
	"GetSpendReport":         decodeMCPSpendReport,
	"GetTenantSpendPolicy":   decodeMCPTenantSpendPolicy,
	"SetTenantSpendPolicy":   decodeMCPTenantSpendPolicy,

	"ListRunners":      decodeMCPListRunners,
	"BlockTask":        decodeMCPBlockTask,
	"ReadmitTask":      decodeMCPReadmitTask,
	"DeleteTask":       decodeMCPDeleteTask,
	"AttentionList":    decodeMCPAttentionList,
	"AttentionResolve": decodeMCPAttentionResolve,

	"GetCostSettings": decodeMCPCostSettings,
	"SetCostSettings": decodeMCPCostSettings,
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

// decodeMCPBlockTask decodes quicbaas_orchestrator_task_block output. It had
// the same gap ReadmitTask did — registered as a tool, absent from the
// transform table — and is fixed with it rather than left as the next report.
func decodeMCPBlockTask(raw []byte) (interface{}, error) {
	var resp orchestrator.BlockTaskResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.BlockTask", "failed to decode MCP block task result")
	}
	return &resp, nil
}

// decodeMCPReadmitTask decodes quicbaas_orchestrator_task_readmit output.
//
// Registered as a tool but absent from the transform table, a SUCCESSFUL
// readmit fell through as the raw tool map and the client refused it as an
// invalid response type — so the verb the job page's Readmit action calls was
// unusable over MCP even though the server had already done the work. The
// tool result mirrors the SDK response field for field, so the decode is
// direct, exactly as DeleteTask's is.
func decodeMCPReadmitTask(raw []byte) (interface{}, error) {
	var resp orchestrator.ReadmitTaskResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.ReadmitTask", "failed to decode MCP readmit task result")
	}
	return &resp, nil
}

// decodeMCPAttentionList decodes quicbaas_orchestrator_attention_list output.
func decodeMCPAttentionList(raw []byte) (interface{}, error) {
	var resp orchestrator.AttentionListResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.AttentionList", "failed to decode MCP attention list result")
	}
	if resp.Items == nil {
		resp.Items = []*orchestrator.AttentionItem{}
	}
	return &resp, nil
}

// decodeMCPAttentionResolve decodes quicbaas_orchestrator_attention_resolve
// output.
func decodeMCPAttentionResolve(raw []byte) (interface{}, error) {
	var resp orchestrator.AttentionItem
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.AttentionResolve", "failed to decode MCP attention resolve result")
	}
	return &resp, nil
}

// decodeMCPDeleteTask decodes quicbaas_orchestrator_task_delete output. The
// tool result mirrors the SDK response field for field, so the decode is
// direct; without it the client would receive the raw tool map and refuse it
// as an invalid response type.
func decodeMCPDeleteTask(raw []byte) (interface{}, error) {
	var resp orchestrator.DeleteTaskResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.DeleteTask", "failed to decode MCP delete task result")
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

// decodeMCPListRunners converts the quicbaas_orchestrator_list_runners result
// into *orchestrator.ListJobRunnersResponse. The tool serialises the same
// {runners, count} envelope the REST endpoint does, so the SDK type decodes it
// directly.
func decodeMCPListRunners(raw []byte) (interface{}, error) {
	var resp orchestrator.ListJobRunnersResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.ListRunners", "failed to decode MCP list runners result")
	}
	if resp.Runners == nil {
		resp.Runners = []*orchestrator.JobRunnerInfo{}
	}
	return &resp, nil
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

// decodeMCPSessionConversation decodes
// quicbaas_orchestrator_session_conversation output.
//
// The tool serialises the server's own SessionHistoryPage, whose JSON field
// names already match the SDK response type, so this is a straight decode
// rather than the reshaping the event transcript needs. It exists as a named
// decoder anyway because the transport's generic path would hand back a
// map[string]interface{}: the SDK's contract is a typed response.
func decodeMCPSessionConversation(raw []byte) (interface{}, error) {
	var out orchestrator.GetSessionConversationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.GetSessionConversation", "failed to decode MCP session conversation result")
	}
	return &out, nil
}

// decodeMCPSpendReport decodes quicbaas_orchestrator_cost output.
//
// The tool serialises the server's own SpendReport, whose JSON field names
// already match the SDK response type, so this is a straight decode. It
// exists as a named decoder because the transport's generic path would hand
// back a map[string]interface{}, and the SDK's contract is a typed response —
// which is also what carries cost_measured, the flag a renderer must consult
// before showing a cost.
// decodeMCPCostSettings decodes the cost settings tools' result: the server's
// CostSettingsReport, whose JSON matches the SDK type.
func decodeMCPCostSettings(raw []byte) (interface{}, error) {
	var out orchestrator.CostSettings
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.CostSettings", "failed to decode MCP cost settings result")
	}
	return &out, nil
}

func decodeMCPSpendReport(raw []byte) (interface{}, error) {
	var out orchestrator.GetSpendReportResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.GetSpendReport", "failed to decode MCP spend report result")
	}
	return &out, nil
}

// decodeMCPTenantSpendPolicy decodes the tenant spend-policy tools' result:
// the server's TenantSpendPolicyReport, whose JSON matches the SDK type.
func decodeMCPTenantSpendPolicy(raw []byte) (interface{}, error) {
	var out orchestrator.TenantSpendPolicy
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"mcp.orchestrator.TenantSpendPolicy", "failed to decode MCP tenant spend policy result")
	}
	return &out, nil
}
