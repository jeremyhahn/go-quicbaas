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

package sdlc

import "crypto/fips140"

// fipsEnabled reports whether the FIPS 140-3 module is enforcing; tests
// override it to cover the FIPS refusal on any host.
var fipsEnabled = fips140.Enabled

// minFIPSWebhookSecretBytes is the shortest webhook signing secret accepted in
// FIPS mode. FIPS 140-3 refuses HMAC keys shorter than 112 bits, so in FIPS
// mode a shorter secret is refused before any MAC is computed.
const minFIPSWebhookSecretBytes = 14
