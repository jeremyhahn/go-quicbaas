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
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// THE SPAWN WIRE CONTRACT
// =============================================================================
//
// orchestration.SpawnRequest, api/proto/orchestrator.proto and the two
// hand-written mappings between them (RemoteACPBackend.Spawn on the way out,
// GRPCSpawnServer.Spawn on the way in) evolve independently, and until this
// file nothing failed when they diverged. In two days the boundary lost four
// things — the permission grant (#218/#232), the pooled account selection
// (#235/#239), the account allow-list, and the turn-failure evidence — and each
// was found by a human reading the diff afterwards. go-quicbaas#248.
//
// EVERY ONE OF THOSE OMISSIONS WAS SILENT, and for one reason: every field on
// this request is inert at its zero value. A dropped field does not produce a
// wrong answer or a transport error; it produces a session that ran with less
// than it was told to. For a restriction — AccountAllow, AccountExclude — or a
// requirement — TrustLevel — "less than it was told to" means WIDER than the
// operator authorized.
//
// So the contract is asserted as a round trip, not as a list. The sentinel
// below sets every exported field of SpawnRequest to a distinctive non-zero
// value; the request is pushed through a real RemoteACPBackend, over a real
// gRPC connection, into a real GRPCSpawnServer, and the SpawnRequest the daemon
// hands its backend is compared field-by-field against the sentinel. Two
// separate failures follow from that, and both are the point:
//
//   - A NEW FIELD fails immediately in
//     TestSpawnWireContract_SentinelCoversEverySpawnRequestField, which walks
//     the struct by reflection and refuses a field the sentinel leaves at its
//     zero value. Adding a field to SpawnRequest is therefore a two-line change
//     that cannot compile-and-pass without someone deciding what the wire does
//     with it.
//
//   - AN UNMAPPED FIELD fails in
//     TestSpawnWireContract_SpawnRequestSurvivesTheRemoteBoundary, naming the
//     field and both values.
//
// A FIELD THAT IS DELIBERATELY NOT FORWARDED GOES ON THE DENY-LIST BELOW, WITH
// ITS REASON. That is the whole difference between an omission somebody decided
// and an omission nobody noticed, and it is the difference this file exists to
// make visible.

// spawnRequestNotOnTheWire names the SpawnRequest fields that deliberately do
// not cross the daemon boundary, and why. A field is allowed in here only when
// forwarding it would be WRONG — not merely when nothing happens to set it
// today, which is a reason that rots the moment something does.
var spawnRequestNotOnTheWire = map[string]string{
	// Set by ACPBackend.Spawn itself, on the daemon, after account resolution:
	// it names a per-session temporary directory holding that account's
	// credential files. It is tagged `json:"-"` for the same reason. A caller
	// never supplies one, so there is nothing here for the wire to lose, and
	// forwarding it would let a caller point the agent's HOME at a path of its
	// choosing on somebody else's host.
	"AccountHomeDir": "spawn-time wiring on the daemon, never caller-supplied (backend.go: `json:\"-\"`)",
	// Names the session a control-plane respawn reconstitutes, so its own
	// record is not counted against the account slot it re-claims. The record
	// lives on the control plane and so does that reservation; a remote session
	// is restored LIVE and never resumes through spawnRequestFromRecord, and a
	// rotation moves to a DIFFERENT account, where excluding the old record is
	// moot. Forwarding it would hand a daemon a way to discount an arbitrary
	// session from another's cap.
	"ReplacesSessionID": "control-plane resume bookkeeping for its own reservation (backend.go: `json:\"-\"`)",

	// The relay is the REMOTE BACKEND'S decision, not the caller's: it reads the
	// keys off the control plane's own session agent and sets the wire field
	// itself (go-quicbaas#1013). A caller-supplied spec would let a caller name
	// the keys an agent advertises; on the daemon the field is set only by the
	// spawn server, from the wire.
	"SigningRelay": "computed by RemoteACPBackend from the signing mount, never caller-supplied (backend.go: `json:\"-\"`)",
}

// spawnRequestProvisionedOnTheDaemon names the SpawnRequest fields that DO
// cross the boundary but are consumed there by design, so the request the
// daemon's backend receives is not the request that was sent — and the test
// that asserts their crossing instead.
//
// Workspace is mutually exclusive with WorkDir (the sentinel's), and the
// daemon turns it into a WorkDir and a spec mount of its own
// (Runtime.provisionWorkspace). A field-equality round trip cannot express
// that; TestSpawnWireContract_ANamedWorkspaceIsProvisionedOnTheDaemon does.
var spawnRequestProvisionedOnTheDaemon = map[string]string{
	"Workspace": "TestSpawnWireContract_ANamedWorkspaceIsProvisionedOnTheDaemon",
}

// sentinelSpawnRequest builds a SpawnRequest with every exported field set to a
// distinctive non-zero value.
//
// The values are chosen to survive the daemon's own normalization, which is a
// real part of this contract rather than a workaround for it. Runtime.Spawn
// resolves the sandbox mode against the daemon's SandboxPolicy and rewrites
// SandboxMode/Sandboxed from the result, and fills ContainerImage from the
// daemon's image map when the caller named none. A sentinel asking for
// SandboxModeRequired under an opt-in policy, with an image already named,
// resolves to itself — so a mismatch in this test is a mapping fault and never
// a normalization artifact.
func sentinelSpawnRequest(mountRoot string) *orchestration.SpawnRequest {
	return &orchestration.SpawnRequest{
		ProjectID:    "proj-sentinel",
		WorkDir:      filepath.Join(mountRoot, "workdir"),
		Model:        "model-sentinel",
		SystemPrompt: "system-prompt-sentinel",
		Env:          map[string]string{"SENTINEL_ENV": "sentinel-value"},
		MCPServers: []orchestration.MCPServerConfig{{
			Name:    "mcp-sentinel",
			Command: "/usr/bin/mcp-sentinel",
			Args:    []string{"--sentinel"},
			URL:     "http://mcp.sentinel.test",
			Env:     map[string]string{"MCP_SENTINEL": "on"},
		}},
		ExtraArgs:  []string{"--permission-mode", "acceptEdits"},
		Persistent: true,

		SandboxMode:    orchestration.SandboxModeRequired,
		Sandboxed:      true,
		ContainerImage: "registry.sentinel.test/agent:v1",
		ExtraMounts: []orchestration.ContainerMount{{
			Source:   filepath.Join(mountRoot, "mount"),
			Target:   "/mnt/sentinel",
			ReadOnly: true,
			Type:     orchestration.MountTypeBind,
		}},

		ImageDigest:     "sha256:0000000000000000000000000000000000000000000000000000000000000001",
		TrustLevel:      orchestration.TrustLevelAttested,
		TrustLabel:      "trust-label-sentinel",
		ReferenceValues: "reference-values-sentinel",

		AccountID:       "acct-sentinel",
		AccountAuto:     true,
		AccountStrategy: "least_usage",
		AccountAllow:    []string{"acct-allow-1", "acct-allow-2"},
		AccountExclude:  []string{"acct-exclude-1"},

		JobID: "job-sentinel",

		AccountHomeDir: "/sentinel/home",

		ReplacesSessionID: "replaced-session-sentinel",
		SigningRelay:      &orchestration.SigningRelaySpec{PublicKeys: [][]byte{[]byte("sentinel-key")}},

		// The resolved storage tier. It travels for the trust tier's own
		// reason: the control plane holds the project record, the daemon
		// creates the container. The sentinel's sizes are a budget that FITS —
		// 2 GiB of tmpfs inside 4 GiB leaves room for the build — because a
		// spawn the daemon refuses on its memory budget never reaches the
		// backend this contract compares at.
		Storage: orchestration.WorkspaceStoragePlan{
			Tier:           orchestration.WorkspaceTierEphemeral,
			TmpfsSizeBytes: 2 * 1024 * 1024 * 1024,
			MemoryBytes:    4 * 1024 * 1024 * 1024,
		},
	}
}

// TestSpawnWireContract_SentinelCoversEverySpawnRequestField is the half of the
// contract that catches a field the day it is ADDED.
//
// A new field on SpawnRequest that nobody thought about the wire for is left at
// its zero value by the sentinel above, and this fails naming it. The author
// then has exactly two ways forward: give it a sentinel value and map it, or
// give it a sentinel value and a deny-list entry saying why it must not travel.
// Both are decisions. Neither is silence.
func TestSpawnWireContract_SentinelCoversEverySpawnRequestField(t *testing.T) {
	t.Parallel()

	sentinel := reflect.ValueOf(*sentinelSpawnRequest(t.TempDir()))
	typ := sentinel.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if _, elsewhere := spawnRequestProvisionedOnTheDaemon[field.Name]; elsewhere {
			continue
		}
		if sentinel.Field(i).IsZero() {
			t.Errorf("SpawnRequest.%s is left at its zero value by sentinelSpawnRequest.\n"+
				"A field the sentinel does not set is a field this contract cannot check. Give it a\n"+
				"distinctive value, then either map it across the daemon boundary or record it in\n"+
				"spawnRequestNotOnTheWire with the reason it must not travel.", field.Name)
		}
	}
}

// TestSpawnWireContract_SpawnRequestSurvivesTheRemoteBoundary is the round trip
// itself: SpawnRequest -> RemoteACPBackend -> proto -> gRPC -> GRPCSpawnServer
// -> SpawnRequest, with both hand-written mappings and the real generated codec
// in the path.
//
// Deliberately end-to-end rather than two unit tests of the mapping functions.
// The mappings are not the contract; the boundary is. A test that called the
// encoder and the decoder directly would pass just as happily if the daemon's
// handler stopped calling the decoder at all.
func TestSpawnWireContract_SpawnRequestSurvivesTheRemoteBoundary(t *testing.T) {
	t.Parallel()

	mountRoot := t.TempDir()
	for _, dir := range []string{"workdir", "mount"} {
		if err := os.Mkdir(filepath.Join(mountRoot, dir), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	received := make(chan *orchestration.SpawnRequest, 1)
	backend := &spawnBackend{
		spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
			received <- req
			return &orchestration.Session{
				ID:        "sess-contract",
				ProjectID: req.ProjectID,
				Backend:   "acp",
				Model:     req.Model,
				Status:    orchestration.SessionStateActive,
				WorkDir:   req.WorkDir,
				CreatedAt: time.Now().UnixNano(),
			}, nil
		},
	}

	remote, cleanup := newContractBoundary(t, backend, &orchestration.MountPolicy{
		AllowedRoots: []string{mountRoot},
	}, &fakeContainerRuntime{available: true}, nil)
	defer cleanup()

	sent := sentinelSpawnRequest(mountRoot)
	// Spawn mutates the request it is given (the daemon normalizes the sandbox
	// fields in place), so the comparison is against an untouched copy.
	expected := *sent

	if _, err := remote.Spawn(context.Background(), sent); err != nil {
		t.Fatalf("remote spawn: %v", err)
	}

	var got *orchestration.SpawnRequest
	select {
	case got = <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon's backend never received a spawn request")
	}

	skipped := make(map[string]bool, len(spawnRequestProvisionedOnTheDaemon))
	for name := range spawnRequestProvisionedOnTheDaemon {
		skipped[name] = true
	}
	compareFields(t, "SpawnRequest", reflect.ValueOf(expected), reflect.ValueOf(*got), spawnRequestNotOnTheWire, skipped)
}

// compareFields reports every exported field of want that differs from got,
// skipping the named deny-list, and reports a deny-listed field that DID
// survive.
//
// Reporting the second direction matters as much as the first: a deny-list
// entry that no longer describes the code is a comment claiming a field is
// withheld while the wire carries it, which is worse than no comment at all.
func compareFields(t *testing.T, typeName string, want, got reflect.Value, denied map[string]string, skipped map[string]bool) {
	t.Helper()

	typ := want.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() || skipped[field.Name] {
			continue
		}
		wantVal := want.Field(i).Interface()
		gotVal := got.Field(i).Interface()
		equal := reflect.DeepEqual(wantVal, gotVal)

		if reason, isDenied := denied[field.Name]; isDenied {
			if equal {
				t.Errorf("%s.%s is on the deny-list (%q) but its value survived the boundary.\n"+
					"Either the field is now forwarded — remove the deny-list entry — or the test is\n"+
					"no longer proving what the entry claims.", typeName, field.Name, reason)
			}
			continue
		}

		if !equal {
			t.Errorf("%s.%s did not survive the daemon boundary.\n"+
				"  sent:     %#v\n"+
				"  received: %#v\n"+
				"Map it in RemoteACPBackend.Spawn and GRPCSpawnServer.Spawn (adding the proto field if\n"+
				"it has none), or record it in the deny-list with the reason it must not travel.",
				typeName, field.Name, wantVal, gotVal)
		}
	}
}

// newContractBoundary stands up a real GRPCSpawnServer over bufconn and a real
// RemoteACPBackend dialing it, so a test drives the same two mappings a control
// plane and a daemon use in production.
func newContractBoundary(
	t *testing.T,
	backend orchestration.AgentBackend,
	mountPolicy *orchestration.MountPolicy,
	containerRuntime orchestration.ContainerRuntime,
	workspaces orchestration.WorkspaceProvisioner,
) (*orchestration.RemoteACPBackend, func()) {
	t.Helper()

	rt, err := NewRuntime(RuntimeConfig{
		Backends:         map[string]orchestration.AgentBackend{"acp": backend},
		SandboxPolicy:    orchestration.SandboxPolicyOptIn,
		MountPolicy:      mountPolicy,
		ContainerRuntime: containerRuntime,
		Logger:           &testLogger{t: t},
		Workspaces:       workspaces,
	})
	if err != nil {
		t.Fatalf("runtime: %v", err)
	}

	lis := bufconn.Listen(spawnServerBufSize)
	srv, err := NewGRPCSpawnServer(GRPCSpawnServerDeps{
		Runtime:          rt,
		BindAddr:         "bufnet",
		Logger:           &testLogger{t: t},
		ListenerOverride: lis,
	})
	if err != nil {
		t.Fatalf("NewGRPCSpawnServer: %v", err)
	}
	srv.SetIdentity(newTestIdentity())
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	remote, err := orchestration.NewRemoteACPBackend(orchestration.RemoteACPBackendDeps{
		Registration: &orchestration.FleetNode{
			ID:       "agent-contract",
			TenantID: "tenant-a",
			Name:     "contract-daemon",
			Host:     "bufnet",
			Backend:  "acp",
			Kind:     orchestration.FleetNodeKindDaemon,
			Status:   orchestration.FleetNodeStatusRunning,
		},
		Backend: "acp",
		Logger:  &testLogger{t: t},
		ConnDialer: func(_ context.Context, target string) (*grpc.ClientConn, error) {
			return grpc.NewClient(
				"passthrough:///"+target,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) {
					return lis.Dial()
				}),
			)
		},
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}

	cleanup := func() {
		_ = remote.CloseAll()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Stop(stopCtx)
	}
	return remote, cleanup
}

// =============================================================================
// THE OTHER DIRECTION: WHAT THE DAEMON REPORTS BACK
// =============================================================================
//
// The response half of #248's class, and the half that had been quietly wrong
// the longest. The daemon is the only party that knows some of what a session
// turned out to be — above all WHICH POOLED ACCOUNT it resolved to, since
// selection runs there against credentials that never cross this wire. What the
// response does not carry, the control plane cannot recover by any other means.
//
// The failure this catches is not an error either. A session whose AccountID
// never came home is recorded with an empty one, its turn usage is attributed
// to no account, and every cost report is quietly short by that session.

// sessionNotOnTheWire names the Session fields the daemon deliberately does not
// report back, and why.
var sessionNotOnTheWire = map[string]string{
	// The agent process runs on the daemon's host, in the daemon's pid
	// namespace. A pid from there addresses nothing on this side — there is no
	// signal to send it and no /proc entry to read — so carrying it would only
	// invite a caller to believe otherwise. Local sessions keep a real one;
	// remote sessions are identified by session id and host.
	"PID": "the process lives in the daemon's pid namespace; the number means nothing here",

	// ContainerID USED TO BE DENIED HERE, and the entry is worth remembering
	// rather than merely deleting. Its reason was that the control plane's own
	// runtime cannot exec into a container on the agent's host — which is true,
	// and is now go-quicbaas#743. But withholding the id did not make that
	// true-ness safe: it made a remote worker indistinguishable from a session
	// that runs no container at all, and the SDLC runner, which refuses to let
	// a worker begin work it could never publish, refused every remote worker
	// on that evidence (go-quicbaas#740). A fact the daemon is the only party
	// to know travels; who may ACT on it is a separate question, answered where
	// the acting happens.
}

// sentinelSession is the Session a daemon's backend produces, with every
// exported field set to a distinctive non-zero value.
func sentinelSession(projectID, workDir string) *orchestration.Session {
	return &orchestration.Session{
		ID:        "sess-sentinel",
		ProjectID: projectID,
		Backend:   "acp",
		Model:     "model-resolved-by-daemon",
		Status:    orchestration.SessionStateActive,
		PID:       4242,
		WorkDir:   workDir,
		CreatedAt: time.Date(2026, 9, 4, 12, 0, 0, 123456789, time.UTC).UnixNano(),

		AccountID:         "acct-resolved-by-daemon",
		AccountStrategy:   "least_usage",
		AccountGeneration: 1758530340123456789,
		CredentialLeaseID: "lease_resolved_by_daemon",
		ReservationID:     "reservation-resolved-by-daemon",
		ContainerID:       "container-resolved-by-daemon",
	}
}

// TestSpawnWireContract_SentinelCoversEverySessionField is the response-side
// twin of the request-side sentinel check: a field added to Session is refused
// until somebody decides whether the daemon reports it.
func TestSpawnWireContract_SentinelCoversEverySessionField(t *testing.T) {
	t.Parallel()

	sentinel := reflect.ValueOf(*sentinelSession("proj", "/work"))
	typ := sentinel.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		if sentinel.Field(i).IsZero() {
			t.Errorf("Session.%s is left at its zero value by sentinelSession.\n"+
				"Give it a distinctive value, then either report it in GRPCSpawnServer.Spawn's response\n"+
				"and read it in RemoteACPBackend.Spawn, or record it in sessionNotOnTheWire with the\n"+
				"reason the daemon must not report it.", field.Name)
		}
	}
}

// TestSpawnWireContract_SessionSurvivesTheRemoteBoundary runs the response the
// other way: the Session a daemon's backend produced, through
// GRPCSpawnServer's response, over gRPC, into the Session RemoteACPBackend
// hands its caller.
func TestSpawnWireContract_SessionSurvivesTheRemoteBoundary(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	backend := &spawnBackend{
		spawnFn: func(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
			return sentinelSession(req.ProjectID, req.WorkDir), nil
		},
	}

	remote, cleanup := newContractBoundary(t, backend, nil, nil, nil)
	defer cleanup()

	got, err := remote.Spawn(context.Background(), &orchestration.SpawnRequest{
		ProjectID: "proj-sentinel",
		WorkDir:   workDir,
		Model:     "model-requested",
	})
	if err != nil {
		t.Fatalf("remote spawn: %v", err)
	}

	want := sentinelSession("proj-sentinel", workDir)
	compareFields(t, "Session", reflect.ValueOf(*want), reflect.ValueOf(*got), sessionNotOnTheWire, nil)
}

// =============================================================================
// THE RESPONSE MESSAGE ITSELF
// =============================================================================
//
// The two tests above compare the two Go structs on either side of the wire,
// and that is one field short of the whole contract: a field can be present on
// BOTH structs and still be dropped, because it is the proto message in the
// middle that the daemon fills in by hand.
//
// That is precisely how #740 happened. api/proto/orchestrator.proto has carried
// `string container_id = 4` on SpawnSessionResponse since the message was
// written. Neither end used it. The daemon built the response without it and
// the control plane read the response without it, so the id — the one fact only
// the daemon can know, and the one the SDLC runner refuses a worker for
// lacking — stopped at a field that existed the whole time.
//
// So this pair pins the MESSAGE. Every field the daemon's handler produces is
// named with the value it must carry, and a field the proto gains that nobody
// fills in fails immediately.

// spawnResponseNotOnTheWire names SpawnSessionResponse fields the daemon
// deliberately leaves unset, and why.
//
// An entry here has to argue that a field the message declares must
// nonetheless never be sent. The only ones are the Agent-reference answers:
// the message is shared with the control plane's SpawnSession verb, which
// resolves an Agent or a pool against the tenant agent catalog and walks its
// failovers itself (orchestration spawnByAgentRef), then hands the daemon a
// request with the reference cleared and a concrete backend. The daemon never
// sees an Agent, so it has nothing to report in either field.
var spawnResponseNotOnTheWire = map[string]string{
	"resolved_agent_id": "the control plane resolves the Agent before the daemon is called; the daemon spawns a backend, not an Agent",
	"failovers":         "the control plane walks a pool's failovers across spawns; each daemon call is a single attempt",
}

// spawnResponseForANamedWorkspace names the response fields the daemon sets
// only when the spawn named a workspace, and the test that asserts them. The
// sentinel spawn names none, so they are correctly unset here.
var spawnResponseForANamedWorkspace = map[string]string{
	"work_dir": "TestSpawnWireContract_ANamedWorkspaceIsProvisionedOnTheDaemon",
}

// spawnResponseSentinelValues is what each field of the response must carry for
// the sentinel session the daemon's backend returns. Written out by name rather
// than derived, so a mapping that changes shape has to be re-stated here by
// somebody who looked at it.
func spawnResponseSentinelValues(sentinel *orchestration.Session) map[string]string {
	return map[string]string{
		"session_id":       sentinel.ID,
		"backend":          "acp",
		"model":            sentinel.Model,
		"container_id":     sentinel.ContainerID,
		"account_id":       sentinel.AccountID,
		"account_strategy": sentinel.AccountStrategy,
		// The generation is an integer, not a string; like the timestamp it is
		// checked separately below.
		"account_generation": "",
		// The timestamp is a message, not a string; it is checked separately
		// against the session's CreatedAt below.
		"timestamp": "",
	}
}

// spawnSentinelResponse drives one real Spawn RPC against a daemon whose
// backend returns sentinelSession, and returns the proto response as it came
// off the wire.
func spawnSentinelResponse(t *testing.T, sentinel *orchestration.Session) *proto.SpawnSessionResponse {
	t.Helper()

	backend := &spawnBackend{
		spawnFn: func(_ context.Context, _ *orchestration.SpawnRequest) (*orchestration.Session, error) {
			return sentinel, nil
		},
	}
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	resp, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		TenantId:  "tenant-a",
		Backend:   "acp",
		ProjectId: "proj-sentinel",
		Model:     "model-requested",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	return resp
}

// TestSpawnWireContract_SpawnSessionResponseCarriesEveryField is the table that
// #740 would have failed. Each row is one field of the proto message, named
// with the value the daemon must put in it.
func TestSpawnWireContract_SpawnSessionResponseCarriesEveryField(t *testing.T) {
	t.Parallel()

	sentinel := sentinelSession("proj-sentinel", "/work")
	resp := spawnSentinelResponse(t, sentinel)

	got := map[string]string{
		"session_id":         resp.GetSessionId(),
		"backend":            resp.GetBackend(),
		"model":              resp.GetModel(),
		"container_id":       resp.GetContainerId(),
		"account_id":         resp.GetAccountId(),
		"account_strategy":   resp.GetAccountStrategy(),
		"account_generation": "",
		"timestamp":          "",
	}

	for name, want := range spawnResponseSentinelValues(sentinel) {
		t.Run(name, func(t *testing.T) {
			if got[name] != want {
				t.Errorf("SpawnSessionResponse.%s = %q, want %q.\n"+
					"The daemon is the only party that knows this; a response that drops it is a fact\n"+
					"the control plane has no other way to recover.", name, got[name], want)
			}
		})
	}

	t.Run("timestamp", func(t *testing.T) {
		if resp.GetTimestamp() == nil {
			t.Fatal("SpawnSessionResponse.timestamp is nil")
		}
		if got := resp.GetTimestamp().AsTime().UnixNano(); got != sentinel.CreatedAt {
			t.Errorf("SpawnSessionResponse.timestamp = %d, want %d", got, sentinel.CreatedAt)
		}
	})

	// Which COPY of the account's credential the session materialized. The
	// control plane compares it against the account's current generation before
	// every prompt; a response that drops it puts remote sessions back on a
	// timestamp comparison that cannot see a rotation landing mid-spawn
	// (go-quicbaas#997).
	t.Run("account_generation", func(t *testing.T) {
		if got := resp.GetAccountGeneration(); got != sentinel.AccountGeneration {
			t.Errorf("SpawnSessionResponse.account_generation = %d, want %d.\n"+
				"The daemon is the only party that knows which credential copy it materialized.",
				got, sentinel.AccountGeneration)
		}
	})
}

// TestSpawnWireContract_SpawnSessionResponseLeavesNoFieldUnset is the half that
// catches a field the day it is ADDED to the proto: it walks the message's own
// descriptor, so a new field nobody filled in fails here rather than in a
// burn-in months later.
func TestSpawnWireContract_SpawnSessionResponseLeavesNoFieldUnset(t *testing.T) {
	t.Parallel()

	resp := spawnSentinelResponse(t, sentinelSession("proj-sentinel", "/work"))

	fields := resp.ProtoReflect().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		name := string(field.Name())
		if _, conditional := spawnResponseForANamedWorkspace[name]; conditional {
			continue
		}
		reason, denied := spawnResponseNotOnTheWire[name]

		set := resp.ProtoReflect().Has(field)
		switch {
		case denied && set:
			t.Errorf("SpawnSessionResponse.%s is on the deny-list (%q) but the daemon set it.\n"+
				"Either it is now reported — remove the deny-list entry — or the entry is stale.",
				name, reason)
		case !denied && !set:
			t.Errorf("SpawnSessionResponse.%s is declared by the proto and the daemon never sets it.\n"+
				"Fill it in GRPCSpawnServer.Spawn and read it in RemoteACPBackend.Spawn, or record it\n"+
				"in spawnResponseNotOnTheWire with the reason the daemon must not report it.\n"+
				"This is exactly the shape of go-quicbaas#740: container_id existed on this message\n"+
				"from the day it was written and neither end ever touched it.", name)
		}
	}
}

// TestSpawnWireContract_SpawnSessionResponseInventsNoContainer is the bad-input
// twin. A daemon session that runs as a HOST PROCESS has no container, and the
// empty string is the answer the SDLC runner acts on: it refuses the worker
// rather than let it start work whose commit, signature and push could never
// run anywhere. A response that supplied an id here would send a tenant's git
// into a sandbox nobody chose.
func TestSpawnWireContract_SpawnSessionResponseInventsNoContainer(t *testing.T) {
	t.Parallel()

	hostProcess := sentinelSession("proj-sentinel", "/work")
	hostProcess.ContainerID = ""

	resp := spawnSentinelResponse(t, hostProcess)

	if resp.GetContainerId() != "" {
		t.Errorf("SpawnSessionResponse.container_id = %q for a host-process session, want empty",
			resp.GetContainerId())
	}
	if resp.GetSessionId() != hostProcess.ID {
		t.Errorf("SpawnSessionResponse.session_id = %q, want %q", resp.GetSessionId(), hostProcess.ID)
	}
}
