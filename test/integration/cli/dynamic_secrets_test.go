// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build integration

package cli

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The dynamic-secrets suite drives the credential generator end to end on
// every transport as the suite's system admin, whose tenant is the system
// tenant: its credentials are minted into the system credential service, so
// the existing `credential retrieve` verb reads them back from go-qrdb.
//
// Generated values are secrets: no assertion message or log line in this
// file names one, and every command whose stdout carries one sets
// RedactStdout.

// dynamicGenerateResponse mirrors the JSON tags of the SDK's
// GenerateCredentialResponse.
type dynamicGenerateResponse struct {
	Credential *struct {
		CredentialID string            `json:"credential_id"`
		Type         string            `json:"type"`
		Data         map[string]string `json:"data"`
		LeaseID      string            `json:"lease_id"`
	} `json:"credential"`
	LeaseID string `json:"lease_id"`
}

// generateDynamicCredential generates one credential from role and returns
// its lease ID, credential ID, value and credential:// reference.
func generateDynamicCredential(t *testing.T, runner *TestRunner, role string) (leaseID, credentialID, value, reference string) {
	t.Helper()
	stdout, _, err := runner.Run(DynamicSecretsGenerateJSONCommand(role))
	require.NoError(t, err, "[%s] dynamic-secrets generate %s failed", runner.transport, role)

	var resp dynamicGenerateResponse
	require.True(t, json.Unmarshal([]byte(stdout), &resp) == nil,
		"[%s] dynamic-secrets generate %s printed invalid JSON (%d bytes)", runner.transport, role, len(stdout))
	require.NotNil(t, resp.Credential, "[%s] dynamic-secrets generate %s returned no credential", runner.transport, role)

	leaseID = resp.LeaseID
	if leaseID == "" {
		leaseID = resp.Credential.LeaseID
	}
	return leaseID, resp.Credential.CredentialID, resp.Credential.Data["value"], resp.Credential.Data["reference"]
}

// cleanupDynamicSecrets revokes the leases and deletes the role, best effort.
func cleanupDynamicSecrets(runner *TestRunner, role string, leaseIDs ...string) {
	for _, id := range leaseIDs {
		if id != "" {
			_, _, _ = runner.Run(DynamicSecretsLeaseRevokeCommand(id))
		}
	}
	_, _, _ = runner.Run(DynamicSecretsRoleDeleteCommand(role))
}

func TestDynamicSecretsCredentialGenerate(t *testing.T) {
	RunForAllTransports(t, "DynamicSecretsCredentialGenerate", func(t *testing.T, runner *TestRunner) {
		role := UniqueKey("dsc-role")
		runner.RunAndAssert(DynamicSecretsRoleCreateCredentialCommand(role))

		lease1, id1, v1, ref1 := generateDynamicCredential(t, runner, role)
		lease2, id2, v2, ref2 := generateDynamicCredential(t, runner, role)
		defer cleanupDynamicSecrets(runner, role, lease1, lease2)

		for _, c := range []struct{ lease, id, value, ref string }{
			{lease1, id1, v1, ref1},
			{lease2, id2, v2, ref2},
		} {
			require.NotEmpty(t, c.id, "[%s] credential has no ID", runner.transport)
			require.NotEmpty(t, c.lease, "[%s] credential %s has no lease", runner.transport, c.id)
			require.Equal(t, "credential://dynamic-"+c.id, c.ref, "[%s] credential %s reference", runner.transport, c.id)
			raw, err := base64.RawURLEncoding.DecodeString(c.value)
			require.True(t, err == nil, "[%s] credential %s value is not unpadded base64url", runner.transport, c.id)
			require.True(t, len(raw) == 32, "[%s] credential %s value decodes to %d bytes, want 32", runner.transport, c.id, len(raw))
		}

		require.NotEqual(t, id1, id2, "[%s] two generations returned the same credential ID", runner.transport)
		require.True(t, v1 != v2, "[%s] two generations returned the same value (credentials %s, %s)", runner.transport, id1, id2)
	})
}

func TestDynamicSecretsCredentialHexConfig(t *testing.T) {
	RunForAllTransports(t, "DynamicSecretsCredentialHexConfig", func(t *testing.T, runner *TestRunner) {
		role := UniqueKey("dsc-hex")
		runner.RunAndAssert(DynamicSecretsRoleCreateCredentialCommand(role, "length=48", "encoding=hex"))

		lease, id, value, _ := generateDynamicCredential(t, runner, role)
		defer cleanupDynamicSecrets(runner, role, lease)

		require.True(t, len(value) == 96, "[%s] credential %s hex value is %d characters, want 96", runner.transport, id, len(value))
		_, err := hex.DecodeString(value)
		require.True(t, err == nil, "[%s] credential %s value is not hex", runner.transport, id)
	})
}

func TestDynamicSecretsCredentialLivesInStoreUntilRevoked(t *testing.T) {
	RunForAllTransports(t, "DynamicSecretsCredentialLivesInStoreUntilRevoked", func(t *testing.T, runner *TestRunner) {
		role := UniqueKey("dsc-store")
		runner.RunAndAssert(DynamicSecretsRoleCreateCredentialCommand(role))
		defer func() { _, _, _ = runner.Run(DynamicSecretsRoleDeleteCommand(role)) }()

		lease, id, value, _ := generateDynamicCredential(t, runner, role)
		require.NotEmpty(t, lease, "[%s] credential %s has no lease", runner.transport, id)
		name := "dynamic-" + id

		stdout, _, err := runner.Run(CredentialRetrieveRedactedCommand(name))
		require.NoError(t, err, "[%s] credential retrieve %s failed", runner.transport, name)
		require.True(t, strings.Contains(stdout, value), "[%s] credential store does not hold credential %s", runner.transport, id)

		leases, _, err := runner.Run(DynamicSecretsLeaseListCommand())
		require.NoError(t, err, "[%s] dynamic-secrets lease list failed", runner.transport)
		require.Contains(t, leases, lease, "[%s] lease list does not show lease %s", runner.transport, lease)

		runner.RunAndAssert(DynamicSecretsLeaseRevokeCommand(lease))

		// Revoke deletes the material; allow a bounded window for it.
		deadline := time.Now().Add(15 * time.Second)
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			_, _, err = runner.Run(CredentialRetrieveRedactedCommand(name))
			if err != nil || time.Now().After(deadline) {
				break
			}
			<-tick.C
		}
		require.Error(t, err, "[%s] credential %s is still in the store after its lease was revoked", runner.transport, id)
	})
}

func TestDynamicSecretsCredentialRoleRejectsBadConfig(t *testing.T) {
	RunForAllTransports(t, "DynamicSecretsCredentialRoleRejectsBadConfig", func(t *testing.T, runner *TestRunner) {
		for _, kv := range []string{"length=8", "colour=blue"} {
			role := UniqueKey("dsc-bad")
			cmd := DynamicSecretsRoleCreateCredentialCommand(role, kv)
			cmd.ExpectSuccess = false
			runner.RunAndAssert(cmd)
			// A role that was wrongly accepted must not outlive the test.
			_, _, _ = runner.Run(DynamicSecretsRoleDeleteCommand(role))
		}
	})
}
