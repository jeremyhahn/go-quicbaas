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

package fleet

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestProductionEnroller_BootstrapPersistsEventIngestAddr asserts that
// the daemon's first-start registration captures the
// "event_ingest_addr" field from the server response and persists it
// to credentials.json. This is the contract the agent's
// EventForwarder relies on to dial the gRPC ingest URL the daemon
// advertised — without it the agent would dial the REST host and
// fail h2 ALPN.
func TestProductionEnroller_BootstrapPersistsEventIngestAddr(t *testing.T) {
	t.Parallel()

	const ingestURL = "https://198.51.100.91:19090"

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "client-abc",
			"client_secret":      "secret-xyz",
			"agent_id":           "agent-001",
			"service_account_id": "sa-001",
			"role":               "agent-runtime",
			"scopes": []string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			},
			"event_ingest_addr": ingestURL,
		}),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "jwt-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
			"scope": strings.Join([]string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			}, " "),
		}),
	}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	enroller := newProdEnroller(t, srv.Client())

	if _, err := enroller.Enroll(context.Background(), cfg); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	got, err := enroller.EventIngestAddrFromDisk(cfg)
	if err != nil {
		t.Fatalf("EventIngestAddrFromDisk: %v", err)
	}
	if got != ingestURL {
		t.Fatalf("expected ingest addr %q persisted, got %q", ingestURL, got)
	}

	// Belt-and-suspenders: re-read credentials.json directly to
	// confirm the on-disk JSON shape carries the field with the
	// correct name. A schema drift here would silently disable
	// event forwarding for every fielded daemon.
	creds, ok, err := readCredentials(filepath.Join(cfg.WorkDir, credentialsFileName))
	if err != nil || !ok {
		t.Fatalf("readCredentials: ok=%v err=%v", ok, err)
	}
	if creds.EventIngestAddr != ingestURL {
		t.Fatalf("on-disk EventIngestAddr=%q want %q", creds.EventIngestAddr, ingestURL)
	}
}

// TestProductionEnroller_BootstrapAcceptsMissingEventIngestAddr
// asserts the daemon registers cleanly when the server omits
// event_ingest_addr (REST-only server, or a server too old to support
// fleet event forwarding). The persisted addr is the empty string
// — the agent serve path uses that to disable the EventForwarder
// rather than dialing a bogus port.
func TestProductionEnroller_BootstrapAcceptsMissingEventIngestAddr(t *testing.T) {
	t.Parallel()

	stub := &serverStub{
		t: t,
		registerBody: mustJSON(t, map[string]any{
			"client_id":          "client-abc",
			"client_secret":      "secret-xyz",
			"agent_id":           "agent-001",
			"service_account_id": "sa-001",
			"role":               "agent-runtime",
			"scopes": []string{
				orchestration.AgentScopeRuntimeSpawn,
				orchestration.AgentScopeRuntimePrompt,
				orchestration.AgentScopeRuntimeStatus,
			},
			// Note: event_ingest_addr deliberately omitted.
		}),
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "jwt-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}),
	}
	srv := httptest.NewServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	enroller := newProdEnroller(t, srv.Client())

	if _, err := enroller.Enroll(context.Background(), cfg); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	got, err := enroller.EventIngestAddrFromDisk(cfg)
	if err != nil {
		t.Fatalf("EventIngestAddrFromDisk: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty ingest addr when server omits field, got %q", got)
	}
}

// TestEventIngestAddrFromDisk_NilConfig surfaces ErrConfigNil when the
// caller passes a nil config — defensive guard against misuse from
// the daemon entrypoint before flag parsing has materialised a config.
func TestEventIngestAddrFromDisk_NilConfig(t *testing.T) {
	t.Parallel()
	enroller := NewProductionEnroller(ProductionEnrollerDeps{Logger: &testLogger{t: t}})
	_, err := enroller.EventIngestAddrFromDisk(nil)
	if !errors.Is(err, ErrConfigNil) {
		t.Fatalf("want ErrConfigNil, got %v", err)
	}
}

// TestEventIngestAddrFromDisk_MissingCredentials surfaces
// ErrEnrollmentMissingCredentials when the credentials file does not
// exist (caller invoked it before bootstrap completed). The error
// type lets the daemon's serve loop branch on
// errors.Is(err, ErrEnrollmentMissingCredentials) without fragile
// substring matches.
func TestEventIngestAddrFromDisk_MissingCredentials(t *testing.T) {
	t.Parallel()
	cfg := newEnrollerConfig(t, "https://nowhere")
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	enroller := NewProductionEnroller(ProductionEnrollerDeps{Logger: &testLogger{t: t}})
	_, err := enroller.EventIngestAddrFromDisk(cfg)
	if !errors.Is(err, ErrEnrollmentMissingCredentials) {
		t.Fatalf("want ErrEnrollmentMissingCredentials, got %v", err)
	}
}
