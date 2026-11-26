//go:build integration
// +build integration

package iam_test

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// fixtureRoleID is the role a fixture user is created holding when the test
// does not exercise a role of its own: user create refuses a user with no
// role. The tenant is fresh, so its Auditor role, the narrowest built-in one,
// carries the identifier go-qrdb seeds it under.
func fixtureRoleID(tenantID string) string {
	return qrdbsdk.SystemRoleID(tenantID, qrdbsdk.RoleNameAuditor)
}

// fixtureRoles is the roles field of a fixture user create body.
func fixtureRoles(tenantID string) []string {
	return []string{fixtureRoleID(tenantID)}
}
