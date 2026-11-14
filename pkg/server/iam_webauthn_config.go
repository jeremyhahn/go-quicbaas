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

package server

import (
	"fmt"
	"net"
	"net/url"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// issuerURLFromConfig constructs the canonical external URL for a server from
// its configuration — the single source of truth shared by the OIDC issuer
// claim, the first-boot activation banner, and the WebAuthn relying-party
// defaults. Loopback bind hosts (empty / wildcard / IPv4 / IPv6 loopback) are
// canonicalised to "localhost" so the derived WebAuthn RP ID is a registrable
// domain rather than an IP literal (which browsers reject). Binding still uses
// the configured Server.Host; only the advertised URL is normalised.
func issuerURLFromConfig(cfg *config.Config) string {
	scheme := "http"
	if cfg.Security.TLSEnabled {
		scheme = "https"
	}

	host := config.WebAuthnIssuerHost(cfg.Server.Host)

	port := cfg.Server.REST.Port
	if port == 0 {
		port = config.DefaultRESTPort
	}

	return fmt.Sprintf("%s://%s:%d", scheme, host, port)
}

// WebAuthnRPIDFromIPError is returned when WebAuthn relying-party defaults
// would have to be derived from an issuer whose hostname is an IP literal.
// The WebAuthn specification requires the RP ID to be a registrable domain
// (a valid effective domain); IP literals are not valid RP IDs and browsers
// (Chrome verified) reject navigator.credentials.create() with "This is an
// invalid domain." We fail closed at configuration time — not at the first
// registration attempt — so the operator gets an actionable error instead of
// an unusable WebAuthn surface.
type WebAuthnRPIDFromIPError struct {
	// Issuer is the canonical issuer URL the default was derived from.
	Issuer string
	// Host is the IP-literal hostname that cannot serve as an RP ID.
	Host string
}

func (e *WebAuthnRPIDFromIPError) Error() string {
	return "iam.webauthn: cannot derive RP ID from IP-literal issuer host " +
		e.Host + " (issuer " + e.Issuer + "): the WebAuthn RP ID must be a " +
		"domain name. Set iam.webauthn.rp_id (and iam.webauthn.rp_origins) " +
		"explicitly, or configure a hostname instead of an IP address."
}

// webauthnDefaultsFromIssuer derives the WebAuthn relying-party defaults from
// the canonical server issuer URL — the single source of truth shared by the
// OIDC issuer, the first-boot activation banner, and the WebAuthn ceremony
// layer. The derived RP ID is the issuer hostname; the derived RP origin is
// the issuer's scheme://host[:port], with the port included exactly as a
// browser would send it in the Origin header (the default scheme ports 443
// for https and 80 for http are omitted, matching browser behaviour and the
// exact-string origin comparison performed during ceremony verification).
//
// If the issuer hostname is an IP literal it returns a WebAuthnRPIDFromIPError
// so configuration validation fails closed rather than producing a relying
// party that no browser can satisfy.
func webauthnDefaultsFromIssuer(issuerURL string) (rpID, rpOrigin string, err error) {
	u, parseErr := url.Parse(issuerURL)
	if parseErr != nil || u.Hostname() == "" {
		// Fall back to the historical dev default when the issuer cannot be
		// parsed. localhost (NOT 127.0.0.1) is mandated because the RP ID must
		// be a domain; an IP literal is rejected by the browser.
		return "localhost", "https://localhost", nil
	}

	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return "", "", &WebAuthnRPIDFromIPError{Issuer: issuerURL, Host: host}
	}

	rpID = host

	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	port := u.Port()
	// Omit only the default scheme ports so the derived origin matches the
	// browser-sent Origin header exactly.
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		rpOrigin = scheme + "://" + host
	} else {
		rpOrigin = scheme + "://" + net.JoinHostPort(host, port)
	}
	return rpID, rpOrigin, nil
}

// webauthnFactoryConfigFromOperator resolves the operator-supplied quicbaas
// WebAuthn configuration into the factory-level shape consumed by the
// per-tenant xkms WebAuthn services, and validates the result.
//
// Resolution: when the operator leaves RPID or RPOrigins empty they are
// derived from the canonical server issuer URL (issuerRPID / issuerRPOrigin)
// so the WebAuthn relying party, the OIDC issuer, and the first-boot
// activation banner always agree on host and port — eliminating the "invalid
// domain" / "error validating origin" class of failures. Explicit
// iam.webauthn.rp_id / rp_origins values remain authoritative and pass through
// unchanged. The display name defaults to "QuicBaaS" when unset.
//
// Validation: the RESOLVED value — not the operator's input — is what the
// relying party is built from, so it is the one whose invariants matter, and
// config.IAMWebAuthnConfig.ValidateResolved is applied to it here. This is the
// fail-closed gate: derivation that yields no RP ID or no origin (an
// IP-literal issuer host with only one half of the identity configured, say)
// returns an error instead of mounting a relying party that rejects every
// ceremony it is ever handed.
func webauthnFactoryConfigFromOperator(c config.IAMWebAuthnConfig, issuerRPID, issuerRPOrigin string) (iam.WebAuthnFactoryConfig, error) {
	// Resolve into a copy of the operator's own config type so exactly one
	// validator describes the contract for both the input and the result.
	resolved := c
	resolved.RPOrigins = append([]string(nil), c.RPOrigins...)
	if resolved.RPID == "" {
		resolved.RPID = issuerRPID
	}
	if resolved.RPDisplayName == "" {
		resolved.RPDisplayName = "QuicBaaS"
	}
	if len(resolved.RPOrigins) == 0 && issuerRPOrigin != "" {
		resolved.RPOrigins = []string{issuerRPOrigin}
	}
	if err := resolved.ValidateResolved(); err != nil {
		return iam.WebAuthnFactoryConfig{}, err
	}

	out := iam.WebAuthnFactoryConfig{
		RPID:                    resolved.RPID,
		RPDisplayName:           resolved.RPDisplayName,
		RPOrigins:               resolved.RPOrigins,
		UserVerification:        resolved.UserVerification,
		AttestationPreference:   resolved.AttestationPreference,
		ResidentKeyRequirement:  resolved.ResidentKeyRequirement,
		AuthenticatorAttachment: resolved.AuthenticatorAttachment,
		Debug:                   resolved.Debug,
	}
	timeout, err := resolved.CeremonyTimeout()
	if err != nil {
		return iam.WebAuthnFactoryConfig{}, err
	}
	out.Timeout = timeout
	return out, nil
}
