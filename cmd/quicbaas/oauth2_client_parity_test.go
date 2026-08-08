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

// Package main: coverage for the oauth2 client verbs that bring the CLI to
// parity with the REST OAuth2 application surface.
package main

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// resetOAuth2ClientFlags clears every oauth2 client flag and restores the
// previous values on cleanup.
func resetOAuth2ClientFlags(t *testing.T) {
	t.Helper()
	orig := struct {
		name           string
		authMethod     string
		grantTypes     []string
		responseTypes  []string
		redirectURIs   []string
		resourcePrefix string
		certPEM        string
		thumbprint     string
		scopes         []string
		force          bool
		output         string
		timeout        time.Duration
	}{
		oauth2ClientName, oauth2ClientAuthMethod, oauth2ClientGrantTypes,
		oauth2ClientResponseTypes, oauth2ClientRedirectURIs, oauth2ClientResourcePrefix,
		oauth2ClientCertPEM, oauth2ClientThumbprint, oauth2ClientScopes,
		iamForce, iamOutputFormat, globalTimeout,
	}

	t.Cleanup(func() {
		oauth2ClientName = orig.name
		oauth2ClientAuthMethod = orig.authMethod
		oauth2ClientGrantTypes = orig.grantTypes
		oauth2ClientResponseTypes = orig.responseTypes
		oauth2ClientRedirectURIs = orig.redirectURIs
		oauth2ClientResourcePrefix = orig.resourcePrefix
		oauth2ClientCertPEM = orig.certPEM
		oauth2ClientThumbprint = orig.thumbprint
		oauth2ClientScopes = orig.scopes
		iamForce = orig.force
		iamOutputFormat = orig.output
		globalTimeout = orig.timeout
	})

	oauth2ClientName = ""
	oauth2ClientAuthMethod = "client_secret_basic"
	oauth2ClientGrantTypes = nil
	oauth2ClientResponseTypes = nil
	oauth2ClientRedirectURIs = nil
	oauth2ClientResourcePrefix = ""
	oauth2ClientCertPEM = ""
	oauth2ClientThumbprint = ""
	oauth2ClientScopes = nil
	iamForce = true
	iamOutputFormat = "table"
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
}

// =============================================================================
// Command Tree / Flag Registration
// =============================================================================

// TestNewOAuth2Command_PersistentFlags verifies --output and --yes are
// reachable from the oauth2 tree. The verbs read iamOutputFormat / iamForce, so
// without these registrations `--output json` failed with "unknown flag" even
// though the help text advertised it.
func TestNewOAuth2Command_PersistentFlags(t *testing.T) {
	cmd := newOAuth2Command()

	require.NotNil(t, cmd.PersistentFlags().Lookup("output"), "oauth2 must accept --output")
	require.NotNil(t, cmd.PersistentFlags().Lookup("yes"), "oauth2 must accept --yes")

	// The flags must actually reach a leaf verb.
	cmd.SetArgs([]string{"client", "list", "--output", "json", "--help"})
	require.NoError(t, cmd.Execute())
}

// TestNewOAuth2ClientCommand_Tree verifies the full client lifecycle is wired.
func TestNewOAuth2ClientCommand_Tree(t *testing.T) {
	cmd := newOAuth2ClientCommand()

	got := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		got[sub.Name()] = true
	}
	for _, name := range []string{"register", "list", "show", "update", "rotate-secret", "delete", "pin-cert"} {
		assert.True(t, got[name], "missing oauth2 client subcommand %q", name)
	}
}

// TestNewOAuth2ClientRegisterCommand_Flags verifies the register verb exposes
// the fields the REST contract accepts, and is reachable as "create".
func TestNewOAuth2ClientRegisterCommand_Flags(t *testing.T) {
	cmd := newOAuth2ClientRegisterCommand()

	for _, name := range []string{
		"name", "grant-type", "redirect-uri", "response-type",
		"resource-prefix", "scope", "auth-method", "cert-pem", "cert-thumbprint",
	} {
		assert.NotNilf(t, cmd.Flags().Lookup(name), "register must expose --%s", name)
	}
	assert.Contains(t, cmd.Aliases, "create", "register must be reachable as create")
}

// =============================================================================
// Grant Type Parsing
// =============================================================================

func TestParseOAuth2GrantTypes_Valid(t *testing.T) {
	got, err := parseOAuth2GrantTypes([]string{"client_credentials", "REFRESH_TOKEN", " authorization_code "})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, sdkiam.GrantTypeClientCredentials, got[0])
	assert.Equal(t, sdkiam.GrantTypeRefreshToken, got[1])
	assert.Equal(t, sdkiam.GrantTypeAuthorizationCode, got[2])

	empty, err := parseOAuth2GrantTypes(nil)
	require.NoError(t, err)
	assert.Nil(t, empty)
}

// TestParseOAuth2GrantTypes_Invalid guards the regression that motivated strict
// parsing: the SDK's ParseGrantType silently maps unknown values to
// authorization_code, which would register a client with the wrong flow.
func TestParseOAuth2GrantTypes_Invalid(t *testing.T) {
	_, err := parseOAuth2GrantTypes([]string{"client_credentials", "magic_beans"})
	require.Error(t, err)

	var grantErr *UnsupportedGrantTypeError
	require.True(t, stderrors.As(err, &grantErr))
	assert.Contains(t, err.Error(), "magic_beans")
}

// =============================================================================
// Register Request Construction
// =============================================================================

// TestBuildRegisterRequest_FullMetadata verifies every flag reaches the SDK
// request.
func TestBuildRegisterRequest_FullMetadata(t *testing.T) {
	resetOAuth2ClientFlags(t)
	oauth2ClientName = "portal"
	oauth2ClientGrantTypes = []string{"authorization_code", "refresh_token"}
	oauth2ClientRedirectURIs = []string{"https://portal.acme.com/callback"}
	oauth2ClientResponseTypes = []string{"code"}
	oauth2ClientResourcePrefix = "orders://"
	oauth2ClientScopes = []string{"read", "write"}

	method, req, err := buildRegisterRequest(&cobra.Command{})
	require.NoError(t, err)

	assert.Equal(t, "client_secret_basic", method)
	assert.Equal(t, "portal", req.Name)
	assert.Equal(t, []sdkiam.GrantType{sdkiam.GrantTypeAuthorizationCode, sdkiam.GrantTypeRefreshToken}, req.GrantTypes)
	assert.Equal(t, []string{"https://portal.acme.com/callback"}, req.RedirectURIs)
	assert.Equal(t, []string{"code"}, req.ResponseTypes)
	assert.Equal(t, "orders://", req.ResourcePrefix)
	assert.Equal(t, []string{"read", "write"}, req.Scopes)
}

// TestBuildRegisterRequest_DefaultsGrantType verifies an omitted --grant-type
// yields a usable machine-to-machine client rather than one that can request
// nothing.
func TestBuildRegisterRequest_DefaultsGrantType(t *testing.T) {
	resetOAuth2ClientFlags(t)
	oauth2ClientName = "svc"

	_, req, err := buildRegisterRequest(&cobra.Command{})
	require.NoError(t, err)
	assert.Equal(t, []sdkiam.GrantType{sdkiam.GrantTypeClientCredentials}, req.GrantTypes)
}

// TestBuildRegisterRequest_InvalidGrantType verifies a bad grant type aborts
// before any request is built.
func TestBuildRegisterRequest_InvalidGrantType(t *testing.T) {
	resetOAuth2ClientFlags(t)
	oauth2ClientName = "svc"
	oauth2ClientGrantTypes = []string{"nonsense"}

	_, req, err := buildRegisterRequest(&cobra.Command{})
	require.Error(t, err)
	assert.Nil(t, req)
}

// =============================================================================
// Update
// =============================================================================

func TestBuildUpdateRequest_OmittedFieldsStayZero(t *testing.T) {
	resetOAuth2ClientFlags(t)
	oauth2ClientName = "portal-prod"

	req, err := buildUpdateRequest(&cobra.Command{})
	require.NoError(t, err)

	assert.Equal(t, "portal-prod", req.Name)
	assert.Nil(t, req.GrantTypes, "an omitted --grant-type must leave the stored value untouched")
	assert.Nil(t, req.RedirectURIs)
	assert.Empty(t, req.ResourcePrefix)
}

func TestBuildUpdateRequest_InvalidGrantType(t *testing.T) {
	resetOAuth2ClientFlags(t)
	oauth2ClientGrantTypes = []string{"not_a_grant"}

	_, err := buildUpdateRequest(&cobra.Command{})
	require.Error(t, err)
}

func TestRunOAuth2ClientUpdate_Happy(t *testing.T) {
	resetOAuth2ClientFlags(t)

	var captured *sdkiam.UpdateAppRequest
	mock := newTestMockIAMClient()
	mock.appClient.UpdateFunc = func(_ context.Context, tenantID, clientID string, req *sdkiam.UpdateAppRequest) (*sdkiam.Application, error) {
		captured = req
		return &sdkiam.Application{ClientID: clientID, TenantID: tenantID, Name: req.Name}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientUpdateCommand()
	oauth2ClientName = "portal-prod"
	oauth2ClientRedirectURIs = []string{"https://portal.acme.com/cb"}

	require.NoError(t, runOAuth2ClientUpdateCommand(cmd, []string{"app_abc123"}))

	require.NotNil(t, captured)
	assert.Equal(t, "portal-prod", captured.Name)
	assert.Equal(t, []string{"https://portal.acme.com/cb"}, captured.RedirectURIs)
}

// TestRunOAuth2ClientUpdate_MissingTenant verifies the tenant guard fires
// before a request is issued.
func TestRunOAuth2ClientUpdate_MissingTenant(t *testing.T) {
	resetOAuth2ClientFlags(t)

	mock := newTestMockIAMClient()
	mock.appClient.UpdateFunc = func(_ context.Context, _, _ string, _ *sdkiam.UpdateAppRequest) (*sdkiam.Application, error) {
		t.Fatal("update must not be attempted without a tenant")
		return nil, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "")

	cmd := newOAuth2ClientUpdateCommand()
	err := runOAuth2ClientUpdateCommand(cmd, []string{"app_abc123"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunOAuth2ClientUpdate_TransportError(t *testing.T) {
	resetOAuth2ClientFlags(t)

	mock := newTestMockIAMClient()
	mock.appClient.UpdateFunc = func(_ context.Context, _, _ string, _ *sdkiam.UpdateAppRequest) (*sdkiam.Application, error) {
		return nil, stderrors.New("update boom")
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientUpdateCommand()
	require.Error(t, runOAuth2ClientUpdateCommand(cmd, []string{"app_abc123"}))
}

// =============================================================================
// Rotate Secret
// =============================================================================

func TestRunOAuth2ClientRotateSecret_Happy(t *testing.T) {
	resetOAuth2ClientFlags(t)

	var capturedClientID string
	mock := newTestMockIAMClient()
	mock.appClient.RotateSecretFunc = func(_ context.Context, _, clientID string) (*sdkiam.RotateSecretResponse, error) {
		capturedClientID = clientID
		return &sdkiam.RotateSecretResponse{ClientID: clientID, ClientSecret: "new_secret"}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientRotateSecretCommand()
	require.NoError(t, runOAuth2ClientRotateSecretCommand(cmd, []string{"app_abc123"}))
	assert.Equal(t, "app_abc123", capturedClientID)
}

func TestRunOAuth2ClientRotateSecret_TransportError(t *testing.T) {
	resetOAuth2ClientFlags(t)

	mock := newTestMockIAMClient()
	mock.appClient.RotateSecretFunc = func(_ context.Context, _, _ string) (*sdkiam.RotateSecretResponse, error) {
		return nil, stderrors.New("rotate boom")
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	cmd := newOAuth2ClientRotateSecretCommand()
	require.Error(t, runOAuth2ClientRotateSecretCommand(cmd, []string{"app_abc123"}))
}

// =============================================================================
// Helpers
// =============================================================================

func TestGrantTypeNames(t *testing.T) {
	names := grantTypeNames([]sdkiam.GrantType{sdkiam.GrantTypeClientCredentials, sdkiam.GrantTypeRefreshToken})
	assert.Equal(t, []string{"client_credentials", "refresh_token"}, names)

	assert.Empty(t, grantTypeNames(nil))
}
