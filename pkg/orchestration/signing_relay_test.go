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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
)

// serveTestAgent serves keyring as an ssh agent on a socket under a short
// private directory, the way the daemon's session agent is served.
func serveTestAgent(t *testing.T, keyring sshagent.Agent) string {
	t.Helper()
	socket := filepath.Join(testSigningSocketsRoot(t), WorkspaceSigningAgentSocketFile)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close agent listener: %v", err)
		}
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				// The client hanging up — EOF — is how every exchange ends.
				if serveErr := sshagent.ServeAgent(keyring, conn); serveErr != nil && !errors.Is(serveErr, io.EOF) {
					t.Logf("test agent exchange ended: %v", serveErr)
				}
			}()
		}
	}()
	return socket
}

// testSessionAgent is a daemon session agent holding one fresh key.
func testSessionAgent(t *testing.T) (string, ssh.PublicKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := sshagent.NewKeyring()
	if err := keyring.Add(sshagent.AddedKey{PrivateKey: private}); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return serveTestAgent(t, keyring), signer.PublicKey()
}

type recordingRelayAuditor struct {
	mu     sync.Mutex
	events []SigningRelaySignEvent
}

func (a *recordingRelayAuditor) RecordSigningRelaySign(_ context.Context, event SigningRelaySignEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

// TestRelaySigningFor_RelaysOnlyWhereTheSocketCannotBeMounted: an embedded
// agent and one advertising this daemon's trait mount the socket directly; an
// agent advertising the relay and not the trait is relayed to; an agent with
// neither is left to refuse the mount.
func TestRelaySigningFor_RelaysOnlyWhereTheSocketCannotBeMounted(t *testing.T) {
	trait := SigningAgentTrait("0123456789abcdef0123456789abcdef")
	embedded := &FleetNode{Kind: FleetNodeKindEmbedded, Capabilities: []string{FleetNodeTraitSigningRelay}}
	colocated := &FleetNode{Kind: FleetNodeKindDaemon, Capabilities: []string{trait, FleetNodeTraitSigningRelay}}
	remote := &FleetNode{Kind: FleetNodeKindDaemon, Capabilities: []string{FleetNodeTraitSigningRelay}}
	neither := &FleetNode{Kind: FleetNodeKindDaemon}

	for name, tc := range map[string]struct {
		reg   *FleetNode
		trait string
		want  bool
	}{
		"embedded":               {embedded, trait, false},
		"co-located":             {colocated, trait, false},
		"remote relay":           {remote, trait, true},
		"remote, no trait known": {remote, "", true},
		"neither":                {neither, trait, false},
		"no registration":        {nil, trait, false},
	} {
		if got := relaySigningFor(tc.reg, tc.trait); got != tc.want {
			t.Errorf("%s: relaySigningFor = %v, want %v", name, got, tc.want)
		}
	}
}

// TestWithoutSigningMount_DropsOnlyTheSigningSocket keeps every other mount in
// order.
func TestWithoutSigningMount_DropsOnlyTheSigningSocket(t *testing.T) {
	mounts := []ContainerMount{
		{Source: "/a", Target: "/mnt/a"},
		{Source: "/s/agent.sock", Target: WorkspaceSigningAgentSocket, ReadOnly: true},
		{Source: "/b", Target: "/mnt/b"},
	}
	got := WithoutSigningMount(mounts)
	if len(got) != 2 || got[0].Target != "/mnt/a" || got[1].Target != "/mnt/b" {
		t.Fatalf("WithoutSigningMount = %+v", got)
	}
	if got := WithoutSigningMount(nil); len(got) != 0 {
		t.Fatalf("WithoutSigningMount(nil) = %+v", got)
	}
}

// TestListSigningAgentKeys_ReadsTheSessionAgent: the keys a relay advertises
// are read off the session's own agent; an agent that is not there, or holds
// nothing, fails typed.
func TestListSigningAgentKeys_ReadsTheSessionAgent(t *testing.T) {
	socket, key := testSessionAgent(t)
	keys, err := listSigningAgentKeys(context.Background(), socket)
	if err != nil || len(keys) != 1 || string(keys[0]) != string(key.Marshal()) {
		t.Fatalf("keys = %v, %v; want the session's one key", keys, err)
	}

	var keysErr *SigningRelayKeysError
	if _, err := listSigningAgentKeys(context.Background(), filepath.Join(testSigningSocketsRoot(t), "gone.sock")); !errors.As(err, &keysErr) {
		t.Fatalf("a missing agent = %v, want *SigningRelayKeysError", err)
	}
	empty := serveTestAgent(t, sshagent.NewKeyring())
	if _, err := listSigningAgentKeys(context.Background(), empty); !errors.As(err, &keysErr) || !strings.Contains(err.Error(), "holds no key") {
		t.Fatalf("an empty agent = %v, want *SigningRelayKeysError naming it", err)
	}
}

// TestSigningRelayServer_SignsOnlyForItsOwnSessionTenantAndKey: a request for
// the session's own tenant, session and advertised key is signed by the
// session's agent and audited with the data's digest; any other is refused
// typed and audited as a refusal. The data itself is never in the audit.
func TestSigningRelayServer_SignsOnlyForItsOwnSessionTenantAndKey(t *testing.T) {
	socket, key := testSessionAgent(t)
	_, otherPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherSigner, err := ssh.NewSignerFromKey(otherPrivate)
	if err != nil {
		t.Fatal(err)
	}
	audit := &recordingRelayAuditor{}
	server := newSigningRelayServer("tenant-a", "sess-1", "agent-2", socket, [][]byte{key.Marshal()}, audit, &agentTestLogger{})
	data := []byte("commit payload")

	result := server.sign(context.Background(), &proto.SigningRelaySign{
		Id: 7, TenantId: "tenant-a", SessionId: "sess-1", KeyBlob: key.Marshal(), Data: data,
	})
	if result.GetId() != 7 || result.GetError() != "" {
		t.Fatalf("result = %+v, want a signature for request 7", result)
	}
	signature := new(ssh.Signature)
	if err := ssh.Unmarshal(result.GetSignature(), signature); err != nil {
		t.Fatal(err)
	}
	if err := key.Verify(data, signature); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}

	for name, req := range map[string]*proto.SigningRelaySign{
		"another tenant":      {TenantId: "tenant-b", SessionId: "sess-1", KeyBlob: key.Marshal(), Data: data},
		"another session":     {TenantId: "tenant-a", SessionId: "sess-9", KeyBlob: key.Marshal(), Data: data},
		"an unadvertised key": {TenantId: "tenant-a", SessionId: "sess-1", KeyBlob: otherSigner.PublicKey().Marshal(), Data: data},
		"an unparseable key":  {TenantId: "tenant-a", SessionId: "sess-1", KeyBlob: []byte("junk"), Data: data},
	} {
		if refused := server.sign(context.Background(), req); refused.GetError() == "" || len(refused.GetSignature()) != 0 {
			t.Errorf("%s: result = %+v, want a refusal", name, refused)
		}
	}
	var mismatch *SigningRelayMismatchError
	if _, err := server.signChecked(context.Background(), &proto.SigningRelaySign{TenantId: "tenant-b"}, key, nil); !errors.As(err, &mismatch) || mismatch.Field != "tenant" {
		t.Fatalf("tenant mismatch = %v, want *SigningRelayMismatchError on tenant", err)
	}

	audit.mu.Lock()
	defer audit.mu.Unlock()
	if len(audit.events) != 5 {
		t.Fatalf("audited %d events, want every request", len(audit.events))
	}
	digest := sha256.Sum256(data)
	first := audit.events[0]
	if !first.Success || first.DataSHA256 != hex.EncodeToString(digest[:]) || first.KeyFingerprint != ssh.FingerprintSHA256(key) ||
		first.SessionID != "sess-1" || first.TenantID != "tenant-a" || first.AgentID != "agent-2" {
		t.Fatalf("audit = %+v", first)
	}
	for _, event := range audit.events[1:] {
		if event.Success || event.Reason == "" {
			t.Errorf("a refusal was audited as %+v", event)
		}
		if strings.Contains(event.Reason, string(data)) {
			t.Errorf("the audit carries the signed data: %+v", event)
		}
	}
}

// TestSigningRelayServer_AStoppedSessionAgentFailsTheSign: the relay dials the
// session's agent per signature, so an agent the daemon stopped answers
// nothing.
func TestSigningRelayServer_AStoppedSessionAgentFailsTheSign(t *testing.T) {
	_, key := testSessionAgent(t)
	server := newSigningRelayServer("tenant-a", "sess-1", "agent-2", filepath.Join(testSigningSocketsRoot(t), "stopped.sock"),
		[][]byte{key.Marshal()}, nil, &agentTestLogger{})
	_, err := server.signChecked(context.Background(),
		&proto.SigningRelaySign{TenantId: "tenant-a", SessionId: "sess-1", KeyBlob: key.Marshal()}, key, nil)
	var signErr *SigningRelaySignError
	if !errors.As(err, &signErr) || !errors.Is(err, ErrSigningRelay) {
		t.Fatalf("sign = %v, want *SigningRelaySignError", err)
	}
}

// TestRemoteACPBackend_AnAgentThatDoesNotAttachFailsTheSpawn: the relay is
// attached before Spawn returns, so an agent that will not attach it fails the
// spawn — typed — and the session it spawned is closed rather than left
// running with no way to sign.
func TestRemoteACPBackend_AnAgentThatDoesNotAttachFailsTheSpawn(t *testing.T) {
	socket, _ := testSessionAgent(t)
	daemon := startBufconnDaemon(t, nil) // SigningRelay is unimplemented there
	reg := newDaemonRegistration()
	reg.Capabilities = []string{"acp", FleetNodeTraitSigningRelay}
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg, Backend: "acp", Logger: &agentTestLogger{}, ConnDialer: daemon.dialer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.CloseAll(); err != nil {
			t.Errorf("close pool: %v", err)
		}
	})

	_, err = backend.Spawn(context.Background(), &SpawnRequest{ExtraMounts: []ContainerMount{{
		Source: socket, Target: WorkspaceSigningAgentSocket, ReadOnly: true,
	}}})
	var openErr *SigningRelayOpenError
	if !errors.As(err, &openErr) || openErr.SessionID != "daemon-sess-1" {
		t.Fatalf("spawn = %v, want *SigningRelayOpenError for the spawned session", err)
	}
	if got := daemon.svc.closeCalls.Load(); got != 1 {
		t.Fatalf("the unattachable session was closed %d times, want once", got)
	}
	sent := daemon.svc.lastSpawnReq
	if sent.GetSigningRelay() == nil || len(sent.GetSigningRelay().GetPublicKeys()) != 1 {
		t.Fatalf("the spawn carried relay %+v, want the session's one key", sent.GetSigningRelay())
	}
	for _, mount := range sent.GetExtraMounts() {
		if mount.GetTarget() == WorkspaceSigningAgentSocket {
			t.Fatal("the daemon's socket path was still sent to an agent that cannot mount it")
		}
	}
}

// TestRemoteACPBackend_ACoLocatedAgentKeepsTheDirectMount: an agent that
// advertises this daemon's signing trait can mount the socket, so it is sent
// the mount and no relay.
func TestRemoteACPBackend_ACoLocatedAgentKeepsTheDirectMount(t *testing.T) {
	trait := SigningAgentTrait("0123456789abcdef0123456789abcdef")
	daemon := startBufconnDaemon(t, nil)
	reg := newDaemonRegistration()
	reg.Capabilities = []string{"acp", trait, FleetNodeTraitSigningRelay}
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: reg, Backend: "acp", Logger: &agentTestLogger{}, ConnDialer: daemon.dialer(),
		SigningTrait: func() string { return trait },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := backend.CloseAll(); err != nil {
			t.Errorf("close pool: %v", err)
		}
	})

	if _, err := backend.Spawn(context.Background(), &SpawnRequest{ExtraMounts: []ContainerMount{{
		Source: "/run/quicbaas/x/agent.sock", Target: WorkspaceSigningAgentSocket, ReadOnly: true,
	}}}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	sent := daemon.svc.lastSpawnReq
	if sent.GetSigningRelay() != nil {
		t.Fatal("a co-located agent was sent a relay")
	}
	if len(sent.GetExtraMounts()) != 1 || sent.GetExtraMounts()[0].GetTarget() != WorkspaceSigningAgentSocket {
		t.Fatalf("mounts = %+v, want the direct signing mount", sent.GetExtraMounts())
	}
}
