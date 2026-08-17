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

package server

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// qrdbPEPRBACAuthorizer implements iam.RBACAuthorizer (P6.1 of the unified
// login-plane plan) over the embedded qrdb node's RBACClient — the SAME
// single decision point every REST/gRPC/QUIC/MCP authz gate already
// consults (GetQRDBNodeClient().RBAC()) and the same store-resolved role
// grants finalizeIdentity enriches elsewhere in the stack. It is the RBAC
// half of the unified PDP: PolicyEvaluator consults it only when no ABAC
// policy explicitly allowed or denied the request.
//
// The qrdb node client is resolved per-call (not cached at construction),
// mirroring serverPermissionResolver: the node client is wired later in the
// server lifecycle than the IAM components that hold this authorizer, so a
// call before the node is ready fails closed rather than panicking on a nil
// client.
type qrdbPEPRBACAuthorizer struct {
	server *Server
}

// newQRDBPEPRBACAuthorizer builds the RBAC fallback authorizer PEPService /
// iam.PolicyEngine.Evaluate delegates to when no ABAC policy resolves a
// request.
func newQRDBPEPRBACAuthorizer(s *Server) *qrdbPEPRBACAuthorizer {
	return &qrdbPEPRBACAuthorizer{server: s}
}

// Compile-time interface check.
var _ iam.RBACAuthorizer = (*qrdbPEPRBACAuthorizer)(nil)

// QRDBNodeClientUnavailableError is returned when the embedded qrdb node's
// client is not yet available (server still composing). RBAC fails closed
// on it — never silently permits.
type QRDBNodeClientUnavailableError struct{}

// Error implements the error interface.
func (e *QRDBNodeClientUnavailableError) Error() string {
	return "pep: qrdb node client is not yet available (server still composing)"
}

// Authorize projects subject into the qrdb RBACClient's Identity contract
// and delegates the decision to it — store-resolved role grants, the same
// system-tenant cross-scope rule, and the same SO deny-list every other
// qrdb-fronted authorization path observes.
func (a *qrdbPEPRBACAuthorizer) Authorize(ctx context.Context, subject *iam.Subject, resource, action string) error {
	node := a.server.GetQRDBNodeClient()
	if node == nil {
		return &QRDBNodeClientUnavailableError{}
	}
	identity := subjectToQRDBIdentity(subject)
	return node.RBAC().Authorize(ctx, identity, resource, action)
}

// subjectToQRDBIdentity projects an iam.Subject onto the qrdb transport's
// Identity contract: ID -> Subject, TenantID (falling back to the legacy
// Tenant alias), Roles verbatim, and string-valued Attributes flattened
// into Metadata (qrdb Metadata is free-form attribute storage that does not
// influence the authorization decision itself, only audit/logging).
func subjectToQRDBIdentity(subject *iam.Subject) *qrdbtransport.Identity {
	if subject == nil {
		return &qrdbtransport.Identity{}
	}
	tenantID := subject.TenantID
	if tenantID == "" {
		tenantID = subject.Tenant
	}
	identity := &qrdbtransport.Identity{
		Subject:  subject.ID,
		TenantID: tenantID,
		Roles:    subject.Roles,
	}
	if len(subject.Attributes) > 0 {
		identity.Metadata = make(map[string]string, len(subject.Attributes))
		for k, v := range subject.Attributes {
			if s, ok := v.(string); ok {
				identity.Metadata[k] = s
			}
		}
	}
	return identity
}
