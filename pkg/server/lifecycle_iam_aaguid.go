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
	"encoding/json"
	"os"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// aaguidEntry is one entry of the SIMPLE allowlist shape — a top-level JSON
// array an operator writes by hand:
//
//	[{"aaguid": "...", "name": "...", "attestation_level": "..."}]
//
// The FIDO Metadata Service v3 shape is a top-level object and is decoded by
// mds3Document below. This comment used to claim this struct covered both,
// which it never did: a real MDS3 file failed to unmarshal.
type aaguidEntry struct {
	AAGUID           string `json:"aaguid"`
	Name             string `json:"name"`
	AttestationLevel string `json:"attestation_level"`
}

// =============================================================================
// The FIDO Metadata Service v3 document
//
// WHAT IS READ, AND WHAT IS DELIBERATELY IGNORED
//
// An MDS3 blob describes every certified authenticator in exhaustive detail.
// This parser reads THREE fields and ignores everything else:
//
//	entries[].aaguid                         -> the allowlist key
//	entries[].metadataStatement.description  -> the operator-facing name
//	entries[].statusReports[].status         -> whether to import the entry
//
// Everything else — authenticatorGetInfo, attestationRootCertificates,
// userVerificationDetails, keyProtection, upv, the legalHeader — is ignored,
// and ignoring it changes nothing about the decision this file makes. The
// allowlist answers one question: "is this authenticator MODEL one the operator
// admits?" Attestation trust paths are verified separately, against
// iam.webauthn.attestation_roots_file, by the WebAuthn attestation gate.
//
// statusReports is NOT optional detail, which is why it is read. The blob
// includes authenticators the Alliance has withdrawn, and importing every entry
// would hand an operator an allowlist that trusts a REVOKED or
// key-compromised model. A convenience feature that quietly widens a security
// boundary is the defect class this work exists to remove, so a disqualified
// entry is skipped.
//
// A NOTE ON THE SIGNED BLOB. The Alliance distributes MDS3 as a JWS, and this
// parser reads the DECODED payload — a plain JSON object. It does not verify
// the blob signature, because it is reading a file the OPERATOR placed on the
// node's disk: the trust boundary is the filesystem, exactly as it is for
// ca_file and attestation_roots_file. Fetching and verifying the blob directly
// from the Alliance would be a different feature with a different trust model.
// =============================================================================

// mds3Document is the subset of a decoded MDS3 payload this parser reads.
type mds3Document struct {
	Entries []mds3Entry `json:"entries"`
}

// mds3Entry is one authenticator in the metadata blob. Entries that identify
// themselves by attestationCertificateKeyIdentifiers instead of an AAGUID —
// the U2F and UAF authenticators every real blob carries — leave AAGUID empty
// and are skipped rather than failing the load.
type mds3Entry struct {
	AAGUID            string             `json:"aaguid"`
	MetadataStatement mds3Statement      `json:"metadataStatement"`
	StatusReports     []mds3StatusReport `json:"statusReports"`
}

// mds3Statement is the metadata statement, read only for its description.
type mds3Statement struct {
	Description string `json:"description"`
}

// mds3StatusReport is one certification-history record.
type mds3StatusReport struct {
	Status string `json:"status"`
}

// disqualifyingMDS3Statuses are the AuthenticatorStatus values that bar an
// entry from the allowlist.
//
// They are the compromise and withdrawal verdicts. Membership is tested against
// the WHOLE status history rather than the latest report: statusReports is an
// append-only record, and a key compromise is not undone by a later
// certification. Testing the history is also order-independent, which matters
// because nothing in the format guarantees the array is sorted.
//
// NOT_FIDO_CERTIFIED is deliberately absent. It means the Alliance has not
// certified the model, not that the model is compromised, and an operator who
// listed an uncertified authenticator in a blob they curated meant to.
var disqualifyingMDS3Statuses = map[string]struct{}{
	"REVOKED":                      {},
	"ATTESTATION_KEY_COMPROMISE":   {},
	"USER_KEY_REMOTE_COMPROMISE":   {},
	"USER_KEY_PHYSICAL_COMPROMISE": {},
	"USER_VERIFICATION_BYPASS":     {},
}

// admitted reports whether this entry may enter the allowlist.
func (e mds3Entry) admitted() bool {
	if e.AAGUID == "" {
		return false
	}
	for _, report := range e.StatusReports {
		if _, bad := disqualifyingMDS3Statuses[strings.ToUpper(strings.TrimSpace(report.Status))]; bad {
			return false
		}
	}
	return true
}

// buildAAGUIDAllowlist constructs an *iam.AAGUIDAllowlist from the operator
// config. It ALWAYS returns an allowlist on success — never nil.
//
// It used to return (nil, nil) for a default config, and the caller then
// skipped enforcement entirely. That made an unconfigured deployment strictly
// worse than a permissive one: no gate at all, and the absence invisible in
// both the config and the logs. An empty strict allowlist is the honest
// representation of "this deployment has not said which authenticators it
// trusts", and it refuses with a message naming the setting to fill in.
//
// Startup behaviour:
//   - mode = "log_only" → AAGUIDPolicyPermissive (an explicit opt-out).
//   - mode = "enforce", or empty → AAGUIDPolicyStrict.
//   - Any OTHER non-empty mode → typed error. Silently treating a typo as
//     strict fails closed, but it turns an operator's intended "log_only" into
//     an unexplained refusal of every authenticator, which is a support call
//     rather than a security decision. An unrecognised value is refused with a
//     message naming the accepted ones, the way every other typed setting is.
//   - Non-empty allowlist_file that cannot be read or parsed → typed error.
//     The caller treats it as fatal: an operator who configured an allowlist
//     and got a deployment that enforces nothing is the failure this returns.
func buildAAGUIDAllowlist(cfg config.IAMAAGUIDConfig) (*iam.AAGUIDAllowlist, error) {
	policy := iam.AAGUIDPolicyStrict
	switch {
	case cfg.Mode == "":
		// Unconfigured is strict: the safe policy is the one you get by doing
		// nothing.
	case strings.EqualFold(cfg.Mode, "enforce"):
	case strings.EqualFold(cfg.Mode, "log_only"):
		policy = iam.AAGUIDPolicyPermissive
	default:
		return nil, &AAGUIDModeError{Mode: cfg.Mode}
	}

	var hexAAGUIDs []string
	if cfg.AllowlistFile != "" {
		data, err := os.ReadFile(cfg.AllowlistFile)
		if err != nil {
			return nil, &AAGUIDAllowlistReadError{Path: cfg.AllowlistFile, Cause: err}
		}
		entries, err := parseAAGUIDAllowlistFile(data)
		if err != nil {
			return nil, &AAGUIDAllowlistParseError{Path: cfg.AllowlistFile, Cause: err}
		}
		hexAAGUIDs = make([]string, 0, len(entries))
		for _, e := range entries {
			if e.AAGUID != "" {
				hexAAGUIDs = append(hexAAGUIDs, e.AAGUID)
			}
		}
	}

	return iam.NewAAGUIDAllowlist(hexAAGUIDs, policy), nil
}

// parseAAGUIDAllowlistFile decodes an operator allowlist file into entries.
//
// Both documented shapes are accepted, chosen by the first significant byte
// rather than by trying one and falling back to the other: a mistyped array
// should be reported as a broken array, not as a document that failed to be an
// object as well.
//
//	[ ... ]  the simple array an operator writes by hand
//	{ ... }  a decoded FIDO Metadata Service v3 payload
//
// Anything else, and any document that yields no usable entry, is an error.
// Returning an empty allowlist would be worse than refusing: under the default
// strict policy it denies every authenticator, and the operator would be
// debugging a gate that looks configured.
func parseAAGUIDAllowlistFile(data []byte) ([]aaguidEntry, error) {
	switch firstJSONToken(data) {
	case '[':
		var entries []aaguidEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return nil, err
		}
		// Same rule the MDS3 branch applies, and the one this function's
		// contract already promised: a document that yields no usable entry is
		// a configuration error, not an empty allowlist.
		//
		// `[]`, or an array whose objects all omit "aaguid", parsed happily and
		// produced a strict allowlist admitting nothing — so the node started,
		// reported healthy, and then refused every WebAuthn enrollment with a
		// message about the authenticator rather than about the file.
		//
		// Refusing loses no capability: "allow nothing" is ALREADY what an
		// unconfigured allowlist means, because a nil allowlist denies any
		// enrollment that must prove hardware. An empty file therefore cannot
		// express anything a deployment could want, and can only be a mistake —
		// a truncated download, a template never filled in, a jq filter that
		// matched nothing.
		if !hasUsableAAGUID(entries) {
			return nil, &AAGUIDAllowlistShapeError{
				Detail: "the array has no entries carrying an aaguid",
			}
		}
		return entries, nil
	case '{':
		return parseMDS3AllowlistDocument(data)
	default:
		return nil, &AAGUIDAllowlistShapeError{}
	}
}

// hasUsableAAGUID reports whether any entry carries an AAGUID. An entry
// without one names an authenticator the gate cannot match, so a document of
// only those admits nothing.
func hasUsableAAGUID(entries []aaguidEntry) bool {
	for _, e := range entries {
		if e.AAGUID != "" {
			return true
		}
	}
	return false
}

// parseMDS3AllowlistDocument extracts the allowlist entries from a decoded
// MDS3 payload, skipping the ones a FIDO2 allowlist cannot or must not hold.
func parseMDS3AllowlistDocument(data []byte) ([]aaguidEntry, error) {
	var doc mds3Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	entries := make([]aaguidEntry, 0, len(doc.Entries))
	for _, e := range doc.Entries {
		if !e.admitted() {
			continue
		}
		entries = append(entries, aaguidEntry{
			AAGUID: e.AAGUID,
			Name:   e.MetadataStatement.Description,
		})
	}
	if len(entries) == 0 {
		return nil, &AAGUIDAllowlistShapeError{
			Detail: "the document has no entries carrying an admissible aaguid",
		}
	}
	return entries, nil
}

// firstJSONToken returns the first non-whitespace byte of a JSON document, or
// zero when there is none.
func firstJSONToken(data []byte) byte {
	for _, b := range data {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b
		}
	}
	return 0
}

// AAGUIDAllowlistShapeError reports an allowlist file that is neither
// documented shape, or one that carries nothing usable.
//
// It names both shapes because the setting accepts both, and an operator who
// pointed it at the wrong file has no way to guess which one they were closer
// to.
type AAGUIDAllowlistShapeError struct {
	// Detail narrows the refusal when the shape was recognised but empty.
	Detail string
}

// Error implements error.
func (e *AAGUIDAllowlistShapeError) Error() string {
	msg := "the file is not a recognised AAGUID allowlist"
	if e != nil && e.Detail != "" {
		msg = e.Detail
	}
	return msg + "; expected either a JSON array of " +
		"{\"aaguid\": \"...\", \"name\": \"...\"} objects, or a decoded FIDO Metadata " +
		"Service v3 document — a JSON object with an \"entries\" array"
}

// AAGUIDAllowlistReadError is returned when the AAGUID allowlist file cannot be read.
type AAGUIDAllowlistReadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *AAGUIDAllowlistReadError) Error() string {
	return "iam.aaguid: failed to read allowlist file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AAGUIDAllowlistReadError) Unwrap() error { return e.Cause }

// AAGUIDAllowlistParseError is returned when the AAGUID allowlist file cannot be parsed.
type AAGUIDAllowlistParseError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *AAGUIDAllowlistParseError) Error() string {
	return "iam.aaguid: failed to parse allowlist file " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AAGUIDAllowlistParseError) Unwrap() error { return e.Cause }

// AAGUIDModeError reports an unrecognised iam.aaguid.mode.
//
// It is an error rather than a silent fall-back to strict. Failing closed on a
// typo is safe, but it is not KIND: an operator who wrote "log-only" and got a
// node refusing every authenticator has no way to tell that from a broken
// allowlist file. Naming the accepted values turns a mystery into a one-line
// fix.
type AAGUIDModeError struct {
	// Mode is the operator-supplied value that did not parse.
	Mode string
}

// Error implements error.
func (e *AAGUIDModeError) Error() string {
	if e == nil {
		return "iam.aaguid.mode: invalid value"
	}
	return "iam.aaguid.mode: unrecognised value \"" + e.Mode +
		"\"; must be \"enforce\" (reject authenticators outside the allowlist) or " +
		"\"log_only\" (permit them and log a warning). An empty value means enforce."
}
