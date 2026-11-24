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

package server

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// TestAttachAccountResolver_ReachesCodexAndACP pins that the control plane's
// pooled-account wiring reaches the codex exec backend as well as the ACP
// ones, and skips a backend that draws no account.
func TestAttachAccountResolver_ReachesCodexAndACP(t *testing.T) {
	codex := orchestration.NewCodexExecBackend(orchestration.CodexExecBackendConfig{Command: "codex"})
	backends := map[string]orchestration.AgentBackend{
		"claude":   orchestration.NewACPBackend(orchestration.ACPBackendConfig{Command: "claude"}),
		"opencode": orchestration.NewACPBackend(orchestration.ACPBackendConfig{Command: "opencode"}),
		"codex":    codex,
		"ollama":   orchestration.NewOllamaBackend(orchestration.OllamaBackendConfig{}),
	}

	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	store := orchestration.NewInMemoryAccountStore()
	tokens := orchestration.NewAccountTokenManager(orchestration.NewAccountSecretStore(mgr), store, 60*time.Second)
	resolver := orchestration.NewAccountResolver(store, tokens, orchestration.NewAccountStrategyRegistry(orchestration.NewCostTracker(nil, nil)))

	attached := attachAccountResolver(backends, resolver, nil)
	if want := []string{"claude", "codex", "opencode"}; !reflect.DeepEqual(attached, want) {
		t.Fatalf("attachAccountResolver() = %v, want %v", attached, want)
	}

	ctx, cancel := context.WithTimeout(orchestration.ContextWithTenant(context.Background(), "system"), 5*time.Second)
	defer cancel()
	_, spawnErr := codex.Spawn(ctx, &orchestration.SpawnRequest{WorkDir: t.TempDir(), AccountAuto: true})
	var notConfigured *orchestration.AccountResolverNotConfiguredError
	if errors.As(spawnErr, &notConfigured) {
		t.Fatalf("codex Spawn still fails closed for want of a resolver: %v", spawnErr)
	}
	if !errors.Is(spawnErr, orchestration.ErrNoAccountAvailable) {
		t.Fatalf("codex Spawn error = %v (%T), want ErrNoAccountAvailable over an empty pool", spawnErr, spawnErr)
	}
}
