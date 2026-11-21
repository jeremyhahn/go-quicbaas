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
	// NodeCertFilename is the basename of the daemon's mTLS client
	// certificate inside DataDir.
	NodeCertFilename = "agent.crt"

	// NodeKeyFilename is the basename of the daemon's mTLS client
	// private key inside DataDir. Installed with 0600 permissions.
	NodeKeyFilename = "agent.key"

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
// enrollment classifier in pkg/orchestration/fleet), so a process that still exits is
// reporting something restarting cannot fix. Escalating the delay and
// capping the start rate turns that into a unit that lands in "failed",
// where monitoring can see it, instead of one that sits in
// "activating (auto-restart)" forever and reads as almost healthy.
func RenderUnit(spec *Spec) string {
	// systemd --user unit files use %h for the user's home directory.
	args := fleetServeArgs(spec, "%h")
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
	// SIGTERM to the daemon alone, so it drains before the `docker run`
	// clients behind its live workers are signalled; the default
	// (control-group) closed a worker's stream mid-prompt on every restart
	// (go-quicbaas#1064). Its leftovers are SIGKILLed after it exits and
	// swept on the next start.
	b.WriteString("KillMode=mixed\n")
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
// user daemon, enables the unit so it starts at next login, ensures the
// remote user's manager lingers, and starts the unit immediately.
//
// The linger step is not optional housekeeping: without it the user
// manager, and the agent with it, dies when this SSH session closes.
// A host where it cannot be enabled fails the install with a
// *LingerNotEnabledError instead of reporting a started agent.
//
// The caller must have verified that systemctl --user is available
// on the remote (see DetectSystemdUser).
func InstallAndStart(ctx context.Context, target Target, unitContent, unitName string) error {
	if target == nil {
		return &SystemdInstallError{Unit: unitName, Stage: "ssh", Cause: errClosed}
	}
	if unitName == "" {
		return &InvalidSpecError{Field: "unit_name", Reason: "unit name is required"}
	}

	unitDir := "$HOME/.config/systemd/user"
	unitPath := unitDir + "/" + unitName

	// Ensure the user systemd directory exists. shellQuotePath lets
	// the remote shell expand $HOME while keeping the remainder literal.
	if _, _, err := target.Exec(ctx, "mkdir -p "+shellQuotePath(unitDir)); err != nil {
		return &SystemdInstallError{Unit: unitName, Stage: "mkdir", Cause: err}
	}

	// Write the unit file. The remote path is quoted so odd $HOME
	// expansions never escape a shell context.
	if err := target.WriteFile(ctx, unitPath, []byte(unitContent), 0o644); err != nil {
		return &SystemdInstallError{Unit: unitName, Stage: "write", Cause: err}
	}

	// Reload + enable.
	for _, cmd := range []string{
		"systemctl --user daemon-reload",
		"systemctl --user enable " + shellQuote(unitName),
	} {
		if _, _, err := target.Exec(ctx, cmd); err != nil {
			return &SystemdInstallError{Unit: unitName, Stage: stageFor(cmd), Cause: err}
		}
	}

	// Lingering, before the unit is started. A --user manager that does
	// not linger is torn down with the last session of its user — this
	// very SSH connection — so starting the agent first would only
	// produce one that enrolls, heartbeats, and is killed on disconnect.
	if err := EnsureLinger(ctx, target, target.User()); err != nil {
		return err
	}

	start := "systemctl --user restart " + shellQuote(unitName)
	if _, _, err := target.Exec(ctx, start); err != nil {
		return &SystemdInstallError{Unit: unitName, Stage: stageFor(start), Cause: err}
	}
	return nil
}

// DetectSystemdUser returns true when the remote host has a working
// user-scoped systemd. It does the minimal probe — asking systemctl
// for its version — so the detect step takes milliseconds.
func DetectSystemdUser(ctx context.Context, target Target) bool {
	if target == nil {
		return false
	}
	_, _, err := target.Exec(ctx, "systemctl --user --version >/dev/null 2>&1")
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

// fleetServeArgs builds the argv slice the installed daemon runs with. The
// slice is shared by the systemd and nohup rendering paths so both
// invocations stay in sync.
//
// Every daemon setting lives in the config file deploy renders next to the
// binary (RenderNodeConfig), so the command line names only that file: one
// schema, fleet.Config, carries every setting, including the capacity, mount
// roots, backend URLs and image map a flag list could not.
//
// homePrefix is the expression each relative path is joined under: the
// systemd unit renderer passes "%h" (systemd-supplied home expansion), the
// nohup script renderer passes "$HOME" (shell-supplied expansion, which
// shellQuotePath leaves outside the quotes so the shell still expands it).
func fleetServeArgs(spec *Spec, homePrefix string) []string {
	return []string{
		"fleet", "serve",
		"--config", shellQuotePath(spec.Layout(homePrefix).ConfigFile),
	}
}
