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

package fleet

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"golang.org/x/crypto/ssh"
	sshagent "golang.org/x/crypto/ssh/agent"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The harness: a control plane and a fleet node on separate runtimes
// =============================================================================

// shortDir is a private directory short enough to bind a unix socket under.
// It is NOT t.TempDir: that path carries the test's name and overruns the
// kernel's socket budget on its own.
func shortDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove %s: %v", dir, err)
		}
	})
	return dir
}

// recordingRelayAudit records every relayed signature the control plane
// audits.
type recordingRelayAudit struct {
	mu     sync.Mutex
	events []orchestration.SigningRelaySignEvent
}

func (a *recordingRelayAudit) RecordSigningRelaySign(_ context.Context, event orchestration.SigningRelaySignEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *recordingRelayAudit) recorded() []orchestration.SigningRelaySignEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]orchestration.SigningRelaySignEvent(nil), a.events...)
}

// relayHarness is the whole relay: the control plane's KMS-backed session
// agent (an in-memory key standing in for the KMS) on the "daemon" side, a
// RemoteACPBackend dialing a fleet node's spawn server over bufconn, and the
// fleet node's runtime binding relay sockets under a root of its own.
type relayHarness struct {
	remote      *orchestration.RemoteACPBackend
	runtime     *Runtime
	listener    *bufconn.Listener
	daemonKey   ssh.PublicKey
	daemonAgent *xkmsssh.AgentSession
	agentRoot   string
	audit       *recordingRelayAudit

	mu      sync.Mutex
	spawned map[string]*orchestration.SpawnRequest
	nextID  int
}

func newRelayHarness(t *testing.T) *relayHarness {
	t.Helper()
	h := &relayHarness{audit: &recordingRelayAudit{}, spawned: map[string]*orchestration.SpawnRequest{}}

	// THE DAEMON'S SIDE: the session's signing agent, bound under the
	// daemon's own sockets root exactly as forgeAccess.startSigning binds it.
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	h.daemonKey = signer.PublicKey()
	daemonRoot := shortDir(t, "qbd")
	name, err := orchestration.NewSigningSocketDirName()
	if err != nil {
		t.Fatal(err)
	}
	h.daemonAgent, err = xkmsssh.StartAgent(context.Background(), xkmsssh.AgentOptions{
		Signers:   []ssh.Signer{signer},
		SocketDir: filepath.Join(daemonRoot, name),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.daemonAgent.Stop(context.Background()) })

	// THE FLEET AGENT'S SIDE: its own sockets root, admitted by its own mount
	// policy, and nothing of the daemon's.
	h.agentRoot = shortDir(t, "qba")
	backend := &spawnBackend{spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.nextID++
		id := "sess-relay-" + string(rune('0'+h.nextID))
		h.spawned[id] = req
		return &orchestration.Session{ID: id, Backend: "acp", Status: orchestration.SessionStateActive, CreatedAt: time.Now().UnixNano()}, nil
	}}
	h.runtime, err = NewRuntime(RuntimeConfig{
		Backends:      map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy: orchestration.SandboxPolicyOptIn,
		MountPolicy:   &orchestration.MountPolicy{SocketsRoot: h.agentRoot},
		Logger:        &testLogger{t: t},
		SocketsRoot:   h.agentRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.runtime.PrepareSigningRelayRoot(); err != nil {
		t.Fatal(err)
	}

	h.listener = bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime: h.runtime, BindAddr: "bufnet", Logger: &testLogger{t: t}, ListenerOverride: h.listener,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetIdentity(newTestIdentity())
	if err := srv.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	h.remote, err = orchestration.NewRemoteACPBackend(orchestration.RemoteACPBackendDeps{
		Registration: &orchestration.FleetNode{
			ID: "agent-relay", TenantID: "tenant-a", Name: "agent-02", Host: "bufnet", Backend: "acp",
			Kind: orchestration.FleetNodeKindDaemon, Status: orchestration.FleetNodeStatusRunning,
			Capabilities: []string{"acp", orchestration.FleetNodeTraitSigningRelay},
		},
		Backend:      "acp",
		Logger:       &testLogger{t: t},
		ConnDialer:   h.dial,
		SigningTrait: func() string { return orchestration.SigningAgentTrait("0123456789abcdef0123456789abcdef") },
		SigningAudit: h.audit,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.remote.CloseAll(); err != nil {
			t.Errorf("close pool: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := srv.Stop(ctx); err != nil {
			t.Errorf("stop spawn server: %v", err)
		}
	})
	return h
}

func (h *relayHarness) dial(_ context.Context, target string) (*grpc.ClientConn, error) {
	return grpc.NewClient("passthrough:///"+target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return h.listener.Dial() }))
}

// signingSpawn is a spawn that mounts the daemon's session agent socket, the
// shape the sdlc runner hands the orchestrator for a delivery.
func (h *relayHarness) signingSpawn() *orchestration.SpawnRequest {
	return &orchestration.SpawnRequest{
		ProjectID: "proj-relay",
		Env:       map[string]string{orchestration.WorkspaceSigningAgentEnv: orchestration.WorkspaceSigningAgentSocket},
		ExtraMounts: []orchestration.ContainerMount{{
			Source: h.daemonAgent.SockPath, Target: orchestration.WorkspaceSigningAgentSocket, ReadOnly: true,
		}},
	}
}

// relayedSocket is the socket the fleet node mounted for a session.
func (h *relayHarness) relayedSocket(t *testing.T, sessionID string) string {
	t.Helper()
	h.mu.Lock()
	req := h.spawned[sessionID]
	h.mu.Unlock()
	if req == nil {
		t.Fatalf("session %s was never spawned on the agent", sessionID)
	}
	for _, mount := range req.ExtraMounts {
		if mount.Target == orchestration.WorkspaceSigningAgentSocket {
			return mount.Source
		}
	}
	t.Fatalf("session %s mounts no signing socket", sessionID)
	return ""
}

// client dials the FleetRuntime service as the control plane does.
func (h *relayHarness) client(t *testing.T) proto.FleetRuntimeClient {
	t.Helper()
	conn, err := h.dial(context.Background(), "bufnet")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	})
	return proto.NewFleetRuntimeClient(conn)
}

// signOver signs data through the agent socket at path, as git does.
func signOver(t *testing.T, path string, data []byte) (*ssh.Signature, error) {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close agent conn: %v", err)
		}
	}()
	client := sshagent.NewClient(conn)
	keys, err := client.List()
	if err != nil {
		return nil, err
	}
	if len(keys) != 1 {
		t.Fatalf("the relayed agent lists %d keys, want the daemon's one", len(keys))
	}
	return client.Sign(keys[0], data)
}

// deliveryCommit is a commit object as git hands it to its signer.
var deliveryCommit = []byte("tree 4b825dc642cb6eb9a060e54bf8d69288fbee4904\n" +
	"author agent-quicbaas <agent-quicbaas@example.com> 1790000000 +0000\n" +
	"committer agent-quicbaas <agent-quicbaas@example.com> 1790000000 +0000\n\n" +
	"delivery: relay the signing agent (#1013)\n")

// =============================================================================
// End to end
// =============================================================================

// TestSigningRelay_ADeliveryCommitSignedThroughTheRelayVerifiesAgainstTheDaemonKey
// is go-quicbaas#1013's whole claim: a session on a fleet node that cannot
// reach the daemon's sockets root gets a signing socket of its agent's own,
// whose signature over a delivery commit verifies against the daemon's KMS
// public key — the key never having left the daemon's side — and the daemon
// audits it with the data's digest, never the data.
func TestSigningRelay_ADeliveryCommitSignedThroughTheRelayVerifiesAgainstTheDaemonKey(t *testing.T) {
	h := newRelayHarness(t)
	sess, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}

	socket := h.relayedSocket(t, sess.ID)
	if !strings.HasPrefix(socket, h.agentRoot+string(filepath.Separator)) {
		t.Fatalf("the agent mounted %s, want a socket under its own root %s", socket, h.agentRoot)
	}
	if socket == h.daemonAgent.SockPath {
		t.Fatal("the agent was handed the daemon's own socket path")
	}

	signature, err := signOver(t, socket, deliveryCommit)
	if err != nil {
		t.Fatalf("relayed sign: %v", err)
	}
	if err := h.daemonKey.Verify(deliveryCommit, signature); err != nil {
		t.Fatalf("the relayed signature does not verify against the daemon's key: %v", err)
	}

	events := h.audit.recorded()
	if len(events) != 1 {
		t.Fatalf("audited %d signatures, want 1", len(events))
	}
	digest := sha256.Sum256(deliveryCommit)
	want := orchestration.SigningRelaySignEvent{
		TenantID: "tenant-a", SessionID: sess.ID, AgentID: "agent-relay",
		KeyFingerprint: ssh.FingerprintSHA256(h.daemonKey), DataSHA256: hex.EncodeToString(digest[:]), Success: true,
	}
	if events[0] != want {
		t.Fatalf("audit event = %+v, want %+v", events[0], want)
	}
}

// TestSigningRelay_StopSigningClosesTheWindow: the daemon's session agent is
// what every relayed signature is made with, so stopping it — a reader's
// StopSigning after its checkout — fails every later relayed sign, and the
// refusal is audited.
func TestSigningRelay_StopSigningClosesTheWindow(t *testing.T) {
	h := newRelayHarness(t)
	sess, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	h.daemonAgent.Stop(context.Background())

	if _, err := signOver(t, h.relayedSocket(t, sess.ID), deliveryCommit); err == nil {
		t.Fatal("a relayed sign succeeded after the daemon stopped the session's agent")
	}
	events := h.audit.recorded()
	if len(events) != 1 || events[0].Success || !strings.Contains(events[0].Reason, "not serving") {
		t.Fatalf("audit = %+v, want one refusal naming the stopped agent", events)
	}
}

// TestSigningRelay_TheRelaySocketGoesWithTheSession: closing the session
// releases the relay — the socket and its directory are gone from the agent's
// root — and a later sweep finds nothing to remove.
func TestSigningRelay_TheRelaySocketGoesWithTheSession(t *testing.T) {
	h := newRelayHarness(t)
	sess, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	socket := h.relayedSocket(t, sess.ID)
	if _, err := os.Stat(socket); err != nil {
		t.Fatalf("the relay socket is not bound: %v", err)
	}

	if err := h.remote.Close(context.Background(), sess.ID); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(socket)); !os.IsNotExist(err) {
		t.Fatalf("the relay's socket directory survived the session: %v", err)
	}
	if _, held := h.runtime.signingRelays.Load(sess.ID); held {
		t.Fatal("the runtime still tracks a closed session's relay")
	}
	sweep, err := h.runtime.PrepareSigningRelayRoot()
	if err != nil || len(sweep.Removed) != 0 {
		t.Fatalf("sweep = %+v, %v; want nothing left to sweep", sweep, err)
	}
}

// TestSigningRelay_ADroppedLinkFailsTyped: when the link drops, the relay is
// lost for good — the agent's signers fail with ErrSigningRelayLinkLost and
// nothing reconnects or falls back.
func TestSigningRelay_ADroppedLinkFailsTyped(t *testing.T) {
	h := newRelayHarness(t)
	sess, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	held, ok := h.runtime.signingRelays.Load(sess.ID)
	if !ok {
		t.Fatal("the runtime holds no relay for the session")
	}
	relay := held.(*signingRelay)

	// The control plane's side goes away: its conn pool is closed under the
	// open stream.
	if err := h.remote.CloseAll(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-relay.done():
	case <-time.After(10 * time.Second):
		t.Fatal("the agent never noticed the link drop")
	}

	_, err = relay.sign(h.daemonKey, deliveryCommit, 0)
	var linkErr *SigningRelayLinkError
	if !errors.As(err, &linkErr) || !errors.Is(err, ErrSigningRelayLinkLost) || linkErr.SessionID != sess.ID {
		t.Fatalf("sign after the drop = %v, want a typed ErrSigningRelayLinkLost for %s", err, sess.ID)
	}
	if _, err := signOver(t, h.relayedSocket(t, sess.ID), deliveryCommit); err == nil {
		t.Fatal("the container could still sign with its link gone")
	}
}

// TestSigningRelay_AnotherSessionsOrTenantsRelayIsRefused: the agent hands a
// relay only to the tenant and session it was spawned for, once. Everything
// else is refused with a status the control plane can act on.
func TestSigningRelay_AnotherSessionsOrTenantsRelayIsRefused(t *testing.T) {
	h := newRelayHarness(t)
	signing, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	plain, err := h.remote.Spawn(context.Background(), &orchestration.SpawnRequest{ProjectID: "proj-plain"})
	if err != nil {
		t.Fatalf("plain spawn: %v", err)
	}
	client := h.client(t)

	for name, tc := range map[string]struct {
		first *proto.SigningRelayControlFrame
		want  codes.Code
	}{
		"another tenant":            {openFrame("tenant-b", signing.ID), codes.PermissionDenied},
		"an unknown session":        {openFrame("tenant-a", "sess-nobody"), codes.NotFound},
		"a session with no relay":   {openFrame("tenant-a", plain.ID), codes.FailedPrecondition},
		"a second attach":           {openFrame("tenant-a", signing.ID), codes.FailedPrecondition},
		"a first frame that is not": {&proto.SigningRelayControlFrame{}, codes.InvalidArgument},
	} {
		stream, err := client.SigningRelay(context.Background())
		if err != nil {
			t.Fatalf("%s: open: %v", name, err)
		}
		if err := stream.Send(tc.first); err != nil {
			t.Fatalf("%s: send: %v", name, err)
		}
		_, err = stream.Recv()
		if status.Code(err) != tc.want {
			t.Errorf("%s: %v, want %s", name, err, tc.want)
		}
	}
}

func openFrame(tenantID, sessionID string) *proto.SigningRelayControlFrame {
	return &proto.SigningRelayControlFrame{Frame: &proto.SigningRelayControlFrame_Open{
		Open: &proto.SigningRelayOpen{TenantId: tenantID, SessionId: sessionID},
	}}
}

// TestSigningRelay_TheRuntimeRefusesEachWrongAttachTyped covers the runtime's
// refusals one by one, including another tenant's session, which the gRPC
// layer's own tenant check would otherwise answer first.
func TestSigningRelay_TheRuntimeRefusesEachWrongAttachTyped(t *testing.T) {
	h := newRelayHarness(t)
	sess, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	for name, tc := range map[string]struct {
		tenant, session string
		want            error
	}{
		"unknown session":  {"tenant-a", "sess-nobody", ErrSigningRelayUnknownSession},
		"another tenant":   {"tenant-b", sess.ID, ErrSigningRelayForeignSession},
		"already attached": {"tenant-a", sess.ID, ErrSigningRelayAlreadyAttached},
	} {
		_, err := h.runtime.AttachSigningRelay(tc.tenant, tc.session, &fakeRelayLink{})
		var attachErr *SigningRelayAttachError
		if !errors.As(err, &attachErr) || !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
}

// TestSigningRelay_ASpawnTheAgentCannotRelayIsRefused: the control plane lists
// the relay's keys from the session's own agent before the spawn goes out, so
// a session agent that is not there fails the spawn typed rather than handing
// the agent a relay with nothing behind it.
func TestSigningRelay_ASpawnTheAgentCannotRelayIsRefused(t *testing.T) {
	h := newRelayHarness(t)
	h.daemonAgent.Stop(context.Background())
	_, err := h.remote.Spawn(context.Background(), h.signingSpawn())
	var keysErr *orchestration.SigningRelayKeysError
	if !errors.As(err, &keysErr) || !errors.Is(err, orchestration.ErrSigningRelay) {
		t.Fatalf("spawn = %v, want *SigningRelayKeysError", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.spawned) != 0 {
		t.Fatalf("the agent spawned %d session(s) for a relay that could not be served", len(h.spawned))
	}
}

// =============================================================================
// The relay on its own
// =============================================================================

// fakeRelayLink answers sign frames with respond, from another goroutine, as
// the control plane would.
type fakeRelayLink struct {
	mu      sync.Mutex
	frames  []*proto.SigningRelayNodeFrame
	sendErr error
	relay   *signingRelay
	respond func(*proto.SigningRelaySign) *proto.SigningRelayResult
}

func (l *fakeRelayLink) Send(frame *proto.SigningRelayNodeFrame) error {
	l.mu.Lock()
	l.frames = append(l.frames, frame)
	err := l.sendErr
	l.mu.Unlock()
	if err != nil {
		return err
	}
	if sign := frame.GetSign(); sign != nil && l.respond != nil {
		go l.relay.deliver(l.respond(sign))
	}
	return nil
}

func (l *fakeRelayLink) lastSign() *proto.SigningRelaySign {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.frames) - 1; i >= 0; i-- {
		if sign := l.frames[i].GetSign(); sign != nil {
			return sign
		}
	}
	return nil
}

func testRelayKey(t *testing.T) (ssh.Signer, *signingRelay) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	relay := newSigningRelay("tenant-a", []ssh.PublicKey{signer.PublicKey()})
	relay.setSession("sess-unit")
	return signer, relay
}

// TestSigningRelay_ASignBeforeAttachFailsTyped: the socket exists from the
// spawn, the link only from the attach; a sign in between is refused, not
// queued.
func TestSigningRelay_ASignBeforeAttachFailsTyped(t *testing.T) {
	signer, relay := testRelayKey(t)
	_, err := relay.signers()[0].Sign(rand.Reader, deliveryCommit)
	if !errors.Is(err, ErrSigningRelayNotAttached) {
		t.Fatalf("sign before attach = %v, want ErrSigningRelayNotAttached", err)
	}
	if relay.signers()[0].PublicKey().Type() != signer.PublicKey().Type() {
		t.Fatal("the relayed signer does not advertise the control plane's key")
	}
}

// TestSigningRelay_TheAnswerTheControlPlaneGivesIsTheOneReturned covers the
// round trip on a fake link: the frame carries the session, tenant, key and
// the algorithm's flag, and the control plane's answer — a signature, or a
// refusal — is what the signer returns.
func TestSigningRelay_TheAnswerTheControlPlaneGivesIsTheOneReturned(t *testing.T) {
	signer, relay := testRelayKey(t)
	link := &fakeRelayLink{relay: relay, respond: func(sign *proto.SigningRelaySign) *proto.SigningRelayResult {
		signature, err := signer.Sign(rand.Reader, sign.GetData())
		if err != nil {
			return &proto.SigningRelayResult{Id: sign.GetId(), Error: err.Error()}
		}
		return &proto.SigningRelayResult{Id: sign.GetId(), Signature: ssh.Marshal(signature)}
	}}
	if err := relay.attach(link); err != nil {
		t.Fatal(err)
	}
	relayed := relay.signers()[0].(ssh.AlgorithmSigner)

	signature, err := relayed.SignWithAlgorithm(rand.Reader, deliveryCommit, ssh.KeyAlgoRSASHA512)
	if err != nil {
		t.Fatalf("relayed sign: %v", err)
	}
	if err := signer.PublicKey().Verify(deliveryCommit, signature); err != nil {
		t.Fatalf("signature does not verify: %v", err)
	}
	sent := link.lastSign()
	if sent.GetSessionId() != "sess-unit" || sent.GetTenantId() != "tenant-a" ||
		sshagent.SignatureFlags(sent.GetFlags()) != sshagent.SignatureFlagRsaSha512 ||
		string(sent.GetKeyBlob()) != string(signer.PublicKey().Marshal()) {
		t.Fatalf("sign frame = %+v", sent)
	}

	if _, err := relayed.SignWithAlgorithm(rand.Reader, deliveryCommit, "ssh-dss"); !errors.As(err, new(*SigningRelayAlgorithmError)) {
		t.Fatalf("an unsupported algorithm = %v, want *SigningRelayAlgorithmError", err)
	}

	link.respond = func(sign *proto.SigningRelaySign) *proto.SigningRelayResult {
		return &proto.SigningRelayResult{Id: sign.GetId(), Error: "signing relay: refused"}
	}
	if _, err := relayed.Sign(rand.Reader, deliveryCommit); !errors.Is(err, ErrSigningRelayRefused) {
		t.Fatalf("a refused sign = %v, want ErrSigningRelayRefused", err)
	}
	link.respond = func(sign *proto.SigningRelaySign) *proto.SigningRelayResult {
		return &proto.SigningRelayResult{Id: sign.GetId(), Signature: []byte("not a signature")}
	}
	if _, err := relayed.Sign(rand.Reader, deliveryCommit); !errors.Is(err, ErrSigningRelayRefused) {
		t.Fatalf("an undecodable signature = %v, want ErrSigningRelayRefused", err)
	}
}

// TestSigningRelay_ALinkThatCannotSendIsLost: a failed send loses the relay for
// good, whether it is the attach or a sign.
func TestSigningRelay_ALinkThatCannotSendIsLost(t *testing.T) {
	_, relay := testRelayKey(t)
	broken := errors.New("stream reset")
	if err := relay.attach(&fakeRelayLink{sendErr: broken}); !errors.Is(err, ErrSigningRelayLinkLost) || !errors.Is(err, broken) {
		t.Fatalf("attach over a broken link = %v, want ErrSigningRelayLinkLost carrying the cause", err)
	}
	if err := relay.attach(&fakeRelayLink{}); !errors.Is(err, ErrSigningRelayAlreadyAttached) {
		t.Fatalf("re-attach after a loss = %v, want refused", err)
	}

	_, second := testRelayKey(t)
	link := &fakeRelayLink{}
	if err := second.attach(link); err != nil {
		t.Fatal(err)
	}
	link.sendErr = broken
	if _, err := second.signers()[0].Sign(rand.Reader, deliveryCommit); !errors.Is(err, ErrSigningRelayLinkLost) {
		t.Fatalf("sign over a broken link = %v, want ErrSigningRelayLinkLost", err)
	}
	select {
	case <-second.done():
	default:
		t.Fatal("a failed send did not lose the relay")
	}
}

// TestRuntime_StartSigningRelayRefusesWhatItCannotServe: a runtime with no
// sockets root serves no relay, and a relay asked to advertise no key, or a
// key that is not one, is invalid input. A spawn that asks for none is left
// alone.
func TestRuntime_StartSigningRelayRefusesWhatItCannotServe(t *testing.T) {
	signer, _ := testRelayKey(t)
	good := &orchestration.SigningRelaySpec{PublicKeys: [][]byte{signer.PublicKey().Marshal()}}

	rootless, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}}, Logger: &testLogger{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rootless.SigningRelayEnabled() {
		t.Fatal("a runtime with no sockets root advertises the relay")
	}
	if _, err := rootless.startSigningRelay("tenant-a", &orchestration.SpawnRequest{SigningRelay: good}); !errors.Is(err, ErrSigningRelayUnsupported) {
		t.Fatalf("relay on a rootless runtime = %v, want ErrSigningRelayUnsupported", err)
	}
	if sweep, err := rootless.PrepareSigningRelayRoot(); err != nil || sweep.Root != "" {
		t.Fatalf("a rootless runtime prepared %+v, %v", sweep, err)
	}

	rooted, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}}, Logger: &testLogger{t: t},
		SocketsRoot: shortDir(t, "qbr"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rooted.SigningRelayEnabled() {
		t.Fatal("a runtime with a sockets root does not advertise the relay")
	}
	for name, spec := range map[string]*orchestration.SigningRelaySpec{
		"no key":    {},
		"not a key": {PublicKeys: [][]byte{[]byte("garbage")}},
	} {
		var invalid *orchestration.InputValidationError
		if _, err := rooted.startSigningRelay("tenant-a", &orchestration.SpawnRequest{SigningRelay: spec}); !errors.As(err, &invalid) {
			t.Errorf("%s: %v, want *InputValidationError", name, err)
		}
	}
	untouched := &orchestration.SpawnRequest{}
	if relay, err := rooted.startSigningRelay("tenant-a", untouched); relay != nil || err != nil || untouched.ExtraMounts != nil {
		t.Fatalf("a spawn with no relay = %v, %v, mounts %v", relay, err, untouched.ExtraMounts)
	}
}

// TestRuntime_PrepareSigningRelayRootSweepsWhatAKilledAgentLeft: the relay
// sockets a killed agent left bound are swept at startup, and a root that
// cannot hold a socket safely refuses the start.
func TestRuntime_PrepareSigningRelayRootSweepsWhatAKilledAgentLeft(t *testing.T) {
	root := shortDir(t, "qbr")
	name, err := orchestration.NewSigningSocketDirName()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}}, Logger: &testLogger{t: t}, SocketsRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	sweep, err := rt.PrepareSigningRelayRoot()
	if err != nil || len(sweep.Removed) != 1 || sweep.Removed[0] != name {
		t.Fatalf("sweep = %+v, %v; want the orphan %s removed", sweep, err, name)
	}

	shared := filepath.Join(shortDir(t, "qbr"), "shared")
	if err := os.Mkdir(shared, 0o777); err != nil { // #nosec G301 -- the point of the case
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil { // #nosec G302 -- umask would otherwise hide it
		t.Fatal(err)
	}
	unsafe, err := NewRuntime(RuntimeConfig{
		Backends: map[string]orchestration.AgentBackend{"acp": &spawnBackend{}}, Logger: &testLogger{t: t}, SocketsRoot: shared,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unsafe.PrepareSigningRelayRoot(); !errors.Is(err, orchestration.ErrSigningSocketRoot) {
		t.Fatalf("a world-writable root = %v, want refused", err)
	}
}

// gatedRelayLink holds the attached frame on the wire until released, and
// answers each sign with a signature from signer.
type gatedRelayLink struct {
	fakeRelayLink
	attachInFlight chan struct{}
	release        chan struct{}
}

func (l *gatedRelayLink) Send(frame *proto.SigningRelayNodeFrame) error {
	if frame.GetAttached() != nil {
		close(l.attachInFlight)
		<-l.release
	}
	return l.fakeRelayLink.Send(frame)
}

// TestSigningRelay_ASignRacingTheAttachQueuesBehindAttached: a sign that sees
// the relay attached while the attached frame is still going out waits behind
// it — the control plane, which requires attached first, never sees a sign
// before it.
func TestSigningRelay_ASignRacingTheAttachQueuesBehindAttached(t *testing.T) {
	signer, relay := testRelayKey(t)
	link := &gatedRelayLink{attachInFlight: make(chan struct{}), release: make(chan struct{})}
	link.relay = relay
	link.respond = func(sign *proto.SigningRelaySign) *proto.SigningRelayResult {
		signature, err := signer.Sign(rand.Reader, sign.GetData())
		if err != nil {
			return &proto.SigningRelayResult{Id: sign.GetId(), Error: err.Error()}
		}
		return &proto.SigningRelayResult{Id: sign.GetId(), Signature: ssh.Marshal(signature)}
	}

	attached := make(chan error, 1)
	go func() { attached <- relay.attach(link) }()
	<-link.attachInFlight

	// Readiness is visible now: the attached frame is mid-send.
	signed := make(chan error, 1)
	go func() {
		_, err := relay.signers()[0].Sign(rand.Reader, deliveryCommit)
		signed <- err
	}()

	link.mu.Lock()
	early := len(link.frames)
	link.mu.Unlock()
	if early != 0 {
		t.Fatalf("%d frame(s) reached the wire before the attached frame", early)
	}

	close(link.release)
	if err := <-attached; err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := <-signed; err != nil {
		t.Fatalf("the racing sign: %v", err)
	}
	link.mu.Lock()
	defer link.mu.Unlock()
	if len(link.frames) != 2 || link.frames[0].GetAttached() == nil || link.frames[1].GetSign() == nil {
		t.Fatalf("frames = %v, want attached then the sign", link.frames)
	}
}
