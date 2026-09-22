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

package fips

import (
	"sort"
	"strings"
)

// The symmetric AEAD policy for a FIPS-activated process, in one place because
// two callers need the same answer: the KMS service, which is where a data
// encryption key's algorithm is actually chosen, and configuration validation,
// which is where a deployment states its intent before anything starts.
//
// Owner ruling 8 (handbook, software/docs/04-security-model.md): ChaCha20-
// Poly1305 stays a DEK option, and under FIPS activation the DEK is AES-GCM.
// ChaCha20-Poly1305 has no FIPS 140-3 approval and never will — it is not a
// question of parameters — so a FIPS-activated process refuses it rather than
// quietly substituting something the operator did not ask for.

// approvedAEADs is the set of symmetric AEAD algorithms FIPS 140-3 approves,
// keyed by canonical name. AES-GCM in all three key sizes is approved; nothing
// else this codebase can name is.
var approvedAEADs = map[string]struct{}{
	"AES-128-GCM": {},
	"AES-192-GCM": {},
	"AES-256-GCM": {},
}

// AEADNotApprovedError reports a symmetric AEAD algorithm that FIPS 140-3 does
// not approve, selected by a process that runs FIPS-activated.
//
// The mode is the constraint, not the request: the same selection is valid on
// a build that is not FIPS-activated, so the message says so rather than
// reading as an unknown algorithm or a corrupt configuration.
type AEADNotApprovedError struct {
	// Operation is the call site that refused, e.g. "kms.CreateDEK".
	Operation string

	// Algorithm is the algorithm as it was selected.
	Algorithm string

	// Approved lists the algorithms that would have been accepted.
	Approved []string
}

// NewAEADNotApprovedError builds the error for an operation and the algorithm
// it refused, filling in the approved set the operator can choose from.
func NewAEADNotApprovedError(operation, algorithm string) *AEADNotApprovedError {
	return &AEADNotApprovedError{
		Operation: operation,
		Algorithm: algorithm,
		Approved:  ApprovedAEADs(),
	}
}

// Error implements the error interface.
func (e *AEADNotApprovedError) Error() string {
	algorithm := e.Algorithm
	if strings.TrimSpace(algorithm) == "" {
		algorithm = "(unset)"
	}
	return e.Operation + ": " + algorithm +
		" is not a FIPS 140-3 approved AEAD and this process is FIPS-activated; approved: " +
		strings.Join(e.Approved, ", ")
}

// ApprovedAEAD reports whether the named symmetric AEAD algorithm is FIPS
// 140-3 approved. Names are matched case-insensitively with surrounding
// whitespace ignored.
//
// It fails closed: a name this package does not recognize — including the
// empty string, which states no algorithm at all — is not approved. A caller
// that treats an unset algorithm as "use the default" must resolve that
// default before asking.
func ApprovedAEAD(algorithm string) bool {
	_, ok := approvedAEADs[canonicalAEADName(algorithm)]
	return ok
}

// ApprovedAEADs returns the approved algorithm names in a stable order, so
// operator-facing messages do not reorder between runs.
func ApprovedAEADs() []string {
	names := make([]string, 0, len(approvedAEADs))
	for name := range approvedAEADs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RequireApprovedAEAD returns nil unless this process is FIPS-activated and
// the named algorithm is unapproved, in which case it returns a typed
// *AEADNotApprovedError. On a build that is not FIPS-activated every algorithm
// is permitted, which is the whole point of one FIPS-capable artifact serving
// both markets (boundary ruling 1).
func RequireApprovedAEAD(operation, algorithm string) error {
	return requireApprovedAEAD(Enabled(), operation, algorithm)
}

// requireApprovedAEAD is RequireApprovedAEAD with the activation state passed
// in, so both branches are reachable from a single build under test.
func requireApprovedAEAD(activated bool, operation, algorithm string) error {
	if !activated || ApprovedAEAD(algorithm) {
		return nil
	}
	return NewAEADNotApprovedError(operation, algorithm)
}

// canonicalAEADName normalizes an algorithm name for lookup.
func canonicalAEADName(algorithm string) string {
	return strings.ToUpper(strings.TrimSpace(algorithm))
}
