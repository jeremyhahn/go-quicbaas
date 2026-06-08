// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// writeSessionToken writes a JWT-style token containing the supplied claims to
// a temp HOME so getCurrentSessionID/getCurrentUserID can read it.
func writeSessionToken(t *testing.T, claims map[string]interface{}) {
	t.Helper()
	tmpDir := t.TempDir()
	quicbaasDir := filepath.Join(tmpDir, ".quicbaas")
	require.NoError(t, os.MkdirAll(quicbaasDir, 0700))

	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claimsJSON)

	tokenData := cli.TokenData{
		AccessToken:  "header." + payload + ".signature",
		RefreshToken: "refresh",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	data, err := json.Marshal(tokenData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(quicbaasDir, "token"), data, 0600))

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })
}

// withSessionRevokeAllState saves and restores the globals the revoke-all
// command depends on.
func withSessionRevokeAllState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	origFactory := iamClientFactory
	origTenant := globalTenantID
	origForce := iamForce
	origUserID := sessionUserID
	origKeep := sessionKeepCurrent
	t.Cleanup(func() {
		iamClientFactory = origFactory
		globalTenantID = origTenant
		iamForce = origForce
		sessionUserID = origUserID
		sessionKeepCurrent = origKeep
		cleanup()
	})
	iamForce = true
}

func TestRunIAMSessionRevokeAll_KeepCurrent(t *testing.T) {
	withSessionRevokeAllState(t)
	writeSessionToken(t, map[string]interface{}{
		"sub": "user-1",
		"sid": "sess-keep",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	var keptSession string
	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.RevokeOthersFunc = func(_ context.Context, keepSessionID string) error {
		keptSession = keepSessionID
		return nil
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "acme"

	cmd := newIAMSessionRevokeAllCommand()
	sessionKeepCurrent = true

	require.NoError(t, runIAMSessionRevokeAllCommand(cmd, nil))
	assert.Equal(t, "sess-keep", keptSession)
}

func TestRunIAMSessionRevokeAll_KeepCurrentNoSession(t *testing.T) {
	withSessionRevokeAllState(t)
	// Token with no session claim -> getCurrentSessionID returns an error,
	// which the command wraps as "failed to get current session ID".
	writeSessionToken(t, map[string]interface{}{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	mockClient := newTestMockIAMClient()
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "acme"

	cmd := newIAMSessionRevokeAllCommand()
	sessionKeepCurrent = true

	err := runIAMSessionRevokeAllCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get current session ID")
}

func TestRunIAMSessionRevokeAll_RevokeAllViaToken(t *testing.T) {
	withSessionRevokeAllState(t)
	writeSessionToken(t, map[string]interface{}{
		"sub": "user-token",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	var gotTenant, gotUser string
	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.RevokeAllFunc = func(_ context.Context, tenantID, userID string) error {
		gotTenant, gotUser = tenantID, userID
		return nil
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "acme"

	cmd := newIAMSessionRevokeAllCommand()
	sessionKeepCurrent = false
	sessionUserID = "" // force token lookup

	require.NoError(t, runIAMSessionRevokeAllCommand(cmd, nil))
	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "user-token", gotUser)
}

func TestRunIAMSessionRevokeAll_RevokeAllSDKError(t *testing.T) {
	withSessionRevokeAllState(t)
	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.RevokeAllFunc = func(_ context.Context, _, _ string) error {
		return errors.New(errors.ErrInternal, "iam", "revoke failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "acme"

	cmd := newIAMSessionRevokeAllCommand()
	sessionUserID = "explicit-user"

	require.Error(t, runIAMSessionRevokeAllCommand(cmd, nil))
}

func TestRunIAMSessionRevokeAll_FactoryError(t *testing.T) {
	withSessionRevokeAllState(t)
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}
	globalTenantID = "acme"

	cmd := newIAMSessionRevokeAllCommand()
	sessionUserID = "explicit-user"

	require.Error(t, runIAMSessionRevokeAllCommand(cmd, nil))
}

func TestRunIAMSessionRevokeAll_KeepCurrentSDKError(t *testing.T) {
	withSessionRevokeAllState(t)
	writeSessionToken(t, map[string]interface{}{
		"sub": "user-1",
		"sid": "sess-keep",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.RevokeOthersFunc = func(_ context.Context, _ string) error {
		return errors.New(errors.ErrInternal, "iam", "revoke others failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "acme"

	cmd := newIAMSessionRevokeAllCommand()
	sessionKeepCurrent = true

	require.Error(t, runIAMSessionRevokeAllCommand(cmd, nil))
}
