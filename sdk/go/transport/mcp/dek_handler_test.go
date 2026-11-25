// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// dekToolResult encodes a service response the way the server's DEK tools
// do (marshalResult), giving the JSON object the fake tool returns.
func dekToolResult(t *testing.T, resp interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func dekTestProgress() (*service.ReencryptionProgress, time.Time) {
	started := time.Unix(1700000000, 0).UTC()
	eta := time.Unix(1700003600, 0).UTC()
	return &service.ReencryptionProgress{
		Status:              "running",
		InProgress:          true,
		TotalKeys:           4,
		ProcessedKeys:       2,
		StartedAt:           &started,
		EstimatedCompletion: &eta,
	}, eta
}

func assertMCPProgressFields(t *testing.T, got *dekclient.ReencryptionProgress, eta time.Time) {
	t.Helper()
	if got == nil {
		t.Fatal("progress is nil")
	}
	if got.Status != "running" {
		t.Errorf("Status = %q, want running", got.Status)
	}
	if !got.EstimatedCompletion.Equal(eta) {
		t.Errorf("EstimatedCompletion = %v, want %v", got.EstimatedCompletion, eta)
	}
	if got.TotalKeys != 4 || got.ProcessedKeys != 2 {
		t.Errorf("keys = %d/%d, want 2/4", got.ProcessedKeys, got.TotalKeys)
	}
}

func TestDEKClientOverMCP_GetReencryptionProgressCarriesStatusAndEstimatedCompletion(t *testing.T) {
	progress, eta := dekTestProgress()
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_dek_get_reencryption_progress",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["path"] != "app/key" {
				t.Errorf("path = %v, want app/key", args["path"])
			}
		},
		result: dekToolResult(t, &service.GetReencryptionProgressResponse{
			OperationID: "op-1", Path: "app/key", Status: "running", Progress: progress,
		}),
	})
	defer cleanup()

	resp, err := dekclient.NewClient(tr).GetReencryptionProgress(context.Background(),
		&dekclient.GetReencryptionProgressRequest{Path: "app/key", OperationID: "op-1"})
	if err != nil {
		t.Fatalf("GetReencryptionProgress: %v", err)
	}
	if resp.OperationID != "op-1" || resp.Status != "running" {
		t.Errorf("response = %+v", resp)
	}
	assertMCPProgressFields(t, resp.Progress, eta)
}

func TestDEKClientOverMCP_GetDEKStatusCarriesStatusAndEstimatedCompletion(t *testing.T) {
	progress, eta := dekTestProgress()
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_dek_status",
		result: dekToolResult(t, &service.GetDEKStatusResponse{
			DEK:                  &service.DEKMetadata{Path: "app/key", Version: 3},
			ReencryptionProgress: progress,
		}),
	})
	defer cleanup()

	resp, err := dekclient.NewClient(tr).GetDEKStatus(context.Background(), "app/key")
	if err != nil {
		t.Fatalf("GetDEKStatus: %v", err)
	}
	if resp.DEK == nil || resp.DEK.Path != "app/key" {
		t.Errorf("DEK = %+v", resp.DEK)
	}
	assertMCPProgressFields(t, resp.ReencryptionProgress, eta)
}

// Every DEK verb whose client method type-switches on a service response
// must get that type back from the MCP transport, or the client returns
// "invalid response type" (#1700).
func TestTransformDEKResponse_EachVerbDecodesToServiceResponse(t *testing.T) {
	cases := []struct {
		verb string
		resp interface{}
	}{
		{"CreateDEK", &service.CreateDEKResponse{}},
		{"GetDEK", &service.GetDEKResponse{}},
		{"ListDEKs", &service.ListDEKsResponse{}},
		{"RotateDEK", &service.RotateDEKResponse{}},
		{"DeleteDEK", &service.DeleteDEKResponse{}},
		{"GetDEKStatus", &service.GetDEKStatusResponse{}},
		{"SetDefaultDEK", &service.SetDefaultDEKResponse{}},
		{"SetDEKSafety", &service.SetDEKSafetyResponse{}},
		{"ClearDEKSafety", &service.ClearDEKSafetyResponse{}},
		{"StartReencryption", &service.StartReencryptionResponse{}},
		{"GetReencryptionProgress", &service.GetReencryptionProgressResponse{}},
	}
	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			got, err := transformDEKResponse(tc.verb, dekToolResult(t, tc.resp))
			if err != nil {
				t.Fatalf("transformDEKResponse: %v", err)
			}
			if fmt.Sprintf("%T", got) != fmt.Sprintf("%T", tc.resp) {
				t.Errorf("result type = %T, want %T", got, tc.resp)
			}
		})
	}
}

func TestTransformDEKResponse_OtherVerbPassesThrough(t *testing.T) {
	result := map[string]interface{}{"path": "app/key"}
	got, err := transformDEKResponse("NotADEKVerb", result)
	if err != nil {
		t.Fatalf("transformDEKResponse: %v", err)
	}
	if m, ok := got.(map[string]interface{}); !ok || m["path"] != "app/key" {
		t.Errorf("result = %#v, want the tool result unchanged", got)
	}
}

func TestIsMCPDEKService(t *testing.T) {
	for service, want := range map[string]bool{
		"DEK": true, "quicbaas.DEK": true, "KMS": false, "DEKAdmin": false,
	} {
		if got := isMCPDEKService(service); got != want {
			t.Errorf("isMCPDEKService(%q) = %v, want %v", service, got, want)
		}
	}
}

func TestDEKClientOverMCP_MalformedProgressIsInternal(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_dek_get_reencryption_progress",
		result:   map[string]interface{}{"progress": "not-an-object"},
	})
	defer cleanup()

	_, err := dekclient.NewClient(tr).GetReencryptionProgress(context.Background(),
		&dekclient.GetReencryptionProgressRequest{Path: "app/key"})
	if err == nil {
		t.Fatal("expected an error for a malformed tool result")
	}
}
