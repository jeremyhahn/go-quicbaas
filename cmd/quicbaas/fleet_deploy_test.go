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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// stubDeployService lets tests substitute each service method. A method a
// test does not set records the spec it was handed and succeeds.
type stubDeployService struct {
	fn        func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)
	preflight func(ctx context.Context, spec *deploy.Spec) (*deploy.PreflightReport, error)
	lifecycle func(action string, spec *deploy.Spec, opts service.UndeployOptions) (*deploy.LifecycleResult, error)
}

func (s *stubDeployService) Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
	return s.fn(ctx, spec)
}

func (s *stubDeployService) Preflight(ctx context.Context, spec *deploy.Spec) (*deploy.PreflightReport, error) {
	return s.preflight(ctx, spec)
}

func (s *stubDeployService) Stop(_ context.Context, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
	return s.lifecycle("stop", spec, service.UndeployOptions{})
}

func (s *stubDeployService) Start(_ context.Context, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
	return s.lifecycle("start", spec, service.UndeployOptions{})
}

func (s *stubDeployService) Undeploy(_ context.Context, spec *deploy.Spec, opts service.UndeployOptions) (*deploy.LifecycleResult, error) {
	return s.lifecycle("undeploy", spec, opts)
}

// installStubService temporarily swaps the deploy factory for the
// duration of a test.
func installStubService(t *testing.T, fn func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)) {
	t.Helper()
	orig := fleetDeployServiceFactory
	fleetDeployServiceFactory = func() (service.FleetDeployService, error) {
		return &stubDeployService{fn: fn}, nil
	}
	t.Cleanup(func() { fleetDeployServiceFactory = orig })
}

// resetDeployFlags reverts the package-level deploy flags to their
// defaults so each test starts from a clean slate.
func resetDeployFlags(t *testing.T) {
	t.Helper()
	deploySSH = ""
	deploySSHPassword = ""
	deployInsecureHostKey = false
	deployServer = ""
	deployBindAddr = ""
	deployBackends = []string{"acp"}
	deployBinary = "bin/quicbaas-agent"
	deployInstallDir = ""
	deployDataDir = ""
	deployWaitOnline = 2 * time.Minute
	deploySystemdFlag = "auto"
	deploySpecFile = ""
	deployProjectsRoot = ""
	deployMaxJobs = 0
	deployPreflightOnly = false
	deploySkipPreflight = false
	deployLocal = false
	deploySSHKeyName = ""
	deploySSHKeyScope = ""
	deploySSHKeyOwner = ""
	undeployPurgeWorkspaces = false
	fleetOutputFmt = "table"
}

func TestParseSSHTarget(t *testing.T) {
	cases := []struct {
		in, user, host string
		wantErr        bool
	}{
		{"alice@node01", "alice", "node01", false},
		{"bob@10.0.0.1:2222", "bob", "10.0.0.1:2222", false},
		{"", "", "", true},
		{"no-at-sign", "", "", true},
		{"@host", "", "", true},
		{"user@", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			u, h, err := parseSSHTarget(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				var ise *deploy.InvalidSpecError
				if !errors.As(err, &ise) {
					t.Fatalf("want InvalidSpecError, got %T", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if u != tc.user || h != tc.host {
				t.Fatalf("got (%q,%q) want (%q,%q)", u, h, tc.user, tc.host)
			}
		})
	}
}

func TestParseSystemdFlag(t *testing.T) {
	cases := []struct {
		in      string
		wantNil bool
		wantVal bool
		wantErr bool
	}{
		{"", true, false, false},
		{"auto", true, false, false},
		{"yes", false, true, false},
		{"true", false, true, false},
		{"on", false, true, false},
		{"no", false, false, false},
		{"false", false, false, false},
		{"off", false, false, false},
		{"unknown", false, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			v, err := parseSystemdFlag(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if tc.wantNil {
				if v != nil {
					t.Fatalf("expected nil, got %v", *v)
				}
				return
			}
			if v == nil || *v != tc.wantVal {
				t.Fatalf("got %v want %v", v, tc.wantVal)
			}
		})
	}
}

func TestBuildDeploySpec_CopiesAllFlags(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "alice@node01"
	deployServer = "https://c:8443"
	deployBackends = []string{"acp", "ollama"}
	deployInstallDir = "opt/bin"
	deployDataDir = "opt/state"
	deployWaitOnline = 45 * time.Second
	deploySystemdFlag = "yes"
	globalTenantID = "acme"
	defer func() { globalTenantID = "default" }()

	spec, err := buildDeploySpec()
	if err != nil {
		t.Fatalf("buildDeploySpec: %v", err)
	}
	if spec.SSHUser != "alice" || spec.Host != "node01" {
		t.Fatalf("ssh user/host parsed wrong: %#v", spec)
	}
	if spec.Tenant != "acme" {
		t.Fatalf("tenant=%q", spec.Tenant)
	}
	if spec.Server != "https://c:8443" {
		t.Fatalf("server=%q", spec.Server)
	}
	if len(spec.Backends) != 2 || spec.Backends[0] != "acp" {
		t.Fatalf("backends=%v", spec.Backends)
	}
	if spec.InstallDir != "opt/bin" || spec.DataDir != "opt/state" {
		t.Fatalf("dirs: %q %q", spec.InstallDir, spec.DataDir)
	}
	if spec.Systemd == nil || *spec.Systemd != true {
		t.Fatalf("systemd flag %v", spec.Systemd)
	}
	if spec.WaitOnline != 45*time.Second {
		t.Fatalf("wait=%v", spec.WaitOnline)
	}
}

func TestBuildDeploySpec_InvalidSSH(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "not-a-target"
	_, err := buildDeploySpec()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildDeploySpec_InvalidSystemd(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deploySystemdFlag = "banana"
	_, err := buildDeploySpec()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunAgentDeploy_CallsService(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "alice@node01"
	deployServer = "https://c:8443"
	deployInsecureHostKey = true
	deploySystemdFlag = "no"
	deployWaitOnline = 0

	installStubService(t, func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
		if spec.Server != "https://c:8443" {
			t.Errorf("server=%q", spec.Server)
		}
		return &deploy.Result{
			Host:          spec.Host,
			InstalledPath: "$HOME/.quicbaas/bin/quicbaas",
			ServiceStatus: "nohup",
		}, nil
	})

	err := runFleetDeploy(nil, nil)
	if err != nil {
		t.Fatalf("runFleetDeploy: %v", err)
	}
}

func TestRunAgentDeploy_PropagatesError(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deployServer = "c"
	deployWaitOnline = 0

	want := &deploy.SSHDialError{Host: "b"}
	installStubService(t, func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
		return nil, want
	})
	err := runFleetDeploy(nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

// setDeployTenant sets the global tenant flag the spec is built from and
// restores it when the test ends.
func setDeployTenant(t *testing.T, tenant string) {
	t.Helper()
	orig := globalTenantID
	globalTenantID = tenant
	t.Cleanup(func() { globalTenantID = orig })
}

// boundTenant returns the tenant tenantctx reads from ctx, failing the test
// when none is bound.
func boundTenant(t *testing.T, ctx context.Context) string {
	t.Helper()
	tenant, err := tenantctx.FromContext(ctx, "fleet deploy test")
	if err != nil {
		t.Errorf("no tenant bound: %v", err)
	}
	return tenant
}

func TestRunFleetDeploy_BindsTheSpecTenant(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deployServer = "c"
	deployWaitOnline = 0
	setDeployTenant(t, "acme")

	var seen string
	installStubService(t, func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
		seen = boundTenant(t, ctx)
		return &deploy.Result{Host: spec.Host}, nil
	})
	if err := runFleetDeploy(nil, nil); err != nil {
		t.Fatalf("runFleetDeploy: %v", err)
	}
	if seen != "acme" {
		t.Fatalf("Deploy saw tenant %q, want acme", seen)
	}
}

func TestRunFleetDeploy_BindsTheSystemTenantWhenTheSpecNamesNone(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deployServer = "c"
	deployWaitOnline = 0
	setDeployTenant(t, "")

	var seen, specTenant string
	installStubService(t, func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
		seen = boundTenant(t, ctx)
		specTenant = spec.Tenant
		return &deploy.Result{Host: spec.Host}, nil
	})
	if err := runFleetDeploy(nil, nil); err != nil {
		t.Fatalf("runFleetDeploy: %v", err)
	}
	if !tenantctx.IsSystem(seen) {
		t.Fatalf("Deploy saw tenant %q, want the system tenant", seen)
	}
	// The service validates spec.Tenant and mints the node certificate from
	// it, so the spec must carry the same tenant as the context.
	if specTenant != tenantctx.SystemTenantID {
		t.Fatalf("Deploy saw spec tenant %q, want %q", specTenant, tenantctx.SystemTenantID)
	}
}

func TestRunFleetDeploy_PreflightOnlyBindsTheSpecTenant(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deployServer = "c"
	deployWaitOnline = 0
	deployPreflightOnly = true
	fleetOutputFmt = "json"
	setDeployTenant(t, "acme")

	var seen string
	orig := fleetDeployServiceFactory
	fleetDeployServiceFactory = func() (service.FleetDeployService, error) {
		return &stubDeployService{preflight: func(ctx context.Context, spec *deploy.Spec) (*deploy.PreflightReport, error) {
			seen = boundTenant(t, ctx)
			return nil, nil
		}}, nil
	}
	t.Cleanup(func() { fleetDeployServiceFactory = orig })

	if err := runFleetDeploy(nil, nil); err != nil {
		t.Fatalf("runFleetDeploy: %v", err)
	}
	if seen != "acme" {
		t.Fatalf("Preflight saw tenant %q, want acme", seen)
	}
}

func TestDeployContext_BindsTheSpecTenant(t *testing.T) {
	ctx := deployContext(context.Background(), &deploy.Spec{Tenant: "acme"})
	if got := boundTenant(t, ctx); got != "acme" {
		t.Fatalf("tenant = %q, want acme", got)
	}
}

func TestDeployContext_FallsBackToTheSystemTenant(t *testing.T) {
	spec := &deploy.Spec{}
	ctx := deployContext(context.Background(), spec)
	if got := boundTenant(t, ctx); !tenantctx.IsSystem(got) {
		t.Fatalf("tenant = %q, want the system tenant", got)
	}
	if spec.Tenant != tenantctx.SystemTenantID {
		t.Fatalf("spec tenant = %q, want %q", spec.Tenant, tenantctx.SystemTenantID)
	}
}

func TestDeployContext_KeepsTheParentDeadline(t *testing.T) {
	want := time.Now().Add(time.Hour)
	parent, cancel := context.WithDeadline(context.Background(), want)
	defer cancel()

	for _, spec := range []*deploy.Spec{{Tenant: "acme"}, {}} {
		got, ok := deployContext(parent, spec).Deadline()
		if !ok || !got.Equal(want) {
			t.Fatalf("tenant %q: deadline = (%v, %v), want %v", spec.Tenant, got, ok, want)
		}
	}
}

func TestFormatAgentDeployResult_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	err := formatFleetDeployResult(&deploy.Result{
		Host: "h:22", InstalledPath: "/a/b", ServiceStatus: "systemd",
		NodeID: "aid", OnlineAt: "2099",
		NodeCertPath: "/etc/qb/agent.crt", NodeCertSerial: "cafe",
	}, "table", buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"h:22", "/a/b", "systemd", "aid", "2099", "agent.crt", "cafe"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestFormatAgentDeployResult_JSON(t *testing.T) {
	buf := &bytes.Buffer{}
	err := formatFleetDeployResult(&deploy.Result{Host: "h", ServiceStatus: "nohup"}, "json", buf)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v; out=%s", err, buf.String())
	}
	if got["host"] != "h" {
		t.Fatalf("host=%v", got["host"])
	}
}

func TestFormatAgentDeployResult_Yaml(t *testing.T) {
	buf := &bytes.Buffer{}
	err := formatFleetDeployResult(&deploy.Result{Host: "h"}, "yaml", buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "host: h") {
		t.Fatalf("yaml out=%q", buf.String())
	}
}

func TestFormatAgentDeployResult_InvalidFormat(t *testing.T) {
	err := formatFleetDeployResult(&deploy.Result{}, "pdf", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestAgentDeployCommand_FlagBinding(t *testing.T) {
	resetDeployFlags(t)
	cmd := newFleetDeployCommand()
	cmd.SetArgs([]string{
		"--ssh", "alice@node01",
		"--server", "https://c:8443",
		"--ssh-insecure-host-key",
		"--backend", "acp",
		"--backend", "ollama",
		"--systemd", "yes",
		"--wait-online", "10s",
	})
	installStubService(t, func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
		if spec.Systemd == nil || !*spec.Systemd {
			t.Errorf("systemd=%v", spec.Systemd)
		}
		if len(spec.Backends) != 2 {
			t.Errorf("backends=%v", spec.Backends)
		}
		if spec.WaitOnline != 10*time.Second {
			t.Errorf("wait=%v", spec.WaitOnline)
		}
		return &deploy.Result{Host: spec.Host, ServiceStatus: "systemd"}, nil
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// The node agent runs runc libcontainer in-process, so deploy must ship the
// cgo agent build (make build-agent), not the CGO_ENABLED=0 static CLI.
func TestAgentDeployCommand_BinaryDefaultsToTheAgentBuild(t *testing.T) {
	flag := newFleetDeployCommand().Flags().Lookup("binary")
	if flag == nil {
		t.Fatal("fleet deploy has no --binary flag")
	}
	if flag.DefValue != "bin/quicbaas-agent" {
		t.Errorf("--binary default = %q, want bin/quicbaas-agent", flag.DefValue)
	}
}

// TestCreateAgentDeployService_DefersToSeamError asserts that when the
// embedded-server cert issuer seam returns an error, createFleetDeployService
// propagates it. This guards the operator against silently misconfiguring
// a non-embedded CLI.
func TestCreateAgentDeployService_DefersToSeamError(t *testing.T) {
	origOrch := orchestratorClientFactory
	orchestratorClientFactory = func() (OrchestratorClient, error) {
		return nil, errors.New("no client")
	}
	defer func() { orchestratorClientFactory = origOrch }()

	origResolve := resolveFleetNodeCertIssuer
	resolveFleetNodeCertIssuer = func() (service.FleetNodeCertIssuer, error) {
		return nil, errors.New("cert issuer unavailable")
	}
	defer func() { resolveFleetNodeCertIssuer = origResolve }()

	_, err := createFleetDeployService()
	if err == nil {
		t.Fatal("expected error from seam")
	}
}

// TestCreateAgentDeployService_UsesIssuerSeam asserts the factory wires
// the seam-provided issuer into the returned service so a happy-path
// Deploy() call will observe it.
func TestCreateAgentDeployService_UsesIssuerSeam(t *testing.T) {
	origOrch := orchestratorClientFactory
	orchestratorClientFactory = func() (OrchestratorClient, error) {
		return nil, errors.New("no client")
	}
	defer func() { orchestratorClientFactory = origOrch }()

	origResolve := resolveFleetNodeCertIssuer
	resolveFleetNodeCertIssuer = func() (service.FleetNodeCertIssuer, error) {
		return stubIssuerOK{}, nil
	}
	defer func() { resolveFleetNodeCertIssuer = origResolve }()

	svc, err := createFleetDeployService()
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// stubIssuerOK satisfies FleetNodeCertIssuer with a minimal success return.
// It is declared here instead of in bindings tests so the CLI test file
// is self-contained.
type stubIssuerOK struct{}

func (stubIssuerOK) IssueFleetNodeCertificate(_ context.Context, _ *service.FleetNodeCertificateRequest) (*service.IssuedFleetNodeCertificate, error) {
	return nil, errors.New("stub issuer not expected to be called")
}

func TestBuildDeploySpec_CarriesTheJobCap(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deployMaxJobs = 2
	spec, err := buildDeploySpec()
	if err != nil || spec.Node.MaxConcurrentJobs != 2 {
		t.Fatalf("buildDeploySpec = (%+v, %v), want Agent.MaxConcurrentJobs 2", spec, err)
	}
}

func TestBuildDeploySpec_RejectsANegativeJobCap(t *testing.T) {
	resetDeployFlags(t)
	deploySSH = "a@b"
	deployMaxJobs = -1
	var invalid *deploy.InvalidSpecError
	if _, err := buildDeploySpec(); !errors.As(err, &invalid) || invalid.Field != "max_concurrent_jobs" {
		t.Fatalf("err = %v, want InvalidSpecError on max_concurrent_jobs", err)
	}
}
