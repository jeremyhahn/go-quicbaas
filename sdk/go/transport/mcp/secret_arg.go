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

import "unicode/utf8"

// SecretNotTextError reports a credential whose bytes are not valid UTF-8.
// An MCP tool argument is a JSON string, which cannot carry such bytes: the
// encoder would replace them and the server would receive a different
// credential, so the call is refused before any request is sent.
type SecretNotTextError struct {
	// Field is the tool argument the credential was bound for.
	Field string
}

// Error implements the error interface.
func (e *SecretNotTextError) Error() string {
	return "mcp: " + e.Field + " is not valid UTF-8 and cannot be carried losslessly as an MCP string argument"
}

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
//
// Bytes that are not valid UTF-8 do not survive a JSON string, so they are
// refused with *SecretNotTextError for field rather than sent altered.
func secretArg(field string, b []byte) (string, error) {
	if !utf8.Valid(b) {
		return "", &SecretNotTextError{Field: field}
	}
	return string(b), nil
}
