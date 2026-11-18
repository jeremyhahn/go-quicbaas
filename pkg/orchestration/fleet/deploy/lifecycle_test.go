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

package deploy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"
)

func lifecycleLayout() Layout {
	return (&Spec{ProjectsRoot: "/srv/projects"}).Layout("/home/u")
}

// installAs makes the fixture look like a node deployed under supervisor.
func installAs(srv *testSSHServer, l Layout, supervisor Supervisor) {
	srv.setExec(fileExistsCommand(l.UnitFile), "", "", supervisor != SupervisorSystemd)
	srv.setExec(fileExistsCommand(l.StartScript), "", "", supervisor != SupervisorNohup)
}

func TestInstalledSupervisor(t *testing.T) {
	l := lifecycleLayout()
	for _, want := range []Supervisor{SupervisorSystemd, SupervisorNohup, SupervisorNone} {
		srv := newTestSSHServer(t, "u", "p")
		installAs(srv, l, want)
		if got := InstalledSupervisor(context.Background(), lingerTestClient(t, srv), l); got != want {
			t.Fatalf("InstalledSupervisor = %q, want %q", got, want)
		}
	}
}

func TestStopAgent(t *testing.T) {
	l := lifecycleLayout()

	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorSystemd)
	if got, err := StopNode(context.Background(), lingerTestClient(t, srv), l); err != nil || got != SupervisorSystemd {
		t.Fatalf("systemd stop: %q %v", got, err)
	}
	if !srv.executed("systemctl --user stop 'quicbaas-agent.service'") {
		t.Fatalf("unit not stopped: %v", srv.ranCommands())
	}

	srv = newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorNohup)
	if got, err := StopNode(context.Background(), lingerTestClient(t, srv), l); err != nil || got != SupervisorNohup {
		t.Fatalf("nohup stop: %q %v", got, err)
	}
	if !srv.executed(stopNohupCommand(l)) {
		t.Fatalf("nohup process not stopped: %v", srv.ranCommands())
	}

	srv = newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorNone)
	var notDeployed *NotDeployedError
	if _, err := StopNode(context.Background(), lingerTestClient(t, srv), l); !errors.As(err, &notDeployed) {
		t.Fatalf("stop on an empty node: want NotDeployedError, got %v", err)
	}

	srv = newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorSystemd)
	srv.setExec("systemctl --user stop 'quicbaas-agent.service'", "", "Failed to stop", true)
	var execErr *RemoteExecError
	if _, err := StopNode(context.Background(), lingerTestClient(t, srv), l); !errors.As(err, &execErr) {
		t.Fatalf("failed stop: want RemoteExecError, got %v", err)
	}
}

// TestStartAgent_SystemdEnsuresLinger: a start is a start over this SSH
// session, so the same linger rule as the install applies — without it the
// agent would die when `fleet start` disconnects.
func TestStartAgent_SystemdEnsuresLinger(t *testing.T) {
	l := lifecycleLayout()
	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorSystemd)
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=yes\n", "", false)
	if got, err := StartNode(context.Background(), lingerTestClient(t, srv), l); err != nil || got != SupervisorSystemd {
		t.Fatalf("start: %q %v", got, err)
	}
	ran := srv.ranCommands()
	linger := slices.Index(ran, "loginctl show-user 'u' -p Linger")
	start := slices.Index(ran, "systemctl --user start 'quicbaas-agent.service'")
	if linger < 0 || start < 0 || linger > start {
		t.Fatalf("linger must be checked before the unit starts: %v", ran)
	}

	srv = newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorSystemd)
	srv.setExec("loginctl show-user 'u' -p Linger", "Linger=no\n", "", false)
	srv.setExec("sudo -n loginctl enable-linger 'u'", "", "password required", true)
	var linger2 *LingerNotEnabledError
	if _, err := StartNode(context.Background(), lingerTestClient(t, srv), l); !errors.As(err, &linger2) {
		t.Fatalf("want LingerNotEnabledError, got %v", err)
	}
	if srv.executed("systemctl --user start 'quicbaas-agent.service'") {
		t.Fatal("unit started without lingering")
	}
}

func TestStartAgent_NohupAndNothing(t *testing.T) {
	l := lifecycleLayout()
	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorNohup)
	if got, err := StartNode(context.Background(), lingerTestClient(t, srv), l); err != nil || got != SupervisorNohup {
		t.Fatalf("nohup start: %q %v", got, err)
	}
	if !srv.executed("sh " + shellQuotePath(l.StartScript)) {
		t.Fatalf("start script not run: %v", srv.ranCommands())
	}

	srv = newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorNone)
	var notDeployed *NotDeployedError
	if _, err := StartNode(context.Background(), lingerTestClient(t, srv), l); !errors.As(err, &notDeployed) {
		t.Fatalf("want NotDeployedError, got %v", err)
	}
	if !strings.Contains(notDeployed.Error(), l.UnitFile) {
		t.Fatalf("error does not name what it looked for: %v", notDeployed)
	}
}

// TestUninstallAgent_KeepsWorkspaces: undeploy removes the install and the
// identity, disables the unit, and leaves the checkouts alone.
func TestUninstallAgent_KeepsWorkspaces(t *testing.T) {
	l := lifecycleLayout()
	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorSystemd)

	result, err := UninstallNode(context.Background(), lingerTestClient(t, srv), l, false)
	if err != nil {
		t.Fatalf("UninstallNode: %v", err)
	}
	ran := strings.Join(srv.ranCommands(), "\n")
	for _, want := range []string{
		"systemctl --user disable --now 'quicbaas-agent.service'",
		"rm -f '/home/u/.config/systemd/user/quicbaas-agent.service'",
		"systemctl --user daemon-reload",
		"'/home/u/.quicbaas/bin/quicbaas'",
		"'/home/u/.quicbaas/bin/agent.yaml'",
		"'/home/u/.quicbaas/agent/agent.key'",
		"'/home/u/.quicbaas/agent/work/credentials.json'",
	} {
		if !strings.Contains(ran, want) {
			t.Fatalf("undeploy did not run %q:\n%s", want, ran)
		}
	}
	if strings.Contains(ran, "rm -rf") || slices.Contains(result.Removed, l.ProjectsRoot) {
		t.Fatalf("workspaces removed without --purge-workspaces:\n%s", ran)
	}
	if result.Supervisor != SupervisorSystemd {
		t.Fatalf("supervisor = %q", result.Supervisor)
	}
}

func TestUninstallAgent_PurgeWorkspaces(t *testing.T) {
	l := lifecycleLayout()
	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorNohup)

	result, err := UninstallNode(context.Background(), lingerTestClient(t, srv), l, true)
	if err != nil {
		t.Fatalf("UninstallNode: %v", err)
	}
	if !srv.executed(stopNohupCommand(l)) {
		t.Fatal("nohup agent not stopped before removal")
	}
	if !srv.executed("rm -rf '/srv/projects' '/home/u/.quicbaas/agent/work'") {
		t.Fatalf("workspaces not purged: %v", srv.ranCommands())
	}
	if !slices.Contains(result.Removed, l.ProjectsRoot) {
		t.Fatalf("result does not list the purged projects root: %v", result.Removed)
	}
}

func TestUninstallAgent_AlreadyGoneSucceeds(t *testing.T) {
	l := lifecycleLayout()
	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorNone)
	result, err := UninstallNode(context.Background(), lingerTestClient(t, srv), l, false)
	if err != nil || result.Supervisor != SupervisorNone {
		t.Fatalf("undeploy of an empty node: %+v %v", result, err)
	}
}

func TestUninstallAgent_StopsOnFailure(t *testing.T) {
	l := lifecycleLayout()
	srv := newTestSSHServer(t, "u", "p")
	installAs(srv, l, SupervisorSystemd)
	srv.setExec("systemctl --user disable --now 'quicbaas-agent.service'", "", "boom", true)
	if _, err := UninstallNode(context.Background(), lingerTestClient(t, srv), l, false); err == nil {
		t.Fatal("a failed disable must fail the undeploy")
	}
	if srv.executed("rm -f '/home/u/.config/systemd/user/quicbaas-agent.service'") {
		t.Fatal("unit file removed although the unit could not be disabled")
	}
}

func TestReadAgentCertificate(t *testing.T) {
	l := lifecycleLayout()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: "agent-agent-01"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	srv := newTestSSHServer(t, "u", "p")
	srv.setExec("cat "+shellQuotePath(l.CertFile), certPEM, "", false)
	cert, err := ReadNodeCertificate(context.Background(), lingerTestClient(t, srv), l)
	if err != nil || cert.Subject.CommonName != "agent-agent-01" {
		t.Fatalf("ReadNodeCertificate: %v %v", cert, err)
	}

	var missing *AgentIdentityMissingError
	for name, setup := range map[string]func(*testSSHServer){
		"absent":  func(s *testSSHServer) { s.setExec("cat "+shellQuotePath(l.CertFile), "", "No such file", true) },
		"not pem": func(s *testSSHServer) { s.setExec("cat "+shellQuotePath(l.CertFile), "junk", "", false) },
		"bad cert": func(s *testSSHServer) {
			s.setExec("cat "+shellQuotePath(l.CertFile), "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n", "", false)
		},
	} {
		srv := newTestSSHServer(t, "u", "p")
		setup(srv)
		if _, err := ReadNodeCertificate(context.Background(), lingerTestClient(t, srv), l); !errors.As(err, &missing) {
			t.Fatalf("%s: want AgentIdentityMissingError, got %v", name, err)
		}
	}
}

func TestDialTCP_ClosedClient(t *testing.T) {
	var c *SSHClient
	var dialErr *SSHDialError
	if _, err := c.DialTCP(context.Background(), "x:1"); !errors.As(err, &dialErr) {
		t.Fatalf("want SSHDialError, got %v", err)
	}
}

// TestUninstallAgent_PurgeRefusesDangerousRoots: a projects_root typo must
// not become `rm -rf` of the root or the home directory.
func TestUninstallAgent_PurgeRefusesDangerousRoots(t *testing.T) {
	for _, root := range []string{"/", "/home/u", "/home", "/home/u/"} {
		l := (&Spec{ProjectsRoot: root}).Layout("/home/u")
		srv := newTestSSHServer(t, "u", "p")
		installAs(srv, l, SupervisorNone)
		var refused *PurgeRefusedError
		if _, err := UninstallNode(context.Background(), lingerTestClient(t, srv), l, true); !errors.As(err, &refused) {
			t.Fatalf("projects_root %q: want PurgeRefusedError, got %v", root, err)
		}
		for _, cmd := range srv.ranCommands() {
			if strings.HasPrefix(cmd, "rm -rf") {
				t.Fatalf("projects_root %q: ran %q", root, cmd)
			}
		}
	}
	if err := checkPurgeTarget("relative/dir", "/home/u"); err == nil {
		t.Fatal("relative purge target accepted")
	}
	if err := checkPurgeTarget("/home/u/.quicbaas/agent/work", "/home/u"); err != nil {
		t.Fatalf("work dir refused: %v", err)
	}
}
