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

package orchestration

import (
	"errors"
	"testing"
	"time"
)

func TestAgentIdentity_Validate_Success(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "agent-001",
		ServiceAccountID: "sa-001",
		TenantID:         "tenant-001",
		Role:             "worker",
		Scopes:           []string{"kms:read", "secrets:read"},
		ClientID:         "client-001",
		ClientSecret:     "secret-001",
	}
	if err := ai.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestAgentIdentity_Validate_MissingFields(t *testing.T) {
	tests := []struct {
		name     string
		identity *AgentIdentity
		wantErr  error
		field    string
	}{
		{
			name:     "missing agent_id",
			identity: &AgentIdentity{ServiceAccountID: "sa", TenantID: "t", Role: "w", Scopes: []string{"a:b"}, ClientID: "c"},
			wantErr:  ErrAgentIdentityRequired,
			field:    "agent_id",
		},
		{
			name:     "missing service_account_id",
			identity: &AgentIdentity{AgentID: "a", TenantID: "t", Role: "w", Scopes: []string{"a:b"}, ClientID: "c"},
			wantErr:  ErrAgentIdentityRequired,
			field:    "service_account_id",
		},
		{
			name:     "missing tenant_id",
			identity: &AgentIdentity{AgentID: "a", ServiceAccountID: "sa", Role: "w", Scopes: []string{"a:b"}, ClientID: "c"},
			wantErr:  ErrAgentIdentityRequired,
			field:    "tenant_id",
		},
		{
			name:     "missing role",
			identity: &AgentIdentity{AgentID: "a", ServiceAccountID: "sa", TenantID: "t", Scopes: []string{"a:b"}, ClientID: "c"},
			wantErr:  ErrAgentIdentityRequired,
			field:    "role",
		},
		{
			name:     "missing scopes",
			identity: &AgentIdentity{AgentID: "a", ServiceAccountID: "sa", TenantID: "t", Role: "w", ClientID: "c"},
			wantErr:  ErrAgentIdentityRequired,
			field:    "scopes",
		},
		{
			name:     "empty scopes",
			identity: &AgentIdentity{AgentID: "a", ServiceAccountID: "sa", TenantID: "t", Role: "w", Scopes: []string{}, ClientID: "c"},
			wantErr:  ErrAgentIdentityRequired,
			field:    "scopes",
		},
		{
			name:     "missing client_id",
			identity: &AgentIdentity{AgentID: "a", ServiceAccountID: "sa", TenantID: "t", Role: "w", Scopes: []string{"a:b"}},
			wantErr:  ErrAgentIdentityRequired,
			field:    "client_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.identity.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got: %v", tt.wantErr, err)
			}
			var reqErr *AgentIdentityRequiredError
			if errors.As(err, &reqErr) {
				if reqErr.Field != tt.field {
					t.Errorf("field = %q, want %q", reqErr.Field, tt.field)
				}
			}
		})
	}
}

func TestAgentIdentity_Validate_InvalidScope(t *testing.T) {
	tests := []struct {
		name      string
		scopes    []string
		wantScope string
	}{
		{
			name:      "no colon separator",
			scopes:    []string{"invalidscope"},
			wantScope: "invalidscope",
		},
		{
			name:      "empty segment",
			scopes:    []string{":read"},
			wantScope: ":read",
		},
		{
			name:      "trailing colon",
			scopes:    []string{"kms:"},
			wantScope: "kms:",
		},
		{
			name:      "starts with number",
			scopes:    []string{"1kms:read"},
			wantScope: "1kms:read",
		},
		{
			name:      "spaces in scope",
			scopes:    []string{"kms :read"},
			wantScope: "kms :read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ai := &AgentIdentity{
				AgentID:          "a",
				ServiceAccountID: "sa",
				TenantID:         "t",
				Role:             "w",
				Scopes:           tt.scopes,
				ClientID:         "c",
			}
			err := ai.Validate()
			if err == nil {
				t.Fatal("expected error for invalid scope, got nil")
			}
			if !errors.Is(err, ErrAgentIdentityInvalidScope) {
				t.Errorf("expected ErrAgentIdentityInvalidScope, got: %v", err)
			}
			var scopeErr *AgentIdentityInvalidScopeError
			if errors.As(err, &scopeErr) {
				if scopeErr.Scope != tt.wantScope {
					t.Errorf("scope = %q, want %q", scopeErr.Scope, tt.wantScope)
				}
			}
		})
	}
}

func TestAgentIdentity_HasScope(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "a",
		ServiceAccountID: "sa",
		TenantID:         "t",
		Role:             "w",
		Scopes:           []string{"kms:read", "secrets:write", "orchestrator:*"},
		ClientID:         "c",
	}

	tests := []struct {
		scope string
		want  bool
	}{
		{"kms:read", true},
		{"secrets:write", true},
		{"kms:write", false},
		{"orchestrator:read", true},
		{"orchestrator:write", true},
		{"orchestrator:admin", true},
		{"unknown:read", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got := ai.HasScope(tt.scope)
			if got != tt.want {
				t.Errorf("HasScope(%q) = %v, want %v", tt.scope, got, tt.want)
			}
		})
	}
}

func TestAgentIdentity_HasAnyScope(t *testing.T) {
	ai := &AgentIdentity{
		AgentID:          "a",
		ServiceAccountID: "sa",
		TenantID:         "t",
		Role:             "w",
		Scopes:           []string{"kms:read"},
		ClientID:         "c",
	}

	if !ai.HasAnyScope("kms:read", "kms:write") {
		t.Error("expected HasAnyScope to return true when one scope matches")
	}
	if ai.HasAnyScope("secrets:read", "secrets:write") {
		t.Error("expected HasAnyScope to return false when no scope matches")
	}
	if ai.HasAnyScope() {
		t.Error("expected HasAnyScope to return false with empty input")
	}
}

func TestAgentSecurityContext_Validate_Success(t *testing.T) {
	sc := &AgentSecurityContext{
		Identity: &AgentIdentity{
			AgentID:          "a",
			ServiceAccountID: "sa",
			TenantID:         "t",
			Role:             "w",
			Scopes:           []string{"kms:read"},
			ClientID:         "c",
		},
		AccessToken:   "token-abc",
		TokenExpiry:   time.Now().Add(time.Hour).Unix(),
		CorrelationID: "corr-001",
	}
	if err := sc.Validate(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestAgentSecurityContext_Validate_MissingFields(t *testing.T) {
	validIdentity := &AgentIdentity{
		AgentID:          "a",
		ServiceAccountID: "sa",
		TenantID:         "t",
		Role:             "w",
		Scopes:           []string{"kms:read"},
		ClientID:         "c",
	}

	tests := []struct {
		name    string
		ctx     *AgentSecurityContext
		wantErr error
		field   string
	}{
		{
			name:    "nil identity",
			ctx:     &AgentSecurityContext{AccessToken: "t", TokenExpiry: 1, CorrelationID: "c"},
			wantErr: ErrAgentSecurityContextRequired,
			field:   "identity",
		},
		{
			name:    "missing access_token",
			ctx:     &AgentSecurityContext{Identity: validIdentity, TokenExpiry: 1, CorrelationID: "c"},
			wantErr: ErrAgentSecurityContextRequired,
			field:   "access_token",
		},
		{
			name:    "zero token_expiry",
			ctx:     &AgentSecurityContext{Identity: validIdentity, AccessToken: "t", CorrelationID: "c"},
			wantErr: ErrAgentSecurityContextRequired,
			field:   "token_expiry",
		},
		{
			name:    "missing correlation_id",
			ctx:     &AgentSecurityContext{Identity: validIdentity, AccessToken: "t", TokenExpiry: 1},
			wantErr: ErrAgentSecurityContextRequired,
			field:   "correlation_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ctx.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("expected %v, got: %v", tt.wantErr, err)
			}
			var reqErr *AgentSecurityContextRequiredError
			if errors.As(err, &reqErr) {
				if reqErr.Field != tt.field {
					t.Errorf("field = %q, want %q", reqErr.Field, tt.field)
				}
			}
		})
	}
}

func TestAgentSecurityContext_Validate_InvalidIdentity(t *testing.T) {
	sc := &AgentSecurityContext{
		Identity: &AgentIdentity{
			// Missing AgentID
			ServiceAccountID: "sa",
			TenantID:         "t",
			Role:             "w",
			Scopes:           []string{"kms:read"},
			ClientID:         "c",
		},
		AccessToken:   "token",
		TokenExpiry:   time.Now().Add(time.Hour).Unix(),
		CorrelationID: "corr-001",
	}
	err := sc.Validate()
	if err == nil {
		t.Fatal("expected error for invalid identity, got nil")
	}
	if !errors.Is(err, ErrAgentIdentityRequired) {
		t.Errorf("expected ErrAgentIdentityRequired, got: %v", err)
	}
}

func TestAgentSecurityContext_IsTokenExpired(t *testing.T) {
	now := time.Now().Unix()

	sc := &AgentSecurityContext{
		TokenExpiry: now + 3600,
	}
	if sc.IsTokenExpired(now) {
		t.Error("expected token to not be expired")
	}
	if !sc.IsTokenExpired(now + 3600) {
		t.Error("expected token to be expired at exact expiry time")
	}
	if !sc.IsTokenExpired(now + 7200) {
		t.Error("expected token to be expired after expiry time")
	}
}

func TestIsValidScopeFormat(t *testing.T) {
	tests := []struct {
		scope string
		valid bool
	}{
		{"kms:read", true},
		{"secrets:write", true},
		{"orchestrator:*", true},
		{"agent:spawn", true},
		{"agent:*", true},
		{"kms:keys:read", true},
		{"my-resource:my-action", true},
		{"res_1:act_2", true},
		{"a:b", true},
		{"", false},
		{"nocolon", false},
		{":read", false},
		{"kms:", false},
		{"1kms:read", false},
		{"kms :read", false},
		{"kms:read:write:extra", false},
		{" kms:read", false},
		{"kms:read ", false},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got := IsValidScopeFormat(tt.scope)
			if got != tt.valid {
				t.Errorf("IsValidScopeFormat(%q) = %v, want %v", tt.scope, got, tt.valid)
			}
		})
	}
}

func TestAgentIdentityRequiredError_ErrorMessage(t *testing.T) {
	err := &AgentIdentityRequiredError{Field: "agent_id"}
	want := "orchestration: agent identity required field missing: agent_id"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrAgentIdentityRequired) {
		t.Error("expected errors.Is to match ErrAgentIdentityRequired")
	}
}

func TestAgentSecurityContextRequiredError_ErrorMessage(t *testing.T) {
	err := &AgentSecurityContextRequiredError{Field: "access_token"}
	want := "orchestration: agent security context required field missing: access_token"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrAgentSecurityContextRequired) {
		t.Error("expected errors.Is to match ErrAgentSecurityContextRequired")
	}
}

func TestAgentIdentityInvalidScopeError_ErrorMessage(t *testing.T) {
	err := &AgentIdentityInvalidScopeError{Scope: "bad"}
	want := "orchestration: agent identity invalid scope format: bad"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, ErrAgentIdentityInvalidScope) {
		t.Error("expected errors.Is to match ErrAgentIdentityInvalidScope")
	}
}
