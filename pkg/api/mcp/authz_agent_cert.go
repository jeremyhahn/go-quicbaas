//go:build mcp

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

package mcp

import (
	"context"
	"crypto/x509"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// mTLS agent identity for the MCP transport.
//
// Spawned and SSH-deployed agents hold a long-lived (365-day) mTLS client
// certificate minted by the platform CA with the shape
//
//	CN=agent-<name>, O=<tenantID>, OU=agents
//
// (pkg/service/agent_cert_issuer.go). When such an agent connects to the MCP
// endpoint it presents that certificate instead of a bearer token: a generic
// ACP agent (claude / codex / opencode) has no way to refresh a 15-minute
// platform token, so a token-based identity would strand long-lived agents
// the moment the refresh token lapses. The certificate identity has no such
// mid-session expiry; renewal is a yearly cert-rotation concern handled by
// the issuing infrastructure, invisible to the agent.
//
// Trust model — fail closed at every step:
//
//  1. The certificate reaches this code ONLY through
//     service.VerifiedPeerCertFromContext, which the MCP HTTP listener
//     populates exclusively from r.TLS.VerifiedChains (pkg/api/mcp/server.go).
//     A chain is verified iff the operator configured a client CA and the
//     peer's certificate chains to it. No client CA configured -> no verified
//     chain -> no certificate identity: the path is implicitly gated on the
//     operator's mTLS posture.
//  2. OU=agents is the platform-wide agent-certificate sentinel — the same
//     belt-and-braces marker the control plane's agent register handler
//     requires (service.DefaultAgentCertOU). A verified certificate WITHOUT
//     that OU is not an agent identity and falls through to the bearer-token
//     requirement; it is never granted agent scopes.
//  3. The tenant comes from the certificate's O= field, stamped by the agent
//     cert issuer from the registering tenant. An agent certificate with no
//     tenant is malformed and is DENIED rather than admitted tenant-less.
//  4. The granted scope set is the fixed least-privilege
//     orchestration.AgentMCPScopes() — inference plus tenant-scoped KV — and
//     the requested tool's qrdb-canonical permission is enforced against it
//     with the same wildcard rules as token scp claims.
//
// A presented bearer token always takes precedence over the certificate:
// operators (and token-holding services) connecting over the same mTLS
// listener are authorized by the unified platform-token validator, never by
// this path.

// agentCertAuthorizedContext derives an AuthorizedContext from a verified
// mTLS agent certificate in ctx.
//
// Returns (nil, nil) when no certificate identity applies (no verified peer
// cert, or the cert is not an agent cert) — the caller falls through to the
// bearer-token requirement. Returns (nil, err) when an agent certificate is
// present but malformed: that is a hard denial, never a fall-through.
func agentCertAuthorizedContext(ctx context.Context) (*platformtoken.AuthorizedContext, *AuthorizationError) {
	cert := service.VerifiedPeerCertFromContext(ctx)
	if cert == nil {
		return nil, nil
	}
	if !isAgentCert(cert) {
		// A verified client certificate that is not an agent cert (e.g. an
		// operator's PIV cert used as a login factor) carries no MCP identity
		// of its own — the caller still needs a bearer token.
		return nil, nil
	}

	tenant := firstNonEmptyTrimmed(cert.Subject.Organization)
	if tenant == "" {
		// OU says "agent" but there is no tenant to scope it to. Admitting it
		// tenant-less would bypass row-level security; deny outright.
		return nil, &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: agent certificate has no tenant (O=) attribute",
		}
	}

	cn := strings.TrimSpace(cert.Subject.CommonName)
	if cn == "" {
		return nil, &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: agent certificate has no common name",
		}
	}

	return &platformtoken.AuthorizedContext{
		Subject:  cn,
		Username: cn,
		TenantID: tenant,
		Scopes:   orchestration.AgentMCPScopes(),
		Roles:    []string{orchestration.AgentRuntimeRole},
	}, nil
}

// authorizeAgentCertContext enforces the requested tool's qrdb-canonical
// permission against a certificate-derived agent context. It mirrors the
// default-deny posture of the bearer path: an unmapped tool is denied (there
// is no registered permission to admit it), and a mapped tool outside the
// agent's bounded scope set is denied with the same permission-denied shape a
// token scp miss produces.
func (ac *AuthorizationChecker) authorizeAgentCertContext(agentCtx *platformtoken.AuthorizedContext, toolName string) (*platformtoken.AuthorizedContext, *AuthorizationError) {
	resource, action, ok := getToolQRDBPermission(toolName)
	if !ok {
		return nil, &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: no qrdb permission mapping for tool " + toolName,
		}
	}
	requiredPerm := resource + ":" + action

	if err := agentCtx.RequireScope(requiredPerm); err != nil {
		return nil, &AuthorizationError{
			Type:    AuthErrorPermissionDenied,
			Message: "permission denied: agent certificate identity lacks " + requiredPerm,
		}
	}
	return agentCtx, nil
}

// isAgentCert reports whether the verified certificate carries the platform's
// agent-certificate OU sentinel (service.DefaultAgentCertOU). The comparison
// is exact per OU value — an OU merely containing the word is not a match.
func isAgentCert(cert *x509.Certificate) bool {
	for _, ou := range cert.Subject.OrganizationalUnit {
		if strings.TrimSpace(ou) == service.DefaultAgentCertOU {
			return true
		}
	}
	return false
}

// firstNonEmptyTrimmed returns the first non-empty value after trimming, or "".
func firstNonEmptyTrimmed(values []string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
