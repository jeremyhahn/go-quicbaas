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

package orchestration

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Sandboxed sessions can be reconstituted, which is what stops sandboxing and
// account rotation from being mutually exclusive.
//
// A sandboxed session's credential is a bind-mounted, per-spawn directory. It
// cannot be rewritten under a running container, so moving one onto a different
// account means building a NEW container around a NEW directory. These tests
// assert exactly that, plus the isolation guarantee it must not weaken: two
// containers never share a materialized credential tree, and the outgoing one
// is deleted.
// =============================================================================

// multiSpecContainerRuntime records EVERY ContainerSpec it is asked to run, so
// a test can compare the mounts of the original container with those of its
// replacement.
type multiSpecContainerRuntime struct {
	t     *testing.T
	specs []*ContainerSpec
}

func (r *multiSpecContainerRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	r.specs = append(r.specs, spec)

	serverRead, clientWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	clientRead, serverWrite, err := os.Pipe()
	if err != nil {
		_ = clientWrite.Close()
		_ = serverRead.Close()
		return nil, err
	}
	done := make(chan struct{})
	go acpMockResponder(r.t, serverRead, serverWrite, done)
	go func() {
		<-ctx.Done()
		close(done)
		_ = serverRead.Close()
		_ = clientWrite.Close()
		_ = clientRead.Close()
	}()
	return &InteractiveProcess{ContainerID: "test-ctr", Stdin: clientWrite, Stdout: clientRead}, nil
}

func (r *multiSpecContainerRuntime) Run(context.Context, *ContainerSpec) (string, error) {
	return "test-ctr", nil
}
func (r *multiSpecContainerRuntime) Stop(context.Context, string) error   { return nil }
func (r *multiSpecContainerRuntime) Remove(context.Context, string) error { return nil }
func (r *multiSpecContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}
func (r *multiSpecContainerRuntime) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}
func (r *multiSpecContainerRuntime) IsAvailable() bool                  { return true }
func (r *multiSpecContainerRuntime) Pull(context.Context, string) error { return nil }

// newSandboxedAccountBackend wires an OpenCode-style (file-credential) backend
// with a two-account pool and a spec-recording container runtime.
func newSandboxedAccountBackend(t *testing.T) (*ACPBackend, *multiSpecContainerRuntime) {
	t.Helper()
	runtime := &multiSpecContainerRuntime{t: t}
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})
	backend.SetContainerRuntime(runtime)

	store := NewInMemoryAccountStore()
	secrets := newTestAccountSecretStore(t)
	ctx := ContextWithTenant(context.Background(), "system")

	for _, id := range []string{"acct-a", "acct-b"} {
		if err := store.Create(ctx, &Account{ID: id, TenantID: "system", Provider: AccountProviderOpenCode}); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
		if err := secrets.Put(ctx, "system", id, &AccountCredential{
			Files: map[string][]byte{".config/opencode/auth.json": []byte(`{"key":"` + id + `-key"}`)},
		}); err != nil {
			t.Fatalf("Put(%s): %v", id, err)
		}
	}
	backend.SetAccountResolver(NewAccountResolver(store,
		NewAccountTokenManager(secrets, store, 60*time.Second), NewAccountStrategyRegistry(nil)))
	return backend, runtime
}

// opencodeMountSource returns the host directory bind-mounted at the
// container's OpenCode config path.
func opencodeMountSource(t *testing.T, spec *ContainerSpec) string {
	t.Helper()
	source, ok := mountTarget(spec, "/home/agent/.config/opencode")
	if !ok {
		t.Fatalf("container spec has no opencode config mount: %+v", spec.Mounts)
	}
	return source
}

func TestACPBackend_ResumeContainer_MaterializesAFreshCredentialDir(t *testing.T) {
	backend, runtime := newSandboxedAccountBackend(t)
	ctx := ContextWithTenant(context.Background(), "system")

	spawnCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	spawnReq := &SpawnRequest{
		WorkDir:        "/tmp",
		Persistent:     true,
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "opencode:test",
		AccountID:      "acct-a",
	}
	sess, err := backend.Spawn(spawnCtx, spawnReq)
	if err != nil {
		t.Fatalf("Spawn(): %v", err)
	}
	if sess.AccountID != "acct-a" {
		t.Fatalf("Spawn().AccountID = %q, want acct-a", sess.AccountID)
	}
	if len(runtime.specs) != 1 {
		t.Fatalf("container runs = %d, want 1", len(runtime.specs))
	}
	firstDir := opencodeMountSource(t, runtime.specs[0])
	assertFileContains(t, firstDir+"/auth.json", "acct-a-key")

	// Rotation closes first: Resume refuses a live session, and the Close is
	// what releases the outgoing account's materialized tree.
	if err := backend.Close(ctx, sess.ID); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	if _, statErr := os.Stat(firstDir); !os.IsNotExist(statErr) {
		t.Fatalf("the outgoing account's credential directory %s survived Close()", firstDir)
	}

	rotation := rotationSpawnRequestFromRecord(&SessionRecord{
		SessionID: sess.ID, WorkDir: "/tmp", Persistent: true,
		SandboxMode: SandboxModeRequired, ContainerImage: "opencode:test",
		AccountID: "acct-a", AccountAuto: true,
	}, "acct-a")

	resumeCtx, resumeCancel := context.WithTimeout(ctx, 5*time.Second)
	defer resumeCancel()
	result, err := backend.Resume(resumeCtx, &ResumeRequest{
		SessionID: sess.ID,
		Spawn:     rotation,
		Transcript: &ReplayTranscript{
			SessionID: sess.ID,
			Turns:     []ReplayTurn{{Role: ReplayRoleUser, Text: "what did we decide?"}},
		},
	})
	if err != nil {
		t.Fatalf("Resume(): %v", err)
	}
	if result.Mode != ResumeModeReplay {
		t.Fatalf("Mode = %q, want replay — a container has no native session store to reload from", result.Mode)
	}
	if result.Session.ID != sess.ID {
		t.Fatalf("resumed session ID = %q, want the operator's original %q", result.Session.ID, sess.ID)
	}
	if result.Session.AccountID != "acct-b" {
		t.Fatalf("resumed AccountID = %q, want acct-b — the rotation excluded acct-a", result.Session.AccountID)
	}

	if len(runtime.specs) != 2 {
		t.Fatalf("container runs = %d, want 2 (a resume builds a new container)", len(runtime.specs))
	}
	secondDir := opencodeMountSource(t, runtime.specs[1])
	if secondDir == firstDir {
		t.Fatalf("the replacement container reused the outgoing account's directory %s", secondDir)
	}
	assertFileContains(t, secondDir+"/auth.json", "acct-b-key")

	// Per-session isolation is preserved: the new tree holds ONLY the new
	// account's material.
	if data, readErr := os.ReadFile(secondDir + "/auth.json"); readErr == nil {
		if strings.Contains(string(data), "acct-a-key") {
			t.Fatalf("the replacement credential tree carries the outgoing account's material: %s", data)
		}
	}

	// Closing the resumed session releases the replacement tree too.
	if err := backend.Close(ctx, sess.ID); err != nil {
		t.Fatalf("Close() after resume: %v", err)
	}
	if _, statErr := os.Stat(secondDir); !os.IsNotExist(statErr) {
		t.Fatalf("the replacement credential directory %s leaked", secondDir)
	}
}

// TestACPBackend_ResumeContainer_KeepsTheOperatorsSessionID guards the identity
// split a container resume creates: the fresh container reports its own ACP
// session id, and prompts must be addressed to THAT while the registry keeps
// the operator's.
func TestACPBackend_ResumeContainer_KeepsTheOperatorsSessionID(t *testing.T) {
	backend, _ := newSandboxedAccountBackend(t)
	ctx := ContextWithTenant(context.Background(), "system")

	resumeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := backend.Resume(resumeCtx, &ResumeRequest{
		SessionID: "operator-session-id",
		Spawn: &SpawnRequest{
			WorkDir: "/tmp", Persistent: true,
			SandboxMode: SandboxModeRequired, ContainerImage: "opencode:test",
			AccountID: "acct-a",
		},
		Transcript: &ReplayTranscript{
			SessionID: "operator-session-id",
			Turns:     []ReplayTurn{{Role: ReplayRoleUser, Text: "p"}},
		},
	})
	if err != nil {
		t.Fatalf("Resume(): %v", err)
	}
	if result.Session.ID != "operator-session-id" {
		t.Fatalf("registered ID = %q, want operator-session-id", result.Session.ID)
	}

	live := liveACPSession(t, backend, "operator-session-id")
	if live.agentID == "" || live.agentID == "operator-session-id" {
		t.Fatalf("agentID = %q, want the container's own ACP session id", live.agentID)
	}
	if live.promptSessionID() != live.agentID {
		t.Fatalf("promptSessionID() = %q, want the agent's own id %q — otherwise session/prompt addresses a session the agent never created",
			live.promptSessionID(), live.agentID)
	}
	_ = backend.Close(ctx, "operator-session-id")
}

func TestACPBackend_Resume_RejectsAnUnsandboxedJSONRPCSession(t *testing.T) {
	backend, _, _, _, cleanup := newTestACPBackendWithAccountPool(t)
	defer cleanup()

	_, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID:  "s1",
		Spawn:      &SpawnRequest{WorkDir: "/tmp", Persistent: true},
		Transcript: &ReplayTranscript{SessionID: "s1", Turns: []ReplayTurn{{Role: ReplayRoleUser, Text: "p"}}},
	})
	var unsupported *SessionResumeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Resume() = %v, want SessionResumeUnsupportedError — a host JSON-RPC agent can restore nothing", err)
	}
}

func TestACPBackend_Resume_RejectsANonPersistentSession(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})

	_, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID: "s1",
		Spawn:     &SpawnRequest{WorkDir: "/tmp"},
	})
	var unsupported *SessionResumeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Resume() = %v, want SessionResumeUnsupportedError", err)
	}
	if !strings.Contains(unsupported.Reason, "non-persistent") {
		t.Fatalf("Reason = %q, want it to name the non-persistent session", unsupported.Reason)
	}
}

func TestACPBackend_Resume_SandboxedWithoutARuntimeIsRefused(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})

	_, err := backend.Resume(context.Background(), &ResumeRequest{
		SessionID: "s1",
		Spawn: &SpawnRequest{
			WorkDir: "/tmp", Persistent: true,
			SandboxMode: SandboxModeRequired, ContainerImage: "opencode:test",
		},
	})
	var unsupported *SessionResumeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("Resume() = %v, want SessionResumeUnsupportedError", err)
	}
	if !strings.Contains(unsupported.Reason, "container runtime") {
		t.Fatalf("Reason = %q, want it to name the missing container runtime", unsupported.Reason)
	}
}

// assertFileContains fails unless path exists and holds want.
func assertFileContains(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if !strings.Contains(string(data), want) {
		t.Fatalf("%s = %q, want it to contain %q", path, data, want)
	}
}
