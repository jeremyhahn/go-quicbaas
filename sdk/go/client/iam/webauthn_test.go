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

package iam

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
)

// webauthnRecorder wraps the package's mockTransport to record the dispatch
// and answer with a canned response.
type webauthnRecorder struct {
	gotService string
	gotMethod  string
}

// transportFor returns a mockTransport that records into rec and replies with
// resp.
func (rec *webauthnRecorder) transportFor(resp interface{}) *mockTransport {
	return &mockTransport{requestFunc: func(_ context.Context, service, method string, _ interface{}) (interface{}, error) {
		rec.gotService, rec.gotMethod = service, method
		return resp, nil
	}}
}

// TestWebAuthnClientDispatch pins the service name and method names the client
// sends. Every transport routes on these exact strings, so a rename here
// silently unroutes the verbs on all of them at once.
func TestWebAuthnClientDispatch(t *testing.T) {
	ctx := context.Background()

	t.Run("List", func(t *testing.T) {
		rec := &webauthnRecorder{}
		c := NewClient(rec.transportFor(map[string]interface{}{"credentials": []interface{}{}, "total": 0}))
		if _, err := c.ListWebAuthnCredentials(ctx, "t1", "u1"); err != nil {
			t.Fatalf("ListWebAuthnCredentials: %v", err)
		}
		if rec.gotService != "quicbaas.IAM.WebAuthn" || rec.gotMethod != "ListWebAuthnCredentials" {
			t.Errorf("dispatched %s/%s, want quicbaas.IAM.WebAuthn/ListWebAuthnCredentials",
				rec.gotService, rec.gotMethod)
		}
	})

	t.Run("DeleteOne", func(t *testing.T) {
		rec := &webauthnRecorder{}
		c := NewClient(rec.transportFor(map[string]interface{}{"success": true}))
		if err := c.DeleteWebAuthnCredential(ctx, "t1", "u1", "AbCd01"); err != nil {
			t.Fatalf("DeleteWebAuthnCredential: %v", err)
		}
		if rec.gotMethod != "DeleteWebAuthnCredential" {
			t.Errorf("method = %s, want DeleteWebAuthnCredential", rec.gotMethod)
		}
	})

	t.Run("DeleteAll", func(t *testing.T) {
		rec := &webauthnRecorder{}
		c := NewClient(rec.transportFor(map[string]interface{}{"deleted": 3}))
		n, err := c.DeleteWebAuthnCredentials(ctx, "t1", "u1")
		if err != nil {
			t.Fatalf("DeleteWebAuthnCredentials: %v", err)
		}
		if n != 3 {
			t.Errorf("deleted = %d, want 3", n)
		}
		if rec.gotMethod != "DeleteWebAuthnCredentials" {
			t.Errorf("method = %s, want DeleteWebAuthnCredentials", rec.gotMethod)
		}
	})
}

// TestWebAuthnClientDecodesEveryTransportShape is the cross-transport contract
// guard. REST returns decoded JSON, gRPC builds its map from protobuf scalars,
// and the embedded transport builds its map from the Go domain type — so the
// same field arrives as a JSON number, a uint32 and an int respectively. All
// three must decode to an identical credential, or the CLI would print
// different results depending on which transport carried the call.
func TestWebAuthnClientDecodesEveryTransportShape(t *testing.T) {
	shapes := map[string]interface{}{
		// REST: JSON-decoded, so every number is a float64.
		"rest": map[string]interface{}{
			"credentials": []interface{}{map[string]interface{}{
				"id": "AbCd01", "aaguid": "aabbccdd", "attestation_type": "packed",
				"transports": []interface{}{"usb", "nfc"},
				"sign_count": float64(7), "backup_eligible": true,
				"backup_state": true, "clone_warning": true,
			}},
			"total": float64(1),
		},
		// gRPC: built from protobuf scalars (uint32 sign count).
		"grpc": map[string]interface{}{
			"credentials": []map[string]interface{}{{
				"id": "AbCd01", "aaguid": "aabbccdd", "attestation_type": "packed",
				"transports": []string{"usb", "nfc"},
				"sign_count": uint32(7), "backup_eligible": true,
				"backup_state": true, "clone_warning": true,
			}},
			"total": 1,
		},
		// Embedded: built from the Go domain summary.
		"embedded": map[string]interface{}{
			"credentials": []map[string]interface{}{{
				"id": "AbCd01", "aaguid": "aabbccdd", "attestation_type": "packed",
				"transports": []string{"usb", "nfc"},
				"sign_count": uint32(7), "backup_eligible": true,
				"backup_state": true, "clone_warning": true,
			}},
			"total": 1,
		},
	}

	want := cli.WebAuthnCredential{
		ID: "AbCd01", AAGUID: "aabbccdd", AttestationType: "packed",
		Transports: []string{"usb", "nfc"}, SignCount: 7,
		BackupEligible: true, BackupState: true, CloneWarning: true,
	}

	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			rec := &webauthnRecorder{}
			c := NewClient(rec.transportFor(shape))
			got, err := c.ListWebAuthnCredentials(context.Background(), "t1", "u1")
			if err != nil {
				t.Fatalf("ListWebAuthnCredentials: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d credentials, want 1", len(got))
			}
			c0 := got[0]
			if c0.ID != want.ID || c0.AAGUID != want.AAGUID || c0.AttestationType != want.AttestationType {
				t.Errorf("identity fields = %+v, want %+v", c0, want)
			}
			if c0.SignCount != want.SignCount {
				t.Errorf("SignCount = %d, want %d", c0.SignCount, want.SignCount)
			}
			if !c0.BackupEligible || !c0.BackupState || !c0.CloneWarning {
				t.Errorf("flags = %+v, want all true", c0)
			}
			if len(c0.Transports) != 2 || c0.Transports[0] != "usb" || c0.Transports[1] != "nfc" {
				t.Errorf("Transports = %v, want [usb nfc]", c0.Transports)
			}
		})
	}
}

// TestWebAuthnClientValidation refuses an incomplete scope before dispatching,
// so no transport is handed an empty tenant or user.
func TestWebAuthnClientValidation(t *testing.T) {
	rec := &webauthnRecorder{}
	c := NewClient(rec.transportFor(nil))
	ctx := context.Background()

	if _, err := c.ListWebAuthnCredentials(ctx, "", "u1"); err == nil {
		t.Error("expected an error for an empty tenant")
	}
	if _, err := c.ListWebAuthnCredentials(ctx, "t1", ""); err == nil {
		t.Error("expected an error for an empty user")
	}
	if err := c.DeleteWebAuthnCredential(ctx, "t1", "u1", ""); err == nil {
		t.Error("expected an error for an empty credential id")
	}
	if _, err := c.DeleteWebAuthnCredentials(ctx, "t1", ""); err == nil {
		t.Error("expected an error for an empty user")
	}
}

// TestWebAuthnClientSurfacesDecodeFailure keeps a malformed delete-all response
// an error. Reporting 0 would be indistinguishable from "this account had no
// credentials" — the one answer an operator clearing a stuck account must not
// be told wrongly.
func TestWebAuthnClientSurfacesDecodeFailure(t *testing.T) {
	rec := &webauthnRecorder{}
	c := NewClient(rec.transportFor(make(chan int)))

	if _, err := c.DeleteWebAuthnCredentials(context.Background(), "t1", "u1"); err == nil {
		t.Error("expected an error when the deleted count cannot be decoded")
	}
}
