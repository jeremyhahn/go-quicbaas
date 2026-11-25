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

// init adds the IAM app, policy, provider, gdpr, mfa and role command groups
// to systemAdminCommands. Their writes take tenants:create on the REST routes,
// which go-qrdb registers ScopeCross (system tenant only), and by owner ruling
// a tenant admin must not hold it. So these groups run as the system admin
// even after ActAsTenantAdmin, like "iam tenant". The tenant is still
// provisioned and deleted by the tenant-admin session.
func init() {
	for _, group := range []string{
		"iam app",
		"iam policy",
		"iam provider",
		"iam gdpr",
		"iam mfa",
		"iam role",
	} {
		systemAdminCommands[group] = true
	}
}
