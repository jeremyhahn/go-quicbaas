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
	"strings"
)

// A systemd --user manager is owned by a login session unless the user
// lingers: with Linger=no, logind stops the manager — and every unit it
// supervises — the moment the last session of that user closes. The
// deploy's own SSH session is such a session, so a unit installed and
// started over it reports "active" for exactly as long as the deploy is
// connected and is killed on disconnect. Enabling lingering is what
// makes the manager outlive the session, and it is the one thing a
// --user install cannot be correct without.
//
// See https://www.freedesktop.org/software/systemd/man/loginctl.html
// (enable-linger) for the property this code reads and sets.

// EnsureLinger guarantees the remote user's systemd manager survives the
// deploy's SSH session. It reads the Linger property with loginctl and,
// when lingering is off, enables it over sudo -n.
//
// Enabling lingering needs root. When sudo is unavailable or would
// prompt, the call fails with a *LingerNotEnabledError naming the exact
// command the operator must run — a deploy that cannot make the agent
// outlive its own session must fail rather than report a healthy unit.
//
// An unreadable Linger property is treated as "off": loginctl declining
// to answer is not evidence that the manager lingers, and running
// enable-linger a second time is harmless.
func EnsureLinger(ctx context.Context, sshClient *SSHClient, user string) error {
	user = strings.TrimSpace(user)
	if sshClient == nil {
		return &LingerNotEnabledError{User: user, Cause: errClosed}
	}
	if user == "" {
		return &InvalidSpecError{
			Field:  "ssh_user",
			Reason: "remote user is required to verify systemd lingering",
		}
	}

	if lingerEnabled(ctx, sshClient, user) {
		return nil
	}

	if _, _, err := sshClient.Exec(ctx, "sudo -n "+lingerEnableCommand(user)); err != nil {
		return &LingerNotEnabledError{
			User:    user,
			Command: "sudo loginctl enable-linger " + user,
			Cause:   err,
		}
	}
	return nil
}

// lingerEnabled reports whether loginctl says the user already lingers.
// A failed probe or an unexpected reply reports false; see EnsureLinger
// for why that is the safe direction.
func lingerEnabled(ctx context.Context, sshClient *SSHClient, user string) bool {
	stdout, _, err := sshClient.Exec(ctx, "loginctl show-user "+shellQuote(user)+" -p Linger")
	if err != nil {
		return false
	}
	return parseLingerProperty(stdout) == "yes"
}

// parseLingerProperty extracts the value of the Linger property from
// `loginctl show-user -p Linger` output ("Linger=no"). It returns the
// lowercased value, or "" when the property is absent.
func parseLingerProperty(out string) string {
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "Linger" {
			continue
		}
		return strings.ToLower(strings.TrimSpace(value))
	}
	return ""
}

// lingerEnableCommand renders the loginctl invocation that enables
// lingering for user, shell-quoted because the remote shell executes it.
// The operator-facing copy in LingerNotEnabledError.Command is built
// from the bare name instead, so it can be pasted as-is.
func lingerEnableCommand(user string) string {
	return "loginctl enable-linger " + shellQuote(user)
}
