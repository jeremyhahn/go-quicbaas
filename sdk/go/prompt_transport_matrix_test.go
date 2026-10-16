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
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/grpc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/quic"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/unix"
)

// TestPromptTransportSupportMatrix pins that EVERY transport carries the
// Prompts service: each row declares prompt.PromptsCapableTransport, answers
// SupportsPrompts, and the SDK client never refuses it as unwired. Unix
// delegates to gRPC and QUIC embeds REST; their wire behaviour is proven by
// the unix delegation test and the quic HTTP/3 test.
func TestPromptTransportSupportMatrix(t *testing.T) {
	cases := []struct {
		name string
		tr   transport.Transport
	}{
		{"rest", &rest.Transport{}},
		{"quic", &quic.Transport{}},
		{"grpc", &grpc.Transport{}},
		{"unix", &unix.Transport{}},
		{"mcp", &mcp.Transport{}},
		{"embedded", &embedded.Transport{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capable, declared := tc.tr.(prompt.PromptsCapableTransport)
			if !declared || !capable.SupportsPrompts() {
				t.Fatalf("transport %q does not carry the Prompts service", tc.name)
			}
			// An empty address is refused by the client before any traffic,
			// so the only refusal a wired transport may give is the
			// validation one, never the unwired-transport one.
			_, err := prompt.NewClient(tc.tr).Effective(context.Background(), &prompt.Address{})
			if stderrors.Is(err, &errors.PromptTransportUnsupportedError{}) {
				t.Fatalf("transport %q is refused as unwired: %v", tc.name, err)
			}
			if !errors.IsInvalidInput(err) {
				t.Fatalf("transport %q: an empty address = %v, want InvalidInput", tc.name, err)
			}
		})
	}
}
