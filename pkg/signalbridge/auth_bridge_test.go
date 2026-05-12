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

// TestAuthBridge_Implicit returns the service account for any Signal
// identity.
func TestAuthBridge_Implicit(t *testing.T) {
	ab, err := NewAuthBridge(AuthBridgeConfig{
		Mode:           AuthModeImplicit,
		TenantID:       "t1",
		ServiceAccount: "bot",
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	got, err := ab.ResolveOperator(context.Background(), "aci-123", "c", ConversationDM)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.SubjectID != "bot" || got.TenantID != "t1" {
		t.Fatalf("unexpected: %+v", got)
	}
}

// TestAuthBridge_Linked rejects unknown senders.
func TestAuthBridge_Linked(t *testing.T) {
	ab, err := NewAuthBridge(AuthBridgeConfig{
		Mode: AuthModeLinked,
		Links: map[string]OperatorIdentity{
			"aci-alice": {TenantID: "t1", SubjectID: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	got, err := ab.ResolveOperator(context.Background(), "aci-alice", "c", ConversationDM)
	if err != nil {
		t.Fatalf("known: %v", err)
	}
	if got.SubjectID != "alice" {
		t.Fatalf("wrong subject: %+v", got)
	}
	if _, err := ab.ResolveOperator(context.Background(), "aci-bob", "c", ConversationDM); !IsKind(err, ErrKindUnauthorized) {
		t.Fatalf("want unauthorized, got %v", err)
	}
}

// TestAuthBridge_GroupOnly rejects DMs and unknown groups.
func TestAuthBridge_GroupOnly(t *testing.T) {
	ab, err := NewAuthBridge(AuthBridgeConfig{
		Mode:           AuthModeGroupOnly,
		ServiceAccount: "bot",
		AllowedGroups:  map[string]struct{}{"grp-1": {}},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := ab.ResolveOperator(context.Background(), "aci", "c", ConversationDM); !IsKind(err, ErrKindUnauthorized) {
		t.Fatalf("DM should be rejected, got %v", err)
	}
	if _, err := ab.ResolveOperator(context.Background(), "aci", "grp-2", ConversationGroup); !IsKind(err, ErrKindUnauthorized) {
		t.Fatalf("unknown group should be rejected, got %v", err)
	}
	got, err := ab.ResolveOperator(context.Background(), "aci", "grp-1", ConversationGroup)
	if err != nil {
		t.Fatalf("allowed group: %v", err)
	}
	if got.Source != "group-admin" {
		t.Fatalf("source: %q", got.Source)
	}
}

// TestNewAuthBridge_Errors covers invalid configs.
func TestNewAuthBridge_Errors(t *testing.T) {
	if _, err := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeImplicit}); err == nil {
		t.Fatalf("implicit without service account should fail")
	}
	if _, err := NewAuthBridge(AuthBridgeConfig{Mode: "nonsense"}); err == nil {
		t.Fatalf("unknown mode should fail")
	}
}

// TestAuthBridge_IsGroupCommandAllowed returns true in non-group-only
// modes and respects allowlist in group-only mode.
func TestAuthBridge_IsGroupCommandAllowed(t *testing.T) {
	ab, _ := NewAuthBridge(AuthBridgeConfig{Mode: AuthModeImplicit, ServiceAccount: "bot"})
	if !ab.IsGroupCommandAllowed(context.Background(), "anything") {
		t.Fatalf("implicit mode should allow all")
	}
	ab2, _ := NewAuthBridge(AuthBridgeConfig{
		Mode:           AuthModeGroupOnly,
		ServiceAccount: "bot",
		AllowedGroups:  map[string]struct{}{"grp": {}},
	})
	if ab2.IsGroupCommandAllowed(context.Background(), "other") {
		t.Fatalf("unknown group should not be allowed")
	}
	if !ab2.IsGroupCommandAllowed(context.Background(), "grp") {
		t.Fatalf("known group should be allowed")
	}
}
