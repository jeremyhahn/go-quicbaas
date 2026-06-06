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

// TestAuthBridge_Link adds a link at runtime that ResolveOperator then
// honors. Starts from an empty Links map (the linked-mode default).
func TestAuthBridge_Link(t *testing.T) {
	ab, err := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeLinked, TenantID: "t1"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	concrete, ok := ab.(*defaultAuthBridge)
	if !ok {
		t.Fatalf("expected *defaultAuthBridge")
	}
	// Before linking, the sender is unauthorized.
	if _, err := ab.ResolveOperator(context.Background(), "aci-x", "c", ConversationDM); !IsKind(err, ErrKindUnauthorized) {
		t.Fatalf("pre-link should be unauthorized, got %v", err)
	}
	concrete.Link("aci-x", OperatorIdentity{SubjectID: "user-x"})
	got, err := ab.ResolveOperator(context.Background(), "aci-x", "c", ConversationDM)
	if err != nil {
		t.Fatalf("post-link resolve: %v", err)
	}
	if got.SubjectID != "user-x" || got.SignalID != "aci-x" || got.TenantID != "t1" || got.Source != "link" {
		t.Fatalf("resolved identity wrong: %+v", got)
	}
}

// TestAuthBridge_Link_NilMapInit covers the lazy-init branch when a bridge
// was built with a nil Links map and Link is called.
func TestAuthBridge_Link_NilMapInit(t *testing.T) {
	ab := &defaultAuthBridge{cfg: AuthBridgeConfig{Mode: AuthModeLinked, TenantID: "t"}}
	ab.Link("aci", OperatorIdentity{SubjectID: "u"})
	got, err := ab.ResolveOperator(context.Background(), "aci", "c", ConversationDM)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SubjectID != "u" {
		t.Fatalf("subject = %q", got.SubjectID)
	}
}

// TestAuthBridge_AllowGroup adds a group to the allowlist at runtime.
func TestAuthBridge_AllowGroup(t *testing.T) {
	ab, err := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeGroupOnly, ServiceAccount: "bot", TenantID: "t"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	concrete := ab.(*defaultAuthBridge)
	if ab.IsGroupCommandAllowed(context.Background(), "grp-1") {
		t.Fatalf("group should not be allowed before AllowGroup")
	}
	concrete.AllowGroup("grp-1")
	if !ab.IsGroupCommandAllowed(context.Background(), "grp-1") {
		t.Fatalf("group should be allowed after AllowGroup")
	}
	if _, err := ab.ResolveOperator(context.Background(), "aci", "grp-1", ConversationGroup); err != nil {
		t.Fatalf("resolve allowed group: %v", err)
	}
}

// TestAuthBridge_AllowGroup_NilMapInit covers the lazy-init branch.
func TestAuthBridge_AllowGroup_NilMapInit(t *testing.T) {
	ab := &defaultAuthBridge{cfg: AuthBridgeConfig{Mode: AuthModeGroupOnly, ServiceAccount: "bot", TenantID: "t"}}
	ab.AllowGroup("grp")
	if !ab.IsGroupCommandAllowed(context.Background(), "grp") {
		t.Fatalf("group should be allowed after AllowGroup with nil initial map")
	}
}

// TestAuthBridge_ResolveOperator_EmptySignalID rejects an empty sender.
func TestAuthBridge_ResolveOperator_EmptySignalID(t *testing.T) {
	ab, _ := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeImplicit, ServiceAccount: "bot"})
	if _, err := ab.ResolveOperator(context.Background(), "", "c", ConversationDM); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("want invalid_input, got %v", err)
	}
}

// TestAuthBridge_LinkedDefaultsTenant fills the configured tenant when a
// linked identity omits one.
func TestAuthBridge_LinkedDefaultsTenant(t *testing.T) {
	ab, _ := NewAuthBridge(AuthBridgeConfig{
		Mode:     AuthModeLinked,
		TenantID: "tenant-default",
		Links:    map[string]OperatorIdentity{"aci": {SubjectID: "u"}},
	})
	got, err := ab.ResolveOperator(context.Background(), "aci", "c", ConversationDM)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.TenantID != "tenant-default" {
		t.Fatalf("tenant = %q, want tenant-default", got.TenantID)
	}
}

// TestNewAuthBridge_DefaultsMode treats an empty mode as implicit.
func TestNewAuthBridge_DefaultsMode(t *testing.T) {
	if _, err := NewAuthBridge(AuthBridgeConfig{ServiceAccount: "bot"}); err != nil {
		t.Fatalf("empty mode should default to implicit: %v", err)
	}
}
