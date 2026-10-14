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
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// PIVLogin handles POST /api/v1/auth/login/piv
//
// This endpoint performs mTLS-based PIV smartcard authentication. The client
// must present a valid TLS client certificate. The certificate's SHA-256
// thumbprint (hex of the cert DER) is matched against a registered user's
// PIV-attested cert bindings (user.CertBindings) via the PIV thumbprint index.
//
// Gate: the endpoint returns 501 when PIV login is not enabled in configuration.
// To enable, set IAM.PIVClientCert.Enabled = true and configure the TLS listener
// with ClientAuth: tls.RequestClientCert.
func (h *IAMHandler) PIVLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Guard: reject when PIV login is not enabled in operator config.
	if !h.server.config.PIVClientCert.Enabled {
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrPIVLoginDisabled, "PIV login is disabled; set iam.piv_login.enabled: true to enable"))
		return
	}

	// Require a TLS client certificate (set by VerifyClientCertIfGiven on the TLS listener).
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrClientCertRequired, "client certificate required for PIV login"))
		return
	}

	cert := r.TLS.PeerCertificates[0]

	// Compute BOTH PIV lookup thumbprints. The primary is the canonical SPKI
	// thumbprint (SHA-256 over SubjectPublicKeyInfo, base64url) qrdb's
	// UserByCertThumbprint resolves; the fallback is the legacy hex SHA-256 of
	// the certificate DER, which qrdb's same cert-thumbprint index resolves.
	// Resolution tries SPKI first, then legacy — mirroring the
	// login-service mTLS primary + alt-lookup order — so a renewed cert (same
	// key, new DER) resolves via the stable SPKI thumbprint while persisted
	// legacy-only bindings keep resolving via the fallback.
	spkiThumbprint := qrdbsdk.ComputeSPKIThumbprint(cert)
	legacyDigest := sha256.Sum256(cert.Raw)
	legacyThumbprint := hex.EncodeToString(legacyDigest[:])
	// thumbprint is the value carried into audit records; the SPKI thumbprint is
	// the canonical identifier.
	thumbprint := spkiThumbprint

	// Defense-in-depth: verify the issuing CA's Subject DN against the
	// operator-configured TrustedIssuers list. Empty list = permissive.
	if trustedIssuers := h.server.config.PIVClientCert.TrustedIssuers; len(trustedIssuers) > 0 {
		issuerDN := cert.Issuer.String()
		issuerLower := strings.ToLower(issuerDN)
		allowed := false
		for _, dn := range trustedIssuers {
			if strings.EqualFold(dn, issuerLower) {
				allowed = true
				break
			}
		}
		if !allowed {
			h.logPIVLoginFailureAudit(ctx, "", thumbprint, ErrPIVCertIssuerRejected,
				"issuer DN \""+issuerDN+"\" not in trusted_issuers", r)
			RespondIAMError(w, r, http.StatusUnauthorized,
				NewIAMAPIError(ErrPIVCertIssuerRejected, "PIV certificate issuer is not trusted"))
			return
		}
	}

	// Resolve the identity store to look up the user by thumbprint.
	identityGetter, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityGetter.GetIdentityStore(ctx)
	if err != nil || store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not configured"))
		return
	}

	// Derive the login tenant before the index lookup: SPIFFE SAN (pinned
	// trust domain) -> home-realm discovery -> fail closed. Injecting the
	// tenant into the context scopes the tenant-scoped PIV index lookup that
	// follows. A non-bootstrap login is NEVER defaulted to the system tenant.
	tenantID, err := h.server.deriveLoginTenant(ctx, r, cert, "")
	if err != nil {
		h.logPIVLoginFailureAudit(ctx, "", thumbprint, ErrUnauthorized,
			"login tenant could not be resolved: "+err.Error(), r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "login tenant could not be resolved"))
		return
	}
	ctx = loginContextWithTenant(ctx, tenantID)

	// SPKI first, then legacy hex digest. Either resolves the same user through
	// qrdb's cert-thumbprint index; an unknown thumbprint fails closed.
	user, err := store.GetUserByPIVThumbprint(ctx, spkiThumbprint)
	if err != nil || user == nil {
		user, err = store.GetUserByPIVThumbprint(ctx, legacyThumbprint)
	}
	if err != nil || user == nil {
		h.logPIVLoginFailureAudit(ctx, "", thumbprint, ErrUnauthorized,
			"no user found for the presented certificate", r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "no user found for the presented certificate"))
		return
	}
	// Defense-in-depth: the resolved user must belong to the derived tenant.
	if user.TenantID != tenantID {
		h.logPIVLoginFailureAudit(ctx, strconv.FormatUint(user.ID, 10), thumbprint, ErrUnauthorized,
			"resolved user tenant does not match derived login tenant", r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, "no user found for the presented certificate"))
		return
	}

	// Resolve OIDC provider for token issuance.
	oidcGetter, ok := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}

	oidcProvider, err := oidcGetter.GetOIDCProvider(ctx)
	if err != nil || oidcProvider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not configured"))
		return
	}

	// Establish — rather than assert — that this certificate came from a
	// smartcard. Until this call the handler issued acr=hardware-attested and
	// attest_level=hardware for ANY TLS client certificate whose thumbprint
	// matched a binding; a self-signed software key produced a token
	// indistinguishable from one backed by a YubiKey.
	attestation, attestErr := h.verifyPIVAttestation(r.TLS.PeerCertificates)
	if attestErr != nil {
		// The DETAIL goes to the audit sink, which is operator-only: the
		// configured attestation_ca_file path, the parser diagnostic, the
		// subject and issuer of the presented certificate. An operator
		// debugging a rejected security key needs all of it.
		//
		// The RESPONSE says only that verification failed. This endpoint is
		// reachable by anyone who can complete a TLS handshake, and the
		// verifier's errors describe the SERVER: construction failures carry
		// configured filesystem paths and parser output, and chain failures
		// carry trust-store contents. Returning them verbatim let an
		// unauthenticated caller enumerate deployment layout one request at a
		// time, using nothing but certificates they minted themselves.
		//
		// The error CODE is unchanged, so a client that branches on
		// piv_cert_issuer_rejected keeps working; only the free-text message
		// stops describing the server's insides.
		h.logPIVLoginFailureAudit(ctx, user.TenantID, thumbprint, ErrPIVCertIssuerRejected,
			"PIV attestation failed: "+attestErr.Error(), r)
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrPIVCertIssuerRejected, ErrMsgPIVAttestationFailed))
		return
	}

	// The ACR follows the evidence. An operator who opted into
	// allow_unattested still gets a working login, but the token says what was
	// actually proven, so a policy requiring hardware refuses it rather than
	// being fooled by a label the handler chose.
	//
	// The amr follows the evidence for the same reason. Nothing on the
	// unattested path proved a card was involved: what happened is an ordinary
	// mutual-TLS authentication with a client certificate whose thumbprint
	// matched a registered binding, which is what "mtls" says here and on a
	// service-account login. Saying "smartcard" would tell a relying party a
	// token had been tapped in a deployment that opted out of checking.
	acr := ""
	amr := []string{"mtls"}
	if attestation.IsHardware() {
		acr = iam.ACRHardwareAttested
		amr = []string{"smartcard", "mfa"}
	}

	tokens, err := oidcProvider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
		User:        user,
		AMR:         amr,
		ACR:         acr,
		Attestation: attestation,
	})
	if err != nil {
		h.logPIVLoginFailureAudit(ctx, user.TenantID, thumbprint, ErrInternalError,
			"token issuance failed: "+err.Error(), r)
		h.server.handleServiceError(w, r, err)
		return
	}

	// Emit success audit event.
	h.logPIVLoginAudit(ctx, user.TenantID, strconv.FormatUint(user.ID, 10), user.Email, thumbprint,
		cert.Subject.String(), cert.Issuer.String(), "smartcard",
		string(attestation.Level()), r)

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"access_token":  tokens.AccessToken,
		"id_token":      tokens.IDToken,
		"refresh_token": tokens.RefreshToken,
		"token_type":    tokens.TokenType,
		"expires_in":    tokens.ExpiresIn,
	})
}

// verifyPIVAttestation establishes what the presented certificate chain proves
// about where its private key lives.
//
// peers is the TLS peer chain as presented: the leaf is the credential being
// authenticated, and the remainder are chain material the card supplied — on a
// YubiKey, the slot-F9 attestation certificate. They are passed as
// intermediates, never as trust anchors: material supplied by the party under
// verification can only help complete a path to a root the OPERATOR chose.
//
// When no attestation CA is configured the login is refused, unless the
// operator explicitly opted into allow_unattested — in which case a
// non-hardware result is returned so the caller can issue a token that says so.
func (h *IAMHandler) verifyPIVAttestation(peers []*x509.Certificate) (*iam.AttestationResult, error) {
	cfg := h.server.config.PIVClientCert

	verifier, err := h.pivAttestationVerifier(cfg)
	if err != nil {
		// allow_unattested covers the deployment that DECIDED not to verify —
		// it does not cover a deployment that meant to and got the
		// configuration wrong. A CA file that was named but cannot be read or
		// parsed is refused either way: silently downgrading there would turn a
		// typo into a fleet-wide loss of hardware assurance, which is precisely
		// the class of silent downgrade this work removes.
		if cfg.AllowUnattested && cfg.AttestationCAFile == "" {
			return iam.UnattestedPIVCredential(), nil
		}
		return nil, err
	}

	var intermediates []*x509.Certificate
	if len(peers) > 1 {
		intermediates = peers[1:]
	}
	attestation, verifyErr := verifier.Verify(peers[0], intermediates)
	if verifyErr != nil {
		if cfg.AllowUnattested {
			return iam.UnattestedPIVCredential(), nil
		}
		return nil, verifyErr
	}
	return attestation, nil
}

// pivAttestationVerifier resolves the process-wide PIV attestation verifier,
// building it once from operator configuration. Rebuilding it per request would
// re-read and re-parse the CA bundle on every login; the trust anchors do not
// change without a restart.
func (h *IAMHandler) pivAttestationVerifier(cfg PIVClientCertConfig) (*iam.PIVAttestationVerifier, error) {
	h.server.pivAttestationOnce.Do(func() {
		h.server.pivAttestation, h.server.pivAttestationErr =
			iam.NewPIVAttestationVerifierFromFiles(cfg.AttestationCAFile, cfg.AttestationIntermediatesFile)
	})
	return h.server.pivAttestation, h.server.pivAttestationErr
}

// logPIVLoginAudit emits a success audit event for a PIV smartcard login.
// Emits event_type "auth.login.piv.success".
//
// attestLevel is what the presented certificate actually PROVED, as distinct
// from attestMethod, which names the credential kind. The record used to carry
// only the latter, so an audit trail could not tell a login backed by a
// verified smartcard from one an operator had opted into accepting unattested.
func (h *IAMHandler) logPIVLoginAudit(ctx context.Context, tenantID, userID, email, certThumbprint, certSubject, certIssuer, attestMethod, attestLevel string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	requestID := r.Header.Get("X-Request-ID")
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Actor: &iam.AuditActor{
			UserID: userID,
			Email:  email,
		},
		Action:  "auth.login.piv.success",
		Outcome: iam.AuditOutcomeSuccess,
		Details: map[string]interface{}{
			"tenant_id":       tenantID,
			"user_id":         userID,
			"cert_thumbprint": certThumbprint,
			"cert_subject":    certSubject,
			"cert_issuer":     certIssuer,
			"attest_method":   attestMethod,
			"attest_level":    attestLevel,
			"request_id":      requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}

// logPIVLoginFailureAudit emits a failure audit event for a PIV smartcard login attempt.
// Emits event_type "auth.login.piv.failure".
func (h *IAMHandler) logPIVLoginFailureAudit(ctx context.Context, tenantID, certThumbprint, errorCode, errorMessage string, r *http.Request) {
	auditGetter, ok := h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return
	}
	logger, err := auditGetter.GetAuditLogger(ctx)
	if err != nil || logger == nil {
		return
	}
	requestID := r.Header.Get("X-Request-ID")
	logger.LogAsync(&iam.AuditEvent{
		TenantID:  tenantID,
		Timestamp: time.Now().UTC(),
		EventType: iam.AuditEventAuthentication,
		Category:  iam.AuditCategoryAuthentication,
		Action:    "auth.login.piv.failure",
		Outcome:   iam.AuditOutcomeFailure,
		Details: map[string]interface{}{
			"cert_thumbprint": certThumbprint,
			"error_code":      errorCode,
			"error_message":   errorMessage,
			"request_id":      requestID,
		},
		IPAddress: r.RemoteAddr,
		UserAgent: r.Header.Get("User-Agent"),
	})
}
