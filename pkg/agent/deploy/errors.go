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

// All errors surfaced by this package are declared as named types so
// callers can programmatically distinguish failure modes with errors.As
// or type switches. This follows the project convention of preferring
// typed errors over fmt.Errorf strings.

// SSHDialError indicates the SSH handshake to the remote host failed.
// The Cause preserves the underlying crypto/ssh or net error so the
// caller can still see network diagnostics.
type SSHDialError struct {
	Host  string
	User  string
	Cause error
}

// Error implements the error interface.
func (e *SSHDialError) Error() string {
	msg := "deploy: ssh dial failed"
	if e.Host != "" {
		msg += " to " + e.Host
	}
	if e.User != "" {
		msg += " as " + e.User
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *SSHDialError) Unwrap() error { return e.Cause }

// SFTPUploadError indicates the SFTP transfer failed. It carries both
// the local and remote paths so the caller can build accurate
// diagnostic messages.
type SFTPUploadError struct {
	LocalPath  string
	RemotePath string
	Cause      error
}

// Error implements the error interface.
func (e *SFTPUploadError) Error() string {
	msg := "deploy: sftp upload failed"
	if e.LocalPath != "" {
		msg += " local=" + e.LocalPath
	}
	if e.RemotePath != "" {
		msg += " remote=" + e.RemotePath
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *SFTPUploadError) Unwrap() error { return e.Cause }

// RemoteExecError indicates a remote command returned a non-zero exit
// status or failed to launch.
type RemoteExecError struct {
	Command string
	Stderr  string
	Cause   error
}

// Error implements the error interface.
func (e *RemoteExecError) Error() string {
	msg := "deploy: remote command failed"
	if e.Command != "" {
		msg += ": " + e.Command
	}
	if e.Stderr != "" {
		msg += " stderr=" + e.Stderr
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *RemoteExecError) Unwrap() error { return e.Cause }

// SystemdInstallError indicates a systemd unit could not be installed
// or started on the remote host.
type SystemdInstallError struct {
	Unit  string
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *SystemdInstallError) Error() string {
	msg := "deploy: systemd install failed"
	if e.Stage != "" {
		msg += " stage=" + e.Stage
	}
	if e.Unit != "" {
		msg += " unit=" + e.Unit
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *SystemdInstallError) Unwrap() error { return e.Cause }

// StartTimeoutError indicates the agent did not come online within the
// caller's WaitOnline budget.
type StartTimeoutError struct {
	Host    string
	Tenant  string
	Elapsed string
}

// Error implements the error interface.
func (e *StartTimeoutError) Error() string {
	msg := "deploy: agent did not appear online"
	if e.Host != "" {
		msg += " host=" + e.Host
	}
	if e.Tenant != "" {
		msg += " tenant=" + e.Tenant
	}
	if e.Elapsed != "" {
		msg += " elapsed=" + e.Elapsed
	}
	return msg
}

// InvalidSpecError indicates the caller supplied a Spec that cannot be
// turned into a valid remote invocation. The Field reports the first
// problematic field name.
type InvalidSpecError struct {
	Field  string
	Reason string
}

// Error implements the error interface.
func (e *InvalidSpecError) Error() string {
	msg := "deploy: invalid spec"
	if e.Field != "" {
		msg += " field=" + e.Field
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}
