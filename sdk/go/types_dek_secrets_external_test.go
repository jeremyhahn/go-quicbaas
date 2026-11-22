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

package quicbaas_test

import (
	"context"
	"errors"
	"testing"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

var errNoSuchVerb = errors.New("payload transport: unexpected verb")

// payloadTransport answers the secrets GetData verb with a stored payload,
// the way a consumer's own transport does, using root-package types only.
type payloadTransport struct {
	payload []byte
}

func (p *payloadTransport) Request(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
	if svc != "quicbaas.Secrets" || method != "GetData" {
		return nil, errNoSuchVerb
	}
	return &quicbaas.GetSecretDataResponse{SecretID: "app/db", Version: 1, Data: p.payload}, nil
}

func (p *payloadTransport) RequestStream(context.Context, string, string, interface{}) (quicbaas.Stream, error) {
	return nil, errNoSuchVerb
}

func (p *payloadTransport) Conn() interface{} { return nil }

func (p *payloadTransport) Close() error { return nil }

func (p *payloadTransport) Healthy() bool { return true }

var _ quicbaas.Transport = (*payloadTransport)(nil)

// TestExternalGetWithBytes_RootPackageOnly borrows a secret field over a
// custom transport built from root-package types alone.
func TestExternalGetWithBytes_RootPackageOnly(t *testing.T) {
	payload := []byte(`{"password":"s3cret","user":"app"}`)
	c := quicbaas.NewSecretsClient(&payloadTransport{payload: payload})
	var got string
	err := c.GetWithBytes(context.Background(), "app/db", "password", func(b []byte) error {
		got = string(b)
		return nil
	})
	if err != nil {
		t.Fatalf("GetWithBytes: %v", err)
	}
	if got != "s3cret" {
		t.Fatalf("borrowed %q, want %q", got, "s3cret")
	}
	for i, b := range payload {
		if b != 0 {
			t.Fatalf("payload byte %d = %#x after GetWithBytes, want it wiped", i, b)
		}
	}
}
