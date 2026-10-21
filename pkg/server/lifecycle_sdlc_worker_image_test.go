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

package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc/workerimage"
)

func TestWorkerImageAuditor_TranslatesEvents(t *testing.T) {
	audit := &recordingAuditLogger{}
	auditor := &workerImageAuditor{resolve: func() iam.AuditLogger { return audit }}

	auditor.RecordWorkerImageEvent(context.Background(), workerimage.AuditEvent{
		Action: workerimage.AuditActionRefresh, Resource: "w:1", Success: false,
		Details: map[string]string{"error": "gate failed", "digest": ""},
	})
	auditor.RecordWorkerImageEvent(context.Background(), workerimage.AuditEvent{
		TenantID: "acme", Action: workerimage.AuditActionPromote, Resource: "p1", Success: true,
		Details: map[string]string{"to": "w:2"},
	})

	events := audit.recorded()
	if len(events) != 2 {
		t.Fatalf("recorded %d events", len(events))
	}
	pass, promotion := events[0], events[1]
	if pass.TenantID != iam.SystemTenantID || pass.Outcome != iam.AuditOutcomeFailure ||
		pass.Resource.Type != "sdlc_worker_image" || pass.Details["error"] != "gate failed" {
		t.Fatalf("pass event = %+v", pass)
	}
	if _, kept := pass.Details["digest"]; kept {
		t.Fatal("empty details are dropped rather than recorded as empty strings")
	}
	if promotion.TenantID != "acme" || promotion.Outcome != iam.AuditOutcomeSuccess ||
		promotion.Resource.Type != "sdlc_binding" || promotion.Resource.ID != "p1" {
		t.Fatalf("promotion event = %+v", promotion)
	}
}

func TestWorkerImageAuditor_NoLoggerYetRecordsNothing(t *testing.T) {
	auditor := &workerImageAuditor{resolve: func() iam.AuditLogger { return nil }}
	auditor.RecordWorkerImageEvent(context.Background(), workerimage.AuditEvent{Action: workerimage.AuditActionRefresh})
}

func TestStartSDLCWorkerImage_ANodeThatCannotRunItCarriesOn(t *testing.T) {
	cases := map[string]func(*Server){
		"disabled":         func(s *Server) { s.config.SDLC.WorkerImage.Enabled = false },
		"no engine":        func(s *Server) { s.orchContainerRuntime = nil },
		"no tenant source": func(s *Server) { s.orchContainerRuntime = &orchestration.ExecContainerRuntime{} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := testReconcileServer(t)
			s.config = config.Default()
			mutate(s)
			if err := s.startSDLCWorkerImage(nil, nil, nil); err != nil {
				t.Fatalf("startSDLCWorkerImage: %v", err)
			}
			if s.sdlcWorkerImage != nil {
				t.Fatal("a loop was started on a node that cannot run it")
			}
			s.stopSDLCWorkerImage()
		})
	}
}

// fakeAgentService is the orchestrator surface the smoker needs; nothing in
// these tests reaches it.
type fakeAgentService struct {
	orchestration.AgentService
}

type fakeProjectReader struct{}

func (fakeProjectReader) Get(context.Context, string, string) (*orchestration.Project, error) {
	return nil, errors.New("unused")
}

func workerImageTestPins() softwarefactoryworker.Pins {
	return softwarefactoryworker.Pins{Go: "1.27.0", Golangci: "v2.13.2", Gosec: "v2.29.0",
		Govulncheck: "v1.8.0", Gopls: "v0.23.0", Serena: "v1.7.0", ToolDependencyFloors: "golang.org/x/mod@v0.41.0",
		SerenaDependencyFloors: "urllib3>=2.8.0",
		Actionlint:             "v1.7.12", Shellcheck: "v0.11.0", ShellcheckSHA256X86_64: "8c3b", ShellcheckSHA256Aarch64: "12b3",
		Opencode: "v1.18.32", OpencodeSHA256X86_64: "1ce4", OpencodeSHA256Aarch64: "abf3",
		Protoc: "21.12", ProtocSHA256X86_64: "aaaa", ProtocSHA256Aarch64: "bbbb",
		ProtocGenGo: "v1.36.12", ProtocGenGoGRPC: "v1.6.1",
		Node: "v26.10.0", NodeSHA256X86_64: "cccc", NodeSHA256Aarch64: "dddd", Npm: "12.2.0",
		Uv: "0.12.23", UvSHA256X86_64: "eeee", UvSHA256Aarch64: "ffff", NpmBundledDependencyFloors: "undici@6.28.1=sha512-YmJi",
		Pyright: "1.1.414", Typescript: "6.0.3", TypescriptLanguageServer: "6.0.1",
		BashLanguageServer: "5.8.1", YamlLanguageServer: "1.24.0", RubyLsp: "0.26.11",
		Clangd: "23.1.0", ClangdSHA256X86_64: "a1a1",
		RustAnalyzer: "2026-10-05", RustAnalyzerSHA256X86_64: "b2b2", RustAnalyzerSHA256Aarch64: "c3c3",
		Marksman: "2026-02-08", MarksmanSHA256X86_64: "d4d4", MarksmanSHA256Aarch64: "e5e5",
		KotlinLsp:              "263.6379.0",
		KotlinLspSHA256X86_64:  "ab8ca4455dc2fc5fe1a24db2bccc46c104254d2c465155c4251ee65df8f3f7cc",
		KotlinLspSHA256Aarch64: "50999901ef8bcfa1e58561b6a8d782a72dea5620fcf92a64130807f8924a56fc",
		OpenJDK:                "21"}
}

func workerImageTestDeps() workerimage.Deps {
	return workerimage.Deps{
		Engine:   &orchestration.ExecContainerRuntime{},
		Tenants:  staticTenants{},
		Bindings: nilBindings{},
		Agents:   nilAgents{},
		Auditor:  &workerImageAuditor{resolve: func() iam.AuditLogger { return nil }},
		Observer: nopObserver{},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hosts:    newLocalFleetNodeHosts(nil),
		Fleet:    newLocalFleetNodeHosts(nil),
	}
}

type staticTenants struct{}

func (staticTenants) ListTenantIDs(context.Context) ([]string, error) { return nil, nil }

type nilBindings struct{ workerimage.Bindings }

type nilAgents struct{ workerimage.Agents }

type nopObserver struct{}

func (nopObserver) ObserveRefresh(workerimage.Outcome, time.Time) {}

func TestNewSDLCWorkerImageRefresher(t *testing.T) {
	good := config.Default().SDLC.WorkerImage
	if _, err := newSDLCWorkerImageRefresher(good, workerImageTestPins(), workerImageTestDeps(),
		fakeAgentService{}, fakeProjectReader{}, nil); err != nil {
		t.Fatalf("the shipped configuration must assemble: %v", err)
	}

	cases := map[string]func(*config.SDLCWorkerImage){
		"bad npm url":       func(c *config.SDLCWorkerImage) { c.NPMRegistryURL = "nope" },
		"half a credential": func(c *config.SDLCWorkerImage) { c.RegistryUsername = "agent" },
		"no secrets service": func(c *config.SDLCWorkerImage) {
			withWorkerImageSecretCredential(c)
		},
		"tagged repository": func(c *config.SDLCWorkerImage) { c.Repository = "w:1" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := good
			mutate(&cfg)
			if _, err := newSDLCWorkerImageRefresher(cfg, workerImageTestPins(), workerImageTestDeps(),
				fakeAgentService{}, fakeProjectReader{}, nil); !errors.Is(err, workerimage.ErrInvalidConfig) {
				t.Fatalf("err = %v, want ErrInvalidConfig", err)
			}
		})
	}
	if _, err := newSDLCWorkerImageRefresher(good, workerImageTestPins(), workerImageTestDeps(),
		nil, fakeProjectReader{}, nil); !errors.Is(err, workerimage.ErrInvalidConfig) {
		t.Fatalf("a smoker with no orchestrator must be refused, got %v", err)
	}
}

// withWorkerImageSecretCredential names a registry and a full secret-backed
// push credential.
func withWorkerImageSecretCredential(c *config.SDLCWorkerImage) {
	c.Registry = "reg.example:3001/o/worker"
	c.RegistryUsername = "agent-company"
	c.RegistryTokenSecret = "sdlc/registry/worker-image/token"
	c.RegistryTokenTenant = "automatethethings"
}

// stubSecretResolver answers every read with one value.
type stubSecretResolver struct{}

func (stubSecretResolver) ResolveAccountSecret(context.Context, string, string) (*orchestration.AccountSecretValue, error) {
	return &orchestration.AccountSecretValue{Data: []byte("token")}, nil
}

func TestNewSDLCWorkerImageRefresher_TheCredentialIsATenantSecret(t *testing.T) {
	cfg := config.Default().SDLC.WorkerImage
	withWorkerImageSecretCredential(&cfg)
	if _, err := newSDLCWorkerImageRefresher(cfg, workerImageTestPins(), workerImageTestDeps(),
		fakeAgentService{}, fakeProjectReader{}, stubSecretResolver{}); err != nil {
		t.Fatalf("a registry with a secret-backed credential must assemble: %v", err)
	}

	cfg.RegistryTokenSecret = "tenant/automatethethings/sdlc/registry/worker-image/token"
	_, err := newSDLCWorkerImageRefresher(cfg, workerImageTestPins(), workerImageTestDeps(),
		fakeAgentService{}, fakeProjectReader{}, stubSecretResolver{})
	var invalid *workerimage.InvalidConfigError
	if !errors.As(err, &invalid) || invalid.Field != "registry_token_secret" {
		t.Fatalf("err = %v, want the storage-path reference refused at startup", err)
	}
}
