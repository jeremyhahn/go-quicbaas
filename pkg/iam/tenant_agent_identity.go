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

package iam

import (
	"context"
	"crypto/x509"
	"strconv"
	"strings"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TenantAgentRoleName is the per-tenant role a Tenant Agent's certificate
// principal holds. Its ID is TenantAgentRoleID(tenant).
const TenantAgentRoleName = "tenant-agent"

// TenantAgentRoleID returns the ID of the Tenant Agent role in tenantID.
func TenantAgentRoleID(tenantID string) string {
	return AgentRoleID(TenantAgentRoleName, tenantID)
}

// TenantAgentGrant returns the complete permission set granted to a tenant's
// Tenant Agent, in qrdb's canonical "resource:action" vocabulary: the agent
// session floor (AgentSessionGrant) plus read access to its tenant's agent
// catalog, SDLC state and spend. Like AgentSessionGrant it is the floor AND
// the ceiling: every enrollment rewrites the role to exactly this set.
//
// It grants nothing else, and the absences are the point:
//
//   - No write, issue-create, bind or delete verb. The Tenant Agent reads its
//     tenant; it does not yet change it.
//   - No iam, secrets, kms, tenant, agent_accounts or barrier verb. It must not
//     administer the platform, read key material, mint another identity or
//     reach another tenant.
//
// Any write verb arrives in a later change, together with the behaviour that
// needs it and its own pinning test.
func TenantAgentGrant() []string {
	return append(AgentSessionGrant(),
		"agents:read",
		"sdlc:read",
		"spend_report:read",
	)
}

var tenantAgentRole = agentRole{
	name:        TenantAgentRoleName,
	description: "Tenant Agent: the tenant's standing coordinating principal; reads its own tenant only.",
}

// EnrollTenantAgentIdentity converges the go-qrdb certificate principal of
// tenantID's Tenant Agent: an mTLS-only user named by the certificate's
// subject CN, holding exactly the tenant's Tenant Agent role, which holds
// exactly TenantAgentGrant().
//
// It refuses (*NotATenantAgentIdentityError) and writes nothing when a record
// with that username already exists holding any other role or auth method,
// so it can never promote a session, fleet runtime or human principal.
func (s *IdentityStore) EnrollTenantAgentIdentity(ctx context.Context, tenantID string, cert *x509.Certificate) (*User, error) {
	if err := validateTenantID(tenantID); err != nil {
		return nil, err
	}
	if contextTenant(ctx) == "" {
		ctx = tenantctx.WithTenant(ctx, tenantID)
	}
	// A nil certificate or an empty subject falls through to the shared path,
	// which refuses it with the same error every certificate enrollment gives.
	if subject := certSubject(cert); subject != "" {
		existing, err := s.userStorage.GetUserByUsername(ctx, tenantID, subject)
		if err != nil && !dberrors.IsNotFound(err) {
			return nil, err
		}
		if err == nil && existing != nil {
			if err := assertIsTenantAgentIdentity(existing, TenantAgentRoleID(tenantID)); err != nil {
				return nil, err
			}
		}
	}
	return s.enrollFleetNodeCertIdentity(ctx, tenantID, tenantAgentRole, FleetNodeCertIdentityOptions{
		Certificate: cert,
		Permissions: TenantAgentGrant(),
	})
}

// certSubject returns cert's trimmed subject CN, or "" for a nil cert.
func certSubject(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	return strings.TrimSpace(cert.Subject.CommonName)
}

// RevokeTenantAgentIdentity deletes tenantID's Tenant Agent principal named
// username. It is idempotent on a missing record and refuses
// (*NotATenantAgentIdentityError) to delete a record that is not exactly a
// Tenant Agent identity.
func (s *IdentityStore) RevokeTenantAgentIdentity(ctx context.Context, tenantID, username string) error {
	if err := validateTenantID(tenantID); err != nil {
		return err
	}
	subject := strings.TrimSpace(username)
	if err := validateFleetNodeCertUsername(subject); err != nil {
		return err
	}
	if contextTenant(ctx) == "" {
		ctx = tenantctx.WithTenant(ctx, tenantID)
	}

	existing, err := s.userStorage.GetUserByUsername(ctx, tenantID, subject)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if existing == nil {
		return nil
	}
	if err := assertIsTenantAgentIdentity(existing, TenantAgentRoleID(tenantID)); err != nil {
		return err
	}
	if err := s.DeleteUser(ctx, tenantID, existing.ID); err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// assertIsTenantAgentIdentity returns nil only when existing holds exactly
// roleID and authenticates by mTLS only.
func assertIsTenantAgentIdentity(existing *User, roleID string) error {
	holdsOnlyTenantAgentRole := len(existing.Roles) == 1 && existing.Roles[0] == roleID
	restrictedToMTLS := len(existing.AuthMethods) == 1 && existing.AuthMethods[0] == AuthMethodMTLS
	if holdsOnlyTenantAgentRole && restrictedToMTLS {
		return nil
	}
	return &NotATenantAgentIdentityError{
		Username: existing.Username,
		TenantID: existing.TenantID,
		Roles:    append([]string(nil), existing.Roles...),
	}
}

// NotATenantAgentIdentityError reports a refusal to enroll over, or revoke, a
// record that is not exactly a Tenant Agent identity.
type NotATenantAgentIdentityError struct {
	// Username is the certificate subject the call named.
	Username string
	// TenantID is the tenant the record belongs to.
	TenantID string
	// Roles are the roles the record actually holds, for the operator's log.
	Roles []string
}

// Error implements error.
func (e *NotATenantAgentIdentityError) Error() string {
	return "iam: " + strconv.Quote(e.Username) + " in tenant " + strconv.Quote(e.TenantID) +
		" is not a Tenant Agent identity (roles: " + strings.Join(e.Roles, ",") +
		"); refusing to change it"
}
