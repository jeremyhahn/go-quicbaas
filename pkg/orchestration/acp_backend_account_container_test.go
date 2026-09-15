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

// specCapturingContainerRuntime is a minimal ContainerRuntime fake that
// records the ContainerSpec passed to RunInteractive (in particular its
// Mounts) and returns a real ACP JSON-RPC responder over pipes, exactly like
// ctxBoundContainerRuntime in acp_backend_container_lifecycle_test.go. A
// dedicated type is used here instead of reusing that one so this test's
// assertions (spec.Mounts) do not depend on a field owned by a different
// test's regression fixture.
type specCapturingContainerRuntime struct {
	t       *testing.T
	gotSpec *ContainerSpec
}

func (r *specCapturingContainerRuntime) RunInteractive(ctx context.Context, spec *ContainerSpec) (*InteractiveProcess, error) {
	r.gotSpec = spec

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

func (r *specCapturingContainerRuntime) Run(context.Context, *ContainerSpec) (string, error) {
	return "test-ctr", nil
}
func (r *specCapturingContainerRuntime) Stop(context.Context, string) error { return nil }

// Exec satisfies the runtime seam. Nothing in this test runs a command
// inside a container; a call would be a test asserting behaviour it never
// arranged, so it refuses rather than answering.
func (r *specCapturingContainerRuntime) Exec(context.Context, string, *ExecSpec) (*ExecResult, error) {
	return nil, &ContainerExecError{ContainerID: "fake", Cause: errNoExecInThisTest}
}
func (r *specCapturingContainerRuntime) Remove(context.Context, string) error { return nil }
func (r *specCapturingContainerRuntime) Logs(context.Context, string) (io.ReadCloser, error) {
	return nil, nil
}
func (r *specCapturingContainerRuntime) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}
func (r *specCapturingContainerRuntime) IsAvailable() bool                  { return true }
func (r *specCapturingContainerRuntime) Pull(context.Context, string) error { return nil }

func mountTarget(spec *ContainerSpec, target string) (string, bool) {
	for _, m := range spec.Mounts {
		if m.Target == target {
			return m.Source, true
		}
	}
	return "", false
}

// TestACPBackend_SpawnContainer_FileBasedAccount_MountsIsolatedDir proves a
// sandboxed spawn that resolves a file-credential account (e.g. OpenCode)
// mounts ONLY that account's freshly materialized directory — never the
// operator's shared ~/.config/opencode — at the container's expected config
// path.
func TestACPBackend_SpawnContainer_FileBasedAccount_MountsIsolatedDir(t *testing.T) {
	runtime := &specCapturingContainerRuntime{t: t}
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode",
		// Where OpenCode's file credential lives. It used to be hardcoded
		// inside the backend; it is configuration now, so a test that
		// asserts the mount has to state it (see CredentialDirSpec).
		CredentialDir: CredentialDirSpec{Rel: ".config/opencode", Target: "/home/agent/.config/opencode"}})
	backend.SetContainerRuntime(runtime)

	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	ctx := ContextWithTenant(context.Background(), "system")

	account := &Account{ID: "acct-1", TenantID: "system", Provider: AccountProviderOpenCode}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := secretStore.Put(ctx, "system", "acct-1", &AccountCredential{
		Files: map[string][]byte{".config/opencode/auth.json": []byte(`{"anthropic":{"apiKey":"acct-1-key"}}`)},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	backend.SetAccountResolver(NewAccountResolver(store, NewAccountTokenManager(secretStore, store, 60*time.Second), NewAccountStrategyRegistry(nil)))

	spawnCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	sess, err := backend.Spawn(spawnCtx, &SpawnRequest{
		WorkDir:        "/tmp",
		AccountID:      "acct-1",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "opencode:test",
	})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	defer backend.Close(context.Background(), sess.ID)

	source, ok := mountTarget(runtime.gotSpec, "/home/agent/.config/opencode")
	if !ok {
		t.Fatalf("no mount found at /home/agent/.config/opencode; mounts = %+v", runtime.gotSpec.Mounts)
	}
	// The mounted source must be the per-session materialized directory,
	// never a path under the real operator home directory.
	if home, _ := os.UserHomeDir(); home != "" && strings.HasPrefix(source, home) {
		t.Fatalf("mount source = %s, leaked the operator's real home directory instead of an isolated one", source)
	}

	content, readErr := os.ReadFile(source + "/auth.json")
	if readErr != nil {
		t.Fatalf("ReadFile(%s/auth.json) error = %v", source, readErr)
	}
	if string(content) != `{"anthropic":{"apiKey":"acct-1-key"}}` {
		t.Fatalf("mounted auth.json content = %s, want acct-1's own credential", content)
	}
}

// TestACPBackend_SpawnContainer_RefusesWhenTheCredentialDirDidNotMount is the
// regression test for a spawn that had everything it needed except the mount,
// and said so nowhere.
//
// A POOLED ACCOUNT'S FILES MUST BE KEYED UNDER CredentialDirSpec.Rel —
// materializeAccountFiles writes each relative path verbatim under the account
// home, and the backend mounts <home>/<Rel>. Files stored at the root instead
// produce a home directory with no <Rel> in it, so nothing mounts.
//
// The backend used to set the CLI's HomeEnv anyway, pointing it at a container
// path nothing had mounted. Measured on the burn-in fleet 2026-09-15: codex
// answered `CODEX_HOME points to "/home/agent/.codex", but that path does not
// exist`, produced no turn, and the runner reported only "(9882 bytes of
// stderr withheld)". Every review failed that way for hours while the account
// read `active` and the credential resolved.
func TestACPBackend_SpawnContainer_RefusesWhenTheCredentialDirDidNotMount(t *testing.T) {
	runtime := &specCapturingContainerRuntime{t: t}
	backend := NewACPBackend(ACPBackendConfig{Command: "codex",
		CredentialDir: CredentialDirSpec{Rel: ".codex", Target: "/home/agent/.codex", HomeEnv: "CODEX_HOME"}})
	backend.SetContainerRuntime(runtime)

	store := NewInMemoryAccountStore()
	secretStore := newTestAccountSecretStore(t)
	ctx := ContextWithTenant(context.Background(), "system")

	account := &Account{ID: "acct-2", TenantID: "system", Provider: AccountProviderCodex}
	if err := store.Create(ctx, account); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// Keyed at the ROOT rather than under ".codex/" — the mistake this refuses.
	if err := secretStore.Put(ctx, "system", "acct-2", &AccountCredential{
		Files: map[string][]byte{"auth.json": []byte(`{"tokens":{}}`)},
	}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	backend.SetAccountResolver(NewAccountResolver(store, NewAccountTokenManager(secretStore, store, 60*time.Second), NewAccountStrategyRegistry(nil)))

	spawnCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := backend.Spawn(spawnCtx, &SpawnRequest{
		WorkDir:        "/tmp",
		AccountID:      "acct-2",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "worker:test",
	})
	if err == nil {
		t.Fatal("a spawn whose credential directory did not mount must be refused, not started with an env var pointing at nothing")
	}
	var missing *CredentialDirMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("want CredentialDirMissingError, got %T: %v", err, err)
	}
	// The refusal has to say what to repair: the relative path the files
	// should have been keyed under.
	if !strings.Contains(missing.Error(), ".codex") {
		t.Fatalf("the refusal must name the credential directory: %s", missing.Error())
	}
}
