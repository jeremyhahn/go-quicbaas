//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestHandleCeremonyInitialize_SecretReachesServiceSealed proves the decoded
// barrier passphrase reaches the service as a live *secure.Secret holding
// exactly the bytes the caller sent.
func TestHandleCeremonyInitialize_SecretReachesServiceSealed(t *testing.T) {
	var got []byte
	svc := &fakeCeremonyService{
		initFn: func(ctx context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			if req.Secret == nil {
				t.Fatal("the passphrase did not reach the service")
			}
			if err := req.Secret.WithBytes(ctx, func(b []byte) error {
				got = append([]byte(nil), b...)
				return nil
			}); err != nil {
				t.Fatalf("open secret: %v", err)
			}
			return &service.CeremonyInitInfo{}, nil
		},
	}
	body := `{"admin_generate":true,"secret":"` + base64.StdEncoding.EncodeToString(wirePassphrase) + `"}`
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if !bytes.Equal(got, wirePassphrase) {
		t.Fatalf("secret reached the service as %q, want %q", got, wirePassphrase)
	}
}

// TestHandleCeremonyInitialize_MalformedSecretRefused proves a secret that is
// not well-formed base64 is refused with BarrierCredentialEncodingError and
// never reaches the service.
func TestHandleCeremonyInitialize_MalformedSecretRefused(t *testing.T) {
	called := false
	svc := &fakeCeremonyService{
		initFn: func(context.Context, *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
			called = true
			return &service.CeremonyInitInfo{}, nil
		},
	}
	w := doCeremonyReq(t, ceremonyServer(t, svc), http.MethodPost, "/api/v1/init/initialize",
		`{"admin_generate":true,"secret":"s3cr3t"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	assertCredentialEncodingRefusal(t, w, "s3cr3t")
	if called {
		t.Fatal("a malformed secret reached the ceremony service")
	}
}
