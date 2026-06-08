// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withProviderCreateState saves and restores all provider-create globals.
func withProviderCreateState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	saved := struct {
		factory                  IAMClientFactory
		tenant, preset, id, name string
		ptype, clientID, secret  string
		issuer, outputFmt        string
		scopes, roles, groups    []string
		auto, linking            bool
	}{
		factory:   iamClientFactory,
		tenant:    globalTenantID,
		preset:    providerPreset,
		id:        providerID,
		name:      providerName,
		ptype:     providerType,
		clientID:  providerClientID,
		secret:    providerSecret,
		issuer:    providerIssuerURL,
		outputFmt: iamOutputFormat,
		scopes:    providerScopes,
		roles:     defaultRoles,
		groups:    defaultGroups,
		auto:      autoProvision,
		linking:   allowLinking,
	}
	t.Cleanup(func() {
		iamClientFactory = saved.factory
		globalTenantID = saved.tenant
		providerPreset = saved.preset
		providerID = saved.id
		providerName = saved.name
		providerType = saved.ptype
		providerClientID = saved.clientID
		providerSecret = saved.secret
		providerIssuerURL = saved.issuer
		iamOutputFormat = saved.outputFmt
		providerScopes = saved.scopes
		defaultRoles = saved.roles
		defaultGroups = saved.groups
		autoProvision = saved.auto
		allowLinking = saved.linking
		cleanup()
	})
	// Start from a clean slate.
	providerPreset = ""
	providerID = ""
	providerName = ""
	providerType = ""
	providerClientID = ""
	providerSecret = ""
	providerIssuerURL = ""
	providerScopes = nil
	defaultRoles = nil
	defaultGroups = nil
	autoProvision = false
	allowLinking = false
}

func TestRunIAMProviderCreate_MissingTenant(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	globalTenantID = ""

	err := runIAMProviderCreate(cmd, "iam.provider.create")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMProviderCreate_MissingProviderAndID(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	globalTenantID = "acme"

	err := runIAMProviderCreate(cmd, "iam.provider.create")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "either --provider or --provider-id")
}

func TestRunIAMProviderCreate_MissingClientID(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "oidc"

	err := runIAMProviderCreate(cmd, "iam.provider.create")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--client-id is required")
}

func TestRunIAMProviderCreate_MissingIssuerURL(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "oidc"
	providerClientID = "client-123"

	err := runIAMProviderCreate(cmd, "iam.provider.create")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--issuer-url is required")
}

func TestRunIAMProviderCreate_SuccessDefaultOutput(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	var gotOpts *cli.CreateProviderOptions
	mock := newTestMockIAMClient()
	mock.CreateProviderFunc = func(_ context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error) {
		gotOpts = opts
		return &cli.Provider{
			ID:            opts.ProviderID,
			Name:          opts.Name,
			Type:          opts.Type,
			Enabled:       true,
			IssuerURL:     opts.IssuerURL,
			AutoProvision: opts.AutoProvision,
			AllowLinking:  opts.AllowLinking,
			DefaultRoles:  []string{"viewer"},
			DefaultGroups: []string{"staff"},
			Created:       time.Now(),
		}, nil
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "oidc"
	providerClientID = "client-123"
	providerIssuerURL = "https://issuer.example.com"
	defaultRoles = []string{"viewer"}
	defaultGroups = []string{"staff"}
	iamOutputFormat = "table"

	require.NoError(t, runIAMProviderCreate(cmd, "iam.provider.create"))
	require.NotNil(t, gotOpts)
	// Default scopes are applied when none are specified.
	assert.Equal(t, []string{"openid", "email", "profile"}, gotOpts.Scopes)
}

func TestRunIAMProviderCreate_SuccessJSONOutput(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	mock := newTestMockIAMClient()
	mock.CreateProviderFunc = func(_ context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error) {
		return &cli.Provider{ID: opts.ProviderID, Type: opts.Type, Created: time.Now()}, nil
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "oidc"
	providerClientID = "client-123"
	providerIssuerURL = "https://issuer.example.com"
	iamOutputFormat = "json"

	require.NoError(t, runIAMProviderCreate(cmd, "iam.provider.create"))
}

func TestRunIAMProviderCreate_DefaultsTypeToOIDC(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	mock := newTestMockIAMClient()
	mock.CreateProviderFunc = func(_ context.Context, opts *cli.CreateProviderOptions) (*cli.Provider, error) {
		assert.Equal(t, "oidc", opts.Type)
		return &cli.Provider{ID: opts.ProviderID, Type: opts.Type, Created: time.Now()}, nil
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "" // should default to oidc
	providerClientID = "client-123"
	providerIssuerURL = "https://issuer.example.com"
	iamOutputFormat = "table"

	require.NoError(t, runIAMProviderCreate(cmd, "iam.provider.create"))
}

func TestRunIAMProviderCreate_SDKError(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	mock := newTestMockIAMClient()
	mock.CreateProviderFunc = func(_ context.Context, _ *cli.CreateProviderOptions) (*cli.Provider, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "create failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "oidc"
	providerClientID = "client-123"
	providerIssuerURL = "https://issuer.example.com"

	require.Error(t, runIAMProviderCreate(cmd, "iam.provider.create"))
}

func TestRunIAMProviderCreate_FactoryError(t *testing.T) {
	withProviderCreateState(t)
	cmd := newIAMProviderCreateCommand()
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}

	globalTenantID = "acme"
	providerID = "my-oidc"
	providerType = "oidc"
	providerClientID = "client-123"
	providerIssuerURL = "https://issuer.example.com"

	require.Error(t, runIAMProviderCreate(cmd, "iam.provider.create"))
}
