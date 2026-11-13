package sdlc

import (
	"context"
	"errors"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// auditForgeCredential is the job audit event a forge credential's
// revocation failure is recorded under; a delivery failure is the checkout's
// evidence and is recorded with it. The record names the session and
// the exit status, never the credential.
const auditForgeCredential = "forge_credential" //nolint:gosec // G101: an audit event name, not a credential

// deliverForgeCredentialScript writes standard input to the file named by $1
// and leaves it readable by its owner only.
//
// THE CREDENTIAL TRAVELS ON STDIN, the one exec channel that is neither
// recorded nor visible to a process listing: the command line, which is, names
// only the path. umask 077 makes the file private from the moment it exists,
// so there is no window in which another user could open it; the chmod then
// drops the owner's own write bit. The exec runs as the container's user, so
// the file is owned by the worker uid.
const deliverForgeCredentialScript = `umask 077 && rm -f -- "$1" && cat > "$1" && chmod 0400 "$1"` //nolint:gosec // G101: a shell script that reads stdin, not a credential

// forgeCredential is the project's stored forge token on its way to ONE
// session's credential tier (go-quicbaas#899).
//
// THE TOKEN NEVER TOUCHES A DISK. It is not on the spawn request, so no host
// writes it beside the workspace and no container config, label, environment
// variable or argument carries it. It reaches the container after the
// container exists, over an exec's standard input, into a file on the tmpfs
// mounted at orchestration.WorkspaceCredentialDir — memory the container's
// removal frees. A daemon that crashes mid-job leaves the file only inside a
// container the reaper removes, and nothing on any host's disk.
//
// THE FORGE CANNOT MINT A TOKEN PER JOB. Gitea's tokens carry no expiry and
// are scoped by category, not by repository, so this delivers the project's
// own credential per session rather than a per-job token with a lease-bound
// TTL. A per-job token needs a forge-side capability first.
type forgeCredential struct {
	// token is the daemon's copy, wiped on wipe.
	token []byte
}

// newForgeCredential holds a copy of token, or answers nil when there is
// none: a checkout over ssh needs no forge token.
func newForgeCredential(token []byte) *forgeCredential {
	if len(token) == 0 {
		return nil
	}
	held := make([]byte, len(token))
	copy(held, token)
	return &forgeCredential{token: held}
}

// Deliver writes the credential to the session's credential tier.
//
// A nil credential delivers nothing. A container that is gone is answered as
// SessionContainerGoneError, the same as a git command sent to it would be;
// every other refusal as ForgeCredentialDeliveryError.
func (c *forgeCredential) Deliver(exec sessionExecFunc, taskID, sessionID string) error {
	if c == nil {
		return nil
	}
	if len(c.token) == 0 {
		return &ForgeCredentialDeliveryError{TaskID: taskID, SessionID: sessionID, Cause: ErrForgeCredentialWiped}
	}
	stdin := make([]byte, len(c.token))
	copy(stdin, c.token)
	result, err := exec(&orchestration.ExecSpec{
		WorkDir: orchestration.WorkspaceCheckoutRoot,
		Cmd:     []string{"sh", "-c", deliverForgeCredentialScript, "sh", orchestration.WorkspaceForgeCredentialFile},
		Stdin:   stdin,
	})
	clear(stdin)
	if err != nil {
		if gone := (&gitSteps{taskID: taskID, sessionID: sessionID}).containerGone(err); gone != nil {
			return gone
		}
		return &ForgeCredentialDeliveryError{TaskID: taskID, SessionID: sessionID, Cause: err}
	}
	if result.ExitCode != 0 {
		return &ForgeCredentialDeliveryError{TaskID: taskID, SessionID: sessionID, ExitCode: result.ExitCode}
	}
	return nil
}

// Revoke removes the credential from the session's credential tier.
//
// A container that is gone has taken its tmpfs, and the file with it, so that
// is a revocation that has already happened and is answered nil. Every other
// failure is ForgeCredentialRevokeError, for the caller to log and audit: a
// close never stops for it, because the container's removal frees the tier
// either way, but nobody is left to believe the file was removed when it was
// not.
func (c *forgeCredential) Revoke(exec sessionExecFunc, sessionID string) error {
	if c == nil {
		return nil
	}
	result, err := exec(&orchestration.ExecSpec{
		WorkDir: orchestration.WorkspaceCheckoutRoot,
		Cmd:     []string{removeCommand, "-f", "--", orchestration.WorkspaceForgeCredentialFile},
	})
	if err != nil {
		if (&gitSteps{sessionID: sessionID}).containerGone(err) != nil {
			return nil
		}
		return &ForgeCredentialRevokeError{SessionID: sessionID, Cause: err}
	}
	if result.ExitCode != 0 {
		return &ForgeCredentialRevokeError{SessionID: sessionID, ExitCode: result.ExitCode}
	}
	return nil
}

// wipe zeroes the daemon's copy. Revoke still runs after it: removing the
// file needs no token.
func (c *forgeCredential) wipe() {
	if c == nil {
		return
	}
	clear(c.token)
	c.token = nil
}

// closeSession revokes the session's forge credential and then closes the
// session, whichever way the job ended.
//
// REVOCATION COMES FIRST because it is an exec into the container, and the
// close removes the container. A revocation that fails is logged and audited
// — the error names the session and the exit status, never the credential —
// and the close goes ahead: the container's removal frees the tier regardless.
// The caller passes a context detached from cancellation, as it does to a
// close, because every unplanned exit arrives with its own already cancelled.
func closeSession(ctx context.Context, job *orchestration.JobRun, sessionID string, workspace *containerWorkspace) {
	if sessionID == "" {
		return
	}
	if err := workspace.forgeCredential().Revoke(sessionExec(ctx, job, sessionID), sessionID); err != nil {
		job.Logger.Warnf("sdlc: %s", err.Error())
		job.Audit(auditForgeCredential, err.Error())
	}
	job.Close(ctx, sessionID)
}

// ErrForgeCredential indicates a forge credential that could not be put on,
// or taken off, a session's credential tier.
var ErrForgeCredential = errors.New("sdlc: the forge credential could not be handled on the session's credential tier")

// ErrForgeCredentialWiped indicates a delivery asked of a credential the
// daemon has already wiped: the workspace it belonged to was released.
var ErrForgeCredentialWiped = errors.New("sdlc: the forge credential was already wiped")

// ForgeCredentialDeliveryError reports a forge credential the session's
// container did not accept onto its credential tier.
//
// It carries no secret: the exit status and the runtime's refusal only. The
// command's own output is not kept, because nothing it could say is worth the
// risk of carrying what it was fed.
type ForgeCredentialDeliveryError struct {
	// TaskID is the attempt whose checkout needed the credential.
	TaskID string

	// SessionID is the session the credential was sent to.
	SessionID string

	// ExitCode is the delivery command's exit status, zero when it never ran.
	ExitCode int

	// Cause is the runtime's refusal, nil when the command ran and failed.
	Cause error
}

// Error implements error.
func (e *ForgeCredentialDeliveryError) Error() string {
	msg := ErrRunner.Error() + ": task " + e.TaskID + ": session " + e.SessionID +
		": the forge credential was not delivered to " + orchestration.WorkspaceForgeCredentialFile
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg + ": exit status " + strconv.Itoa(e.ExitCode)
}

// Unwrap returns the sentinels and the cause for errors.Is/As support.
//
// IT IS ErrWorkspaceUnfit: a container that cannot hold a file on its own
// tmpfs will refuse identically on every attempt, so it is an operator's to
// repair rather than the task's to retry.
func (e *ForgeCredentialDeliveryError) Unwrap() []error {
	errs := []error{ErrRunner, ErrWorkspaceUnfit, ErrForgeCredential}
	if e.Cause != nil {
		errs = append(errs, e.Cause)
	}
	return errs
}

// ForgeCredentialRevokeError reports a forge credential that could not be
// removed from a session's credential tier when the session closed.
type ForgeCredentialRevokeError struct {
	// SessionID is the session being closed.
	SessionID string

	// ExitCode is the removal's exit status, zero when it never ran.
	ExitCode int

	// Cause is the runtime's refusal, nil when the command ran and failed.
	Cause error
}

// Error implements error.
func (e *ForgeCredentialRevokeError) Error() string {
	msg := ErrRunner.Error() + ": session " + e.SessionID +
		": the forge credential was not removed from " + orchestration.WorkspaceForgeCredentialFile
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg + ": exit status " + strconv.Itoa(e.ExitCode)
}

// Unwrap returns the sentinels and the cause for errors.Is/As support.
func (e *ForgeCredentialRevokeError) Unwrap() []error {
	errs := []error{ErrRunner, ErrForgeCredential}
	if e.Cause != nil {
		errs = append(errs, e.Cause)
	}
	return errs
}
