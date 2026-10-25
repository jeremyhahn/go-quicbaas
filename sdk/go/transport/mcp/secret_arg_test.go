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

package mcp

import (
	"context"
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// textSecretCases are the verbs that carry a credential as a JSON string
// argument, so its bytes must survive as text.
var textSecretCases = []secretVerbCase{
	secretVerbCases[0], // BarrierInitialize
	secretVerbCases[1], // BarrierUnseal
	secretVerbCases[3], // BarrierEnableEncryption
	secretVerbCases[4], // BarrierEnableEncryption tenant
	{"CeremonyInitialize", "qrdb_ceremony_initialize", "secret", func(ctx context.Context, tr *Transport, secret []byte) error {
		_, err := tr.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{Threshold: 1, Secret: secret})
		return err
	}},
	{"BarrierSeal", "qrdb_barrier_seal", "so_pin", func(ctx context.Context, tr *Transport, secret []byte) error {
		ctx, release, err := qrdbsdk.WithSOPIN(ctx, secret)
		if err != nil {
			return err
		}
		defer release()
		return tr.BarrierSeal(ctx)
	}},
}

// A credential that is not valid UTF-8 would be altered by the JSON string
// it travels in, so it is refused with the typed error before any request.
func TestTextSecretVerbsRefuseBytesThatAreNotText(t *testing.T) {
	for _, tc := range textSecretCases {
		t.Run(tc.name, func(t *testing.T) {
			tr, called := newMultiToolMCPServerTLS(t, []string{tc.tool}, func(*testing.T, string, map[string]interface{}) map[string]interface{} {
				return map[string]interface{}{"ok": true}
			})

			err := tc.call(context.Background(), tr, []byte{0xff, 0x80})
			var notText *SecretNotTextError
			if !stderrors.As(err, &notText) {
				t.Fatalf("expected SecretNotTextError, got %v", err)
			}
			if notText.Field != tc.arg {
				t.Fatalf("SecretNotTextError.Field = %q, want %q", notText.Field, tc.arg)
			}
			if len(*called) != 0 {
				t.Fatalf("an unrepresentable secret must not be sent, server saw %v", *called)
			}
		})
	}
}

// A leading byte-order mark is valid text and reaches the server unchanged.
func TestTextSecretVerbsKeepALeadingBOM(t *testing.T) {
	secret := string([]byte{0xef, 0xbb, 0xbf}) + "passphrase"
	for _, tc := range textSecretCases {
		t.Run(tc.name, func(t *testing.T) {
			var received interface{}
			tr, _ := newMultiToolMCPServerTLS(t, []string{tc.tool}, func(_ *testing.T, _ string, args map[string]interface{}) map[string]interface{} {
				received = args[tc.arg]
				return map[string]interface{}{"ok": true}
			})

			if err := tc.call(context.Background(), tr, []byte(secret)); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if received != secret {
				t.Fatalf("%s argument = %q, want %q", tc.arg, received, secret)
			}
		})
	}
}
