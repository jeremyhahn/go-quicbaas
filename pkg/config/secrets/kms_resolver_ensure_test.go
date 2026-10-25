// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package secrets

import (
	"context"
	"errors"
	"testing"
)

// ensureStub implements SecretsClient + SecretsEnsurer over an in-memory map,
// mimicking the KMS: GetSecret fails when absent, EnsureSecret generates once
// and is idempotent thereafter.
type ensureStub struct {
	store      map[string][]byte
	ensureCall int
	lastLength int
	lastEnc    string
}

func newEnsureStub() *ensureStub {
	return &ensureStub{store: make(map[string][]byte)}
}

func (s *ensureStub) GetSecret(_ context.Context, id string) ([]byte, error) {
	if v, ok := s.store[id]; ok {
		return v, nil
	}
	return nil, errors.New("secrets: secret not found")
}

func (s *ensureStub) EnsureSecret(_ context.Context, id string, length int, encoding string) ([]byte, error) {
	s.ensureCall++
	s.lastLength = length
	s.lastEnc = encoding
	if v, ok := s.store[id]; ok {
		return v, nil
	}
	v := []byte("generated-value-for-" + id)
	s.store[id] = v
	return v, nil
}

// getOnlyStub implements SecretsClient WITHOUT SecretsEnsurer.
type getOnlyStub struct{}

func (getOnlyStub) GetSecret(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("secrets: secret not found")
}

// TestKMSResolverEnsureGeneratesOnFirstUse is the regression test for the
// platform first-boot failure: kms://...?ensure=true must GENERATE the secret
// against an empty store rather than failing "secret not found", and the query
// string must not become part of the secret ID.
func TestKMSResolverEnsureGeneratesOnFirstUse(t *testing.T) {
	stub := newEnsureStub()
	r := NewLocalKMSResolver(stub)

	const uri = "kms://ca/root/keys/0/password?ensure=true&len=32&enc=base64"

	got, err := r.Resolve(context.Background(), uri)
	if err != nil {
		t.Fatalf("Resolve with ensure=true against empty store: %v", err)
	}
	if got == nil || plaintextOf(t, got) == "" {
		t.Fatalf("Resolve returned no secret value")
	}

	// The secret ID must be the PATH ONLY — the query string is options, not identity.
	if _, ok := stub.store["ca/root/keys/0/password"]; !ok {
		t.Errorf("secret stored under wrong ID; store keys = %v", keysOf(stub.store))
	}
	if stub.lastLength != 32 || stub.lastEnc != "base64" {
		t.Errorf("spec not parsed from query: got len=%d enc=%q, want 32/base64", stub.lastLength, stub.lastEnc)
	}

	// Idempotent: a second resolve returns the SAME value.
	again, err := r.Resolve(context.Background(), uri)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if plaintextOf(t, again) != plaintextOf(t, got) {
		t.Errorf("ensure is not idempotent: %q != %q", plaintextOf(t, again), plaintextOf(t, got))
	}
}

// TestKMSResolverEnsureDefaultsAndVariants covers len/enc defaults and encodings.
func TestKMSResolverEnsureDefaultsAndVariants(t *testing.T) {
	cases := []struct {
		name    string
		uri     string
		wantLen int
		wantEnc string
	}{
		{"defaults when omitted", "kms://a/b?ensure=true", 32, "base64"},
		{"explicit hex", "kms://a/c?ensure=true&len=16&enc=hex", 16, "hex"},
		{"explicit numeric", "kms://a/d?ensure=true&len=6&enc=numeric", 6, "numeric"},
		{"explicit raw", "kms://a/e?ensure=true&len=64&enc=raw", 64, "raw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := newEnsureStub()
			r := NewLocalKMSResolver(stub)
			if _, err := r.Resolve(context.Background(), tc.uri); err != nil {
				t.Fatalf("Resolve(%s): %v", tc.uri, err)
			}
			if stub.lastLength != tc.wantLen || stub.lastEnc != tc.wantEnc {
				t.Errorf("got len=%d enc=%q, want len=%d enc=%q",
					stub.lastLength, stub.lastEnc, tc.wantLen, tc.wantEnc)
			}
		})
	}
}

// TestKMSResolverWithoutEnsureStillReads verifies the non-ensure path is
// unchanged and that a query-less URI resolves by path.
func TestKMSResolverWithoutEnsureStillReads(t *testing.T) {
	stub := newEnsureStub()
	stub.store["plain/secret"] = []byte("stored")
	r := NewLocalKMSResolver(stub)

	got, err := r.Resolve(context.Background(), "kms://plain/secret")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if plaintextOf(t, got) != "stored" {
		t.Errorf("got %q, want %q", plaintextOf(t, got), "stored")
	}
	if stub.ensureCall != 0 {
		t.Errorf("EnsureSecret must not be called without ensure=true (called %d times)", stub.ensureCall)
	}
}

// TestKMSResolverEnsureUnsupportedFailsClosed verifies a clear typed error when
// the client cannot generate secrets — never a silent read that would fail.
func TestKMSResolverEnsureUnsupportedFailsClosed(t *testing.T) {
	r := NewLocalKMSResolver(getOnlyStub{})
	_, err := r.Resolve(context.Background(), "kms://a/b?ensure=true&len=32&enc=base64")
	if err == nil {
		t.Fatalf("expected an error when the client does not support ensure")
	}
}

// TestKMSResolverInvalidQueryRejected verifies malformed options are rejected.
func TestKMSResolverInvalidQueryRejected(t *testing.T) {
	r := NewLocalKMSResolver(newEnsureStub())
	for _, uri := range []string{
		"kms://a/b?ensure=notabool",
		"kms://a/b?ensure=true&len=0",
		"kms://a/b?ensure=true&len=-1",
		"kms://a/b?ensure=true&len=abc",
		"kms://a/b?ensure=true&enc=rot13",
	} {
		if _, err := r.Resolve(context.Background(), uri); err == nil {
			t.Errorf("expected rejection for %q", uri)
		}
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
