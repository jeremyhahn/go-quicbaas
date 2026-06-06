// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"context"
	"testing"
)

// TestMemoryStateStore_Account round-trips the account through the
// in-memory store and confirms LoadAccount returns a copy.
func TestMemoryStateStore_Account(t *testing.T) {
	store := NewMemoryStateStore()
	if _, err := store.LoadAccount(); !IsKind(err, ErrKindNotFound) {
		t.Fatalf("empty store: want not_found, got %v", err)
	}
	if err := store.SaveAccount(&SignalAccount{ACI: "mem-aci", Password: "pw"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.LoadAccount()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ACI != "mem-aci" {
		t.Fatalf("aci = %q", got.ACI)
	}
	// Mutating the returned copy must not affect the stored record.
	got.ACI = "tampered"
	again, _ := store.LoadAccount()
	if again.ACI != "mem-aci" {
		t.Fatalf("store record mutated through returned copy")
	}
}

// TestNewOrchestratorBot_RequiredFields walks each required-field guard in
// the constructor so every branch is exercised.
func TestNewOrchestratorBot_RequiredFields(t *testing.T) {
	base := func() BotConfigRuntime {
		store := NewMemoryStateStore()
		mgr, _ := NewSessionManager(store, DefaultVerbosityPrefs())
		signal := newFakeSignalIO()
		tr, _ := NewEventTranslator(signal, mgr)
		auth, _ := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeImplicit, ServiceAccount: "bot"})
		return BotConfigRuntime{
			Config:       &BotConfig{},
			Signal:       signal,
			Orchestrator: newErrOrchestrator(),
			Auth:         auth,
			Manager:      mgr,
			Translator:   tr,
			Logger:       noopLogger{},
		}
	}
	cases := []struct {
		name   string
		mutate func(rt *BotConfigRuntime)
	}{
		{"no config", func(rt *BotConfigRuntime) { rt.Config = nil }},
		{"no signal", func(rt *BotConfigRuntime) { rt.Signal = nil }},
		{"no orchestrator", func(rt *BotConfigRuntime) { rt.Orchestrator = nil }},
		{"no auth", func(rt *BotConfigRuntime) { rt.Auth = nil }},
		{"no manager", func(rt *BotConfigRuntime) { rt.Manager = nil }},
		{"no translator", func(rt *BotConfigRuntime) { rt.Translator = nil }},
		{"no logger", func(rt *BotConfigRuntime) { rt.Logger = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := base()
			tc.mutate(&rt)
			if _, err := NewOrchestratorBot(rt); !IsKind(err, ErrKindInvalidInput) {
				t.Fatalf("want invalid_input, got %v", err)
			}
		})
	}
}

// TestNewOrchestratorBot_DefaultsParser fills in an IntentParser when none
// is supplied.
func TestNewOrchestratorBot_DefaultsParser(t *testing.T) {
	store := NewMemoryStateStore()
	mgr, _ := NewSessionManager(store, DefaultVerbosityPrefs())
	signal := newFakeSignalIO()
	tr, _ := NewEventTranslator(signal, mgr)
	auth, _ := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeImplicit, ServiceAccount: "bot"})
	bot, err := NewOrchestratorBot(BotConfigRuntime{
		Config:       &BotConfig{},
		Signal:       signal,
		Orchestrator: newErrOrchestrator(),
		Auth:         auth,
		Manager:      mgr,
		Translator:   tr,
		Logger:       noopLogger{},
		// Parser intentionally omitted.
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if bot.parser == nil {
		t.Fatalf("parser should be defaulted")
	}
}

// TestBot_HandleInbound_Unauthorized sends a 🔒 reply when the auth bridge
// denies the sender, without dispatching any intent.
func TestBot_HandleInbound_Unauthorized(t *testing.T) {
	store := NewMemoryStateStore()
	mgr, _ := NewSessionManager(store, DefaultVerbosityPrefs())
	signal := newFakeSignalIO()
	tr, _ := NewEventTranslator(signal, mgr)
	orch := newErrOrchestrator()
	// Linked mode with no links => every sender is unauthorized.
	auth, _ := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeLinked, TenantID: "t"})
	bot, err := NewOrchestratorBot(BotConfigRuntime{
		Config:       &BotConfig{TenantID: "t"},
		Signal:       signal,
		Orchestrator: orch,
		Auth:         auth,
		Manager:      mgr,
		Translator:   tr,
		Parser:       NewIntentParser(),
		Logger:       noopLogger{},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := bot.handleInbound(context.Background(), dmMsg("spawn claude")); err != nil {
		t.Fatalf("handleInbound: %v", err)
	}
	if !contains(lastSend(signal), "🔒") || !contains(lastSend(signal), "not authorized") {
		t.Fatalf("body = %q", lastSend(signal))
	}
	if orch.spawns != 0 {
		t.Fatalf("unauthorized sender must not spawn")
	}
}

// TestBot_MaybeStopSubscription_StillAttached leaves a subscription in
// place when another conversation is still attached to the session.
func TestBot_MaybeStopSubscription_StillAttached(t *testing.T) {
	bot, _, _, mgr := newDispatchBot(t)
	op := OperatorIdentity{TenantID: "t", SubjectID: "bot"}
	if err := mgr.Attach("conv-a", "sess-X", ConversationDM, op); err != nil {
		t.Fatalf("attach a: %v", err)
	}
	if err := mgr.Attach("conv-b", "sess-X", ConversationDM, op); err != nil {
		t.Fatalf("attach b: %v", err)
	}
	// Register a fake subscription cancel so we can observe it survives.
	stopped := false
	bot.subsMu.Lock()
	bot.subscriptions["sess-X"] = func() { stopped = true }
	bot.subsMu.Unlock()

	// conv-a still leaves conv-b attached => subscription must stay.
	_ = mgr.Detach("conv-a")
	bot.maybeStopSubscription("sess-X")
	if stopped {
		t.Fatalf("subscription should NOT be stopped while conv-b is attached")
	}

	// Detaching the last conversation must stop the subscription.
	_ = mgr.Detach("conv-b")
	bot.maybeStopSubscription("sess-X")
	if !stopped {
		t.Fatalf("subscription should be stopped once no conversation is attached")
	}
}
