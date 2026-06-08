// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withGDPRGrantState saves and restores the GDPR-grant globals.
func withGDPRGrantState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	origTenant := globalTenantID
	origUser := gdprUserID
	origPurpose := gdprPurpose
	origVersion := gdprVersion
	origFormat := iamOutputFormat
	t.Cleanup(func() {
		globalTenantID = origTenant
		gdprUserID = origUser
		gdprPurpose = origPurpose
		gdprVersion = origVersion
		iamOutputFormat = origFormat
		cleanup()
	})
}

func TestRunIAMGDPRConsentGrant_MissingTenant(t *testing.T) {
	withGDPRGrantState(t)
	globalTenantID = ""

	err := runIAMGDPRConsentGrantCommand(newIAMGDPRConsentGrantCommand(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMGDPRConsentGrant_InvalidPurpose(t *testing.T) {
	withGDPRGrantState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	_ = mock

	cmd := newIAMGDPRConsentGrantCommand()
	globalTenantID = "acme"
	gdprUserID = "user_1"
	gdprPurpose = "not-a-purpose"

	err := runIAMGDPRConsentGrantCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid purpose")
}

func TestRunIAMGDPRConsentGrant_JSONOutput(t *testing.T) {
	withGDPRGrantState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.gdprClient.GetConsentFunc = func(_ context.Context, tenantID, userID, purpose string) (*iam.Consent, error) {
		return &iam.Consent{ID: "c1", TenantID: tenantID, UserID: userID, Purpose: purpose}, nil
	}

	cmd := newIAMGDPRConsentGrantCommand()
	globalTenantID = "acme"
	gdprUserID = "user_1"
	gdprPurpose = "analytics"
	iamOutputFormat = "json"

	require.NoError(t, runIAMGDPRConsentGrantCommand(cmd, nil))
}

func TestRunIAMGDPRConsentGrant_GetConsentFallbackTable(t *testing.T) {
	withGDPRGrantState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	// GrantConsent succeeds, GetConsent fails -> fallback display path.
	mock.gdprClient.GetConsentFunc = func(_ context.Context, _, _, _ string) (*iam.Consent, error) {
		return nil, errors.New(errors.ErrNotFound, "iam", "consent not retrievable")
	}

	cmd := newIAMGDPRConsentGrantCommand()
	globalTenantID = "acme"
	gdprUserID = "user_1"
	gdprPurpose = "marketing"
	iamOutputFormat = "table"

	require.NoError(t, runIAMGDPRConsentGrantCommand(cmd, nil))
}

func TestRunIAMGDPRConsentGrant_GetConsentFallbackJSON(t *testing.T) {
	withGDPRGrantState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.gdprClient.GetConsentFunc = func(_ context.Context, _, _, _ string) (*iam.Consent, error) {
		return nil, errors.New(errors.ErrNotFound, "iam", "consent not retrievable")
	}

	cmd := newIAMGDPRConsentGrantCommand()
	globalTenantID = "acme"
	gdprUserID = "user_1"
	gdprPurpose = "functional"
	iamOutputFormat = "json"

	require.NoError(t, runIAMGDPRConsentGrantCommand(cmd, nil))
}

func TestRunIAMGDPRConsentGrant_GrantSDKError(t *testing.T) {
	withGDPRGrantState(t)
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.gdprClient.GrantConsentFunc = func(_ context.Context, _, _ string, _ *iam.GrantConsentRequest) error {
		return errors.New(errors.ErrInternal, "iam", "grant failed")
	}

	cmd := newIAMGDPRConsentGrantCommand()
	globalTenantID = "acme"
	gdprUserID = "user_1"
	gdprPurpose = "data_processing"

	require.Error(t, runIAMGDPRConsentGrantCommand(cmd, nil))
}

func TestRunIAMGDPRConsentGrant_FactoryError(t *testing.T) {
	withGDPRGrantState(t)
	orig := iamClientFactory
	t.Cleanup(func() { iamClientFactory = orig })
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}

	cmd := newIAMGDPRConsentGrantCommand()
	globalTenantID = "acme"
	gdprUserID = "user_1"
	gdprPurpose = "personalization"

	require.Error(t, runIAMGDPRConsentGrantCommand(cmd, nil))
}
