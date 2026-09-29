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

package fleet

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Runtime <-> account resolver wiring (go-quicbaas#734).
//
// Two things had to be true before a remote pooled spawn could work, and each
// is a one-line change that is easy to lose in a refactor:
//
//  1. every ACP backend the daemon serves must HOLD the resolver, and
//  2. the spawn context must CARRY the tenant, because the resolver reads
//     tenancy from the context and from nowhere else.
// =============================================================================

// recordingAccountResolver captures the resolve requests a spawn produced.
type recordingAccountResolver struct {
	mu       sync.Mutex
	requests []orchestration.AccountResolveRequest
}

func (r *recordingAccountResolver) Resolve(
	_ context.Context, req orchestration.AccountResolveRequest,
) (*orchestration.ResolvedAccountCredential, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return &orchestration.ResolvedAccountCredential{
		AccountID: req.AccountID,
		Strategy:  "pinned",
		Env:       map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": "resolved"},
	}, nil
}

func (r *recordingAccountResolver) snapshot() []orchestration.AccountResolveRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]orchestration.AccountResolveRequest(nil), r.requests...)
}

// newACPRuntime builds a Runtime whose single backend is a real ACPBackend, so
// the resolver wiring under test is the production path rather than a double.
func newACPRuntime(t *testing.T, cfg RuntimeConfig) (*Runtime, *orchestration.ACPBackend) {
	t.Helper()
	// The command is an ABSOLUTE PATH INTO A TEMP DIRECTORY whose basename is
	// "claude". The basename is what ACPBackend derives the account provider
	// from, and the absolute path is what guarantees these tests never launch
	// the operator's real claude CLI if one happens to be on PATH — a unit test
	// must not run an agent on the host. The file does not exist, so the spawn
	// fails at exec; account resolution has already happened by then, which is
	// what every assertion below is about.
	backend := orchestration.NewACPBackend(orchestration.ACPBackendConfig{
		Command: filepath.Join(t.TempDir(), "claude"),
	})
	cfg.Backends = map[string]orchestration.AgentBackend{"claude": backend}
	cfg.Logger = &testLogger{t: t}
	rt, err := NewRuntime(cfg)
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return rt, backend
}

// TestRuntime_AttachesTheConfiguredAccountResolver proves a resolver supplied
// at construction reaches the ACP backends. Without it every pooled spawn is
// refused AccountResolverNotConfiguredError, which is the defect #734 recorded.
func TestRuntime_AttachesTheConfiguredAccountResolver(t *testing.T) {
	resolver := &recordingAccountResolver{}
	rt, _ := newACPRuntime(t, RuntimeConfig{AccountResolver: resolver})

	// The spawn will fail later (there is no claude binary here); what matters
	// is that resolution was ATTEMPTED, which only happens when the backend
	// holds a resolver.
	_, _ = rt.Spawn(context.Background(), "tenant-a", nil, "claude",
		&orchestration.SpawnRequest{AccountID: "acct-1", WorkDir: t.TempDir()})

	if len(resolver.snapshot()) == 0 {
		t.Fatal("the configured resolver was never consulted; the backend did not receive it")
	}
}

// TestRuntime_SetAccountResolverAttachesLate covers the daemon's actual
// bring-up order, where the resolver is built after the runtime.
func TestRuntime_SetAccountResolverAttachesLate(t *testing.T) {
	rt, _ := newACPRuntime(t, RuntimeConfig{})

	// Before wiring, a pooled spawn is refused for want of a resolver.
	_, err := rt.Spawn(context.Background(), "tenant-a", nil, "claude",
		&orchestration.SpawnRequest{AccountID: "acct-1", WorkDir: t.TempDir()})
	var notConfigured *orchestration.AccountResolverNotConfiguredError
	if !errors.As(err, &notConfigured) {
		t.Fatalf("unwired spawn error = %v (%T), want *AccountResolverNotConfiguredError", err, err)
	}

	resolver := &recordingAccountResolver{}
	rt.SetAccountResolver(resolver)

	_, _ = rt.Spawn(context.Background(), "tenant-a", nil, "claude",
		&orchestration.SpawnRequest{AccountID: "acct-1", WorkDir: t.TempDir()})
	if len(resolver.snapshot()) == 0 {
		t.Fatal("SetAccountResolver did not reach the ACP backend")
	}
}

// TestRuntime_SetAccountResolverIgnoresNil proves the setter cannot be used to
// silently disarm a wired daemon: "no resolver" is fail-closed for every
// pooled spawn and must not be reachable by accident.
func TestRuntime_SetAccountResolverIgnoresNil(t *testing.T) {
	resolver := &recordingAccountResolver{}
	rt, _ := newACPRuntime(t, RuntimeConfig{AccountResolver: resolver})

	rt.SetAccountResolver(nil)

	_, _ = rt.Spawn(context.Background(), "tenant-a", nil, "claude",
		&orchestration.SpawnRequest{AccountID: "acct-1", WorkDir: t.TempDir()})
	if len(resolver.snapshot()) == 0 {
		t.Fatal("SetAccountResolver(nil) cleared the wired resolver")
	}
}

// TestRuntime_SpawnCarriesTheTenantForAccountResolution is the other half of
// the wiring: the resolver reads tenancy from the CONTEXT, and the daemon knew
// the tenant only as an argument. Without the binding every remote pooled spawn
// would ask the pool for tenant "" and be refused.
func TestRuntime_SpawnCarriesTheTenantForAccountResolution(t *testing.T) {
	resolver := &recordingAccountResolver{}
	rt, _ := newACPRuntime(t, RuntimeConfig{AccountResolver: resolver})

	_, _ = rt.Spawn(context.Background(), "tenant-a", nil, "claude",
		&orchestration.SpawnRequest{
			AccountID: "acct-1",
			ProjectID: "project-x",
			WorkDir:   t.TempDir(),
		})

	requests := resolver.snapshot()
	if len(requests) == 0 {
		t.Fatal("the resolver was never consulted")
	}
	req := requests[0]
	if req.TenantID != "tenant-a" {
		t.Errorf("resolve request carried tenant %q, want tenant-a", req.TenantID)
	}
	if req.ProjectID != "project-x" {
		t.Errorf("resolve request carried project %q, want project-x", req.ProjectID)
	}
	if req.Provider != orchestration.AccountProviderClaude {
		t.Errorf("resolve request carried provider %q, want claude", req.Provider)
	}
	if req.AccountID != "acct-1" {
		t.Errorf("resolve request carried account %q, want acct-1", req.AccountID)
	}
}

// TestRuntime_SpawnWithoutAnAccountSkipsResolution proves the feature stays
// opt-in: a spawn naming no account never touches the pool, so a daemon with a
// misconfigured pool still serves every ordinary spawn.
func TestRuntime_SpawnWithoutAnAccountSkipsResolution(t *testing.T) {
	resolver := &recordingAccountResolver{}
	rt, _ := newACPRuntime(t, RuntimeConfig{AccountResolver: resolver})

	_, _ = rt.Spawn(context.Background(), "tenant-a", nil, "claude",
		&orchestration.SpawnRequest{WorkDir: t.TempDir()})

	if len(resolver.snapshot()) != 0 {
		t.Fatal("a spawn naming no account consulted the pool")
	}
}
