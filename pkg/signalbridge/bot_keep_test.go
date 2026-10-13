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

package signalbridge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// chatBackend is an orchestration backend whose sessions do nothing; the test
// is about what the orchestrator records for them, not what they run.
type chatBackend struct{}

func (chatBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	return &orchestration.Session{ID: "chat-1", ProjectID: req.ProjectID, Status: orchestration.SessionStateActive}, nil
}

func (chatBackend) Prompt(context.Context, *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{}, nil
}

func (chatBackend) Status(_ context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: sessionID, State: orchestration.SessionStateIdle}, nil
}

func (chatBackend) Close(context.Context, string) error { return nil }

func (chatBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{}
}

func (chatBackend) Type() string { return "chat" }

func (chatBackend) ToolGuidance() string { return "" }

// domainOrchestrator hands the bot's spawns to a real orchestration service,
// decoding the SDK request into the domain one the way the REST server does,
// persistent by default included. Every other call goes to the mock.
type domainOrchestrator struct {
	*mockOrchestrator
	svc    orchestration.AgentService
	tenant string
}

func (d *domainOrchestrator) SpawnSession(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var domain orchestration.SpawnSessionRequest
	if err := json.Unmarshal(body, &domain); err != nil {
		return nil, err
	}
	domain.Persistent = req.Persistent == nil || *req.Persistent
	resp, err := d.svc.SpawnSession(orchestration.ContextWithTenant(ctx, d.tenant), &domain)
	if err != nil {
		return nil, err
	}
	return &orchestratorSDK.SpawnSessionResponse{SessionID: resp.SessionID, Backend: resp.Backend}, nil
}

// sessionSweeper is the orchestration service's session reconciliation pass.
type sessionSweeper interface {
	SweepSessions(ctx context.Context) (orchestration.SessionReconcileReport, error)
}

// TestBot_ChatSessionSurvivesTheIdleSweep spawns a session from chat and runs
// reconciliation once it has been quiet past the idle bound. A chat session is
// bound to no job, so without keep the sweep would conclude it between the
// operator's messages.
func TestBot_ChatSessionSurvivesTheIdleSweep(t *testing.T) {
	store := orchestration.NewInMemoryAgentStore()
	svc, err := orchestration.NewAgentService(store, map[string]orchestration.AgentBackend{"chat": chatBackend{}}, noopLogger{})
	if err != nil {
		t.Fatalf("NewAgentService: %v", err)
	}
	kvSetter, ok := svc.(interface{ SetKVStore(interfaces.KVStore) })
	if !ok {
		t.Fatalf("%T cannot take a KV store", svc)
	}
	kvSetter.SetKVStore(orchestration.NewMemoryTaskKV())
	sweeper, ok := svc.(sessionSweeper)
	if !ok {
		t.Fatalf("%T has no session sweep", svc)
	}

	bot, _, mock, _ := newTestBot(t)
	bot.orchestrator = &domainOrchestrator{mockOrchestrator: mock, svc: svc, tenant: "t"}
	if err := bot.spawnOrDispatch(context.Background(), dmMsg("spawn chat"), testOperator, Intent{Kind: IntentSpawn, Backend: "chat"}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	records, err := store.ListAllSessions(context.Background())
	if err != nil {
		t.Fatalf("ListAllSessions: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("stored %d sessions, want the one the bot spawned", len(records))
	}
	record := records[0]
	if !record.Keep {
		t.Fatal("the bot spawned a session that is not kept")
	}
	var bound time.Duration
	for _, threshold := range orchestration.DefaultJobIdleThresholds().Resolved() {
		bound = max(bound, threshold)
	}
	record.Status = orchestration.SessionStateIdle
	record.LastActivity = time.Now().Add(-(bound + time.Hour)).UnixNano()
	if err := store.PutSession(context.Background(), record); err != nil {
		t.Fatalf("PutSession: %v", err)
	}

	report, err := sweeper.SweepSessions(context.Background())
	if err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want the chat session left alone", report.Findings)
	}
	stored, err := store.GetSession(context.Background(), record.TenantID, record.SessionID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if stored.Status == orchestration.SessionStateClosed {
		t.Error("the sweep concluded the chat session")
	}
}
