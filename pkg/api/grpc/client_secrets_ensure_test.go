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

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestEnsureSecretGRPC_InvalidInput verifies the input-validation guard on the
// gRPC client (nil request / empty secret_id are rejected before any RPC). The
// happy path is exercised by the integration adapter suite against a real
// server.
func TestEnsureSecretGRPC_InvalidInput(t *testing.T) {
	c := &Client{}
	cases := []struct {
		name string
		req  *service.EnsureSecretRequest
	}{
		{"nil request", nil},
		{"empty secret id", &service.EnsureSecretRequest{Length: 8, Encoding: "hex"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.EnsureSecret(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected InvalidArgument error")
			}
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("expected InvalidArgument, got: %v", err)
			}
		})
	}
}
