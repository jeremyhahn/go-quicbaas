//go:build integration

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

package cli

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// inviteTransportsToSkip excludes MCP, which serves no invite tools pending
// an owner ruling, on top of the transports the IAM suite already skips.
var inviteTransportsToSkip = append([]Transport{TransportMCP}, iamTransportsToSkip...)

// TestIAMTenantProvisionCertMode provisions a tenant whose admin is enrolled
// from a CSR and checks the issued certificate is written where asked.
func TestIAMTenantProvisionCertMode(t *testing.T) {
	RunForTransports(t, "IAMTenantProvisionCertMode", inviteTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("provision-cert")
		dir := t.TempDir()
		csrPath := filepath.Join(dir, "admin.csr")
		certOut := filepath.Join(dir, "admin.crt")
		require.NoError(t, os.WriteFile(csrPath, newInviteTestCSR(t), 0o600), "write admin CSR")

		provisionCmd := CommandDef{
			Name: fmt.Sprintf("iam tenant provision %s cert", tenantID),
			Args: []string{
				"iam", "tenant", "provision", tenantID,
				"--isolation-type", "prefix",
				"--admin-csr", csrPath,
				"--admin-cert-out", certOut,
			},
			ExpectSuccess:  true,
			ExpectOutput:   []string{"Tenant provisioned successfully", tenantID},
			Timeout:        10 * time.Second,
			SkipTransports: inviteTransportsToSkip,
		}
		runner.RunAndAssert(provisionCmd)
		if runner.IsSkipped(provisionCmd) {
			return
		}
		defer deleteInviteTestTenant(runner, tenantID)

		certPEM, err := os.ReadFile(certOut)
		require.NoError(t, err, "the issued admin certificate must be written to --admin-cert-out")
		block, _ := pem.Decode(certPEM)
		require.NotNil(t, block, "the admin certificate file must hold PEM")
		_, err = x509.ParseCertificate(block.Bytes)
		require.NoError(t, err, "the admin certificate must parse")
	})
}

// TestIAMInviteShowAndActivatePIV provisions a tenant in invite mode, shows
// the admin's invite, refuses an untrusted certificate, activates with a
// trusted one, and checks the consumed invite is gone.
func TestIAMInviteShowAndActivatePIV(t *testing.T) {
	RunForTransports(t, "IAMInviteShowAndActivatePIV", inviteTransportsToSkip, func(t *testing.T, runner *TestRunner) {
		if !runner.RequireIAMAvailable() {
			return
		}

		tenantID := UniqueKey("provision-invite")
		provisionCmd := CommandDef{
			Name: fmt.Sprintf("iam tenant provision %s invite", tenantID),
			Args: []string{
				"iam", "tenant", "provision", tenantID,
				"--isolation-type", "prefix",
				"--admin-enrollment", "invite",
				"--admin-username", "alice",
				"--output", "json",
			},
			ExpectSuccess:  true,
			Timeout:        10 * time.Second,
			SkipTransports: inviteTransportsToSkip,
		}
		stdout, _, err := runner.Run(provisionCmd)
		if runner.IsSkipped(provisionCmd) {
			return
		}
		require.NoError(t, err, "provision in invite mode should succeed: %s", stdout)
		defer deleteInviteTestTenant(runner, tenantID)

		token := parseJSONField(t, stdout, "invite_token")
		require.NotEmpty(t, token, "invite mode must return an invite token")
		adminUserID := parseJSONField(t, stdout, "admin_user_id")
		require.NotEmpty(t, adminUserID, "provision must return the admin user ID")

		runner.RunAndAssert(inviteShowCommand(tenantID, token, true, adminUserID))

		dir := t.TempDir()
		untrustedPath := filepath.Join(dir, "untrusted.crt")
		require.NoError(t, os.WriteFile(untrustedPath, newUntrustedTestCert(t), 0o600), "write untrusted cert")
		runner.RunAndAssert(inviteActivatePIVCommand(tenantID, token, untrustedPath, false, ""))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		certPEM, _, err := helpers.IssueTenantClientCert(ctx, tenantID)
		require.NoError(t, err, "issue a trusted tenant client cert")
		trustedPath := filepath.Join(dir, "trusted.crt")
		require.NoError(t, os.WriteFile(trustedPath, certPEM, 0o600), "write trusted cert")
		runner.RunAndAssert(inviteActivatePIVCommand(tenantID, token, trustedPath, true, adminUserID))

		// The invite is one-time: once redeemed it can no longer be shown.
		runner.RunAndAssert(inviteShowCommand(tenantID, token, false, ""))
	})
}

// inviteShowCommand builds 'iam invite show' for tenantID and token.
func inviteShowCommand(tenantID, token string, expectSuccess bool, expectUserID string) CommandDef {
	cmd := CommandDef{
		Name:           fmt.Sprintf("iam invite show %s", tenantID),
		Args:           []string{"iam", "invite", "show", "--tenant", tenantID, "--token", token},
		ExpectSuccess:  expectSuccess,
		Timeout:        10 * time.Second,
		SkipTransports: inviteTransportsToSkip,
	}
	if expectUserID != "" {
		cmd.ExpectOutput = []string{"Invite:", expectUserID}
	}
	return cmd
}

// inviteActivatePIVCommand builds 'iam invite activate-piv' for tenantID,
// token and the certificate at certPath.
func inviteActivatePIVCommand(tenantID, token, certPath string, expectSuccess bool, expectUserID string) CommandDef {
	cmd := CommandDef{
		Name: fmt.Sprintf("iam invite activate-piv %s %s", tenantID, filepath.Base(certPath)),
		Args: []string{
			"iam", "invite", "activate-piv",
			"--tenant", tenantID, "--token", token, "--cert", certPath,
		},
		ExpectSuccess:  expectSuccess,
		Timeout:        10 * time.Second,
		SkipTransports: inviteTransportsToSkip,
	}
	if expectUserID != "" {
		cmd.ExpectOutput = []string{"Invite activated:", expectUserID}
	}
	return cmd
}

// deleteInviteTestTenant removes a tenant the invite tests provisioned.
func deleteInviteTestTenant(runner *TestRunner, tenantID string) {
	runner.Run(CommandDef{
		Name:           fmt.Sprintf("iam tenant delete %s cleanup", tenantID),
		Args:           []string{"iam", "tenant", "delete", tenantID, "--yes"},
		ExpectSuccess:  true,
		Timeout:        10 * time.Second,
		SkipTransports: inviteTransportsToSkip,
	})
}

// newInviteTestCSR returns a PEM CSR for a fresh P-256 admin key.
func newInviteTestCSR(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate admin key")
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "admin"},
	}, key)
	require.NoError(t, err, "create admin CSR")
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// newUntrustedTestCert returns a self-signed PEM certificate that chains to
// no root the server trusts.
func newUntrustedTestCert(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate untrusted key")
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "untrusted"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err, "self-sign untrusted cert")
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
