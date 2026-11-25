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

//go:build integration

package cli

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// nonExistentNumericUserID is a well-formed decimal user ID that no test
// creates, for the cases that want the server's not-found answer rather than
// its "not a decimal uint64" refusal.
const nonExistentNumericUserID = "999999999"

// createTenantUserID creates a user in tenantID through the CLI and returns the
// numeric user ID the server assigned. Since #389 a user ID is a uint64 the API
// reads from a decimal string, so a test that acts on a user must pass a real
// one. The user is removed with the tenant, which every caller deletes.
//
// On a transport the user-create command skips, it returns "" and the
// caller's own commands, which carry the same skip list, are skipped too.
func createTenantUserID(t *testing.T, runner *TestRunner, tenantID, prefix string) string {
	t.Helper()

	email := UniqueKey(prefix) + "@example.com"
	cmd := IAMUserCreateCommand(tenantID, email, prefix)
	cmd.Args = append(cmd.Args, "--output", "json")
	stdout, stderr, err := runner.Run(cmd)
	if runner.IsSkipped(cmd) {
		return ""
	}
	require.NoError(t, err, "user create should succeed: stdout=%s stderr=%s", stdout, stderr)

	var user map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &user), "user create JSON output should parse: %s", stdout)
	id, ok := user["id"].(string)
	require.True(t, ok, "user create output should carry a string id: %s", stdout)
	_, err = strconv.ParseUint(id, 10, 64)
	require.NoError(t, err, "user id %q should be a decimal uint64", id)
	return id
}

// createImpersonationTarget creates a tenant and a user in it as the runner's
// system admin, and returns both IDs. Impersonation start resolves the target
// user in the named tenant before it mints a session, so the target must be a
// real user; the runner stays the system admin, which holds user:impersonate.
// The tenant, and with it the user, is deleted when the test ends.
func createImpersonationTarget(t *testing.T, runner *TestRunner, prefix string) (tenantID, userID string) {
	t.Helper()

	tenantID = UniqueKey(prefix + "-tenant")
	runner.RunAndAssert(IAMTenantCreateCommand(tenantID, prefix))
	t.Cleanup(func() {
		runner.Run(IAMTenantDeleteCommand(tenantID))
	})
	// Tenants are isolated: the system admin may not act for this test tenant.
	if expectCrossTenantRefusal(t, runner, IAMUserCreateCommand(tenantID, UniqueKey(prefix+"-target")+"@example.com", prefix+"-target")) {
		return tenantID, ""
	}
	return tenantID, createTenantUserID(t, runner, tenantID, prefix+"-target")
}

// tenantAdminUsernamePrefix starts the username helpers.TenantAdmin gives the
// admin user it provisions in each test tenant ("tenant-admin-<tenant>").
const tenantAdminUsernamePrefix = "tenant-admin-"

// tenantAdminUserID returns the numeric user ID of the tenant admin the runner
// acts as after ActAsTenantAdmin(tenantID), read from `iam user list` by its
// username. The session commands name their user by this ID: the API refuses
// a username such as "admin" because it is not a decimal uint64.
//
// On a transport the user-list command skips, it returns "" and the caller's
// session commands, which carry the same skip list, are skipped too.
func tenantAdminUserID(t *testing.T, runner *TestRunner, tenantID string) string {
	t.Helper()

	cmd := CommandDef{
		Name:           "iam user list json tenant=" + tenantID,
		Args:           []string{"iam", "user", "list", "--tenant", tenantID, "--output", "json"},
		ExpectSuccess:  true,
		Timeout:        15 * time.Second,
		SkipTransports: iamTransportsToSkip,
	}
	stdout, stderr, err := runner.Run(cmd)
	if runner.IsSkipped(cmd) {
		return ""
	}
	require.NoError(t, err, "user list should succeed: stdout=%s stderr=%s", stdout, stderr)

	var users []map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(stdout), &users), "user list JSON output should parse: %s", stdout)
	for _, user := range users {
		username, ok := user["username"].(string)
		if !ok || !strings.HasPrefix(username, tenantAdminUsernamePrefix) {
			continue
		}
		id, ok := user["id"].(string)
		require.True(t, ok, "tenant admin %q should carry a string id: %v", username, user)
		_, err = strconv.ParseUint(id, 10, 64)
		require.NoError(t, err, "tenant admin id %q should be a decimal uint64", id)
		return id
	}
	require.Failf(t, "tenant admin not listed", "no user named %s* in tenant %s: %s",
		tenantAdminUsernamePrefix, tenantID, stdout)
	return ""
}
