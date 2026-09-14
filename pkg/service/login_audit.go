// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// Login-ceremony rejection reporting.
// =============================================================================
//
// Every login rejection reaches the caller as the SAME uniform 401 — that is
// deliberate (RespondLoginRejected, pkg/api/rest), because a differentiated
// error is a tenant/user enumeration oracle. The cost is that the operator
// sees exactly what the attacker sees: nothing.
//
// So the reason has to be recorded on the SERVER side or it does not exist
// anywhere. Before this seam it did not: a failed FIDO2 login produced no log
// line at any level and no audit event, so four materially different payloads
// yielded byte-identical evidence and the defect could only be chased by
// reading source (go-quicbaas#484).
//
// The seam lives on the facade rather than in a transport handler because the
// facade is the ONE funnel every transport already routes through (REST, gRPC,
// MCP, embedded, ...). Recording here means a rejection is reported once, the
// same way, whichever protocol presented it.
//
// WHAT IS SAFE TO RECORD: the lookup key is reduced to a SHA-256 digest plus
// its byte length. The digest is comparable against an enrolled factor's
// credential digest — which is what "did the client present the right key?"
// actually asks — and the length distinguishes the encoding mistakes that
// produce a resolution miss (raw credential bytes vs. the base64url string vs.
// base64 of that string), without ever writing a credential identifier into a
// log. The proof itself (Signature, assertion) is NEVER recorded.

// The caller controls the session id and the method-metadata key names, and
// both are echoed into the record. An unauthenticated POST can therefore
// choose what gets written to the log and the audit store, so every
// client-derived string is bounded before it is recorded and the key set is
// capped. Without this the reporting seam added for go-quicbaas#484 would be
// a log-amplification primitive reachable without credentials.
const (
	// maxRecordedFieldLen bounds any single client-derived string. Generous
	// against real values (a UUID session id, a base64url credential id) and
	// far below what makes a log line an attack.
	maxRecordedFieldLen = 256

	// maxRecordedMetadataKeys bounds how many metadata key names are kept.
	// The FIDO2 contract uses two; PIV uses a handful.
	maxRecordedMetadataKeys = 16

	// truncationMarker is appended to any value that was cut, so a reader
	// never mistakes a bounded value for a complete one.
	truncationMarker = "...[truncated]"
)

// bounded returns s truncated to maxRecordedFieldLen, marked when it was cut.
func bounded(s string) string {
	if len(s) <= maxRecordedFieldLen {
		return s
	}
	return s[:maxRecordedFieldLen] + truncationMarker
}

// LoginStage names which half of the ceremony rejected the attempt. The two
// halves fail for genuinely different reasons — begin cannot resolve a
// relying party or a tenant, finish cannot resolve a factor or verify a
// proof — so an operator reading the record needs to know which ran.
type LoginStage string

const (
	// LoginStageBegin is the challenge-issuing half (BeginLogin).
	LoginStageBegin LoginStage = "begin"

	// LoginStageFinish is the proof-consuming half (FinishLogin).
	LoginStageFinish LoginStage = "finish"
)

// LoginRejection is the server-side account of one refused login attempt. It
// carries everything an operator needs to tell an encoding bug from a missing
// enrollment from a failed signature, and nothing that would let the record
// itself become a credential.
type LoginRejection struct {
	// Stage is the ceremony half that refused the attempt.
	Stage LoginStage

	// FactorType is the factor the caller presented ("fido2", "piv",
	// "mtls", "pkcs11"), verbatim from the request.
	FactorType string

	// SessionID is the ceremony session id. Empty on a begin rejection —
	// no session exists yet.
	SessionID string

	// TenantID is the tenant the attempt was scoped to. Known on begin
	// (the caller names it) and only from context on finish, because
	// qrdb's FinishLoginRequest carries no tenant.
	TenantID string

	// Subject is the caller-supplied identity hint (an email or username)
	// from BeginLogin. Empty on finish: the proof identifies the user by
	// lookup key, and when resolution fails there is no subject to name.
	Subject string

	// FactorID is the enrolled factor the attempt resolved to before it
	// was refused. Non-empty exactly when resolution succeeded and
	// VERIFICATION failed — which is the single most diagnostic
	// distinction in this record.
	FactorID string

	// LookupKeyDigest is the hex SHA-256 of the presented lookup key, or
	// empty when none was presented.
	LookupKeyDigest string

	// LookupKeyLen is the byte length of the presented lookup key. A
	// length that does not match the enrolled encoding is the signature of
	// a client/server encoding disagreement.
	LookupKeyLen int

	// MetadataKeys are the method-metadata key NAMES the caller supplied,
	// never their values. A missing required key is a contract mismatch
	// and this is how it becomes visible.
	MetadataKeys []string

	// Reason is the typed server-side error rendered in full. It is
	// recorded here and NEVER returned to the caller.
	Reason string
}

// LoginCeremonyAuditor receives the server-side account of a refused login.
//
// Implementations are expected to be non-blocking and to swallow their own
// failures: a rejection report must never change the outcome of the ceremony
// that produced it, and an audit sink that is down must not convert a 401 into
// a 500.
type LoginCeremonyAuditor interface {
	// RecordLoginRejection reports one refused login attempt.
	RecordLoginRejection(ctx context.Context, rejection LoginRejection)
}

// describeBeginRejection builds the record for a refused BeginLogin.
func describeBeginRejection(req *qrdbtransport.BeginLoginRequest, err error) LoginRejection {
	rejection := LoginRejection{
		Stage:  LoginStageBegin,
		Reason: rejectionReason(err),
	}
	if req != nil {
		rejection.FactorType = bounded(string(req.Type))
		rejection.TenantID = bounded(req.TenantID)
		rejection.Subject = bounded(req.Hint)
	}
	rejection.FactorID = rejectionFactorID(err)
	return rejection
}

// describeFinishRejection builds the record for a refused FinishLogin.
func describeFinishRejection(req *qrdbtransport.FinishLoginRequest, err error) LoginRejection {
	rejection := LoginRejection{
		Stage:  LoginStageFinish,
		Reason: rejectionReason(err),
	}
	if req != nil {
		rejection.FactorType = bounded(string(req.Type))
		rejection.SessionID = bounded(req.SessionID)
		rejection.LookupKeyDigest = lookupKeyDigest(req.LookupKey)
		rejection.LookupKeyLen = len(req.LookupKey)
		rejection.MetadataKeys = metadataKeys(req.MethodMetadata)
	}
	rejection.FactorID = rejectionFactorID(err)
	return rejection
}

// describeVerifiedFinishRejection builds the record for a login refused AFTER
// the factor proof passed. It is the one rejection that can name the
// authenticated subject and the factor that authorised it, because both were
// resolved before the failure.
func describeVerifiedFinishRejection(
	req *qrdbtransport.FinishLoginRequest,
	resp *qrdbtransport.FinishLoginResponse,
	err error,
) LoginRejection {
	rejection := describeFinishRejection(req, err)
	if resp == nil {
		return rejection
	}
	if resp.FactorID != "" {
		rejection.FactorID = resp.FactorID
	}
	if resp.Identity != nil {
		rejection.Subject = bounded(resp.Identity.Subject)
		rejection.TenantID = bounded(resp.Identity.TenantID)
	}
	return rejection
}

// rejectionReason renders the server-side error. Typed errors already print
// their own detail (qrdb's FactorVerificationError carries the verifier's
// reason, LoginSessionNotFoundError carries the session id), so the rendered
// error IS the diagnosis; no reclassification is needed or wanted.
func rejectionReason(err error) string {
	if err == nil {
		return ""
	}
	// Typed errors embed request-supplied values (LoginSessionNotFoundError
	// renders the caller's session id), so the rendered error is bounded like
	// any other client-derived string.
	return bounded(err.Error())
}

// rejectionFactorID returns the factor a rejection names, when it names one.
//
// Only qrdb's FactorVerificationError does, and that is the point: its
// presence in the record means resolution SUCCEEDED and the proof was
// refused, which separates "this credential is not enrolled" from "this
// credential is enrolled and the signature did not check out" — the two
// failures that are indistinguishable from the client side.
func rejectionFactorID(err error) string {
	var verification *qrdbtransport.FactorVerificationError
	if errors.As(err, &verification) {
		return verification.FactorID
	}
	var notFound *qrdbtransport.FactorNotFoundError
	if errors.As(err, &notFound) {
		return notFound.FactorID
	}
	return ""
}

// lookupKeyDigest reduces a presented lookup key to a comparable hex digest.
// An empty key yields an empty digest so "no key presented" stays visibly
// different from "a key that hashes to the digest of nothing".
func lookupKeyDigest(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])
}

// metadataKeys returns the sorted key names of the caller's method metadata.
// Values are deliberately dropped: they carry the proof material.
func metadataKeys(metadata map[string]string) []string {
	if len(metadata) == 0 {
		return nil
	}
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, bounded(key))
	}
	slices.Sort(keys)
	if len(keys) > maxRecordedMetadataKeys {
		// Sorted first, so the retained set is deterministic rather than
		// whichever keys the map happened to yield.
		keys = keys[:maxRecordedMetadataKeys]
	}
	return keys
}
