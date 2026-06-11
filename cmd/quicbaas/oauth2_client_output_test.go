// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	sdkiam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withIAMOutputFormat sets and restores the global IAM output format.
func withIAMOutputFormat(t *testing.T, format string) {
	t.Helper()
	orig := iamOutputFormat
	t.Cleanup(func() { iamOutputFormat = orig })
	iamOutputFormat = format
}

func TestOAuth2ClientList_JSONOutput(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.ListFunc = func(_ context.Context, tenantID string) (*sdkiam.ListAppsResponse, error) {
		return &sdkiam.ListAppsResponse{
			Applications: []sdkiam.Application{
				{ClientID: "app_1", Name: "svc", TenantID: tenantID, CreatedAt: time.Now()},
			},
			TotalCount: 1,
		}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")
	withIAMOutputFormat(t, "json")

	cmd := newOAuth2ClientListCommand()
	require.NoError(t, cmd.RunE(cmd, nil))
}

func TestOAuth2ClientList_EmptyTableOutput(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.ListFunc = func(_ context.Context, _ string) (*sdkiam.ListAppsResponse, error) {
		return &sdkiam.ListAppsResponse{Applications: nil, TotalCount: 0}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")
	withIAMOutputFormat(t, "table")

	cmd := newOAuth2ClientListCommand()
	require.NoError(t, cmd.RunE(cmd, nil))
}

func TestOAuth2ClientList_SDKError(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.ListFunc = func(_ context.Context, _ string) (*sdkiam.ListAppsResponse, error) {
		return nil, &testError{msg: "list failed"}
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")
	withIAMOutputFormat(t, "table")

	cmd := newOAuth2ClientListCommand()
	require.Error(t, cmd.RunE(cmd, nil))
}

func TestOAuth2ClientShow_JSONOutput(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.GetFunc = func(_ context.Context, tenantID, clientID string) (*sdkiam.Application, error) {
		return &sdkiam.Application{
			ClientID:           clientID,
			Name:               "svc",
			TenantID:           tenantID,
			MTLSCertThumbprint: "thumb",
			MTLSSubjectDN:      "CN=svc",
			MTLSSANDNS:         "svc.example.com",
			Scopes:             []string{"read", "write"},
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")
	withIAMOutputFormat(t, "json")

	cmd := newOAuth2ClientShowCommand()
	require.NoError(t, cmd.RunE(cmd, []string{"app_1"}))
}

func TestOAuth2ClientShow_TableWithAllFields(t *testing.T) {
	mock := newTestMockIAMClient()
	mock.appClient.GetFunc = func(_ context.Context, tenantID, clientID string) (*sdkiam.Application, error) {
		return &sdkiam.Application{
			ClientID:           clientID,
			Name:               "svc",
			TenantID:           tenantID,
			MTLSCertThumbprint: "thumb",
			MTLSSubjectDN:      "CN=svc",
			MTLSSANDNS:         "svc.example.com",
			Scopes:             []string{"read"},
			CreatedAt:          time.Now(),
			UpdatedAt:          time.Now(),
		}, nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")
	withIAMOutputFormat(t, "table")

	cmd := newOAuth2ClientShowCommand()
	require.NoError(t, cmd.RunE(cmd, []string{"app_1"}))
}

func TestOAuth2ClientShow_FactoryError(t *testing.T) {
	orig := iamClientFactory
	t.Cleanup(func() { iamClientFactory = orig })
	iamClientFactory = func() (sdkiam.Client, error) { return nil, &testError{msg: "factory boom"} }
	withTenant(t, "acme")

	cmd := newOAuth2ClientShowCommand()
	require.Error(t, cmd.RunE(cmd, []string{"app_1"}))
}

func TestOAuth2ClientList_FactoryError(t *testing.T) {
	orig := iamClientFactory
	t.Cleanup(func() { iamClientFactory = orig })
	iamClientFactory = func() (sdkiam.Client, error) { return nil, &testError{msg: "factory boom"} }
	withTenant(t, "acme")

	cmd := newOAuth2ClientListCommand()
	require.Error(t, cmd.RunE(cmd, nil))
}

func TestOAuth2ClientRegister_JSONOutput(t *testing.T) {
	mock := newTestMockIAMClient()
	withMockIAM(t, mock)
	withTenant(t, "acme")
	withIAMOutputFormat(t, "json")
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "client_secret_basic"

	cmd := newOAuth2ClientRegisterCommand()
	require.NoError(t, cmd.RunE(cmd, nil))
}

func TestOAuth2ClientRegister_FactoryError(t *testing.T) {
	orig := iamClientFactory
	t.Cleanup(func() { iamClientFactory = orig })
	iamClientFactory = func() (sdkiam.Client, error) { return nil, &testError{msg: "factory boom"} }
	withTenant(t, "acme")
	resetOAuth2RegisterFlags(t)
	oauth2ClientAuthMethod = "client_secret_basic"

	cmd := newOAuth2ClientRegisterCommand()
	require.Error(t, cmd.RunE(cmd, nil))
}

func TestOAuth2ClientDelete_DeclinedConfirmation(t *testing.T) {
	mock := newTestMockIAMClient()
	deleteCalled := false
	mock.appClient.DeleteFunc = func(_ context.Context, _, _ string) error {
		deleteCalled = true
		return nil
	}
	withMockIAM(t, mock)
	withTenant(t, "acme")

	origForce := iamForce
	iamForce = false
	t.Cleanup(func() { iamForce = origForce })

	// With stdin unavailable, fmt.Scanln reads nothing -> response stays "",
	// which is treated as a "no" and cancels the delete without an error.
	cmd := newOAuth2ClientDeleteCommand()
	require.NoError(t, cmd.RunE(cmd, []string{"app_1"}))
	require.False(t, deleteCalled, "delete must not run when confirmation is declined")
}
