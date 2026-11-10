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
// +build integration

package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// setupSessionSelf is setupUserForSessionTests for tests that call the
// caller-scoped session routes (/api/v1/iam/sessions...). It creates a tenant
// and a PIV user in it, both as the system admin, binds a fixture-CA client
// certificate to the user through the invite flow and logs the user in once.
// It returns the tenant, the user and a view of tc whose HTTP helpers send
// that user's own bearer. The user holds no role: the session routes act on
// the caller's own sessions. The caller deletes the tenant with
// cleanupSessionTests. The view holds no gRPC connection, so closing it is a
// no-op.
func setupSessionSelf(t *testing.T, tc *testCluster) (tenantID, userID string, self *testCluster) {
	t.Helper()

	tenantID = generateUniqueTenantID()
	require.NoError(t, tc.asSystemAdmin().postJSON("/api/v1/tenants", map[string]interface{}{
		"id":   tenantID,
		"name": "Session Test Tenant",
	}, nil))

	// One login, one session: every self-call reuses this bearer so the
	// test's own calls add no sessions of their own.
	user := enrollPIVUser(t, tc, tenantID, "session")
	return tenantID, user.id, pivUserView(t, tc, user)
}
