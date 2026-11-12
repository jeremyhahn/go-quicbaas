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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestWebauthnFactoryConfigFromOperator_DerivesFromIssuer verifies that the
// translator derives RPID and RPOrigins from the issuer-supplied defaults
// (including the port) when the operator omits them. This is the fix for the
// "invalid domain" / "error validating origin" bug: the WebAuthn relying
// party must agree with the issuer/banner host and port.
func TestWebauthnFactoryConfigFromOperator_DerivesFromIssuer(t *testing.T) {
	got, err := webauthnFactoryConfigFromOperator(
		config.IAMWebAuthnConfig{}, "localhost", "https://localhost:18080")
	if err != nil {
		t.Fatalf("an empty operator section derives its identity and must resolve: %v", err)
	}
	if got.RPID != "localhost" {
		t.Fatalf("expected derived RPID=localhost, got %q", got.RPID)
	}
	if got.RPDisplayName != "QuicBaaS" {
		t.Fatalf("expected default RPDisplayName=QuicBaaS, got %q", got.RPDisplayName)
	}
	if len(got.RPOrigins) != 1 || got.RPOrigins[0] != "https://localhost:18080" {
		t.Fatalf("expected derived RPOrigins=[https://localhost:18080], got %v", got.RPOrigins)
	}
	if got.Timeout != 0 {
		t.Fatalf("expected zero Timeout when operator field empty, got %v", got.Timeout)
	}
}

// TestWebauthnFactoryConfigFromOperator_PreservesOperatorValues verifies
// that explicit operator values pass through unchanged.
func TestWebauthnFactoryConfigFromOperator_PreservesOperatorValues(t *testing.T) {
	op := config.IAMWebAuthnConfig{
		RPID:                    "example.com",
		RPDisplayName:           "Example, Inc.",
		RPOrigins:               []string{"https://app.example.com", "https://admin.example.com"},
		Timeout:                 "90s",
		UserVerification:        "required",
		AttestationPreference:   "direct",
		ResidentKeyRequirement:  "required",
		AuthenticatorAttachment: "platform",
		Debug:                   true,
	}
	got, err := webauthnFactoryConfigFromOperator(op, "localhost", "https://localhost:18080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.RPID != "example.com" {
		t.Fatalf("RPID not preserved: %q", got.RPID)
	}
	if got.RPDisplayName != "Example, Inc." {
		t.Fatalf("RPDisplayName not preserved: %q", got.RPDisplayName)
	}
	if len(got.RPOrigins) != 2 {
		t.Fatalf("RPOrigins length wrong: %d", len(got.RPOrigins))
	}
	if got.Timeout != 90*time.Second {
		t.Fatalf("Timeout not parsed: %v", got.Timeout)
	}
	if got.UserVerification != "required" {
		t.Fatalf("UserVerification not preserved: %q", got.UserVerification)
	}
	if !got.Debug {
		t.Fatalf("Debug not preserved")
	}
}

// TestWebauthnFactoryConfigFromOperator_InvalidTimeoutRejected verifies that
// a malformed ceremony timeout is refused rather than silently discarded. The
// resolution path used to swallow the parse error, so an operator typo
// reverted to the library default with no signal anywhere.
func TestWebauthnFactoryConfigFromOperator_InvalidTimeoutRejected(t *testing.T) {
	op := config.IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://example.com"},
		Timeout:       "not-a-duration",
	}
	_, err := webauthnFactoryConfigFromOperator(op, "localhost", "https://localhost:18080")
	var cve *config.ConfigValidationError
	if !errors.As(err, &cve) {
		t.Fatalf("expected *config.ConfigValidationError, got %T: %v", err, err)
	}
	if cve.Field != "iam.webauthn.timeout" {
		t.Fatalf("expected field iam.webauthn.timeout, got %q", cve.Field)
	}
}

// TestWebauthnFactoryConfigFromOperator_OriginSliceIsCopied verifies
// that mutations to the operator's input slice do not leak into the
// returned config. Important for hot-reload scenarios where the
// operator config is rebuilt while the factory holds a stale snapshot.
func TestWebauthnFactoryConfigFromOperator_OriginSliceIsCopied(t *testing.T) {
	origins := []string{"https://example.com"}
	op := config.IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     origins,
	}
	got, err := webauthnFactoryConfigFromOperator(op, "localhost", "https://localhost:18080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Mutate the operator slice; the factory copy must not change.
	origins[0] = "https://attacker.example.com"
	if got.RPOrigins[0] != "https://example.com" {
		t.Fatalf("origin slice was not copied: factory got %q after mutation",
			got.RPOrigins[0])
	}
}

// TestWebauthnDefaultsFromIssuer_WithPort verifies the dev-default case: an
// https issuer on a non-default port derives RPID=hostname and an RP origin
// that INCLUDES the port exactly as a browser sends it. This is the regression
// guard for the live "error validating origin" failure (portless default).
func TestWebauthnDefaultsFromIssuer_WithPort(t *testing.T) {
	rpID, rpOrigin, err := webauthnDefaultsFromIssuer("https://localhost:18080")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rpID != "localhost" {
		t.Fatalf("expected RPID=localhost, got %q", rpID)
	}
	if rpOrigin != "https://localhost:18080" {
		t.Fatalf("expected RPOrigin=https://localhost:18080, got %q", rpOrigin)
	}
}

// TestWebauthnDefaultsFromIssuer_DefaultHTTPSPortOmitted verifies that the
// default scheme port (443 for https) is omitted from the derived origin, so
// it matches the browser-sent Origin header for a standard-port deployment.
func TestWebauthnDefaultsFromIssuer_DefaultHTTPSPortOmitted(t *testing.T) {
	rpID, rpOrigin, err := webauthnDefaultsFromIssuer("https://app.example.com:443")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rpID != "app.example.com" {
		t.Fatalf("expected RPID=app.example.com, got %q", rpID)
	}
	if rpOrigin != "https://app.example.com" {
		t.Fatalf("expected RPOrigin without port, got %q", rpOrigin)
	}
}

// TestWebauthnDefaultsFromIssuer_DefaultHTTPPortOmitted verifies that the
// default http port (80) is omitted from the derived origin.
func TestWebauthnDefaultsFromIssuer_DefaultHTTPPortOmitted(t *testing.T) {
	_, rpOrigin, err := webauthnDefaultsFromIssuer("http://app.example.com:80")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rpOrigin != "http://app.example.com" {
		t.Fatalf("expected RPOrigin without port, got %q", rpOrigin)
	}
}

// TestWebauthnDefaultsFromIssuer_IPLiteralFailsClosed verifies the owner-flagged
// fail-closed behaviour: an IP-literal issuer host cannot serve as a WebAuthn
// RP ID and must surface a typed WebAuthnRPIDFromIPError at config time.
func TestWebauthnDefaultsFromIssuer_IPLiteralFailsClosed(t *testing.T) {
	_, _, err := webauthnDefaultsFromIssuer("https://127.0.0.1:18080")
	if err == nil {
		t.Fatal("expected error for IP-literal issuer host, got nil")
	}
	var ipErr *WebAuthnRPIDFromIPError
	if !errors.As(err, &ipErr) {
		t.Fatalf("expected *WebAuthnRPIDFromIPError, got %T: %v", err, err)
	}
	if ipErr.Host != "127.0.0.1" {
		t.Fatalf("expected Host=127.0.0.1, got %q", ipErr.Host)
	}
	if !strings.Contains(ipErr.Error(), "rp_id") {
		t.Fatalf("error message should mention rp_id remediation, got %q", ipErr.Error())
	}
}

// TestWebauthnDefaultsFromIssuer_IPv6LiteralFailsClosed verifies the IPv6
// loopback literal is also rejected (it is a valid IP, not a domain).
func TestWebauthnDefaultsFromIssuer_IPv6LiteralFailsClosed(t *testing.T) {
	_, _, err := webauthnDefaultsFromIssuer("https://[::1]:18080")
	var ipErr *WebAuthnRPIDFromIPError
	if !errors.As(err, &ipErr) {
		t.Fatalf("expected *WebAuthnRPIDFromIPError, got %T: %v", err, err)
	}
	if ipErr.Host != "::1" {
		t.Fatalf("expected Host=::1, got %q", ipErr.Host)
	}
}

// TestWebauthnDefaultsFromIssuer_UnparseableFallsBack verifies that an
// unparseable issuer falls back to the domain dev default (localhost) rather
// than producing an empty/invalid RP ID.
func TestWebauthnDefaultsFromIssuer_UnparseableFallsBack(t *testing.T) {
	rpID, rpOrigin, err := webauthnDefaultsFromIssuer("://not a url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rpID != "localhost" || rpOrigin != "https://localhost" {
		t.Fatalf("expected localhost fallback, got rpID=%q rpOrigin=%q", rpID, rpOrigin)
	}
}

// TestIssuerURLFromConfig_CanonicalisesLoopback verifies the owner-flagged
// default change: a default Server.Host of 127.0.0.1 (and the IPv4/IPv6
// loopback / wildcard variants) is canonicalised to "localhost" in the
// advertised issuer URL, while the configured REST port is preserved.
func TestIssuerURLFromConfig_CanonicalisesLoopback(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1": "https://localhost:28084",
		"0.0.0.0":   "https://localhost:28084",
		"::1":       "https://localhost:28084",
		"::":        "https://localhost:28084",
		"localhost": "https://localhost:28084",
		"":          "https://localhost:28084",
	}
	for host, want := range cases {
		cfg := &config.Config{}
		cfg.Security.TLSEnabled = true
		cfg.Server.Host = host
		cfg.Server.REST.Port = 28084
		if got := issuerURLFromConfig(cfg); got != want {
			t.Fatalf("host %q: expected issuer %q, got %q", host, want, got)
		}
	}
}

// TestIssuerURLFromConfig_PreservesExplicitHost verifies that an explicitly
// configured hostname is advertised unchanged (only loopback is normalised).
func TestIssuerURLFromConfig_PreservesExplicitHost(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "auth.example.com"
	cfg.Server.REST.Port = 443
	if got := issuerURLFromConfig(cfg); got != "https://auth.example.com:443" {
		t.Fatalf("explicit host not preserved: %q", got)
	}
}

// TestIssuerURLFromConfig_UnsetPortUsesDefaultsLayer proves an unset REST
// port falls back to the daemon's defaults-layer REST port, so the WebAuthn
// origin cannot drift from the port the REST listener actually binds.
func TestIssuerURLFromConfig_UnsetPortUsesDefaultsLayer(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.TLSEnabled = true
	cfg.Server.Host = "auth.example.com"

	want := fmt.Sprintf("https://auth.example.com:%d", config.Default().Server.REST.Port)
	if got := issuerURLFromConfig(cfg); got != want {
		t.Fatalf("unset REST port: expected issuer %q, got %q", want, got)
	}
}

// TestWebauthnConfig_ThreeValuesAgree is the regression test for the exact live
// failure: from ONE server config the begin-options RP ID, the activation
// banner URL host, and the configured RP origins must all be consistent —
// RP ID == banner host, and the RP origins must contain the banner URL's
// origin. With stock defaults this previously had NO satisfiable origin.
func TestWebauthnConfig_ThreeValuesAgree(t *testing.T) {
	// Stock dev config: default Server.Host (127.0.0.1) + a chosen REST port.
	cfg := config.DefaultConfig()
	cfg.Server.REST.Port = 28084

	issuer := issuerURLFromConfig(cfg)

	// (1) The activation banner URL is built as issuer + "/ui/#/invite/<token>".
	bannerURL := issuer + "/ui/#/invite/sometoken"

	// (2) The WebAuthn relying party derives from the same issuer.
	rpID, rpOrigin, err := webauthnDefaultsFromIssuer(issuer)
	if err != nil {
		t.Fatalf("derivation failed for stock issuer %q: %v", issuer, err)
	}
	wa, err := webauthnFactoryConfigFromOperator(cfg.IAM.WebAuthn, rpID, rpOrigin)
	if err != nil {
		t.Fatalf("resolving the stock dev config failed: %v", err)
	}

	// Banner host must equal RP ID (begin-options rp.id).
	wantHost := "localhost"
	if !strings.HasPrefix(bannerURL, "https://"+wantHost+":28084/") {
		t.Fatalf("banner host mismatch: %q", bannerURL)
	}
	if wa.RPID != wantHost {
		t.Fatalf("RP ID (%q) does not match banner host (%q)", wa.RPID, wantHost)
	}

	// RP origins must contain the banner URL's origin (scheme+host+port).
	wantOrigin := "https://localhost:28084"
	found := false
	for _, o := range wa.RPOrigins {
		if o == wantOrigin {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("RP origins %v do not contain banner origin %q", wa.RPOrigins, wantOrigin)
	}
}

// TestWebauthnFactoryConfigFromOperator_FailsClosedOnUnresolvableIdentity is
// the regression guard for the live defect behind issue #344: on a node whose
// issuer host is an IP literal, derivation yields nothing, and an operator who
// supplied only one half of the relying-party identity used to get a relying
// party built on the other half's zero value — an empty origin list, or an
// empty origin string, which no browser Origin header can ever match. Every
// ceremony failed, silently, forever. Resolution now fails closed instead.
func TestWebauthnFactoryConfigFromOperator_FailsClosedOnUnresolvableIdentity(t *testing.T) {
	cases := []struct {
		name      string
		op        config.IAMWebAuthnConfig
		wantField string
	}{
		{
			name:      "rp_id supplied, origin underivable",
			op:        config.IAMWebAuthnConfig{RPID: "example.com"},
			wantField: "iam.webauthn.rp_origins",
		},
		{
			name:      "origins supplied, rp_id underivable",
			op:        config.IAMWebAuthnConfig{RPOrigins: []string{"https://example.com"}},
			wantField: "iam.webauthn.rp_id",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Empty issuer defaults are what webauthnDefaultsFromIssuer returns
			// alongside a WebAuthnRPIDFromIPError.
			_, err := webauthnFactoryConfigFromOperator(tc.op, "", "")
			var cve *config.ConfigValidationError
			if !errors.As(err, &cve) {
				t.Fatalf("expected *config.ConfigValidationError, got %T: %v", err, err)
			}
			if cve.Field != tc.wantField {
				t.Fatalf("expected field %q, got %q (msg=%q)", tc.wantField, cve.Field, cve.Message)
			}
		})
	}
}

// TestWebauthnFactoryConfigFromOperator_RejectsMalformedOperatorOrigin
// verifies that resolution validates operator-supplied values too, not only
// the derived halves: an origin that can never match any ceremony — here a URL
// origin with no host — is refused at startup rather than mounted.
func TestWebauthnFactoryConfigFromOperator_RejectsMalformedOperatorOrigin(t *testing.T) {
	op := config.IAMWebAuthnConfig{
		RPID:          "example.com",
		RPDisplayName: "Example",
		RPOrigins:     []string{"https://app.example.com", "https://"},
	}
	_, err := webauthnFactoryConfigFromOperator(op, "localhost", "https://localhost:18080")
	var cve *config.ConfigValidationError
	if !errors.As(err, &cve) {
		t.Fatalf("expected *config.ConfigValidationError, got %T: %v", err, err)
	}
	if cve.Field != "iam.webauthn.rp_origins" {
		t.Fatalf("expected field iam.webauthn.rp_origins, got %q", cve.Field)
	}
	if !strings.Contains(cve.Message, "entry 1") {
		t.Fatalf("expected the message to name the offending entry, got %q", cve.Message)
	}
}

// TestWebauthnFactoryConfigFromOperator_ShippedIntegrationShapeResolves pins
// the two deployment shapes that must keep resolving: the derive-everything
// config every shipped file in this repository uses, and the explicit
// relying-party block the integration fixture pins.
func TestWebauthnFactoryConfigFromOperator_ShippedIntegrationShapeResolves(t *testing.T) {
	explicit := config.IAMWebAuthnConfig{
		RPID:          "localhost",
		RPDisplayName: "QuicBaaS Integration Tests",
		RPOrigins: []string{
			"https://localhost:8080",
			"https://localhost",
			"http://localhost:8080",
		},
		AttestationPreference: "none",
		UserVerification:      "discouraged",
	}
	if _, err := webauthnFactoryConfigFromOperator(explicit, "localhost", "https://localhost:8080"); err != nil {
		t.Fatalf("the integration fixture's WebAuthn block must resolve: %v", err)
	}

	derivedOnly, err := webauthnFactoryConfigFromOperator(
		config.IAMWebAuthnConfig{}, "localhost", "https://localhost:8080")
	if err != nil {
		t.Fatalf("the derive-everything shape must resolve: %v", err)
	}
	if derivedOnly.RPID != "localhost" || len(derivedOnly.RPOrigins) != 1 {
		t.Fatalf("derivation produced an unexpected relying party: %+v", derivedOnly)
	}
}
