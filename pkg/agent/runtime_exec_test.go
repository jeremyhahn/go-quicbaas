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

package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// execRuntime is THIS daemon's container runtime in the exec tests: it
// records every command run in one of its containers and answers what the
// test scripted.
type execRuntime struct {
	*fakeContainerRuntime

	mu     sync.Mutex
	execs  []string
	stdout string
	// deadline records whether each command ran under a deadline.
	deadline []bool
}

func (e *execRuntime) Exec(ctx context.Context, containerID string, spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, hasDeadline := ctx.Deadline()
	e.deadline = append(e.deadline, hasDeadline)
	e.execs = append(e.execs, containerID+":"+strings.Join(spec.Cmd, " "))
	return &orchestration.ExecResult{ExitCode: 3, Stdout: e.stdout, Stderr: "said something"}, nil
}

// containerBackend spawns sessions that run in a named container.
func containerBackend() *spawnBackend {
	return &spawnBackend{spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
		return &orchestration.Session{
			ID: "sess-exec", Backend: "acp", Status: orchestration.SessionStateActive,
			WorkDir: req.WorkDir, ContainerID: "ctr-agent-1", CreatedAt: time.Now().UnixNano(),
		}, nil
	}}
}

// TestExecSession_TheControlPlaneRunsCommandsInTheAgentsContainer is round
// 6's P1 across the real boundary: a control plane's RemoteACPBackend runs a
// checkout command in a session the AGENT spawned, and it lands in the
// agent's container runtime, in that session's container, under a deadline —
// with a non-zero exit carried back as an answer, not an error.
func TestExecSession_TheControlPlaneRunsCommandsInTheAgentsContainer(t *testing.T) {
	t.Parallel()
	agentRuntime := &execRuntime{fakeContainerRuntime: &fakeContainerRuntime{available: true}, stdout: "cloned"}
	remote, cleanup := newContractBoundary(t, containerBackend(), &orchestration.MountPolicy{}, agentRuntime, nil)
	defer cleanup()

	sess, err := remote.Spawn(context.Background(), &orchestration.SpawnRequest{ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("remote spawn: %v", err)
	}
	result, err := remote.ExecInSession(context.Background(), sess.ID, &orchestration.ExecSpec{
		Cmd: []string{"git", "clone", "repo"}, WorkDir: "/workspace",
	})
	if err != nil {
		t.Fatalf("ExecInSession: %v", err)
	}
	if result.ExitCode != 3 || result.Stdout != "cloned" || result.Stderr != "said something" {
		t.Fatalf("result = %+v, want the agent's answer", result)
	}
	agentRuntime.mu.Lock()
	defer agentRuntime.mu.Unlock()
	if len(agentRuntime.execs) != 1 || agentRuntime.execs[0] != "ctr-agent-1:git clone repo" {
		t.Fatalf("agent execs = %v, want the command in the session's own container", agentRuntime.execs)
	}
	if !agentRuntime.deadline[0] {
		t.Fatal("the command ran with no deadline")
	}
}

// TestExecSession_RefusesWhatTheCallerDoesNotOwn: a session this daemon never
// spawned, one of another tenant, and one running no container are refused,
// and none of them reaches the container runtime.
func TestExecSession_RefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	t.Parallel()
	agentRuntime := &execRuntime{fakeContainerRuntime: &fakeContainerRuntime{available: true}}
	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": containerBackend()},
		SandboxPolicy:    orchestration.SandboxPolicyOptIn,
		ContainerRuntime: agentRuntime,
		MountPolicy:      &orchestration.MountPolicy{},
		Logger:           &testLogger{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.Spawn(context.Background(), "tenant-a", nil, "acp", &orchestration.SpawnRequest{})
	if err != nil {
		t.Fatal(err)
	}
	spec := &orchestration.ExecSpec{Cmd: []string{"id"}}

	var notFound *orchestration.SessionNotFoundError
	if _, _, err := rt.ExecSession(context.Background(), "tenant-a", "never-spawned", spec); !errors.As(err, &notFound) {
		t.Fatalf("unknown session: %v", err)
	}
	if _, _, err := rt.ExecSession(context.Background(), "tenant-b", sess.ID, spec); !errors.As(err, &notFound) {
		t.Fatalf("another tenant's session: %v, want it answered as not found", err)
	}
	if _, _, err := rt.ExecSession(context.Background(), "tenant-a", sess.ID, &orchestration.ExecSpec{}); err == nil {
		t.Fatal("an empty command was run")
	}
	if err := rt.Close(context.Background(), "acp", sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := rt.ExecSession(context.Background(), "tenant-a", sess.ID, spec); !errors.As(err, &notFound) {
		t.Fatalf("a closed session: %v", err)
	}
	agentRuntime.mu.Lock()
	defer agentRuntime.mu.Unlock()
	if len(agentRuntime.execs) != 0 {
		t.Fatalf("a refused command reached the runtime: %v", agentRuntime.execs)
	}

	hostProcess := &spawnBackend{}
	bare, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": hostProcess}, SandboxPolicy: orchestration.SandboxPolicyOptIn,
		MountPolicy: &orchestration.MountPolicy{}, Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bare.Spawn(context.Background(), "tenant-a", nil, "acp", &orchestration.SpawnRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bare.ExecSession(context.Background(), "tenant-a", "sess-1", spec); !errors.As(err, &notFound) {
		t.Fatalf("a host-process session has no container to find: %v", err)
	}
}

// TestExecSession_OutputIsBounded keeps the end of an oversized output and
// says it cut.
func TestExecSession_OutputIsBounded(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("a", MaxExecOutputBytes) + "the reason it failed"
	out, cut := boundOutput(big)
	if !cut || len(out) != MaxExecOutputBytes || !strings.HasSuffix(out, "the reason it failed") {
		t.Fatalf("bounded = %d bytes, cut=%v", len(out), cut)
	}
	if out, cut := boundOutput("small"); cut || out != "small" {
		t.Fatalf("a small output was touched: %q %v", out, cut)
	}
}

// execOverTheWire spawns a session on a real agent boundary whose container
// runtime answers stdout, runs one command in it through the control plane's
// RemoteACPBackend, and returns what came back.
func execOverTheWire(t *testing.T, stdout string) *orchestration.ExecResult {
	t.Helper()
	agentRuntime := &execRuntime{fakeContainerRuntime: &fakeContainerRuntime{available: true}, stdout: stdout}
	remote, cleanup := newContractBoundary(t, containerBackend(), &orchestration.MountPolicy{}, agentRuntime, nil)
	t.Cleanup(cleanup)
	sess, err := remote.Spawn(context.Background(), &orchestration.SpawnRequest{ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("remote spawn: %v", err)
	}
	result, err := remote.ExecInSession(context.Background(), sess.ID, &orchestration.ExecSpec{Cmd: []string{"cat", "out"}})
	if err != nil {
		t.Fatalf("ExecInSession: %v — the response did not survive the wire", err)
	}
	return result
}

// TestExecSession_OutputThatIsNotUTF8CrossesTheWire is round 7's first
// regression: a command whose output is not UTF-8 — a binary blob, a latin-1
// log — is carried byte for byte, and its exit status with it. As a protobuf
// string the response could not be serialized and both were lost.
func TestExecSession_OutputThatIsNotUTF8CrossesTheWire(t *testing.T) {
	t.Parallel()
	raw := "caf\xe9 \xff\xfe binary\x00tail"
	if utf8.ValidString(raw) {
		t.Fatal("the fixture must not be UTF-8")
	}
	result := execOverTheWire(t, raw)
	if result.ExitCode != 3 {
		t.Fatalf("exit status = %d, want 3", result.ExitCode)
	}
	if result.Stdout != raw {
		t.Fatalf("stdout = %q, want the bytes the command wrote %q", result.Stdout, raw)
	}
}

// TestExecSession_TruncationMidCharacterCrossesTheWire is round 7's second
// regression: output over the bound whose cut would land inside a multibyte
// character is cut at the next character boundary instead, stays valid UTF-8,
// keeps its end, and crosses the wire with its exit status.
func TestExecSession_TruncationMidCharacterCrossesTheWire(t *testing.T) {
	t.Parallel()
	// "€" is three bytes. With one leading byte the cut at len-Max lands on
	// the euro's second byte.
	out := "x" + strings.Repeat("\u20ac", MaxExecOutputBytes/3) + "the reason it failed"
	cut := len(out) - MaxExecOutputBytes
	if utf8.RuneStart(out[cut]) {
		t.Fatalf("the fixture's cut at %d must land mid-character", cut)
	}
	result := execOverTheWire(t, out)
	if result.ExitCode != 3 {
		t.Fatalf("exit status = %d, want 3", result.ExitCode)
	}
	if !utf8.ValidString(result.Stdout) {
		t.Fatal("the truncated output is no longer valid UTF-8")
	}
	if len(result.Stdout) > MaxExecOutputBytes || !strings.HasSuffix(result.Stdout, "the reason it failed") ||
		!strings.HasPrefix(result.Stdout, "\u20ac") {
		t.Fatalf("truncated to %d bytes starting %q", len(result.Stdout), result.Stdout[:3])
	}
}
