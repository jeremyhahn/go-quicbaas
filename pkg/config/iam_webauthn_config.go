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

package config

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
)

// IAMWebAuthnConfig holds the relying-party configuration for the WebAuthn /
// FIDO2 ceremony layer mounted from go-xkms.
//
// # Required versus derived
//
// Every relying-party identity field in this section is OPTIONAL. A node that
// configures none of them is not misconfigured — it is relying on derivation:
// the server resolves the RP ID and the RP origin from its canonical issuer
// URL (the same value that backs the OIDC issuer claim and the first-boot
// activation banner), so the relying party, the issuer, and the banner always
// agree on host and port. Operator-supplied values are authoritative and pass
// through derivation unchanged.
//
// The fields mirror github.com/go-webauthn/webauthn/webauthn.Config so the
// xkms factory can construct one directly without remapping.
type IAMWebAuthnConfig struct {
	// RPID is the Relying Party ID. It is validated exactly as the relying
	// party validates it (protocol.ValidateRPID): a bare host with no scheme,
	// port or path — a dotted domain such as "example.com", or "localhost", or
	// an IP literal. Note the library is broader than a browser here: a
	// browser additionally requires a registrable domain matching the page
	// origin, and rejects an IP literal outright, so an IP RP ID serves only
	// non-browser authenticators. Optional: when empty the issuer hostname is
	// used.
	RPID string `mapstructure:"rp_id"`

	// RPDisplayName is the human-readable name shown by the authenticator
	// during ceremonies. Optional; defaults to "QuicBaaS".
	RPDisplayName string `mapstructure:"rp_display_name"`

	// RPOrigins is the list of permitted origins. A browser origin is a full
	// URL origin including scheme (e.g. "https://app.example.com"); any entry
	// that is not an http/https URL is matched verbatim against the ceremony
	// origin, which is how Android native-application origins
	// ("android:apk-key-hash:...") are supported. Optional: when empty the
	// issuer origin — scheme://host[:port], with the default scheme port
	// omitted exactly as a browser sends it — is used.
	RPOrigins []string `mapstructure:"rp_origins"`

	// Timeout is the ceremony timeout (e.g. "60s"). When empty the
	// underlying library default applies. When set it must parse as a Go
	// duration and be positive — a typo here is refused rather than
	// silently reverting to the default.
	Timeout string `mapstructure:"timeout"`

	// UserVerification selects the user-verification policy:
	// "preferred" (default), "required", or "discouraged".
	UserVerification string `mapstructure:"user_verification"`

	// AttestationPreference controls attestation conveyance:
	// "none" (default), "indirect", "direct", or "enterprise".
	//
	// A deployment whose Assurance is "hardware" has this raised to "direct"
	// at service-construction time: with "none" conveyance the browser strips
	// the attestation statement, the authenticator reports no AAGUID, and a
	// hardware requirement could only ever refuse. Configure it explicitly
	// only to select "enterprise".
	AttestationPreference string `mapstructure:"attestation_preference"`

	// AttestationRootsFile is a PEM bundle of FIDO attestation ROOT certificates
	// — the manufacturer roots that sign authenticator batch attestation
	// certificates (Yubico's FIDO2 attestation root, and so on).
	//
	// It is REQUIRED for hardware enrollment. go-webauthn reports an attestation
	// type of "basic_full" after checking only that the statement is internally
	// consistent; it explicitly does not walk the certificate chain. Without a
	// trust anchor, a self-generated attestation certificate carrying an
	// allowlisted AAGUID is indistinguishable from a real security key. When
	// this is empty, hardware enrollment is refused with a message naming the
	// setting.
	AttestationRootsFile string `mapstructure:"attestation_roots_file"`

	// Assurance is the minimum attestation level a NON-administrative WebAuthn
	// registration must prove: "hardware" (default), "any", or
	// "insecure-allow-software-administrators".
	//
	// "hardware" requires a manufacturer-attested authenticator whose AAGUID is
	// in the iam.aaguid allowlist. "any" accepts an authenticator that conveys
	// no attestation — including a virtual one — and is the explicit opt-in for
	// test environments and for tenant users the operator does not hold to a
	// hardware bar.
	//
	// "any" NEVER applies to an administrative account. An account holding an
	// administrative role is always required to prove hardware, so loosening
	// the tenant policy cannot loosen the one that guards the platform.
	//
	// "insecure-allow-software-administrators" additionally drops the
	// administrator bar. It exists for automated test environments that drive a
	// headless software authenticator or a CDP virtual authenticator and still
	// need to enroll administrators. A node running it announces itself at WARN
	// on every boot. There is no shorter alias, and an unrecognised value is
	// refused rather than defaulted, so it cannot be reached by accident.
	//
	// Empty means "hardware": the safe policy is the one an operator gets by
	// configuring nothing.
	Assurance string `mapstructure:"assurance"`

	// ResidentKeyRequirement selects the resident-key policy:
	// "discouraged", "preferred", or "required".
	ResidentKeyRequirement string `mapstructure:"resident_key_requirement"`

	// AuthenticatorAttachment optionally constrains the authenticator
	// type: "platform", "cross-platform", or empty (no constraint).
	AuthenticatorAttachment string `mapstructure:"authenticator_attachment"`

	// Debug enables verbose webauthn logging in non-production builds.
	Debug bool `mapstructure:"debug"`
}

// Validate checks the operator-supplied WebAuthn section and is run by the
// boot-time configuration gate (config.Validate) for every node.
//
// It validates SHAPE, not presence. RPID, RPDisplayName and RPOrigins may all
// be empty: that node derives its relying-party identity from the issuer URL
// at startup, which is a supported deployment shape and the shape ten of the
// eleven configuration files shipped in this repository use (the integration
// fixture is the one that pins the block explicitly). Refusing it here would
// turn derivation into a fleet-wide boot failure.
//
// What is refused is a supplied value that can never work: an RP ID the
// relying party itself would refuse at construction, and an entry in RPOrigins
// that is not a usable origin. Those are dead settings today — silently
// accepted, silently non-functional — so a node carrying one gets an
// actionable error at boot instead of a WebAuthn surface that rejects every
// ceremony, or none at all.
// The bar is the embedded relying party's own behaviour, deliberately: a
// validator stricter than the thing it guards refuses configurations that
// work.
//
// The value the relying party is actually built from is checked separately by
// ValidateResolved, after derivation has been applied.
func (c *IAMWebAuthnConfig) Validate() error {
	return c.validate(false)
}

// ValidateResolved checks the WebAuthn configuration AFTER runtime derivation
// has filled in whatever the operator omitted. This is the value the relying
// party is constructed from, so its invariants are the ones that matter: an RP
// ID and at least one origin are genuinely required here, because a relying
// party without them accepts no ceremony at all.
//
// It is the fail-closed gate on the WebAuthn init path: derivation that
// produces no usable identity (an IP-literal issuer host, say) must stop
// startup rather than mount a surface that rejects every registration.
func (c *IAMWebAuthnConfig) ValidateResolved() error {
	return c.validate(true)
}

// validate implements both Validate and ValidateResolved. The two differ only
// in whether the relying-party identity fields may be empty: in the operator's
// input they may (derivation fills them in), in the resolved config they may
// not (nothing fills them in after this point).
func (c *IAMWebAuthnConfig) validate(resolved bool) error {
	switch {
	case c.RPID != "":
		if err := validateWebAuthnRPID(c.RPID); err != nil {
			return err
		}
	case resolved:
		return &ConfigValidationError{
			Field: "iam.webauthn.rp_id",
			Message: "resolved relying party has no RP ID: derivation from the " +
				"issuer URL produced none and iam.webauthn.rp_id is unset",
		}
	}

	if resolved && c.RPDisplayName == "" {
		return &ConfigValidationError{
			Field:   "iam.webauthn.rp_display_name",
			Message: "resolved relying party has no display name",
		}
	}

	if resolved && len(c.RPOrigins) == 0 {
		return &ConfigValidationError{
			Field: "iam.webauthn.rp_origins",
			Message: "resolved relying party has no origins: derivation from the " +
				"issuer URL produced none and iam.webauthn.rp_origins is empty",
		}
	}
	for i, o := range c.RPOrigins {
		if err := validateWebAuthnOrigin(o, i); err != nil {
			return err
		}
	}

	if _, err := c.CeremonyTimeout(); err != nil {
		return err
	}

	// Validates AND canonicalises: the value forwarded downstream is the
	// trimmed, lowercased token, not the operator's raw spelling.
	for _, e := range webauthnPolicyEnums {
		if err := e.canonicalize(c); err != nil {
			return err
		}
	}
	return nil
}

// CeremonyTimeout returns the configured ceremony timeout. A zero duration
// means "unset" — the underlying webauthn library's own default applies.
//
// This is the single place the Timeout string is interpreted, so the value the
// boot-time gate validates and the value the relying party is built with can
// never disagree. A typo is reported, never silently discarded: reverting to
// the library default behind the operator's back is how a deliberately short
// ceremony window becomes a long one with nothing in the logs.
func (c *IAMWebAuthnConfig) CeremonyTimeout() (time.Duration, error) {
	if c.Timeout == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 0, &ConfigValidationError{
			Field:   "iam.webauthn.timeout",
			Message: "must be a Go duration such as \"60s\": " + err.Error(),
		}
	}
	if d <= 0 {
		return 0, &ConfigValidationError{
			Field:   "iam.webauthn.timeout",
			Message: "must be positive; leave it unset to use the library default",
		}
	}
	return d, nil
}

// webauthnPolicyEnum describes one ceremony-policy field whose value must come
// from a fixed set. Membership is a map lookup rather than a switch chain so
// the four fields share one code path and one error shape.
type webauthnPolicyEnum struct {
	field   string
	value   func(*IAMWebAuthnConfig) string
	set     func(*IAMWebAuthnConfig, string)
	allowed map[string]bool
	oneOf   string
}

// canonicalize reports a ConfigValidationError when the field holds a value
// outside its set, and otherwise REWRITES the field to its canonical form. An
// empty value is always allowed: it selects the library default.
//
// The value is normalised the way the RUNTIME parsers normalise it — trimmed
// and lowercased. Without that, boot-time validation and the parser
// disagree: iam.ParseWebAuthnAssurance accepts "HARDWARE" and "  any  ", so a
// config using either would satisfy the parser's contract and still be refused
// at boot, making a documented accepted value unusable.
//
// The rewrite is why this does more than check. Accepting a value and then
// forwarding the operator's raw spelling is how a config reports healthy at
// boot and fails later: these fields are handed to go-xkms
// (pkg/server/qrdb_node.go quicbaasWebAuthnConfigToXKMS), whose own validator
// matches the tokens EXACTLY — `pkg/webauthn/config.go` switches on
// "none"/"indirect"/"direct"/"enterprise" with no trimming. So
// `attestation_preference: " Direct "` passed this gate and then failed when
// the WebAuthn service was built, which made the boot-time acceptance
// misleading rather than authoritative. Normalising ONCE here, at the boundary
// where the value is validated, means every consumer downstream reads the form
// the downstream validators accept — with no accessor a future call site can
// forget to use.
func (e webauthnPolicyEnum) canonicalize(c *IAMWebAuthnConfig) error {
	raw := e.value(c)
	v := strings.ToLower(strings.TrimSpace(raw))
	if v != "" && !e.allowed[v] {
		return &ConfigValidationError{
			Field:   e.field,
			Message: "must be one of " + e.oneOf,
		}
	}
	if v != raw {
		e.set(c, v)
	}
	return nil
}

// webauthnPolicyEnums is the ceremony-policy value sets, mirroring the
// protocol enumerations in github.com/go-webauthn/webauthn/protocol.
var webauthnPolicyEnums = []webauthnPolicyEnum{
	{
		field:   "iam.webauthn.user_verification",
		value:   func(c *IAMWebAuthnConfig) string { return c.UserVerification },
		set:     func(c *IAMWebAuthnConfig, v string) { c.UserVerification = v },
		allowed: map[string]bool{"preferred": true, "required": true, "discouraged": true},
		oneOf:   "preferred, required, discouraged",
	},
	{
		field:   "iam.webauthn.attestation_preference",
		value:   func(c *IAMWebAuthnConfig) string { return c.AttestationPreference },
		set:     func(c *IAMWebAuthnConfig, v string) { c.AttestationPreference = v },
		allowed: map[string]bool{"none": true, "indirect": true, "direct": true, "enterprise": true},
		oneOf:   "none, indirect, direct, enterprise",
	},
	{
		field: "iam.webauthn.assurance",
		value: func(c *IAMWebAuthnConfig) string { return c.Assurance },
		set:   func(c *IAMWebAuthnConfig, v string) { c.Assurance = v },
		allowed: map[string]bool{
			"hardware":                               true,
			"any":                                    true,
			"insecure-allow-software-administrators": true,
		},
		oneOf: "hardware, any, insecure-allow-software-administrators",
	},
	{
		field:   "iam.webauthn.resident_key_requirement",
		value:   func(c *IAMWebAuthnConfig) string { return c.ResidentKeyRequirement },
		set:     func(c *IAMWebAuthnConfig, v string) { c.ResidentKeyRequirement = v },
		allowed: map[string]bool{"discouraged": true, "preferred": true, "required": true},
		oneOf:   "discouraged, preferred, required",
	},
	{
		field:   "iam.webauthn.authenticator_attachment",
		value:   func(c *IAMWebAuthnConfig) string { return c.AuthenticatorAttachment },
		set:     func(c *IAMWebAuthnConfig, v string) { c.AuthenticatorAttachment = v },
		allowed: map[string]bool{"platform": true, "cross-platform": true},
		oneOf:   "platform, cross-platform, or empty",
	},
}

// validateWebAuthnRPID refuses an RP ID the relying party itself would refuse.
//
// The rule is not restated here: it is protocol.ValidateRPID, the same check
// github.com/go-webauthn/webauthn applies when the relying party is
// constructed (webauthn.Config.validate). Delegating keeps the boot-time
// message and the runtime behaviour from ever drifting apart, and — as
// important — keeps this validator from inventing a stricter rule than the
// library's. It accepts an IP literal, for instance, which browsers reject but
// the non-browser software authenticator does not; refusing it here would
// break the explicit rp_id escape hatch that iam_init.go offers a node whose
// issuer host is an IP address.
//
// An RP ID this rejects fails today too — silently, when relying-party
// construction fails and the WebAuthn surface is disabled with a log line.
// Reporting it at boot changes when the operator finds out, not whether the
// configuration ever worked.
func validateWebAuthnRPID(rpID string) error {
	if err := protocol.ValidateRPID(rpID); err != nil {
		return &ConfigValidationError{
			Field: "iam.webauthn.rp_id",
			Message: err.Error() +
				" (rp_id is a bare effective domain such as \"example.com\")",
		}
	}
	return nil
}

// validateWebAuthnOrigin refuses an entry that is not a usable origin, and so
// is dead configuration however the relying party is asked to match it.
//
// The bar is the relying party's own comparison, not a stricter idea of a
// well-formed origin. protocol.IsOriginInHaystack dispatches on exactly one
// test (protocol.isPossibleFQDN): an entry beginning with http:// or https://
// is compared as a URL — scheme and host only, case-insensitively, with the
// default scheme port normalised — while every other entry is compared
// verbatim against the ceremony origin, which is how Android
// native-application origins ("android:apk-key-hash:...") are supported. So a
// URL entry carrying a path, query, fragment or userinfo is accepted, as is
// any entry in the verbatim-matched form, including one url.Parse rejects.
//
// What is refused is narrower than "cannot match": the empty string, and a URL
// origin carrying no host ("https://", "http://:8080"). Strictly, the relying
// party WOULD match those against themselves — every string matches itself
// under the exact-string branch, which is why "accept whatever could match" is
// not a contract a validator can hold; it would refuse nothing at all and be
// the dead control this file exists to stop being. Matching one requires a
// client to send that same non-origin as its ceremony origin, which no client
// does, and permitting it would authorise something that is not an origin.
// These two shapes are worth naming because they are the ones seen in the
// field: the empty entry is what a half-failed relying-party derivation used
// to produce, and the hostless URL is what a "https://" typo leaves behind.
func validateWebAuthnOrigin(origin string, index int) error {
	fail := func(msg string) error {
		return &ConfigValidationError{
			Field:   "iam.webauthn.rp_origins",
			Message: "entry " + strconv.Itoa(index) + " " + msg,
		}
	}
	if origin == "" {
		return fail("must not be empty")
	}

	// The same test the relying party dispatches on. An entry that is not a
	// URL origin is matched verbatim, and nothing about its shape can be
	// checked without refusing a form the relying party accepts.
	lower := strings.ToLower(origin)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return nil
	}

	u, err := url.Parse(origin)
	if err != nil {
		// Unparseable but not dead: go-webauthn falls back to exact-string
		// matching when the ceremony origin is equally unparseable, a case
		// upstream pins with "http://app.example.%%%&123?1"
		// (ShouldHandleInvalidURLAsSimpleStringMatch). Refusing it here would
		// again be stricter than the relying party.
		return nil
	}
	// Hostname(), not Host: "http://:8080" parses with a non-empty Host of
	// ":8080" and a hostname of "", which is not a usable origin. No real
	// client origin is hostless, so nothing is authorised by refusing it.
	if u.Hostname() == "" {
		return fail("has no host: an origin is scheme://host[:port], " +
			"for example \"https://app.example.com\"")
	}
	return nil
}
