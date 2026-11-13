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

// Fix implements Fixer. A dial that failed for a reason with a fix of its
// own — a host-key mismatch the resolver reported — defers to that fix;
// otherwise the fix is to make the host reachable.
func (e *SSHDialError) Fix() string {
	if inner := FixFor(e.Cause); inner != "" {
		return inner
	}
	return "confirm " + e.Host + " accepts ssh from this machine (ssh " + e.User + "@<host> -p <port>); check the address, port, firewall and that sshd is running"
}

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

// LingerNotEnabledError indicates the remote user's systemd --user
// manager will not outlive the deploy's SSH session, and the deploy could
// not enable lingering itself. Command carries the exact privileged
// command the operator must run before re-deploying.
type LingerNotEnabledError struct {
	User    string
	Command string
	Cause   error
}

// Error implements the error interface.
func (e *LingerNotEnabledError) Error() string {
	msg := "deploy: systemd --user lingering is not enabled"
	if e.User != "" {
		msg += " for " + e.User
	}
	msg += "; the agent would be stopped when the deploy's ssh session closes"
	if e.Command != "" {
		msg += "; enable it with: " + e.Command
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *LingerNotEnabledError) Unwrap() error { return e.Cause }

// FleetNodeUnreachableError indicates the deployed agent's runtime port did
// not accept a connection. Addr is the host:port that was dialed.
type FleetNodeUnreachableError struct {
	Addr  string
	Cause error
}

// Error implements the error interface.
func (e *FleetNodeUnreachableError) Error() string {
	msg := "deploy: agent runtime port is not reachable"
	if e.Addr != "" {
		msg += " addr=" + e.Addr
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *FleetNodeUnreachableError) Unwrap() error { return e.Cause }

// StartTimeoutError indicates the agent did not come online within the
// caller's WaitOnline budget.
type StartTimeoutError struct {
	Host    string
	Tenant  string
	Elapsed string

	// Cause is the last reason the agent was rejected as online, when
	// there was one — typically the failed liveness probe of the
	// agent's runtime port. Nil when the agent simply never appeared
	// in the fleet registry.
	Cause error
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
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *StartTimeoutError) Unwrap() error { return e.Cause }

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

// SpecFileError reports a node spec file that could not be read, decoded,
// or accepted. Path names the file.
type SpecFileError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *SpecFileError) Error() string {
	msg := "deploy: node spec " + e.Path
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *SpecFileError) Unwrap() error { return e.Cause }

// DeployOwnedFieldError reports an agent-section key the deploy sets itself.
// Owner is the spec field that declares the setting instead.
type DeployOwnedFieldError struct {
	Field string
	Owner string
}

// Error implements the error interface.
func (e *DeployOwnedFieldError) Error() string {
	return "deploy: invalid spec: " + e.Field + " is set by the deploy from the spec's `" +
		e.Owner + "`; declare it there and remove it from the node section"
}

// CapacityUndeclaredError reports a spec that does not declare how many jobs
// the node can run at once. A daemon that reports no capacity leaves the
// fleet's capacity undeclared, so admission cannot bound the fleet at all.
type CapacityUndeclaredError struct {
	Host     string
	Declared int
}

// Error implements the error interface.
func (e *CapacityUndeclaredError) Error() string {
	msg := "deploy: node"
	if e.Host != "" {
		msg += " " + e.Host
	}
	msg += " declares no capacity"
	if e.Declared < 0 {
		msg += " (agent.max_concurrent_jobs is negative)"
	}
	return msg + "; set agent.max_concurrent_jobs in the node spec (or pass --max-concurrent-jobs) " +
		"to the number of concurrent agent containers the host can sustain"
}

// AgentConfigInvalidError reports a daemon config the deploy would render but
// the daemon would refuse at start-up. Cause is fleet.Config.Validate's error.
type AgentConfigInvalidError struct {
	Cause error
}

// Error implements the error interface.
func (e *AgentConfigInvalidError) Error() string {
	return "deploy: invalid spec: the rendered agent config would not start: " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *AgentConfigInvalidError) Unwrap() error { return e.Cause }

// NotDeployedError reports a node with no agent installed where a lifecycle
// command expected one: neither the systemd unit nor the start script exists.
type NotDeployedError struct {
	Host   string
	Layout Layout
}

// Error implements the error interface.
func (e *NotDeployedError) Error() string {
	return "deploy: no agent is installed on " + e.Host + " (neither " + e.Layout.UnitFile +
		" nor " + e.Layout.StartScript + " exists); deploy it first, or pass the --install-dir the deploy used"
}

// AgentIdentityMissingError reports that the agent certificate the deploy
// installed could not be read from the node.
type AgentIdentityMissingError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *AgentIdentityMissingError) Error() string {
	msg := "deploy: cannot read the agent certificate " + e.Path
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *AgentIdentityMissingError) Unwrap() error { return e.Cause }

// PurgeRefusedError reports a --purge-workspaces target undeploy will not
// delete: the filesystem root, the user's home, or an ancestor of it.
type PurgeRefusedError struct {
	Path string
}

// Error implements the error interface.
func (e *PurgeRefusedError) Error() string {
	return "deploy: refusing to purge " + e.Path + ": it is not a workspace directory; check projects_root / --projects-root"
}

// LocalTargetError reports that the local install target could not
// resolve the user or home directory it installs for.
type LocalTargetError struct {
	Cause error
}

// Error implements the error interface.
func (e *LocalTargetError) Error() string {
	msg := "deploy: local target unavailable"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *LocalTargetError) Unwrap() error { return e.Cause }

// LocalExecError reports a command on the local install target that
// returned a non-zero exit status or failed to launch.
type LocalExecError struct {
	Command string
	Stderr  string
	Cause   error
}

// Error implements the error interface.
func (e *LocalExecError) Error() string {
	msg := "deploy: local command failed: " + e.Command
	if e.Stderr != "" {
		msg += " stderr=" + e.Stderr
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *LocalExecError) Unwrap() error { return e.Cause }

// LocalWriteError reports a file the local install target could not
// write. Source names the local file an upload copied from, if any.
type LocalWriteError struct {
	Path   string
	Source string
	Cause  error
}

// Error implements the error interface.
func (e *LocalWriteError) Error() string {
	msg := "deploy: local write failed path=" + e.Path
	if e.Source != "" {
		msg += " source=" + e.Source
	}
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *LocalWriteError) Unwrap() error { return e.Cause }

// LocalSSHConflictError reports a local install that also names an ssh
// target: --local installs on this host, so --ssh (or a spec's host or
// ssh credentials) contradicts it.
type LocalSSHConflictError struct {
	// Field is the ssh setting given alongside --local.
	Field string
}

// Error implements the error interface.
func (e *LocalSSHConflictError) Error() string {
	return "deploy: --local installs on this host and cannot be combined with an ssh target (" + e.Field +
		"); drop --local to deploy over ssh, or drop " + e.Field + " to install here"
}
