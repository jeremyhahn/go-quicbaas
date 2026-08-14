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

package quic

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// orchestratorServicePrefix is the service name prefix for Orchestrator operations.
const orchestratorServicePrefix = "quicbaas.Orchestrator"

// isOrchestratorService reports whether svc names an Orchestrator service.
func isOrchestratorService(svc string) bool {
	return strings.HasPrefix(svc, orchestratorServicePrefix) || svc == "Orchestrator"
}

// orchestratorResponseConverter defines the function signature for per-method
// response converters. Each function receives raw JSON bytes from the wire
// and returns the typed SDK value.
type orchestratorResponseConverter func(data []byte) (interface{}, error)

// orchestratorResponseConverters maps method names to their response converters
// for O(1) dispatch, following the same pattern as embeddingResponseConverters.
var orchestratorResponseConverters = map[string]orchestratorResponseConverter{
	"SpawnSession":  convertSpawnSessionResponse,
	"PromptSession": convertPromptSessionResponse,
	"SessionStatus": convertSessionStatusResponse,
	"CloseSession":  convertCloseSessionResponse,

	"RotateSessionAccount": convertRotateSessionAccountResponse,

	"ListSessions": convertListSessionsResponse,
	"CreateTask":   convertCreateTaskResponse,
	"GetTask":      convertGetTaskResponse,
	"ListTasks":    convertListTasksResponse,
	"ClaimTask":    convertClaimTaskResponse,
	"CompleteTask": convertCompleteTaskResponse,
	"ListBackends": convertListBackendsResponse,
	"Health":       convertHealthResponse,
	"Inference":    convertInferenceResponse,
}

// decodeOrchestratorResponse decodes raw wire bytes into the appropriate SDK
// orchestrator type. Unlike convertOrchestratorResponse (used in tests with a
// pre-parsed map), this function receives the raw JSON payload from the server
// and handles both JSON object and JSON array shapes without an intermediate
// map[string]interface{} decode step.
func decodeOrchestratorResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}

	converter, ok := orchestratorResponseConverters[method]
	if !ok {
		// Unknown method: fall through to a generic map decode.
		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal,
				"quic.decodeOrchestratorResponse", "failed to decode response")
		}
		return decoded, nil
	}

	return converter(data)
}

// convertOrchestratorResponse converts a pre-parsed map response to the
// appropriate SDK orchestrator type. Used in tests where the response has
// already been decoded from JSON into a map[string]interface{}.
func convertOrchestratorResponse(method string, decoded map[string]interface{}) (interface{}, error) {
	if decoded == nil {
		return nil, nil
	}

	converter, ok := orchestratorResponseConverters[method]
	if !ok {
		return decoded, nil
	}

	data, err := json.Marshal(decoded)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertOrchestratorResponse", "failed to marshal response")
	}

	return converter(data)
}

// convertSpawnSessionResponse converts JSON data to a SpawnSessionResponse.
func convertSpawnSessionResponse(data []byte) (interface{}, error) {
	var resp orchestrator.SpawnSessionResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSpawnSessionResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertPromptSessionResponse converts the service's turn payload into the
// flat SDK response. The wire carries content blocks and a stop reason, which
// share no field names with PromptSessionResponse, so the decode goes through
// the shared converter rather than straight into the SDK type.
func convertPromptSessionResponse(data []byte) (interface{}, error) {
	var wire orchestrator.PromptSessionWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertPromptSessionResponse", "failed to unmarshal response")
	}
	return wire.Response()
}

// convertSessionStatusResponse converts JSON data to a SessionStatusResponse.
func convertSessionStatusResponse(data []byte) (interface{}, error) {
	var wire orchestrator.SessionStatusWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertSessionStatusResponse", "failed to unmarshal response")
	}
	return wire.Response(), nil
}

// convertCloseSessionResponse converts JSON data to a CloseSessionResponse.
func convertCloseSessionResponse(data []byte) (interface{}, error) {
	var wire orchestrator.CloseSessionWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertCloseSessionResponse", "failed to unmarshal response")
	}
	return wire.Response(), nil
}

// convertRotateSessionAccountResponse converts JSON data to a
// RotateSessionAccountResponse. The server encodes
// orchestration.RotateSessionAccountResult, whose JSON tags mirror the SDK
// type field for field, so no intermediate wire struct is involved.
func convertRotateSessionAccountResponse(data []byte) (interface{}, error) {
	var resp orchestrator.RotateSessionAccountResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertRotateSessionAccountResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertListSessionsResponse converts JSON data to a ListSessionsResponse.
func convertListSessionsResponse(data []byte) (interface{}, error) {
	var wire orchestrator.ListSessionsWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertListSessionsResponse", "failed to unmarshal response")
	}
	return wire.Response(), nil
}

// convertCreateTaskResponse converts JSON data to a Task.
func convertCreateTaskResponse(data []byte) (interface{}, error) {
	var resp orchestrator.Task
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertCreateTaskResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertGetTaskResponse converts JSON data to a Task.
func convertGetTaskResponse(data []byte) (interface{}, error) {
	var resp orchestrator.Task
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertGetTaskResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertListTasksResponse converts JSON data to a ListTasksResponse.
func convertListTasksResponse(data []byte) (interface{}, error) {
	var resp orchestrator.ListTasksResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertListTasksResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertClaimTaskResponse converts JSON data to a ClaimTaskResponse.
func convertClaimTaskResponse(data []byte) (interface{}, error) {
	var resp orchestrator.ClaimTaskResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertClaimTaskResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// convertCompleteTaskResponse converts JSON data to a CompleteTaskResponse.
func convertCompleteTaskResponse(data []byte) (interface{}, error) {
	var resp orchestrator.CompleteTaskResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertCompleteTaskResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// agentBackendWire is the wire shape for a backend as returned by the server's
// handleOrchestratorListBackends handler (mirrors pkg/orchestration.AgentBackendInfo).
type agentBackendWire struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	DefaultModel string `json:"default_model,omitempty"`
	Status       string `json:"status,omitempty"`
	Available    *bool  `json:"available,omitempty"`
}

// convertListBackendsResponse converts JSON data to a ListBackendsResponse.
// The server returns []*AgentBackendInfo (a raw JSON array); we map it to the
// SDK's BackendInfo struct. If the payload is a wrapped object it is decoded
// directly.
func convertListBackendsResponse(data []byte) (interface{}, error) {
	// Try the wrapped object shape first: {"backends":[...],"count":N}
	var respObj orchestrator.ListBackendsResponse
	if err := json.Unmarshal(data, &respObj); err == nil && respObj.Backends != nil {
		return &respObj, nil
	}

	// Server returned a raw array of AgentBackendInfo objects.
	var wired []agentBackendWire
	if err := json.Unmarshal(data, &wired); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertListBackendsResponse", "failed to unmarshal response")
	}

	backends := make([]*orchestrator.BackendInfo, 0, len(wired))
	for _, w := range wired {
		available := true
		if w.Available != nil {
			available = *w.Available
		}
		status := w.Status
		if status == "" {
			if available {
				status = "available"
			} else {
				status = "unavailable"
			}
		}
		backends = append(backends, &orchestrator.BackendInfo{
			Name:      w.Name,
			Type:      w.Type,
			Status:    status,
			Available: available,
		})
	}

	return &orchestrator.ListBackendsResponse{
		Backends: backends,
		Count:    len(backends),
	}, nil
}

// healthWireRaw decodes the health payload with all number fields as
// json.Number so we can disambiguate between the server's integer-count shape
// and a bool shape without re-encoding.
type healthWireRaw struct {
	Timestamp     json.Number       `json:"timestamp"`
	TotalSessions json.Number       `json:"total_sessions"`
	Healthy       json.RawMessage   `json:"healthy"`
	Unhealthy     json.Number       `json:"unhealthy"`
	Closed        json.Number       `json:"closed"`
	Status        string            `json:"status,omitempty"`
	Components    map[string]string `json:"components,omitempty"`
}

// convertHealthResponse converts JSON data to a HealthCheckReport.
// The server returns pkg/orchestration.HealthCheckReport which uses integer
// counts; we map those to the SDK's boolean-centric HealthCheckReport.
func convertHealthResponse(data []byte) (interface{}, error) {
	var raw healthWireRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertHealthResponse", "failed to unmarshal response")
	}

	ts, _ := raw.Timestamp.Int64()
	unhealthy, _ := raw.Unhealthy.Int64()
	totalSessions, _ := raw.TotalSessions.Int64()

	// Determine whether the "healthy" field is a bool (SDK shape) or int
	// (server shape). A JSON bool starts with 't' or 'f'.
	var healthyBool bool
	var healthyCount int64
	if len(raw.Healthy) > 0 && (raw.Healthy[0] == 't' || raw.Healthy[0] == 'f') {
		_ = json.Unmarshal(raw.Healthy, &healthyBool)
	} else {
		var n json.Number
		_ = json.Unmarshal(raw.Healthy, &n)
		healthyCount, _ = n.Int64()
		// In the server shape, healthy == total_sessions means all healthy.
		healthyBool = unhealthy == 0
	}

	status := raw.Status
	if status == "" {
		if unhealthy == 0 {
			status = "ok"
		} else {
			status = "degraded"
		}
	}

	components := raw.Components
	if components == nil {
		components = map[string]string{
			"total_sessions": strconv.FormatInt(totalSessions, 10),
			"healthy":        strconv.FormatInt(healthyCount, 10),
			"unhealthy":      strconv.FormatInt(unhealthy, 10),
		}
	}

	return &orchestrator.HealthCheckReport{
		Healthy:    healthyBool,
		Status:     status,
		Timestamp:  ts,
		Components: components,
	}, nil
}

// convertInferenceResponse converts JSON data to an InferenceResponse.
func convertInferenceResponse(data []byte) (interface{}, error) {
	var resp orchestrator.InferenceResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertInferenceResponse", "failed to unmarshal response")
	}
	return &resp, nil
}

// injectTenantID injects the tenant ID from ctx into the JSON-encoded payload.
// It is a no-op when the context carries no tenant, or when the payload already
// contains a non-empty "tenant_id" field. This ensures that all Orchestrator
// wire requests carry the caller's tenant scope without requiring every SDK
// request struct to expose a TenantID field.
func injectTenantID(ctx context.Context, payload []byte) []byte {
	tenantID := transport.TenantFromContext(ctx)
	if tenantID == "" {
		return payload
	}

	// Decode into a generic map so we can inspect and set tenant_id without
	// knowing the concrete request type.
	var body map[string]interface{}
	if err := json.Unmarshal(payload, &body); err != nil {
		// Payload is not a JSON object (e.g. empty body "{}" encoded differently).
		// Return unchanged; the server will surface the tenant validation error.
		return payload
	}

	// Do not overwrite an existing non-empty tenant_id already set by the caller.
	if existing, ok := body["tenant_id"]; ok {
		if s, ok := existing.(string); ok && s != "" {
			return payload
		}
	}

	body["tenant_id"] = tenantID

	updated, err := json.Marshal(body)
	if err != nil {
		// Marshal of a round-tripped map should never fail; return original as
		// a defensive fallback.
		return payload
	}

	return updated
}
