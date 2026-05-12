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

func TestRenderStartScript_ContainsAllFlags(t *testing.T) {
	spec := &Spec{
		Server:    "https://c:8443",
		Tenant:    "acme",
		AgentBind: "127.0.0.1:9091",
		Backends:  []string{"acp"},
	}
	s := RenderStartScript(spec)
	for _, want := range []string{
		"#!/bin/sh",
		"nohup",
		"agent", "serve",
		"--server", "https://c:8443",
		"--tenant", "acme",
		"--bind", "127.0.0.1:9091",
		"--backends", "acp",
		"--agent-cert", "$HOME/.quicbaas/agent/agent.crt",
		"--agent-key", "$HOME/.quicbaas/agent/agent.key",
		"--server-ca", "$HOME/.quicbaas/agent/server-ca.crt",
		".quicbaas/agent/agent.log",
		".quicbaas/agent/agent.pid",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("script missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "--enrollment-token") {
		t.Fatalf("script still references --enrollment-token:\n%s", s)
	}
	if strings.Contains(s, "--central") {
		t.Fatalf("script still references --central:\n%s", s)
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
	spec := &Spec{
		Server:    "c",
		Tenant:    "t",
		AgentBind: "b",
		Backends:  []string{"x", "y"},
	}
	args := agentServeArgs(spec, "%h")
	if args[0] != "agent" || args[1] != "serve" {
		t.Fatalf("prefix wrong: %v", args[:2])
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--server", "--tenant", "--bind", "--backends", "--agent-cert", "--agent-key", "--server-ca"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
}
