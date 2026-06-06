//go:build rest && iam

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
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// recoverLogger is the narrow logging surface the recoverer uses. The
// production *logger.Logger satisfies this trivially via its existing
// Infof/Warnf methods; tests inject a t.Logf-backed adapter without
// depending on the interfaces package.
type recoverLogger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
}

// =============================================================================
// Heartbeat Self-Healing Recovery (iam build tag)
// =============================================================================
//
// Background. The central QuicBaaS daemon keeps the agent registry in
// memory. When the central restarts, every deployed agent's heartbeat
// hits a 404 because the registry has no entry for the agent's ID.
// Forcing the agent to re-enroll on every 404 produces a thundering
// herd, invalidates in-flight tokens, and races other tenant work.
//
// The cleaner contract is: a heartbeat carrying a valid mTLS peer
// certificate from a CA the central trusts is itself an authentication
// of the agent. The certificate's tenant + CN combination
// deterministically maps to the agent's canonical AgentID via
// iam.AgentAppName. When the cert validates, the handler re-publishes
// the agent into the fleet registry from the cert metadata and accepts
// the heartbeat. The agent never sees a 404; the operator sees one
// INFO log line per recovered agent at restart time.
//
// This file implements that recovery. The recoverer is wired in by
// pkg/server lifecycle alongside the existing AgentRegistrar adapter;
// builds without the iam tag never compile this file and the heartbeat
// handler falls through to its original 404 path.

// IAMHeartbeatRecovererDeps bundles the collaborators the recoverer
// needs. All fields are required.
type IAMHeartbeatRecovererDeps struct {
	// Registrar publishes a fresh AgentRegistration into the fleet
	// registry. Reuses the same adapter the mTLS register handler uses
	// so a recovered agent and a freshly-enrolled agent occupy the same
	// row shape.
	Registrar iam.AgentRegistrar

	// Logger is used for the single INFO line per recovery decision so
	// operators can audit which agents the central re-published after a
	// restart. Required.
	Logger recoverLogger
}

// IAMHeartbeatRecoverer implements AgentHeartbeatRecoverer using the
// request's mTLS peer certificate to re-publish a missing agent into
// the fleet registry. Safe for concurrent callers.
type IAMHeartbeatRecoverer struct {
	registrar iam.AgentRegistrar
	logger    recoverLogger
}

// Compile-time interface satisfaction check.
var _ AgentHeartbeatRecoverer = (*IAMHeartbeatRecoverer)(nil)

// NewIAMHeartbeatRecoverer constructs the production recoverer. Returns
// an error when any required dep is nil so a misconfigured server fails
// fast at startup instead of on the first heartbeat.
func NewIAMHeartbeatRecoverer(deps IAMHeartbeatRecovererDeps) (*IAMHeartbeatRecoverer, error) {
	if deps.Registrar == nil {
		return nil, ErrHeartbeatRecoverNoRegistrar
	}
	if deps.Logger == nil {
		return nil, ErrHeartbeatRecoverNoLogger
	}
	return &IAMHeartbeatRecoverer{
		registrar: deps.Registrar,
		logger:    deps.Logger,
	}, nil
}

// Recover walks the recovery decision tree:
//
//  1. Extract & validate the mTLS peer certificate (OU=agents stamp +
//     ClientAuth EKU). Missing/invalid → return false.
//  2. Read the certificate tenant from Subject.Organization. Missing or
//     not matching the supplied tenantID → return false.
//  3. Derive the canonical AgentID from (certTenant, cert CN) via
//     iam.AgentAppName. Mismatch with the supplied agentID → return
//     false (defence: a stolen cert for one daemon must not heartbeat
//     as a different daemon).
//  4. Republish the agent to the fleet registry through the registrar.
//     Failure → return false. Success → log INFO and return true.
//
// Returning false makes the heartbeat handler fall through to its
// original 404 path so the agent's client-side fallback can re-enroll
// explicitly.
func (h *IAMHeartbeatRecoverer) Recover(ctx context.Context, r *http.Request, tenantID, agentID string, payload *heartbeatBody) bool {
	if r == nil || r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return false
	}

	peer, err := validateRecoveryPeerCert(r.TLS.PeerCertificates[0])
	if err != nil {
		h.logger.Warnf("agent heartbeat recover: cert validation failed: agent=%s tenant=%s: %v",
			agentID, tenantID, err)
		return false
	}

	certTenant := strings.TrimSpace(extractCertTenant(peer))
	if certTenant == "" {
		h.logger.Warnf("agent heartbeat recover: peer cert missing tenant claim: agent=%s tenant=%s",
			agentID, tenantID)
		return false
	}
	if certTenant != tenantID {
		h.logger.Warnf("agent heartbeat recover: cert tenant=%q does not match request tenant=%q for agent=%s",
			certTenant, tenantID, agentID)
		return false
	}

	expected := iam.AgentAppName(certTenant, peer.Subject.CommonName)
	if expected != agentID {
		h.logger.Warnf("agent heartbeat recover: cert identity %q does not match agent_id %q (cn=%s tenant=%s)",
			expected, agentID, peer.Subject.CommonName, certTenant)
		return false
	}

	host := recoveredHostFromHeartbeat(payload, peer)
	backends := backendsFromHeartbeat(payload)
	regReq := &iam.AgentRegistrarRequest{
		AgentID:   agentID,
		TenantID:  certTenant,
		Name:      agentID,
		Host:      host,
		Backends:  backends,
		AgentType: "agent-runtime",
		Kind:      "daemon",
		Metadata: map[string]string{
			"cert_cn":     peer.Subject.CommonName,
			"cert_serial": peer.SerialNumber.Text(16),
			"recovered":   "true",
		},
	}
	if err := h.registrar.RegisterAgent(ctx, regReq); err != nil {
		// AlreadyExists from a concurrent recovery is benign — another
		// goroutine just re-published the same agent. Treat it as a
		// successful recovery so the retry hits the live entry.
		if isAgentAlreadyExistsError(err) {
			h.logger.Infof("agent heartbeat recover: race lost — peer registrar already published agent=%s tenant=%s",
				agentID, certTenant)
			return true
		}
		h.logger.Warnf("agent heartbeat recover: registrar.RegisterAgent failed: agent=%s tenant=%s: %v",
			agentID, certTenant, err)
		return false
	}

	h.logger.Infof("agent re-registered via heartbeat: id=%s tenant=%s reason=registry-lost",
		agentID, certTenant)
	return true
}

// validateRecoveryPeerCert mirrors the OU + ClientAuth EKU stamps the
// mTLS register handler enforces, so a cert that was minted for any
// other purpose cannot drive a recovery. Returns the cert verbatim on
// success.
func validateRecoveryPeerCert(peer *x509.Certificate) (*x509.Certificate, error) {
	if peer == nil {
		return nil, ErrHeartbeatRecoverNoPeerCert
	}
	hasOU := false
	for _, ou := range peer.Subject.OrganizationalUnit {
		if ou == agentRegistrarOU {
			hasOU = true
			break
		}
	}
	if !hasOU {
		return nil, ErrHeartbeatRecoverInvalidPeerOU
	}
	hasClientAuth := false
	for _, usage := range peer.ExtKeyUsage {
		if usage == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
			break
		}
	}
	if !hasClientAuth {
		return nil, ErrHeartbeatRecoverInvalidPeerEKU
	}
	return peer, nil
}

// recoveredHostFromHeartbeat returns the dialable host:port for the
// recovered agent. Preference order:
//
//  1. The bind_addr the agent supplied in the heartbeat body. The
//     daemon binds to a wildcard host (0.0.0.0 / [::]) by default;
//     when that's the case we substitute the cert CN's IP so the
//     dispatcher has a routable destination.
//  2. The cert CN with the "agent-" prefix stripped, port 0. Used
//     when the agent's heartbeat omits bind_addr (older daemon).
//
// Returning a port-0 entry is a graceful fallback that lets the
// registry row exist; the agent's next normal heartbeat (which DOES
// supply bind_addr) corrects the row.
func recoveredHostFromHeartbeat(payload *heartbeatBody, peer *x509.Certificate) string {
	if payload != nil && strings.TrimSpace(payload.BindAddr) != "" {
		return rewriteWildcardHost(payload.BindAddr, peer)
	}
	return recoveredHostFromCert(peer)
}

// rewriteWildcardHost replaces a 0.0.0.0 / [::] host component with a
// cert-derived dialable address while preserving the bound port. The
// preference order is:
//   - Cert CN with the optional "agent-" prefix stripped (the deploy
//     issuer stamps the daemon's LAN IP / hostname there).
//   - First DNS SAN.
//
// Returns bindAddr unchanged when no derivation succeeds.
func rewriteWildcardHost(bindAddr string, peer *x509.Certificate) string {
	host, port, err := splitHostPortSafe(bindAddr)
	if err != nil {
		return bindAddr
	}
	if !isWildcardHost(host) {
		return bindAddr
	}
	if peer != nil {
		if cn := strings.TrimSpace(peer.Subject.CommonName); cn != "" {
			cn = strings.TrimPrefix(cn, "agent-")
			return cn + ":" + port
		}
		if len(peer.DNSNames) > 0 {
			return peer.DNSNames[0] + ":" + port
		}
	}
	return bindAddr
}

// splitHostPortSafe is a thin wrapper that returns "" / err on
// unparsable input. Kept here (instead of inlining net.SplitHostPort)
// so the recovery decision tree reads cleanly.
func splitHostPortSafe(addr string) (string, string, error) {
	h, p, err := netSplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return h, p, nil
}

// netSplitHostPort is a function variable so tests can stub it. The
// production binding is net.SplitHostPort.
var netSplitHostPort = func(addr string) (string, string, error) {
	return net.SplitHostPort(addr)
}

// backendsFromHeartbeat returns the backend list the daemon advertised
// in the heartbeat body. Reused from the heartbeat payload so the
// recovered registry row's Backends field matches what dispatch
// expects to filter on.
func backendsFromHeartbeat(payload *heartbeatBody) []string {
	if payload == nil || len(payload.Backends) == 0 {
		return nil
	}
	out := make([]string, 0, len(payload.Backends))
	for _, b := range payload.Backends {
		if t := strings.TrimSpace(b); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// recoveredHostFromCert returns the "host:port" the orchestrator can
// dial to reach the recovered agent when the heartbeat body does NOT
// supply a bind_addr (legacy agents predating the bind_addr-in-body
// rollout). We derive the host from the cert: the deploy issuer stamps
// the daemon's LAN IP / hostname as the Subject.CommonName (the
// "agent-" prefix is then stripped), or falls back to the first DNS
// SAN. The port is set to defaultRecoveryAgentPort — the documented
// default the agent serve command binds to — so the orchestrator
// dispatcher can route inference RPCs to a recovered legacy agent
// without waiting for the operator to redeploy the agent binary.
//
// New agents send bind_addr on every tick; recoveredHostFromHeartbeat
// honours that path verbatim and bypasses this fallback.
func recoveredHostFromCert(peer *x509.Certificate) string {
	if peer == nil {
		return ""
	}
	if cn := strings.TrimSpace(peer.Subject.CommonName); cn != "" {
		cn = strings.TrimPrefix(cn, "agent-")
		return cn + ":" + defaultRecoveryAgentPort
	}
	if len(peer.DNSNames) > 0 {
		return peer.DNSNames[0] + ":" + defaultRecoveryAgentPort
	}
	return ""
}

// defaultRecoveryAgentPort is the gRPC port we assume an agent is
// listening on when the heartbeat body does not advertise bind_addr.
// Matches the default the `quicbaas agent serve --bind` flag uses
// (see cmd/quicbaas/agent_serve.go). Operators who run the agent on a
// non-default port get the correct value once the agent is upgraded
// to a binary that includes bind_addr in its heartbeat body — the
// next heartbeat re-publishes the row through the same recovery path
// (idempotent register-on-heartbeat) with the correct port.
const defaultRecoveryAgentPort = "9091"

// isAgentAlreadyExistsError reports whether err is the registrar's
// idempotent "already published" sentinel. We treat it as a successful
// recovery — another goroutine got there first.
func isAgentAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	var existing *orchestration.AgentAlreadyExistsError
	return errors.Is(err, orchestration.ErrAgentRegistryAlreadyExists) || errors.As(err, &existing)
}

// =============================================================================
// Typed Errors
// =============================================================================

// ErrHeartbeatRecoverNoRegistrar indicates NewIAMHeartbeatRecoverer was
// called without a registrar. Surfacing it at startup time stops the
// server from booting in a state where heartbeat recovery would
// silently no-op.
var ErrHeartbeatRecoverNoRegistrar = errors.New("agent heartbeat recover: registrar required")

// ErrHeartbeatRecoverNoLogger indicates NewIAMHeartbeatRecoverer was
// called without a logger.
var ErrHeartbeatRecoverNoLogger = errors.New("agent heartbeat recover: logger required")

// ErrHeartbeatRecoverNoPeerCert indicates the request did not present
// any mTLS peer certificate; recovery cannot authenticate the agent.
var ErrHeartbeatRecoverNoPeerCert = errors.New("agent heartbeat recover: missing mTLS peer certificate")

// ErrHeartbeatRecoverInvalidPeerOU indicates the peer cert is missing
// the canonical "agents" OU stamp.
var ErrHeartbeatRecoverInvalidPeerOU = errors.New("agent heartbeat recover: peer certificate missing required OU=agents stamp")

// ErrHeartbeatRecoverInvalidPeerEKU indicates the peer cert is missing
// the ClientAuth Extended Key Usage.
var ErrHeartbeatRecoverInvalidPeerEKU = errors.New("agent heartbeat recover: peer certificate missing ClientAuth EKU")

// ErrHeartbeatRecoverIdentityMismatch indicates the cert's
// (tenant, CN) pair does not derive the supplied agentID via
// iam.AgentAppName — recovery refuses any cert that does not own the
// claimed identity.
var ErrHeartbeatRecoverIdentityMismatch = errors.New("agent heartbeat recover: peer certificate identity does not match agent ID")
