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

package subprotocol

import (
	"crypto/fips140"
	"os"
	"testing"
)

// TestMain runs the subprotocol handler tests with the RFC 6455 handshake
// permitted under GODEBUG=fips140=only. These tests drive the handlers over a
// real connection, and the handshake's Sec-WebSocket-Accept needs SHA-1, which
// the Go Cryptographic Module refuses in only-mode. The WebSocket transport is
// itself refused in that mode (websocket.NewServer), so no deployment reaches
// these handlers there; the exemption only lets the handler logic be tested.
// WithoutEnforcement is inherited by every goroutine the tests spawn.
func TestMain(m *testing.M) {
	code := 0
	fips140.WithoutEnforcement(func() {
		code = m.Run()
	})
	os.Exit(code)
}
