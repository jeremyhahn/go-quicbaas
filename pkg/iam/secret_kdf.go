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

package iam

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// Client secrets rest as a PBKDF2-HMAC-SHA256 digest.
//
// PBKDF2 is the KDF the owner ratified for this repository. It is also the
// only choice here that survives the repository's own compliance gate: CI runs
// with GODEBUG=fips140=only, and PBKDF2-HMAC-SHA256 is approved under
// SP 800-132 while bcrypt's Blowfish core is not approved at all. The
// derivation below uses the standard library's crypto/pbkdf2, which is served
// by the Go Cryptographic Module, rather than golang.org/x/crypto/pbkdf2,
// which is not.
const (
	// pbkdf2Scheme names the derivation in the stored digest. It exists so a
	// future migration to a different KDF can be told apart by parsing rather
	// than by guessing.
	pbkdf2Scheme = "pbkdf2-sha256"

	// pbkdf2Iterations is the one work factor this package will verify at.
	// 600,000 is OWASP's current guidance for PBKDF2-HMAC-SHA256.
	//
	// MEASURE IT BEFORE QUOTING IT, and say on WHAT. The cost of one
	// verification is hardware-dependent across roughly a 5x range, so a bare
	// figure is not a fact about this code — it is a fact about one machine:
	//
	//	~57.7 ms   Intel Core Ultra 9 285K, Go 1.26, SHA extensions available
	//	~131.6 ms  same host and build, GODEBUG=cpu.sha=off
	//	~308 ms    recorded 2026-08-25 on a 32-core host whose CPU model was
	//	           not captured — carried forward as evidence that slower
	//	           deployments exist, NOT as a reproducible data point. Anyone
	//	           relying on it should re-measure and record the model.
	//
	// (The first two are medians of 7 runs at these exact parameters,
	// measured 2026-08-26.)
	//
	// An earlier revision called the ~57 ms figure stale and replaced it with
	// ~308 ms. It was not stale — it is what this hardware actually does. Nor
	// is the gap explained by SHA acceleration alone: disabling the SHA
	// extensions here costs ~2.3x, not 5x, so the rest is the difference
	// between machines. Both numbers are real; neither supersedes the other.
	//
	// Which end to reason from depends on the question, and the two answers
	// point OPPOSITE ways — carrying one across to the other inverts it:
	//
	//   - The WORK FACTOR is justified at the FAST end. An attacker brings
	//     good hardware, so 600,000 iterations has to be defensible at
	//     ~57.7 ms. Reading the slow figure as "the" cost makes the factor
	//     look 5x more expensive than it is and argues for lowering it on a
	//     credential path, which is the wrong direction.
	//   - SERVER CAPACITY, and any rate limit protecting it, is reasoned at
	//     the SLOW end. At a fixed request rate a slower host spends more CPU
	//     per derivation, so the same limit protects it LESS: 10 req/s costs
	//     ~0.6 of a core at ~57.7 ms but ~3.1 cores at ~308 ms. A bound
	//     justified on fast hardware does not carry to a slow deployment.
	//
	// Either way an UNAUTHENTICATED path reaching this KDF needs a rate limit
	// ahead of it: even at ~57.7 ms a single core serves only ~17
	// verifications per second, so a handful of sources saturates a host
	// (#118 F1, #167).
	//
	// It is a single accepted value rather than a per-digest one, and that is
	// a security property rather than a simplification. Authentication is
	// required to be non-enumerating: every attempt that REACHES verification,
	// against a real account or an unknown client ID, must cost the same. An
	// ineligible account is checked against timingEqualizerDigest, which is
	// fixed at this count — so if a stored digest could carry some other count,
	// an account holding a cheaper one would answer measurably faster than an
	// unknown client ID and its existence would leak, however identical the
	// returned error. A dearer one leaks the same way and doubles as a denial
	// of service, since the stored value would be dictating how long a request
	// worker is occupied.
	//
	// The qualifier "that reaches verification" is load-bearing and was added
	// after this comment was found asserting more than the code delivers.
	// AuthenticateServiceAccount does equalize every outcome. The OAuth2
	// application path does NOT yet: AppManager.ValidateClient looks the app up
	// first and returns on a miss without deriving, so an unknown client ID
	// answers in microseconds while a known one with a wrong secret pays the
	// full cost. That gap is #150 and is NOT claimed fixed here.
	//
	// The cost of this is that raising the work factor does not migrate
	// existing secrets: digests at the old count stop parsing, so they are
	// classified legacy and fail closed until InvalidateLegacySecretHashes and
	// RotateCredentials reissue them. That is the behavior already specified
	// for any unrecognized stored value, and service accounts are short-lived
	// machine identities the orchestrator creates and deletes per job, so the
	// rotation is cheap. Raising this value also requires regenerating
	// timingEqualizerDigest; TestTimingEqualizerDigest_MatchesCurrentParameters
	// fails until you do.
	pbkdf2Iterations = 600000

	// pbkdf2SaltLength is 16 bytes. FIPS 140-3 requires a PBKDF2 salt of at
	// least 128 bits, and crypto/pbkdf2 refuses anything shorter under
	// GODEBUG=fips140=only.
	pbkdf2SaltLength = 16

	// pbkdf2KeyLength is the derived key length, matching SHA-256's output so
	// the digest carries the full strength of the underlying PRF.
	pbkdf2KeyLength = 32
)

// secretHashEncoding is unpadded base64, keeping the '$'-delimited digest free
// of '=' so the field stays a single unambiguous token.
var secretHashEncoding = base64.RawStdEncoding

// Sentinel errors for client-secret derivation and parsing.
var (
	// ErrSecretHashMalformed indicates a stored value is not a digest this
	// package wrote.
	ErrSecretHashMalformed = errors.New("iam: client secret hash is malformed")

	// ErrSecretRotationRequired indicates the stored value is a pre-#37 bcrypt
	// digest. Authentication against it is refused rather than attempted, so
	// this is deliberately distinguishable from "wrong secret": the credential
	// is not bad, the record is obsolete, and the operator's action is to
	// rotate it rather than to debug a broken login.
	ErrSecretRotationRequired = errors.New("iam: stored client secret predates the current KDF and must be rotated")

	// ErrSecretEmpty indicates an empty client secret was offered for hashing.
	// An empty secret carries no entropy, so a digest of one is a credential
	// that authenticates anybody who presents nothing.
	ErrSecretEmpty = errors.New("iam: client secret must not be empty")
)

// SecretRotationRequiredError reports that a stored client secret is in the
// pre-#37 bcrypt form, which this package no longer verifies. It carries no
// part of the stored value and no client identifier: it says what must happen,
// not to whom.
type SecretRotationRequiredError struct{}

// Error implements the error interface.
func (e *SecretRotationRequiredError) Error() string {
	return ErrSecretRotationRequired.Error()
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SecretRotationRequiredError) Unwrap() error {
	return ErrSecretRotationRequired
}

// SecretHashMalformedError reports that a stored value could not be parsed as
// a PBKDF2 digest. Reason names the specific defect for logs; it never carries
// any part of the stored value, which for a pre-KDF record is the client
// secret itself.
type SecretHashMalformedError struct {
	Reason string
}

// Error implements the error interface.
func (e *SecretHashMalformedError) Error() string {
	if e.Reason == "" {
		return ErrSecretHashMalformed.Error()
	}
	return ErrSecretHashMalformed.Error() + ": " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SecretHashMalformedError) Unwrap() error {
	return ErrSecretHashMalformed
}

// secretDigest is a parsed PBKDF2 digest.
type secretDigest struct {
	iterations int
	salt       []byte
	key        []byte
}

// deriveSecretHash derives the stored form of a client secret: a
// self-describing PBKDF2-HMAC-SHA256 digest over a fresh random salt, encoded
// as
//
//	$pbkdf2-sha256$i=<iterations>$<base64 salt>$<base64 derived key>
//
// The digest is salted and non-invertible, so a compromised store yields no
// usable credential. The plaintext secret exists only in the value handed back
// to the caller at creation and rotation.
func deriveSecretHash(secret string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", &SecretHashError{Err: err}
	}

	key, err := deriveKey(secret, salt, pbkdf2Iterations)
	if err != nil {
		return "", &SecretHashError{Err: err}
	}
	defer key.Destroy()

	return formatSecretHash(&secretDigest{
		iterations: pbkdf2Iterations,
		salt:       salt,
		//secure:unborrowed injected-interface formatSecretHash consumes the derived key immediately before key.Destroy runs.
		key: key.Bytes(),
	}), nil
}

// HashClientSecret returns the stored form of a client secret the caller
// already holds, taking that secret in protected memory.
//
// An application registered through AppManager is given a generated secret and
// never needs this. It exists for the caller that must provision an
// application with a specific secret -- importing an existing client, or a
// test fixture that authenticates with a known value. Before the #37 migration
// such a caller produced the stored form itself with bcrypt; the PBKDF2 digest
// this package now stores cannot be constructed outside it, so without this
// the migration would leave that caller with no supported option.
//
// It takes *secure.Secret rather than a string so that no PUBLIC entry point
// of this package invites a credential to be held in immutable, unwipeable,
// garbage-collected memory (Codex review of #118, protected-memory mandate).
// The caller keeps the secret protected right up to this boundary and owns
// destroying it afterwards.
//
// HONEST LIMIT, do not read this as full protected-memory compliance: the
// FIPS-approved KDF this package is required to use, crypto/pbkdf2.Key, takes
// its password as a `string`. Deriving therefore materialises the secret as an
// immutable Go string for the duration of the call, and that copy cannot be
// wiped. This narrows the plaintext window from the caller's whole lifetime to
// one call and keeps the public surface protected-first; it does not eliminate
// the window, and it cannot while the derivation stays inside the Go
// Cryptographic Module. Every existing internal derivation path has the same
// window. The conflict between the protected-memory mandate and the FIPS
// mandate at this line is recorded for a ruling rather than papered over.
//
// The digest is salted, so two calls with the same secret return different
// values and neither may be compared for equality; verification goes through
// the storage layer's ValidateClientSecret.
func HashClientSecret(secret *secure.Secret) (string, error) {
	if secret == nil {
		return "", ErrSecretEmpty
	}

	buf, err := secret.Open()
	if err != nil {
		return "", &SecretHashError{Err: err}
	}
	defer buf.Destroy()

	//secure:unborrowed injected-interface deriveSecretHash currently accepts the PBKDF2 secret as a string.
	plaintext := buf.Bytes()
	// Refused at the provisioning boundary rather than at authentication.
	// deriveSecretHash("") is a perfectly well-formed digest, so without this
	// an application could be provisioned with an empty secret and would then
	// authenticate anyone presenting nothing (Codex review of #118).
	if len(plaintext) == 0 {
		return "", ErrSecretEmpty
	}

	//secure:unwiped injected-interface deriveSecretHash currently accepts the PBKDF2 secret as a string.
	return deriveSecretHash(string(plaintext))
}

// deriveKey runs the KDF and hands the result back in protected memory.
//
// crypto/pbkdf2 allocates and returns an ordinary slice, and there is no
// variant that writes into a caller's buffer, so the derived key does exist
// unprotected for the moment between those two statements. Copying it into a
// secure.Buffer and wiping the original is the narrowest window the standard
// library allows, and it is the same stack-owned abstraction the rest of this
// repository uses for key material (pkg/api/rest/secureio, pkg/service).
//
// The caller owns the returned buffer and must Destroy it.
func deriveKey(secret string, salt []byte, iterations int) (*secure.Buffer, error) {
	raw, err := pbkdf2.Key(sha256.New, secret, salt, iterations, pbkdf2KeyLength)
	if err != nil {
		return nil, err
	}
	defer secure.Wipe(raw)

	return secure.NewBufferFromBytes(raw)
}

// formatSecretHash encodes a digest in the stored representation.
func formatSecretHash(digest *secretDigest) string {
	return "$" + pbkdf2Scheme +
		"$i=" + strconv.Itoa(digest.iterations) +
		"$" + secretHashEncoding.EncodeToString(digest.salt) +
		"$" + secretHashEncoding.EncodeToString(digest.key)
}

// parseSecretHash decodes a stored digest, validating every field it will
// later feed to the KDF. It rejects rather than repairs: a value that does not
// parse is not treated as a digest at all, so a pre-KDF record can never be
// mistaken for one.
func parseSecretHash(stored string) (*secretDigest, error) {
	// A conforming digest is "", "pbkdf2-sha256", "i=<n>", salt, key once
	// split on '$' — the leading empty field comes from the leading '$'.
	fields := strings.Split(stored, "$")
	if len(fields) != 5 || fields[0] != "" {
		return nil, &SecretHashMalformedError{Reason: "expected 4 dollar-delimited fields"}
	}

	if fields[1] != pbkdf2Scheme {
		return nil, &SecretHashMalformedError{Reason: "unrecognized scheme"}
	}

	iterationField, ok := strings.CutPrefix(fields[2], "i=")
	if !ok {
		return nil, &SecretHashMalformedError{Reason: "missing iteration parameter"}
	}
	iterations, err := strconv.Atoi(iterationField)
	if err != nil {
		return nil, &SecretHashMalformedError{Reason: "iteration count is not an integer"}
	}
	// Exactly one work factor is accepted, and it is checked here so that a
	// non-conforming count never reaches the KDF. This is what keeps every
	// verification the same price: a cheaper stored digest would answer faster
	// than the equalizer and leak that the client exists, and a dearer one
	// would let a tampered record decide how long a request worker is held.
	if iterations != pbkdf2Iterations {
		return nil, &SecretHashMalformedError{Reason: "iteration count is not the accepted work factor"}
	}

	salt, err := secretHashEncoding.DecodeString(fields[3])
	if err != nil {
		return nil, &SecretHashMalformedError{Reason: "salt is not valid base64"}
	}
	// Exactly, not at least. This package always writes pbkdf2SaltLength, and
	// an over-long salt is as much of a problem as a short one: PBKDF2 hashes
	// the salt for every block it derives, so a tampered record carrying a
	// megabyte of valid base64 would cost measurably more to verify than the
	// fixed-cost equalizer — reopening the timing oracle from the other end
	// and letting stored data dictate CPU and allocation.
	if len(salt) != pbkdf2SaltLength {
		return nil, &SecretHashMalformedError{Reason: "salt is not the accepted length"}
	}

	key, err := secretHashEncoding.DecodeString(fields[4])
	if err != nil {
		return nil, &SecretHashMalformedError{Reason: "derived key is not valid base64"}
	}
	if len(key) != pbkdf2KeyLength {
		return nil, &SecretHashMalformedError{Reason: "derived key is the wrong length"}
	}

	return &secretDigest{iterations: iterations, salt: salt, key: key}, nil
}

// compareSecretHash reports whether secret derives to the stored digest.
//
// Every comparison costs the same, because parseSecretHash admits exactly one
// work factor: a digest carrying any other count does not parse, never reaches
// the KDF, and is reported as a mismatch. The comparison itself is constant
// time.
func compareSecretHash(stored, secret string) bool {
	digest, err := parseSecretHash(stored)
	if err != nil {
		return false
	}

	candidate, err := deriveKey(secret, digest.salt, digest.iterations)
	if err != nil {
		// Deriving failed, so nothing was compared. Reporting a mismatch keeps
		// the caller fail-closed, which is the same direction every other
		// unusable-input path in this file takes.
		return false
	}
	defer candidate.Destroy()

	// The stored key was decoded out of a string this package does not own, so
	// its copy is wiped here too rather than left for the collector.
	defer secure.Wipe(digest.key)

	//secure:unborrowed injected-interface ConstantTimeCompare reads the protected candidate without retaining it.
	return subtle.ConstantTimeCompare(candidate.Bytes(), digest.key) == 1
}
