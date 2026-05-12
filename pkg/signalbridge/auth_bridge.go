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
	"sync"
)

// AuthBridge resolves a Signal sender identity to a QuicBaaS operator
// under the configured AuthMode. The three policies are documented on
// AuthMode.
type AuthBridge interface {
	// ResolveOperator returns the OperatorIdentity the orchestrator
	// will see for an inbound message. Returns ErrKindUnauthorized when
	// the policy denies the caller.
	ResolveOperator(ctx context.Context, signalID string, conversationID string, kind ConversationKind) (*OperatorIdentity, error)

	// IsGroupCommandAllowed reports whether commands from group
	// conversationID are permitted. Used in AuthModeGroupOnly to
	// filter unknown groups.
	IsGroupCommandAllowed(ctx context.Context, conversationID string) bool
}

// AuthBridgeConfig configures the default AuthBridge implementation.
type AuthBridgeConfig struct {
	Mode     AuthMode
	TenantID string
	// ServiceAccount is the QuicBaaS subject used under
	// AuthModeImplicit. In that mode the Signal identity is recorded
	// for audit but does not control QuicBaaS authorization.
	ServiceAccount string
	// Links is the current map from Signal ACI to an OperatorIdentity
	// under AuthModeLinked. Empty when links have not yet been
	// established.
	Links map[string]OperatorIdentity
	// AllowedGroups is the set of group conversation IDs that may issue
	// commands under AuthModeGroupOnly.
	AllowedGroups map[string]struct{}
}

// defaultAuthBridge implements AuthBridge for the three documented
// policies. Links and group allowlist are mutable behind a mutex so
// tests and the runtime link command can update them without
// recreating the bridge.
type defaultAuthBridge struct {
	cfg AuthBridgeConfig
	mu  sync.RWMutex
}

// NewAuthBridge constructs a bridge from cfg. Default mode is
// AuthModeImplicit when empty.
func NewAuthBridge(cfg AuthBridgeConfig) (AuthBridge, error) {
	const op = "signalbridge.NewAuthBridge"
	if cfg.Mode == "" {
		cfg.Mode = AuthModeImplicit
	}
	switch cfg.Mode {
	case AuthModeImplicit:
		if cfg.ServiceAccount == "" {
			return nil, newError(op, ErrKindInvalidInput, "ServiceAccount is required for implicit mode")
		}
	case AuthModeLinked:
		if cfg.Links == nil {
			cfg.Links = make(map[string]OperatorIdentity)
		}
	case AuthModeGroupOnly:
		if cfg.AllowedGroups == nil {
			cfg.AllowedGroups = make(map[string]struct{})
		}
	default:
		return nil, newError(op, ErrKindInvalidInput, "unknown AuthMode: "+string(cfg.Mode))
	}
	if cfg.TenantID == "" {
		cfg.TenantID = "default"
	}
	return &defaultAuthBridge{cfg: cfg}, nil
}

// ResolveOperator implements AuthBridge.
func (a *defaultAuthBridge) ResolveOperator(ctx context.Context, signalID string, conversationID string, kind ConversationKind) (*OperatorIdentity, error) {
	const op = "signalbridge.AuthBridge.ResolveOperator"
	if signalID == "" {
		return nil, newError(op, ErrKindInvalidInput, "signalID required")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()

	switch a.cfg.Mode {
	case AuthModeImplicit:
		return &OperatorIdentity{
			TenantID:    a.cfg.TenantID,
			SubjectID:   a.cfg.ServiceAccount,
			DisplayName: signalID,
			SignalID:    signalID,
			Source:      "implicit",
		}, nil

	case AuthModeLinked:
		if ident, ok := a.cfg.Links[signalID]; ok {
			ident.SignalID = signalID
			if ident.TenantID == "" {
				ident.TenantID = a.cfg.TenantID
			}
			if ident.Source == "" {
				ident.Source = "link"
			}
			return &ident, nil
		}
		return nil, &Error{Op: op, Kind: ErrKindUnauthorized, Msg: "no quicbaas link for Signal identity " + signalID}

	case AuthModeGroupOnly:
		if kind != ConversationGroup {
			return nil, &Error{Op: op, Kind: ErrKindUnauthorized, Msg: "group-only mode: DMs are read-only"}
		}
		if _, ok := a.cfg.AllowedGroups[conversationID]; !ok {
			return nil, &Error{Op: op, Kind: ErrKindUnauthorized, Msg: "group " + conversationID + " not on allowlist"}
		}
		return &OperatorIdentity{
			TenantID:    a.cfg.TenantID,
			SubjectID:   a.cfg.ServiceAccount,
			DisplayName: signalID,
			SignalID:    signalID,
			Source:      "group-admin",
		}, nil
	}
	return nil, newError(op, ErrKindInternal, "unreachable mode branch")
}

// IsGroupCommandAllowed implements AuthBridge.
func (a *defaultAuthBridge) IsGroupCommandAllowed(ctx context.Context, conversationID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.cfg.Mode != AuthModeGroupOnly {
		return true
	}
	_, ok := a.cfg.AllowedGroups[conversationID]
	return ok
}

// Link adds (or replaces) an entry in the linked-identity map. Used
// by the `quicbaas signalbridge link` CLI subcommand to complete a
// one-time verification handshake.
func (a *defaultAuthBridge) Link(signalID string, ident OperatorIdentity) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Links == nil {
		a.cfg.Links = make(map[string]OperatorIdentity)
	}
	a.cfg.Links[signalID] = ident
}

// AllowGroup adds a conversation ID to the AuthModeGroupOnly allowlist.
func (a *defaultAuthBridge) AllowGroup(conversationID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.AllowedGroups == nil {
		a.cfg.AllowedGroups = make(map[string]struct{})
	}
	a.cfg.AllowedGroups[conversationID] = struct{}{}
}
