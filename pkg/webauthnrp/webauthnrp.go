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

// Package webauthnrp holds the shape rules for a WebAuthn relying party's
// identity: its RP ID and its allowed origins. pkg/config applies them to the
// global relying party at boot and pkg/iam applies them to a tenant's
// override when the tenant's settings are written and again when the
// tenant's relying party is built, so a value refused in one place cannot be
// accepted in another.
package webauthnrp

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
)

// RPIDError reports an RP ID that is not a bare effective domain.
type RPIDError struct {
	Cause error
}

// Error implements error.
func (e *RPIDError) Error() string {
	return e.Cause.Error() + " (rp_id is a bare effective domain such as \"example.com\")"
}

// Unwrap returns the relying party's own reason for refusing the RP ID.
func (e *RPIDError) Unwrap() error { return e.Cause }

// OriginError reports an allowed-origin entry that is not a usable origin.
type OriginError struct {
	// Index is the entry's position in the origin list.
	Index int
	// Reason says what is wrong with the entry.
	Reason string
}

// Error implements error.
func (e *OriginError) Error() string {
	return "entry " + strconv.Itoa(e.Index) + " " + e.Reason
}

// Reasons an origin entry is refused.
const (
	reasonOriginEmpty  = "must not be empty"
	reasonOriginNoHost = "has no host: an origin is scheme://host[:port], " +
		"for example \"https://app.example.com\""
)

// ValidateRPID refuses an RP ID the relying party itself would refuse,
// returning an *RPIDError.
//
// The rule is not restated here: it is protocol.ValidateRPID, the same check
// github.com/go-webauthn/webauthn applies when the relying party is
// constructed (webauthn.Config.validate). Delegating keeps the validation
// message and the runtime behaviour from ever drifting apart, and — as
// important — keeps this validator from inventing a stricter rule than the
// library's. It accepts an IP literal, for instance, which browsers reject but
// the non-browser software authenticator does not; refusing it here would
// break the explicit rp_id escape hatch that iam_init.go offers a node whose
// issuer host is an IP address.
func ValidateRPID(rpID string) error {
	if err := protocol.ValidateRPID(rpID); err != nil {
		return &RPIDError{Cause: err}
	}
	return nil
}

// ValidateOrigin refuses an entry that is not a usable origin, and so is dead
// configuration however the relying party is asked to match it. index is the
// entry's position in its list and is carried on the returned *OriginError.
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
// not a contract a validator can hold; it would refuse nothing at all.
// Matching one requires a client to send that same non-origin as its ceremony
// origin, which no client does, and permitting it would authorise something
// that is not an origin. These two shapes are worth naming because they are
// the ones seen in the field: the empty entry is what a half-failed
// relying-party derivation used to produce, and the hostless URL is what a
// "https://" typo leaves behind.
func ValidateOrigin(origin string, index int) error {
	if origin == "" {
		return &OriginError{Index: index, Reason: reasonOriginEmpty}
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
		return &OriginError{Index: index, Reason: reasonOriginNoHost}
	}
	return nil
}

// ValidateOrigins applies ValidateOrigin to every entry of origins and returns
// the first failure.
func ValidateOrigins(origins []string) error {
	for i, o := range origins {
		if err := ValidateOrigin(o, i); err != nil {
			return err
		}
	}
	return nil
}
