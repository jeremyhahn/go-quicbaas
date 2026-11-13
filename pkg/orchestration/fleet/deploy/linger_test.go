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

// lingerTestClient dials the in-process SSH fixture as user "u" so the
// linger helpers resolve the same remote user every test asserts on.
func lingerTestClient(t *testing.T, srv *testSSHServer) *SSHClient {
	t.Helper()
	client, err := DialSSH(context.Background(), &Spec{
		Host: srv.addr(), SSHUser: "u", SSHPassword: "p", InsecureHostKey: true,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestEnsureLinger_EnablesWhenDisabled is the remote-agent failure mode: the
// user manager does not linger, so the agent would be killed with the
// deploy's SSH session. With sudo available the deploy fixes it itself.
func TestEnsureLinger_EnablesWhenDisabled(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	srv.setExec("sudo -n loginctl enable-linger 'u'", "", "", false)

	client := lingerTestClient(t, srv)
	if err := EnsureLinger(context.Background(), client, "u"); err != nil {
		t.Fatalf("EnsureLinger: %v", err)
	}
	if !srv.executed("sudo -n loginctl enable-linger 'u'") {
		t.Fatalf("enable-linger was not executed; ran=%v", srv.ranCommands())
	}
}

// TestEnsureLinger_NoSudoFailsDeploy pins the second half of the rule: a
// deploy that cannot enable lingering must fail loudly, naming the exact
// command the operator has to run, rather than start an agent that dies
// with the session.
func TestEnsureLinger_NoSudoFailsDeploy(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	srv.setExec("sudo -n loginctl enable-linger 'u'", "", "sudo: a password is required", true)

	client := lingerTestClient(t, srv)
	err := EnsureLinger(context.Background(), client, "u")
	if err == nil {
		t.Fatal("expected LingerNotEnabledError")
	}
	var lingerErr *LingerNotEnabledError
	if !errors.As(err, &lingerErr) {
		t.Fatalf("want LingerNotEnabledError, got %T: %v", err, err)
	}
	if lingerErr.User != "u" {
		t.Fatalf("user=%q", lingerErr.User)
	}
	want := "sudo loginctl enable-linger u"
	if lingerErr.Command != want {
		t.Fatalf("command=%q want %q", lingerErr.Command, want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("message must name the operator command: %v", err)
	}
}

// TestEnsureLinger_AlreadyEnabled proves the check is a no-op on a host
// an operator already configured: no privileged command is attempted.
func TestEnsureLinger_AlreadyEnabled(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=yes\n", "", false)

	client := lingerTestClient(t, srv)
	if err := EnsureLinger(context.Background(), client, "u"); err != nil {
		t.Fatalf("EnsureLinger: %v", err)
	}
	for _, cmd := range srv.ranCommands() {
		if strings.Contains(cmd, "enable-linger") {
			t.Fatalf("enable-linger must not run when lingering is on; ran=%v", srv.ranCommands())
		}
	}
}

// TestEnsureLinger_UnreadableStateEnables covers a loginctl that cannot
// report the property (missing user object, unexpected output): the
// deploy must not read silence as "lingering is on".
func TestEnsureLinger_UnreadableStateEnables(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("loginctl show-user 'u' -p Linger", "", "Failed to get user: No such file", true)
	srv.setExec("sudo -n loginctl enable-linger 'u'", "", "", false)

	client := lingerTestClient(t, srv)
	if err := EnsureLinger(context.Background(), client, "u"); err != nil {
		t.Fatalf("EnsureLinger: %v", err)
	}
	if !srv.executed("sudo -n loginctl enable-linger 'u'") {
		t.Fatalf("enable-linger was not executed; ran=%v", srv.ranCommands())
	}
}

func TestEnsureLinger_NilClient(t *testing.T) {
	err := EnsureLinger(context.Background(), nil, "u")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
	var lingerErr *LingerNotEnabledError
	if !errors.As(err, &lingerErr) {
		t.Fatalf("want LingerNotEnabledError, got %T: %v", err, err)
	}
}

func TestEnsureLinger_EmptyUser(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	client := lingerTestClient(t, srv)

	err := EnsureLinger(context.Background(), client, "   ")
	var specErr *InvalidSpecError
	if !errors.As(err, &specErr) {
		t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
	}
	if specErr.Field != "ssh_user" {
		t.Fatalf("field=%q", specErr.Field)
	}
}

func TestParseLingerProperty(t *testing.T) {
	cases := map[string]string{
		"Linger=yes\n":               "yes",
		"Linger=no\n":                "no",
		"  Linger=YES  ":             "yes",
		"Name=u\nLinger=no\nState=x": "no",
		"":                           "",
		"Linger":                     "",
		"State=active\n":             "",
	}
	for out, want := range cases {
		if got := parseLingerProperty(out); got != want {
			t.Fatalf("parseLingerProperty(%q)=%q want %q", out, got, want)
		}
	}
}

// TestInstallAndStart_EnablesLingerBeforeStart pins the ordering that
// makes the fix hold: the unit is never started until the user manager
// is known to survive the deploy's session.
func TestInstallAndStart_EnablesLingerBeforeStart(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	srv.setExec("sudo -n loginctl enable-linger 'u'", "", "", false)

	client := lingerTestClient(t, srv)
	if err := InstallAndStart(context.Background(), client, "[Unit]\n", DefaultUnitName); err != nil {
		t.Fatalf("InstallAndStart: %v", err)
	}

	ran := srv.ranCommands()
	enableAt, startAt := -1, -1
	for i, cmd := range ran {
		switch {
		case strings.Contains(cmd, "enable-linger"):
			enableAt = i
		case strings.Contains(cmd, "--user restart"):
			startAt = i
		}
	}
	if enableAt < 0 || startAt < 0 {
		t.Fatalf("missing commands: enable-linger=%d restart=%d ran=%v", enableAt, startAt, ran)
	}
	if enableAt > startAt {
		t.Fatalf("lingering must be enabled before the unit starts; ran=%v", ran)
	}
}

// TestInstallAndStart_LingerFailureStopsBeforeStart proves the deploy
// aborts instead of starting an agent that would be killed with the
// session — the state that made the fleet registry lie on the remote fleet node.
func TestInstallAndStart_LingerFailureStopsBeforeStart(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	srv.setExec("sudo -n loginctl enable-linger 'u'", "", "sudo: a password is required", true)

	client := lingerTestClient(t, srv)
	err := InstallAndStart(context.Background(), client, "[Unit]\n", DefaultUnitName)
	var lingerErr *LingerNotEnabledError
	if !errors.As(err, &lingerErr) {
		t.Fatalf("want LingerNotEnabledError, got %T: %v", err, err)
	}
	for _, cmd := range srv.ranCommands() {
		if strings.Contains(cmd, "--user restart") {
			t.Fatalf("unit must not start when lingering cannot be enabled; ran=%v", srv.ranCommands())
		}
	}
}

func TestSSHClient_User(t *testing.T) {
	srv := newTestSSHServer(t, "u", "p")
	client := lingerTestClient(t, srv)
	if got := client.User(); got != "u" {
		t.Fatalf("User()=%q want %q", got, "u")
	}
	var nilClient *SSHClient
	if got := nilClient.User(); got != "" {
		t.Fatalf("nil client User()=%q want empty", got)
	}
}
