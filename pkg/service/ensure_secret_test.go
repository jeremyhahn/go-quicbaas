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

package service

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// newServiceForEnsureTest builds a real secretsService backed by a real
// *secrets.Manager with an in-memory backend. This is the exact wiring
// that go-trusted-platform hits through the embedded transport:
//
//	SDK client
//	  → Transport.Request("quicbaas.Secrets", "Ensure", req)
//	    → handleSecretsEnsure
//	      → secretsService.EnsureSecret     (this file's SUT)
//	        → *secrets.Manager.EnsureSecret
//	          → Backend.Get / Backend.Store
//
// These tests exercise the last three stages. The transport layer and
// SDK client are tested elsewhere (handlers_secrets_test.go).
func newServiceForEnsureTest(t *testing.T) *secretsService {
	t.Helper()
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{
		Backend: secrets.NewMemoryBackend(),
	})
	if err != nil {
		t.Fatalf("secrets.NewManager: %v", err)
	}
	return newSecretsService(mgr, nil)
}

// TestServiceEnsureSecret_GenerateThenRead verifies the service-layer
// generate-on-first-access contract: first call produces a fresh random
// value, second call returns the same bytes with Generated=false.
func TestServiceEnsureSecret_GenerateThenRead(t *testing.T) {
	svc := newServiceForEnsureTest(t)
	ctx := systemTenantCtx()

	req := &EnsureSecretRequest{
		SecretID: "tenants/test/nodes/n1/secrets/platform/so-pin",
		Length:   8,
		Encoding: secrets.EncodingNumeric,
	}

	resp1, err := svc.EnsureSecret(ctx, req)
	if err != nil {
		t.Fatalf("first EnsureSecret: %v", err)
	}
	if !resp1.Generated {
		t.Fatalf("first call should report Generated=true, got false")
	}
	if len(resp1.Data) != 8 {
		t.Fatalf("numeric length: want 8, got %d", len(resp1.Data))
	}
	for _, b := range resp1.Data {
		if b < '0' || b > '9' {
			t.Fatalf("numeric encoding must be digits only, got %q", resp1.Data)
		}
	}

	resp2, err := svc.EnsureSecret(ctx, req)
	if err != nil {
		t.Fatalf("second EnsureSecret: %v", err)
	}
	if resp2.Generated {
		t.Fatalf("second call should report Generated=false, got true")
	}
	if string(resp1.Data) != string(resp2.Data) {
		t.Fatalf("EnsureSecret returned DIFFERENT bytes across calls — persistence is broken.\nfirst=%q\nsecond=%q",
			resp1.Data, resp2.Data)
	}
}

// TestServiceEnsureSecret_SpecMismatch verifies that a follow-up call
// with a different length or encoding is a hard error, not a silent
// rotation.
func TestServiceEnsureSecret_SpecMismatch(t *testing.T) {
	svc := newServiceForEnsureTest(t)
	ctx := systemTenantCtx()
	path := "p/spec-mismatch"

	_, err := svc.EnsureSecret(ctx, &EnsureSecretRequest{
		SecretID: path, Length: 16, Encoding: secrets.EncodingBase64,
	})
	if err != nil {
		t.Fatalf("initial EnsureSecret: %v", err)
	}

	// Different length on same path.
	_, err = svc.EnsureSecret(ctx, &EnsureSecretRequest{
		SecretID: path, Length: 32, Encoding: secrets.EncodingBase64,
	})
	if !errors.Is(err, secrets.ErrSecretSpecMismatch) {
		t.Fatalf("length mismatch: want ErrSecretSpecMismatch, got %v", err)
	}

	// Different encoding.
	_, err = svc.EnsureSecret(ctx, &EnsureSecretRequest{
		SecretID: path, Length: 16, Encoding: secrets.EncodingHex,
	})
	if !errors.Is(err, secrets.ErrSecretSpecMismatch) {
		t.Fatalf("encoding mismatch: want ErrSecretSpecMismatch, got %v", err)
	}
}

// TestServiceEnsureSecret_InvalidRequest rejects malformed requests at
// the service layer before reaching the Manager.
func TestServiceEnsureSecret_InvalidRequest(t *testing.T) {
	svc := newServiceForEnsureTest(t)
	ctx := systemTenantCtx()

	cases := []struct {
		name string
		req  *EnsureSecretRequest
	}{
		{"nil", nil},
		{"empty secret id", &EnsureSecretRequest{Length: 16, Encoding: secrets.EncodingBase64}},
		{"zero length", &EnsureSecretRequest{SecretID: "p", Length: 0, Encoding: secrets.EncodingBase64}},
		{"unknown encoding", &EnsureSecretRequest{SecretID: "p", Length: 16, Encoding: "weird"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.EnsureSecret(ctx, tc.req); err == nil {
				t.Fatalf("expected error for %q, got nil", tc.name)
			}
		})
	}
}

// TestServiceEnsureSecret_ConcurrentSamePath confirms that N
// goroutines racing on the same path through the service layer all
// receive identical bytes. This exercises the process-local mutex
// fallback in the Manager because no distributed Locker is wired in
// this test.
func TestServiceEnsureSecret_ConcurrentSamePath(t *testing.T) {
	svc := newServiceForEnsureTest(t)
	ctx := systemTenantCtx()
	req := &EnsureSecretRequest{
		SecretID: "p/concurrent",
		Length:   32,
		Encoding: secrets.EncodingBase64,
	}

	const goroutines = 16
	results := make([]*EnsureSecretResponse, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = svc.EnsureSecret(ctx, req)
		}()
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	first := string(results[0].Data)
	if first == "" {
		t.Fatal("first result empty")
	}
	for i := 1; i < goroutines; i++ {
		if string(results[i].Data) != first {
			t.Fatalf("goroutine %d got different bytes than goroutine 0:\n%q\nvs\n%q",
				i, results[i].Data, first)
		}
	}

	// Exactly one goroutine should report Generated=true.
	generatedCount := 0
	for _, r := range results {
		if r.Generated {
			generatedCount++
		}
	}
	if generatedCount != 1 {
		t.Fatalf("exactly one goroutine should have generated the secret; got %d", generatedCount)
	}
}

// TestServiceEnsureSecret_OperatorSecretRefusal verifies that
// EnsureSecret refuses to adopt a path previously populated by a
// plain Store() call. Mixing the two is a caller bug we surface
// instead of swallowing.
func TestServiceEnsureSecret_OperatorSecretRefusal(t *testing.T) {
	svc := newServiceForEnsureTest(t)
	ctx := systemTenantCtx()
	path := "p/operator-owned"

	// Manually write a secret via the underlying Manager — bypasses
	// the auto-generated metadata marker.
	if err := svc.secretsManager.Store(ctx, path,
		map[string][]byte{"value": []byte("operator-written")},
		map[string]string{"owner": "devops"}); err != nil {
		t.Fatalf("direct Store: %v", err)
	}

	_, err := svc.EnsureSecret(ctx, &EnsureSecretRequest{
		SecretID: path, Length: 32, Encoding: secrets.EncodingBase64,
	})
	if err == nil {
		t.Fatal("expected error when EnsureSecret sees an operator-owned path")
	}
	if !strings.Contains(err.Error(), "non-auto-generated") {
		t.Errorf("error should explain adoption refusal: %v", err)
	}
}
