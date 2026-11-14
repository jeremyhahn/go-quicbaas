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

package service

import (
	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

// KeyAlgorithmFromAttributes names the KeyAlgorithm that go-xkms KeyAttributes
// describe, the same name the KMS service stores (AES-256-GCM when attrs is nil
// or names no algorithm).
func KeyAlgorithmFromAttributes(attrs *kctypes.KeyAttributes) KeyAlgorithm {
	return KeyAlgorithm(attrsToAlgorithm(attrs))
}
