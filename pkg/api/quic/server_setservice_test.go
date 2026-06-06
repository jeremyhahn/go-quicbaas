//go:build quic

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

package quic

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SetBarrierService / SetCeremonyService wire the lifecycle-supplied
// servicers into the server. They must accept both a concrete servicer and
// nil (disable) without panicking.
func TestServer_SetBarrierAndCeremonyService(t *testing.T) {
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	server, err := NewServer(&mockService{}, config, createServerTestLogger())
	require.NoError(t, err)

	server.SetBarrierService(&mockBarrierServicer{})
	server.SetBarrierService(nil)

	server.SetCeremonyService(&mockCeremonyServicer{})
	server.SetCeremonyService(nil)
}
