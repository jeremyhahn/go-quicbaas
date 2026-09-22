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
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
)

// Choosing a data encryption key's algorithm is the one moment the DEK's
// cipher is decided, so it is where owner ruling 8 is enforced: ChaCha20-
// Poly1305 stays a DEK option, and a FIPS-activated process gets AES-GCM or a
// typed refusal. Substituting AES silently would hand the operator a key that
// is not the one they asked for, under a name that says otherwise.

// fipsActivated reports whether this process runs FIPS-activated. It is a
// variable so a test can exercise both sides of the gate on a single build;
// production always reads the runtime through fips.Enabled.
var fipsActivated = fips.Enabled

// requireFIPSApprovedDEKAlgorithm refuses a DEK algorithm that FIPS 140-3 does
// not approve when this process is FIPS-activated, and permits everything
// otherwise. The algorithm must already be resolved — an empty name is not a
// selection and fails closed under activation.
//
// The returned error carries a *fips.AEADNotApprovedError, so a caller can
// tell a mode constraint from an unknown algorithm with errors.As, and is
// classified as invalid input because the request named something this
// deployment cannot serve.
func requireFIPSApprovedDEKAlgorithm(op string, algorithm KeyAlgorithm) error {
	if !fipsActivated() || fips.ApprovedAEAD(string(algorithm)) {
		return nil
	}
	return WrapError(
		fips.NewAEADNotApprovedError(op, string(algorithm)),
		errors.ErrInvalidInput,
		op,
		"",
	)
}
