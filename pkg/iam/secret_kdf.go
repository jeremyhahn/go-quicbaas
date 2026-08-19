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
	// 600,000 is OWASP's current guidance for PBKDF2-HMAC-SHA256 and measures
	// at ~57ms here, which is what bcrypt at DefaultCost cost before it.
	//
	// It is a single accepted value rather than a per-digest one, and that is
	// a security property rather than a simplification. Authentication is
	// required to be non-enumerating: every attempt, against a real account or
	// an unknown client ID, must cost the same. An ineligible account is
	// checked against timingEqualizerDigest, which is fixed at this count — so
	// if a stored digest could carry some other count, an account holding a
	// cheaper one would answer measurably faster than an unknown client ID and
	// its existence would leak, however identical the returned error. A dearer
	// one leaks the same way and doubles as a denial of service, since the
	// stored value would be dictating how long a request worker is occupied.
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
)

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
		key:        key.Bytes(),
	}), nil
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

	return subtle.ConstantTimeCompare(candidate.Bytes(), digest.key) == 1
}
