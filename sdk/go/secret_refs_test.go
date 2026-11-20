// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

var errSecretRefNotFound = errors.New("secret not found")

// secretRefStubClient implements only GetWithBytes and Ensure; any other
// method panics through the nil embedded interface.
type secretRefStubClient struct {
	SecretsClient
	stored    map[string][]byte
	ensureReq *secrets.EnsureSecretRequest
	ensured   []byte
}

func (s *secretRefStubClient) GetWithBytes(_ context.Context, path, field string, fn func([]byte) error, _ ...secrets.GetOption) error {
	if field != "" {
		return errors.New("unexpected field " + field)
	}
	b, ok := s.stored[path]
	if !ok {
		return errSecretRefNotFound
	}
	return fn(b)
}

func (s *secretRefStubClient) Ensure(_ context.Context, req *secrets.EnsureSecretRequest) (*secrets.EnsureSecretResponse, error) {
	s.ensureReq = req
	return &secrets.EnsureSecretResponse{Path: req.Path, Data: s.ensured}, nil
}

func TestSecretResolverRegistry_KMSReadsStoredBytes(t *testing.T) {
	stub := &secretRefStubClient{stored: map[string][]byte{"app/db/password": []byte("s3cret")}}
	reg := NewSecretResolverRegistry(stub)
	v, err := reg.Resolve(context.Background(), "kms://app/db/password")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := v.String(); got != "s3cret" {
		t.Fatalf("value = %q, want %q", got, "s3cret")
	}
}

func TestSecretResolverRegistry_KMSEnsure(t *testing.T) {
	stub := &secretRefStubClient{ensured: []byte("ZW5zdXJlZA==")}
	reg := NewSecretResolverRegistry(stub)
	v, err := reg.Resolve(context.Background(), "kms://app/token?ensure=true&len=32&enc=base64")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if stub.ensureReq == nil {
		t.Fatal("Ensure was not called")
	}
	if stub.ensureReq.Path != "app/token" || stub.ensureReq.Length != 32 || stub.ensureReq.Encoding != "base64" {
		t.Fatalf("Ensure request = %+v", *stub.ensureReq)
	}
	if got := v.String(); got != "ZW5zdXJlZA==" {
		t.Fatalf("value = %q, want %q", got, "ZW5zdXJlZA==")
	}
}

func TestSecretResolverRegistry_MissingSecretErrors(t *testing.T) {
	stub := &secretRefStubClient{stored: map[string][]byte{}}
	reg := NewSecretResolverRegistry(stub)
	_, err := reg.Resolve(context.Background(), "kms://app/missing")
	if err == nil {
		t.Fatal("Resolve of a missing secret returned no error")
	}
	// LocalKMSResolver reports the failure without wrapping the client's
	// error, so the test checks that the error names the reference.
	if !strings.Contains(err.Error(), "kms://app/missing") {
		t.Fatalf("error %v does not name the reference", err)
	}
}

func TestSecretResolverRegistry_LiteralVerbatim(t *testing.T) {
	reg := NewSecretResolverRegistry(&secretRefStubClient{})
	v, err := reg.Resolve(context.Background(), "plain-value")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := v.String(); got != "plain-value" {
		t.Fatalf("value = %q, want %q", got, "plain-value")
	}
}
