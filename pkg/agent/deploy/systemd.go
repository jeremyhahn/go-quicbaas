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
	"path"
	"strconv"
	"strings"
)

// DefaultUnitName is the systemd unit file installed by the deploy
// primitives. It lives under ~/.config/systemd/user/ on the remote
// host so a system-level install is never required.
const DefaultUnitName = "quicbaas-agent.service"

// Agent identity material filenames. The deploy flow uploads the mTLS
// client certificate, private key, and server CA bundle into DataDir on
// the remote host; the rendered systemd unit / nohup script references
// these paths on the daemon command line.
const (
	// AgentCertFilename is the basename of the daemon's mTLS client
	// certificate inside DataDir.
	AgentCertFilename = "agent.crt"

	// AgentKeyFilename is the basename of the daemon's mTLS client
	// private key inside DataDir. Installed with 0600 permissions.
	AgentKeyFilename = "agent.key"

	// ServerCAFilename is the basename of the PEM-encoded CA bundle the
	// daemon trusts when dialing the QuicBaaS server.
	ServerCAFilename = "server-ca.crt"
)

// RenderUnit produces the systemd --user unit content for the given
// spec. The unit is intentionally minimal: a single ExecStart line and
// a bounded Restart=on-failure policy. Keeping the body small makes it
// easy for operators to audit after the deploy finishes.
//
// The restart policy is deliberately not a flat retry. The daemon itself
// waits out a control plane that is merely unreachable (see the
// enrollment classifier in pkg/agent), so a process that still exits is
// reporting something restarting cannot fix. Escalating the delay and
// capping the start rate turns that into a unit that lands in "failed",
// where monitoring can see it, instead of one that sits in
// "activating (auto-restart)" forever and reads as almost healthy.
func RenderUnit(spec *Spec) string {
	// systemd --user unit files use %h for the user's home directory.
	args := agentServeArgs(spec, "%h")
	bin := path.Join("%h", spec.ResolvedInstallDir(), "quicbaas")
	exec := bin + " " + strings.Join(args, " ")

	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=QuicBaaS Agent Runtime\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n")
	// StartLimit* are [Unit] properties; systemd ignores them in
	// [Service], so placing them here is what actually bounds the loop.
	b.WriteString("StartLimitIntervalSec=")
	b.WriteString(strconv.Itoa(startLimitIntervalSeconds))
	b.WriteString("\n")
	b.WriteString("StartLimitBurst=")
	b.WriteString(strconv.Itoa(startLimitBurst))
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("ExecStart=")
	b.WriteString(exec)
	b.WriteString("\n")
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=")
	b.WriteString(strconv.Itoa(restartBaseSeconds))
	b.WriteString("\n")
	// RestartSteps / RestartMaxDelaySec need systemd >= 254. Older
	// versions log "Unknown key name" and fall back to the flat
	// RestartSec above, which the StartLimit* pair still bounds.
	b.WriteString("RestartSteps=")
	b.WriteString(strconv.Itoa(restartSteps))
	b.WriteString("\n")
	b.WriteString("RestartMaxDelaySec=")
	b.WriteString(strconv.Itoa(restartMaxDelaySeconds))
	b.WriteString("\n")
	b.WriteString("WorkingDirectory=%h/")
	b.WriteString(spec.ResolvedDataDir())
	b.WriteString("\n")
	b.WriteString("\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

// Restart policy constants for the rendered unit.
//
// The schedule escalates from 5s to 120s over 5 steps, and the start-rate
// limit gives up after 5 starts inside 5 minutes — roughly a minute and a
// half of escalating retries. That is long enough to ride out a host-local
// hiccup and short enough that a genuinely misconfigured agent shows up as
// a failed unit while the operator is still watching the deploy.
const (
	restartBaseSeconds        = 5
	restartSteps              = 5
	restartMaxDelaySeconds    = 120
	startLimitIntervalSeconds = 300
	startLimitBurst           = 5
)

// InstallAndStart writes the rendered unit content to the remote
// host under ~/.config/systemd/user/<unitName>, then reloads the
// user daemon, enables the unit so it starts at next login, and
// starts it immediately.
//
// The caller must have verified that systemctl --user is available
// on the remote (see DetectSystemdUser).
func InstallAndStart(ctx context.Context, sshClient *SSHClient, unitContent, unitName string) error {
	if sshClient == nil {
		return &SystemdInstallError{Unit: unitName, Stage: "ssh", Cause: errClosed}
	}
	if unitName == "" {
		return &InvalidSpecError{Field: "unit_name", Reason: "unit name is required"}
	}

	unitDir := "$HOME/.config/systemd/user"
	unitPath := unitDir + "/" + unitName

	// Ensure the user systemd directory exists. shellQuotePath lets
	// the remote shell expand $HOME while keeping the remainder literal.
	if _, _, err := sshClient.Exec(ctx, "mkdir -p "+shellQuotePath(unitDir)); err != nil {
		return &SystemdInstallError{Unit: unitName, Stage: "mkdir", Cause: err}
	}

	// Write the unit file. The remote path is quoted so odd $HOME
	// expansions never escape a shell context.
	if err := sshClient.WriteFile(ctx, unitPath, []byte(unitContent), 0o644); err != nil {
		return &SystemdInstallError{Unit: unitName, Stage: "write", Cause: err}
	}

	// Reload + enable + start.
	cmds := []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable " + shellQuote(unitName),
		"systemctl --user restart " + shellQuote(unitName),
	}
	for _, cmd := range cmds {
		if _, _, err := sshClient.Exec(ctx, cmd); err != nil {
			return &SystemdInstallError{Unit: unitName, Stage: stageFor(cmd), Cause: err}
		}
	}
	return nil
}

// DetectSystemdUser returns true when the remote host has a working
// user-scoped systemd. It does the minimal probe — asking systemctl
// for its version — so the detect step takes milliseconds.
func DetectSystemdUser(ctx context.Context, sshClient *SSHClient) bool {
	if sshClient == nil {
		return false
	}
	_, _, err := sshClient.Exec(ctx, "systemctl --user --version >/dev/null 2>&1")
	return err == nil
}

// stageFor classifies the command into a short stage label used when
// building SystemdInstallError.
func stageFor(cmd string) string {
	switch {
	case strings.Contains(cmd, "daemon-reload"):
		return "daemon-reload"
	case strings.Contains(cmd, "enable"):
		return "enable"
	case strings.Contains(cmd, "restart"), strings.Contains(cmd, "start"):
		return "start"
	default:
		return "exec"
	}
}

// agentServeArgs builds the argv slice the installed daemon should run
// with. The slice is shared by the systemd and nohup rendering paths
// so both invocations stay in sync. Each argument is shell-quoted
// because the resulting string is embedded directly into a script or
// unit ExecStart line.
//
// homePrefix is the expression each relative path is joined under: the
// systemd unit renderer passes "%h" (systemd-supplied home expansion),
// the nohup script renderer passes "$HOME" (shell-supplied expansion).
//
// The daemon authenticates to the server over mTLS using the agent
// certificate, private key, and server CA bundle that the deploy flow
// uploaded into DataDir.
func agentServeArgs(spec *Spec, homePrefix string) []string {
	dataDir := path.Join(homePrefix, spec.ResolvedDataDir())
	certPath := path.Join(dataDir, AgentCertFilename)
	keyPath := path.Join(dataDir, AgentKeyFilename)
	caPath := path.Join(dataDir, ServerCAFilename)

	// workDir hosts the agent's own runtime state (session workspaces,
	// scratch, socket dirs). We pin it under dataDir so the --user
	// systemd instance never tries to mkdir /var/lib/quicbaas-agent,
	// which requires root.
	workDir := path.Join(dataDir, "work")

	args := []string{
		"agent", "serve",
		"--server", shellQuote(spec.Server),
		"--tenant", shellQuote(spec.Tenant),
		"--bind", shellQuote(spec.AgentBind),
		"--backends", shellQuote(strings.Join(spec.Backends, ",")),
		"--agent-cert", shellQuote(certPath),
		"--agent-key", shellQuote(keyPath),
		"--server-ca", shellQuote(caPath),
		"--work-dir", shellQuote(workDir),
	}

	// The MCP endpoint is optional: only the control plane knows the MCP
	// listener address, and it is omitted entirely when unconfigured so the
	// daemon command line is unchanged for non-MCP deployments.
	if endpoint := strings.TrimSpace(spec.MCPEndpoint); endpoint != "" {
		args = append(args, "--mcp-endpoint", shellQuote(endpoint))
	}
	return args
}
