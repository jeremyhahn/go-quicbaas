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
	"fmt"
	"strings"
	"sync"
	"testing"
)

// clientAuthTestSecret derives ONE client secret and its digest for the whole
// file. Each derivation is a deliberate 600,000-iteration PBKDF2 pass -- that
// cost is the anti-enumeration property under test elsewhere -- so minting a
// fresh pair per subtest would buy nothing and spend seconds of every run.
var clientAuthTestSecret = sync.OnceValues(func() (clientSecretPair, error) {
	plaintext, hashed, err := generateClientSecret()
	return clientSecretPair{Plaintext: plaintext, Hashed: hashed}, err
})

// clientSecretPair is one issued client secret and the digest stored for it.
type clientSecretPair struct {
	Plaintext string
	Hashed    string
}

// =============================================================================
// go-quicbaas #644, third defect — a refused client authentication must say,
// server side, WHICH check refused it.
//
// /oauth2/token answered 401 invalid_client with no server log line at all, and
// handleClientCredentialsGrant collapsed `err != nil || !valid`, so a lookup
// miss and a wrong secret were indistinguishable even in the code. Diagnosing
// the burn-in outage needed a hand-built reproduction against a live node.
//
// The response stays byte-identical and the derivation cost stays equalized —
// only the operator's log gains the reason.
// =============================================================================

// captureLogger records the operator-facing lines a storage emits.
type captureLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *captureLogger) Warnf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

// find returns the first captured line containing every fragment.
func (l *captureLogger) find(fragments ...string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		matched := true
		for _, f := range fragments {
			if !strings.Contains(line, f) {
				matched = false
				break
			}
		}
		if matched {
			return line
		}
	}
	return ""
}

// all returns every captured line joined, for negative assertions.
func (l *captureLogger) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

// newClientAuthLogFixture builds a storage over a routed KV holding one agent
// application in a barrier tenant, and returns the storage, the log capture,
// and the plaintext secret the application was issued.
func newClientAuthLogFixture(t *testing.T) (*BackendStorage, *captureLogger, string) {
	t.Helper()
	kv := newRoutingTestKV()
	kv.barrierTenant("automatethethings")
	log := &captureLogger{}
	storage := NewBackendStorage(kv, WithStorageLogger(log))

	issued, err := clientAuthTestSecret()
	if err != nil {
		t.Fatalf("generateClientSecret: %v", err)
	}
	app := agentApp("automatethethings", "app_automatethethings_QQnqeujdEo")
	app.ClientSecret = issued.Hashed
	if err := storage.CreateApp(context.Background(), app); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
	return storage, log, issued.Plaintext
}

// TestValidateClientSecret_LogsWhichCheckRefused is the regression: each
// distinct refusal names itself in the operator log.
func TestValidateClientSecret_LogsWhichCheckRefused(t *testing.T) {
	for _, tc := range []struct {
		name       string
		clientID   string
		secret     func(issued string) string
		wantReason string
	}{
		{
			name:       "unknown client",
			clientID:   "app_automatethethings_NOSUCHCLIENT",
			secret:     func(issued string) string { return issued },
			wantReason: clientAuthReasonUnknownClient,
		},
		{
			name:       "wrong secret",
			clientID:   "app_automatethethings_QQnqeujdEo",
			secret:     func(string) string { return "sk_wrong-secret" },
			wantReason: clientAuthReasonSecretMismatch,
		},
		{
			name:       "empty secret",
			clientID:   "app_automatethethings_QQnqeujdEo",
			secret:     func(string) string { return "" },
			wantReason: clientAuthReasonEmptySecret,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage, log, issued := newClientAuthLogFixture(t)
			ctx := ContextWithTenant(context.Background(), "automatethethings")

			ok, _ := storage.ValidateClientSecret(ctx, tc.clientID, tc.secret(issued))
			if ok {
				t.Fatal("ValidateClientSecret accepted a credential it must refuse")
			}
			if line := log.find("client authentication failed", tc.clientID, tc.wantReason); line == "" {
				t.Fatalf("no log line naming reason %q for client %q; captured:\n%s", tc.wantReason, tc.clientID, log.all())
			}
		})
	}
}

// TestValidateClientSecret_LogsTheTenantMismatch covers the third distinct
// refusal shape: the record resolved, but it belongs to a different tenant than
// the request was scoped to.
func TestValidateClientSecret_LogsTheTenantMismatch(t *testing.T) {
	storage, log, secret := newClientAuthLogFixture(t)

	// The record resolves through the legacy global key under a foreign
	// tenant's context, exactly as a pre-scoping deployment's would.
	ctx := ContextWithTenant(context.Background(), "acme")
	if _, err := storage.ValidateClientSecret(ctx, "app_automatethethings_QQnqeujdEo", secret); err != nil {
		t.Logf("ValidateClientSecret returned %v", err)
	}
	if line := log.find("client authentication", clientAuthReasonTenantMismatch); line == "" {
		t.Fatalf("no log line naming the tenant mismatch; captured:\n%s", log.all())
	}
}

// TestValidateClientSecret_NeverLogsSecretMaterial is the error-handling half:
// the diagnostic must not become the credential leak it was added to avoid.
func TestValidateClientSecret_NeverLogsSecretMaterial(t *testing.T) {
	storage, log, secret := newClientAuthLogFixture(t)
	ctx := ContextWithTenant(context.Background(), "automatethethings")

	for _, presented := range []string{secret, "sk_wrong-secret", ""} {
		if _, err := storage.ValidateClientSecret(ctx, "app_automatethethings_QQnqeujdEo", presented); err != nil {
			t.Logf("ValidateClientSecret(%d bytes) returned %v", len(presented), err)
		}
	}
	if _, err := storage.ValidateClientSecret(ctx, "app_automatethethings_NOSUCH", secret); err == nil {
		t.Fatal("unknown client accepted")
	}

	captured := log.all()
	if strings.Contains(captured, secret) {
		t.Fatalf("the issued client secret appears in the log:\n%s", captured)
	}
	if strings.Contains(captured, "sk_wrong-secret") {
		t.Fatalf("a presented client secret appears in the log:\n%s", captured)
	}
	// The stored digest is credential-adjacent material and must not leak either.
	app, err := storage.GetApp(ctx, "app_automatethethings_QQnqeujdEo")
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if strings.Contains(captured, app.ClientSecret) {
		t.Fatalf("the stored secret digest appears in the log:\n%s", captured)
	}
}
