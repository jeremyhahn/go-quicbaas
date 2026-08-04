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
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
)

// fakeMCPPresetResolver is a test double for MCPPresetResolver. It records the
// arguments it was called with and replays a scripted result, letting the
// orchestration tests assert the Spawn wiring without a real cert issuer.
type fakeMCPPresetResolver struct {
	url     string
	env     map[string]string
	err     error
	calls   atomic.Int32
	cleaned atomic.Int32

	gotSessionID string
	gotTenantID  string
}

func (f *fakeMCPPresetResolver) Resolve(_ context.Context, sessionID, tenantID string) (string, map[string]string, func(), error) {
	f.calls.Add(1)
	f.gotSessionID = sessionID
	f.gotTenantID = tenantID
	if f.err != nil {
		return "", nil, nil, f.err
	}
	cleanup := func() { f.cleaned.Add(1) }
	return f.url, f.env, cleanup, nil
}

// newOneShotCLIBackend returns an ACP backend in one-shot CLI mode so Spawn
// stores a session without launching any subprocess — the spawn path is fully
// in-memory, exercising the MCP-resolution wiring deterministically.
func newOneShotCLIBackend(resolver MCPPresetResolver) *ACPBackend {
	return NewACPBackend(ACPBackendConfig{
		Command:           "claude",
		CLIMode:           true,
		MCPPresetResolver: resolver,
	})
}

// TestSpawn_ResolvesQuicBaaSPreset verifies that a "quicbaas" preset with an
// empty URL is resolved at spawn: the entry gets the resolver's URL and env,
// the QUICBAAS_MCP_SERVERS payload reflects the resolved values, the resolver
// is called with the tenant from context, and the cleanup runs exactly once on
// Close.
func TestSpawn_ResolvesQuicBaaSPreset(t *testing.T) {
	resolver := &fakeMCPPresetResolver{
		url: "https://127.0.0.1:11443/mcp",
		env: map[string]string{
			"QUICBAAS_MCP_CERT": "/ephemeral/cert.pem",
			"QUICBAAS_MCP_KEY":  "/ephemeral/key.pem",
			"QUICBAAS_MCP_CA":   "/ephemeral/ca.pem",
		},
	}
	b := newOneShotCLIBackend(resolver)

	req := &SpawnRequest{
		WorkDir: "/tmp",
		MCPServers: []MCPServerConfig{{
			Name: QuicBaaSMCPPresetName,
			URL:  "", // empty preset — must be resolved
		}},
	}

	ctx := ContextWithTenant(context.Background(), "tenant-abc")
	sess, err := b.Spawn(ctx, req)
	if err != nil {
		t.Fatalf("Spawn: unexpected error: %v", err)
	}

	if got := resolver.calls.Load(); got != 1 {
		t.Fatalf("resolver call count = %d, want 1", got)
	}
	if resolver.gotTenantID != "tenant-abc" {
		t.Errorf("resolver tenant = %q, want %q", resolver.gotTenantID, "tenant-abc")
	}
	if resolver.gotSessionID == "" {
		t.Error("resolver session id is empty; want a generated identifier")
	}

	// The preset entry must carry the resolved URL + env in place.
	entry := req.MCPServers[0]
	if entry.URL != resolver.url {
		t.Errorf("entry URL = %q, want %q", entry.URL, resolver.url)
	}
	for k, v := range resolver.env {
		if entry.Env[k] != v {
			t.Errorf("entry Env[%q] = %q, want %q", k, entry.Env[k], v)
		}
	}

	// QUICBAAS_MCP_SERVERS must reflect the resolved values so the spawned
	// agent reads the real endpoint + cert paths.
	encoded, ok := req.Env[MCPServersEnvVar]
	if !ok {
		t.Fatal("QUICBAAS_MCP_SERVERS not set on request env")
	}
	var decoded []MCPServerConfig
	if jsonErr := json.Unmarshal([]byte(encoded), &decoded); jsonErr != nil {
		t.Fatalf("decode QUICBAAS_MCP_SERVERS: %v", jsonErr)
	}
	if len(decoded) != 1 || decoded[0].URL != resolver.url {
		t.Fatalf("decoded servers = %+v, want one entry with URL %q", decoded, resolver.url)
	}
	if decoded[0].Env["QUICBAAS_MCP_KEY"] != "/ephemeral/key.pem" {
		t.Errorf("decoded key path = %q, want /ephemeral/key.pem", decoded[0].Env["QUICBAAS_MCP_KEY"])
	}

	// Cleanup must not have fired yet — only on Close.
	if got := resolver.cleaned.Load(); got != 0 {
		t.Fatalf("cleanup ran before Close: count = %d", got)
	}

	if closeErr := b.Close(ctx, sess.ID); closeErr != nil {
		t.Fatalf("Close: unexpected error: %v", closeErr)
	}
	if got := resolver.cleaned.Load(); got != 1 {
		t.Fatalf("cleanup ran %d times after Close, want 1", got)
	}
}

// TestSpawn_PresetResolverError leaves the preset unresolved when the resolver
// returns an error: the spawn still succeeds (the env var carries the empty-URL
// preset, which is harmless) and no cleanup is registered.
func TestSpawn_PresetResolverError(t *testing.T) {
	resolver := &fakeMCPPresetResolver{err: errors.New("ca offline")}
	b := newOneShotCLIBackend(resolver)

	req := &SpawnRequest{
		WorkDir: "/tmp",
		MCPServers: []MCPServerConfig{{
			Name: QuicBaaSMCPPresetName,
		}},
	}

	sess, err := b.Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: should not fail on resolver error, got: %v", err)
	}
	if resolver.calls.Load() != 1 {
		t.Fatalf("resolver call count = %d, want 1", resolver.calls.Load())
	}
	// URL stays empty — preset unresolved.
	if req.MCPServers[0].URL != "" {
		t.Errorf("entry URL = %q, want empty (unresolved)", req.MCPServers[0].URL)
	}
	if closeErr := b.Close(context.Background(), sess.ID); closeErr != nil {
		t.Fatalf("Close: unexpected error: %v", closeErr)
	}
	if resolver.cleaned.Load() != 0 {
		t.Errorf("cleanup ran %d times; want 0 (nothing was resolved)", resolver.cleaned.Load())
	}
}

// TestSpawn_NonPresetAndPopulatedURLUntouched confirms the resolver only fires
// for the empty-URL "quicbaas" preset: a differently-named server and a
// quicbaas server that already carries a URL are both left as-is.
func TestSpawn_NonPresetAndPopulatedURLUntouched(t *testing.T) {
	resolver := &fakeMCPPresetResolver{url: "https://should-not-be-used/mcp"}
	b := newOneShotCLIBackend(resolver)

	req := &SpawnRequest{
		WorkDir: "/tmp",
		MCPServers: []MCPServerConfig{
			{Name: "other", URL: ""},
			{Name: QuicBaaSMCPPresetName, URL: "https://operator-supplied/mcp"},
		},
	}

	sess, err := b.Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: unexpected error: %v", err)
	}
	if resolver.calls.Load() != 0 {
		t.Fatalf("resolver called %d times; want 0", resolver.calls.Load())
	}
	if req.MCPServers[1].URL != "https://operator-supplied/mcp" {
		t.Errorf("operator-supplied URL was overwritten: %q", req.MCPServers[1].URL)
	}
	_ = b.Close(context.Background(), sess.ID)
}

// TestSpawn_NoResolverConfigured leaves an empty-URL preset untouched when no
// resolver is wired (the default), so non-server callers are unaffected.
func TestSpawn_NoResolverConfigured(t *testing.T) {
	b := newOneShotCLIBackend(nil)

	req := &SpawnRequest{
		WorkDir:    "/tmp",
		MCPServers: []MCPServerConfig{{Name: QuicBaaSMCPPresetName}},
	}

	sess, err := b.Spawn(context.Background(), req)
	if err != nil {
		t.Fatalf("Spawn: unexpected error: %v", err)
	}
	if req.MCPServers[0].URL != "" {
		t.Errorf("entry URL = %q, want empty (no resolver)", req.MCPServers[0].URL)
	}
	// The env var should still carry the (empty-URL) preset — harmless.
	if _, ok := req.Env[MCPServersEnvVar]; !ok {
		t.Error("QUICBAAS_MCP_SERVERS missing; preset should still be forwarded")
	}
	_ = b.Close(context.Background(), sess.ID)
}
