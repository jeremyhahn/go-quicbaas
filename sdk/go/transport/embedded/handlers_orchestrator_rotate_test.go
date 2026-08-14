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

package embedded

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// quicbaas.Orchestrator/RotateSessionAccount over the in-process transport
// =============================================================================

// rotatingFakeAgentService is a fakeAgentService that also implements the
// optional rotation verb. The bare fakeAgentService deliberately does not,
// which is what makes the "deployment without rotation" branch reachable.
type rotatingFakeAgentService struct {
	*fakeAgentService

	lastRequest *orchestration.RotateSessionAccountRequest
}

func (f *rotatingFakeAgentService) RotateSessionAccount(
	_ context.Context,
	req *orchestration.RotateSessionAccountRequest,
) (*orchestration.RotateSessionAccountResult, error) {
	f.lastRequest = req
	if err := f.err(); err != nil {
		return nil, err
	}
	return &orchestration.RotateSessionAccountResult{
		SessionID:         req.SessionID,
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              orchestration.AccountRotationModeInPlace,
	}, nil
}

var _ orchestration.SessionAccountRotationService = (*rotatingFakeAgentService)(nil)

func newRotatingOrchTransport(t *testing.T, fail bool) (*Transport, *rotatingFakeAgentService) {
	t.Helper()
	agent := &rotatingFakeAgentService{fakeAgentService: &fakeAgentService{fail: fail}}
	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        agent,
		proj:         &fakeProjectService{fail: fail},
		admin:        &fakeAgentAdminService{fail: fail},
	})
	require.NoError(t, err)
	return tr, agent
}

func TestEmbeddedRotateSessionAccount_Success(t *testing.T) {
	tr, agent := newRotatingOrchTransport(t, false)

	availableAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Second)
	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorSDK.RotateSessionAccountRequest{
			SessionID:    "sess-1",
			Reason:       "seat retired",
			HoldOutgoing: true,
			AvailableAt:  availableAt,
		})
	require.NoError(t, err)

	result, ok := resp.(*orchestratorSDK.RotateSessionAccountResponse)
	require.Truef(t, ok, "response is %T, want *orchestrator.RotateSessionAccountResponse", resp)
	assert.Equal(t, "sess-1", result.SessionID)
	assert.Equal(t, "acct-a", result.PreviousAccountID)
	assert.Equal(t, "acct-b", result.AccountID)
	assert.Equal(t, "in_place", result.Mode)

	require.NotNil(t, agent.lastRequest)
	assert.Equal(t, "seat retired", agent.lastRequest.Reason)
	assert.True(t, agent.lastRequest.HoldOutgoing)
	assert.True(t, availableAt.Equal(agent.lastRequest.AvailableAt))
}

func TestEmbeddedRotateSessionAccount_ServiceError(t *testing.T) {
	tr, _ := newRotatingOrchTransport(t, true)

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorSDK.RotateSessionAccountRequest{SessionID: "sess-1"})
	require.ErrorIs(t, err, errOrch)
	assert.Nil(t, resp)
}

func TestEmbeddedRotateSessionAccount_WrongType(t *testing.T) {
	tr, agent := newRotatingOrchTransport(t, false)

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorSDK.CloseSessionRequest{SessionID: "sess-1"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Nil(t, agent.lastRequest, "a mistyped request must not reach the orchestrator")
}

// TestEmbeddedRotateSessionAccount_UnsupportedDeployment covers the branch
// where the wired agent service predates the pooled account subsystem.
func TestEmbeddedRotateSessionAccount_UnsupportedDeployment(t *testing.T) {
	tr := newOrchTransport(t, false)

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorSDK.RotateSessionAccountRequest{SessionID: "sess-1"})
	require.ErrorIs(t, err, orchestration.ErrSessionRotationUnsupported)
	assert.Nil(t, resp)
}

// TestEmbeddedSpawnSession_CarriesAccountAllow is the embedded half of gap 2.
func TestEmbeddedSpawnSession_CarriesAccountAllow(t *testing.T) {
	agent := &capturingSpawnAgentService{fakeAgentService: &fakeAgentService{}}
	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        agent,
		proj:         &fakeProjectService{},
		admin:        &fakeAgentAdminService{},
	})
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "SpawnSession",
		&orchestratorSDK.SpawnSessionRequest{
			Backend:      "claude",
			AccountID:    "acct-a",
			AccountAllow: []string{"acct-a", "acct-b"},
		})
	require.NoError(t, err)

	require.NotNil(t, agent.spawned)
	assert.Equal(t, []string{"acct-a", "acct-b"}, agent.spawned.AccountAllow,
		"the embedded transport dropped account_allow")
}

// capturingSpawnAgentService records the SpawnSessionRequest the handler built.
type capturingSpawnAgentService struct {
	*fakeAgentService

	spawned *orchestration.SpawnSessionRequest
}

func (f *capturingSpawnAgentService) SpawnSession(
	_ context.Context,
	req *orchestration.SpawnSessionRequest,
) (*orchestration.SpawnSessionResponse, error) {
	f.spawned = req
	return &orchestration.SpawnSessionResponse{SessionID: "sess-1", Backend: req.Backend}, nil
}

// =============================================================================
// End-to-end-ish: a real orchestration service behind the in-process transport
// =============================================================================

// pooledTestBackend is a minimal AgentBackend backed by a pooled provider
// account. It rotates in place, which is the cheap mechanism the orchestrator
// prefers, and hands back whichever account nextAccount names — so a test can
// "move the pool" between the spawn and the rotation.
type pooledTestBackend struct {
	mu          sync.Mutex
	nextAccount string
	sessions    map[string]string // session id -> account currently serving it
}

func newPooledTestBackend(first string) *pooledTestBackend {
	return &pooledTestBackend{nextAccount: first, sessions: map[string]string{}}
}

func (b *pooledTestBackend) setNextAccount(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextAccount = id
}

// pick resolves the account a spawn or rotation should land on, honouring the
// request's exclusion and allow-list the same way the real resolver does.
func (b *pooledTestBackend) pick(req *orchestration.SpawnRequest) (accountID, strategy string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if req.AccountID != "" {
		return req.AccountID, "pinned"
	}
	if !req.AccountAuto {
		return "", ""
	}
	for _, excluded := range req.AccountExclude {
		if excluded == b.nextAccount {
			return "", ""
		}
	}
	if len(req.AccountAllow) > 0 {
		allowed := false
		for _, id := range req.AccountAllow {
			if id == b.nextAccount {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", ""
		}
	}
	return b.nextAccount, "least_usage"
}

func (b *pooledTestBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	accountID, strategy := b.pick(req)
	if accountID == "" {
		return nil, &orchestration.NoAccountAvailableError{Strategy: strategy}
	}
	id := "sess-" + accountID
	b.mu.Lock()
	b.sessions[id] = accountID
	b.mu.Unlock()
	return &orchestration.Session{
		ID:              id,
		Backend:         "pool",
		Status:          orchestration.SessionStateIdle,
		WorkDir:         req.WorkDir,
		CreatedAt:       time.Now().UnixNano(),
		AccountID:       accountID,
		AccountStrategy: strategy,
	}, nil
}

func (b *pooledTestBackend) RotateAccount(
	_ context.Context,
	req *orchestration.RotateAccountRequest,
) (*orchestration.RotateAccountResult, error) {
	accountID, strategy := b.pick(req.Spawn)
	if accountID == "" {
		return nil, &orchestration.NoAccountAvailableError{Strategy: strategy}
	}
	b.mu.Lock()
	b.sessions[req.SessionID] = accountID
	b.mu.Unlock()
	return &orchestration.RotateAccountResult{AccountID: accountID, Strategy: strategy}, nil
}

func (b *pooledTestBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	return &orchestration.PromptResponse{
		SessionID: req.SessionID,
		Content:   []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
	}, nil
}

func (b *pooledTestBackend) Status(_ context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	return &orchestration.SessionStatus{SessionID: sessionID, State: orchestration.SessionStateIdle}, nil
}

func (b *pooledTestBackend) Close(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
	return nil
}

func (b *pooledTestBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{
		Kind:               orchestration.BackendKindAgent,
		PersistentSessions: true,
		ACP:                true,
	}
}

func (b *pooledTestBackend) Type() string         { return "pool" }
func (b *pooledTestBackend) ToolGuidance() string { return "" }

var (
	_ orchestration.AgentBackend          = (*pooledTestBackend)(nil)
	_ orchestration.SessionAccountRotator = (*pooledTestBackend)(nil)
)

// embeddedTestLogger is a no-op interfaces.Logger for the in-process service.
type embeddedTestLogger struct{}

func (l *embeddedTestLogger) Debug(string, ...any)              {}
func (l *embeddedTestLogger) Debugf(string, ...interface{})     {}
func (l *embeddedTestLogger) Info(string, ...any)               {}
func (l *embeddedTestLogger) Infof(string, ...interface{})      {}
func (l *embeddedTestLogger) Warn(string, ...any)               {}
func (l *embeddedTestLogger) Warnf(string, ...interface{})      {}
func (l *embeddedTestLogger) Error(string, ...any)              {}
func (l *embeddedTestLogger) Errorf(string, ...interface{})     {}
func (l *embeddedTestLogger) Fatal(string)                      {}
func (l *embeddedTestLogger) Fatalf(string, ...interface{})     {}
func (l *embeddedTestLogger) WithError(error) interfaces.Logger { return l }

func (l *embeddedTestLogger) WithField(string, interface{}) interfaces.Logger {
	return l
}

func (l *embeddedTestLogger) WithFields(map[string]interface{}) interfaces.Logger {
	return l
}

// TestEmbeddedRotateSessionAccount_EndToEnd drives a REAL orchestration service
// through the in-process transport: spawn a session on a pooled account, ask
// the transport to rotate it, and assert the durable record moved to a
// different account AND opened a new attribution segment.
//
// The attribution segment is the point. A rotation that changed only the
// scalar AccountID would silently bill the replacement account for the
// outgoing account's turns.
func TestEmbeddedRotateSessionAccount_EndToEnd(t *testing.T) {
	backend := newPooledTestBackend("acct-a")
	store := orchestration.NewAgentStore(kvstore.NewMemory())
	agentSvc, err := orchestration.NewAgentService(
		store,
		map[string]orchestration.AgentBackend{"pool": backend},
		&embeddedTestLogger{},
	)
	require.NoError(t, err)

	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        agentSvc,
		proj:         &fakeProjectService{},
		admin:        &fakeAgentAdminService{},
	})
	require.NoError(t, err)

	ctx := orchestration.ContextWithTenant(context.Background(), "tenant-a")

	spawnResp, err := tr.Request(ctx, "quicbaas.Orchestrator", "SpawnSession",
		&orchestratorSDK.SpawnSessionRequest{
			Backend:      "pool",
			WorkDir:      "/e2e",
			AccountAuto:  true,
			AccountAllow: []string{"acct-a", "acct-b"},
		})
	require.NoError(t, err)
	spawn, ok := spawnResp.(*orchestratorSDK.SpawnSessionResponse)
	require.True(t, ok)
	require.NotEmpty(t, spawn.SessionID)

	// The pool moves: the strategy would now pick acct-b.
	backend.setNextAccount("acct-b")

	rotateResp, err := tr.Request(ctx, "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorSDK.RotateSessionAccountRequest{
			SessionID: spawn.SessionID,
			Reason:    "operator says this seat is bad",
		})
	require.NoError(t, err)
	rotation, ok := rotateResp.(*orchestratorSDK.RotateSessionAccountResponse)
	require.True(t, ok)

	assert.Equal(t, spawn.SessionID, rotation.SessionID, "a rotation must never rename the session")
	assert.Equal(t, "acct-a", rotation.PreviousAccountID)
	assert.Equal(t, "acct-b", rotation.AccountID)
	assert.NotEqual(t, rotation.PreviousAccountID, rotation.AccountID,
		"the operator asked to be moved off this account")

	record, err := store.GetSession(ctx, "tenant-a", spawn.SessionID)
	require.NoError(t, err)
	assert.Equal(t, "acct-b", record.AccountID, "the durable record still names the old account")

	require.Len(t, record.AccountHistory, 2, "a rotation must close one attribution segment and open another")
	first := record.AccountHistory[0]
	second := record.AccountHistory[1]
	assert.Equal(t, "acct-a", first.AccountID)
	assert.NotZero(t, first.To, "the outgoing segment must be closed or its account keeps being billed")
	assert.Equal(t, "operator says this seat is bad", first.Reason)
	assert.Equal(t, "acct-b", second.AccountID)
	assert.Zero(t, second.To, "exactly one segment is in force on a live record")
	assert.GreaterOrEqual(t, second.From, first.To, "the new segment must open no earlier than the old one closed")
}
