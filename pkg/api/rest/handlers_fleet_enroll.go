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
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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

// fleetNodeSPIFFEKind is the SPIFFE workload kind of an agent certificate: a
// fleet node is a machine identity, and the agent issuer stamps go-qrdb's
// service kind (pkg/service XKMSFleetNodeCertIssuer). The kind, not the subject
// OU, is what marks a certificate as an agent's: go-xkms v0.57 stamps the
// tenant as the first OU of every certificate it signs (CA m779 62(a)).
const fleetNodeSPIFFEKind = qrdbsdk.SpiffeKindService

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
// issued by the server's CA. This handler reads the tenant from the
// certificate's first OU, verifies the certificate is an agent's
// (fleetNodeCertTenant) and carries the ClientAuth EKU, and mints OAuth2
// client credentials the daemon uses for heartbeat and event-forwarding from
// that point on.
//
// The request body's tenant_id is cross-checked against the certificate's
// tenant to prevent a valid certificate from registering for a tenant the
// operator did not intend.
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

	// The certificate's OU carries its tenant, and go-qrdb authenticates it
	// into that tenant (or into its SPIFFE SAN's, which must agree). It must be
	// the tenant being registered, or the identity enrolled below is written
	// where the authenticator never looks, and a valid certificate could
	// register a daemon for an unintended tenant.
	certTenant, err := fleetNodeCertTenant(peer, s.config.SPIFFETrustDomain)
	if err != nil {
		var notAgent *FleetNodeCertNotAgentError
		var noSPIFFE *FleetNodeCertNoSPIFFEError
		var clusterOU *FleetNodeCertOUIsTrustDomainError
		if errors.Is(err, errInvalidPeerOU) || errors.As(err, &notAgent) || errors.As(err, &noSPIFFE) || errors.As(err, &clusterOU) {
			respondFleetEnrollError(w, r, http.StatusUnauthorized, "invalid_client", err.Error())
			return
		}
		respondFleetEnrollError(w, r, http.StatusForbidden, "tenant_mismatch", err.Error())
		return
	}
	if certTenant != req.TenantID {
		mismatch := &FleetNodeCertTenantMismatchError{Certificate: certTenant, Registering: req.TenantID}
		respondFleetEnrollError(w, r, http.StatusForbidden, "tenant_mismatch", mismatch.Error())
		return
	}

	// Stopgap until go-quicbaas#1821: only a certificate the fleet issuer
	// recorded for this tenant enrolls. A server with no record store fails
	// closed rather than skipping the check.
	records := s.fleetNodeCertRecordsOrNil()
	if records == nil {
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", "fleet node certificate records not available")
		return
	}
	if err := records.VerifyFleetNodeCertificate(ctx, req.TenantID, peer); err != nil {
		var notRecorded *service.FleetNodeCertNotRecordedError
		if errors.As(err, &notRecorded) {
			respondFleetEnrollError(w, r, http.StatusForbidden, "certificate_not_recorded", notRecorded.Error())
			return
		}
		respondFleetEnrollError(w, r, http.StatusInternalServerError, "server_error", "fleet node certificate record lookup failed")
		return
	}

	// The address is checked before anything is issued: a node the control
	// plane cannot dial would be placed work it can never be sent, and every
	// call routed to it would fail far from here (go-quicbaas#795).
	dialHost, reachability, err := s.fleetNodeDialAddress(ctx, &req, peer)
	if err != nil {
		code := "agent_address_unreachable"
		if errors.Is(err, orchestration.ErrAgentAddressLoopbackInCluster) {
			code = "agent_address_loopback_in_cluster"
		}
		respondFleetEnrollError(w, r, http.StatusUnprocessableEntity, code, err.Error())
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
	// A loopback address passes the probe on the node hosting the agent, but
	// no other node of a cluster can dial it.
	if s.fleetControlPlaneClustered() {
		if err := orchestration.RefuseLoopbackInCluster(address); err != nil {
			return "", "", err
		}
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
// request's TLS state and asserts the cert carries the ClientAuth EKU. Any
// missing/invalid value is surfaced as a typed error.
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

// extractCertTenant returns the tenant ID carried in the peer certificate's
// first Organizational Unit, where every layer puts it (DS-1899) and go-xkms
// v0.57 stamps it on every certificate it signs (CA m779 62(a)). Empty return
// means the claim is missing and registration must be rejected.
func extractCertTenant(cert *x509.Certificate) string {
	if cert == nil || len(cert.Subject.OrganizationalUnit) == 0 {
		return ""
	}
	return strings.TrimSpace(cert.Subject.OrganizationalUnit[0])
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

// fleetNodeCertTenant returns the tenant an agent certificate authenticates
// into, refusing a certificate that is not an agent's.
//
// The tenant is the first subject OU (extractCertTenant). go-qrdb
// authenticates an mTLS identity into a tenant only when that OU and the trust
// domain of its SPIFFE SAN, spiffe://<tenant>/<kind>/<name>, agree (sdk
// TenantFromClientCert). A SAN, when present, must therefore name the same
// tenant as the OU, and the agent kind the agent issuer stamps: a SAN of
// another kind is a certificate the CA minted for another role. The SAN's kind
// is the proof that the certificate is an agent's (CA m779, ruling 62(a)), so a
// certificate with no SPIFFE SAN is refused: its OU alone names a tenant, not
// an agent, and any tenant client certificate carries one. A malformed SAN
// fails closed, exactly as go-qrdb's authenticator does with it.
//
// clusterTrustDomain is the cluster's own SPIFFE trust domain
// (iam.spiffe_trust_domain). It is never a tenant: go-qrdb refuses to
// authenticate a certificate whose OU names it, so it is refused here before
// any grant is written. Empty skips the check, as go-qrdb does.
func fleetNodeCertTenant(peer *x509.Certificate, clusterTrustDomain string) (string, error) {
	tenantID := extractCertTenant(peer)
	if tenantID == "" {
		return "", errInvalidPeerOU
	}
	if clusterTrustDomain != "" && tenantID == clusterTrustDomain {
		return "", &FleetNodeCertOUIsTrustDomainError{OU: tenantID}
	}
	id, err := qrdbsdk.WorkloadFromCert(peer)
	switch {
	case errors.Is(err, qrdbsdk.ErrSpiffeNoTenantID):
		return "", &FleetNodeCertNoSPIFFEError{OU: tenantID}
	case err != nil:
		return "", &FleetNodeCertSPIFFEInvalidError{Cause: err}
	case id.Kind != fleetNodeSPIFFEKind:
		return "", &FleetNodeCertNotAgentError{Kind: id.Kind}
	case id.TrustDomain != tenantID:
		return "", &FleetNodeCertOUTenantMismatchError{OU: tenantID, SPIFFE: id.TrustDomain}
	}
	return tenantID, nil
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

// FleetNodeCertTenantMismatchError reports a client certificate that
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

// FleetNodeCertNotAgentError reports a client certificate whose SPIFFE SAN
// names a workload kind other than the agent kind: the CA minted it for
// another role, so it may not register a fleet node.
type FleetNodeCertNotAgentError struct {
	// Kind is the SPIFFE workload kind the certificate carries.
	Kind string
}

// Error implements the error interface.
func (e *FleetNodeCertNotAgentError) Error() string {
	return "agent register: certificate SPIFFE kind " + strconv.Quote(e.Kind) +
		" is not the agent kind " + strconv.Quote(fleetNodeSPIFFEKind)
}

// FleetNodeCertNoSPIFFEError reports a client certificate that carries no
// SPIFFE SAN. Without a SAN it has no workload kind, so nothing proves it is
// an agent's: a tenant OU alone is on every client certificate of the tenant.
type FleetNodeCertNoSPIFFEError struct {
	// OU is the tenant the certificate's first Organizational Unit names.
	OU string
}

// Error implements the error interface.
func (e *FleetNodeCertNoSPIFFEError) Error() string {
	return "agent register: certificate for tenant " + strconv.Quote(e.OU) +
		" carries no SPIFFE SAN, so it does not prove the agent kind " + strconv.Quote(fleetNodeSPIFFEKind)
}

// FleetNodeCertOUTenantMismatchError reports a client certificate whose OU
// tenant and SPIFFE SAN tenant disagree. go-qrdb authenticates it into the
// SAN's tenant, so the OU's claim is not the identity it logs in as.
type FleetNodeCertOUTenantMismatchError struct {
	// OU is the tenant the certificate's first Organizational Unit names.
	OU string
	// SPIFFE is the tenant the certificate's SPIFFE SAN names.
	SPIFFE string
}

// Error implements the error interface.
func (e *FleetNodeCertOUTenantMismatchError) Error() string {
	return "agent register: certificate OU tenant " + strconv.Quote(e.OU) +
		" does not match its SPIFFE tenant " + strconv.Quote(e.SPIFFE)
}

// FleetNodeCertOUIsTrustDomainError reports a client certificate whose OU
// names the cluster's own SPIFFE trust domain. That domain is never a tenant:
// go-qrdb refuses to authenticate such a certificate, so a fleet node enrolled
// with it would hold a grant on an identity that can never log in.
type FleetNodeCertOUIsTrustDomainError struct {
	// OU is the certificate's first Organizational Unit, which equals the
	// cluster trust domain.
	OU string
}

// Error implements the error interface.
func (e *FleetNodeCertOUIsTrustDomainError) Error() string {
	return "agent register: certificate OU " + strconv.Quote(e.OU) +
		" is the cluster trust domain, which names no tenant"
}

// Unwrap exposes go-qrdb's cause, so errors.Is matches the refusal go-qrdb's
// authenticator gives the same certificate.
func (e *FleetNodeCertOUIsTrustDomainError) Unwrap() error {
	return qrdbsdk.ErrClientCertificateTenantIsClusterTrustDomain
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

	// errInvalidPeerOU indicates the peer certificate carries no tenant in
	// its first Organizational Unit.
	errInvalidPeerOU = errors.New("agent register: peer certificate missing the tenant OU")

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
