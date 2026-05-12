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

	"github.com/jeremyhahn/go-quicbaas/pkg/agent/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubDeployService lets tests substitute the Deploy implementation.
type stubDeployService struct {
	fn func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)
}

func (s *stubDeployService) Deploy(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error) {
	return s.fn(ctx, spec)
}

// installStubService temporarily swaps the deploy factory for the
// duration of a test.
func installStubService(t *testing.T, fn func(ctx context.Context, spec *deploy.Spec) (*deploy.Result, error)) {
	t.Helper()
	orig := agentDeployServiceFactory
	agentDeployServiceFactory = func() (service.AgentDeployService, error) {
		return &stubDeployService{fn: fn}, nil
	}
	t.Cleanup(func() { agentDeployServiceFactory = orig })
}

// resetDeployFlags reverts the package-level deploy flags to their
// defaults so each test starts from a clean slate.
func resetDeployFlags(t *testing.T) {
	t.Helper()
	deploySSH = ""
	deploySSHPassword = ""
	deployInsecureHostKey = false
	deployServer = ""
	deployBindAddr = "0.0.0.0:9091"
	deployBackends = []string{"acp"}
	deployBinary = "bin/quicbaas-static"
	deployInstallDir = ""
	deployDataDir = ""
	deployWaitOnline = 2 * time.Minute
	deploySystemdFlag = "auto"
	agentOutputFmt = "table"
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

	err := runAgentDeploy(nil, nil)
	if err != nil {
		t.Fatalf("runAgentDeploy: %v", err)
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
	err := runAgentDeploy(nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatAgentDeployResult_Table(t *testing.T) {
	buf := &bytes.Buffer{}
	err := formatAgentDeployResult(&deploy.Result{
		Host: "h:22", InstalledPath: "/a/b", ServiceStatus: "systemd",
		AgentID: "aid", OnlineAt: "2099",
		AgentCertPath: "/etc/qb/agent.crt", AgentCertSerial: "cafe",
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
	err := formatAgentDeployResult(&deploy.Result{Host: "h", ServiceStatus: "nohup"}, "json", buf)
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
	err := formatAgentDeployResult(&deploy.Result{Host: "h"}, "yaml", buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "host: h") {
		t.Fatalf("yaml out=%q", buf.String())
	}
}

func TestFormatAgentDeployResult_InvalidFormat(t *testing.T) {
	err := formatAgentDeployResult(&deploy.Result{}, "pdf", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestAgentDeployCommand_FlagBinding(t *testing.T) {
	resetDeployFlags(t)
	cmd := newAgentDeployCommand()
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

// TestCreateAgentDeployService_DefersToSeamError asserts that when the
// embedded-server cert issuer seam returns an error, createAgentDeployService
// propagates it. This guards the operator against silently misconfiguring
// a non-embedded CLI.
func TestCreateAgentDeployService_DefersToSeamError(t *testing.T) {
	origOrch := orchestratorClientFactory
	orchestratorClientFactory = func() (OrchestratorClient, error) {
		return nil, errors.New("no client")
	}
	defer func() { orchestratorClientFactory = origOrch }()

	origResolve := resolveAgentCertIssuer
	resolveAgentCertIssuer = func() (service.AgentCertIssuer, error) {
		return nil, errors.New("cert issuer unavailable")
	}
	defer func() { resolveAgentCertIssuer = origResolve }()

	_, err := createAgentDeployService()
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

	origResolve := resolveAgentCertIssuer
	resolveAgentCertIssuer = func() (service.AgentCertIssuer, error) {
		return stubIssuerOK{}, nil
	}
	defer func() { resolveAgentCertIssuer = origResolve }()

	svc, err := createAgentDeployService()
	if err != nil {
		t.Fatal(err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
}

// stubIssuerOK satisfies AgentCertIssuer with a minimal success return.
// It is declared here instead of in bindings tests so the CLI test file
// is self-contained.
type stubIssuerOK struct{}

func (stubIssuerOK) IssueAgentCertificate(_ context.Context, _ *service.AgentCertificateRequest) (*service.IssuedAgentCertificate, error) {
	return nil, errors.New("stub issuer not expected to be called")
}
