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
	"crypto/x509"
	"encoding/pem"
	"path"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet"
)

// The lifecycle primitives stop, start and remove an agent a deploy
// installed. They find out how it was installed from the host itself — a unit
// file means systemd, a start script means nohup — rather than trusting the
// caller's spec, so `fleet stop` stops what is actually running even when it
// was deployed with other flags.
//
// Deregistering from the fleet registry is the service layer's half: these
// primitives only touch the node.

// Supervisor names how an installed agent is run.
type Supervisor string

const (
	// SupervisorSystemd is a systemd --user unit.
	SupervisorSystemd Supervisor = "systemd"
	// SupervisorNohup is the nohup start script.
	SupervisorNohup Supervisor = "nohup"
	// SupervisorNone means nothing is installed.
	SupervisorNone Supervisor = ""
)

func fileExistsCommand(p string) string {
	return "test -f " + shellQuotePath(p)
}

// InstalledSupervisor reports how the agent under l is installed, or
// SupervisorNone when neither a unit nor a start script is there.
func InstalledSupervisor(ctx context.Context, c *SSHClient, l Layout) Supervisor {
	if _, _, err := c.Exec(ctx, fileExistsCommand(l.UnitFile)); err == nil {
		return SupervisorSystemd
	}
	if _, _, err := c.Exec(ctx, fileExistsCommand(l.StartScript)); err == nil {
		return SupervisorNohup
	}
	return SupervisorNone
}

func unitCommand(verb string) string {
	return "systemctl --user " + verb + " " + shellQuote(DefaultUnitName)
}

// stopNohupCommand ends the process the start script recorded and removes
// its pid file. A pid file naming a process that is already gone is not an
// error: the agent is stopped either way.
func stopNohupCommand(l Layout) string {
	pid := shellQuotePath(l.PIDFile)
	return "if [ -f " + pid + " ]; then kill \"$(cat " + pid + ")\" 2>/dev/null || true; rm -f " + pid + "; fi"
}

// StopNode stops the installed agent and reports how it was supervised. A
// node with nothing installed is a *NotDeployedError.
func StopNode(ctx context.Context, c *SSHClient, l Layout) (Supervisor, error) {
	switch supervisor := InstalledSupervisor(ctx, c, l); supervisor {
	case SupervisorSystemd:
		_, _, err := c.Exec(ctx, unitCommand("stop"))
		return supervisor, err
	case SupervisorNohup:
		_, _, err := c.Exec(ctx, stopNohupCommand(l))
		return supervisor, err
	default:
		return SupervisorNone, &NotDeployedError{Host: c.Host(), Layout: l}
	}
}

// StartNode starts the installed agent and reports how it is supervised. A
// systemd start needs lingering exactly as the install did, so it is ensured
// again first: an operator may have turned it off since.
func StartNode(ctx context.Context, c *SSHClient, l Layout) (Supervisor, error) {
	switch supervisor := InstalledSupervisor(ctx, c, l); supervisor {
	case SupervisorSystemd:
		if err := EnsureLinger(ctx, c, c.User()); err != nil {
			return supervisor, err
		}
		_, _, err := c.Exec(ctx, unitCommand("start"))
		return supervisor, err
	case SupervisorNohup:
		_, _, err := c.Exec(ctx, "sh "+shellQuotePath(l.StartScript))
		return supervisor, err
	default:
		return SupervisorNone, &NotDeployedError{Host: c.Host(), Layout: l}
	}
}

// UninstallResult lists what UninstallNode removed.
type UninstallResult struct {
	Supervisor Supervisor
	Removed    []string
}

// UninstallNode stops and disables the agent, then removes everything the
// deploy installed: the unit, the config file, the binary, the start script,
// the mTLS identity, the daemon's persisted credentials, and its log and pid
// files. Workspaces — the projects root and the rest of the work directory —
// are kept unless purgeWorkspaces is set, because they hold checkouts a job
// may still need. Removing an agent that is already gone succeeds.
func UninstallNode(ctx context.Context, c *SSHClient, l Layout, purgeWorkspaces bool) (*UninstallResult, error) {
	result := &UninstallResult{Supervisor: InstalledSupervisor(ctx, c, l)}

	switch result.Supervisor {
	case SupervisorSystemd:
		for _, cmd := range []string{unitCommand("disable --now"), "rm -f " + shellQuotePath(l.UnitFile), "systemctl --user daemon-reload"} {
			if _, _, err := c.Exec(ctx, cmd); err != nil {
				return result, err
			}
		}
		result.Removed = append(result.Removed, l.UnitFile)
	case SupervisorNohup:
		if _, _, err := c.Exec(ctx, stopNohupCommand(l)); err != nil {
			return result, err
		}
	}

	files := []string{
		l.Binary, l.ConfigFile, l.StartScript,
		l.CertFile, l.KeyFile, l.ServerCAFile,
		path.Join(l.WorkDir, fleet.CredentialsFileName),
		l.LogFile, l.PIDFile,
	}
	if _, _, err := c.Exec(ctx, "rm -f "+quotePaths(files)); err != nil {
		return result, err
	}
	result.Removed = append(result.Removed, files...)

	if purgeWorkspaces {
		dirs := []string{l.ProjectsRoot, l.WorkDir}
		for _, dir := range dirs {
			if err := checkPurgeTarget(dir, l.Home); err != nil {
				return result, err
			}
		}
		if _, _, err := c.Exec(ctx, "rm -rf "+quotePaths(dirs)); err != nil {
			return result, err
		}
		result.Removed = append(result.Removed, dirs...)
	}

	// The deploy's own directories go too, but only if nothing else is left
	// in them: rmdir refuses a non-empty directory, which is the point.
	if _, _, err := c.Exec(ctx, "rmdir "+quotePaths([]string{l.InstallDir, l.DataDir})+" 2>/dev/null || true"); err != nil {
		return result, err
	}
	return result, nil
}

// checkPurgeTarget refuses to recursively delete a directory that is the
// filesystem root, the user's home, or an ancestor of it. projects_root is
// operator-supplied, and a typo there must not turn --purge-workspaces into
// `rm -rf /`.
func checkPurgeTarget(dir, home string) error {
	dir = path.Clean(dir)
	home = path.Clean(home)
	if !path.IsAbs(dir) || dir == "/" || dir == home || strings.HasPrefix(home+"/", dir+"/") {
		return &PurgeRefusedError{Path: dir}
	}
	return nil
}

func quotePaths(paths []string) string {
	quoted := make([]string, 0, len(paths))
	for _, p := range paths {
		quoted = append(quoted, shellQuotePath(p))
	}
	return strings.Join(quoted, " ")
}

// ReadNodeCertificate reads the mTLS certificate the deploy installed. The
// fleet registry keys an agent by an ID derived from this certificate's
// Common Name, so this is how stop and undeploy find the registration to
// remove without trusting the caller to name it.
func ReadNodeCertificate(ctx context.Context, c *SSHClient, l Layout) (*x509.Certificate, error) {
	stdout, _, err := c.Exec(ctx, "cat "+shellQuotePath(l.CertFile))
	if err != nil {
		return nil, &AgentIdentityMissingError{Path: l.CertFile, Cause: err}
	}
	block, _ := pem.Decode([]byte(stdout))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, &AgentIdentityMissingError{Path: l.CertFile, Cause: &stringError{msg: "no PEM certificate"}}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, &AgentIdentityMissingError{Path: l.CertFile, Cause: err}
	}
	return cert, nil
}
