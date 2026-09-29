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

// Package main provides tests for IAM federation CLI commands.
package main

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// errTestNoServer is a sentinel returned by the stub IAM client factory so the
// operator command exits after forcing the system tenant but before any network
// call.
var errTestNoServer = errors.New("no server in unit test")

// TestProviderPresets tests that all provider presets are properly defined.
func TestProviderPresets(t *testing.T) {
	tests := []struct {
		name           string
		preset         string
		requiresDomain bool
		expectedType   string
	}{
		{"Google", "google", false, "oidc"},
		{"Microsoft", "microsoft", true, "oidc"},
		{"Azure", "azure", true, "oidc"},
		{"Okta", "okta", true, "oidc"},
		{"Auth0", "auth0", true, "oidc"},
		{"GitHub", "github", false, "oauth2"},
		{"GitLab", "gitlab", false, "oidc"},
		{"Keycloak", "keycloak", true, "oidc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preset, ok := ProviderPresets[tt.preset]
			if !ok {
				t.Fatalf("preset %s not found", tt.preset)
			}

			if preset.RequiresDomain != tt.requiresDomain {
				t.Errorf("preset %s RequiresDomain = %v, want %v",
					tt.preset, preset.RequiresDomain, tt.requiresDomain)
			}

			if preset.Type != tt.expectedType {
				t.Errorf("preset %s Type = %s, want %s",
					tt.preset, preset.Type, tt.expectedType)
			}

			if preset.Name == "" {
				t.Errorf("preset %s has empty Name", tt.preset)
			}

			if preset.IssuerURL == "" {
				t.Errorf("preset %s has empty IssuerURL", tt.preset)
			}

			if len(preset.Scopes) == 0 {
				t.Errorf("preset %s has no Scopes", tt.preset)
			}

			if preset.RequiresDomain && preset.DomainHelp == "" {
				t.Errorf("preset %s requires domain but has no DomainHelp", tt.preset)
			}
		})
	}
}

// TestGetValidPresetNames tests that getValidPresetNames returns all presets sorted.
func TestGetValidPresetNames(t *testing.T) {
	names := getValidPresetNames()

	if len(names) != len(ProviderPresets) {
		t.Errorf("getValidPresetNames returned %d names, expected %d",
			len(names), len(ProviderPresets))
	}

	// Verify names are sorted
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("names not sorted: %s > %s", names[i-1], names[i])
		}
	}

	// Verify all preset names are included
	for key := range ProviderPresets {
		found := false
		for _, name := range names {
			if name == key {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("preset %s not found in getValidPresetNames output", key)
		}
	}
}

// TestListProviderPresets tests the ListProviderPresets function.
func TestListProviderPresets(t *testing.T) {
	presets := ListProviderPresets()

	if len(presets) != len(ProviderPresets) {
		t.Errorf("ListProviderPresets returned %d presets, expected %d",
			len(presets), len(ProviderPresets))
	}
}

// TestApplyProviderPreset tests the applyProviderPreset function.
func TestApplyProviderPreset(t *testing.T) {
	tests := []struct {
		name           string
		preset         string
		domain         string
		expectError    bool
		errorContains  string
		expectedID     string
		expectedIssuer string
	}{
		{
			name:           "Google preset without domain",
			preset:         "google",
			domain:         "",
			expectError:    false,
			expectedID:     "google",
			expectedIssuer: "https://accounts.google.com",
		},
		{
			name:           "Okta preset with domain",
			preset:         "okta",
			domain:         "acme",
			expectError:    false,
			expectedID:     "okta",
			expectedIssuer: "https://acme.okta.com",
		},
		{
			name:          "Okta preset without domain fails",
			preset:        "okta",
			domain:        "",
			expectError:   true,
			errorContains: "--domain is required",
		},
		{
			name:           "Azure preset with tenant ID",
			preset:         "azure",
			domain:         "tenant-guid",
			expectError:    false,
			expectedID:     "azure",
			expectedIssuer: "https://login.microsoftonline.com/tenant-guid/v2.0",
		},
		{
			name:           "Auth0 preset with domain",
			preset:         "auth0",
			domain:         "mycompany",
			expectError:    false,
			expectedID:     "auth0",
			expectedIssuer: "https://mycompany.auth0.com",
		},
		{
			name:          "Unknown preset fails",
			preset:        "unknown",
			domain:        "",
			expectError:   true,
			errorContains: "unknown provider preset",
		},
		{
			name:           "GitHub preset",
			preset:         "github",
			domain:         "",
			expectError:    false,
			expectedID:     "github",
			expectedIssuer: "https://github.com",
		},
		{
			name:           "GitLab preset",
			preset:         "gitlab",
			domain:         "",
			expectError:    false,
			expectedID:     "gitlab",
			expectedIssuer: "https://gitlab.com",
		},
		{
			name:           "Keycloak preset with domain",
			preset:         "keycloak",
			domain:         "keycloak.example.com",
			expectError:    false,
			expectedID:     "keycloak",
			expectedIssuer: "https://keycloak.example.com/realms/master",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset global variables before each test
			providerPreset = tt.preset
			providerDomain = tt.domain
			providerID = ""
			providerName = ""
			providerType = ""
			providerIssuerURL = ""
			providerScopes = nil

			// Create a mock command
			cmd := &cobra.Command{}
			cmd.Flags().String("type", "", "")
			cmd.Flags().String("issuer-url", "", "")
			cmd.Flags().StringSlice("scopes", nil, "")

			err := applyProviderPreset(cmd)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if providerID != tt.expectedID {
				t.Errorf("providerID = %q, want %q", providerID, tt.expectedID)
			}

			if providerIssuerURL != tt.expectedIssuer {
				t.Errorf("providerIssuerURL = %q, want %q", providerIssuerURL, tt.expectedIssuer)
			}

			if providerName == "" {
				t.Error("providerName should be set from preset")
			}

			if len(providerScopes) == 0 {
				t.Error("providerScopes should be set from preset")
			}
		})
	}
}

// TestApplyProviderPresetUserOverrides tests that user-specified values override presets.
func TestApplyProviderPresetUserOverrides(t *testing.T) {
	// Reset global variables
	providerPreset = "google"
	providerDomain = ""
	providerID = "custom-id"
	providerName = "Custom Name"
	providerType = ""
	providerIssuerURL = "https://custom.issuer.com"
	providerScopes = nil

	// Create a mock command where issuer-url is "changed" by user
	cmd := &cobra.Command{}
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("issuer-url", "", "")
	cmd.Flags().StringSlice("scopes", nil, "")
	// Set flags before calling applyProviderPreset to simulate user input
	_ = cmd.Flags().Set("issuer-url", "https://custom.issuer.com")

	err := applyProviderPreset(cmd)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// User-specified ID should be preserved (set before applyProviderPreset)
	if providerID != "custom-id" {
		t.Errorf("providerID = %q, want 'custom-id'", providerID)
	}

	// User-specified name should be preserved
	if providerName != "Custom Name" {
		t.Errorf("providerName = %q, want 'Custom Name'", providerName)
	}

	// Type should come from preset (not overridden by user)
	if providerType != "oidc" {
		t.Errorf("providerType = %q, want 'oidc'", providerType)
	}

	// User-specified issuer URL should be preserved (flag was "changed")
	if providerIssuerURL != "https://custom.issuer.com" {
		t.Errorf("providerIssuerURL = %q, want 'https://custom.issuer.com'", providerIssuerURL)
	}
}

// TestApplyProviderPresetCaseInsensitive tests that preset names are case-insensitive.
func TestApplyProviderPresetCaseInsensitive(t *testing.T) {
	testCases := []string{"GOOGLE", "Google", "GoOgLe", "google"}

	for _, preset := range testCases {
		t.Run(preset, func(t *testing.T) {
			providerPreset = preset
			providerDomain = ""
			providerID = ""
			providerName = ""
			providerType = ""
			providerIssuerURL = ""
			providerScopes = nil

			cmd := &cobra.Command{}
			cmd.Flags().String("type", "", "")
			cmd.Flags().String("issuer-url", "", "")
			cmd.Flags().StringSlice("scopes", nil, "")

			err := applyProviderPreset(cmd)
			if err != nil {
				t.Fatalf("preset %s failed: %v", preset, err)
			}

			if providerIssuerURL != "https://accounts.google.com" {
				t.Errorf("preset %s: providerIssuerURL = %q, want 'https://accounts.google.com'",
					preset, providerIssuerURL)
			}
		})
	}
}

// TestNewIAMProviderCommand tests that the provider command is properly structured.
func TestNewIAMProviderCommand(t *testing.T) {
	cmd := newIAMProviderCommand()

	if cmd.Use != "provider" {
		t.Errorf("cmd.Use = %q, want 'provider'", cmd.Use)
	}

	// Check subcommands exist
	expectedSubcommands := []string{"create", "get", "update", "delete", "list"}
	for _, name := range expectedSubcommands {
		found := false
		for _, subcmd := range cmd.Commands() {
			if subcmd.Use == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("subcommand %q not found", name)
		}
	}
}

// TestNewIAMProviderCreateCommand tests the create command flags.
func TestNewIAMProviderCreateCommand(t *testing.T) {
	cmd := newIAMProviderCreateCommand()

	if cmd.Use != "create" {
		t.Errorf("cmd.Use = %q, want 'create'", cmd.Use)
	}

	// Check key flags exist
	expectedFlags := []string{
		"provider", "domain", "provider-id", "name", "type",
		"client-id", "client-secret", "issuer-url", "scopes",
		"auto-provision", "allow-linking", "default-roles",
	}

	for _, flagName := range expectedFlags {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("flag --%s not found", flagName)
		}
	}
	// quicbaas keeps no groups; default-roles is the provisioning grant.
	if cmd.Flags().Lookup("default-groups") != nil {
		t.Error("flag --default-groups must not exist")
	}
}

// TestNewIAMProviderOperatorCommand tests the operator subcommand structure and
// flags, mirroring the create command convention.
func TestNewIAMProviderOperatorCommand(t *testing.T) {
	cmd := newIAMProviderOperatorCommand()

	if cmd.Use != "operator" {
		t.Errorf("cmd.Use = %q, want 'operator'", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Fatal("operator command must have a RunE")
	}

	// The operator command must expose the same registration flags as create.
	expectedFlags := []string{
		"provider", "domain", "provider-id", "name", "type",
		"client-id", "client-secret", "issuer-url", "scopes",
		"auto-provision", "allow-linking", "default-roles",
	}
	for _, flagName := range expectedFlags {
		if cmd.Flags().Lookup(flagName) == nil {
			t.Errorf("flag --%s not found on operator command", flagName)
		}
	}
}

// TestIAMProviderCommand_IncludesOperator proves the operator subcommand is wired
// into the provider command group.
func TestIAMProviderCommand_IncludesOperator(t *testing.T) {
	cmd := newIAMProviderCommand()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "operator" {
			found = true
			break
		}
	}
	if !found {
		t.Error("provider command group must include the 'operator' subcommand")
	}
}

// TestRunIAMProviderOperatorCommand_ForcesSystemTenant proves the operator
// command forces the reserved system tenant regardless of the global --tenant,
// so an operator IdP can never be registered against a data-plane tenant. The
// run fails (no live server) but only AFTER the tenant has been forced, which is
// what we assert.
func TestRunIAMProviderOperatorCommand_ForcesSystemTenant(t *testing.T) {
	// Simulate a stale/data-plane global tenant selection.
	prevTenant := globalTenantID
	prevPreset := providerPreset
	prevID := providerID
	prevClientID := providerClientID
	prevIssuer := providerIssuerURL
	prevType := providerType
	prevScopes := providerScopes
	defer func() {
		globalTenantID = prevTenant
		providerPreset = prevPreset
		providerID = prevID
		providerClientID = prevClientID
		providerIssuerURL = prevIssuer
		providerType = prevType
		providerScopes = prevScopes
	}()

	globalTenantID = "tenant-acme"
	providerPreset = ""
	providerID = "corp-idp"
	providerType = "oidc"
	providerClientID = "client-id"
	providerIssuerURL = "https://idp.example.com"
	providerScopes = []string{"openid"}

	// Route a real IAM-client construction failure so the run returns before any
	// network call but after the tenant override executed.
	iamClientFactory = func() (iam.Client, error) {
		return nil, errTestNoServer
	}
	defer resetIAMFactory()

	cmd := newIAMProviderOperatorCommand()
	err := runIAMProviderOperatorCommand(cmd, nil)
	if err == nil {
		t.Fatal("expected an error from the operator command without a live server")
	}
	if globalTenantID != operatorTenantID {
		t.Fatalf("operator command must force the system tenant, got %q", globalTenantID)
	}
}

// TestNewIAMIdentityCommand tests that the identity command is properly structured.
func TestNewIAMIdentityCommand(t *testing.T) {
	cmd := newIAMIdentityCommand()

	if cmd.Use != "identity" {
		t.Errorf("cmd.Use = %q, want 'identity'", cmd.Use)
	}

	// Check subcommands exist
	expectedSubcommands := []string{"link", "unlink", "list"}
	for _, name := range expectedSubcommands {
		found := false
		for _, subcmd := range cmd.Commands() {
			if subcmd.Use == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("subcommand %q not found", name)
		}
	}
}

// TestProviderPresetIssuerURLTemplates tests that domain templates are properly substituted.
func TestProviderPresetIssuerURLTemplates(t *testing.T) {
	tests := []struct {
		preset   string
		domain   string
		expected string
	}{
		{"okta", "company", "https://company.okta.com"},
		{"okta", "acme-corp", "https://acme-corp.okta.com"},
		{"auth0", "tenant", "https://tenant.auth0.com"},
		{"azure", "12345-guid", "https://login.microsoftonline.com/12345-guid/v2.0"},
		{"microsoft", "organizations", "https://login.microsoftonline.com/organizations/v2.0"},
		{"keycloak", "kc.example.com", "https://kc.example.com/realms/master"},
	}

	for _, tt := range tests {
		t.Run(tt.preset+"_"+tt.domain, func(t *testing.T) {
			providerPreset = tt.preset
			providerDomain = tt.domain
			providerID = ""
			providerName = ""
			providerType = ""
			providerIssuerURL = ""
			providerScopes = nil

			cmd := &cobra.Command{}
			cmd.Flags().String("type", "", "")
			cmd.Flags().String("issuer-url", "", "")
			cmd.Flags().StringSlice("scopes", nil, "")

			err := applyProviderPreset(cmd)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if providerIssuerURL != tt.expected {
				t.Errorf("providerIssuerURL = %q, want %q", providerIssuerURL, tt.expected)
			}
		})
	}
}

// TestProviderPresetScopes tests that each preset has appropriate scopes.
func TestProviderPresetScopes(t *testing.T) {
	// All OIDC providers should have 'openid' scope
	oidcProviders := []string{"google", "microsoft", "azure", "okta", "auth0", "gitlab", "keycloak"}
	for _, name := range oidcProviders {
		preset := ProviderPresets[name]
		hasOpenID := false
		for _, scope := range preset.Scopes {
			if scope == "openid" {
				hasOpenID = true
				break
			}
		}
		if !hasOpenID {
			t.Errorf("OIDC provider %s should have 'openid' scope", name)
		}
	}

	// GitHub should have 'read:user' scope
	github := ProviderPresets["github"]
	hasReadUser := false
	for _, scope := range github.Scopes {
		if scope == "read:user" {
			hasReadUser = true
			break
		}
	}
	if !hasReadUser {
		t.Error("GitHub preset should have 'read:user' scope")
	}

	// Okta should have 'groups' scope
	okta := ProviderPresets["okta"]
	hasGroups := false
	for _, scope := range okta.Scopes {
		if scope == "groups" {
			hasGroups = true
			break
		}
	}
	if !hasGroups {
		t.Error("Okta preset should have 'groups' scope")
	}
}

// containsString checks if s contains substr.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
