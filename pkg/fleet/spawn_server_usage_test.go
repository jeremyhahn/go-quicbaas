// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package fleet

import (
	"context"
	"encoding/json"
	"testing"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// promptFrame runs one Prompt against a spawn server whose backend answers
// with resp, and returns the single frame it streams.
func promptFrame(t *testing.T, resp *orchestration.PromptResponse) *proto.PromptSessionResponse {
	t.Helper()
	backend := &spawnBackend{
		promptFn: func(context.Context, *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
			return resp, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	t.Cleanup(cleanup)

	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend: "acp", TenantId: "tenant-a", SessionId: "sess-1", Prompt: "hi",
	})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	frame, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream.Recv: %v", err)
	}
	return frame
}

// TestGRPCSpawnServer_PromptSendsTheTurnsUsage: the daemon has no ledger, so
// the usage its CLI reported must travel back with the answer or the turn is
// charged to nothing (go-quicbaas#1046).
func TestGRPCSpawnServer_PromptSendsTheTurnsUsage(t *testing.T) {
	t.Parallel()
	frame := promptFrame(t, &orchestration.PromptResponse{
		SessionID: "sess-1",
		Content:   []orchestration.ContentBlock{{Type: "text", Text: "done"}},
		Usage:     &orchestration.TokenUsage{InputTokens: 900, OutputTokens: 100},
	})
	var usage orchestration.TokenUsage
	if err := json.Unmarshal(frame.GetUsageJson(), &usage); err != nil {
		t.Fatalf("usage_json %q does not decode: %v", frame.GetUsageJson(), err)
	}
	if usage.Total() != 1000 {
		t.Fatalf("usage = %+v, want 1000 tokens", usage)
	}
}

func TestGRPCSpawnServer_PromptWithNoUsageSendsNone(t *testing.T) {
	t.Parallel()
	frame := promptFrame(t, &orchestration.PromptResponse{
		SessionID: "sess-1",
		Content:   []orchestration.ContentBlock{{Type: "text", Text: "done"}},
	})
	if len(frame.GetUsageJson()) != 0 {
		t.Fatalf("usage_json = %q, want nothing: reported-nothing must stay distinguishable from zero", frame.GetUsageJson())
	}
}
