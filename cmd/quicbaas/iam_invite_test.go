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

package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withInviteState installs the IAM mock, sets the invite scope and returns
// the mock and a buffer that captures the command's output.
func withInviteState(t *testing.T) (*testMockIAMClient, *bytes.Buffer) {
	t.Helper()
	logCleanup := iamTenantUserTestInitLogger(t)
	mockClient, mockCleanup := setupIAMMock(t)
	origFormat, origTenant := iamOutputFormat, globalTenantID
	t.Cleanup(func() {
		iamOutputFormat, globalTenantID = origFormat, origTenant
		inviteToken, inviteCertPath = "", ""
		mockCleanup()
		logCleanup()
	})
	iamOutputFormat = "table"
	globalTenantID = "acme"
	inviteToken = "tok-123"
	inviteCertPath = ""
	return mockClient, &bytes.Buffer{}
}

// newInviteTestCommand returns a command whose output goes to out.
func newInviteTestCommand(out *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(out)
	return cmd
}

// writeTestPIVCert writes a freshly generated self-signed certificate as PEM
// and returns its path and contents.
func writeTestPIVCert(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "piv-user"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return writeTestFile(t, "piv.crt", certPEM), certPEM
}

func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// assertUsageError asserts err is the CLI's typed usage error carrying msg.
func assertUsageError(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err)
	assertInvalidInput(t, err)
	assert.Contains(t, err.Error(), msg)
}

func TestNewIAMInviteCommand_Structure(t *testing.T) {
	cmd := newIAMInviteCommand()
	assert.Equal(t, "invite", cmd.Use)
	require.NotNil(t, cmd.PersistentFlags().Lookup("token"))
	assert.Nil(t, cmd.PersistentFlags().Lookup("tenant"), "the invite verbs use the root --tenant")

	names := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.ElementsMatch(t, []string{"show", "activate-piv"}, names)

	activate := newIAMInviteActivatePIVCommand()
	assert.NotNil(t, activate.Flags().Lookup("cert"))
}

func TestIAMInviteShowCommand_PrintsMetadata(t *testing.T) {
	mockClient, out := withInviteState(t)
	expires := time.Date(2026, 10, 20, 9, 30, 0, 0, time.UTC)
	var gotTenant, gotToken string
	mockClient.inviteClient.GetInviteFunc = func(_ context.Context, tenantID, token string) (*iam.InviteMetadata, error) {
		gotTenant, gotToken = tenantID, token
		return &iam.InviteMetadata{
			UserID:      "user_42",
			Email:       "alice@acme.test",
			Name:        "Alice",
			AuthMethods: []string{"fido2", "piv"},
			ExpiresAt:   expires,
		}, nil
	}

	require.NoError(t, runIAMInviteShowCommand(newInviteTestCommand(out), nil))

	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "tok-123", gotToken)
	assert.Contains(t, out.String(), "User ID: user_42")
	assert.Contains(t, out.String(), "Email: alice@acme.test")
	assert.Contains(t, out.String(), "Name: Alice")
	assert.Contains(t, out.String(), "Auth Methods: fido2, piv")
	assert.Contains(t, out.String(), "Expires: 2026-10-20 09:30:00")
	assert.NotContains(t, out.String(), "tok-123", "the token must not be echoed")
}

func TestIAMInviteShowCommand_JSONOutput(t *testing.T) {
	mockClient, out := withInviteState(t)
	iamOutputFormat = "json"
	mockClient.inviteClient.GetInviteFunc = func(_ context.Context, _, _ string) (*iam.InviteMetadata, error) {
		return &iam.InviteMetadata{UserID: "user_42", AuthMethods: []string{"piv"}}, nil
	}

	require.NoError(t, runIAMInviteShowCommand(newInviteTestCommand(out), nil))
	assert.Contains(t, out.String(), `"user_id": "user_42"`)
	assert.Contains(t, out.String(), `"auth_methods": [`)
}

func TestIAMInviteShowCommand_MissingScope(t *testing.T) {
	tests := []struct {
		name    string
		tenant  string
		token   string
		wantMsg string
	}{
		{"missing tenant", "", "tok-123", "--tenant is required"},
		{"missing token", "acme", "", "--token is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient, out := withInviteState(t)
			globalTenantID, inviteToken = tt.tenant, tt.token
			called := false
			mockClient.inviteClient.GetInviteFunc = func(_ context.Context, _, _ string) (*iam.InviteMetadata, error) {
				called = true
				return nil, nil
			}

			err := runIAMInviteShowCommand(newInviteTestCommand(out), nil)
			assertUsageError(t, err, tt.wantMsg)
			assert.False(t, called, "a missing flag must not reach the server")
		})
	}
}

func TestIAMInviteShowCommand_InvalidOutputFormat(t *testing.T) {
	_, out := withInviteState(t)
	iamOutputFormat = "xml"

	require.Error(t, runIAMInviteShowCommand(newInviteTestCommand(out), nil))
	assert.Empty(t, out.String())
}

func TestIAMInviteShowCommand_SDKError(t *testing.T) {
	mockClient, out := withInviteState(t)
	mockClient.inviteClient.GetInviteFunc = func(_ context.Context, _, _ string) (*iam.InviteMetadata, error) {
		return nil, errors.New(errors.ErrNotFound, "iam.invite", "invite not found")
	}

	err := runIAMInviteShowCommand(newInviteTestCommand(out), nil)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrNotFound, qbErr.Code)
	assert.Contains(t, err.Error(), "invite not found")
}

func TestIAMInviteShowCommand_FactoryError(t *testing.T) {
	_, out := withInviteState(t)
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "no connection")
	}

	err := runIAMInviteShowCommand(newInviteTestCommand(out), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no connection")
}

func TestIAMInviteActivatePIVCommand_SendsCertAndPrintsResult(t *testing.T) {
	mockClient, out := withInviteState(t)
	path, certPEM := writeTestPIVCert(t)
	inviteCertPath = path
	var gotTenant, gotToken, gotCert string
	mockClient.inviteClient.ActivatePIVFunc = func(_ context.Context, tenantID, token, cert string) (*iam.ActivationResponse, error) {
		gotTenant, gotToken, gotCert = tenantID, token, cert
		return &iam.ActivationResponse{UserID: "user_42", Message: "user activated", Warning: "activation write retried"}, nil
	}

	require.NoError(t, runIAMInviteActivatePIVCommand(newInviteTestCommand(out), nil))

	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "tok-123", gotToken)
	assert.Equal(t, certPEM, gotCert)
	assert.Contains(t, out.String(), "Invite activated:")
	assert.Contains(t, out.String(), "User ID: user_42")
	assert.Contains(t, out.String(), "Message: user activated")
	assert.Contains(t, out.String(), "Warning: activation write retried")
}

func TestIAMInviteActivatePIVCommand_JSONOutput(t *testing.T) {
	mockClient, out := withInviteState(t)
	iamOutputFormat = "json"
	inviteCertPath, _ = writeTestPIVCert(t)
	mockClient.inviteClient.ActivatePIVFunc = func(_ context.Context, _, _, _ string) (*iam.ActivationResponse, error) {
		return &iam.ActivationResponse{UserID: "user_42", Message: "user activated"}, nil
	}

	require.NoError(t, runIAMInviteActivatePIVCommand(newInviteTestCommand(out), nil))
	assert.Contains(t, out.String(), `"user_id": "user_42"`)
	assert.NotContains(t, out.String(), `"warning"`)
}

func TestIAMInviteActivatePIVCommand_BadFlagsAndCertificates(t *testing.T) {
	notACert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte("csr")})
	garbled := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not der")})
	tests := []struct {
		name    string
		tenant  string
		token   string
		cert    func(t *testing.T) string
		wantMsg string
	}{
		{"missing tenant", "", "tok-123", func(t *testing.T) string { p, _ := writeTestPIVCert(t); return p }, "--tenant is required"},
		{"missing token", "acme", "", func(t *testing.T) string { p, _ := writeTestPIVCert(t); return p }, "--token is required"},
		{"missing cert", "acme", "tok-123", func(*testing.T) string { return "" }, "--cert is required"},
		{"unreadable cert", "acme", "tok-123", func(t *testing.T) string { return filepath.Join(t.TempDir(), "absent.crt") }, "failed to read certificate file"},
		{"not PEM", "acme", "tok-123", func(t *testing.T) string { return writeTestFile(t, "piv.crt", "plain text") }, "does not hold a PEM CERTIFICATE block"},
		{"wrong PEM type", "acme", "tok-123", func(t *testing.T) string { return writeTestFile(t, "piv.crt", string(notACert)) }, "does not hold a PEM CERTIFICATE block"},
		{"invalid DER", "acme", "tok-123", func(t *testing.T) string { return writeTestFile(t, "piv.crt", string(garbled)) }, "does not hold a valid X.509 certificate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient, out := withInviteState(t)
			globalTenantID, inviteToken, inviteCertPath = tt.tenant, tt.token, tt.cert(t)
			called := false
			mockClient.inviteClient.ActivatePIVFunc = func(_ context.Context, _, _, _ string) (*iam.ActivationResponse, error) {
				called = true
				return nil, nil
			}

			err := runIAMInviteActivatePIVCommand(newInviteTestCommand(out), nil)
			assertUsageError(t, err, tt.wantMsg)
			assert.False(t, called, "a bad flag or certificate must not reach the server")
		})
	}
}

func TestIAMInviteActivatePIVCommand_SDKError(t *testing.T) {
	mockClient, out := withInviteState(t)
	inviteCertPath, _ = writeTestPIVCert(t)
	mockClient.inviteClient.ActivatePIVFunc = func(_ context.Context, _, _, _ string) (*iam.ActivationResponse, error) {
		return nil, errors.New(errors.ErrUnauthorized, "iam.invite", "certificate chain is not trusted")
	}

	err := runIAMInviteActivatePIVCommand(newInviteTestCommand(out), nil)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrUnauthorized, qbErr.Code)
	assert.Contains(t, err.Error(), "certificate chain is not trusted")
	assert.Empty(t, out.String())
}
