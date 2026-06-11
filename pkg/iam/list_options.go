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

package iam

// Type aliases for list options to provide consistent naming across IAM operations.

// ListTenantsOptions defines options for listing tenants.
type ListTenantsOptions = ListOptions

// ListUsersOptions defines options for listing users.
type ListUsersOptions = ListOptions

// ListGroupsOptions defines options for listing groups.
type ListGroupsOptions = ListOptions

// ListRolesOptions defines options for listing roles.
type ListRolesOptions = ListOptions

// ListPoliciesOptions defines options for listing policies.
type ListPoliciesOptions = ListOptions

// ListAppsOptions defines options for listing OAuth applications.
type ListAppsOptions = ListOptions
