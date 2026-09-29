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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// capturingListSessionsAgentService records the service request the embedded
// handler built from the SDK request.
type capturingListSessionsAgentService struct {
	*fakeAgentService

	listed *orchestration.ListSessionsRequest
}

func (f *capturingListSessionsAgentService) ListSessions(
	_ context.Context,
	req *orchestration.ListSessionsRequest,
) (*orchestration.ListSessionsResponse, error) {
	f.listed = req
	return &orchestration.ListSessionsResponse{}, nil
}

func newListSessionsTransport(t *testing.T) (*Transport, *capturingListSessionsAgentService) {
	t.Helper()
	agent := &capturingListSessionsAgentService{fakeAgentService: &fakeAgentService{}}
	tr, err := New(&orchMock{
		mockQuicBaaS: &mockQuicBaaS{},
		agent:        agent,
		proj:         &fakeProjectService{},
		admin:        &fakeFleetAdminService{},
	})
	require.NoError(t, err)
	return tr, agent
}

// TestEmbeddedListSessions_ForwardsStatusFilter holds the in-process transport
// to the same contract as the wire ones: the lifecycle filter reaches the
// service. The gRPC transport dropped it (go-quicbaas#792), which handed
// callers closed records back under an "active" query.
func TestEmbeddedListSessions_ForwardsStatusFilter(t *testing.T) {
	tr, agent := newListSessionsTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestratorSDK.ListSessionsRequest{Backend: "claude", Status: "active"})
	require.NoError(t, err)

	require.NotNil(t, agent.listed)
	assert.Equal(t, orchestration.SessionState("active"), agent.listed.Status,
		"the embedded transport dropped the status filter")
	assert.Equal(t, "claude", agent.listed.Backend)
}

// TestEmbeddedListSessions_EmptyStatusStaysEmpty proves an unset filter is
// passed through as the empty state, which the service reads as every state.
func TestEmbeddedListSessions_EmptyStatusStaysEmpty(t *testing.T) {
	tr, agent := newListSessionsTransport(t)

	_, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListSessions",
		&orchestratorSDK.ListSessionsRequest{})
	require.NoError(t, err)

	require.NotNil(t, agent.listed)
	assert.Equal(t, orchestration.SessionState(""), agent.listed.Status)
	assert.Empty(t, agent.listed.Backend)
}

// capturingSpawnPersistentService records the service request the embedded
// handler built.
type capturingSpawnPersistentService struct {
	*fakeAgentService

	spawned *orchestration.SpawnSessionRequest
}

func (f *capturingSpawnPersistentService) SpawnSession(
	_ context.Context,
	req *orchestration.SpawnSessionRequest,
) (*orchestration.SpawnSessionResponse, error) {
	f.spawned = req
	return &orchestration.SpawnSessionResponse{SessionID: "sess-1", Backend: req.Backend}, nil
}

// TestEmbeddedSpawnSession_PersistentMatchesTheWireTransports holds the
// in-process transport to the same default as gRPC and REST.
//
// A non-persistent session is never resumable and is concluded rather than
// restored at boot, so this flag decides whether a session is eligible for the
// reconciliation this PR adds at all. The embedded handler never set it, so an
// in-process caller silently got one-shot sessions.
func TestEmbeddedSpawnSession_PersistentMatchesTheWireTransports(t *testing.T) {
	yes, no := true, false
	cases := []struct {
		name string
		req  *orchestratorSDK.SpawnSessionRequest
		want bool
	}{
		{"unset defaults to persistent", &orchestratorSDK.SpawnSessionRequest{Backend: "claude"}, true},
		{"explicit true", &orchestratorSDK.SpawnSessionRequest{Backend: "claude", Persistent: &yes}, true},
		{"explicit false", &orchestratorSDK.SpawnSessionRequest{Backend: "claude", Persistent: &no}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			agent := &capturingSpawnPersistentService{fakeAgentService: &fakeAgentService{}}
			tr, err := New(&orchMock{
				mockQuicBaaS: &mockQuicBaaS{},
				agent:        agent,
				proj:         &fakeProjectService{},
				admin:        &fakeFleetAdminService{},
			})
			require.NoError(t, err)

			_, err = tr.Request(context.Background(), "quicbaas.Orchestrator", "SpawnSession", tc.req)
			require.NoError(t, err)
			require.NotNil(t, agent.spawned)
			assert.Equal(t, tc.want, agent.spawned.Persistent,
				"the embedded transport must resolve Persistent exactly as the wire ones do")
		})
	}
}
