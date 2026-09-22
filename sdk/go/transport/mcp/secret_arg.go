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

package mcp

// secretArg renders a barrier credential as the plaintext string an MCP tool
// argument has to be.
//
// This is MCP's documented exception to go-qrdb's wipeable-credential
// contract (go-qrdb#184), mirrored here so this transport has exactly one
// place where the conversion happens, as go-qrdb does with its own
// secretArg.
//
// It is NOT cosmetic. An MCP tool argument is carried as JSON, and Go's
// encoder renders a []byte as BASE64 — handing `req.Secret` straight to the
// argument map would send "cHc=" where the daemon expects "pw", and an
// existing barrier could no longer be unsealed with its real passphrase over
// this transport. The conversion has to be explicit and typed as a string.
//
// The caller's own []byte stays wipeable and every caller still wipes it, so
// the residue is bounded to the string returned here. A caller that cannot
// accept that residue should not choose MCP for barrier operations.
func secretArg(b []byte) string {
	return string(b)
}
