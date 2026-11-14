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

package crypto

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// A caller that encrypted under version N of a key can ask for that same
// version on decrypt; EncryptResponse reports it, so DecryptRequest must
// carry it to the server (#1572).
func TestClient_Decrypt_KeyVersionReachesServer(t *testing.T) {
	for _, version := range []int{0, 3} {
		var sent *service.DecryptRequest
		tr := &mockTransport{
			requestFn: func(_ context.Context, _, _ string, req interface{}) (interface{}, error) {
				sent, _ = req.(*service.DecryptRequest)
				return &DecryptResponse{Plaintext: []byte("p")}, nil
			},
		}

		_, err := NewClient(tr).Decrypt(context.Background(), &DecryptRequest{
			KeyID:      "key-1",
			Ciphertext: []byte("c"),
			KeyVersion: version,
		})
		if err != nil {
			t.Fatalf("version %d: Decrypt() error = %v", version, err)
		}
		if sent == nil {
			t.Fatalf("version %d: transport got no *service.DecryptRequest", version)
		}
		if sent.KeyVersion != uint64(version) {
			t.Errorf("version %d: server request KeyVersion = %d", version, sent.KeyVersion)
		}
	}
}

func TestClient_Decrypt_NegativeKeyVersionRefused(t *testing.T) {
	called := false
	tr := &mockTransport{
		requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			called = true
			return &DecryptResponse{}, nil
		},
	}

	_, err := NewClient(tr).Decrypt(context.Background(), &DecryptRequest{
		KeyID:      "key-1",
		Ciphertext: []byte("c"),
		KeyVersion: -1,
	})
	if errors.Code(err) != errors.CodeInvalidInput {
		t.Fatalf("expected invalid input, got %v", err)
	}
	if called {
		t.Error("a negative key version must be refused before the transport is called")
	}
}
