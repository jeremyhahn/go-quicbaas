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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fleetAppManagerAccessor is the narrow interface the register handler
// needs from the service layer for OAuth2 application creation.
type fleetAppManagerAccessor interface {
	// GetAppManager returns the IAM AppManager used to register the
	// fleet node's OAuth2 client application. Required for the
	// client_credentials grant flow.
	GetAppManager(ctx context.Context) (*iam.AppManager, error)
	// GetAppStorage returns the underlying AppStorage for direct access when
	// registering mTLS applications that require fields not exposed by AppManager.
	GetAppStorage(ctx context.Context) (iam.AppStorage, error)
}

// fleetIdentityStoreAccessor is the narrow interface the register handler
// needs from the service layer to enroll the identity the daemon's client
// certificate authenticates as. It is the same accessor every other IAM
// handler resolves the identity store through.
type fleetIdentityStoreAccessor interface {
	// GetIdentityStore returns the IAM identity store, whose user and role
	// persistence is routed to go-qrdb's canonical UserServicer / RoleClient
	// (pkg/server iam_init.go) — the same records go-qrdb's own authorization
	// resolves an mTLS principal's roles from.
	GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
}

// fleetEnrollRequest is the JSON body accepted by
// POST /api/v1/fleet/enroll. The daemon authenticates via its mTLS
// client certificate on the TLS handshake; this body only carries
// self-declared capabilities used for registry bookkeeping.
type fleetEnrollRequest struct {
	TenantID      string            `json:"tenant_id"`
	BindAddr      string            `json:"bind_addr,omitempty"`
	AdvertiseAddr string            `json:"advertise_addr,omitempty"`
	SandboxPolicy string            `json:"sandbox_policy,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// fleetEnrollResponse mirrors the shape the daemon's enroller decodes
// with DisallowUnknownFields — field names are load-bearing.
//
// EventIngestAddr is the URL the fleet node must dial for the gRPC
// PushEvents bidi-stream. The REST listener intentionally advertises
// only http/1.1 in ALPN, so the daemon must dial the gRPC listener
// directly. When omitted (zero string) the fleet node skips event
// forwarding rather than dialing the wrong port.
//
// MTLSClientID is the client_id for the OAuth2 application registered
// with self_signed_tls_client_auth using the fleet node's mTLS peer certificate.
// When present, the fleet node should prefer /oauth2/token-mtls over
// /oauth2/token so that no client_secret is required on disk.
// Old fleet nodes that do not recognise this field fall back to client_secret.
type fleetEnrollResponse struct {
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

// fleetRegistrarOU is the Organizational Unit the server requires on
// fleet node mTLS certificates. The deploy-service cert issuer stamps this
// value by default; anything else is rejected to stop a client cert
// that was minted for a different purpose from authenticating here.
const fleetRegistrarOU = "agents"

// registerFleetEnrollmentPublicRoutes wires the mTLS-only register
// route (POST /api/v1/fleet/enroll) onto the root router. The
// endpoint is public with respect to the server-side authorization
// middleware because it IS the authentication bootstrap — the TLS
// handshake authenticates the caller via the client certificate.
//
// The TLS listener MUST be configured with ClientAuth set to
// VerifyClientCertIfGiven or RequireAndVerifyClientCert for this
// endpoint to be safe. Server bootstrap enforces this pairing.
func (s *Server) registerFleetEnrollmentPublicRoutes(router interface {
	Post(pattern string, h http.HandlerFunc)
}) {
	router.Post("/api/v1/fleet/enroll", s.HandleFleetEnroll)
}

// HandleFleetEnroll handles POST /api/v1/fleet/enroll. The TLS
// handshake establishes the daemon's identity via a client certificate
// issued by the server's CA. This handler extracts the tenant claim
// from the certificate's Organization field, verifies the required OU
// and ClientAuth EKU are present, and mints OAuth2 client credentials
// the daemon uses for heartbeat and event-forwarding from that point on.
//
// The request body's tenant_id is cross-checked against the certificate
// Organization value to prevent a valid certificate from registering
// for a tenant the operator did not intend.
func (s *Server) HandleFleetEnroll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract & verify the mTLS peer certificate. TLS client-auth is
	// configured at the listener; an empty PeerCertificates slice here
	// means the listener admitted the request without a cert, which
	// indicates a misconfiguration.
	peer, err := extractFleetNodePeerCertificate(r)
	if err != nil {
		respondFleetEnrollError(w, r, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}

	// Cap body size before Unmarshal so a misbehaving client cannot
	// wedge the server by streaming a large payload on a public endpoint.
	limited := http.MaxBytesReader(w, r.Body, defaultRegisterRequestMaxBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", "request body too large or unreadable")
		return
	}
	defer r.Body.Close()

	var req fleetEnrollRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.TenantID) == "" {
		respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", "tenant_id is required")
		return
	}

	// The cert Organization carries the tenant claim. Cross-check
	// against the request body so a valid cert cannot be used to
	// register a daemon for an unintended tenant.
	certTenant := extractCertTenant(peer)
	if certTenant == "" {
		respondFleetEnrollError(w, r, http.StatusUnauthorized, "invalid_client", "peer certificate missing tenant claim (Organization)")
		return
	}
	if certTenant != req.TenantID {
		respondFleetEnrollError(w, r, http.StatusForbidden, "tenant_mismatch",
			fmt.Sprintf("certificate tenant %q does not match request tenant %q", certTenant, req.TenantID))
		return
	}

	// The tenant go-qrdb will AUTHENTICATE this certificate into must be the
	// tenant being registered, or the identity enrolled below is written where
	// the authenticator never looks. qrdb derives it from the certificate's
	// tenant-scoped SPIFFE SAN (pkg/auth MTLSAuthenticator.TenantFromCert), so
	// that SAN — not the Organization claim cross-checked above — is the
	// authority when both are present.
	if err := assertCertAuthenticatesInto(peer, req.TenantID); err != nil {
		// A certificate carrying NO tenant SPIFFE SAN is reported, not
		// refused: it is a certificate this build's issuer would not mint, and
		// turning it into a registration failure would lock out a fleet that
		// enrolled with one. Its consequence is stated instead, because the
		// enrollment below cannot help it.
		var noTenantSAN *FleetNodeCertNoSPIFFETenantError
		if !errors.As(err, &noTenantSAN) {
			respondFleetEnrollError(w, r, http.StatusForbidden, "tenant_mismatch", err.Error())
			return
		}
		s.logger.Warnf("agent register: %v; re-issue the certificate with a tenant-scoped SPIFFE SAN or its"+
			" control-plane calls will stay unauthorized", err)
	}

	// The address is checked before anything is issued: a node the control
	// plane cannot dial would be placed work it can never be sent, and every
	// call routed to it would fail far from here (go-quicbaas#795).
	dialHost, reachability, err := s.fleetNodeDialAddress(ctx, &req, peer)
	if err != nil {
		respondFleetEnrollError(w, r, http.StatusUnprocessableEntity, "agent_address_unreachable", err.Error())
		return
	}

	accessor, ok := s.service.(fleetAppManagerAccessor)
	if !ok {
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", "agent registrar service not available")
		return
	}
	appManager, err := accessor.GetAppManager(ctx)
	if err != nil {
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	if appManager == nil {
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", "IAM AppManager not initialized")
		return
	}

	// Create the OAuth2 application the daemon will use for all future
	// authenticated calls (heartbeat, event forwarding). The app lives
	// in the claimed tenant; deletion of the tenant or app revokes the
	// daemon cleanly.
	//
	// RegisterAgentApp is keyed on the fleet node identity this request already
	// authenticated (tenant + certificate CN), so a daemon that retries
	// enrollment converges on the client it has instead of leaving an orphan
	// behind (go-quicbaas#644). Either way it leaves with a usable secret.
	//
	// Scopes is the GRANT, not a label: it is written onto the application
	// record and bounds every token the daemon is subsequently issued. The
	// register response advertised these entitlements long before anything
	// conferred them, so an enrolled fleet node authenticated fine and was then
	// refused 403 on every heartbeat (go-quicbaas#653).
	commonName := peer.Subject.CommonName
	appName := iam.FleetNodeAppName(req.TenantID, commonName)
	app, err := appManager.RegisterAgentApp(ctx, req.TenantID, commonName, iam.RegisterAppOptions{
		Name:           appName,
		GrantTypes:     []string{iam.GrantTypeClientCredentials},
		ResponseTypes:  []string{},
		ResourcePrefix: "agent-runtime",
		Scopes:         iam.FleetRuntimeGrant(),
	})
	if err != nil {
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Confer the SAME grant on the identity the daemon's CERTIFICATE
	// authenticates as. The application grant above bounds the daemon's bearer
	// tokens, but go-qrdb authenticates any request carrying a client
	// certificate as the certificate's subject and resolves that principal's
	// authority from its user record — and the daemon presents its certificate
	// on every call because it needs one for /oauth2/token-mtls. Without this
	// the enrolled daemon authenticates as a principal holding no roles at all
	// and is refused 403 on every heartbeat (go-quicbaas#663).
	//
	// Fatal, not best-effort: a registration that hands back credentials the
	// control plane will refuse is not a registration, and the response below
	// advertises this exact grant.
	if err := enrollFleetNodeCertIdentity(ctx, s.service, req.TenantID, peer); err != nil {
		var invalid *iam.ValidationError
		if errors.As(err, &invalid) {
			respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", err.Error())
		return
	}

	// Also register a self_signed_tls_client_auth application pinned to the
	// peer certificate. New fleet nodes use this to fetch tokens at /oauth2/token-mtls
	// without storing a client_secret on disk. Old fleet nodes ignore mtls_client_id.
	mtlsClientID := registerMTLSApp(ctx, s.logger, accessor, req.TenantID, appName, peer)

	// Publish into the fleet registry so the orchestrator dispatcher
	// can route work to the daemon immediately. Registration failures
	// are logged but do NOT roll back the OAuth2 app — the daemon
	// already has credentials it will use, and a failed register can
	// recover on the next heartbeat.
	if registrar, ok := s.fleetRegistrar.(iam.FleetRegistrar); ok && registrar != nil {
		regMeta := mergeRegisterMetadata(&req, peer)
		// Stamp OAuth2 client IDs into the registration record so that
		// DisableFleetNode can revoke them at the IAM layer without a separate lookup.
		regMeta["oauth2_client_id"] = app.ClientID
		if mtlsClientID != "" {
			regMeta["mtls_client_id"] = mtlsClientID
		}
		regReq := &iam.FleetRegistrarRequest{
			NodeID:   appName,
			TenantID: req.TenantID,
			Name:     appName,
			Host:     dialHost,
			// Set only when the node declared it; Host is then the same.
			AdvertisedAddress: strings.TrimSpace(req.AdvertiseAddr),
			Reachability:      string(reachability),
			Backends:          backendsFromRegisterMetadata(req.Metadata),
			NodeType:          "agent-runtime",
			Kind:              "daemon",
			Metadata:          regMeta,
		}
		// A daemon already in the registry is REFRESHED by this call rather
		// than refused (pkg/server fleetRegistrarAdapter), so the failures
		// that reach here are either a refusal about WHO is registering --
		// which the response must state, because the caller is being told it
		// may not have this record -- or an operational fault, which stays
		// non-fatal: the daemon leaves with credentials it can use and the
		// heartbeat recovery path re-publishes it.
		if regErr := registrar.RegisterFleetNode(ctx, regReq); regErr != nil {
			if status, code, refused := fleetRegistrarRefusal(regErr); refused {
				respondFleetEnrollError(w, r, status, code, regErr.Error())
				return
			}
			s.logger.Warnf("agent register: registrar failed for agent_id=%s: %v (the heartbeat recovery path will re-publish it)", appName, regErr)
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
	// dialable target from the fleet node's perspective).
	ingestAddr := dialableEventIngestAddr(s.eventIngestAddr(), r)

	RespondJSON(w, r, http.StatusCreated, &fleetEnrollResponse{
		ClientID:         app.ClientID,
		ClientSecret:     app.ClientSecret, // plaintext — only returned here
		AgentID:          appName,
		ServiceAccountID: appName,
		Role:             orchestration.FleetRuntimeRole,
		Scopes:           iam.FleetRuntimeGrant(),
		EventIngestAddr:  ingestAddr,
		MTLSClientID:     mtlsClientID,
	})
}

// fleetNodeDialAddress returns the address the control plane registers an
// enrolling node under and what it learned about reaching it.
//
// The node's advertised address wins when it declared one. Otherwise the
// address is derived from its bind address: a bind of 0.0.0.0:<port> or
// [::]:<port> can't be dialed as-is, so the host component is rewritten from
// the certificate or the self-reported hostname so the RemoteACPBackend
// dispatcher can reach it over the network.
//
// With a prober wired, an address the control plane cannot resolve and dial
// is refused with its *orchestration.AgentAddressUnreachableError; without
// one the address is registered as unverified.
func (s *Server) fleetNodeDialAddress(ctx context.Context, req *fleetEnrollRequest, peer *x509.Certificate) (string, orchestration.FleetNodeReachability, error) {
	address := strings.TrimSpace(req.AdvertiseAddr)
	if address == "" {
		address = dialableFleetNodeHost(req.BindAddr, req.Hostname, peer)
	}
	prober := s.fleetAddressProberOrNil()
	if prober == nil {
		return address, orchestration.FleetNodeReachabilityUnverified, nil
	}
	if err := prober.Probe(ctx, address); err != nil {
		return "", "", err
	}
	return address, orchestration.FleetNodeReachable, nil
}

// extractFleetNodePeerCertificate pulls the mTLS peer certificate from the
// request's TLS state and asserts the cert carries the required OU and
// EKU stamps. Any missing/invalid value is surfaced as a typed error.
func extractFleetNodePeerCertificate(r *http.Request) (*x509.Certificate, error) {
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
		if ou == fleetRegistrarOU {
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
// by the fleet node deploy service when it pre-computes an expected
// AgentID for wait-online polling.
func sanitizeAppName(cn string) string {
	return iam.SanitizeAppName(cn)
}

// backendsFromRegisterMetadata returns the daemon's declared backends from the
// registration request metadata. Callers pass a comma-separated list under the
// "backends" key.
//
// An absent or empty key yields nil, and the record is written naming no
// backend. That is not a placeholder waiting to be filled in: the heartbeat
// DOES reconcile a record's backend axis now (go-quicbaas#699,
// orchestration.FleetNodeObservation), but it refuses an observation that reports
// no served backend, so a daemon that advertises nothing here stays
// advertising nothing until it serves something. A daemon whose runtime
// constructed no backend can honour no task, and the empty record is how that
// fault stays visible (go-quicbaas#681).
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

// fleetRegistrarRefusal classifies a fleet-registry failure that is a refusal
// ABOUT THE CALLER rather than an operational fault, returning the status and
// error code the registration must answer with.
//
// Both refusals are answers to "may this certificate have this record?", and
// both must reach the daemon: a fleet node ID whose record belongs to a different
// principal is an identity takeover -- the ID is a SANITISED derivation of the
// certificate CommonName (iam.SanitizeAppName folds space, slash, colon and
// backslash to "-"), so two distinct subjects can address one record -- and an
// fleet node an operator disabled must not re-enroll its way back into service.
//
// Everything else is operational and stays non-fatal to the registration.
func fleetRegistrarRefusal(err error) (status int, code string, refused bool) {
	switch {
	case errors.Is(err, orchestration.ErrFleetNodePrincipalMismatch):
		return http.StatusConflict, "agent_id_conflict", true
	case errors.Is(err, orchestration.ErrFleetNodeDisabled):
		return http.StatusForbidden, "agent_disabled", true
	default:
		return 0, "", false
	}
}

// mergeRegisterMetadata produces the registry metadata we stamp on the
// registration record. Includes the daemon's sandbox policy and
// certificate fingerprint so operators auditing the registry can trace
// a row back to the issuing cert.
func mergeRegisterMetadata(req *fleetEnrollRequest, cert *x509.Certificate) map[string]string {
	out := map[string]string{
		"sandbox_policy": req.SandboxPolicy,
		"hostname":       req.Hostname,
	}
	if cert != nil && cert.SerialNumber != nil {
		out["cert_serial"] = cert.SerialNumber.Text(16)
	}
	if cert != nil && cert.Subject.CommonName != "" {
		// The record's OWNER. A re-registration may refresh this record only
		// when it presents the same principal, so this is load-bearing rather
		// than decorative (orchestration.FleetNodePrincipalMetadataKey).
		out[orchestration.FleetNodePrincipalMetadataKey] = cert.Subject.CommonName
	}
	if cert != nil {
		// The KEY that principal presented, in the stack's one canonical mTLS
		// identity algorithm -- the same thumbprint #674 binds onto the
		// enrolled user record. Refreshed on every registration and never
		// compared: a re-keyed renewal legitimately changes it, which is
		// exactly why EnrollFleetNodeCertIdentity re-pins the binding instead of
		// checking it.
		out[orchestration.FleetNodePrincipalKeyMetadataKey] = qrdbsdk.ComputeSPKIThumbprint(cert)
	}
	for k, v := range req.Metadata {
		if _, exists := out[k]; !exists {
			out[k] = v
		}
	}
	return out
}

// registerMTLSApp creates a secondary OAuth2 application pinned to the fleet node's
// mTLS peer certificate using self_signed_tls_client_auth (RFC 8705 §2.2).
// On success it returns the new client_id; on failure it logs and returns "".
// Failure is non-fatal: the fleet node falls back to the legacy client_secret flow.
func registerMTLSApp(ctx context.Context, log interface {
	Warnf(format string, args ...interface{})
	Infof(format string, args ...interface{})
}, accessor fleetAppManagerAccessor, tenantID, baseAppName string, peer *x509.Certificate) string {
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
		Scopes:                  iam.FleetRuntimeGrant(),
		TokenEndpointAuthMethod: iam.AuthMethodSelfSignedTLSClientAuth,
		MTLSCertThumbprint:      thumbprint,
	}
	createErr := appStorage.CreateApp(ctx, mtlsApp)
	if createErr == nil {
		log.Infof("agent register: self_signed_tls_client_auth app registered: client_id=%s thumbprint=%s", mtlsAppName, thumbprint)
		return mtlsAppName
	}
	// coded-sentinel-ok: the whole already-exists class is the intent. The mTLS
	// client id is derived from the fleet node identity, so an already-present
	// record under it is this fleet node's own from an earlier enrollment; every
	// other failure class is the operational one this branch reports.
	if !errors.Is(createErr, iam.ErrAlreadyExists) {
		log.Warnf("agent register: failed to register self_signed_tls_client_auth app %s: %v", mtlsAppName, createErr)
		return ""
	}

	// The client id is derived from the fleet node identity, so a repeat enrollment
	// finds its own record. Re-pin it to the certificate the daemon is
	// presenting NOW -- a renewed certificate has a different thumbprint, and
	// leaving the old one pinned locks the daemon out of /oauth2/token-mtls --
	// and hand the id back, so the response is not silently missing the mTLS
	// credential on every registration after the first (go-quicbaas#644).
	if updateErr := appStorage.UpdateApp(ctx, mtlsApp); updateErr != nil {
		log.Warnf("agent register: failed to re-pin self_signed_tls_client_auth app %s: %v", mtlsAppName, updateErr)
		return ""
	}
	log.Infof("agent register: self_signed_tls_client_auth app re-pinned: client_id=%s thumbprint=%s", mtlsAppName, thumbprint)
	return mtlsAppName
}

// assertCertAuthenticatesInto verifies the certificate authenticates into
// tenantID — the tenant whose IAM the enrolled identity is written to.
//
// go-qrdb derives an mTLS identity's tenant from the certificate's
// tenant-scoped SPIFFE SAN, falling back to the first Organizational Unit when
// there is none (pkg/auth MTLSAuthenticator.TenantFromCert). This endpoint
// requires OU=fleet nodes, so that fallback can never name a tenant: a certificate
// carrying no SPIFFE SAN authenticates into a tenant called "agents" and its
// principal is unresolvable wherever this enrollment writes it. Both
// conditions are reported rather than papered over.
//
// A malformed SAN fails closed, exactly as qrdb's authenticator does with it.
func assertCertAuthenticatesInto(peer *x509.Certificate, tenantID string) error {
	id, err := qrdbsdk.TenantFromCert(peer, "")
	switch {
	case err == nil:
		if id.TenantID != tenantID {
			return &FleetNodeCertTenantMismatchError{Certificate: id.TenantID, Registering: tenantID}
		}
		return nil
	case errors.Is(err, qrdbsdk.ErrSpiffeNoTenantID):
		return &FleetNodeCertNoSPIFFETenantError{CommonName: peer.Subject.CommonName}
	default:
		return &FleetNodeCertSPIFFEInvalidError{Cause: err}
	}
}

// enrollFleetNodeCertIdentity confers the fleet node runtime grant on the identity the
// daemon's client certificate authenticates as, through the identity store
// whose users and roles ARE go-qrdb's (pkg/server iam_init.go routes both to
// the embedded node). See iam.IdentityStore.EnrollFleetNodeCertIdentity for why a
// certificate principal can hold authority no other way.
func enrollFleetNodeCertIdentity(ctx context.Context, service interface{}, tenantID string, peer *x509.Certificate) error {
	accessor, ok := service.(fleetIdentityStoreAccessor)
	if !ok {
		return errFleetNodeIdentityStoreUnavailable
	}
	store, err := accessor.GetIdentityStore(ctx)
	if err != nil {
		return err
	}
	if store == nil {
		return errFleetNodeIdentityStoreUnavailable
	}
	_, err = store.EnrollFleetNodeCertIdentity(ctx, tenantID, iam.FleetNodeCertIdentityOptions{
		Certificate: peer,
		DisplayName: peer.Subject.CommonName,
		// The ONE definition of what a fleet node daemon may do, shared with the
		// OAuth2 application grant: one daemon, one grant, whichever of its two
		// credentials it presents.
		Permissions: iam.FleetRuntimeGrant(),
	})
	return err
}

// FleetNodeCertTenantMismatchError reports a client certificate whose SPIFFE SAN
// authenticates into a different tenant than the one being registered.
type FleetNodeCertTenantMismatchError struct {
	// Certificate is the tenant the certificate authenticates into.
	Certificate string
	// Registering is the tenant the registration names.
	Registering string
}

// Error implements the error interface.
func (e *FleetNodeCertTenantMismatchError) Error() string {
	return "agent register: certificate authenticates into tenant " + strconv.Quote(e.Certificate) +
		", not the registering tenant " + strconv.Quote(e.Registering)
}

// FleetNodeCertNoSPIFFETenantError reports a client certificate carrying no
// tenant-scoped SPIFFE SAN. Such a certificate authenticates into a tenant
// named by its OU — "agents" on this endpoint — so no enrollment in the
// registering tenant could ever authorize it.
type FleetNodeCertNoSPIFFETenantError struct {
	// CommonName is the certificate subject, for the operator's log.
	CommonName string
}

// Error implements the error interface.
func (e *FleetNodeCertNoSPIFFETenantError) Error() string {
	return "agent register: certificate for " + strconv.Quote(e.CommonName) +
		" carries no tenant-scoped SPIFFE SAN, so it cannot authenticate into the registering tenant"
}

// FleetNodeCertSPIFFEInvalidError reports a malformed SPIFFE SAN. Fails closed,
// matching go-qrdb's authenticator.
type FleetNodeCertSPIFFEInvalidError struct {
	// Cause is the SPIFFE parse failure.
	Cause error
}

// Error implements the error interface.
func (e *FleetNodeCertSPIFFEInvalidError) Error() string {
	return "agent register: certificate SPIFFE identity is invalid: " + e.Cause.Error()
}

// Unwrap exposes the parse failure to errors.Is / errors.As.
func (e *FleetNodeCertSPIFFEInvalidError) Unwrap() error { return e.Cause }

// errFleetNodeIdentityStoreUnavailable indicates the identity store the fleet node's
// certificate principal must be enrolled in is not wired. Registration fails
// closed rather than issuing credentials the control plane will refuse.
var errFleetNodeIdentityStoreUnavailable = errors.New("agent register: IAM identity store not available")

// fleetEnrollErrorBody is the JSON shape for registration error
// responses. It mirrors the OAuth2 error envelope (RFC 6749 §5.2) so
// operators can parse errors with the same code as /oauth2/token.
type fleetEnrollErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// respondFleetEnrollError writes a structured error response.
func respondFleetEnrollError(w http.ResponseWriter, r *http.Request, status int, code, detail string) {
	body := &fleetEnrollErrorBody{Error: code, ErrorDescription: detail}
	w.Header().Set("Cache-Control", "no-store")
	enc, err := json.Marshal(body)
	if err != nil {
		RespondInternalError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(enc)))
	w.WriteHeader(status)
	writeResponseBody(w, r, enc)
}

// Typed errors for fleet node mTLS register.
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

// dialableFleetNodeHost returns a "host:port" string the orchestrator can
// dial to reach the daemon. When bindAddr uses a wildcard host (0.0.0.0
// or [::]) the function rewrites the host component using (in order of
// preference): the peer certificate's CN with the "agent-" prefix
// stripped (the issued cert uses the daemon's LAN IP as the CN — always
// resolvable), the first DNS SAN, and finally the self-reported
// hostname (a short hostname that may or may not resolve centrally).
// Returns bindAddr unchanged when the host is already a concrete IP or
// DNS name.
func dialableFleetNodeHost(bindAddr, hostname string, peer *x509.Certificate) string {
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
// request's Host header instead. The fleet node already proved that Host is
// reachable by reaching this handler; using it for the gRPC dial keeps
// SNI/cert verification consistent with the REST connection.
//
// Returns the empty string when configured is empty so the response
// omits event_ingest_addr (fleet node then disables event forwarding rather
// than dialing a wrong port).
//
// Returns configured unchanged when its host is already a concrete IP
// or DNS name. Returns the empty string when configured is unparseable
// — a malformed configured URL is a server bug; refusing to advertise
// it is safer than returning a value the fleet node will fail to dial.
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
	// endpoint the fleet node already used to reach this handler). Strip
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
var _ = orchestration.FleetNodeStatusRegistered
