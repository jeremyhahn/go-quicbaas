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

package auth

// ReservedKeyPrefixError is returned when a key uses a reserved internal prefix.
type ReservedKeyPrefixError struct {
	Prefix string
}

// Error implements the error interface.
func (e *ReservedKeyPrefixError) Error() string {
	return "key uses reserved internal prefix '" + e.Prefix + "'"
}
