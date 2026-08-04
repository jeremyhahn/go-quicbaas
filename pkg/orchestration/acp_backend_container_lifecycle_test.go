package orchestration

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// ctxBoundContainerRuntime is a ContainerRuntime fake whose interactive
// container is bound to the context passed to RunInteractive — exactly as the
// production ExecContainerRuntime binds `docker run -i --rm` via
// exec.CommandContext. When that context is cancelled, the container's stdio
// pipes close (the agent process dies). This lets a test distinguish a
// container bound to the Spawn request ctx (the pre-fix bug, where the
// follow-up Prompt finds a closed transport) from one detached onto its own
// lifetime (the fix).
type ctxBoundContainerRuntime struct {
	t      *testing.T
	gotCtx context.Context
}

func (r *ctxBoundContainerRuntime) RunInteractive(ctx context.Context, _ *ContainerSpec) (*InteractiveProcess, error) {
	r.gotCtx = ctx

	// Pipe pair wiring a mock ACP responder to the client transport, mirroring
	// mockTransportFactory.
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

	// Mirror exec.CommandContext: cancelling the context the container was
	// started with kills the process and tears down its stdio.
	go func() {
		<-ctx.Done()
		close(done)
		_ = serverRead.Close()
		_ = clientWrite.Close()
		_ = clientRead.Close()
	}()

	return &InteractiveProcess{ContainerID: "test-ctr", Stdin: clientWrite, Stdout: clientRead}, nil
}

func (r *ctxBoundContainerRuntime) Run(context.Context, *ContainerSpec) (string, error) {
	return "test-ctr", nil
}
func (r *ctxBoundContainerRuntime) Stop(context.Context, string) error   { return nil }
func (r *ctxBoundContainerRuntime) Remove(context.Context, string) error { return nil }
func (r *ctxBoundContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}
func (r *ctxBoundContainerRuntime) IsRunning(context.Context, string) (bool, error) { return true, nil }
func (r *ctxBoundContainerRuntime) IsAvailable() bool                               { return true }
func (r *ctxBoundContainerRuntime) Pull(context.Context, string) error              { return nil }

// TestSpawnContainerJSONRPC_DetachesContainerFromRequestCtx is a regression test
// for the sandboxed-session "acp: session closed" bug: spawnContainerJSONRPC
// passed the Spawn request ctx to RunInteractive, so gRPC cancelling that ctx
// the instant Spawn returned killed the container — and the follow-up Prompt
// hit a closed stdio transport. The persistent CLI path (spawnCLI) already
// detached its subprocess via context.WithoutCancel; the container path must
// do the same.
func TestSpawnContainerJSONRPC_DetachesContainerFromRequestCtx(t *testing.T) {
	rt := &ctxBoundContainerRuntime{t: t}
	backend := NewACPBackend(ACPBackendConfig{
		Command:      "opencode",
		Args:         []string{"acp"},
		DefaultModel: "test-model",
	})
	backend.SetContainerRuntime(rt)

	spawnCtx, cancelSpawn := context.WithCancel(context.Background())
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{
		ProjectID:      "p",
		WorkDir:        "/tmp",
		Model:          "test-model",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "img:latest",
	})
	if err != nil {
		cancelSpawn()
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() { _ = backend.Close(context.Background(), sess.ID) })

	// Simulate gRPC cancelling the Spawn RPC ctx the moment Spawn returns.
	cancelSpawn()

	// The container ctx must be detached from the request ctx, so cancelling the
	// request does not cancel the container.
	if rt.gotCtx.Err() != nil {
		t.Fatalf("container ctx was cancelled with the Spawn request ctx: %v — "+
			"the container is bound to the request lifetime (regression)", rt.gotCtx.Err())
	}

	// And a follow-up Prompt (a fresh request) must still reach the live agent.
	promptCtx, cancelPrompt := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelPrompt()
	resp, err := backend.Prompt(promptCtx, &PromptRequest{SessionID: sess.ID, Prompt: "ping"})
	if err != nil {
		t.Fatalf("Prompt after Spawn-ctx cancel failed (container died with the request): %v", err)
	}
	if resp == nil {
		t.Fatal("Prompt returned nil response")
	}

	// Close must tear the container down (fire procCancel), cancelling the ctx.
	_ = backend.Close(context.Background(), sess.ID)
	if rt.gotCtx.Err() == nil {
		t.Error("Close did not cancel the container ctx — procCancel not wired to the session")
	}
}
