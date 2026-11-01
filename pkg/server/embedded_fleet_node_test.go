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

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// EmbeddedDialerRegistry tests
// =============================================================================

func TestEmbeddedDialerRegistry_RegisterLookupUnregister(t *testing.T) {
	t.Parallel()

	reg := NewEmbeddedDialerRegistry()

	// Register a nil dialer to verify the registry stores and retrieves any value.
	reg.Register("test-host", nil)

	d, ok := reg.Lookup("test-host")
	assert.True(t, ok, "registered host must be found")
	assert.Nil(t, d, "stored nil dialer should return nil")

	_, ok = reg.Lookup("unknown-host")
	assert.False(t, ok, "unknown host must return not-found")

	reg.Unregister("test-host")
	_, ok = reg.Lookup("test-host")
	assert.False(t, ok, "must be absent after unregister")

	// Unregistering an absent host is a no-op.
	reg.Unregister("does-not-exist")
}

// =============================================================================
// Typed error tests
// =============================================================================

func TestEmbeddedFleetNodeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantText []string
		sentinel error
	}{
		{
			name:     "already started",
			err:      &EmbeddedFleetNodeAlreadyStartedError{AgentID: "embedded-host-abc123"},
			wantText: []string{"already started", "embedded-host-abc123"},
			sentinel: ErrEmbeddedFleetNodeAlreadyStarted,
		},
		{
			name:     "not enabled",
			err:      &EmbeddedFleetNodeNotEnabledError{},
			wantText: []string{"not enabled"},
			sentinel: ErrEmbeddedFleetNodeNotEnabled,
		},
		{
			name:     "build backends",
			err:      &EmbeddedFleetNodeBuildBackendsError{Kinds: []string{"unknown", "fake"}},
			wantText: []string{"no backends constructed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			for _, text := range tt.wantText {
				assert.Contains(t, tt.err.Error(), text)
			}
			if tt.sentinel != nil {
				assert.True(t, errors.Is(tt.err, tt.sentinel), "Unwrap must return the sentinel")
			}
		})
	}
}

// =============================================================================
// startEmbeddedFleetNode — error path tests
// =============================================================================

func TestStartEmbeddedAgent_ErrorWhenAlreadyStarted(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	// Inject a fake pre-existing state to simulate already-started.
	srv.embeddedFleetNodeState = &embeddedFleetNode{nodeID: "embedded-already"}
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()

	err := srv.startEmbeddedFleetNode()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrEmbeddedFleetNodeAlreadyStarted),
		"expected ErrEmbeddedFleetNodeAlreadyStarted, got %v", err)
}

func TestStartEmbeddedAgent_ErrorWhenNoRegistry(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = nil // No registry.

	err := srv.startEmbeddedFleetNode()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmbeddedFleetNodeNoRegistry,
		"expected ErrEmbeddedFleetNodeNoRegistry, got %v", err)
}

func TestStartEmbeddedAgent_ErrorOnUnknownBackend(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"unknown-backend-xyz"}
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.Error(t, err, "expected error for unknown backend")
}

// =============================================================================
// startEmbeddedFleetNode + stopEmbeddedFleetNode — happy path (no TLS, no CA)
// =============================================================================

// TestStartAndStopEmbeddedAgent_OllamaBackend starts the embedded agent,
// checks its fleet registration reflects the configuration (capabilities,
// GPU, host affinity, metadata), then stops it and checks that both a second
// stop of the stopped agent and a server-level stop with no agent running
// are no-ops.
func TestStartAndStopEmbeddedAgent_OllamaBackend(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.OllamaURL = "http://127.0.0.1:11434"
	srv.config.Server.EmbeddedFleetNode.GPUAvailable = true
	srv.config.Server.EmbeddedFleetNode.HostAffinity = "gpu-node-42"
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.NoError(t, err, "embedded agent must start without error")
	ea := srv.embeddedFleetNodeState
	require.NotNil(t, ea, "embeddedFleetNodeState must be set after start")

	nodeID := ea.nodeID
	assert.Contains(t, nodeID, "embedded-", "agent ID must have embedded prefix")

	// Verify the agent is registered in the fleet registry.
	ctx := context.Background()
	reg, err := srv.fleetRegistry.GetFleetNode(ctx, embeddedFleetNodeTenantID, nodeID)
	require.NoError(t, err, "agent must appear in fleet registry")
	assert.Equal(t, orchestration.FleetNodeKindEmbedded, reg.Kind)
	assert.Equal(t, orchestration.FleetNodeStatusIdle, reg.Status)
	assert.Contains(t, reg.Capabilities, "ollama", "capabilities must include ollama")
	assert.Contains(t, reg.Capabilities, "gpu", "capabilities must include gpu when GPUAvailable=true")
	assert.Equal(t, "gpu-node-42", reg.Host, "registration host must match configured host affinity")
	assert.Equal(t, "true", reg.Metadata["embedded"], "metadata must mark agent as embedded")

	// Graceful stop.
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	srv.stopEmbeddedFleetNode(stopCtx)
	assert.Nil(t, srv.embeddedFleetNodeState, "embeddedFleetNodeState must be nil after stop")

	// Agent must be deregistered.
	_, getErr := srv.fleetRegistry.GetFleetNode(ctx, embeddedFleetNodeTenantID, nodeID)
	assert.Error(t, getErr, "agent must be removed from registry on stop")

	// A second stop of the stopped agent takes the already-stopped fast path.
	require.NoError(t, ea.stop(stopCtx), "second stop must be an idempotent no-op")

	// A server-level stop with no agent running is a no-op.
	srv.stopEmbeddedFleetNode(stopCtx)
	assert.Nil(t, srv.embeddedFleetNodeState)
}

// =============================================================================
// buildEmbeddedFleetNodeBackends tests
// =============================================================================

// TestBuildEmbeddedAgentBackends covers the kinds the embedded agent builds:
// an empty list defaults to ollama, unknown kinds are skipped, a list of only
// unknown kinds is an error, and the claude backend names DefaultClaudeModel
// instead of leaving the choice to the CLI, whose own default is below the
// Opus floor (go-quicbaas#1012).
func TestBuildEmbeddedAgentBackends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kinds   []string
		url     string
		want    []string
		wantErr bool
	}{
		{name: "defaults to ollama", kinds: nil, url: "http://127.0.0.1:11434", want: []string{"ollama"}},
		{name: "unknown kind skipped", kinds: []string{"ollama", "unknown-kind"}, url: config.DefaultOllamaBaseURL, want: []string{"ollama"}},
		{name: "all unknown", kinds: []string{"unsupported-a", "unsupported-b"}, wantErr: true},
		{name: "ACP kinds", kinds: []string{"ollama", "claude", "codex", "opencode"}, url: config.DefaultOllamaBaseURL, want: []string{"ollama", "claude", "codex", "opencode"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			backends, err := buildEmbeddedFleetNodeBackends(tt.kinds, tt.url, orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, makeNoopLogger())
			if tt.wantErr {
				var buildErr *EmbeddedFleetNodeBuildBackendsError
				require.ErrorAs(t, err, &buildErr)
				return
			}
			require.NoError(t, err)
			require.Len(t, backends, len(tt.want))
			for _, name := range tt.want {
				require.Contains(t, backends, name)
			}
			if b, ok := backends["claude"]; ok {
				claude, isACP := b.(*orchestration.ACPBackend)
				require.True(t, isACP, "the claude backend must be an ACP backend")
				assert.Equal(t, orchestration.DefaultClaudeModel, claude.DefaultModel())
			}
		})
	}
}

// =============================================================================
// buildEmbeddedFleetNodeCapabilities tests
// =============================================================================

func TestBuildEmbeddedAgentCapabilities(t *testing.T) {
	t.Parallel()

	withGPU := buildEmbeddedFleetNodeCapabilities([]string{"ollama", "acp"}, true)
	assert.Contains(t, withGPU, "ollama")
	assert.Contains(t, withGPU, "acp")
	assert.Contains(t, withGPU, "gpu")

	noGPU := buildEmbeddedFleetNodeCapabilities([]string{"ollama"}, false)
	assert.Contains(t, noGPU, "ollama")
	assert.NotContains(t, noGPU, "gpu", "gpu must not appear when GPUAvailable=false")
}

// =============================================================================
// primaryBackend tests
// =============================================================================

func TestPrimaryBackend(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "acp", primaryBackend([]string{"acp", "ollama"}), "the first kind is primary")
	assert.Equal(t, "ollama", primaryBackend(nil))
	assert.Equal(t, "ollama", primaryBackend([]string{}))
}

// =============================================================================
// installEmbeddedBackendDialer tests
// =============================================================================

func TestInstallEmbeddedBackendDialer_WiresDialerRegistry(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.embeddedDialerRegistry = NewEmbeddedDialerRegistry()
	// embeddedBackendDialers is nil by default.
	require.Nil(t, srv.embeddedBackendDialers)

	reg := &orchestration.FleetNode{
		ID:   "embedded-test-001",
		Host: "test-embedded-host",
	}

	srv.installEmbeddedBackendDialer("embedded-test-001", reg, nil)

	assert.NotNil(t, srv.embeddedBackendDialers, "must be initialized after first call")
	_, ok := srv.embeddedDialerRegistry.Lookup("test-embedded-host")
	assert.True(t, ok, "dialer must be registered by host key")
}

// =============================================================================
// buildEmbeddedFleetNodeMetadata tests
// =============================================================================

func TestBuildEmbeddedAgentMetadata(t *testing.T) {
	t.Parallel()

	withCert := buildEmbeddedFleetNodeMetadata("myhost", []byte("fake-cert-pem"))
	assert.Equal(t, "true", withCert["embedded"])
	assert.Equal(t, "myhost", withCert["hostname"])
	assert.Equal(t, "true", withCert["cert_issued"], "cert_issued must be set when certPEM is non-empty")

	withoutCert := buildEmbeddedFleetNodeMetadata("myhost", nil)
	assert.Equal(t, "true", withoutCert["embedded"])
	_, hasCert := withoutCert["cert_issued"]
	assert.False(t, hasCert, "cert_issued must be absent when certPEM is empty")
}

// =============================================================================
// buildEmbeddedFleetNodeIdentity tests
// =============================================================================

// TestBuildEmbeddedAgentIdentity verifies that the synthetic identity carries
// every field GRPCSpawnServer.authorize expects and passes
// AgentIdentity.Validate, the check used before RPC dispatch.
func TestBuildEmbeddedAgentIdentity(t *testing.T) {
	t.Parallel()

	id := buildEmbeddedFleetNodeIdentity("embedded-host-abc123", "default")

	assert.Equal(t, "embedded-host-abc123", id.AgentID, "AgentID must match")
	assert.Equal(t, "embedded-host-abc123", id.ServiceAccountID, "ServiceAccountID must match")
	assert.Equal(t, "default", id.TenantID, "TenantID must match")
	assert.Equal(t, "agent-runtime", id.Role, "Role must be agent-runtime")
	assert.NotEmpty(t, id.ClientID, "ClientID must be non-empty")
	assert.Subset(t, id.Scopes, orchestration.FleetRuntimeScopes(), "identity must carry the runtime scopes")
	assert.NoError(t, id.Validate(), "embedded identity must pass Validate()")
}

// TestStartEmbeddedAgent_SpawnServerHasIdentity verifies that after
// startEmbeddedFleetNode the GRPCSpawnServer has a non-nil identity so RPCs
// are not rejected with "fleet node daemon has not completed enrollment".
// This pins the routing-bug fix: the embedded spawn server must be pre-seeded
// with an identity because it never goes through the remote enrollment path.
func TestStartEmbeddedAgent_SpawnServerHasIdentity(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.NoError(t, err, "embedded agent must start without error")
	defer srv.stopEmbeddedFleetNode(context.Background())

	ea := srv.embeddedFleetNodeState
	require.NotNil(t, ea)

	id := ea.spawnServer.Identity()
	require.NotNil(t, id, "spawn server must have an identity set — no enrollment required for embedded agents")
	assert.Equal(t, ea.nodeID, id.AgentID, "identity agent ID must match embedded agent ID")
	assert.Equal(t, embeddedFleetNodeTenantID, id.TenantID, "identity tenant must be the embedded agent tenant")
	assert.Contains(t, id.Scopes, orchestration.AgentScopeRuntimeSpawn, "identity must include spawn scope")
	assert.Contains(t, id.Scopes, orchestration.AgentScopeRuntimePrompt, "identity must include prompt scope")
	assert.Contains(t, id.Scopes, orchestration.AgentScopeRuntimeInfer, "identity must include infer scope")
}

// =============================================================================
// Helpers
// =============================================================================

// makeNoopLogger returns a minimal interfaces.Logger that discards all output.
func makeNoopLogger() interfaces.Logger {
	return &noopLogger{}
}

type noopLogger struct{}

func (n *noopLogger) Debug(_ string, _ ...any)                              {}
func (n *noopLogger) Debugf(_ string, _ ...interface{})                     {}
func (n *noopLogger) Info(_ string, _ ...any)                               {}
func (n *noopLogger) Infof(_ string, _ ...interface{})                      {}
func (n *noopLogger) Warn(_ string, _ ...any)                               {}
func (n *noopLogger) Warnf(_ string, _ ...interface{})                      {}
func (n *noopLogger) Error(_ string, _ ...any)                              {}
func (n *noopLogger) Errorf(_ string, _ ...interface{})                     {}
func (n *noopLogger) Fatal(_ string)                                        {}
func (n *noopLogger) Fatalf(_ string, _ ...interface{})                     {}
func (n *noopLogger) WithField(_ string, _ interface{}) interfaces.Logger   { return n }
func (n *noopLogger) WithFields(_ map[string]interface{}) interfaces.Logger { return n }
func (n *noopLogger) WithError(_ error) interfaces.Logger                   { return n }

// TestStartEmbeddedAgent_IssuesMTLSIdentityWhenCAPresent drives the
// mTLS-identity issuance branch of startEmbeddedFleetNode: when the server has a
// CA wired, the embedded agent requests an agent certificate and records the
// cert-issued flag in its fleet metadata. This covers the cert-issuance path
// that the no-CA happy-path test skips.
func TestStartEmbeddedAgent_IssuesMTLSIdentityWhenCAPresent(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.xkmsCA = newFakeTrustedCA(t)
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.NoError(t, err, "embedded agent must start with a CA-issued mTLS identity")
	defer srv.stopEmbeddedFleetNode(context.Background())

	nodeID := srv.embeddedFleetNodeState.nodeID
	reg, err := srv.fleetRegistry.GetFleetNode(context.Background(), embeddedFleetNodeTenantID, nodeID)
	require.NoError(t, err)
	assert.Equal(t, "true", reg.Metadata["cert_issued"],
		"the fleet metadata must record that an mTLS cert was issued from the CA")
}

// buildEmbeddedFleetNodeBackends constructs the ACP/exec backends for the
// claude, codex, and opencode kinds in addition to ollama.
// removeErrRegistry embeds a FleetRegistry but makes RemoveFleetNode fail, so the
// deregister-error warning branch of stopEmbeddedFleetNode runs.
type removeErrRegistry struct{ orchestration.FleetRegistry }

func (removeErrRegistry) RemoveFleetNode(context.Context, string, string) error {
	return errors.New("registry deregister refused")
}

// TestStopEmbeddedAgent_DeregisterErrorTolerated covers the RemoveFleetNode-error
// warning branch of stopEmbeddedFleetNode: a failing fleet deregistration is logged
// but does not prevent the agent from stopping and the state from clearing.
func TestStopEmbeddedAgent_DeregisterErrorTolerated(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.OllamaURL = "http://127.0.0.1:11434"
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	require.NoError(t, srv.startEmbeddedFleetNode())
	require.NotNil(t, srv.embeddedFleetNodeState)

	// Swap in a registry whose RemoveFleetNode fails so the deregister-warning
	// branch of stopEmbeddedFleetNode runs.
	srv.fleetRegistry = removeErrRegistry{srv.fleetRegistry}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.stopEmbeddedFleetNode(ctx)

	require.Nil(t, srv.embeddedFleetNodeState,
		"embedded agent state must clear even when fleet deregistration fails")
}

// TestStartEmbeddedAgent_RefusesAnInvalidSandboxSecurityPolicy: a policy the
// spawn could not apply fails startup, rather than every sandboxed session.
func TestStartEmbeddedAgent_RefusesAnInvalidSandboxSecurityPolicy(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*config.EmbeddedFleetNodeConfig){
		"NegativePidsLimit": func(c *config.EmbeddedFleetNodeConfig) { c.SandboxPidsLimit = -1 },
		"AddBackAll":        func(c *config.EmbeddedFleetNodeConfig) { c.SandboxCapAdd = []string{"ALL"} },
		"MalformedUlimit":   func(c *config.EmbeddedFleetNodeConfig) { c.SandboxUlimits = []string{"memlock"} },
		"SoftAboveHard":     func(c *config.EmbeddedFleetNodeConfig) { c.SandboxUlimits = []string{"memlock=2:1"} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv := withTempServer(t)
			srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
			srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
			srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()
			mutate(&srv.config.Server.EmbeddedFleetNode)

			err := srv.startEmbeddedFleetNode()
			require.Error(t, err)
			assert.ErrorIs(t, err, orchestration.ErrInvalidInput)
			assert.Contains(t, err.Error(), "sandbox security policy")
			assert.Nil(t, srv.embeddedFleetNodeState, "a refused start must leave no agent state")
		})
	}
}

// TestStartEmbeddedAgent_AcceptsOperatorSandboxUlimits: a valid sandbox_ulimits
// entry, in the runtime's own name=soft[:hard] syntax, starts the agent.
func TestStartEmbeddedAgent_AcceptsOperatorSandboxUlimits(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()
	srv.config.Server.EmbeddedFleetNode.SandboxUlimits = []string{"memlock=1073741824", "nofile=1024:4096"}

	require.NoError(t, srv.startEmbeddedFleetNode())
	require.NotNil(t, srv.embeddedFleetNodeState)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.stopEmbeddedFleetNode(ctx)
	require.Nil(t, srv.embeddedFleetNodeState)
}
