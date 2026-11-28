package config

import (
	"net"
	"strings"
)

// WebAuthnIssuerHost returns the host the canonical issuer URL is built
// from for the given server.host: the wildcard and loopback hosts become
// "localhost" (a WebAuthn RP ID must be a domain, and localhost is the one
// browsers accept for local use); any other host is returned unchanged.
// The issuer URL, the activation banner and the WebAuthn RP ID defaults
// all derive from this one mapping.
func WebAuthnIssuerHost(serverHost string) string {
	switch serverHost {
	case "", "0.0.0.0", "::", "127.0.0.1", "::1", "localhost":
		return "localhost"
	}
	return serverHost
}

// validateWebAuthnRPIDSource refuses a configuration whose WebAuthn RP ID
// would be derived from an IP address. With iam.webauthn.rp_id unset the
// RP ID defaults to the issuer host, and a browser rejects an IP literal
// as an RP ID, so every passkey ceremony would fail at login. The rule
// fails at load and names the setting instead.
func (v *Validator) validateWebAuthnRPIDSource(cfg *Config) {
	if cfg.IAM.WebAuthn.RPID != "" {
		return
	}
	host := WebAuthnIssuerHost(cfg.Server.Host)
	// A bracketed IPv6 literal ("[fd00::1]") is still an IP address.
	if net.ParseIP(strings.Trim(host, "[]")) == nil {
		return
	}
	v.addError("iam.webauthn.rp_id",
		"server.host "+cfg.Server.Host+" is an IP address, and the WebAuthn RP ID "+
			"defaults to the server host; a browser refuses an IP address as an RP ID. "+
			"Set iam.webauthn.rp_id to the domain name clients use to reach this node "+
			"(and iam.webauthn.rp_origins to its origin), or set server.host to a hostname")
}
