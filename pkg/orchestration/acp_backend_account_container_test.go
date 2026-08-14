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
func (r *specCapturingContainerRuntime) Stop(context.Context, string) error   { return nil }
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
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode"})
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
