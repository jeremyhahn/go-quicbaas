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

package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// TestBuildAudienceWithDefault covers the precedence rules: explicit resource
// wins, default applies only when no resource is sent, and neither set falls
// back to the client-only audience.
func TestBuildAudienceWithDefault(t *testing.T) {
	tests := []struct {
		name            string
		clientID        string
		resource        string
		defaultResource string
		want            []string
	}{
		{
			name:     "no resource and no default yields client only",
			clientID: "https://issuer.example.com",
			want:     []string{"https://issuer.example.com"},
		},
		{
			name:            "no resource uses default",
			clientID:        "https://issuer.example.com",
			defaultResource: "api://orchestrator",
			want:            []string{"https://issuer.example.com", "api://orchestrator"},
		},
		{
			name:            "explicit resource overrides default",
			clientID:        "https://issuer.example.com",
			resource:        "https://api.example.com/mcp",
			defaultResource: "api://orchestrator",
			want:            []string{"https://issuer.example.com", "https://api.example.com/mcp"},
		},
		{
			name:            "default equal to clientID is not duplicated",
			clientID:        "https://issuer.example.com",
			defaultResource: "https://issuer.example.com",
			want:            []string{"https://issuer.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAudienceWithDefault(tt.clientID, tt.resource, tt.defaultResource)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("aud[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestValidateDefaultAudience covers the validator: empty (no default) is valid,
// api:// and https are accepted, http is localhost-only, other schemes and
// non-absolute / fragment forms are rejected.
func TestValidateDefaultAudience(t *testing.T) {
	tests := []struct {
		name     string
		audience string
		wantErr  bool
	}{
		{name: "empty is valid (no default)", audience: ""},
		{name: "api scheme service audience", audience: "api://orchestrator"},
		{name: "https URI", audience: "https://api.example.com/mcp"},
		{name: "http localhost dev", audience: "http://localhost:8080"},
		{name: "http non-localhost rejected", audience: "http://api.example.com", wantErr: true},
		{name: "ftp scheme rejected", audience: "ftp://api.example.com", wantErr: true},
		{name: "relative rejected", audience: "orchestrator", wantErr: true},
		{name: "fragment rejected", audience: "https://api.example.com#frag", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDefaultAudience(tt.audience)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tt.audience)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.audience, err)
			}
			if tt.wantErr {
				var uriErr *ResourceURIError
				if !errors.As(err, &uriErr) {
					t.Fatalf("expected *ResourceURIError, got %T: %v", err, err)
				}
			}
		})
	}
}

// TestTokenIssuance_DefaultAudience drives the client_credentials token path to
// verify the server-side default audience behavior end to end.
func TestTokenIssuance_DefaultAudience(t *testing.T) {
	provider, privateKey, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	hashedSecret := currentSecretHash(t, "test-secret")

	// App WITH a default audience configured.
	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:        "svc-with-default",
		ClientSecret:    hashedSecret,
		TenantID:        "tenant-1",
		GrantTypes:      []string{GrantTypeClientCredentials},
		DefaultAudience: "api://orchestrator",
		// The permission this fixture requests. A client_credentials token
		// carries the grant on its application record, not the scope the
		// client named (go-quicbaas#653).
		Scopes: []string{"mcp:tools"},
	}); err != nil {
		t.Fatalf("CreateApp(with default): %v", err)
	}

	// App WITHOUT a default audience (current behavior must be unchanged).
	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:     "svc-no-default",
		ClientSecret: hashedSecret,
		TenantID:     "tenant-1",
		GrantTypes:   []string{GrantTypeClientCredentials},
		// The permission this fixture requests. A client_credentials token
		// carries the grant on its application record, not the scope the
		// client named (go-quicbaas#653).
		Scopes: []string{"mcp:tools"},
	}); err != nil {
		t.Fatalf("CreateApp(no default): %v", err)
	}

	// App with an INVALID stored default audience: token issuance must ignore
	// it and fall back to today's behavior rather than fail.
	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:        "svc-bad-default",
		ClientSecret:    hashedSecret,
		TenantID:        "tenant-1",
		GrantTypes:      []string{GrantTypeClientCredentials},
		DefaultAudience: "not-a-uri",
		// The permission this fixture requests. A client_credentials token
		// carries the grant on its application record, not the scope the
		// client named (go-quicbaas#653).
		Scopes: []string{"mcp:tools"},
	}); err != nil {
		t.Fatalf("CreateApp(bad default): %v", err)
	}

	tests := []struct {
		name     string
		clientID string
		resource string
		want     []string
	}{
		{
			name:     "no resource and app has default uses default",
			clientID: "svc-with-default",
			want:     []string{"https://test.example.com", "api://orchestrator"},
		},
		{
			name:     "no resource and no default keeps issuer-only (unchanged)",
			clientID: "svc-no-default",
			want:     []string{"https://test.example.com"},
		},
		{
			name:     "explicit resource overrides app default",
			clientID: "svc-with-default",
			resource: "https://api.example.com/mcp",
			want:     []string{"https://test.example.com", "https://api.example.com/mcp"},
		},
		{
			name:     "invalid stored default is ignored",
			clientID: "svc-bad-default",
			want:     []string{"https://test.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := provider.Token(ctx, &TokenRequest{
				GrantType:    GrantTypeClientCredentials,
				ClientID:     tt.clientID,
				ClientSecret: "test-secret",
				Scope:        []string{"mcp:tools"},
				Resource:     tt.resource,
			})
			if err != nil {
				t.Fatalf("Token() error: %v", err)
			}

			claims, err := xkms.JWTVerifyWithKeyAndClaims(resp.AccessToken, &privateKey.PublicKey, "RS256")
			if err != nil {
				t.Fatalf("verify token: %v", err)
			}
			audClaim, ok := claims["aud"].([]interface{})
			if !ok {
				t.Fatalf("aud claim not []interface{}: %T", claims["aud"])
			}
			got := make([]string, 0, len(audClaim))
			for _, a := range audClaim {
				if s, ok := a.(string); ok {
					got = append(got, s)
				}
			}
			if len(got) != len(tt.want) {
				t.Fatalf("aud = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("aud[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestRegisterApp_RejectsInvalidDefaultAudience verifies the CRUD boundary
// rejects a malformed default audience on create.
func TestRegisterApp_RejectsInvalidDefaultAudience(t *testing.T) {
	storage := NewMemoryStorage()
	mgr := NewAppManager(storage, nil)

	_, err := mgr.RegisterApp(context.Background(), "tenant-1", RegisterAppOptions{
		Name:            "bad-app",
		GrantTypes:      []string{GrantTypeClientCredentials},
		DefaultAudience: "ftp://nope",
	})
	if err == nil {
		t.Fatal("expected RegisterApp to reject an invalid default audience")
	}
}

// TestUpdateApp_DefaultAudience verifies update both sets a valid default and
// rejects an invalid one.
func TestUpdateApp_DefaultAudience(t *testing.T) {
	storage := NewMemoryStorage()
	mgr := NewAppManager(storage, nil)
	ctx := context.Background()

	created, err := mgr.RegisterApp(ctx, "tenant-1", RegisterAppOptions{
		Name:       "svc",
		GrantTypes: []string{GrantTypeClientCredentials},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}

	good := "api://orchestrator"
	updated, err := mgr.UpdateApp(ctx, created.ClientID, UpdateAppOptions{DefaultAudience: &good})
	if err != nil {
		t.Fatalf("UpdateApp(valid default): %v", err)
	}
	if updated.DefaultAudience != good {
		t.Fatalf("DefaultAudience = %q, want %q", updated.DefaultAudience, good)
	}

	bad := "ftp://nope"
	if _, err := mgr.UpdateApp(ctx, created.ClientID, UpdateAppOptions{DefaultAudience: &bad}); err == nil {
		t.Fatal("expected UpdateApp to reject an invalid default audience")
	}
}

// TestDefaultAudienceForClient_EdgeCases covers the helper's fail-open paths:
// an empty client id and an unknown client both resolve to "" (no default)
// rather than failing token issuance.
func TestDefaultAudienceForClient_EdgeCases(t *testing.T) {
	provider, _, appStorage, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()

	if got := provider.defaultAudienceForClient(ctx, ""); got != "" {
		t.Fatalf("empty client id must yield no default, got %q", got)
	}

	if got := provider.defaultAudienceForClient(ctx, "no-such-client"); got != "" {
		t.Fatalf("unknown client must yield no default, got %q", got)
	}

	// Known client with a valid default resolves it.
	if err := appStorage.CreateApp(ctx, &Application{
		ClientID:        "known-client",
		TenantID:        "tenant-1",
		GrantTypes:      []string{GrantTypeClientCredentials},
		DefaultAudience: "api://orchestrator",
	}); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	if got := provider.defaultAudienceForClient(ctx, "known-client"); got != "api://orchestrator" {
		t.Fatalf("expected api://orchestrator, got %q", got)
	}
}

// TestValidateDefaultAudience_ParseError covers the url.Parse failure branch.
func TestValidateDefaultAudience_ParseError(t *testing.T) {
	// A control byte (0x7f) makes url.Parse fail outright.
	err := ValidateDefaultAudience("api://orchestrator\u007f")
	if err == nil {
		t.Fatal("expected parse error for malformed audience")
	}
	var uriErr *ResourceURIError
	if !errors.As(err, &uriErr) {
		t.Fatalf("expected *ResourceURIError, got %T", err)
	}
}
