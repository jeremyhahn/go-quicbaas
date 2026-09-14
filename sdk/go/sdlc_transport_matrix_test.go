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

package quicbaas

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/unix"
)

// TestSDLCTransportSupportMatrix pins that EVERY transport carries the SDLC
// service (docs/00 L4, Tier 1): each transport package asserts
// sdlc.SDLCCapableTransport at compile time, and this test refuses a row
// silently dropping the marker. The Unix transport delegates to gRPC
// verbatim and the QUIC transport embeds REST, so their capability is the
// handler that actually routes, not a wrapper promotion.
func TestSDLCTransportSupportMatrix(t *testing.T) {
	cases := []struct {
		name string
		tr   interface{}
	}{
		{"rest", (*rest.Transport)(nil)},
		{"quic", (*quic.Transport)(nil)},
		{"grpc", (*grpc.Transport)(nil)},
		{"unix", (*unix.Transport)(nil)},
		{"mcp", (*mcp.Transport)(nil)},
		{"embedded", (*embedded.Transport)(nil)},
	}
	for _, tc := range cases {
		// A nil pointer is enough to see the declaration; QUIC promotes the
		// marker from the REST transport it embeds, so the method itself is
		// exercised by each transport's own tests, not through a nil here.
		if _, declared := tc.tr.(sdlc.SDLCCapableTransport); !declared {
			t.Errorf("transport %q does not declare SDLC support; every transport carries the module's service", tc.name)
		}
	}
}
