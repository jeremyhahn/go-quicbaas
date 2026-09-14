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

package prompts

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashPrefix names the digest a hash was taken with, so a stored hash stays
// readable when the algorithm changes.
const HashPrefix = "sha256:"

// Hash identifies a body by content.
//
// It is what a job records for every prompt it ran with, so "which words did
// this run actually use" is answerable from the record rather than from
// whatever the store holds now.
func Hash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return HashPrefix + hex.EncodeToString(sum[:])
}
