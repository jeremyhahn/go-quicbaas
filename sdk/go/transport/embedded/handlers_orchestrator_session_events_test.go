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

package embedded

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// quicbaas.Orchestrator/GetSessionHistory and SubscribeSessionEvents over the
// in-process transport
// =============================================================================

// sessionEventsFixture is an in-process transport over an agent service whose
// session event log already holds a session's opening exchange.
type sessionEventsFixture struct {
	tr  *Transport
	log orchestration.SessionEventLog
}

const (
	sessionFixtureTenant  = "t1"
	sessionFixtureSession = "s1"
)

func newSessionEventsFixture(t *testing.T) *sessionEventsFixture {
	t.Helper()
	tr := newConversationTransport(t, sessionFixtureTenant, sessionFixtureSession)
	agent, ok := tr.service.Agent().(*loggingFakeAgentService)
	require.True(t, ok)
	return &sessionEventsFixture{tr: tr, log: agent.log}
}

// tenantContext binds a call to tenantID the way an authenticated caller is.
func tenantContext(tenantID string) context.Context {
	return qrdbsdk.ContextWithTenant(context.Background(), tenantID)
}

// eventKind decodes the Kind of the session event an envelope carries.
func eventKind(t *testing.T, env *orchestratorSDK.SessionEventEnvelope) orchestration.SessionEventKind {
	t.Helper()
	var ev orchestration.SessionEvent
	require.NoError(t, json.Unmarshal(env.PayloadJSON, &ev))
	return ev.Kind
}

// TestEmbeddedGetSessionHistory_PagesTheEventLog asserts an in-process SDK
// caller reads the session's raw event log, one page at a time.
func TestEmbeddedGetSessionHistory_PagesTheEventLog(t *testing.T) {
	f := newSessionEventsFixture(t)
	client := orchestratorSDK.NewClient(f.tr)
	ctx := tenantContext(sessionFixtureTenant)

	first, err := client.GetSessionHistory(ctx, &orchestratorSDK.GetSessionHistoryRequest{
		TenantID: sessionFixtureTenant, SessionID: sessionFixtureSession, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, first.Events, 1)
	assert.Equal(t, uint64(1), first.Events[0].Sequence)
	assert.Equal(t, sessionFixtureSession, first.Events[0].SessionID)
	assert.Equal(t, sessionFixtureTenant, first.Events[0].TenantID)
	assert.Equal(t, orchestration.KindPromptSubmitted, eventKind(t, first.Events[0]))
	require.Equal(t, uint64(2), first.NextSeq, "a full page names the next one")

	rest, err := client.GetSessionHistory(ctx, &orchestratorSDK.GetSessionHistoryRequest{
		SessionID: sessionFixtureSession, FromSequence: first.NextSeq,
	})
	require.NoError(t, err)
	require.Len(t, rest.Events, 1)
	assert.Equal(t, orchestration.KindAssistantChunk, eventKind(t, rest.Events[0]))
	assert.Zero(t, rest.NextSeq, "the last page names no next one")
}

// TestEmbeddedGetSessionHistory_RejectsBadInput asserts a mistyped or typed-nil
// request, an unaddressed one, a deployment with no event log and a caller
// naming another tenant are all typed refusals, never an empty page.
func TestEmbeddedGetSessionHistory_RejectsBadInput(t *testing.T) {
	ctx := tenantContext(sessionFixtureTenant)

	t.Run("wrong request type", func(t *testing.T) {
		f := newSessionEventsFixture(t)
		_, err := f.tr.Request(ctx, "quicbaas.Orchestrator", "GetSessionHistory",
			&orchestratorSDK.GetSessionConversationRequest{SessionID: sessionFixtureSession})
		var typeErr *InvalidRequestTypeError
		require.ErrorAs(t, err, &typeErr)
	})

	t.Run("typed nil request", func(t *testing.T) {
		f := newSessionEventsFixture(t)
		_, err := f.tr.Request(ctx, "quicbaas.Orchestrator", "GetSessionHistory",
			(*orchestratorSDK.GetSessionHistoryRequest)(nil))
		var typeErr *InvalidRequestTypeError
		require.ErrorAs(t, err, &typeErr)
	})

	t.Run("no session id", func(t *testing.T) {
		f := newSessionEventsFixture(t)
		_, err := f.tr.Request(ctx, "quicbaas.Orchestrator", "GetSessionHistory",
			&orchestratorSDK.GetSessionHistoryRequest{TenantID: sessionFixtureTenant})
		require.ErrorIs(t, err, orchestration.ErrSessionIDRequired)
	})

	t.Run("agent service keeps no event log", func(t *testing.T) {
		tr, err := New(&orchMock{
			mockQuicBaaS: &mockQuicBaaS{},
			agent:        &fakeAgentService{},
			proj:         &fakeProjectService{},
			admin:        &fakeFleetAdminService{},
		})
		require.NoError(t, err)
		_, reqErr := orchestratorSDK.NewClient(tr).GetSessionHistory(ctx,
			&orchestratorSDK.GetSessionHistoryRequest{SessionID: sessionFixtureSession})
		require.ErrorIs(t, reqErr, orchestration.ErrSessionLogRequired)
	})

	t.Run("unknown session is not found", func(t *testing.T) {
		f := newSessionEventsFixture(t)
		_, err := orchestratorSDK.NewClient(f.tr).GetSessionHistory(ctx,
			&orchestratorSDK.GetSessionHistoryRequest{SessionID: "no-such-session"})
		assert.True(t, sdkerrors.IsNotFound(err), "got %v", err)
		require.ErrorIs(t, err, orchestration.ErrSessionLogNotFound)
	})

	t.Run("registered session with no events reads an empty page", func(t *testing.T) {
		f := newSessionEventsFixture(t)
		require.NoError(t, f.log.PutMeta(context.Background(), &orchestration.SessionMeta{
			SessionID: "s-empty", TenantID: sessionFixtureTenant,
		}))
		page, err := orchestratorSDK.NewClient(f.tr).GetSessionHistory(ctx,
			&orchestratorSDK.GetSessionHistoryRequest{SessionID: "s-empty"})
		require.NoError(t, err)
		assert.Empty(t, page.Events)
		assert.Zero(t, page.NextSeq)
	})

	t.Run("caller names another tenant", func(t *testing.T) {
		f := newSessionEventsFixture(t)
		_, err := orchestratorSDK.NewClient(f.tr).GetSessionHistory(tenantContext("tenant-b"),
			&orchestratorSDK.GetSessionHistoryRequest{TenantID: sessionFixtureTenant, SessionID: sessionFixtureSession})
		require.True(t, isCrossTenant(err), "got %v", err)
	})
}

// TestEmbeddedSubscribeSessionEvents_ReplaysThenTails asserts an in-process
// SDK subscriber receives the logged history, then an event appended while it
// is following, in sequence order.
func TestEmbeddedSubscribeSessionEvents_ReplaysThenTails(t *testing.T) {
	f := newSessionEventsFixture(t)
	ctx, cancel := context.WithTimeout(tenantContext(sessionFixtureTenant), 10*time.Second)
	defer cancel()

	subCtx, stop := context.WithCancel(ctx)
	defer stop()

	events, err := orchestratorSDK.NewClient(f.tr).SubscribeSessionEvents(subCtx,
		&orchestratorSDK.SubscribeSessionEventsRequest{SessionID: sessionFixtureSession, Follow: true})
	require.NoError(t, err)

	next := func() *orchestratorSDK.SessionEventEnvelope {
		t.Helper()
		select {
		case env, open := <-events:
			require.True(t, open, "the stream closed before the expected event")
			return env
		case <-ctx.Done():
			require.FailNow(t, "no event before the deadline")
			return nil
		}
	}

	for _, want := range []uint64{1, 2} {
		env := next()
		assert.Equal(t, want, env.Sequence)
		assert.True(t, env.FromReplay, "history arrives as replay")
	}

	require.NoError(t, orchestration.AppendNextSessionEvent(ctx, f.log, &orchestration.SessionEvent{
		SessionID: sessionFixtureSession, TenantID: sessionFixtureTenant,
		Kind:   orchestration.KindPromptSubmitted,
		Prompt: &orchestration.PromptSubmittedPayload{Prompt: "and the next"},
	}))
	live := next()
	assert.Equal(t, uint64(3), live.Sequence)
	assert.False(t, live.FromReplay, "a tailed event is live")
	assert.Equal(t, orchestration.KindPromptSubmitted, eventKind(t, live))

	// Cancelling the subscription's context ends the stream: the channel
	// closes with nothing further on it.
	stop()
	for {
		select {
		case env, open := <-events:
			if !open {
				return
			}
			require.Failf(t, "an event after cancel", "got sequence %d", env.Sequence)
		case <-ctx.Done():
			require.FailNow(t, "the cancelled subscription did not end")
		}
	}
}

// TestEmbeddedSubscribeSessionEvents_UnknownSessionNotFound proves a
// subscription to a session with no metadata and no logged event ends its
// stream with a typed not-found error and no event.
func TestEmbeddedSubscribeSessionEvents_UnknownSessionNotFound(t *testing.T) {
	f := newSessionEventsFixture(t)
	s, err := f.tr.RequestStream(tenantContext(sessionFixtureTenant), "quicbaas.Orchestrator", "SubscribeSessionEvents",
		&orchestratorSDK.SubscribeSessionEventsRequest{SessionID: "no-such-session", Follow: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	msg, recvErr := s.Recv()
	require.NoError(t, recvErr)
	streamErr, ok := msg.(error)
	require.Truef(t, ok, "expected a refusal, got %T", msg)
	assert.True(t, sdkerrors.IsNotFound(streamErr), "got %v", streamErr)
	require.ErrorIs(t, streamErr, orchestration.ErrSessionLogNotFound)
}

// TestEmbeddedSubscribeSessionEvents_ReplayOnlyEnds asserts a subscriber that
// does not follow gets the history from its requested sequence and then the
// stream ends.
func TestEmbeddedSubscribeSessionEvents_ReplayOnlyEnds(t *testing.T) {
	f := newSessionEventsFixture(t)
	ctx, cancel := context.WithTimeout(tenantContext(sessionFixtureTenant), 10*time.Second)
	defer cancel()

	events, err := orchestratorSDK.NewClient(f.tr).SubscribeSessionEvents(ctx,
		&orchestratorSDK.SubscribeSessionEventsRequest{SessionID: sessionFixtureSession, FromSequence: 2})
	require.NoError(t, err)

	var got []uint64
	for {
		select {
		case env, open := <-events:
			if !open {
				assert.Equal(t, []uint64{2}, got)
				return
			}
			got = append(got, env.Sequence)
		case <-ctx.Done():
			require.FailNow(t, "the replay-only stream did not end")
		}
	}
}

// TestEmbeddedSubscribeSessionEvents_RejectsBadInput asserts every unusable
// subscription ends its stream with a typed error and no event.
func TestEmbeddedSubscribeSessionEvents_RejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		ctx   context.Context
		req   interface{}
		check func(t *testing.T, err error)
	}{
		{
			name: "wrong request type",
			ctx:  tenantContext(sessionFixtureTenant),
			req:  &orchestratorSDK.GetSessionHistoryRequest{SessionID: sessionFixtureSession},
			check: func(t *testing.T, err error) {
				var typeErr *InvalidRequestTypeError
				require.ErrorAs(t, err, &typeErr)
			},
		},
		{
			name: "typed nil request",
			ctx:  tenantContext(sessionFixtureTenant),
			req:  (*orchestratorSDK.SubscribeSessionEventsRequest)(nil),
			check: func(t *testing.T, err error) {
				var typeErr *InvalidRequestTypeError
				require.ErrorAs(t, err, &typeErr)
			},
		},
		{
			name: "no session id",
			ctx:  tenantContext(sessionFixtureTenant),
			req:  &orchestratorSDK.SubscribeSessionEventsRequest{Follow: true},
			check: func(t *testing.T, err error) {
				require.ErrorIs(t, err, orchestration.ErrSessionIDRequired)
			},
		},
		{
			name: "caller names another tenant",
			ctx:  tenantContext("tenant-b"),
			req: &orchestratorSDK.SubscribeSessionEventsRequest{
				TenantID: sessionFixtureTenant, SessionID: sessionFixtureSession, Follow: true,
			},
			check: func(t *testing.T, err error) {
				require.True(t, isCrossTenant(err), "got %v", err)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSessionEventsFixture(t)
			s, err := f.tr.RequestStream(tt.ctx, "quicbaas.Orchestrator", "SubscribeSessionEvents", tt.req)
			require.NoError(t, err)
			defer func() { require.NoError(t, s.Close()) }()

			msg, recvErr := s.Recv()
			if recvErr == nil {
				err, _ = msg.(error)
			} else {
				err = recvErr
			}
			require.Error(t, err, "expected a refusal, got %v", msg)
			tt.check(t, err)
		})
	}
}
