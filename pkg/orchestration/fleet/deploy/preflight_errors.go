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
	"errors"
	"strconv"
	"strings"
)

// Every preflight failure is a typed error that carries the exact fix. The
// fix is the point: the remote fleet node's failures were found one deploy at a time, and each
// took a person to work out what to run. Fixer is how a report reads it.

// Fixer is implemented by errors that know the command or edit that resolves
// them.
type Fixer interface {
	Fix() string
}

// FixFor returns the fix carried by the outermost Fixer in err's chain, or
// "". An error that merely wraps a more specific one (an *SSHDialError around
// a host-key mismatch) defers to the fix of what it wraps; see
// SSHDialError.Fix.
func FixFor(err error) string {
	var fixer Fixer
	if errors.As(err, &fixer) {
		return fixer.Fix()
	}
	return ""
}

// Fix implements Fixer for the linger check.
func (e *LingerNotEnabledError) Fix() string { return e.Command }

// sshAuthFailureText is the text golang.org/x/crypto/ssh uses when every
// offered method was refused. The package declares no type for it, so the
// text is the only signal there is.
const sshAuthFailureText = "ssh: unable to authenticate"

// IsSSHAuthFailure reports whether a dial failed because the server refused
// every credential offered, as opposed to being unreachable or presenting an
// unexpected host key.
func IsSSHAuthFailure(err error) bool {
	return err != nil && strings.Contains(err.Error(), sshAuthFailureText)
}

// SSHKeyNotAuthorizedError reports a deploy key the remote user does not
// accept. AuthorizedKey is the key's authorized_keys line, so the fix can be
// pasted as-is.
type SSHKeyNotAuthorizedError struct {
	Host          string
	User          string
	AuthorizedKey string
	Cause         error
}

// Error implements the error interface.
func (e *SSHKeyNotAuthorizedError) Error() string {
	msg := "deploy: " + e.User + "@" + e.Host + " does not accept the deploy's ssh credential"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Fix implements Fixer.
func (e *SSHKeyNotAuthorizedError) Fix() string {
	if e.AuthorizedKey == "" {
		return "check the ssh user and password for " + e.User + "@" + e.Host
	}
	return "on " + e.Host + " as " + e.User + ", append to ~/.ssh/authorized_keys: " + strings.TrimSpace(e.AuthorizedKey)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *SSHKeyNotAuthorizedError) Unwrap() error { return e.Cause }

// SystemdUserUnavailableError reports a host with no working systemd --user
// manager when the spec asked for a systemd install.
type SystemdUserUnavailableError struct {
	Host string
}

// Error implements the error interface.
func (e *SystemdUserUnavailableError) Error() string {
	return "deploy: " + e.Host + " has no working systemd --user manager, and the spec requires systemd"
}

// Fix implements Fixer.
func (e *SystemdUserUnavailableError) Fix() string {
	return "run the agent under a user with a systemd --user manager, or deploy with --systemd no (nohup fallback)"
}

// ContainerRuntimeUnavailableError reports that the node's container runtime
// cannot be used by the deploy user. Reason says which part is missing so Fix
// can name the one command that resolves it.
type ContainerRuntimeUnavailableError struct {
	Runtime string
	User    string
	Reason  ContainerRuntimeFailure
	Cause   error
}

// ContainerRuntimeFailure classifies why a container runtime is unusable.
type ContainerRuntimeFailure string

const (
	// RuntimeNotInstalled: no runtime binary on the user's PATH.
	RuntimeNotInstalled ContainerRuntimeFailure = "not installed"
	// RuntimeNotInGroup: docker is installed but the user is not in the
	// docker group, so the daemon socket refuses it.
	RuntimeNotInGroup ContainerRuntimeFailure = "user not in the docker group"
	// RuntimeNotResponding: the binary is there and permitted, but the
	// runtime does not answer.
	RuntimeNotResponding ContainerRuntimeFailure = "not responding"
)

// Error implements the error interface.
func (e *ContainerRuntimeUnavailableError) Error() string {
	msg := "deploy: container runtime " + e.Runtime + " is unusable by " + e.User + ": " + string(e.Reason)
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Fix implements Fixer.
func (e *ContainerRuntimeUnavailableError) Fix() string {
	switch e.Reason {
	case RuntimeNotInGroup:
		return "sudo usermod -aG docker " + e.User + " (the group applies to new login sessions; the deploy's next ssh session is one)"
	case RuntimeNotResponding:
		return "start the runtime: sudo systemctl enable --now " + e.Runtime
	default:
		return "install docker or podman on the node, or set agent.container_runtime: none if it runs no sandboxed sessions"
	}
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *ContainerRuntimeUnavailableError) Unwrap() error { return e.Cause }

// InsufficientDiskError reports a filesystem with less free space than the
// node needs for Purpose ("worker images" or "workspaces").
type InsufficientDiskError struct {
	Path          string
	Purpose       string
	FreeBytes     uint64
	RequiredBytes uint64
}

// Error implements the error interface.
func (e *InsufficientDiskError) Error() string {
	return "deploy: " + e.Path + " has " + formatGiB(e.FreeBytes) + " free for " + e.Purpose +
		"; at least " + formatGiB(e.RequiredBytes) + " is required"
}

// Fix implements Fixer.
func (e *InsufficientDiskError) Fix() string {
	if e.Purpose == purposeWorkspaces {
		return "free space under " + e.Path + ", or set projects_root in the node spec to a larger volume"
	}
	return "free space under " + e.Path + " (docker system prune), or move the runtime's data root to a larger volume"
}

// WorkerImageUnavailableError reports a worker image that is neither present
// on the node nor pullable from its registry.
type WorkerImageUnavailableError struct {
	Image   string
	Runtime string
	Cause   error
}

// Error implements the error interface.
func (e *WorkerImageUnavailableError) Error() string {
	msg := "deploy: worker image " + e.Image + " is not on the node and cannot be pulled"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Fix implements Fixer.
func (e *WorkerImageUnavailableError) Fix() string {
	if e.Runtime == "" {
		return "install a container runtime on the node; worker images need one"
	}
	return "on the node, build it (make build-worker-image) or make it pullable: " + e.Runtime + " login <registry>; " + e.Runtime + " pull " + e.Image
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *WorkerImageUnavailableError) Unwrap() error { return e.Cause }

// WorkerImageUIDMismatchError reports a worker image built for a different
// uid than the one the daemon runs as. Sandboxed sessions run as the daemon's
// uid, and an image with no passwd entry for it fails every signed commit
// ("No user exists for uid").
type WorkerImageUIDMismatchError struct {
	Image     string
	ImageUser string
	RemoteUID string
	RemoteGID string
}

// Error implements the error interface.
func (e *WorkerImageUIDMismatchError) Error() string {
	imageUser := e.ImageUser
	if imageUser == "" {
		imageUser = "root (no USER)"
	}
	return "deploy: worker image " + e.Image + " runs as " + imageUser +
		" but the agent runs as uid " + e.RemoteUID + "; sandboxed sessions would have no user entry"
}

// Fix implements Fixer.
func (e *WorkerImageUIDMismatchError) Fix() string {
	return "rebuild it on the node as the deploy user (make build-worker-image), or with " +
		"--build-arg AGENT_UID=" + e.RemoteUID + " --build-arg AGENT_GID=" + e.RemoteGID
}

// ControlPlaneUnreachableError reports that the node cannot open a verified
// TLS connection to the control plane. Stage is "dial" (no route, refused,
// no name) or "tls" (the server's certificate does not verify against the
// CA the deploy installs).
type ControlPlaneUnreachableError struct {
	Server string
	Addr   string
	Stage  string
	Cause  error
}

// Error implements the error interface.
func (e *ControlPlaneUnreachableError) Error() string {
	msg := "deploy: the node cannot reach the control plane " + e.Server
	if e.Stage != "" {
		msg += " (" + e.Stage + ")"
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Fix implements Fixer.
func (e *ControlPlaneUnreachableError) Fix() string {
	switch e.Stage {
	case "tls":
		return "the server at " + e.Addr + " must present a certificate issued by the deploy's CA for that name; " +
			"set --server to the name on the control plane's certificate"
	case "url":
		return "set --server to the control plane's https://host:port address"
	case "ca":
		return "run the deploy against a control plane whose CA bundle it can read (quicbaas xkms ca bundle)"
	default:
		return "open " + e.Addr + " from the node to the control plane (firewall, routing, DNS), or set --server to an address the node can reach"
	}
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *ControlPlaneUnreachableError) Unwrap() error { return e.Cause }

// PreflightFailedError is the result of a preflight with at least one failed
// check. Failures holds each check's typed error; errors.As reaches any of
// them.
type PreflightFailedError struct {
	Host     string
	Failures []error
}

// Error implements the error interface. Each failure is listed with its fix.
func (e *PreflightFailedError) Error() string {
	var b strings.Builder
	b.WriteString("deploy: preflight failed for ")
	b.WriteString(e.Host)
	b.WriteString(" (")
	b.WriteString(strconv.Itoa(len(e.Failures)))
	b.WriteString(" check")
	if len(e.Failures) != 1 {
		b.WriteString("s")
	}
	b.WriteString(")")
	for _, failure := range e.Failures {
		b.WriteString("\n  - ")
		b.WriteString(failure.Error())
		if fix := FixFor(failure); fix != "" {
			b.WriteString("\n    fix: ")
			b.WriteString(fix)
		}
	}
	return b.String()
}

// Unwrap exposes every failure for errors.Is / errors.As.
func (e *PreflightFailedError) Unwrap() []error { return e.Failures }

// formatGiB renders a byte count in GiB with one decimal place.
func formatGiB(bytes uint64) string {
	tenths := bytes * 10 / (1 << 30)
	return strconv.FormatUint(tenths/10, 10) + "." + strconv.FormatUint(tenths%10, 10) + " GiB"
}
