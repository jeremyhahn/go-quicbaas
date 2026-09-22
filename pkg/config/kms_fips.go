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

package config

import (
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
)

// fipsActivated reports whether this process runs FIPS-activated. It is a
// variable so a test can reach both sides of the check on a single build;
// production always reads the runtime through fips.Enabled.
var fipsActivated = fips.Enabled

// validateFIPSDEKAlgorithm refuses a configured default DEK algorithm that
// FIPS 140-3 does not approve when this process is FIPS-activated. A
// deployment that states ChaCha20-Poly1305 is then told so at startup, rather
// than at its first key operation, because the mode — not the request — is
// what makes the selection impossible (owner ruling 8).
//
// An unset algorithm is not a selection: the shipped default is AES-256-GCM,
// which is approved, so it passes.
func validateFIPSDEKAlgorithm(algorithm string) error {
	if !fipsActivated() || strings.TrimSpace(algorithm) == "" {
		return nil
	}
	if fips.ApprovedAEAD(algorithm) {
		return nil
	}

	notApproved := fips.NewAEADNotApprovedError("kms.defaults.algorithm", algorithm)
	return &ValidationError{
		Field:   "kms.defaults.algorithm",
		Message: notApproved.Error(),
		Cause:   notApproved,
	}
}
