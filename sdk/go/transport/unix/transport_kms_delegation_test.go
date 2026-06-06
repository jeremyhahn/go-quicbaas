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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// TestTransport_KMSSecretsCrypto_Delegate proves the Unix transport forwards
// KMS / Secrets / Crypto requests to the underlying gRPC transport's
// per-service dispatch rather than rejecting them as unsupported.
//
// With no server listening and a short deadline every call fails on connection
// — but crucially NOT with "transport not supported" / "not yet supported by
// SDK". Reaching a connection error proves the service name was classified and
// routed through the gRPC KMS/Secrets/Crypto handler that this change wired in.
// The gRPC handler-level tests assert the full proto round-trip; this test
// guards the inheritance contract that Unix delegates verbatim.
func TestTransport_KMSSecretsCrypto_Delegate(t *testing.T) {
	cases := []struct {
		name    string
		service string
		method  string
		req     interface{}
	}{
		{
			name:    "kms-generate-key",
			service: "quicbaas.KMS",
			method:  "GenerateKey",
			req:     &kmsclient.GenerateKeyRequest{Name: "k", Algorithm: kmsclient.AlgorithmAES256GCM},
		},
		{
			name:    "secrets-store",
			service: "quicbaas.Secrets",
			method:  "Store",
			req:     &secretsclient.StoreSecretRequest{Path: "/p", Data: map[string]interface{}{"k": "v"}},
		},
		{
			name:    "crypto-generate-random",
			service: "quicbaas.Crypto",
			method:  "GenerateRandom",
			req:     &service.GenerateRandomRequest{NumBytes: 16},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, tc.service, tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			// The dispatch must have routed through the typed handler, which on a
			// dead connection surfaces a connection/transport error — never the
			// classic "not supported" regression.
			if msg := err.Error(); strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("request was rejected as unsupported, dispatch regression: %v", err)
			}
		})
	}
}
