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
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
)

// =============================================================================
// go-quicbaas #529 — /api/v1/iam/{tenant_id}/webauthn/* was registered TWICE:
// once by the module route table and once by a root chi.Route mounted from
// RegisterRoutes, whose mount returned early and silently whenever the
// xkms-backed manager was not yet resolvable.
//
// Two handlers claiming one tenant-scoped security path, selected by
// initialization order and reported nowhere, is the shape these tests exist to
// keep out. They assert three properties: the path is registered exactly once,
// the deleted second mount cannot come back, and an unavailable manager is
// LOUD rather than silent.
// =============================================================================

// tenantWebAuthnPaths is the surface under test, spelled out rather than
// derived from tenantWebAuthnRoutes so a mistake in that list cannot make these
// tests agree with it.
var tenantWebAuthnPaths = map[string]string{
	"POST /api/v1/iam/{tenant_id}/webauthn/registration/begin":  "/api/v1/iam/acme/webauthn/registration/begin",
	"POST /api/v1/iam/{tenant_id}/webauthn/registration/finish": "/api/v1/iam/acme/webauthn/registration/finish",
	"GET /api/v1/iam/{tenant_id}/webauthn/registration/status":  "/api/v1/iam/acme/webauthn/registration/status",
	"POST /api/v1/iam/{tenant_id}/webauthn/login/begin":         "/api/v1/iam/acme/webauthn/login/begin",
	"POST /api/v1/iam/{tenant_id}/webauthn/login/finish":        "/api/v1/iam/acme/webauthn/login/finish",
}

// TestTenantWebAuthnRoutes_RegisteredExactlyOnce is the route-table half of
// #529: a duplicate registration of any tenant-scoped WebAuthn path must fail
// this test. Both tables are checked — Routes() is what the sealed-window
// listener serves and ModuleRoutes() is what the unified server serves — so a
// duplicate cannot hide in the difference between them.
func TestTenantWebAuthnRoutes_RegisteredExactlyOnce(t *testing.T) {
	for name, table := range map[string][]RouteSpec{
		"Routes":       Routes(),
		"ModuleRoutes": ModuleRoutes(),
	} {
		t.Run(name, func(t *testing.T) {
			counts := map[string]int{}
			for _, spec := range table {
				counts[spec.Method+" "+spec.Pattern]++
			}
			for key := range tenantWebAuthnPaths {
				switch counts[key] {
				case 1:
					// exactly one handler owns the path
				case 0:
					t.Errorf("%s is not registered at all: the tenant WebAuthn ceremony is unreachable", key)
				default:
					t.Errorf("%s is registered %d times; a tenant-scoped security path served by more than one handler picks its handler by initialization order (#529)",
						key, counts[key])
				}
			}
		})
	}
}

// TestRegisterRoutes_DoesNotMountTenantWebAuthn pins the deletion itself. The
// removed mount lived on the legacy in-process router that RegisterRoutes
// builds, and its absence — not a comment asking the next engineer not to
// re-add it — is what keeps the second handler from returning.
//
// The assertion walks the registered PATTERNS rather than serving requests, so
// it reports a re-added mount directly instead of inferring it from a status
// code that several other faults also produce.
func TestRegisterRoutes_DoesNotMountTenantWebAuthn(t *testing.T) {
	_, _, _, handler, cleanup := setupIAMTestServer(t)
	defer cleanup()

	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	registered := map[string]bool{}
	walkErr := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		registered[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk: %v", walkErr)
	}
	if len(registered) == 0 {
		t.Fatal("the walk found no routes at all; the assertion below would pass vacuously")
	}

	for key := range tenantWebAuthnPaths {
		if registered[key] {
			t.Errorf("%s is mounted on the legacy router; the module route table is the single owner of this path (#529)", key)
		}
	}
}

// TestModuleWebAuthnHandler_UnavailableIsLoudAndNeverSilent covers the second
// half of #529: a security-relevant surface that cannot answer says so. The
// handler must refuse with 503 — never a 404 that reads as "no such route", and
// never a fallback to a handler with different tenant semantics — and it must
// log a warning naming the affected routes and what it costs the deployment.
func TestModuleWebAuthnHandler_UnavailableIsLoudAndNeverSilent(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultSlog(&buf)
	defer restore()

	// A Server with no IAM handler: the factory can never resolve.
	handler := (&Server{}).moduleWebAuthnHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/acme/webauthn/login/begin", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d (a silently absent route is indistinguishable from a wrong URL)",
			w.Code, http.StatusServiceUnavailable)
	}

	logged := buf.String()
	if !strings.Contains(logged, "tenant WebAuthn ceremony unavailable") {
		t.Fatalf("no warning was logged for an unavailable ceremony surface; got %q", logged)
	}
	for key := range tenantWebAuthnPaths {
		if !strings.Contains(logged, key) {
			t.Errorf("the warning does not name %s; an operator cannot tell which surface is down", key)
		}
	}
	if !strings.Contains(logged, "no user in any tenant can enroll or present a passkey") {
		t.Errorf("the warning does not state the consequence: %q", logged)
	}
}

// TestModuleWebAuthnHandler_WarnsOncePerEpisode proves the warning is a signal
// rather than a flood. An unavailable manager behind a login page is a
// request-rate event, and one line per request buries the first one, which is
// the only one that carries new information.
func TestModuleWebAuthnHandler_WarnsOncePerEpisode(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultSlog(&buf)
	defer restore()

	handler := (&Server{}).moduleWebAuthnHandler()
	for i := 0; i < 5; i++ {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, "/api/v1/iam/acme/webauthn/login/begin", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("request %d: status = %d, want 503 — every request must refuse, only the LOG is deduplicated", i, w.Code)
		}
	}

	if n := strings.Count(buf.String(), "tenant WebAuthn ceremony unavailable"); n != 1 {
		t.Fatalf("warning logged %d times across 5 refusals, want exactly 1", n)
	}
}

// TestModuleWebAuthnHandler_OneEpisodeAcrossEveryBoundRoute pins where the
// episode flag lives. The module table binds this handler once per ceremony
// path, so a flag held in the closure would warn five times about one outage —
// the same event, the same five route names, five lines. The flag is the
// server's, and this is the assertion that keeps it there.
func TestModuleWebAuthnHandler_OneEpisodeAcrossEveryBoundRoute(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultSlog(&buf)
	defer restore()

	server := &Server{}
	// One handler per bound route, exactly as the module route table binds them.
	for range tenantWebAuthnPaths {
		handler := server.moduleWebAuthnHandler()
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, "/api/v1/iam/acme/webauthn/login/begin", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503", w.Code)
		}
	}

	if n := strings.Count(buf.String(), "tenant WebAuthn ceremony unavailable"); n != 1 {
		t.Fatalf("warning logged %d times for one outage across %d bound routes, want exactly 1",
			n, len(tenantWebAuthnPaths))
	}
}

// TestModuleWebAuthnHandler_ConcurrentFirstRefusalsWarnOnce covers the
// concurrent entry into an episode: the flag is a CompareAndSwap, so racing
// first requests must still produce one line, not one per goroutine.
func TestModuleWebAuthnHandler_ConcurrentFirstRefusalsWarnOnce(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	restore := swapDefaultSlog(&lockedWriter{w: &buf, mu: &mu})
	defer restore()

	handler := (&Server{}).moduleWebAuthnHandler()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handler(httptest.NewRecorder(),
				httptest.NewRequest(http.MethodPost, "/api/v1/iam/acme/webauthn/login/begin", nil))
		}()
	}
	wg.Wait()

	mu.Lock()
	logged := buf.String()
	mu.Unlock()
	if n := strings.Count(logged, "tenant WebAuthn ceremony unavailable"); n != 1 {
		t.Fatalf("warning logged %d times across 16 concurrent refusals, want exactly 1", n)
	}
}

// TestBuildXKMSWebAuthnDispatcher_RefusesARequestThatNamesNoTenant is the
// third property #529 asks for, in the form quicbaas can actually enforce: the
// handler serving a tenant-scoped path derives its tenant from the PATH and
// refuses a request that names none. It never falls back to a default — a
// silent default is how a tenant-named route ends up bound to the system
// tenant.
func TestBuildXKMSWebAuthnDispatcher_RefusesARequestThatNamesNoTenant(t *testing.T) {
	// Mounted WITHOUT a {tenant_id} parameter, which is the shape a
	// single-tenant handler would have.
	router := chi.NewRouter()
	router.Post("/api/v1/iam/webauthn/registration/begin", buildXKMSWebAuthnDispatcher(nil))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/iam/webauthn/registration/begin", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: a ceremony request that names no tenant must be refused, not defaulted",
			w.Code, http.StatusBadRequest)
	}
	if body := w.Body.String(); !strings.Contains(body, "tenant_id is required") {
		t.Fatalf("body = %q, want it to name the missing tenant id", body)
	}
}

// swapDefaultSlog points slog.Default() at w for the duration of a test and
// returns the restore func. The handler logs through slog.Default(), so this is
// the seam that makes "did it warn?" observable without adding a logger
// parameter to production code for the test's benefit.
func swapDefaultSlog(w io.Writer) func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return func() { slog.SetDefault(previous) }
}

// lockedWriter serialises writes from concurrent goroutines so the race
// detector has nothing to report about the test's own buffer.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// jsonLogRoutes is a helper for reading the structured "routes" attribute back
// out of a captured log line, so a test asserts on the recorded VALUE rather
// than on substring luck.
func jsonLogRoutes(t *testing.T, line string) []string {
	t.Helper()
	var record struct {
		Routes []string `json:"routes"`
	}
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, line)
	}
	return record.Routes
}

// TestModuleWebAuthnHandler_WarningCarriesTheRoutesAsStructuredData asserts the
// route list is a structured attribute, not only text: an operator's log
// pipeline filters on the field, and a message that only reads well to a human
// is not an alertable signal.
func TestModuleWebAuthnHandler_WarningCarriesTheRoutesAsStructuredData(t *testing.T) {
	var buf bytes.Buffer
	restore := swapDefaultSlog(&buf)
	defer restore()

	handler := (&Server{}).moduleWebAuthnHandler()
	handler(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/api/v1/iam/acme/webauthn/login/begin", nil))

	line := strings.TrimSpace(buf.String())
	if line == "" {
		t.Fatal("nothing was logged")
	}
	routes := jsonLogRoutes(t, line)
	if len(routes) != len(tenantWebAuthnPaths) {
		t.Fatalf("warning names %d routes, want %d", len(routes), len(tenantWebAuthnPaths))
	}
	for _, route := range routes {
		if _, ok := tenantWebAuthnPaths[route]; !ok {
			t.Errorf("warning names %q, which is not a tenant WebAuthn ceremony route", route)
		}
	}
}
