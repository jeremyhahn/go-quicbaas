//go:build iam

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
	"testing"
)

// TestValidateResourceIndicator tests RFC 8707 resource indicator validation.
func TestValidateResourceIndicator(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		config   *ResourceIndicatorConfig
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "empty resource is valid",
			resource: "",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  false,
		},
		{
			name:     "valid HTTPS resource URI",
			resource: "https://api.example.com/mcp",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  false,
		},
		{
			name:     "valid localhost HTTP URI for development",
			resource: "http://localhost:8080/mcp",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  false,
		},
		{
			name:     "valid 127.0.0.1 HTTP URI for development",
			resource: "http://127.0.0.1:8080/mcp",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  false,
		},
		{
			name:     "HTTP resource URI rejected (non-localhost)",
			resource: "http://api.example.com/mcp",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  true,
			errMsg:   "HTTPS",
		},
		{
			name:     "relative URI rejected",
			resource: "/mcp/resources",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  true,
			errMsg:   "absolute",
		},
		{
			name:     "URI with fragment rejected",
			resource: "https://api.example.com/mcp#section",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  true,
			errMsg:   "fragment",
		},
		{
			name:     "invalid URI format rejected",
			resource: "://invalid-uri",
			config:   DefaultResourceIndicatorConfig(),
			wantErr:  true,
			errMsg:   "invalid",
		},
		{
			name:     "resource in allowed list",
			resource: "https://api.example.com/mcp",
			config: &ResourceIndicatorConfig{
				Enabled:          true,
				AllowedResources: []string{"https://api.example.com/mcp"},
				RequireHTTPS:     true,
			},
			wantErr: false,
		},
		{
			name:     "resource with prefix in allowed list",
			resource: "https://api.example.com/mcp/v1",
			config: &ResourceIndicatorConfig{
				Enabled:          true,
				AllowedResources: []string{"https://api.example.com/mcp"},
				RequireHTTPS:     true,
			},
			wantErr: false,
		},
		{
			name:     "resource not in allowed list",
			resource: "https://other.example.com/mcp",
			config: &ResourceIndicatorConfig{
				Enabled:          true,
				AllowedResources: []string{"https://api.example.com/mcp"},
				RequireHTTPS:     true,
			},
			wantErr: true,
			errMsg:  "not in allowed list",
		},
		{
			name:     "nil config allows any valid URI",
			resource: "https://api.example.com/mcp",
			config:   nil,
			wantErr:  false,
		},
		{
			name:     "HTTP allowed when RequireHTTPS is false",
			resource: "http://api.example.com/mcp",
			config: &ResourceIndicatorConfig{
				Enabled:          true,
				AllowedResources: nil,
				RequireHTTPS:     false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResourceIndicator(tt.resource, tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateResourceIndicator() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !stringContains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateResourceIndicator() error = %v, want error containing %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateResourceIndicator() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestBuildAudience tests audience construction per RFC 8707.
func TestBuildAudience(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
		resource string
		want     []string
	}{
		{
			name:     "only client ID when no resource",
			clientID: "https://issuer.example.com",
			resource: "",
			want:     []string{"https://issuer.example.com"},
		},
		{
			name:     "client ID and resource when resource provided",
			clientID: "https://issuer.example.com",
			resource: "https://api.example.com/mcp",
			want:     []string{"https://issuer.example.com", "https://api.example.com/mcp"},
		},
		{
			name:     "only client ID when resource equals client ID",
			clientID: "https://issuer.example.com",
			resource: "https://issuer.example.com",
			want:     []string{"https://issuer.example.com"},
		},
		{
			name:     "multiple different URIs",
			clientID: "client-123",
			resource: "https://resource.example.com",
			want:     []string{"client-123", "https://resource.example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildAudience(tt.clientID, tt.resource)
			if len(got) != len(tt.want) {
				t.Errorf("BuildAudience() len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("BuildAudience()[%d] = %v, want %v", i, v, tt.want[i])
				}
			}
		})
	}
}

// TestValidateAudience tests audience validation per RFC 8707.
func TestValidateAudience(t *testing.T) {
	tests := []struct {
		name             string
		audience         []string
		expectedResource string
		want             bool
	}{
		{
			name:             "empty expected resource always valid",
			audience:         []string{"https://issuer.example.com"},
			expectedResource: "",
			want:             true,
		},
		{
			name:             "resource in audience",
			audience:         []string{"https://issuer.example.com", "https://api.example.com/mcp"},
			expectedResource: "https://api.example.com/mcp",
			want:             true,
		},
		{
			name:             "resource not in audience",
			audience:         []string{"https://issuer.example.com"},
			expectedResource: "https://api.example.com/mcp",
			want:             false,
		},
		{
			name:             "single element audience matches",
			audience:         []string{"https://api.example.com/mcp"},
			expectedResource: "https://api.example.com/mcp",
			want:             true,
		},
		{
			name:             "empty audience with expected resource",
			audience:         []string{},
			expectedResource: "https://api.example.com/mcp",
			want:             false,
		},
		{
			name:             "nil audience with expected resource",
			audience:         nil,
			expectedResource: "https://api.example.com/mcp",
			want:             false,
		},
		{
			name:             "nil audience with empty expected",
			audience:         nil,
			expectedResource: "",
			want:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateAudience(tt.audience, tt.expectedResource)
			if got != tt.want {
				t.Errorf("ValidateAudience() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDefaultResourceIndicatorConfig tests default configuration.
func TestDefaultResourceIndicatorConfig(t *testing.T) {
	config := DefaultResourceIndicatorConfig()

	if config == nil {
		t.Fatal("DefaultResourceIndicatorConfig() returned nil")
	}

	if !config.Enabled {
		t.Error("DefaultResourceIndicatorConfig().Enabled should be true")
	}

	if config.AllowedResources != nil {
		t.Error("DefaultResourceIndicatorConfig().AllowedResources should be nil")
	}

	if !config.RequireHTTPS {
		t.Error("DefaultResourceIndicatorConfig().RequireHTTPS should be true")
	}
}

// stringContains checks if a string contains a substring.
func stringContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
