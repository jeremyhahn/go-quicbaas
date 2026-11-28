// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dekTenantPlaintextValue = "dek-it-plaintext"
	dekTenantAADValue       = "order-42"
	dekTenantWrongAADValue  = "order-43"
)

// dekTenantStatus is the subset of `dek tenant status --output json` the
// keyring test asserts on.
type dekTenantStatus struct {
	CurrentVersion uint64 `json:"current_version"`
	Versions       []struct {
		Version   uint64     `json:"version"`
		RetiredAt *time.Time `json:"retired_at"`
	} `json:"versions"`
}

// dekTenantSealed is the JSON view of `dek tenant encrypt` and `rewrap`.
type dekTenantSealed struct {
	Ciphertext  string `json:"ciphertext"`
	Version     uint64 `json:"version"`
	FromVersion uint64 `json:"from_version"`
	ToVersion   uint64 `json:"to_version"`
}

// dekTenantOpened is the JSON view of `dek tenant decrypt`. The CLI prints
// the plaintext as a string, not base64.
type dekTenantOpened struct {
	Plaintext string `json:"plaintext"`
	Version   uint64 `json:"version"`
}

// runDEKTenantJSON runs cmd, requires it to succeed and decodes its JSON
// stdout into out.
func runDEKTenantJSON(t *testing.T, runner *TestRunner, cmd CommandDef, out any) {
	t.Helper()
	stdout, stderr, err := runner.Run(cmd)
	require.NoError(t, err, "[%s] %s failed: stdout=%s stderr=%s", runner.transport, cmd.Name, stdout, stderr)
	require.NoError(t, json.Unmarshal([]byte(stdout), out),
		"[%s] %s: stdout is not the expected JSON: stdout=%s stderr=%s", runner.transport, cmd.Name, stdout, stderr)
}

// requireDEKTenantFails runs cmd as a negative case and requires it to fail.
func requireDEKTenantFails(t *testing.T, runner *TestRunner, cmd CommandDef, why string) {
	t.Helper()
	cmd.Name += " (" + why + ")"
	cmd.ExpectSuccess = false
	stdout, stderr, err := runner.Run(cmd)
	require.Error(t, err, "[%s] %s succeeded but must fail: stdout=%s stderr=%s", runner.transport, cmd.Name, stdout, stderr)
}

// TestDEKTenantKeyring drives the tenant DEK keyring through
// `quicbaas dek tenant` on every transport, as a fresh tenant's admin: the
// lifecycle (status, rotate, encrypt, decrypt, rewrap, retire), the AAD
// binding, the refusal to retire the current version, and that a ciphertext
// under a retired version is unreadable while its rewrapped copy opens.
func TestDEKTenantKeyring(t *testing.T) {
	RunForAllTransports(t, "TenantKeyring", func(t *testing.T, runner *TestRunner) {
		// The keyring is tenant data: act as a fresh tenant's admin, so the
		// keyring starts empty and version numbers are absolute.
		runner.ActAsTenantAdmin(UniqueKey("dek-tenant"))
		tr := runner.transport

		var status dekTenantStatus
		runDEKTenantJSON(t, runner, DEKTenantStatusCommand(), &status)
		require.Equal(t, uint64(0), status.CurrentVersion, "[%s] a fresh tenant has no keyring", tr)
		require.Empty(t, status.Versions, "[%s] a fresh tenant has no keyring versions", tr)

		status = dekTenantStatus{}
		runDEKTenantJSON(t, runner, DEKTenantRotateCommand(), &status)
		require.Equal(t, uint64(1), status.CurrentVersion, "[%s] the first rotate creates version 1", tr)

		var ct1 dekTenantSealed
		runDEKTenantJSON(t, runner, DEKTenantEncryptCommand(dekTenantPlaintextValue, dekTenantAADValue), &ct1)
		require.NotEmpty(t, ct1.Ciphertext, "[%s] encrypt returned no ciphertext", tr)
		require.Equal(t, uint64(1), ct1.Version, "[%s] encrypt seals under the current version", tr)

		var opened dekTenantOpened
		runDEKTenantJSON(t, runner, DEKTenantDecryptCommand(ct1.Ciphertext, dekTenantAADValue), &opened)
		assert.Equal(t, dekTenantPlaintextValue, opened.Plaintext, "[%s] decrypt of ct1", tr)
		assert.Equal(t, uint64(1), opened.Version, "[%s] decrypt of ct1 reports its version", tr)

		requireDEKTenantFails(t, runner, DEKTenantDecryptCommand(ct1.Ciphertext, dekTenantWrongAADValue), "wrong aad")

		status = dekTenantStatus{}
		runDEKTenantJSON(t, runner, DEKTenantRotateCommand(), &status)
		require.Equal(t, uint64(2), status.CurrentVersion, "[%s] the second rotate creates version 2", tr)

		var ct2 dekTenantSealed
		runDEKTenantJSON(t, runner, DEKTenantRewrapCommand(ct1.Ciphertext, dekTenantAADValue), &ct2)
		require.NotEmpty(t, ct2.Ciphertext, "[%s] rewrap returned no ciphertext", tr)
		assert.Equal(t, uint64(1), ct2.FromVersion, "[%s] rewrap from version", tr)
		assert.Equal(t, uint64(2), ct2.ToVersion, "[%s] rewrap to version", tr)
		require.NotEqual(t, ct1.Ciphertext, ct2.Ciphertext, "[%s] rewrap must return a new ciphertext", tr)

		opened = dekTenantOpened{}
		runDEKTenantJSON(t, runner, DEKTenantDecryptCommand(ct2.Ciphertext, dekTenantAADValue), &opened)
		assert.Equal(t, dekTenantPlaintextValue, opened.Plaintext, "[%s] decrypt of the rewrapped ct2", tr)
		assert.Equal(t, uint64(2), opened.Version, "[%s] decrypt of ct2 reports version 2", tr)

		requireDEKTenantFails(t, runner, DEKTenantRetireCommand(2), "current version")

		status = dekTenantStatus{}
		runDEKTenantJSON(t, runner, DEKTenantRetireCommand(1), &status)

		status = dekTenantStatus{}
		runDEKTenantJSON(t, runner, DEKTenantStatusCommand(), &status)
		require.Equal(t, uint64(2), status.CurrentVersion, "[%s] retire keeps the current version", tr)
		retired := map[uint64]bool{}
		for _, v := range status.Versions {
			retired[v.Version] = v.RetiredAt != nil
		}
		require.Contains(t, retired, uint64(1), "[%s] status lists version 1", tr)
		require.Contains(t, retired, uint64(2), "[%s] status lists version 2", tr)
		assert.True(t, retired[1], "[%s] version 1 is retired", tr)
		assert.False(t, retired[2], "[%s] version 2 is not retired", tr)

		// The proof that retirement makes data unreadable: ct1 was sealed
		// under version 1, whose key is now destroyed.
		requireDEKTenantFails(t, runner, DEKTenantDecryptCommand(ct1.Ciphertext, dekTenantAADValue), "retired version")

		opened = dekTenantOpened{}
		runDEKTenantJSON(t, runner, DEKTenantDecryptCommand(ct2.Ciphertext, dekTenantAADValue), &opened)
		assert.Equal(t, dekTenantPlaintextValue, opened.Plaintext, "[%s] the rewrapped ct2 still opens after retire", tr)
	})
}
