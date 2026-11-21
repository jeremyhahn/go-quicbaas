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

// TestRenderUnit_RunsFromConfigFile: the unit names only the rendered config
// file, so no daemon setting can be missing from it for want of a flag.
func TestRenderUnit_RunsFromConfigFile(t *testing.T) {
	spec := &Spec{InstallDir: "bin", DataDir: "state"}
	unit := RenderUnit(spec)
	for _, want := range []string{
		"[Unit]",
		"[Service]",
		"[Install]",
		"Restart=on-failure",
		"ExecStart=%h/bin/quicbaas fleet serve --config '%h/bin/agent.yaml'\n",
		"WorkingDirectory=%h/state",
	} {
		if !strings.Contains(unit, want) {
			t.Fatalf("unit missing %q:\n%s", want, unit)
		}
	}
	for _, gone := range []string{"--server", "--tenant", "--enrollment-token", "--central"} {
		if strings.Contains(unit, gone) {
			t.Fatalf("unit still passes %s on the command line:\n%s", gone, unit)
		}
	}
}

func TestRenderUnit_DefaultsApplied(t *testing.T) {
	unit := RenderUnit(&Spec{})
	if !strings.Contains(unit, "%h/.quicbaas/bin/quicbaas fleet serve --config '%h/.quicbaas/bin/agent.yaml'") {
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

// TestRenderUnit_RestartPolicyCannotSpin pins the supervisor half of the
// enrollment-loop fix (issue #643). The daemon now waits out a server that
// is merely unreachable, so a process that still exits is reporting a
// condition restarting cannot fix. The unit must therefore escalate its
// restart delay and give up into "failed", where monitoring sees it,
// instead of sitting in "activating (auto-restart)" forever.
func TestRenderUnit_RestartPolicyCannotSpin(t *testing.T) {
	spec := &Spec{
		Server:     "https://c.example.com:8443",
		Tenant:     "acme",
		NodeBind:   "0.0.0.0:9091",
		Backends:   []string{"acp"},
		InstallDir: "bin",
		DataDir:    "state",
	}
	unit := RenderUnit(spec)
	for _, want := range []string{
		"RestartSec=5",
		"RestartSteps=5",
		"RestartMaxDelaySec=120",
		"StartLimitIntervalSec=300",
		"StartLimitBurst=5",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}

	// The start-rate limit is a [Unit] property; systemd silently ignores
	// it in [Service], which is how the original unit ended up with no
	// effective limit at all.
	unitSection, serviceSection, ok := strings.Cut(unit, "[Service]")
	if !ok {
		t.Fatalf("unit has no [Service] section:\n%s", unit)
	}
	for _, key := range []string{"StartLimitIntervalSec=", "StartLimitBurst="} {
		if !strings.Contains(unitSection, key) {
			t.Errorf("%s must live in [Unit]:\n%s", key, unit)
		}
		if strings.Contains(serviceSection, key) {
			t.Errorf("%s must not be repeated in [Service]:\n%s", key, unit)
		}
	}
}

// TestRenderUnit_SignalsTheDaemonAloneSoItCanDrain pins go-quicbaas#1064. The
// default KillMode (control-group) sends SIGTERM to every process in the unit
// at once, the `docker run` client behind each live worker included, which
// closed a worker's stream mid-prompt before the daemon had begun to drain.
// mixed signals the daemon alone and leaves the rest until it has exited.
func TestRenderUnit_SignalsTheDaemonAloneSoItCanDrain(t *testing.T) {
	unit := RenderUnit(&Spec{
		Server:     "https://c.example.com:8443",
		Tenant:     "acme",
		NodeBind:   "0.0.0.0:9091",
		Backends:   []string{"acp"},
		InstallDir: "bin",
		DataDir:    "state",
	})
	_, serviceSection, ok := strings.Cut(unit, "[Service]")
	if !ok {
		t.Fatalf("unit has no [Service] section:\n%s", unit)
	}
	if !strings.Contains(serviceSection, "KillMode=mixed\n") {
		t.Errorf("[Service] must set KillMode=mixed so the daemon drains before its workers are signalled:\n%s", unit)
	}
}
