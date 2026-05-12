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

package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRenderUnit_ContainsAllFlags(t *testing.T) {
	spec := &Spec{
		Server:     "https://c.example.com:8443",
		Tenant:     "acme",
		AgentBind:  "0.0.0.0:9091",
		Backends:   []string{"acp", "ollama"},
		InstallDir: "bin",
		DataDir:    "state",
	}
	unit := RenderUnit(spec)
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"Restart=on-failure",
		"agent", "serve",
		"--server", "https://c.example.com:8443",
		"--tenant", "acme",
		"--bind", "0.0.0.0:9091",
		"--backends", "acp,ollama",
		"--agent-cert", "state/agent.crt",
		"--agent-key", "state/agent.key",
		"--server-ca", "state/server-ca.crt",
		"WorkingDirectory=%h/state",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	// Ensure the legacy enrollment-token flag is gone.
	if strings.Contains(unit, "--enrollment-token") {
		t.Fatalf("unit still references --enrollment-token:\n%s", unit)
	}
	if strings.Contains(unit, "--central") {
		t.Fatalf("unit still references --central:\n%s", unit)
	}
}

func TestRenderUnit_DefaultsApplied(t *testing.T) {
	spec := &Spec{
		Server:    "c",
		Tenant:    "t",
		AgentBind: ":9091",
		Backends:  []string{"acp"},
	}
	unit := RenderUnit(spec)
	if !strings.Contains(unit, ".quicbaas/bin/quicbaas") {
		t.Fatalf("expected default install dir in unit:\n%s", unit)
	}
	if !strings.Contains(unit, "%h/.quicbaas/agent") {
		t.Fatalf("expected default data dir in unit:\n%s", unit)
	}
}

func TestInstallAndStart_WritesAndStarts(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p \"$HOME\"'/.config/systemd/user'", "", "", false)
	srv.setExec("chmod 644 \"$HOME\"'/.config/systemd/user/quicbaas-agent.service'", "", "", false)
	srv.setExec("systemctl --user daemon-reload", "", "", false)
	srv.setExec("systemctl --user enable 'quicbaas-agent.service'", "", "", false)
	srv.setExec("systemctl --user restart 'quicbaas-agent.service'", "", "", false)

	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	unit := "[Unit]\nDescription=Test\n"
	if err := InstallAndStart(ctx, client, unit, DefaultUnitName); err != nil {
		t.Fatalf("InstallAndStart: %v", err)
	}

	if got, ok := srv.written("$HOME/.config/systemd/user/quicbaas-agent.service"); !ok || string(got) != unit {
		t.Fatalf("unit not written correctly: ok=%v got=%q", ok, got)
	}
}

func TestInstallAndStart_FailsAtStart(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p \"$HOME\"'/.config/systemd/user'", "", "", false)
	srv.setExec("chmod 644 \"$HOME\"'/.config/systemd/user/quicbaas-agent.service'", "", "", false)
	srv.setExec("systemctl --user daemon-reload", "", "", false)
	srv.setExec("systemctl --user enable 'quicbaas-agent.service'", "", "", false)
	// Start fails.
	srv.setExec("systemctl --user restart 'quicbaas-agent.service'", "", "unit fell over", true)

	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	err = InstallAndStart(ctx, client, "x", DefaultUnitName)
	if err == nil {
		t.Fatal("expected error at start")
	}
	var sysErr *SystemdInstallError
	if !errors.As(err, &sysErr) {
		t.Fatalf("want SystemdInstallError, got %T: %v", err, err)
	}
	if sysErr.Stage != "start" {
		t.Fatalf("stage=%q", sysErr.Stage)
	}
}

func TestInstallAndStart_NilClient(t *testing.T) {
	err := InstallAndStart(context.Background(), nil, "u", "unit.service")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallAndStart_EmptyUnitName(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if err := InstallAndStart(ctx, client, "x", ""); err == nil {
		t.Fatal("expected error for empty unit name")
	}
}

func TestDetectSystemdUser(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("systemctl --user --version >/dev/null 2>&1", "", "", false)
	client, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	if !DetectSystemdUser(context.Background(), client) {
		t.Fatal("expected detect true")
	}

	// Configure the probe to fail.
	srv.setExec("systemctl --user --version >/dev/null 2>&1", "", "not found", true)
	if DetectSystemdUser(context.Background(), client) {
		t.Fatal("expected detect false on failure")
	}
}

func TestDetectSystemdUser_Nil(t *testing.T) {
	if DetectSystemdUser(context.Background(), nil) {
		t.Fatal("nil client must report false")
	}
}

func TestStageFor(t *testing.T) {
	cases := map[string]string{
		"systemctl --user daemon-reload": "daemon-reload",
		"systemctl --user enable foo":    "enable",
		"systemctl --user restart foo":   "start",
		"systemctl --user start foo":     "start",
		"something else":                 "exec",
	}
	for cmd, want := range cases {
		if got := stageFor(cmd); got != want {
			t.Fatalf("stageFor(%q)=%q want %q", cmd, got, want)
		}
	}
}
