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

package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// registerPayloadCapture is a stub of the server's mTLS register endpoint that
// keeps the body the daemon sent, so a test can assert on the vocabulary the
// daemon advertises rather than on a log line about it.
type registerPayloadCapture struct {
	t    *testing.T
	body map[string]any
}

func (c *registerPayloadCapture) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/register", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(c.t, err)
		require.NoError(c.t, json.Unmarshal(raw, &c.body))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(c.t, json.NewEncoder(w).Encode(map[string]any{
			"client_id":          "client-abc",
			"client_secret":      "secret-xyz",
			"agent_id":           "agent-001",
			"service_account_id": "sa-001",
			"role":               "agent-runtime",
			"scopes": []string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			},
		}))
	})
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(c.t, json.NewEncoder(w).Encode(map[string]any{
			"access_token": "jwt-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"scope": strings.Join([]string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			}, " "),
		}))
	})
	return mux
}

// advertisedBackendsOnEnroll runs one enrollment against a capturing stub and
// returns the comma-separated metadata.backends the daemon declared.
func advertisedBackendsOnEnroll(t *testing.T, kinds []BackendKind, served func() []string) string {
	t.Helper()

	capture := &registerPayloadCapture{t: t}
	srv := httptest.NewTLSServer(capture.handler())
	t.Cleanup(srv.Close)

	cfg := newEnrollerConfig(t, srv.URL)
	cfg.Backends = kinds
	require.NoError(t, os.MkdirAll(cfg.WorkDir, 0o700))

	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		HTTPClient:     srv.Client(),
		Logger:         &testLogger{t: t},
		ServedBackends: served,
	})

	_, err := enroller.Enroll(context.Background(), cfg)
	require.NoError(t, err)

	metadata, ok := capture.body["metadata"].(map[string]any)
	require.True(t, ok, "register payload carries a metadata object")
	backends, ok := metadata["backends"].(string)
	require.True(t, ok, "register metadata carries a backends string")
	return backends
}

// TestEnroll_AdvertisesServedBackendsNotTransport is the live case: a daemon
// started with --backends acp serves claude, codex and opencode, and must
// register those three names. Registering the transport name "acp" left every
// dispatcher that filters by backend name unable to see it (go-quicbaas#681).
func TestEnroll_AdvertisesServedBackendsNotTransport(t *testing.T) {
	withProbe(t, &stubDetector{}) // no GPU: the list is exactly the served set

	got := advertisedBackendsOnEnroll(t, []BackendKind{BackendACP}, func() []string {
		return []string{"claude", "codex", "opencode"}
	})

	require.Equal(t, "claude,codex,opencode", got)
	require.NotContains(t, strings.Split(got, ","), "acp",
		"the transport kind is not a backend a task can route to")
}

// TestEnroll_AdvertisesGPUBesideServedBackends pins gpu-host's shape: the gpu
// trait still rides along with the served list, because that is how the
// scheduler filters to GPU-capable daemons.
func TestEnroll_AdvertisesGPUBesideServedBackends(t *testing.T) {
	withProbe(t, &stubDetector{nvidiaSMI: true})

	got := advertisedBackendsOnEnroll(t, []BackendKind{BackendOllama}, func() []string {
		return []string{"ollama"}
	})

	require.Equal(t, "ollama,gpu", got)
}

// TestEnroll_AdvertisesConfiguredKindsWithoutARuntime covers the enroller no
// caller wired a runtime into: it has nothing more specific to say than the
// kinds the config named, and says exactly that rather than nothing.
func TestEnroll_AdvertisesConfiguredKindsWithoutARuntime(t *testing.T) {
	withProbe(t, &stubDetector{})

	t.Run("no served source", func(t *testing.T) {
		got := advertisedBackendsOnEnroll(t, []BackendKind{BackendACP, BackendOllama}, nil)
		require.Equal(t, "acp,ollama", got)
	})

	t.Run("unset config falls to the effective default", func(t *testing.T) {
		got := advertisedBackendsOnEnroll(t, nil, nil)
		require.Equal(t, "acp,ollama", got)
	})
}

// TestEnroll_AWiredRuntimeIsAuthoritativeEvenWhenItServesNothing is the
// no-silent-fallback rule at its only interesting point.
//
// A daemon whose backend construction produced nothing serves nothing, and
// substituting the configured transport kinds there would mint exactly the
// phantom "acp" record this derivation exists to remove — on the one daemon
// least able to honour a task routed to it. It advertises nothing instead, so
// the fault is visible rather than disguised as a working agent.
func TestEnroll_AWiredRuntimeIsAuthoritativeEvenWhenItServesNothing(t *testing.T) {
	withProbe(t, &stubDetector{})

	for name, served := range map[string]func() []string{
		"a nil reply":    func() []string { return nil },
		"an empty reply": func() []string { return []string{} },
	} {
		t.Run(name, func(t *testing.T) {
			got := advertisedBackendsOnEnroll(t, []BackendKind{BackendACP}, served)
			require.Empty(t, got, "a daemon that serves nothing advertises nothing")
			require.NotContains(t, got, "acp",
				"the configured kinds must not stand in for a runtime that answered")
		})
	}
}

// TestEnroll_ServingNothingStillRegisters covers what the control plane does
// with that answer: the registration is accepted — the daemon is not put into
// a crash-restart loop over it — and the record it leaves matches no backend,
// so no task is ever routed to a daemon that cannot run it.
func TestEnroll_ServingNothingStillRegisters(t *testing.T) {
	withProbe(t, &stubDetector{})

	capture := &registerPayloadCapture{t: t}
	srv := httptest.NewTLSServer(capture.handler())
	t.Cleanup(srv.Close)

	cfg := newEnrollerConfig(t, srv.URL)
	cfg.Backends = []BackendKind{BackendACP}
	require.NoError(t, os.MkdirAll(cfg.WorkDir, 0o700))

	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		HTTPClient:     srv.Client(),
		Logger:         &testLogger{t: t},
		ServedBackends: func() []string { return nil },
	})

	identity, err := enroller.Enroll(context.Background(), cfg)
	require.NoError(t, err, "enrollment must not fail over an empty backend set")
	require.Equal(t, "agent-001", identity.AgentID)
}

// TestAdvertisedBackends_DoesNotAliasTheRuntimeSlice guards the payload
// against the GPU append writing into the runtime's own backing array, which
// would corrupt the set the heartbeat and the ready log read next.
func TestAdvertisedBackends_DoesNotAliasTheRuntimeSlice(t *testing.T) {
	t.Parallel()

	served := []string{"claude", "codex", "opencode"}
	enroller := NewProductionEnroller(ProductionEnrollerDeps{
		Logger:         &testLogger{t: t},
		ServedBackends: func() []string { return served },
	})

	advertised := enroller.advertisedBackends(&Config{})
	require.Equal(t, served, advertised)

	advertised[0] = "mutated"
	require.Equal(t, []string{"claude", "codex", "opencode"}, served,
		"the payload must not write through to the runtime's set")
}

// TestRuntimeBackends_IsTheOneServedTruth pins the derivation the ready log,
// the heartbeat status and the register payload all read: the names the
// runtime actually serves, in a stable order.
//
// Stability is not cosmetic. The set is a Go map, so an unsorted read
// reorders between restarts, and a register payload an operator diffs — or a
// ready line an operator greps — would change with nothing behind it.
func TestRuntimeBackends_IsTheOneServedTruth(t *testing.T) {
	t.Parallel()

	// The backends a --backends acp daemon constructs, offered to the runtime
	// in the order buildAgentBackends inserts them.
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{
			"opencode": &mockBackend{},
			"claude":   &mockBackend{},
			"codex":    &mockBackend{},
		},
		Logger: &testLogger{t: t},
	})
	require.NoError(t, err)

	want := []string{"claude", "codex", "opencode"}
	for range 8 {
		require.Equal(t, want, rt.Backends())
	}
}

// TestRuntimeBackends_ReportsOnlyWhatConstructed pins that the served truth is
// the constructed set and nothing else: a daemon offering one backend
// advertises one, never the transport kind that would have produced three.
func TestRuntimeBackends_ReportsOnlyWhatConstructed(t *testing.T) {
	t.Parallel()

	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"ollama": &mockBackend{}},
		Logger:   &testLogger{t: t},
	})
	require.NoError(t, err)

	require.Equal(t, []string{"ollama"}, rt.Backends())
}
