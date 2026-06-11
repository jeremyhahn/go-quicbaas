//go:build rest

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

package rest

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// agentAppManagerAccessor is the narrow interface the register handler
// needs from the service layer for OAuth2 application creation.
type agentAppManagerAccessor interface {
	// GetAppManager returns the IAM AppManager used to register the
	// agent's OAuth2 client application. Required for the
	// client_credentials grant flow.
	GetAppManager(ctx context.Context) (*iam.AppManager, error)
	// GetAppStorage returns the underlying AppStorage for direct access when
	// registering mTLS applications that require fields not exposed by AppManager.
	GetAppStorage(ctx context.Context) (iam.AppStorage, error)
}

// agentRegisterRequest is the JSON body accepted by
// POST /api/v1/agents/register. The daemon authenticates via its mTLS
// client certificate on the TLS handshake; this body only carries
// self-declared capabilities used for registry bookkeeping.
type agentRegisterRequest struct {
	TenantID      string            `json:"tenant_id"`
	BindAddr      string            `json:"bind_addr,omitempty"`
	SandboxPolicy string            `json:"sandbox_policy,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// agentRegisterResponse mirrors the shape the daemon's enroller decodes
// with DisallowUnknownFields — field names are load-bearing.
//
// EventIngestAddr is the URL the agent must dial for the gRPC
// PushEvents bidi-stream. The REST listener intentionally advertises
// only http/1.1 in ALPN, so the daemon must dial the gRPC listener
// directly. When omitted (zero string) the agent skips event
// forwarding rather than dialing the wrong port.
//
// MTLSClientID is the client_id for the OAuth2 application registered
// with self_signed_tls_client_auth using the agent's mTLS peer certificate.
// When present, the agent should prefer /oauth2/token-mtls over
// /oauth2/token so that no client_secret is required on disk.
// Old agents that do not recognise this field fall back to client_secret.
type agentRegisterResponse struct {
	ClientID         string   `json:"client_id"`
	ClientSecret     string   `json:"client_secret"`
	AgentID          string   `json:"agent_id"`
	ServiceAccountID string   `json:"service_account_id"`
	Role             string   `json:"role,omitempty"`
	Scopes           []string `json:"scopes"`
	EventIngestAddr  string   `json:"event_ingest_addr,omitempty"`
	MTLSClientID     string   `json:"mtls_client_id,omitempty"`
}

// defaultRegisterRequestMaxBytes caps the request body size on the
// register endpoint. The body is small (≤ a few KB); this guards a
// potentially-public endpoint against hostile large uploads.
const defaultRegisterRequestMaxBytes int64 = 64 * 1024

// agentRegistrarOU is the Organizational Unit the server requires on
// agent mTLS certificates. The deploy-service cert issuer stamps this
// value by default; anything else is rejected to stop a client cert
// that was minted for a different purpose from authenticating here.
const agentRegistrarOU = "agents"

// defaultAgentRuntimeScopes returns the scopes granted to daemons
// registered through the mTLS endpoint. Kept in sync with
// pkg/orchestration/agent_identity.go.
func defaultAgentRuntimeScopes() []string {
	return []string{
		"agent:runtime:spawn",
		"agent:runtime:prompt",
		"agent:runtime:status",
	}
}

// registerAgentRegistrationPublicRoutes wires the mTLS-only register
// route (POST /api/v1/agents/register) onto the root router. The
// endpoint is public with respect to the server-side authorization
// middleware because it IS the authentication bootstrap — the TLS
// handshake authenticates the caller via the client certificate.
//
// The TLS listener MUST be configured with ClientAuth set to
// VerifyClientCertIfGiven or RequireAndVerifyClientCert for this
// endpoint to be safe. Server bootstrap enforces this pairing.
func (s *Server) registerAgentRegistrationPublicRoutes(router interface {
	Post(pattern string, h http.HandlerFunc)
}) {
	router.Post("/api/v1/agents/register", s.HandleAgentRegister)
}

// HandleAgentRegister handles POST /api/v1/agents/register. The TLS
// handshake establishes the daemon's identity via a client certificate
// issued by the server's CA. This handler extracts the tenant claim
// from the certificate's Organization field, verifies the required OU
// and ClientAuth EKU are present, and mints OAuth2 client credentials
// the daemon uses for heartbeat and event-forwarding from that point on.
//
// The request body's tenant_id is cross-checked against the certificate
// Organization value to prevent a valid certificate from registering
// for a tenant the operator did not intend.
func (s *Server) HandleAgentRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract & verify the mTLS peer certificate. TLS client-auth is
	// configured at the listener; an empty PeerCertificates slice here
	// means the listener admitted the request without a cert, which
	// indicates a misconfiguration.
	peer, err := extractAgentPeerCertificate(r)
	if err != nil {
		respondAgentRegisterError(w, r, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}

	// Cap body size before Unmarshal so a misbehaving client cannot
	// wedge the server by streaming a large payload on a public endpoint.
	limited := http.MaxBytesReader(w, r.Body, defaultRegisterRequestMaxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		respondAgentRegisterError(w, r, http.StatusBadRequest, "invalid_request", "request body too large or unreadable")
		return
	}
	defer r.Body.Close()

	var req agentRegisterRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondAgentRegisterError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.TenantID) == "" {
		respondAgentRegisterError(w, r, http.StatusBadRequest, "invalid_request", "tenant_id is required")
		return
	}

	// The cert Organization carries the tenant claim. Cross-check
	// against the request body so a valid cert cannot be used to
	// register a daemon for an unintended tenant.
	certTenant := extractCertTenant(peer)
	if certTenant == "" {
		respondAgentRegisterError(w, r, http.StatusUnauthorized, "invalid_client", "peer certificate missing tenant claim (Organization)")
		return
	}
	if certTenant != req.TenantID {
		respondAgentRegisterError(w, r, http.StatusForbidden, "tenant_mismatch",
			fmt.Sprintf("certificate tenant %q does not match request tenant %q", certTenant, req.TenantID))
		return
	}

	accessor, ok := s.service.(agentAppManagerAccessor)
	if !ok {
		respondAgentRegisterError(w, r, http.StatusInternalServerError, "server_error", "agent registrar service not available")
		return
	}
	appManager, err := accessor.GetAppManager(ctx)
	if err != nil {
		respondAgentRegisterError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if appManager == nil {
		respondAgentRegisterError(w, r, http.StatusInternalServerError, "server_error", "IAM AppManager not initialized")
		return
	}

	// Create the OAuth2 application the daemon will use for all future
	// authenticated calls (heartbeat, event forwarding). The app lives
	// in the claimed tenant; deletion of the tenant or app revokes the
	// daemon cleanly.
	commonName := peer.Subject.CommonName
	appName := iam.AgentAppName(req.TenantID, commonName)
	app, err := appManager.RegisterApp(ctx, req.TenantID, iam.RegisterAppOptions{
		Name:           appName,
		GrantTypes:     []string{iam.GrantTypeClientCredentials},
		ResponseTypes:  []string{},
		ResourcePrefix: "agent-runtime",
	})
	if err != nil {
		respondAgentRegisterError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Also register a self_signed_tls_client_auth application pinned to the
	// peer certificate. New agents use this to fetch tokens at /oauth2/token-mtls
	// without storing a client_secret on disk. Old agents ignore mtls_client_id.
	mtlsClientID := registerMTLSApp(ctx, s.logger, accessor, req.TenantID, appName, peer)

	// Publish into the fleet registry so the orchestrator dispatcher
	// can route work to the daemon immediately. Registration failures
	// are logged but do NOT roll back the OAuth2 app — the daemon
	// already has credentials it will use, and a failed register can
	// recover on the next heartbeat.
	if registrar, ok := s.agentRegistrar.(iam.AgentRegistrar); ok && registrar != nil {
		// Translate the daemon's bind address into a dialable host. A
		// bind of 0.0.0.0:<port> or [::]:<port> can't be dialed as-is;
		// rewrite the host component using the daemon's self-reported
		// hostname (which maps to its LAN IP via DNS/mDNS) so the
		// RemoteACPBackend dispatcher can reach it over the network.
		dialHost := dialableAgentHost(req.BindAddr, req.Hostname, peer)
		regMeta := mergeRegisterMetadata(&req, peer)
		// Stamp OAuth2 client IDs into the registration record so that
		// DisableAgent can revoke them at the IAM layer without a separate lookup.
		regMeta["oauth2_client_id"] = app.ClientID
		if mtlsClientID != "" {
			regMeta["mtls_client_id"] = mtlsClientID
		}
		regReq := &iam.AgentRegistrarRequest{
			AgentID:   appName,
			TenantID:  req.TenantID,
			Name:      appName,
			Host:      dialHost,
			Backends:  backendsFromRegisterMetadata(req.Metadata),
			AgentType: "agent-runtime",
			Kind:      "daemon",
			Metadata:  regMeta,
		}
		if regErr := registrar.RegisterAgent(ctx, regReq); regErr != nil {
			s.logger.Warnf("agent register: registrar failed for agent_id=%s: %v (daemon will reconcile on heartbeat)", appName, regErr)
		}
	}

	s.logger.Infof("agent register: tenant=%s agent_id=%s client_id=%s cn=%s serial=%x",
		req.TenantID, appName, app.ClientID, commonName, peer.SerialNumber)

	// Resolve the gRPC ingest URL the daemon must dial for PushEvents.
	// The REST listener intentionally advertises only http/1.1 in ALPN,
	// so the daemon CANNOT use the REST host for the gRPC stream — that
	// is what produced the "no application protocol" handshake errors.
	// The configured value is plumbed in from server lifecycle; we
	// rewrite a wildcard host to the request's Host header (a known
	// dialable target from the agent's perspective).
	ingestAddr := dialableEventIngestAddr(s.eventIngestAddr(), r)

	RespondJSON(w, r, http.StatusCreated, &agentRegisterResponse{
		ClientID:         app.ClientID,
		ClientSecret:     app.ClientSecret, // plaintext — only returned here
		AgentID:          appName,
		ServiceAccountID: appName,
		Role:             "agent-runtime",
		Scopes:           defaultAgentRuntimeScopes(),
		EventIngestAddr:  ingestAddr,
		MTLSClientID:     mtlsClientID,
	})
}

// extractAgentPeerCertificate pulls the mTLS peer certificate from the
// request's TLS state and asserts the cert carries the required OU and
// EKU stamps. Any missing/invalid value is surfaced as a typed error.
func extractAgentPeerCertificate(r *http.Request) (*x509.Certificate, error) {
	if r.TLS == nil {
		return nil, errMissingPeerCertificate
	}
	if len(r.TLS.PeerCertificates) == 0 {
		return nil, errMissingPeerCertificate
	}
	peer := r.TLS.PeerCertificates[0]
	if peer == nil {
		return nil, errMissingPeerCertificate
	}

	// Require the "agents" OU stamp — defence-in-depth against
	// operator misuse of a cert the same CA minted for another role.
	found := false
	for _, ou := range peer.Subject.OrganizationalUnit {
		if ou == agentRegistrarOU {
			found = true
			break
		}
	}
	if !found {
		return nil, errInvalidPeerOU
	}

	// Require ClientAuth EKU.
	hasClientAuth := false
	for _, usage := range peer.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
			break
		}
	}
	if !hasClientAuth {
		return nil, errInvalidPeerEKU
	}
	return peer, nil
}

// extractCertTenant returns the tenant ID carried on the peer
// certificate's Organization field. Empty return means the claim is
// missing and registration must be rejected.
func extractCertTenant(cert *x509.Certificate) string {
	if cert == nil || len(cert.Subject.Organization) == 0 {
		return ""
	}
	return strings.TrimSpace(cert.Subject.Organization[0])
}

// sanitizeAppName delegates to iam.SanitizeAppName so the REST handler
// and any other in-package callers share the canonical sanitiser used
// by the agent deploy service when it pre-computes an expected
// AgentID for wait-online polling.
func sanitizeAppName(cn string) string {
	return iam.SanitizeAppName(cn)
}

// backendsFromRegisterMetadata returns the daemon's declared backends
// from the registration request metadata. Callers pass a comma-separated
// list under the "backends" key; we return nil when absent so the
// registry entry accepts the row and the first heartbeat can correct it.
func backendsFromRegisterMetadata(meta map[string]string) []string {
	if meta == nil {
		return nil
	}
	raw, ok := meta["backends"]
	if !ok || raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// mergeRegisterMetadata produces the registry metadata we stamp on the
// registration record. Includes the daemon's sandbox policy and
// certificate fingerprint so operators auditing the registry can trace
// a row back to the issuing cert.
func mergeRegisterMetadata(req *agentRegisterRequest, cert *x509.Certificate) map[string]string {
	out := map[string]string{
		"sandbox_policy": req.SandboxPolicy,
		"hostname":       req.Hostname,
	}
	if cert != nil && cert.SerialNumber != nil {
		out["cert_serial"] = cert.SerialNumber.Text(16)
	}
	if cert != nil && cert.Subject.CommonName != "" {
		out["cert_cn"] = cert.Subject.CommonName
	}
	for k, v := range req.Metadata {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// registerMTLSApp creates a secondary OAuth2 application pinned to the agent's
// mTLS peer certificate using self_signed_tls_client_auth (RFC 8705 §2.2).
// On success it returns the new client_id; on failure it logs and returns "".
// Failure is non-fatal: the agent falls back to the legacy client_secret flow.
func registerMTLSApp(ctx context.Context, log interface {
	Warnf(format string, args ...interface{})
	Infof(format string, args ...interface{})
}, accessor agentAppManagerAccessor, tenantID, baseAppName string, peer *x509.Certificate) string {
	appStorage, err := accessor.GetAppStorage(ctx)
	if err != nil || appStorage == nil {
		log.Warnf("agent register: app storage unavailable, skipping self_signed_tls_client_auth registration: %v", err)
		return ""
	}
	mtlsAppName := baseAppName + "-mtls"
	thumbprint := iam.CertThumbprintS256(peer)
	mtlsApp := &iam.Application{
		ClientID:                mtlsAppName,
		Name:                    mtlsAppName,
		TenantID:                tenantID,
		GrantTypes:              []string{iam.GrantTypeClientCredentials},
		ResponseTypes:           []string{},
		ResourcePrefix:          "agent-runtime",
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
		MTLSCertThumbprint:      thumbprint,
	}
	if createErr := appStorage.CreateApp(ctx, mtlsApp); createErr != nil {
		log.Warnf("agent register: failed to register self_signed_tls_client_auth app %s: %v", mtlsAppName, createErr)
		return ""
	}
	log.Infof("agent register: self_signed_tls_client_auth app registered: client_id=%s thumbprint=%s", mtlsAppName, thumbprint)
	return mtlsAppName
}

// agentRegisterErrorBody is the JSON shape for registration error
// responses. It mirrors the OAuth2 error envelope (RFC 6749 §5.2) so
// operators can parse errors with the same code as /oauth2/token.
type agentRegisterErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// respondAgentRegisterError writes a structured error response.
func respondAgentRegisterError(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	body := &agentRegisterErrorBody{Error: code, ErrorDescription: detail}
	w.Header().Set("Cache-Control", "no-store")
	enc, _ := json.Marshal(body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(enc)))
	w.WriteHeader(status)
	_, _ = w.Write(enc)
}

// Typed errors for agent mTLS register.
var (
	// errMissingPeerCertificate indicates the TLS handshake did not
	// present a peer certificate. Surfaced to operators as 401.
	errMissingPeerCertificate = errors.New("agent register: missing mTLS peer certificate")

	// errInvalidPeerOU indicates the peer certificate's OU value is not
	// the canonical "agents" stamp.
	errInvalidPeerOU = errors.New("agent register: peer certificate missing required OU=agents stamp")

	// errInvalidPeerEKU indicates the peer certificate does not carry
	// the ClientAuth Extended Key Usage.
	errInvalidPeerEKU = errors.New("agent register: peer certificate missing ClientAuth EKU")
)

// dialableAgentHost returns a "host:port" string the orchestrator can
// dial to reach the daemon. When bindAddr uses a wildcard host (0.0.0.0
// or [::]) the function rewrites the host component using (in order of
// preference): the peer certificate's CN with the "agent-" prefix
// stripped (the issued cert uses the daemon's LAN IP as the CN — always
// resolvable), the first DNS SAN, and finally the self-reported
// hostname (a short hostname that may or may not resolve centrally).
// Returns bindAddr unchanged when the host is already a concrete IP or
// DNS name.
func dialableAgentHost(bindAddr, hostname string, peer *x509.Certificate) string {
	if bindAddr == "" {
		return bindAddr
	}
	host, port, err := net.SplitHostPort(bindAddr)
	if err != nil {
		return bindAddr
	}
	if !isWildcardHost(host) {
		return bindAddr
	}
	if peer != nil {
		if cn := strings.TrimPrefix(peer.Subject.CommonName, "agent-"); cn != "" && cn != peer.Subject.CommonName {
			return net.JoinHostPort(cn, port)
		}
		if len(peer.DNSNames) > 0 {
			return net.JoinHostPort(peer.DNSNames[0], port)
		}
	}
	if hostname != "" {
		return net.JoinHostPort(hostname, port)
	}
	return bindAddr
}

// isWildcardHost reports whether host is a wildcard bind address that
// can't be dialed as a destination.
func isWildcardHost(host string) bool {
	return host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
}

// dialableEventIngestAddr rewrites a configured ingest URL whose host
// component is a wildcard bind (0.0.0.0 / ::) into a URL that uses the
// request's Host header instead. The agent already proved that Host is
// reachable by reaching this handler; using it for the gRPC dial keeps
// SNI/cert verification consistent with the REST connection.
//
// Returns the empty string when configured is empty so the response
// omits event_ingest_addr (agent then disables event forwarding rather
// than dialing a wrong port).
//
// Returns configured unchanged when its host is already a concrete IP
// or DNS name. Returns the empty string when configured is unparseable
// — a malformed configured URL is a server bug; refusing to advertise
// it is safer than returning a value the agent will fail to dial.
func dialableEventIngestAddr(configured string, r *http.Request) string {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return ""
	}
	u, err := url.Parse(configured)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if !isWildcardHost(host) {
		return u.String()
	}
	// Wildcard bind — substitute the request's Host (the dialable
	// endpoint the agent already used to reach this handler). Strip
	// any port from r.Host and reuse the configured URL's port.
	reqHost := r.Host
	if h, _, splitErr := net.SplitHostPort(reqHost); splitErr == nil {
		reqHost = h
	}
	if reqHost == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		u.Host = reqHost
	} else {
		u.Host = net.JoinHostPort(reqHost, port)
	}
	return u.String()
}

// Compile-time interface checks so a refactor of iam's types is caught
// before runtime.
var _ = orchestration.AgentStatusRegistered
