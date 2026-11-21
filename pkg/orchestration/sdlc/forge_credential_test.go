package sdlc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestForgeAccess_TheForgeTokenReachesNoSpawnSurface pins go-quicbaas#899:
// the forge token is not on the spawn request in any form a host writes to
// disk, nor in its environment, argument vector or labels. It reaches the
// container only through the credential tier, a tmpfs, after the container
// exists.
func TestForgeAccess_TheForgeTokenReachesNoSpawnSurface(t *testing.T) {
	rig := newAccessRig(t)
	req, _, err := rig.open(t, testOrchProject(), SigningScopeCheckout)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	secret := string(rig.tokens.token)

	for name, content := range req.Workspace.Spec.Files {
		if bytes.Contains(content, rig.tokens.token) {
			t.Errorf("spec file %q carries the forge token to the host's disk", name)
		}
	}
	for key, value := range req.Env {
		if strings.Contains(value, secret) {
			t.Errorf("env %s carries the forge token", key)
		}
	}
	for _, arg := range req.ExtraArgs {
		if strings.Contains(arg, secret) {
			t.Errorf("argv %q carries the forge token", arg)
		}
	}
	// Whatever else the request carries — its container config, its labels,
	// the spec — travels encoded, so the encoding is what is searched.
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	if bytes.Contains(encoded, rig.tokens.token) ||
		bytes.Contains(encoded, []byte(base64.StdEncoding.EncodeToString(rig.tokens.token))) {
		t.Error("the encoded spawn request carries the forge token")
	}

	var tier *orchestration.ContainerMount
	for i := range req.ExtraMounts {
		if req.ExtraMounts[i].Target == orchestration.WorkspaceCredentialDir {
			tier = &req.ExtraMounts[i]
		}
	}
	if tier == nil || tier.Type != orchestration.MountTypeTmpfs || tier.Source != "" {
		t.Fatalf("credential tier = %+v, want a tmpfs at %s", tier, orchestration.WorkspaceCredentialDir)
	}
	if tier.TmpfsSizeBytes <= 0 {
		t.Error("the credential tier is unsized, so it could take the container's whole memory budget")
	}
	if got := req.Workspace.Spec.Spec.ForgeTokenFile; got != orchestration.WorkspaceForgeCredentialFile {
		t.Errorf("spec forge_token_file = %q, want %q on the tmpfs", got, orchestration.WorkspaceForgeCredentialFile)
	}
}

// credentialExec is a session's container as the credential sees it: every
// command it is sent, and the answer the test chose.
type credentialExec struct {
	mu    sync.Mutex
	specs []orchestration.ExecSpec

	// result and err are what every exec answers.
	result *orchestration.ExecResult
	err    error
}

func (c *credentialExec) exec(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	recorded := *spec
	recorded.Stdin = bytes.Clone(spec.Stdin)
	c.specs = append(c.specs, recorded)
	if c.err != nil {
		return nil, c.err
	}
	if c.result != nil {
		return c.result, nil
	}
	return &orchestration.ExecResult{}, nil
}

func (c *credentialExec) sent() []orchestration.ExecSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]orchestration.ExecSpec(nil), c.specs...)
}

// TestForgeCredential_DeliversOnStdinToAPrivateFileOnTheTmpfs: the token is
// the exec's standard input and nowhere in its command or environment; the
// command writes the credential tier's file, private from creation and
// read-only after.
func TestForgeCredential_DeliversOnStdinToAPrivateFileOnTheTmpfs(t *testing.T) {
	token := []byte("t0ken-delivered")
	cred := newForgeCredential(token)
	clear(token) // the caller's buffer is its own; the credential holds a copy
	container := &credentialExec{}

	if err := cred.Deliver(container.exec, "task-1", "session-1"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	sent := container.sent()
	if len(sent) != 1 {
		t.Fatalf("execs = %d, want one", len(sent))
	}
	spec := sent[0]
	if string(spec.Stdin) != "t0ken-delivered" {
		t.Fatalf("stdin = %q, want the token", spec.Stdin)
	}
	cmd := strings.Join(spec.Cmd, " ")
	if strings.Contains(cmd, "t0ken") || len(spec.Env) != 0 {
		t.Fatalf("the token is on the command line or in the environment: %q %v", cmd, spec.Env)
	}
	if spec.Cmd[len(spec.Cmd)-1] != orchestration.WorkspaceForgeCredentialFile ||
		!strings.HasPrefix(orchestration.WorkspaceForgeCredentialFile, orchestration.WorkspaceCredentialDir+"/") {
		t.Fatalf("delivered to %q, want %s on the credential tier", spec.Cmd[len(spec.Cmd)-1], orchestration.WorkspaceForgeCredentialFile)
	}
	for _, want := range []string{"umask 077", "chmod 0400"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("the delivery does not %q: %q", want, cmd)
		}
	}
}

// TestForgeCredential_NoTokenDeliversAndRevokesNothing: a checkout over ssh
// holds no forge credential, so nothing is sent to its container.
func TestForgeCredential_NoTokenDeliversAndRevokesNothing(t *testing.T) {
	cred := newForgeCredential(nil)
	if cred != nil {
		t.Fatalf("an empty token made a credential: %+v", cred)
	}
	container := &credentialExec{}
	if err := cred.Deliver(container.exec, "task-1", "session-1"); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if err := cred.Revoke(container.exec, "session-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if n := len(container.sent()); n != 0 {
		t.Fatalf("execs = %d, want none", n)
	}
}

// TestForgeCredential_ADeliveryRefusedIsTypedAndCarriesNoSecret: a container
// that cannot write its own tmpfs is unfit, and says so without the token.
func TestForgeCredential_ADeliveryRefusedIsTypedAndCarriesNoSecret(t *testing.T) {
	cases := map[string]*credentialExec{
		"non-zero exit":   {result: &orchestration.ExecResult{ExitCode: 1, Stderr: "cat: t0ken-x: read-only file system"}},
		"runtime refusal": {err: &orchestration.SessionNotContainerizedError{SessionID: "session-1"}},
	}
	for name, container := range cases {
		t.Run(name, func(t *testing.T) {
			err := newForgeCredential([]byte("t0ken-x")).Deliver(container.exec, "task-1", "session-1")
			var refused *ForgeCredentialDeliveryError
			if !errors.As(err, &refused) || !errors.Is(err, ErrWorkspaceUnfit) || !errors.Is(err, ErrForgeCredential) {
				t.Fatalf("Deliver = %v, want *ForgeCredentialDeliveryError, unfit", err)
			}
			if strings.Contains(err.Error(), "t0ken") {
				t.Fatalf("the error carries the token: %q", err.Error())
			}
		})
	}
}

// TestForgeCredential_AContainerGoneIsGoneNotRefused: a delivery to a
// container that is gone is the same answer a git command would get; a
// revocation from one has already happened, because its tmpfs went with it.
func TestForgeCredential_AContainerGoneIsGoneNotRefused(t *testing.T) {
	for name, cause := range map[string]error{
		"stopped": &orchestration.ContainerNotRunningError{ContainerID: "c-1"},
		"removed": &orchestration.ContainerNotFoundError{ContainerID: "c-1"},
	} {
		t.Run(name, func(t *testing.T) {
			container := &credentialExec{err: cause}
			cred := newForgeCredential([]byte("t0ken"))
			var gone *SessionContainerGoneError
			if err := cred.Deliver(container.exec, "task-1", "session-1"); !errors.As(err, &gone) {
				t.Fatalf("Deliver = %v, want *SessionContainerGoneError", err)
			}
			if err := cred.Revoke(container.exec, "session-1"); err != nil {
				t.Fatalf("Revoke from a container that is gone = %v, want nil", err)
			}
		})
	}
}

// TestForgeCredential_RevokeRemovesTheFile: revocation removes the credential
// tier's file and needs no token to do it, so it still runs after the
// workspace was released and the daemon's copy wiped.
func TestForgeCredential_RevokeRemovesTheFile(t *testing.T) {
	cred := newForgeCredential([]byte("t0ken"))
	cred.wipe()
	container := &credentialExec{}
	if err := cred.Revoke(container.exec, "session-1"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	sent := container.sent()
	if len(sent) != 1 {
		t.Fatalf("execs = %d, want one", len(sent))
	}
	want := []string{"rm", "-f", "--", orchestration.WorkspaceForgeCredentialFile}
	if strings.Join(sent[0].Cmd, " ") != strings.Join(want, " ") || len(sent[0].Stdin) != 0 {
		t.Fatalf("revoke sent %+v, want %v", sent[0], want)
	}
}

// TestForgeCredential_ARevocationRefusedIsSurfaced: a removal that fails is
// returned typed, not swallowed, and names no secret.
func TestForgeCredential_ARevocationRefusedIsSurfaced(t *testing.T) {
	cases := map[string]*credentialExec{
		"non-zero exit":   {result: &orchestration.ExecResult{ExitCode: 1}},
		"runtime refusal": {err: &orchestration.SessionNotContainerizedError{SessionID: "session-1"}},
	}
	for name, container := range cases {
		t.Run(name, func(t *testing.T) {
			err := newForgeCredential([]byte("t0ken")).Revoke(container.exec, "session-1")
			var refused *ForgeCredentialRevokeError
			if !errors.As(err, &refused) || !errors.Is(err, ErrForgeCredential) || refused.SessionID != "session-1" {
				t.Fatalf("Revoke = %v, want *ForgeCredentialRevokeError", err)
			}
			if strings.Contains(err.Error(), "t0ken") {
				t.Fatalf("the error carries the token: %q", err.Error())
			}
		})
	}
}

// TestContainerWorkspace_ReleaseWipesTheForgeCredential: the daemon's copy is
// zeroed when the workspace is released, and a delivery asked of it after
// that is refused rather than sending an empty credential.
func TestContainerWorkspace_ReleaseWipesTheForgeCredential(t *testing.T) {
	cred := newForgeCredential([]byte("t0ken"))
	held := cred.token
	workspace := &containerWorkspace{credential: cred}
	workspace.Release(context.Background())

	for _, b := range held {
		if b != 0 {
			t.Fatal("the daemon's copy was released but not zeroed")
		}
	}
	container := &credentialExec{}
	if err := workspace.forgeCredential().Deliver(container.exec, "task-1", "session-1"); !errors.Is(err, ErrForgeCredentialWiped) {
		t.Fatalf("Deliver after Release = %v, want ErrForgeCredentialWiped", err)
	}
	if n := len(container.sent()); n != 0 {
		t.Fatalf("execs = %d, want none", n)
	}
	var none *containerWorkspace
	if none.forgeCredential() != nil {
		t.Fatal("a nil workspace answered a credential")
	}
}

// TestRunner_TheForgeCredentialIsOnTheTierForTheAttemptAndOffItAfter: the
// token reaches the tier before the first checkout command, is the project's
// stored token, and is gone from the tier once the attempt has closed.
func TestRunner_TheForgeCredentialIsOnTheTierForTheAttemptAndOffItAfter(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rig.container.mu.Lock()
	defer rig.container.mu.Unlock()
	if rig.container.credentialDeliveries != 1 || rig.container.credentialAt != 0 {
		t.Fatalf("%d deliveries, the first after %d checkout commands; want one, before the first",
			rig.container.credentialDeliveries, rig.container.credentialAt)
	}
	if len(rig.container.credential) != 0 || rig.container.credentialRevoked != 1 {
		t.Fatalf("the tier holds %d bytes after %d revocations, want it emptied once at close",
			len(rig.container.credential), rig.container.credentialRevoked)
	}
}

// closeAgents is the agent service a closing session sees: it records every
// exec and close in order.
type closeAgents struct {
	orchestration.AgentService

	mu     sync.Mutex
	calls  []string
	execFn func(*orchestration.ExecSpec) (*orchestration.ExecResult, error)
}

func (a *closeAgents) ExecSession(_ context.Context, req *orchestration.ExecSessionRequest) (*orchestration.ExecResult, error) {
	a.mu.Lock()
	a.calls = append(a.calls, "exec "+strings.Join(req.Spec.Cmd, " "))
	a.mu.Unlock()
	return a.execFn(req.Spec)
}

func (a *closeAgents) CloseSession(_ context.Context, req *orchestration.CloseSessionRequest) (*orchestration.CloseSessionResponse, error) {
	a.mu.Lock()
	a.calls = append(a.calls, "close "+req.SessionID)
	a.mu.Unlock()
	return &orchestration.CloseSessionResponse{}, nil
}

func (a *closeAgents) recorded() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

// warnLogger records every warning, for the test to search for a secret.
type warnLogger struct {
	interfaces.Logger

	mu    sync.Mutex
	warns []string
}

func (l *warnLogger) Warnf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.warns = append(l.warns, fmt.Sprintf(format, args...))
}

func (l *warnLogger) recorded() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.warns...)
}

// closeJob is a job run over agents that records its audit trail.
func closeJob(agents *closeAgents, log *warnLogger) (*orchestration.JobRun, func() []string) {
	var mu sync.Mutex
	var audits []string
	job := orchestration.NewJobRun(orchestration.JobRunConfig{
		JobID:  "job-1",
		Agents: agents,
		Logger: log,
		AuditFn: func(eventType, details string) {
			if eventType == auditForgeCredential {
				mu.Lock()
				audits = append(audits, details)
				mu.Unlock()
			}
		},
	})
	return job, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), audits...)
	}
}

// TestCloseSession_RevokesBeforeTheContainerGoes: the revocation is an exec
// into the container, so it runs before the close that removes it.
func TestCloseSession_RevokesBeforeTheContainerGoes(t *testing.T) {
	agents := &closeAgents{execFn: func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		return &orchestration.ExecResult{}, nil
	}}
	log := &warnLogger{}
	job, audits := closeJob(agents, log)
	workspace := &containerWorkspace{credential: newForgeCredential([]byte("t0ken"))}

	closeSession(context.Background(), job, "session-1", workspace)

	want := []string{"exec rm -f -- " + orchestration.WorkspaceForgeCredentialFile, "close session-1"}
	if got := agents.recorded(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if len(log.recorded()) != 0 || len(audits()) != 0 {
		t.Fatalf("a clean revocation reported a failure: %q %q", log.recorded(), audits())
	}

	// No session, nothing to revoke or close; no credential, nothing to revoke.
	bare := &closeAgents{}
	bareJob, _ := closeJob(bare, log)
	closeSession(context.Background(), bareJob, "", workspace)
	closeSession(context.Background(), bareJob, "session-2", nil)
	if got := bare.recorded(); len(got) != 1 || got[0] != "close session-2" {
		t.Fatalf("calls = %q, want the close alone", got)
	}
}

// TestCloseSession_ARevocationFailureIsLoggedAndAuditedAndTheCloseGoesAhead:
// the failure reaches the operator — in the log and on the job — with no
// secret in it, and the container is still closed, which frees the tier.
func TestCloseSession_ARevocationFailureIsLoggedAndAuditedAndTheCloseGoesAhead(t *testing.T) {
	agents := &closeAgents{execFn: func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		return &orchestration.ExecResult{ExitCode: 1}, nil
	}}
	log := &warnLogger{}
	job, audits := closeJob(agents, log)
	workspace := &containerWorkspace{credential: newForgeCredential([]byte("t0ken"))}

	closeSession(context.Background(), job, "session-1", workspace)

	if got := agents.recorded(); len(got) != 2 || got[1] != "close session-1" {
		t.Fatalf("calls = %q, want the revocation and then the close", got)
	}
	reported := append(log.recorded(), audits()...)
	if len(log.recorded()) != 1 || len(audits()) != 1 {
		t.Fatalf("logged %q, audited %q; want one of each", log.recorded(), audits())
	}
	for _, line := range reported {
		if !strings.Contains(line, "session-1") || !strings.Contains(line, "not removed") {
			t.Errorf("report %q does not name the session and the failure", line)
		}
		if strings.Contains(line, "t0ken") {
			t.Errorf("report %q carries the token", line)
		}
	}
}
