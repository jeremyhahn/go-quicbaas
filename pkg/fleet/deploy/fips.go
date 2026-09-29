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

package deploy

import (
	"crypto/fips140"

	"golang.org/x/crypto/ssh"
)

// The SSH transport negotiates three algorithm classes independently — key
// exchange, cipher and MAC — and golang.org/x/crypto/ssh's defaults put an
// unapproved algorithm first in every one of them: X25519 for key exchange,
// AES-GCM (with an SSH-counter nonce, which crypto/cipher rejects as an
// "arbitrary IV") for the cipher, and it also offers ChaCha20-Poly1305. Under
// GODEBUG=fips140=only the Go Cryptographic Module refuses each in turn, so the
// handshake dies before authentication and agent remote deploy is unavailable
// on exactly the regulated hosts docs/04 requires the platform to run on
// (#111). Constraining all three classes to their FIPS-approved members lets
// SSHClient connect to any peer that also offers one (OpenSSH does by default)
// and fails closed rather than silently negotiating an unapproved algorithm.

// fipsApprovedKeyExchanges is the FIPS-approved key-exchange set: ECDH over the
// NIST P-curves with SHA-2.
var fipsApprovedKeyExchanges = []string{
	ssh.KeyExchangeECDHP256,
	ssh.KeyExchangeECDHP384,
	ssh.KeyExchangeECDHP521,
}

// fipsApprovedCiphers is the FIPS-approved cipher set: AES in CTR mode. AES-GCM
// as SSH uses it is refused for its arbitrary-IV construction, and
// ChaCha20-Poly1305 has no FIPS approval.
var fipsApprovedCiphers = []string{
	ssh.CipherAES256CTR,
	ssh.CipherAES192CTR,
	ssh.CipherAES128CTR,
}

// fipsApprovedMACs is the FIPS-approved MAC set: HMAC-SHA-2, encrypt-then-MAC
// variants first. HMAC-SHA-1 and UMAC are excluded.
var fipsApprovedMACs = []string{
	ssh.HMACSHA512ETM,
	ssh.HMACSHA256ETM,
	ssh.HMACSHA512,
	ssh.HMACSHA256,
}

// applyFIPSAlgorithms constrains the client's negotiable key exchanges, ciphers
// and MACs to the FIPS-approved sets when the process runs in FIPS 140-3 mode.
// It fails closed: with only approved algorithms offered, a handshake against a
// peer that provides none in any class aborts — surfacing as an SSHDialError —
// rather than negotiating an unapproved algorithm the module would then refuse.
// On a non-FIPS host it leaves the fields nil so x/crypto/ssh keeps its default
// preference order unchanged.
func applyFIPSAlgorithms(cfg *ssh.ClientConfig) {
	if cfg == nil {
		return
	}
	if !fips140.Enabled() {
		return
	}
	cfg.KeyExchanges = fipsApprovedKeyExchanges
	cfg.Ciphers = fipsApprovedCiphers
	cfg.MACs = fipsApprovedMACs
}
