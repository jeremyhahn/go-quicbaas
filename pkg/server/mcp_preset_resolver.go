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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Env var names the spawned agent reads to locate its per-session mTLS material
// for the QuicBaaS MCP endpoint. They are carried inside each MCP server's Env
// map (round-tripped through QUICBAAS_MCP_SERVERS). The MCP transport
// authenticates the agent by the client certificate at QUICBAAS_MCP_CERT /
// QUICBAAS_MCP_KEY, verified against the CA at QUICBAAS_MCP_CA.
const (
	mcpCertEnvVar = orchestration.MCPCertEnvVar
	mcpKeyEnvVar  = orchestration.MCPKeyEnvVar
	mcpCAEnvVar   = orchestration.MCPCAEnvVar
)

// ephemeralAgentCertValidityDays bounds the lifetime of a per-spawn agent
// certificate. The certificate exists only for the duration of the session and
// is deleted on Close; one day is the smallest meaningful validity the issuer
// accepts and is far longer than any single session needs while leaving the
// agent's clock skew tolerance intact.
const ephemeralAgentCertValidityDays = 1

// AgentMCPPresetResolver implements orchestration.MCPPresetResolver by minting
// a fresh, short-lived mTLS agent certificate per spawn and writing it to a
// private per-session directory. The certificate carries OU=agents and
// O=<tenant> so the MCP transport's agent-cert authorizer
// (pkg/api/mcp/authz_agent_cert.go) recognizes it and scopes it to the tenant.
//
// The resolver never persists a long-lived key: the private key is held in an
// mlocked secure.Buffer, copied to a 0600 file, and the buffer is destroyed
// immediately. The directory is removed by the cleanup the resolver returns,
// which the ACP backend fires on session Close.
//
// This is the embedded-agent wiring. A remote agent daemon would implement the
// same seam with its own issuer and MCP endpoint.
type AgentMCPPresetResolver struct {
	issuer        service.AgentCertIssuer
	endpointURL   string
	hostname      string
	defaultTenant string
	logger        interfaces.Logger
}

// Compile-time interface check.
var _ orchestration.MCPPresetResolver = (*AgentMCPPresetResolver)(nil)

// NewAgentMCPPresetResolver constructs a resolver. issuer mints the ephemeral
// certificates (reuse the same issuer that signs the embedded agent identity so
// the cert chains to the platform CA the MCP listener trusts). endpointURL is
// the fully-qualified https://host:port/mcp address the agent connects to.
// hostname seeds the certificate's DNS SAN/CN default. defaultTenant is used
// when a spawn carries no tenant in context (the embedded agent runs in the
// system tenant).
func NewAgentMCPPresetResolver(issuer service.AgentCertIssuer, endpointURL, hostname, defaultTenant string, logger interfaces.Logger) (*AgentMCPPresetResolver, error) {
	if issuer == nil {
		return nil, ErrMCPPresetResolverNilIssuer
	}
	if strings.TrimSpace(endpointURL) == "" {
		return nil, ErrMCPPresetResolverNoEndpoint
	}
	if strings.TrimSpace(hostname) == "" {
		return nil, ErrMCPPresetResolverNoHostname
	}
	return &AgentMCPPresetResolver{
		issuer:        issuer,
		endpointURL:   endpointURL,
		hostname:      hostname,
		defaultTenant: strings.TrimSpace(defaultTenant),
		logger:        logger,
	}, nil
}

// Resolve implements orchestration.MCPPresetResolver. It mints an ephemeral
// agent certificate for the session, writes cert/key/ca into a private
// temporary directory (key at 0600), and returns the endpoint URL, the env map
// pointing at the three files, and a cleanup that removes the directory.
func (r *AgentMCPPresetResolver) Resolve(ctx context.Context, sessionID, tenantID string) (string, map[string]string, func(), error) {
	tenant := strings.TrimSpace(tenantID)
	if tenant == "" {
		tenant = r.defaultTenant
	}
	if tenant == "" {
		return "", nil, nil, ErrMCPPresetResolverNoTenant
	}

	cn := "agent-" + strings.TrimSpace(sessionID)
	if cn == "agent-" {
		cn = "agent-" + r.hostname
	}

	issued, err := r.issuer.IssueAgentCertificate(ctx, &service.AgentCertificateRequest{
		TenantID: tenant,
		Hostname: r.hostname,
		Subject: service.AgentCertificateSubject{
			CommonName:         cn,
			Organization:       tenant,
			OrganizationalUnit: service.DefaultAgentCertOU,
		},
		ExtraDNSNames: []string{"localhost"},
		ValidityDays:  ephemeralAgentCertValidityDays,
	})
	if err != nil {
		return "", nil, nil, &MCPPresetResolveError{Op: "issue ephemeral agent certificate", Cause: err}
	}

	// From here the issued private key lives in an mlocked buffer that MUST be
	// destroyed before we return on any path.
	dir, err := os.MkdirTemp("", "quicbaas-mcp-cert-*")
	if err != nil {
		issued.PrivateKey.Destroy()
		return "", nil, nil, &MCPPresetResolveError{Op: "create ephemeral cert dir", Cause: err}
	}
	// The MkdirTemp dir is already 0700; tighten defensively in case of umask
	// surprises so only the spawning user can traverse it.
	if chmodErr := os.Chmod(dir, 0700); chmodErr != nil {
		issued.PrivateKey.Destroy()
		_ = os.RemoveAll(dir)
		return "", nil, nil, &MCPPresetResolveError{Op: "secure ephemeral cert dir", Cause: chmodErr}
	}

	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	caPath := filepath.Join(dir, "ca.pem")

	if writeErr := os.WriteFile(certPath, issued.CertPEM, 0644); writeErr != nil {
		issued.PrivateKey.Destroy()
		_ = os.RemoveAll(dir)
		return "", nil, nil, &MCPPresetResolveError{Op: "write agent certificate", Cause: writeErr}
	}

	// Write the private key at 0600, then destroy the mlocked buffer
	// immediately so no plaintext key lingers in resident memory.
	keyErr := os.WriteFile(keyPath, issued.PrivateKey.Bytes(), 0600)
	issued.PrivateKey.Destroy()
	if keyErr != nil {
		_ = os.RemoveAll(dir)
		return "", nil, nil, &MCPPresetResolveError{Op: "write agent private key", Cause: keyErr}
	}

	if writeErr := os.WriteFile(caPath, issued.CAPEM, 0644); writeErr != nil {
		_ = os.RemoveAll(dir)
		return "", nil, nil, &MCPPresetResolveError{Op: "write ca bundle", Cause: writeErr}
	}

	cleanup := func() { _ = os.RemoveAll(dir) }
	env := map[string]string{
		mcpCertEnvVar: certPath,
		mcpKeyEnvVar:  keyPath,
		mcpCAEnvVar:   caPath,
	}

	if r.logger != nil {
		r.logger.Debugf("mcp preset resolver: issued ephemeral agent cert cn=%s tenant=%s dir=%s", cn, tenant, dir)
	}
	return r.endpointURL, env, cleanup, nil
}

// =============================================================================
// Typed errors
// =============================================================================

// ErrMCPPresetResolverNilIssuer is returned by NewAgentMCPPresetResolver when
// no cert issuer is supplied. Fail fast at wiring rather than minting nil-cert
// sessions at runtime.
var ErrMCPPresetResolverNilIssuer = errors.New("mcp preset resolver: agent cert issuer is required")

// ErrMCPPresetResolverNoEndpoint is returned when the resolver is constructed
// without an MCP endpoint URL.
var ErrMCPPresetResolverNoEndpoint = errors.New("mcp preset resolver: endpoint url is required")

// ErrMCPPresetResolverNoHostname is returned when the resolver is constructed
// without a hostname for the certificate SAN.
var ErrMCPPresetResolverNoHostname = errors.New("mcp preset resolver: hostname is required")

// ErrMCPPresetResolverNoTenant is returned by Resolve when neither the spawn
// context nor the resolver's default supplies a tenant: an agent certificate
// with no O= attribute would be denied by the MCP authorizer, so refuse to
// mint one.
var ErrMCPPresetResolverNoTenant = errors.New("mcp preset resolver: no tenant available for agent certificate")

// MCPPresetResolveError wraps a failure encountered while provisioning the
// ephemeral MCP agent certificate. Op labels the stage; Cause carries the
// underlying error.
type MCPPresetResolveError struct {
	// Op is a short stage label ("issue ephemeral agent certificate",
	// "write agent private key", ...).
	Op string

	// Cause is the wrapped underlying error. Always non-nil.
	Cause error
}

// Error implements the error interface.
func (e *MCPPresetResolveError) Error() string {
	return fmt.Sprintf("mcp preset resolver: %s: %v", e.Op, e.Cause)
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *MCPPresetResolveError) Unwrap() error {
	return e.Cause
}
