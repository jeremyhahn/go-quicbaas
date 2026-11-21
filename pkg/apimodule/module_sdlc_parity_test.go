//go:build rest && mcp

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

package apimodule

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/giteafake"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Cross-transport SDLC parity (go-quicbaas#124)
//
// Drives the SDK SDLC client over every transport against ONE real SDLC
// service — in-memory store, a real project service, and a fake Gitea only at
// the forge boundary — and asserts each verb answers alike on every
// transport: bind, binding, forges, delivery, the reviewer selection and its
// override, conformance and unbind, and an unknown project's typed error.
//
// QUIC is the REST transport over HTTP/3 (sdk/go/transport/quic embeds the
// REST transport and the server mounts the same router), so the REST row
// covers its wire format.
// =============================================================================

// sdlcParityProject is the one project every transport binds in turn; the
// fake forge serves exactly its repository.
const sdlcParityProject = "sdlc-parity"

// sdlcParityTask is the task whose delivery the rig seeds.
const sdlcParityTask = "task-parity"

// sdlcParityPullRequest is the pull request the reviewer verbs address.
const sdlcParityPullRequest = 41

// sdlcParityKV gives every tenant its own in-memory store.
type sdlcParityKV struct {
	mu     sync.Mutex
	stores map[string]qrdbsdk.KVStore
}

func (r *sdlcParityKV) Resolve(_ context.Context, tenantID string) (qrdbsdk.KVStore, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	store, ok := r.stores[tenantID]
	if !ok {
		store = qrdbsdk.NewMemoryKVStore()
		r.stores[tenantID] = store
	}
	return store, nil
}

// sdlcParitySecrets resolves every token reference the binding names.
type sdlcParitySecrets struct{ service.SecretsService }

func (sdlcParitySecrets) GetSecret(context.Context, *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{Data: []byte("token")}, nil
}

// sdlcParityAgents is the tenant catalog the binding's seats are checked
// against.
type sdlcParityAgents struct{}

func (sdlcParityAgents) Catalogue(context.Context, string) (orchestration.AgentCatalogue, error) {
	catalogue := orchestration.AgentCatalogue{Agents: map[string]orchestration.Agent{}, Pools: map[string]orchestration.AgentPool{}}
	for _, agent := range []orchestration.Agent{
		{ID: "claude-coder", Name: "claude-coder", Backend: "claude", Model: "claude-opus-5-5",
			Roles: []orchestration.AgentRole{orchestration.AgentRoleCoding, orchestration.AgentRoleTriage}},
		{ID: "codex-reviewer", Name: "codex-reviewer", Backend: "codex", Model: "gpt-5.5",
			Roles: []orchestration.AgentRole{orchestration.AgentRoleReview}},
		{ID: "qwen-reviewer", Name: "qwen-reviewer", Backend: "ollama-agent", Model: "qwen3.8:27b-q4_K_M",
			Roles: []orchestration.AgentRole{orchestration.AgentRoleReview}},
	} {
		catalogue.Agents[agent.ID] = agent
	}
	return catalogue, nil
}

// sdlcParityService builds the one real SDLC service every transport in this
// test reaches, with sdlcParityProject on record and one delivery seeded. It
// returns the fake forge's API URL for the binding to point at.
func sdlcParityService(t *testing.T) (*service.SDLCService, string) {
	t.Helper()
	ctx := context.Background()

	forge := giteafake.New(t, "acme", sdlcParityProject)
	store, err := sdlc.NewStore(&sdlcParityKV{stores: map[string]qrdbsdk.KVStore{}})
	require.NoError(t, err)
	node := qrdbnode.Shared(t)
	routes, err := sdlc.NewWebhookRouteIndex(node.Transaction, node.KV)
	require.NoError(t, err)
	guard, err := sdlc.NewRepoHostGuard([]string{"forge.test"}, false)
	require.NoError(t, err)
	verification, err := orchestration.NewExecutionGuard([]string{"make test"})
	require.NoError(t, err)

	tasks, err := orchestration.NewAgentService(orchestration.NewInMemoryAgentStore(), nil, logger.NewAdapter(testLogger(t)))
	require.NoError(t, err)
	taskWirer, ok := tasks.(interface{ SetTaskStore(orchestration.TaskStore) })
	require.True(t, ok, "the agent service no longer accepts a task store")
	taskWirer.SetTaskStore(orchestration.NewInMemoryTaskStore())

	projects := orchestration.NewInMemoryProjectStore()
	require.NoError(t, projects.Create(ctx, &orchestration.Project{
		ID: sdlcParityProject, TenantID: parityProjectTenant, Name: sdlcParityProject,
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/" + sdlcParityProject + ".git",
			Author:  orchestration.GitAuthor{Name: "agent", Email: "agent@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent"}},
		},
	}))

	svc, err := service.NewSDLCService(service.SDLCServiceConfig{
		Store: store, Routes: routes, Guard: guard, Verification: verification,
		Projects: service.NewProjectService(projects), ProjectWriter: projects, Tasks: tasks,
		Secrets: sdlcParitySecrets{}, WebhookPublicURL: "https://factory.test", Agents: sdlcParityAgents{},
	})
	require.NoError(t, err)

	require.NoError(t, store.PutDelivery(ctx, parityProjectTenant, &sdlc.Delivery{
		TaskID: sdlcParityTask, ProjectID: sdlcParityProject, Attempt: 1,
		Branch: "task/parity", Base: "main", Target: "main", RequiresPullRequest: true,
		PullRequest: sdlc.DeliveryPullRequest{Number: 9, State: sdlc.PullRequestOpen},
	}))
	return svc, forge.APIURL()
}

// sdlcParityTransports builds one SDK transport per reachable transport, each
// served by the real server-side SDLC adapter over svc.
func sdlcParityTransports(t *testing.T, svc *service.SDLCService) map[string]transport.Transport {
	t.Helper()

	facade := mocks.NewMockQuicBaaS()
	facade.SetSDLCService(svc)
	return parityTransports(t, facade, func(srv grpclib.ServiceRegistrar) {
		pb.RegisterSDLCServiceServer(srv, grpcapi.NewSDLCServer(facade, testLogger(t)))
	})
}

// sdlcParityBinding is a self-reviewed binding with one advisory alternate
// reviewer, so the reviewer verbs have a profile to switch to.
func sdlcParityBinding(forgeURL string) sdlcclient.ProjectBinding {
	advisory := false
	return sdlcclient.ProjectBinding{
		ProjectID: sdlcParityProject,
		Forge: sdlcclient.ForgeConfig{Type: "gitea", BaseURL: forgeURL, Owner: "acme", Repo: sdlcParityProject,
			TriggerLabel: "task:ready", TokenSecretRef: "tok"},
		Strategy:     "github-flow",
		Worker:       agents.AgentRef{AgentID: "claude-coder"},
		Verification: []string{"make test"},
		Review:       sdlcclient.ReviewPolicy{RequiredApprovals: 1},
		Reviewer:     sdlcclient.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"},
		Reviewers: []sdlcclient.ReviewerConfig{{
			Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer", TokenSecretRef: "qwen-tok",
			CountsTowardMerge: &advisory,
		}},
	}
}

// sdlcRoundTrip is everything one transport answered across the round trip.
type sdlcRoundTrip struct {
	Bound       *sdlcclient.ProjectBinding
	Read        *sdlcclient.ProjectBinding
	Forges      *sdlcclient.ListForgesResponse
	Delivery    *sdlcclient.Delivery
	Default     *sdlcclient.ReviewerSelection
	Overridden  *sdlcclient.ReviewerSelection
	Cleared     *sdlcclient.ReviewerSelection
	Conformance *sdlcclient.ConformanceReport
}

// runSDLCRoundTrip binds the project over client, drives every verb, unbinds
// it and requires the binding gone. Times the server stamps are checked set
// and then cleared, since each transport's pass stamps its own.
func runSDLCRoundTrip(t *testing.T, client sdlcclient.Client, forgeURL string) *sdlcRoundTrip {
	t.Helper()
	ctx := context.Background()
	var trip sdlcRoundTrip
	var err error

	trip.Bound, err = client.Bind(ctx, &sdlcclient.BindRequest{
		TenantID: parityProjectTenant, ProjectID: sdlcParityProject, Binding: sdlcParityBinding(forgeURL),
	})
	require.NoError(t, err, "Bind")
	trip.Read, err = client.Binding(ctx, parityProjectTenant, sdlcParityProject)
	require.NoError(t, err, "Binding")
	for _, binding := range []*sdlcclient.ProjectBinding{trip.Bound, trip.Read} {
		assert.False(t, binding.CreatedAt.IsZero(), "binding CreatedAt")
		assert.False(t, binding.UpdatedAt.IsZero(), "binding UpdatedAt")
		binding.CreatedAt, binding.UpdatedAt = time.Time{}, time.Time{}
	}

	trip.Forges, err = client.ListForges(ctx)
	require.NoError(t, err, "ListForges")

	trip.Delivery, err = client.Delivery(ctx, parityProjectTenant, sdlcParityTask)
	require.NoError(t, err, "Delivery")
	trip.Delivery.CreatedAt = trip.Delivery.CreatedAt.Truncate(time.Second).UTC()
	trip.Delivery.UpdatedAt = trip.Delivery.UpdatedAt.Truncate(time.Second).UTC()

	trip.Default, err = client.Reviewer(ctx, parityProjectTenant, sdlcParityProject, sdlcParityPullRequest)
	require.NoError(t, err, "Reviewer")
	trip.Overridden, err = client.SetReviewer(ctx, &sdlcclient.SetReviewerRequest{
		TenantID: parityProjectTenant, ProjectID: sdlcParityProject, PullRequest: sdlcParityPullRequest, Profile: "qwen",
	})
	require.NoError(t, err, "SetReviewer")
	assert.False(t, trip.Overridden.UpdatedAt.IsZero(), "override UpdatedAt")
	trip.Overridden.UpdatedAt = time.Time{}
	trip.Cleared, err = client.ClearReviewer(ctx, parityProjectTenant, sdlcParityProject, sdlcParityPullRequest)
	require.NoError(t, err, "ClearReviewer")

	trip.Conformance, err = client.Conformance(ctx, parityProjectTenant, sdlcParityProject)
	require.NoError(t, err, "Conformance")
	assert.False(t, trip.Conformance.CheckedAt.IsZero(), "conformance CheckedAt")
	trip.Conformance.CheckedAt = time.Time{}

	require.NoError(t, client.Unbind(ctx, parityProjectTenant, sdlcParityProject), "Unbind")
	_, err = client.Binding(ctx, parityProjectTenant, sdlcParityProject)
	assert.True(t, sdkerrors.IsNotFound(err), "Binding after Unbind: %v", err)
	return &trip
}

// TestSDLCTransportParity_RoundTrip binds, reads, overrides, checks and
// unbinds one project through every transport in turn and requires every
// transport to answer each verb alike.
func TestSDLCTransportParity_RoundTrip(t *testing.T) {
	svc, forgeURL := sdlcParityService(t)

	var want *sdlcRoundTrip
	var wantFrom string
	for name, tr := range sdlcParityTransports(t, svc) {
		trip := runSDLCRoundTrip(t, sdlcclient.NewClient(tr), forgeURL)

		assert.Equal(t, "qwen-reviewer", trip.Read.Reviewers[0].Login, "%s Binding reviewers", name)
		assert.Contains(t, trip.Forges.Forges, "gitea", "%s ListForges", name)
		assert.Equal(t, sdlcParityProject, trip.Delivery.ProjectID, "%s Delivery", name)
		assert.Equal(t, "default", trip.Default.Profile, "%s Reviewer", name)
		assert.False(t, trip.Default.Overridden, "%s Reviewer", name)
		assert.Equal(t, "qwen", trip.Overridden.Profile, "%s SetReviewer", name)
		assert.True(t, trip.Overridden.Overridden, "%s SetReviewer", name)
		assert.False(t, trip.Overridden.CountsTowardMerge, "%s SetReviewer", name)
		assert.False(t, trip.Cleared.Overridden, "%s ClearReviewer", name)
		assert.Equal(t, sdlcParityProject, trip.Conformance.ProjectID, "%s Conformance", name)

		if want == nil {
			want, wantFrom = trip, name
			continue
		}
		assert.Equal(t, want, trip, "%s answers the round trip differently from %s", name, wantFrom)
	}
}

// TestSDLCTransportParity_UnknownProject asks every transport about a project
// that does not exist and requires the same typed not-found from each.
func TestSDLCTransportParity_UnknownProject(t *testing.T) {
	svc, _ := sdlcParityService(t)
	ctx := context.Background()
	const missing = "no-such-project"

	for name, tr := range sdlcParityTransports(t, svc) {
		t.Run(name, func(t *testing.T) {
			client := sdlcclient.NewClient(tr)

			_, err := client.Binding(ctx, parityProjectTenant, missing)
			assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err), "Binding: %v", err)
			_, err = client.Conformance(ctx, parityProjectTenant, missing)
			assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err), "Conformance: %v", err)
			_, err = client.Reviewer(ctx, parityProjectTenant, missing, sdlcParityPullRequest)
			assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err), "Reviewer: %v", err)
			_, err = client.Delivery(ctx, parityProjectTenant, "no-such-task")
			assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(err), "Delivery: %v", err)
		})
	}
}
