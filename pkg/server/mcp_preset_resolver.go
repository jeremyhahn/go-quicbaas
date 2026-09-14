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
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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

// AgentSessionIdentityStore is the identity surface a session enrollment needs:
// create the principal a session's certificate authenticates as, and remove it
// when the session closes. *iam.IdentityStore satisfies it, and it is declared
// as an interface here so the resolver depends on the two verbs rather than on
// the whole store.
//
// Identity belongs to go-qrdb. Both methods reach it through pkg/iam, which is
// QuicBaaS's boundary onto qrdb's user and role stores -- there is no local user
// model behind this seam.
type AgentSessionIdentityStore interface {
	// EnrollAgentSessionIdentity creates or converges the session principal.
	EnrollAgentSessionIdentity(ctx context.Context, tenantID string, cert *x509.Certificate) (*iam.User, error)

	// RevokeAgentSessionIdentity removes it.
	RevokeAgentSessionIdentity(ctx context.Context, tenantID, username string) error
}

// AgentSessionIdentityStoreResolver returns the identity store to enroll a
// session into. It is a function rather than a value because the resolver is
// wired at server construction, BEFORE the IAM provider is up: resolving per
// spawn is what lets the resolver exist while the store it needs does not yet.
type AgentSessionIdentityStoreResolver func(ctx context.Context) (AgentSessionIdentityStore, error)

// agentSessionRevokeTimeout bounds the revocation a session Close performs. The
// cleanup runs on a fresh context because the spawn context that produced the
// session is usually already cancelled by then, and an unbounded one would let
// a stalled store hold a Close open.
const agentSessionRevokeTimeout = 15 * time.Second

// AgentMCPPresetResolver implements orchestration.MCPPresetResolver by minting
// a fresh, short-lived mTLS agent certificate per spawn, ENROLLING it as a
// session-scoped qrdb principal, and writing the material to a private
// per-session directory. The cleanup it returns revokes the principal and
// deletes the directory when the session closes.
//
// The enrollment is the part without which none of the rest means anything.
// go-qrdb authenticates the certificate and then resolves that principal's
// roles from the user record its Subject CommonName names (pkg/auth/middleware
// finalizeIdentity); a certificate no record names proceeds with EMPTY roles,
// and the MCP surface is default-deny. Minting a certificate and handing it to
// an agent therefore conferred nothing at all -- the agent connected and was
// refused every tool but the five public ones (go-quicbaas#901).
//
// The tenant the principal is written to is the tenant qrdb will AUTHENTICATE
// the certificate into, read back off the minted certificate's SPIFFE SAN, not
// the tenant the spawn asked for. The two agree by construction here; the
// resolver asserts it rather than assuming it, because an enrollment written to
// any other tenant is invisible to the authenticator and reproduces the very
// defect this fixes.
//
// The resolver never persists a long-lived key: the private key is held in an
// mlocked secure.Buffer, copied to a 0600 file, and the buffer is destroyed
// immediately.
//
// This is the embedded-agent wiring. A remote agent daemon would implement the
// same seam with its own issuer and MCP endpoint.
type AgentMCPPresetResolver struct {
	issuer            service.AgentCertIssuer
	identities        AgentSessionIdentityStoreResolver
	endpointURL       string
	hostname          string
	defaultTenant     string
	spiffeTrustDomain string
	logger            interfaces.Logger
}

// Compile-time interface check.
var _ orchestration.MCPPresetResolver = (*AgentMCPPresetResolver)(nil)

// AgentMCPPresetResolverConfig carries the resolver's dependencies. Every field
// is required; there is no useful partial resolver, and a missing one is a
// wiring defect that must surface at construction rather than as a session with
// no authority.
type AgentMCPPresetResolverConfig struct {
	// Issuer mints the ephemeral certificates. Reuse the same issuer that
	// signs the embedded agent identity so the certificate chains to the
	// platform CA the MCP listener trusts for client auth.
	Issuer service.AgentCertIssuer

	// Identities resolves the identity store each session principal is
	// enrolled into.
	Identities AgentSessionIdentityStoreResolver

	// EndpointURL is the fully-qualified https://host:port/mcp address the
	// spawned agent connects to.
	EndpointURL string

	// Hostname seeds the certificate's DNS SAN and its CommonName default.
	Hostname string

	// DefaultTenant is used when a spawn carries no tenant in context (the
	// embedded agent runs in the system tenant).
	DefaultTenant string

	// SPIFFETrustDomain is the operator-configured trust domain
	// (iam.spiffe_trust_domain). It is REQUIRED, because it is the only thing
	// that puts a tenant-scoped SPIFFE SAN on the certificate, and without that
	// SAN go-qrdb derives the identity's tenant from the first Subject OU --
	// "agents" on every agent certificate -- so the session would authenticate
	// into a phantom tenant no enrollment can reach. First boot already refuses
	// to issue the admin certificate without it (ErrNoSPIFFETrustDomain), so
	// requiring it here invents no identifier and excludes no running
	// deployment.
	SPIFFETrustDomain string

	// Logger receives the per-spawn diagnostics. Optional.
	Logger interfaces.Logger
}

// NewAgentMCPPresetResolver constructs a resolver from cfg.
func NewAgentMCPPresetResolver(cfg AgentMCPPresetResolverConfig) (*AgentMCPPresetResolver, error) {
	if cfg.Issuer == nil {
		return nil, ErrMCPPresetResolverNilIssuer
	}
	if cfg.Identities == nil {
		return nil, ErrMCPPresetResolverNilIdentities
	}
	if strings.TrimSpace(cfg.EndpointURL) == "" {
		return nil, ErrMCPPresetResolverNoEndpoint
	}
	if strings.TrimSpace(cfg.Hostname) == "" {
		return nil, ErrMCPPresetResolverNoHostname
	}
	if strings.TrimSpace(cfg.SPIFFETrustDomain) == "" {
		return nil, ErrMCPPresetResolverNoTrustDomain
	}
	return &AgentMCPPresetResolver{
		issuer:            cfg.Issuer,
		identities:        cfg.Identities,
		endpointURL:       cfg.EndpointURL,
		hostname:          cfg.Hostname,
		defaultTenant:     strings.TrimSpace(cfg.DefaultTenant),
		spiffeTrustDomain: strings.TrimSpace(cfg.SPIFFETrustDomain),
		logger:            cfg.Logger,
	}, nil
}

// Resolve implements orchestration.MCPPresetResolver. It mints an ephemeral
// agent certificate for the session, enrolls it as a session-scoped qrdb
// principal holding exactly iam.AgentSessionGrant, writes cert/key/ca into a
// private temporary directory (key at 0600), and returns the endpoint URL, the
// env map pointing at the three files, and a cleanup that revokes the principal
// and removes the directory.
//
// Failure at ANY stage returns an error and leaves nothing behind: the caller
// forwards the preset unresolved (pkg/orchestration resolveMCPPresets logs and
// skips), so a spawn never proceeds with half an identity.
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

	// Resolve the identity store BEFORE minting anything. A store that is not
	// up yet means no session can be given authority, and finding that out
	// after issuing a certificate would leave one in the CA's store for a
	// session that is about to be refused every tool.
	identities, err := r.identities(ctx)
	if err != nil {
		return "", nil, nil, &MCPPresetResolveError{Op: "resolve identity store", Cause: err}
	}
	if identities == nil {
		return "", nil, nil, &MCPPresetResolveError{Op: "resolve identity store", Cause: ErrMCPPresetResolverNoIdentityStore}
	}

	issued, err := r.issuer.IssueAgentCertificate(ctx, &service.AgentCertificateRequest{
		TenantID: tenant,
		Hostname: r.hostname,
		Subject: service.AgentCertificateSubject{
			CommonName:         cn,
			Organization:       tenant,
			OrganizationalUnit: service.DefaultAgentCertOU,
		},
		ExtraDNSNames:     []string{"localhost"},
		ValidityDays:      ephemeralAgentCertValidityDays,
		SPIFFETrustDomain: r.spiffeTrustDomain,
	})
	if err != nil {
		return "", nil, nil, &MCPPresetResolveError{Op: "issue ephemeral agent certificate", Cause: err}
	}

	// From here the issued private key lives in an mlocked buffer that MUST be
	// destroyed before we return on any path.
	peer := issued.ParsedCert
	if peer == nil {
		issued.PrivateKey.Destroy()
		return "", nil, nil, &MCPPresetResolveError{Op: "enroll session identity", Cause: ErrMCPPresetResolverNoParsedCert}
	}

	// Enroll into the tenant qrdb will AUTHENTICATE this certificate into, and
	// refuse to proceed when that is not the tenant the spawn named. A record in
	// any other tenant is one the authenticator never looks in.
	authTenant, err := r.authenticatedTenant(peer, tenant)
	if err != nil {
		issued.PrivateKey.Destroy()
		return "", nil, nil, &MCPPresetResolveError{Op: "verify certificate tenant binding", Cause: err}
	}

	principal, err := identities.EnrollAgentSessionIdentity(ctx, authTenant, peer)
	if err != nil {
		issued.PrivateKey.Destroy()
		return "", nil, nil, &MCPPresetResolveError{Op: "enroll session identity", Cause: err}
	}

	// Every return past this point must revoke: the principal exists now, and
	// leaving one behind for a session that never started would accumulate
	// live identities nothing will ever close.
	revoke := func() {
		revokeCtx, cancel := context.WithTimeout(context.Background(), agentSessionRevokeTimeout)
		defer cancel()
		if revokeErr := identities.RevokeAgentSessionIdentity(revokeCtx, authTenant, principal.Username); revokeErr != nil && r.logger != nil {
			r.logger.Warnf("mcp preset resolver: revoking agent session identity %s in tenant %s failed: %v",
				principal.Username, authTenant, revokeErr)
		}
	}

	dir, err := createAgentSessionDir()
	if err != nil {
		issued.PrivateKey.Destroy()
		revoke()
		return "", nil, nil, err
	}

	// writeAgentSessionMaterial destroys the mlocked key buffer on every path,
	// so it is not destroyed again here.
	material, err := writeAgentSessionMaterial(dir, issued)
	if err != nil {
		_ = os.RemoveAll(dir)
		revoke()
		return "", nil, nil, err
	}

	// Revocation runs BEFORE the directory is removed so a store failure is
	// reported against material that still exists on disk for the operator to
	// inspect. Both steps always run; neither can skip the other.
	cleanup := func() {
		revoke()
		_ = os.RemoveAll(material.dir)
	}

	if r.logger != nil {
		r.logger.Debugf("mcp preset resolver: enrolled ephemeral agent session cn=%s tenant=%s scopes=%v dir=%s",
			principal.Username, authTenant, iam.AgentSessionGrant(), material.dir)
	}
	return r.endpointURL, material.env(), cleanup, nil
}

// agentSessionMaterial names the files a spawned agent reads its MCP identity
// from. They live together in one private directory, so removing that directory
// is the whole of the on-disk cleanup.
type agentSessionMaterial struct {
	// dir is the private per-session directory holding all three files.
	dir string

	// certPath, keyPath and caPath are the leaf certificate, its private key,
	// and the CA bundle the agent verifies the endpoint against.
	certPath string
	keyPath  string
	caPath   string
}

// env renders the material as the environment variables the spawned agent reads
// to locate it.
func (m *agentSessionMaterial) env() map[string]string {
	return map[string]string{
		mcpCertEnvVar: m.certPath,
		mcpKeyEnvVar:  m.keyPath,
		mcpCAEnvVar:   m.caPath,
	}
}

// createAgentSessionDir creates the private directory one session's material
// lives in. Owner-only: the key inside it is a credential, and a world-readable
// parent would make the 0600 on the key itself decorative.
func createAgentSessionDir() (string, error) {
	dir, err := os.MkdirTemp("", "quicbaas-mcp-cert-*")
	if err != nil {
		return "", &MCPPresetResolveError{Op: "create ephemeral cert dir", Cause: err}
	}
	// MkdirTemp already creates it 0700; tighten defensively in case of umask
	// surprises so only the spawning user can traverse it.
	if err := os.Chmod(dir, 0700); err != nil { // #nosec G302 -- a directory needs the execute bit to be traversable; 0700 is owner-only.
		_ = os.RemoveAll(dir)
		return "", &MCPPresetResolveError{Op: "secure ephemeral cert dir", Cause: err}
	}
	return dir, nil
}

// writeAgentSessionMaterial writes the issued certificate, its private key and
// the CA bundle into dir.
//
// It DESTROYS issued.PrivateKey on every path, success or failure: the plaintext
// key lives in an mlocked buffer and this is the only function that needs it, so
// destroying it here is what guarantees it cannot outlive the write. The caller
// must not destroy it again.
//
// It returns partial material on no path — a caller that gets an error has
// nothing to read — but it does NOT remove dir, because it did not create it.
// The caller owns the directory and removes it on failure.
func writeAgentSessionMaterial(dir string, issued *service.IssuedAgentCertificate) (*agentSessionMaterial, error) {
	material := &agentSessionMaterial{
		dir:      dir,
		certPath: filepath.Join(dir, "cert.pem"),
		keyPath:  filepath.Join(dir, "key.pem"),
		caPath:   filepath.Join(dir, "ca.pem"),
	}

	if err := os.WriteFile(material.certPath, issued.CertPEM, 0644); err != nil { // #nosec G306 -- public certificate material inside the 0700 directory created above.
		issued.PrivateKey.Destroy()
		return nil, &MCPPresetResolveError{Op: "write agent certificate", Cause: err}
	}

	// Write the private key at 0600, then destroy the mlocked buffer
	// immediately so no plaintext key lingers in resident memory.
	keyErr := os.WriteFile(material.keyPath, issued.PrivateKey.Bytes(), 0600)
	issued.PrivateKey.Destroy()
	if keyErr != nil {
		return nil, &MCPPresetResolveError{Op: "write agent private key", Cause: keyErr}
	}

	if err := os.WriteFile(material.caPath, issued.CAPEM, 0644); err != nil { // #nosec G306 -- public CA bundle inside the 0700 directory created above.
		return nil, &MCPPresetResolveError{Op: "write ca bundle", Cause: err}
	}
	return material, nil
}

// authenticatedTenant returns the tenant go-qrdb will authenticate peer into,
// asserting it is the tenant the spawn named.
//
// qrdb derives it from the certificate's tenant-scoped SPIFFE SAN and falls back
// to the first Subject OU when there is none (pkg/auth
// MTLSAuthenticator.TenantFromCert). Every agent certificate carries OU=agents,
// so that fallback names a tenant literally called "agents": it is reported, not
// used. A malformed SAN fails closed, exactly as qrdb's authenticator does.
func (r *AgentMCPPresetResolver) authenticatedTenant(peer *x509.Certificate, spawnTenant string) (string, error) {
	id, err := qrdbsdk.TenantFromCert(peer, "")
	switch {
	case err == nil:
		if id.TenantID != spawnTenant {
			return "", &AgentSessionCertTenantMismatchError{Certificate: id.TenantID, Spawn: spawnTenant}
		}
		return id.TenantID, nil
	case errors.Is(err, qrdbsdk.ErrSpiffeNoTenantID):
		return "", &AgentSessionCertNoSPIFFETenantError{CommonName: peer.Subject.CommonName}
	default:
		return "", err
	}
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

// ErrMCPPresetResolverNilIdentities is returned by NewAgentMCPPresetResolver
// when no identity-store resolver is supplied. A resolver without one could
// mint certificates but enroll nothing, which is exactly the defect #901
// recorded, so it must fail at wiring.
var ErrMCPPresetResolverNilIdentities = errors.New("mcp preset resolver: identity store resolver is required")

// ErrMCPPresetResolverNoTrustDomain is returned by NewAgentMCPPresetResolver
// when no SPIFFE trust domain is configured. Without one the ephemeral
// certificate carries no tenant SPIFFE SAN and go-qrdb authenticates it into a
// tenant named by its OU, where no enrollment can reach it.
var ErrMCPPresetResolverNoTrustDomain = errors.New(
	"mcp preset resolver: iam.spiffe_trust_domain is required to bind an agent session certificate to its tenant")

// ErrMCPPresetResolverNoIdentityStore is returned by Resolve when the identity
// store resolver reports no store — the IAM provider is not up, so no session
// can be given authority yet.
var ErrMCPPresetResolverNoIdentityStore = errors.New("mcp preset resolver: identity store is unavailable")

// ErrMCPPresetResolverNoParsedCert is returned by Resolve when the issuer
// produced no parsed leaf certificate. The enrollment derives the principal's
// username, tenant and binding FROM the certificate, so there is nothing to
// enroll without it.
var ErrMCPPresetResolverNoParsedCert = errors.New("mcp preset resolver: issuer returned no parsed certificate")

// AgentSessionCertTenantMismatchError reports an ephemeral certificate whose
// SPIFFE SAN authenticates into a different tenant than the spawn named.
// Enrolling into either tenant would be wrong: the spawn's tenant is where the
// authenticator never looks, and the certificate's is not the tenant the caller
// asked for.
type AgentSessionCertTenantMismatchError struct {
	// Certificate is the tenant the certificate authenticates into.
	Certificate string
	// Spawn is the tenant the spawn named.
	Spawn string
}

// Error implements the error interface.
func (e *AgentSessionCertTenantMismatchError) Error() string {
	return "mcp preset resolver: session certificate authenticates into tenant " + strconv.Quote(e.Certificate) +
		", not the spawning tenant " + strconv.Quote(e.Spawn)
}

// AgentSessionCertNoSPIFFETenantError reports an ephemeral certificate carrying
// no tenant-scoped SPIFFE SAN. go-qrdb would authenticate it into a tenant named
// by its OU — "agents" — so no enrollment in the spawning tenant could ever
// authorize it, and the session is refused a credential rather than handed a
// useless one.
type AgentSessionCertNoSPIFFETenantError struct {
	// CommonName is the certificate subject, for the operator's log.
	CommonName string
}

// Error implements the error interface.
func (e *AgentSessionCertNoSPIFFETenantError) Error() string {
	return "mcp preset resolver: session certificate for " + strconv.Quote(e.CommonName) +
		" carries no tenant-scoped SPIFFE SAN, so it cannot authenticate into the spawning tenant"
}

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
