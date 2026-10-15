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

// Package tpmsim provisions a go-xkms TPM on the in-process TPM 2.0
// simulator for tests that need the real producer of TPM evidence rather
// than a double: seal and unseal, credential activation, quotes.
//
// The simulator is linked only under the tpm_simulator build tag (go-xkms
// stubs it out otherwise), so Provision exists only under that tag too. This
// file carries no tag so the package always builds.
package tpmsim
