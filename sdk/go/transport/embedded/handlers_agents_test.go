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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/spend"
)

// catalogAgentService is an agent service that holds a real agent catalog.
type catalogAgentService struct {
	orchestration.AgentService
	directory *orchestration.AgentDirectory
}

func (s catalogAgentService) AgentDirectory() *orchestration.AgentDirectory { return s.directory }

// agentsMock is the facade with an agent service; agent is nil for a node
// with no orchestrator.
type agentsMock struct {
	*mockQuicBaaS
	agent orchestration.AgentService
}

func (m *agentsMock) Agent() orchestration.AgentService { return m.agent }

// anyModels is a backend catalog that serves every backend with no curated
// model list.
type anyModels struct{}

func (anyModels) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

// anyAccounts is an account store that holds every account named, each a
// codex account: the round trip's Agent runs on codex, and the catalog refuses
// an account of another provider than its backend draws on.
type anyAccounts struct{}

func (anyAccounts) Get(_ context.Context, tenantID, accountID string) (*orchestration.Account, error) {
	return &orchestration.Account{ID: accountID, TenantID: tenantID, Provider: orchestration.AccountProviderCodex}, nil
}

func newAgentsTransport(t *testing.T) *Transport {
	t.Helper()
	directory, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: anyModels{}, Accounts: anyAccounts{},
	})
	require.NoError(t, err)
	tr, err := New(&agentsMock{mockQuicBaaS: &mockQuicBaaS{}, agent: catalogAgentService{directory: directory}})
	require.NoError(t, err)
	return tr
}

func agentsTenantCtx(tenant string) context.Context {
	return orchestration.ContextWithTenant(iam.ContextWithTenant(context.Background(), tenant), tenant)
}

// TestAgentsEmbedded_TheCatalogRoundTrips drives every verb in-process and
// checks that every field of an Agent survives the conversion both ways.
func TestAgentsEmbedded_TheCatalogRoundTrips(t *testing.T) {
	tr := newAgentsTransport(t)
	ctx := agentsTenantCtx("acme")
	request := func(method string, req interface{}) interface{} {
		t.Helper()
		resp, err := tr.Request(ctx, agents.ServiceName, method, req)
		require.NoError(t, err, method)
		return resp
	}

	full := &agents.Agent{
		ID: "codex-reviewer", Name: "Codex reviewer", Description: "reviews", Roles: []string{"review", "triage"},
		Backend: "codex", Model: "gpt-5.5", Image: "worker:1", ReasoningEffort: "high",
		Provider:      agents.ProviderEndpoint{Name: "local", BaseURL: "http://llm.test/v1", WireAPI: "responses", RequiresAuth: true},
		Account:       agents.AccountPolicy{Strategy: "least_usage", Accounts: []string{"codex-1", "codex-2"}},
		Prompt:        agents.Prompt{System: "Be terse."},
		MCPScopes:     []string{"kv:read"},
		Capabilities:  []string{"gpu"},
		ContextTokens: 200000, MaxConcurrent: 2,
		Placement: agents.RolePolicy{Prefer: "review", Reserve: 1},
		Spend:     spend.Limits{Windows: []spend.Window{{Name: "daily", Size: 24 * time.Hour, MaxEquivalentMicros: 5_000_000}}},
	}
	created := request("CreateAgent", &agents.SaveAgentRequest{TenantID: "acme", Agent: full}).(*agents.Agent)
	assert.False(t, created.CreatedAt.IsZero())
	got := request("GetAgent", &agents.AgentRequest{TenantID: "acme", AgentID: "codex-reviewer"}).(*agents.Agent)
	got.CreatedAt, got.UpdatedAt = full.CreatedAt, full.UpdatedAt
	assert.Equal(t, full, got, "every field survives the round trip")

	full.Model = "gpt-5.6"
	updated := request("UpdateAgent", &agents.SaveAgentRequest{TenantID: "acme", AgentID: "codex-reviewer", Agent: full}).(*agents.Agent)
	assert.Equal(t, "gpt-5.6", updated.Model)
	assert.Len(t, request("ListAgents", &agents.ListAgentsRequest{TenantID: "acme"}).(*agents.AgentList).Agents, 1)

	pool := request("CreatePool", &agents.SavePoolRequest{TenantID: "acme", Pool: &agents.Pool{ID: "reviewers", Name: "Reviewers", Agents: []string{"codex-reviewer"}}}).(*agents.Pool)
	assert.Equal(t, []string{"codex-reviewer"}, pool.Agents)
	assert.Equal(t, "Reviewers", request("GetPool", &agents.PoolRequest{TenantID: "acme", PoolID: "reviewers"}).(*agents.Pool).Name)
	request("UpdatePool", &agents.SavePoolRequest{TenantID: "acme", PoolID: "reviewers", Pool: &agents.Pool{Name: "Seats", Agents: []string{"codex-reviewer"}}})
	assert.Len(t, request("ListPools", &agents.ListPoolsRequest{TenantID: "acme"}).(*agents.PoolList).Pools, 1)

	// The pool references the Agent, so the Agent names it as a user and
	// cannot be deleted.
	used := request("AgentUsedBy", &agents.AgentRequest{TenantID: "acme", AgentID: "codex-reviewer"}).(*agents.UsageList)
	require.Len(t, used.UsedBy, 1)
	assert.Equal(t, agents.Usage{Kind: "pool", ID: "reviewers", Slot: "member"}, used.UsedBy[0])
	_, err := tr.Request(ctx, agents.ServiceName, "DeleteAgent", &agents.AgentRequest{TenantID: "acme", AgentID: "codex-reviewer"})
	assert.True(t, errors.Is(err, orchestration.ErrAgentInUse), "err = %v", err)

	assert.Empty(t, request("PoolUsedBy", &agents.PoolRequest{TenantID: "acme", PoolID: "reviewers"}).(*agents.UsageList).UsedBy)
	assert.True(t, request("DeletePool", &agents.PoolRequest{TenantID: "acme", PoolID: "reviewers"}).(*agents.Deleted).Deleted)
	assert.True(t, request("DeleteAgent", &agents.AgentRequest{TenantID: "acme", AgentID: "codex-reviewer"}).(*agents.Deleted).Deleted)
}

func TestAgentsEmbedded_RefusesWhatItCannotServe(t *testing.T) {
	tr := newAgentsTransport(t)
	ctx := agentsTenantCtx("acme")
	for _, method := range []string{"ListAgents", "GetAgent", "CreateAgent", "UpdateAgent", "DeleteAgent", "AgentUsedBy",
		"ListPools", "GetPool", "CreatePool", "UpdatePool", "DeletePool", "PoolUsedBy"} {
		_, err := tr.Request(ctx, agents.ServiceName, method, &struct{}{})
		assert.Error(t, err, "%s accepted a request of the wrong type", method)
	}
	_, err := tr.Request(ctx, agents.ServiceName, "GetAgent", &agents.AgentRequest{TenantID: "acme", AgentID: "ghost"})
	assert.True(t, errors.Is(err, orchestration.ErrAgentUnknown), "err = %v", err)
	_, err = tr.Request(ctx, agents.ServiceName, "ListAgents", &agents.ListAgentsRequest{TenantID: "other"})
	assert.Error(t, err, "another tenant's catalog is refused")

	noOrchestrator, err := New(&agentsMock{mockQuicBaaS: &mockQuicBaaS{}})
	require.NoError(t, err)
	_, err = noOrchestrator.Request(ctx, agents.ServiceName, "ListAgents", &agents.ListAgentsRequest{TenantID: "acme"})
	assert.Error(t, err, "a node with no orchestrator has no catalog")

	noCatalog, err := New(&agentsMock{mockQuicBaaS: &mockQuicBaaS{}, agent: struct{ orchestration.AgentService }{}})
	require.NoError(t, err)
	_, err = noCatalog.Request(ctx, agents.ServiceName, "ListPools", &agents.ListPoolsRequest{TenantID: "acme"})
	assert.True(t, errors.Is(err, orchestration.ErrAgentCatalogUnavailable), "err = %v", err)
	assert.True(t, tr.SupportsAgents())
}
