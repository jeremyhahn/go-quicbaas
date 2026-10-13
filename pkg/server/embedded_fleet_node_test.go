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

func TestEmbeddedDialerRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()

	reg := NewEmbeddedDialerRegistry()

	// Register a nil dialer to verify the registry stores and retrieves any value.
	reg.Register("test-host", nil)

	d, ok := reg.Lookup("test-host")
	assert.True(t, ok, "registered host must be found")
	assert.Nil(t, d, "stored nil dialer should return nil")

	_, ok2 := reg.Lookup("unknown-host")
	assert.False(t, ok2, "unknown host must return not-found")
}

func TestEmbeddedDialerRegistry_Unregister(t *testing.T) {
	t.Parallel()

	reg := NewEmbeddedDialerRegistry()
	reg.Register("host-a", nil)

	_, ok := reg.Lookup("host-a")
	require.True(t, ok, "must be registered before unregister")

	reg.Unregister("host-a")

	_, ok = reg.Lookup("host-a")
	assert.False(t, ok, "must be absent after unregister")
}

func TestEmbeddedDialerRegistry_UnregisterNonexistent(t *testing.T) {
	t.Parallel()
	// Should be a no-op — must not panic.
	reg := NewEmbeddedDialerRegistry()
	reg.Unregister("does-not-exist")
}

// =============================================================================
// Typed error tests
// =============================================================================

func TestEmbeddedAgentAlreadyStartedError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	err := &EmbeddedFleetNodeAlreadyStartedError{AgentID: "embedded-host-abc123"}
	assert.Contains(t, err.Error(), "already started", "error message must mention already started")
	assert.Contains(t, err.Error(), "embedded-host-abc123", "error must include agent ID")
	assert.True(t, errors.Is(err, ErrEmbeddedFleetNodeAlreadyStarted), "Unwrap must return sentinel")
}

func TestEmbeddedAgentNotEnabledError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	err := &EmbeddedFleetNodeNotEnabledError{}
	assert.Contains(t, err.Error(), "not enabled", "error message must mention not enabled")
	assert.True(t, errors.Is(err, ErrEmbeddedFleetNodeNotEnabled), "Unwrap must return sentinel")
}

func TestEmbeddedAgentBuildBackendsError_Error(t *testing.T) {
	t.Parallel()

	err := &EmbeddedFleetNodeBuildBackendsError{Kinds: []string{"unknown", "fake"}}
	assert.Contains(t, err.Error(), "no backends constructed", "error must describe failure")
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

func TestStartAndStopEmbeddedAgent_OllamaBackend(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.OllamaURL = "http://127.0.0.1:11434"
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.NoError(t, err, "embedded agent must start without error")
	require.NotNil(t, srv.embeddedFleetNodeState, "embeddedFleetNodeState must be set after start")

	nodeID := srv.embeddedFleetNodeState.nodeID
	assert.True(t, len(nodeID) > 0, "agent ID must be non-empty")
	assert.Contains(t, nodeID, "embedded-", "agent ID must have embedded prefix")

	// Verify the agent is registered in the fleet registry.
	ctx := context.Background()
	reg, err := srv.fleetRegistry.GetFleetNode(ctx, embeddedFleetNodeTenantID, nodeID)
	require.NoError(t, err, "agent must appear in fleet registry")
	assert.Equal(t, orchestration.FleetNodeKindEmbedded, reg.Kind)
	assert.Equal(t, orchestration.FleetNodeStatusIdle, reg.Status)
	assert.Contains(t, reg.Capabilities, "ollama", "capabilities must include ollama")
	assert.Equal(t, "true", reg.Metadata["embedded"], "metadata must mark agent as embedded")

	// Graceful stop.
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	srv.stopEmbeddedFleetNode(stopCtx)
	assert.Nil(t, srv.embeddedFleetNodeState, "embeddedFleetNodeState must be nil after stop")

	// Agent must be deregistered.
	_, getErr := srv.fleetRegistry.GetFleetNode(ctx, embeddedFleetNodeTenantID, nodeID)
	assert.Error(t, getErr, "agent must be removed from registry on stop")
}

func TestStartEmbeddedAgent_GPUCapability(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.GPUAvailable = true
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.NoError(t, err)
	defer srv.stopEmbeddedFleetNode(context.Background())

	nodeID := srv.embeddedFleetNodeState.nodeID
	reg, err := srv.fleetRegistry.GetFleetNode(context.Background(), embeddedFleetNodeTenantID, nodeID)
	require.NoError(t, err)
	assert.Contains(t, reg.Capabilities, "gpu", "capabilities must include gpu when GPUAvailable=true")
}

func TestStartEmbeddedAgent_HostAffinityTag(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.HostAffinity = "gpu-node-42"
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	err := srv.startEmbeddedFleetNode()
	require.NoError(t, err)
	defer srv.stopEmbeddedFleetNode(context.Background())

	nodeID := srv.embeddedFleetNodeState.nodeID
	reg, err := srv.fleetRegistry.GetFleetNode(context.Background(), embeddedFleetNodeTenantID, nodeID)
	require.NoError(t, err)
	assert.Equal(t, "gpu-node-42", reg.Host, "registration host must match configured host affinity")
}

func TestStopEmbeddedAgent_IdempotentWhenNotStarted(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()

	// Must not panic when called without a running embedded agent.
	srv.stopEmbeddedFleetNode(context.Background())
	srv.stopEmbeddedFleetNode(context.Background()) // second call must also be safe
}

// =============================================================================
// buildEmbeddedFleetNodeBackends tests
// =============================================================================

func TestBuildEmbeddedAgentBackends_DefaultsToOllama(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends(nil, "http://127.0.0.1:11434", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)
	_, ok := backends["ollama"]
	assert.True(t, ok, "default backend must be ollama")
}

func TestBuildEmbeddedAgentBackends_UnknownKindSkipped(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	// Mixing a valid backend with an unknown one: should succeed with just the valid backend.
	backends, err := buildEmbeddedFleetNodeBackends([]string{"ollama", "unknown-kind"}, config.DefaultOllamaBaseURL, orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)
	_, ok := backends["ollama"]
	assert.True(t, ok)
}

func TestBuildEmbeddedAgentBackends_AllUnknownReturnsError(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	_, err := buildEmbeddedFleetNodeBackends([]string{"unsupported-a", "unsupported-b"}, "", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.Error(t, err, "all-unknown backends must return error")
}

// =============================================================================
// buildEmbeddedFleetNodeCapabilities tests
// =============================================================================

func TestBuildEmbeddedAgentCapabilities_IncludesBackendsAndGPU(t *testing.T) {
	t.Parallel()

	caps := buildEmbeddedFleetNodeCapabilities([]string{"ollama", "acp"}, true)
	assert.Contains(t, caps, "ollama")
	assert.Contains(t, caps, "acp")
	assert.Contains(t, caps, "gpu")
}

func TestBuildEmbeddedAgentCapabilities_NoGPU(t *testing.T) {
	t.Parallel()

	caps := buildEmbeddedFleetNodeCapabilities([]string{"ollama"}, false)
	assert.Contains(t, caps, "ollama")
	assert.NotContains(t, caps, "gpu", "gpu must not appear when GPUAvailable=false")
}

// =============================================================================
// primaryBackend tests
// =============================================================================

func TestPrimaryBackend_ReturnsFirstKind(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ollama", primaryBackend([]string{"ollama", "acp"}))
}

func TestPrimaryBackend_DefaultsToOllama(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "ollama", primaryBackend(nil))
	assert.Equal(t, "ollama", primaryBackend([]string{}))
}

// =============================================================================
// Config field tests
// =============================================================================

func TestEmbeddedAgentConfig_DefaultsOllamaURL(t *testing.T) {
	t.Parallel()

	cfg := config.EmbeddedFleetNodeConfig{}
	assert.Equal(t, "", cfg.OllamaURL, "zero value must be empty (CLI default applied at flag parse)")
	assert.False(t, cfg.Enabled)
	assert.False(t, cfg.GPUAvailable)
}

// =============================================================================
// installEmbeddedBackendDialer tests
// =============================================================================

func TestInstallEmbeddedBackendDialer_WiresDialerRegistry(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.embeddedDialerRegistry = NewEmbeddedDialerRegistry()

	reg := &orchestration.FleetNode{
		ID:   "embedded-test-001",
		Host: "test-embedded-host",
	}

	srv.installEmbeddedBackendDialer("embedded-test-001", reg, nil)

	// Verify the dialer registry got the entry.
	_, ok := srv.embeddedDialerRegistry.Lookup("test-embedded-host")
	assert.True(t, ok, "dialer must be registered by host key")
}

func TestInstallEmbeddedBackendDialer_InitializesMap(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	// embeddedBackendDialers is nil by default.
	assert.Nil(t, srv.embeddedBackendDialers)

	reg := &orchestration.FleetNode{
		ID:   "embedded-init-test",
		Host: "host-x",
	}
	srv.installEmbeddedBackendDialer("embedded-init-test", reg, nil)

	assert.NotNil(t, srv.embeddedBackendDialers, "must be initialized after first call")
}

// =============================================================================
// buildEmbeddedFleetNodeMetadata tests
// =============================================================================

func TestBuildEmbeddedAgentMetadata_WithCert(t *testing.T) {
	t.Parallel()

	m := buildEmbeddedFleetNodeMetadata("myhost", []byte("fake-cert-pem"))
	assert.Equal(t, "true", m["embedded"])
	assert.Equal(t, "myhost", m["hostname"])
	assert.Equal(t, "true", m["cert_issued"], "cert_issued must be set when certPEM is non-empty")
}

func TestBuildEmbeddedAgentMetadata_WithoutCert(t *testing.T) {
	t.Parallel()

	m := buildEmbeddedFleetNodeMetadata("myhost", nil)
	assert.Equal(t, "true", m["embedded"])
	_, hasCert := m["cert_issued"]
	assert.False(t, hasCert, "cert_issued must be absent when certPEM is empty")
}

// =============================================================================
// buildEmbeddedFleetNodeIdentity tests
// =============================================================================

// TestBuildEmbeddedAgentIdentity_ContainsRequiredFields verifies that the
// synthetic identity satisfies all fields expected by GRPCSpawnServer.authorize.
func TestBuildEmbeddedAgentIdentity_ContainsRequiredFields(t *testing.T) {
	t.Parallel()

	id := buildEmbeddedFleetNodeIdentity("embedded-host-abc123", "default")

	assert.Equal(t, "embedded-host-abc123", id.AgentID, "AgentID must match")
	assert.Equal(t, "embedded-host-abc123", id.ServiceAccountID, "ServiceAccountID must match")
	assert.Equal(t, "default", id.TenantID, "TenantID must match")
	assert.Equal(t, "agent-runtime", id.Role, "Role must be agent-runtime")
	assert.NotEmpty(t, id.Scopes, "Scopes must be non-empty")
	assert.NotEmpty(t, id.ClientID, "ClientID must be non-empty")

	// Verify all runtime scopes are present.
	expectedScopes := orchestration.FleetRuntimeScopes()
	for _, scope := range expectedScopes {
		assert.Contains(t, id.Scopes, scope, "identity must include scope %q", scope)
	}
}

// TestBuildEmbeddedAgentIdentity_PassesValidate verifies the synthetic identity
// satisfies AgentIdentity.Validate — the same check used before RPC dispatch.
func TestBuildEmbeddedAgentIdentity_PassesValidate(t *testing.T) {
	t.Parallel()

	id := buildEmbeddedFleetNodeIdentity("embedded-mynode-1234abcd", "tenant-1")
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
func TestBuildEmbeddedAgentBackends_ACPKinds(t *testing.T) {
	t.Parallel()

	log := makeNoopLogger()
	backends, err := buildEmbeddedFleetNodeBackends(
		[]string{"ollama", "claude", "codex", "opencode"}, config.DefaultOllamaBaseURL, orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, log)
	require.NoError(t, err)

	for _, name := range []string{"ollama", "claude", "codex", "opencode"} {
		if _, ok := backends[name]; !ok {
			t.Fatalf("expected backend %q to be constructed", name)
		}
	}
}

// TestBuildEmbeddedAgentBackends_ClaudeRunsTheCodingTierByDefault asserts the
// embedded agent's claude backend names DefaultClaudeModel instead of leaving
// the choice to the CLI, whose own default is below the Opus floor
// (go-quicbaas#1012).
func TestBuildEmbeddedAgentBackends_ClaudeRunsTheCodingTierByDefault(t *testing.T) {
	t.Parallel()

	backends, err := buildEmbeddedFleetNodeBackends(
		[]string{"claude"}, "", orchestration.SandboxNetworkDefault, orchestration.SandboxSecurity{}, nil, makeNoopLogger())
	require.NoError(t, err)

	claude, ok := backends["claude"].(*orchestration.ACPBackend)
	require.True(t, ok, "the claude backend must be an ACP backend")
	assert.Equal(t, orchestration.DefaultClaudeModel, claude.DefaultModel())
}

// TestEmbeddedAgent_StopIsIdempotent covers the already-stopped guard of
// embeddedFleetNode.stop: a second stop on the same agent returns nil immediately
// without re-invoking the spawn server's Stop.
func TestEmbeddedAgent_StopIsIdempotent(t *testing.T) {
	t.Parallel()

	srv := withTempServer(t)
	srv.fleetRegistry = orchestration.NewInMemoryFleetRegistry()
	srv.config.Server.EmbeddedFleetNode.Enabled = true
	srv.config.Server.EmbeddedFleetNode.Backends = []string{"ollama"}
	srv.config.Server.EmbeddedFleetNode.OllamaURL = "http://127.0.0.1:11434"
	srv.config.Server.EmbeddedFleetNode.DataDir = t.TempDir()

	require.NoError(t, srv.startEmbeddedFleetNode())
	ea := srv.embeddedFleetNodeState
	require.NotNil(t, ea)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, ea.stop(ctx), "first stop must succeed")
	// Second stop must take the already-stopped fast path and return nil.
	require.NoError(t, ea.stop(ctx), "second stop must be an idempotent no-op")

	// Clean up server-level state so Shutdown does not double-stop.
	srv.embeddedFleetNodeState = nil
}

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
