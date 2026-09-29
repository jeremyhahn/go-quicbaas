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

// TestRenderStartScript_RunsFromConfigFile: the nohup launcher runs the same
// config file as the unit, and every $HOME path is left for the shell to
// expand rather than single-quoted into a literal "$HOME" directory.
func TestRenderStartScript_RunsFromConfigFile(t *testing.T) {
	s := RenderStartScript(&Spec{})
	for _, want := range []string{
		"#!/bin/sh",
		"nohup \"$HOME\"'/.quicbaas/bin/quicbaas' fleet serve --config \"$HOME\"'/.quicbaas/bin/agent.yaml'",
		">> \"$HOME\"'/.quicbaas/agent/agent.log' 2>&1 &",
		"echo $! > \"$HOME\"'/.quicbaas/agent/agent.pid'",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q:\n%s", want, s)
		}
	}
	for _, gone := range []string{"'$HOME", "--server", "--enrollment-token", "--central"} {
		if strings.Contains(s, gone) {
			t.Fatalf("script contains %q:\n%s", gone, s)
		}
	}
}

func TestStartBackground_WritesAndExecutes(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("mkdir -p '/home/u/bin'", "", "", false)
	srv.setExec("chmod 755 '/home/u/bin/start.sh'", "", "", false)
	srv.setExec("sh '/home/u/bin/start.sh'", "", "", false)

	ctx := context.Background()
	client, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	script := "#!/bin/sh\necho hi\n"
	if err := StartBackground(ctx, client, script, "/home/u/bin/start.sh"); err != nil {
		t.Fatalf("StartBackground: %v", err)
	}

	if got, ok := srv.written("/home/u/bin/start.sh"); !ok || string(got) != script {
		t.Fatalf("script not written: ok=%v got=%q", ok, got)
	}
}

func TestStartBackground_NilClient(t *testing.T) {
	err := StartBackground(context.Background(), nil, "x", "/tmp/s.sh")
	if err == nil {
		t.Fatal("expected error")
	}
	var execErr *RemoteExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("want RemoteExecError, got %T", err)
	}
}

func TestStartBackground_EmptyPath(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	ctx := context.Background()
	c, err := DialSSH(ctx, &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	err = StartBackground(ctx, c, "x", "")
	if err == nil {
		t.Fatal("expected error")
	}
	var specErr *InvalidSpecError
	if !errors.As(err, &specErr) {
		t.Fatalf("want InvalidSpecError, got %T", err)
	}
}

func TestAgentServeArgsOrdering(t *testing.T) {
	for prefix, want := range map[string]string{
		"%h":    "fleet serve --config '%h/.quicbaas/bin/agent.yaml'",
		"$HOME": "fleet serve --config \"$HOME\"'/.quicbaas/bin/agent.yaml'",
	} {
		if got := strings.Join(fleetServeArgs(&Spec{}, prefix), " "); got != want {
			t.Fatalf("fleetServeArgs(%q) = %q, want %q", prefix, got, want)
		}
	}
}
