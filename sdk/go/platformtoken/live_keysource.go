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

package platformtoken

import (
	"context"
	"crypto"
)

// KeyFunc returns the issuer's verification key for kid. The validator calls
// it on every verification and never caches its answer, so the issuer decides
// each lookup: a key it has stopped trusting is refused at once, and a kid it
// has not loaded yet can be resolved by its own refresh. A kid the issuer
// holds no key for is reported with *UnknownKeyIDError.
type KeyFunc func(ctx context.Context, kid string) (crypto.PublicKey, error)

// liveKeySource resolves every kid through the issuer's KeyFunc.
type liveKeySource struct {
	lookup KeyFunc
}

// publicKey asks the issuer for kid's key.
func (s liveKeySource) publicKey(ctx context.Context, kid string) (crypto.PublicKey, error) {
	return s.lookup(ctx, kid)
}
