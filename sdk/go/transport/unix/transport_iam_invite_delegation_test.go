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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestTransport_Invite_Delegates verifies every quicbaas.IAM.Invite verb is
// carried by the gRPC transport the Unix transport delegates to: with no
// server listening the call fails on the connection, never as unsupported or
// as a malformed request.
func TestTransport_Invite_Delegates(t *testing.T) {
	cases := []struct {
		method string
		req    map[string]interface{}
	}{
		{"GetInvite", map[string]interface{}{"tenant_id": "acme", "token": "one-time-token"}},
		{"ActivateFIDO2Begin", map[string]interface{}{"tenant_id": "acme", "token": "one-time-token"}},
		{"ActivateFIDO2Finish", map[string]interface{}{
			"tenant_id": "acme", "token": "one-time-token",
			"credential": map[string]interface{}{"id": "cred"},
		}},
		{"ActivatePIV", map[string]interface{}{"tenant_id": "acme", "token": "one-time-token", "cert_pem": "CERT-PEM"}},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.IAM.Invite", tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("%s was rejected as unsupported, dispatch regression: %v", tc.method, err)
			}
			if errors.IsInvalidInput(err) {
				t.Fatalf("a well-formed %s request was refused as invalid: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_Invite_MissingTenant verifies a request without the tenant the
// server routes by is refused as invalid input before any dial.
func TestTransport_Invite_MissingTenant(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.IAM.Invite", "ActivatePIV",
		map[string]interface{}{"token": "one-time-token", "cert_pem": "CERT-PEM"})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("expected an invalid-input error, got %v", err)
	}
}
