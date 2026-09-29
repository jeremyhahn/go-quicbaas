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

package orchestration

import (
	"context"
	"testing"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// remoteBackendWith returns a RemoteACPBackend over a daemon that answers every
// prompt with resp.
func remoteBackendWith(t *testing.T, resp *proto.PromptSessionResponse) *RemoteACPBackend {
	t.Helper()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{promptResp: resp})
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := backend.CloseAll(); closeErr != nil {
			t.Logf("CloseAll: %v", closeErr)
		}
	})
	return backend
}

// TestRemoteACPBackend_PromptCarriesTheTurnsUsage is the burn-in's "usage is
// zero" regression: a remote turn's usage must reach the PromptResponse the
// control plane charges from.
func TestRemoteACPBackend_PromptCarriesTheTurnsUsage(t *testing.T) {
	t.Parallel()
	usageJSON, err := PromptUsageToWire(&TokenUsage{InputTokens: 2000, OutputTokens: 500})
	if err != nil {
		t.Fatalf("PromptUsageToWire: %v", err)
	}
	backend := remoteBackendWith(t, &proto.PromptSessionResponse{
		SessionId: "sess-1", Content: []*proto.ContentBlock{{Type: "text", Text: "done"}},
		StopReason: "end_turn", UsageJson: usageJSON,
	})

	resp, err := backend.Prompt(context.Background(), &PromptRequest{SessionID: "sess-1", Prompt: "go"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Usage == nil || resp.Usage.Total() != 2500 {
		t.Fatalf("Usage = %+v, want the daemon's 2500 tokens", resp.Usage)
	}
}

func TestRemoteACPBackend_AnUndecodableUsageDoesNotFailTheTurn(t *testing.T) {
	t.Parallel()
	backend := remoteBackendWith(t, &proto.PromptSessionResponse{
		SessionId: "sess-1", Content: []*proto.ContentBlock{{Type: "text", Text: "done"}},
		UsageJson: []byte("{broken"),
	})

	resp, err := backend.Prompt(context.Background(), &PromptRequest{SessionID: "sess-1", Prompt: "go"})
	if err != nil {
		t.Fatalf("Prompt: %v — bookkeeping must not fail a turn whose answer arrived", err)
	}
	if resp.Usage != nil {
		t.Fatalf("Usage = %+v, want none: an undecodable figure is not a measurement", resp.Usage)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("Content = %+v, want the answer", resp.Content)
	}
}

// TestRemoteACPBackend_SpawnCarriesTheDaemonsReservation: the reservation the
// daemon's resolution took against this control plane's table comes back on
// the spawn response, so the record this side writes can name it.
func TestRemoteACPBackend_SpawnCarriesTheDaemonsReservation(t *testing.T) {
	t.Parallel()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{spawnResp: &proto.SpawnSessionResponse{
		SessionId: "daemon-sess-1", Backend: "acp", AccountId: "claude2", ReservationId: "res-7",
	}})
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(), Backend: "acp", Logger: &agentTestLogger{}, ConnDialer: daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := backend.CloseAll(); closeErr != nil {
			t.Logf("CloseAll: %v", closeErr)
		}
	})
	sess, err := backend.Spawn(context.Background(), &SpawnRequest{WorkDir: "/w", AccountAuto: true})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if sess.ReservationID != "res-7" {
		t.Fatalf("ReservationID = %q, want the daemon's reservation", sess.ReservationID)
	}
}

// TestPromptSession_ARemoteTurnIsChargedToItsAccount closes the loop: a turn
// served by a remote backend lands on the account's usage, which is what
// GET /agent-accounts/{id}/usage reports and what balancing ranks on.
func TestPromptSession_ARemoteTurnIsChargedToItsAccount(t *testing.T) {
	backend := &usageReportingPoolBackend{poolResumeBackend: poolResumeBackend{autoPick: "claude2"}}
	h := newRotationHarness(t, backend)
	ctx := ContextWithTenant(context.Background(), "tenant-a")
	sessionID := spawnRotatableSession(t, h, ctx, nil)

	if _, err := h.service.PromptSession(ctx, &PromptSessionRequest{SessionID: sessionID, Prompt: "go"}); err != nil {
		t.Fatalf("PromptSession: %v", err)
	}
	report, err := h.tracker.GetAccountUsage(ctx, "tenant-a", "claude2")
	if err != nil {
		t.Fatalf("GetAccountUsage: %v", err)
	}
	if report.TotalTokens != 700 {
		t.Fatalf("TotalTokens = %d, want the turn's 700", report.TotalTokens)
	}
}

// usageReportingPoolBackend answers every turn with the usage a remote daemon
// now sends back.
type usageReportingPoolBackend struct {
	poolResumeBackend
}

func (b *usageReportingPoolBackend) Prompt(_ context.Context, req *PromptRequest) (*PromptResponse, error) {
	return &PromptResponse{
		SessionID: req.SessionID,
		Content:   []ContentBlock{{Type: "text", Text: "done"}},
		Usage:     &TokenUsage{InputTokens: 600, OutputTokens: 100},
	}, nil
}
