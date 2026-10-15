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

package service

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"

	"golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Adding a fleet node is a declared, pre-flighted, reversible operation:
// Preflight says whether a deploy would work without changing anything,
// Deploy runs it first, and Stop / Start / Undeploy take the node back out —
// each of them removing the fleet node from the fleet registry so a node that is
// not running never reads "running" there.

// FleetNodeRemover removes a fleet node from the fleet registry.
type FleetNodeRemover interface {
	RemoveFleetNode(ctx context.Context, req *orchestratorSDK.RemoveFleetNodeRequest) (*orchestratorSDK.RemoveFleetNodeResponse, error)
}

// FleetCABundleSource returns the CA bundle a deploy installs as the
// daemon's server-ca.crt, without issuing anything. Preflight verifies the
// control plane against exactly this bundle. Every FleetNodeCertIssuer shipped in
// this repository implements it.
type FleetCABundleSource interface {
	FleetCABundle(ctx context.Context, tenantID string) ([]byte, error)
}

// nodeOperator is the set of node-side operations the preflight and
// lifecycle paths run over an SSH session. The production implementation
// delegates to pkg/fleet/deploy; tests substitute a fake.
type nodeOperator interface {
	Preflight(ctx context.Context, sess sshSession, in deploy.PreflightInput) []deploy.PreflightCheck
	Stop(ctx context.Context, sess sshSession, l deploy.Layout) (deploy.Supervisor, error)
	Start(ctx context.Context, sess sshSession, l deploy.Layout) (deploy.Supervisor, error)
	Uninstall(ctx context.Context, sess sshSession, l deploy.Layout, purgeWorkspaces bool) (*deploy.UninstallResult, error)
	ReadCertificate(ctx context.Context, sess sshSession, l deploy.Layout) (*x509.Certificate, error)
}

// Preflight implements FleetDeployService.Preflight.
func (s *fleetDeployService) Preflight(ctx context.Context, spec *deploy.Spec) (*deploy.PreflightReport, error) {
	if err := s.validate(spec); err != nil {
		return nil, err
	}
	s.stampMCPEndpoint(spec)
	return s.preflight(ctx, spec)
}

// preflight dials the node on a session of its own, records the SSH-level
// checks the dial itself answers, then runs the session checks. The returned
// error is the report's: nil when every check passed.
func (s *fleetDeployService) preflight(ctx context.Context, spec *deploy.Spec) (*deploy.PreflightReport, error) {
	report := &deploy.PreflightReport{Host: spec.Host}

	caPEM, caErr := s.caBundle(ctx, spec)

	sess, err := s.dial(ctx, spec)
	if err != nil {
		if spec.Local {
			// No ssh to check: the only way to fail is not knowing who
			// this process runs as.
			report.Add(deploy.FailedCheck(deploy.CheckRemoteUser, err))
		} else {
			report.Add(s.dialFailureChecks(ctx, spec, err)...)
		}
		return finished(report)
	}
	defer sess.Close()
	if !spec.Local {
		report.Add(s.dialSuccessChecks(spec)...)
	}

	home, err := s.remoteHome(ctx, sess)
	if err != nil {
		report.Add(deploy.FailedCheck(deploy.CheckRemoteUser, err))
		return finished(report)
	}

	checks := s.nodeOps().Preflight(ctx, sess, deploy.PreflightInput{
		Spec:        spec,
		Layout:      spec.Layout(home),
		ServerCAPEM: caPEM,
	})
	if caErr != nil {
		// Without the deploy's CA the control-plane check has nothing to
		// verify against; report why, not merely that it had no CA.
		for i := range checks {
			if checks[i].Name == deploy.CheckControlPlane {
				checks[i] = deploy.FailedCheck(deploy.CheckControlPlane, &deploy.ControlPlaneUnreachableError{
					Server: spec.Server, Stage: "ca", Cause: caErr,
				})
			}
		}
	}
	report.Add(checks...)
	return finished(report)
}

// finished returns a completed report with its verdict: nil when every check
// passed, the *deploy.PreflightFailedError otherwise.
func finished(report *deploy.PreflightReport) (*deploy.PreflightReport, error) {
	err := report.Err()
	return report, err
}

// caBundle fetches the CA the deploy installs, from the configured issuer.
func (s *fleetDeployService) caBundle(ctx context.Context, spec *deploy.Spec) ([]byte, error) {
	source, ok := s.certIssuer.(FleetCABundleSource)
	if !ok {
		return nil, &FleetCABundleUnavailableError{}
	}
	return source.FleetCABundle(ctx, spec.Tenant)
}

// dialSuccessChecks records what a successful dial proved.
func (s *fleetDeployService) dialSuccessChecks(spec *deploy.Spec) []deploy.PreflightCheck {
	hostKey := "verified against its pin in the ssh known-hosts store"
	if spec.InsecureHostKey {
		hostKey = "NOT verified (--ssh-insecure-host-key)"
	} else if s.hostKeys == nil {
		hostKey = "verified by the dial's own policy"
	}
	credential := "password accepted"
	if spec.SSHKeyName != "" {
		credential = "stored key " + spec.SSHKeyName + " accepted"
	}
	return []deploy.PreflightCheck{
		deploy.PassedCheck(deploy.CheckSSH, spec.SSHUser+"@"+spec.Host),
		deploy.PassedCheck(deploy.CheckHostKey, hostKey),
		deploy.PassedCheck(deploy.CheckDeployKey, credential),
	}
}

// dialFailureChecks turns a failed dial into the check it failed, so the
// report names the host key or the deploy key rather than "ssh failed".
//
// Host-key verification runs before authentication in the SSH handshake, so
// an authentication failure also proves the host key was accepted.
func (s *fleetDeployService) dialFailureChecks(ctx context.Context, spec *deploy.Spec, err error) []deploy.PreflightCheck {
	reachable := deploy.PassedCheck(deploy.CheckSSH, spec.SSHUser+"@"+spec.Host+" reachable")

	var notPinned *SSHHostKeyNotPinnedError
	var mismatch *SSHHostKeyMismatchError
	var algorithm *SSHHostKeyAlgorithmMismatchError
	switch {
	case errors.As(err, &notPinned):
		// Resolved before any connection was attempted.
		return []deploy.PreflightCheck{deploy.FailedCheck(deploy.CheckHostKey, err)}
	case errors.As(err, &mismatch), errors.As(err, &algorithm):
		return []deploy.PreflightCheck{reachable, deploy.FailedCheck(deploy.CheckHostKey, err)}
	case deploy.IsSSHAuthFailure(err):
		return []deploy.PreflightCheck{
			reachable,
			deploy.PassedCheck(deploy.CheckHostKey, "accepted"),
			deploy.FailedCheck(deploy.CheckDeployKey, &deploy.SSHKeyNotAuthorizedError{
				Host:          hostnameForCert(spec.Host),
				User:          spec.SSHUser,
				AuthorizedKey: s.authorizedKeyLine(ctx, spec),
				Cause:         err,
			}),
		}
	default:
		return []deploy.PreflightCheck{deploy.FailedCheck(deploy.CheckSSH, err)}
	}
}

// authorizedKeyLine returns the deploy key's authorized_keys line, or "" for
// a password deploy or a key the store cannot produce.
func (s *fleetDeployService) authorizedKeyLine(ctx context.Context, spec *deploy.Spec) string {
	if spec.SSHKeyName == "" || s.sshKeys == nil {
		return ""
	}
	signer, err := s.sshKeys.Signer(ctx, SSHKeyRef{Name: spec.SSHKeyName, Scope: spec.SSHKeyScope, Owner: spec.SSHKeyOwner})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " " + spec.SSHKeyName
}

// nodeOps returns the node operator, defaulting to the real primitives the
// way resolveHome and probe default: an unset dependency is never a no-op.
func (s *fleetDeployService) nodeOps() nodeOperator {
	if s.node == nil {
		return realNodeOperator{}
	}
	return s.node
}

// remoteHome resolves the connected user's home directory.
func (s *fleetDeployService) remoteHome(ctx context.Context, sess sshSession) (string, error) {
	resolve := s.resolveHome
	if resolve == nil {
		resolve = defaultResolveRemoteHome
	}
	return resolve(ctx, sess)
}

// Stop implements FleetDeployService.Stop.
//
// The process is stopped before the registration is removed. The other
// order would let the still-running daemon's next heartbeat find itself
// missing and re-register — putting "running" straight back.
func (s *fleetDeployService) Stop(ctx context.Context, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
	if err := s.validateLifecycle(spec, true); err != nil {
		return nil, err
	}
	result := &deploy.LifecycleResult{Host: spec.Host, Action: "stop"}

	err := s.onNode(ctx, spec, func(sess sshSession, l deploy.Layout) error {
		cert, err := s.nodeOps().ReadCertificate(ctx, sess, l)
		if err != nil {
			return err
		}
		result.NodeID = iam.FleetNodeAppName(spec.Tenant, cert.Subject.CommonName)
		result.Supervisor, err = s.nodeOps().Stop(ctx, sess, l)
		return err
	})
	if err != nil {
		return result, err
	}
	return result, s.deregister(ctx, spec, result)
}

// Start implements FleetDeployService.Start.
func (s *fleetDeployService) Start(ctx context.Context, spec *deploy.Spec) (*deploy.LifecycleResult, error) {
	if err := s.validateLifecycle(spec, false); err != nil {
		return nil, err
	}
	if spec.WaitOnline > 0 && s.agents != nil && nodeProbeAddr(spec) == "" {
		return nil, undialableBindError(spec)
	}
	result := &deploy.LifecycleResult{Host: spec.Host, Action: "start"}

	// As with Deploy, the session is closed before the fleet node is checked:
	// what start claims is a fleet node that outlives the session starting it.
	err := s.onNode(ctx, spec, func(sess sshSession, l deploy.Layout) error {
		if cert, err := s.nodeOps().ReadCertificate(ctx, sess, l); err == nil {
			result.NodeID = iam.FleetNodeAppName(spec.Tenant, cert.Subject.CommonName)
		}
		var err error
		result.Supervisor, err = s.nodeOps().Start(ctx, sess, l)
		return err
	})
	if err != nil {
		return result, err
	}

	if spec.WaitOnline > 0 && s.agents != nil {
		_, onlineAt, err := s.waitOnline(ctx, spec, result.NodeID)
		if err != nil {
			return result, err
		}
		result.OnlineAt = onlineAt
	}
	return result, nil
}

// Undeploy implements FleetDeployService.Undeploy.
func (s *fleetDeployService) Undeploy(ctx context.Context, spec *deploy.Spec, opts UndeployOptions) (*deploy.LifecycleResult, error) {
	if err := s.validateLifecycle(spec, true); err != nil {
		return nil, err
	}
	result := &deploy.LifecycleResult{Host: spec.Host, Action: "undeploy"}

	err := s.onNode(ctx, spec, func(sess sshSession, l deploy.Layout) error {
		// Read the identity before removing it: it is the only record of
		// which registration is this node's.
		if cert, err := s.nodeOps().ReadCertificate(ctx, sess, l); err == nil {
			result.NodeID = iam.FleetNodeAppName(spec.Tenant, cert.Subject.CommonName)
		}
		uninstalled, err := s.nodeOps().Uninstall(ctx, sess, l, opts.PurgeWorkspaces)
		if uninstalled != nil {
			result.Supervisor = uninstalled.Supervisor
			result.Removed = uninstalled.Removed
		}
		return err
	})
	if err != nil {
		return result, err
	}
	if result.NodeID == "" {
		result.Note = "no agent certificate on the node, so no registration to remove; " +
			"check `quicbaas fleet list` for a stale entry and remove it with `quicbaas fleet remove`"
		return result, nil
	}
	return result, s.deregister(ctx, spec, result)
}

// onNode dials the node, resolves its layout, runs fn, and closes the
// session before returning.
func (s *fleetDeployService) onNode(ctx context.Context, spec *deploy.Spec, fn func(sshSession, deploy.Layout) error) error {
	sess, err := s.dial(ctx, spec)
	if err != nil {
		return err
	}
	defer sess.Close()
	home, err := s.remoteHome(ctx, sess)
	if err != nil {
		return err
	}
	return fn(sess, spec.Layout(home))
}

// deregister removes the node's fleet node from the fleet registry. A fleet node the
// registry does not hold is already where stop and undeploy want it.
func (s *fleetDeployService) deregister(ctx context.Context, spec *deploy.Spec, result *deploy.LifecycleResult) error {
	_, err := s.remover.RemoveFleetNode(ctx, &orchestratorSDK.RemoveFleetNodeRequest{
		TenantID: spec.Tenant,
		NodeID:   result.NodeID,
	})
	if err != nil && !sdkerrors.IsNotFound(err) {
		return &FleetNodeDeregisterError{AgentID: result.NodeID, Tenant: spec.Tenant, Cause: err}
	}
	result.Deregistered = true
	return nil
}

// validateLifecycle checks what a lifecycle command needs: a node to reach,
// a tenant to address its registration in, and — for the commands that
// deregister — a registry to remove it from.
func (s *fleetDeployService) validateLifecycle(spec *deploy.Spec, deregisters bool) error {
	if spec == nil {
		return &deploy.InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	if err := validateTarget(spec); err != nil {
		return err
	}
	if spec.Tenant == "" {
		return &deploy.InvalidSpecError{Field: "tenant", Reason: "tenant is required"}
	}
	if conflict := sshAuthConflict(spec); conflict != nil {
		return conflict
	}
	if deregisters && s.remover == nil {
		return &FleetRegistryUnavailableError{}
	}
	return nil
}

// =============================================================================
// Typed errors
// =============================================================================

// FleetNodeDeregisterError reports a fleet registry removal that failed after
// the fleet node was stopped. The fleet node is not running; the registry still says
// it is, and Fix names the command that corrects it.
type FleetNodeDeregisterError struct {
	AgentID string
	Tenant  string
	Cause   error
}

// Error implements the error interface.
func (e *FleetNodeDeregisterError) Error() string {
	return "agent deploy: the agent is stopped but " + e.AgentID +
		" could not be removed from the fleet registry: " + e.Cause.Error()
}

// Fix implements deploy.Fixer.
func (e *FleetNodeDeregisterError) Fix() string {
	return "quicbaas fleet remove " + e.AgentID + " --tenant " + e.Tenant
}

// Unwrap returns the wrapped cause.
func (e *FleetNodeDeregisterError) Unwrap() error { return e.Cause }

// FleetRegistryUnavailableError reports a stop or undeploy with no fleet
// registry client to deregister through.
type FleetRegistryUnavailableError struct{}

// Error implements the error interface.
func (e *FleetRegistryUnavailableError) Error() string {
	return "agent deploy: no fleet registry client is configured, so the agent could not be deregistered; " +
		"run the command where the CLI can reach the control plane"
}

// FleetCABundleUnavailableError reports a certificate issuer that cannot
// hand out the CA bundle on its own, so preflight has nothing to verify the
// control plane against.
type FleetCABundleUnavailableError struct{}

// Error implements the error interface.
func (e *FleetCABundleUnavailableError) Error() string {
	return "agent deploy: the certificate issuer cannot provide the CA bundle the deploy installs"
}

// =============================================================================
// Production adapter
// =============================================================================

// realNodeOperator delegates to the pkg/fleet/deploy primitives.
type realNodeOperator struct{}

func noClientError() error {
	return &deploy.InvalidSpecError{Field: "ssh_session", Reason: "no underlying client"}
}

func (realNodeOperator) Preflight(ctx context.Context, sess sshSession, in deploy.PreflightInput) []deploy.PreflightCheck {
	c := underlyingClient(sess)
	if c == nil {
		return []deploy.PreflightCheck{deploy.FailedCheck(deploy.CheckSSH, noClientError())}
	}
	return deploy.RunPreflight(ctx, c, in)
}

func (realNodeOperator) Stop(ctx context.Context, sess sshSession, l deploy.Layout) (deploy.Supervisor, error) {
	c := underlyingClient(sess)
	if c == nil {
		return deploy.SupervisorNone, noClientError()
	}
	return deploy.StopNode(ctx, c, l)
}

func (realNodeOperator) Start(ctx context.Context, sess sshSession, l deploy.Layout) (deploy.Supervisor, error) {
	c := underlyingClient(sess)
	if c == nil {
		return deploy.SupervisorNone, noClientError()
	}
	return deploy.StartNode(ctx, c, l)
}

func (realNodeOperator) Uninstall(ctx context.Context, sess sshSession, l deploy.Layout, purgeWorkspaces bool) (*deploy.UninstallResult, error) {
	c := underlyingClient(sess)
	if c == nil {
		return nil, noClientError()
	}
	return deploy.UninstallNode(ctx, c, l, purgeWorkspaces)
}

func (realNodeOperator) ReadCertificate(ctx context.Context, sess sshSession, l deploy.Layout) (*x509.Certificate, error) {
	c := underlyingClient(sess)
	if c == nil {
		return nil, noClientError()
	}
	return deploy.ReadNodeCertificate(ctx, c, l)
}
