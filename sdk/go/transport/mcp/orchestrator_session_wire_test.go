// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// MCP session-response contract.
//
// The quicbaas_orchestrator_* session tools serialize the orchestrator
// service's own shape (content blocks, "state", an RFC 3339 timestamp). The
// MCP handler used to pass those results through untouched, so the SDK client
// failed its type assertion instead of returning a turn. These tests pin the
// transformation for each session tool.
// =============================================================================

func TestMCPTransformPromptSession(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("tool result becomes a done response carrying the turn text", func(t *testing.T) {
		result := map[string]interface{}{
			"session_id": "sess-001",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "SELF-HOSTING ONLINE"},
			},
			"model":       "claude-fable-5",
			"stop_reason": "end_turn",
			// The MCP tools format the envelope timestamp as RFC 3339, unlike
			// the epoch nanoseconds REST and QUIC send.
			"timestamp": "2026-08-08T12:37:10.175886792Z",
		}

		out, err := h.transformResponse("PromptSession", result)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.PromptSessionResponse)
		if !ok {
			t.Fatalf("result type = %T, want *orchestrator.PromptSessionResponse", out)
		}
		if resp.Response != "SELF-HOSTING ONLINE" || !resp.Done {
			t.Errorf("response = %+v, want the turn text and Done", resp)
		}
	})

	t.Run("raw JSON string results are decoded too", func(t *testing.T) {
		out, err := h.transformResponse("PromptSession",
			`{"session_id":"sess-001","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		if resp := out.(*orchestrator.PromptSessionResponse); resp.Response != "ok" {
			t.Errorf("Response = %q", resp.Response)
		}
	})

	t.Run("a turn with nothing in it is a typed error", func(t *testing.T) {
		out, err := h.transformResponse("PromptSession", map[string]interface{}{"session_id": "sess-001"})
		if err == nil {
			t.Fatalf("transformResponse() = %+v, want an error", out)
		}
		var empty *orchestrator.EmptyTurnError
		if !errors.As(err, &empty) {
			t.Fatalf("error %v should be a *orchestrator.EmptyTurnError", err)
		}
	})

	t.Run("nil result is an error, not a nil response", func(t *testing.T) {
		if _, err := h.transformResponse("PromptSession", nil); err == nil {
			t.Fatal("transformResponse(nil) should error")
		}
	})

	t.Run("undecodable result is an error", func(t *testing.T) {
		if _, err := h.transformResponse("PromptSession", "not json at all"); err == nil {
			t.Fatal("transformResponse should reject an undecodable result")
		}
	})
}

func TestMCPTransformSessionTools(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("SessionStatus maps state onto status", func(t *testing.T) {
		out, err := h.transformResponse("SessionStatus", map[string]interface{}{
			"session_id":    "sess-001",
			"state":         "active",
			"last_activity": float64(1786205526),
			"uptime":        float64(1494),
			"timestamp":     "2026-08-08T12:37:00.331543767Z",
		})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.SessionStatusResponse)
		if !ok {
			t.Fatalf("result type = %T", out)
		}
		if resp.Status != "active" {
			t.Errorf("Status = %q", resp.Status)
		}
	})

	t.Run("ListSessions derives the count", func(t *testing.T) {
		out, err := h.transformResponse("ListSessions", map[string]interface{}{
			"sessions": []interface{}{
				map[string]interface{}{"session_id": "sess-001", "backend": "claude", "state": "active"},
			},
			"timestamp": "2026-08-08T12:37:00.520347903Z",
		})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.ListSessionsResponse)
		if !ok {
			t.Fatalf("result type = %T", out)
		}
		if resp.Count != 1 || resp.Sessions[0].Backend != "claude" {
			t.Errorf("response = %+v, want one populated session", resp)
		}
	})

	t.Run("CloseSession acknowledgement means closed", func(t *testing.T) {
		out, err := h.transformResponse("CloseSession", map[string]interface{}{
			"session_id": "sess-001",
			"timestamp":  "2026-08-08T12:37:00.331543767Z",
		})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.CloseSessionResponse)
		if !ok {
			t.Fatalf("result type = %T", out)
		}
		if !resp.Closed {
			t.Error("Closed = false although the tool acknowledged the close")
		}
	})

	t.Run("malformed session tool results are errors", func(t *testing.T) {
		for _, method := range []string{"SessionStatus", "ListSessions", "CloseSession"} {
			if _, err := h.transformResponse(method, "not json at all"); err == nil {
				t.Errorf("%s: transformResponse should reject an undecodable result", method)
			}
		}
	})

	t.Run("ListBackends decodes into the SDK response", func(t *testing.T) {
		in := map[string]interface{}{"backends": []interface{}{}, "total": float64(0), "limit": float64(25)}
		out, err := h.transformResponse("ListBackends", in)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.ListBackendsResponse)
		if !ok {
			t.Fatalf("result type = %T, want *orchestrator.ListBackendsResponse", out)
		}
		if resp.Total != 0 || resp.Limit != 25 {
			t.Fatalf("page envelope = total %d limit %d", resp.Total, resp.Limit)
		}
	})
}

// =============================================================================
// ListRunners over MCP.
//
// quicbaas_orchestrator_list_runners serializes the same {runners, count}
// envelope the REST endpoint does. Without a transform the SDK client's type
// assertion would fail on the raw map, which is the defect the session tools
// above were written from — so it is pinned here for the runners tool too.
// =============================================================================

func TestMCPTransformListRunners(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("tool result becomes the typed runners response", func(t *testing.T) {
		result := map[string]interface{}{
			"runners": []interface{}{
				map[string]interface{}{
					"name": "workflow",
					"states": []interface{}{
						map[string]interface{}{
							"state":          "initializing",
							"label":          "Initializing",
							"session_backed": false,
						},
						map[string]interface{}{
							"state":          "executing",
							"label":          "Executing",
							"session_backed": true,
						},
					},
				},
			},
			"count": 1,
		}

		out, err := h.transformResponse("ListRunners", result)
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp, ok := out.(*orchestrator.ListJobRunnersResponse)
		if !ok {
			t.Fatalf("result type = %T, want *orchestrator.ListJobRunnersResponse", out)
		}
		if resp.Count != 1 || len(resp.Runners) != 1 {
			t.Fatalf("runners = %d (count %d), want 1", len(resp.Runners), resp.Count)
		}
		if resp.Runners[0].Name != "workflow" {
			t.Errorf("runner name = %q, want workflow", resp.Runners[0].Name)
		}
		if len(resp.Runners[0].States) != 2 {
			t.Fatalf("states = %d, want 2", len(resp.Runners[0].States))
		}
		if resp.Runners[0].States[0].Label != "Initializing" {
			t.Errorf("first label = %q, want Initializing", resp.Runners[0].States[0].Label)
		}
		if resp.Runners[0].States[0].SessionBacked {
			t.Error("initializing must not be reported as session-backed")
		}
		if !resp.Runners[0].States[1].SessionBacked {
			t.Error("executing must be reported as session-backed")
		}
	})

	t.Run("an empty registry decodes to an empty slice, never nil", func(t *testing.T) {
		out, err := h.transformResponse("ListRunners", map[string]interface{}{"count": 0})
		if err != nil {
			t.Fatalf("transformResponse() error = %v", err)
		}
		resp := out.(*orchestrator.ListJobRunnersResponse)
		if resp.Runners == nil {
			t.Fatal("Runners is nil; a caller ranging over it must not have to nil-check")
		}
		if len(resp.Runners) != 0 {
			t.Fatalf("runners = %d, want 0", len(resp.Runners))
		}
	})

	t.Run("a malformed tool result is an error, not an empty answer", func(t *testing.T) {
		if _, err := h.transformResponse("ListRunners", "not json at all"); err == nil {
			t.Error("transformResponse should reject an undecodable result")
		}
	})
}

func TestMCPTransformListJobs(t *testing.T) {
	h := &orchestratorMCPHandler{}
	result := map[string]interface{}{
		"jobs": []interface{}{
			map[string]interface{}{
				"job_id":      "job-1",
				"state":       "completed",
				"task_id":     "task-a",
				"attempt":     float64(2),
				"event_count": float64(3),
				"source": map[string]interface{}{
					"kind": "forge_issue",
					"ref":  "repo#1",
					"url":  "https://forge.example/repo/issues/1",
				},
			},
		},
		"total":       float64(7),
		"limit":       float64(1),
		"offset":      float64(2),
		"next_offset": float64(3),
		"count":       float64(1),
	}

	got, err := h.transformResponse("ListJobs", result)
	if err != nil {
		t.Fatalf("transform ListJobs: %v", err)
	}
	resp, ok := got.(*orchestrator.ListJobsResponse)
	if !ok {
		t.Fatalf("response = %T, want *orchestrator.ListJobsResponse", got)
	}
	if resp.Total != 7 || resp.Limit != 1 || resp.Offset != 2 || resp.NextOffset != 3 {
		t.Fatalf("envelope = %+v, want total 7 limit 1 offset 2 next 3", resp.Envelope)
	}
	if len(resp.Jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(resp.Jobs))
	}
	job := resp.Jobs[0]
	if job.JobID != "job-1" || job.TaskID != "task-a" || job.Attempt != 2 || job.EventCount != 3 {
		t.Fatalf("job = %+v, want decoded job metadata", job)
	}
	if job.Source == nil || job.Source.Ref != "repo#1" {
		t.Fatalf("source = %+v, want decoded source", job.Source)
	}
}
