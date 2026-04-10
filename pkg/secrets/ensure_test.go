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

package secrets

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func newTestManagerForEnsure(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(&ManagerOptions{Backend: NewMemoryBackend()})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

// TestEnsureSecret_GenerateThenRead verifies the first-call-generates,
// second-call-reads-same-bytes contract. This is the ONLY behaviour
// that matters for the persistent-autogen use case; a regression here
// breaks the entire TPM provisioning flow in go-trusted-platform.
func TestEnsureSecret_GenerateThenRead(t *testing.T) {
	m := newTestManagerForEnsure(t)
	ctx := context.Background()
	path := "tenants/test/nodes/node-1/secrets/platform/so-pin"
	spec := &SecretSpec{Length: 8, Encoding: EncodingNumeric}

	first, generated1, err := m.EnsureSecret(ctx, path, spec)
	if err != nil {
		t.Fatalf("first EnsureSecret: %v", err)
	}
	if !generated1 {
		t.Fatalf("first call should report generated=true, got false")
	}
	if len(first) != 8 {
		t.Fatalf("numeric length: want 8, got %d", len(first))
	}
	for _, b := range first {
		if b < '0' || b > '9' {
			t.Fatalf("numeric encoding must be digits only, got %q", first)
		}
	}

	second, generated2, err := m.EnsureSecret(ctx, path, spec)
	if err != nil {
		t.Fatalf("second EnsureSecret: %v", err)
	}
	if generated2 {
		t.Fatalf("second call should report generated=false, got true")
	}
	if string(first) != string(second) {
		t.Fatalf("EnsureSecret returned DIFFERENT bytes on repeat call — persistence is broken.\nfirst=%q\nsecond=%q", first, second)
	}
}

// TestEnsureSecret_AllEncodings verifies every spec encoding is
// accepted and produces the right shape.
func TestEnsureSecret_AllEncodings(t *testing.T) {
	cases := []struct {
		name     string
		spec     *SecretSpec
		checkLen func(t *testing.T, b []byte)
	}{
		{
			name: "base64",
			spec: &SecretSpec{Length: 32, Encoding: EncodingBase64},
			checkLen: func(t *testing.T, b []byte) {
				// 32 bytes → raw URL base64 (no padding) is 43 chars.
				if len(b) != 43 {
					t.Errorf("base64 len: want 43, got %d", len(b))
				}
			},
		},
		{
			name: "hex",
			spec: &SecretSpec{Length: 16, Encoding: EncodingHex},
			checkLen: func(t *testing.T, b []byte) {
				if len(b) != 32 {
					t.Errorf("hex len: want 32, got %d", len(b))
				}
			},
		},
		{
			name: "numeric",
			spec: &SecretSpec{Length: 6, Encoding: EncodingNumeric},
			checkLen: func(t *testing.T, b []byte) {
				if len(b) != 6 {
					t.Errorf("numeric len: want 6, got %d", len(b))
				}
			},
		},
		{
			name: "raw",
			spec: &SecretSpec{Length: 24, Encoding: EncodingRaw},
			checkLen: func(t *testing.T, b []byte) {
				if len(b) != 24 {
					t.Errorf("raw len: want 24, got %d", len(b))
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManagerForEnsure(t)
			got, gen, err := m.EnsureSecret(context.Background(),
				"p/"+tc.name, tc.spec)
			if err != nil {
				t.Fatalf("EnsureSecret: %v", err)
			}
			if !gen {
				t.Errorf("first call: want generated=true, got false")
			}
			tc.checkLen(t, got)
			// Re-read returns the same bytes and generated=false.
			got2, gen2, err := m.EnsureSecret(context.Background(),
				"p/"+tc.name, tc.spec)
			if err != nil {
				t.Fatalf("re-read EnsureSecret: %v", err)
			}
			if gen2 {
				t.Errorf("re-read: want generated=false, got true")
			}
			if string(got) != string(got2) {
				t.Fatalf("re-read mismatch: %q vs %q", got, got2)
			}
		})
	}
}

// TestEnsureSecret_SpecMismatch verifies that a later caller asking for
// the same path with a DIFFERENT spec gets a hard error, no rotation,
// and no silent return of the stale bytes.
func TestEnsureSecret_SpecMismatch(t *testing.T) {
	m := newTestManagerForEnsure(t)
	ctx := context.Background()
	path := "p/spec-mismatch"

	if _, _, err := m.EnsureSecret(ctx, path,
		&SecretSpec{Length: 16, Encoding: EncodingBase64}); err != nil {
		t.Fatalf("initial EnsureSecret: %v", err)
	}

	// Different length.
	_, _, err := m.EnsureSecret(ctx, path,
		&SecretSpec{Length: 32, Encoding: EncodingBase64})
	if !errors.Is(err, ErrSecretSpecMismatch) {
		t.Fatalf("length mismatch: want ErrSecretSpecMismatch, got %v", err)
	}

	// Different encoding.
	_, _, err = m.EnsureSecret(ctx, path,
		&SecretSpec{Length: 16, Encoding: EncodingHex})
	if !errors.Is(err, ErrSecretSpecMismatch) {
		t.Fatalf("encoding mismatch: want ErrSecretSpecMismatch, got %v", err)
	}
}

// TestEnsureSecret_InvalidSpec rejects malformed specs up front.
func TestEnsureSecret_InvalidSpec(t *testing.T) {
	m := newTestManagerForEnsure(t)
	ctx := context.Background()

	cases := []*SecretSpec{
		nil,
		{Length: 0, Encoding: EncodingBase64},
		{Length: -1, Encoding: EncodingBase64},
		{Length: 16, Encoding: ""},
		{Length: 16, Encoding: "unknown"},
	}
	for i, spec := range cases {
		_, _, err := m.EnsureSecret(ctx, "p/invalid", spec)
		if err == nil {
			t.Errorf("case %d: expected error for %+v, got nil", i, spec)
		}
	}
}

// TestEnsureSecret_AdoptsOperatorSecretFails verifies that a path
// previously populated by operator Store() cannot be picked up by
// EnsureSecret — mixing the two is a caller bug that we surface
// instead of silently overwriting or returning stale data.
func TestEnsureSecret_AdoptsOperatorSecretFails(t *testing.T) {
	m := newTestManagerForEnsure(t)
	ctx := context.Background()
	path := "p/operator-owned"

	if err := m.Store(ctx, path,
		map[string][]byte{"value": []byte("operator-written")},
		map[string]string{"owner": "devops"}); err != nil {
		t.Fatalf("Store: %v", err)
	}

	_, _, err := m.EnsureSecret(ctx, path,
		&SecretSpec{Length: 32, Encoding: EncodingBase64})
	if err == nil {
		t.Fatal("expected error when EnsureSecret sees an operator-owned path, got nil")
	}
	if !strings.Contains(err.Error(), "non-auto-generated") {
		t.Errorf("error message should explain the adoption refusal: %v", err)
	}
}

// TestEnsureSecret_ConcurrentSamePath verifies that N goroutines
// racing on the same path with no distributed locker configured all
// receive the same stable bytes, and exactly one of them reports
// generated=true. The process-local fallback mutex in Manager must
// serialize the critical section atomically with the generated-flag
// computation.
func TestEnsureSecret_ConcurrentSamePath(t *testing.T) {
	m := newTestManagerForEnsure(t)
	ctx := context.Background()
	path := "p/concurrent"
	spec := &SecretSpec{Length: 32, Encoding: EncodingBase64}

	const goroutines = 16
	type result struct {
		data      []byte
		generated bool
	}
	results := make([]result, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			data, gen, err := m.EnsureSecret(ctx, path, spec)
			results[i] = result{data: data, generated: gen}
			errs[i] = err
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	first := string(results[0].data)
	if first == "" {
		t.Fatal("first result empty")
	}
	generatedCount := 0
	for i := 0; i < goroutines; i++ {
		if string(results[i].data) != first {
			t.Fatalf("goroutine %d got different bytes than goroutine 0: %q vs %q",
				i, results[i].data, first)
		}
		if results[i].generated {
			generatedCount++
		}
	}
	if generatedCount != 1 {
		t.Fatalf("exactly one goroutine should have generated the secret; got %d", generatedCount)
	}
}
