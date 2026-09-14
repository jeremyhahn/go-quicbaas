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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// localPreFirstTurnError is the failure exactly as an in-process backend
// produces it: the CLI's result object, is_error, a terminal reason, and a
// reported-and-zero usage record.
func localPreFirstTurnError() error {
	return &PromptError{SessionID: "ses-local", Cause: &StreamJSONError{
		Command: "claude",
		Result:  "Not logged in · Please run /login",
		TurnFailureEvidence: TurnFailureEvidence{
			TerminalReason: "api_error",
			NumTurns:       1,
			Usage:          &TokenUsage{},
		},
	}}
}

// remoteOf is the same failure after a round trip through the daemon
// boundary: the daemon maps it to a status and attaches the evidence, the
// client reads the detail back off the wire.
//
// It exercises the REAL codec in both directions — AttachTurnFailureEvidence
// marshals the detail into a genuine grpc status, wrapRemoteTurnFailure
// unmarshals it — rather than handing the client a struct the test built. A
// test that skipped the encoding would agree with itself about a wire format
// neither side ever used.
func remoteOf(t *testing.T, localErr error) error {
	t.Helper()
	onTheWire := AttachTurnFailureEvidence(
		status.Error(codes.Internal, localErr.Error()), localErr)
	return &RemoteBackendRPCError{Op: "Prompt", Err: wrapRemoteTurnFailure("Prompt", onTheWire)}
}

// TestRemoteTurnFailure_ClassifiesIdenticallyToLocal is go-quicbaas#235's
// remote half.
//
// THE ASSERTION IS THE EQUALITY, not two independently-stated expectations. A
// distributed worker must reach the same verdict about its own attempt as a
// local one, and writing the remote expectation out by hand would let the two
// drift apart while both tests stayed green — which is exactly how the argv
// (#218), the account selection (#235) and this evidence each came to be
// dropped at the same boundary.
func TestRemoteTurnFailure_ClassifiesIdenticallyToLocal(t *testing.T) {
	localErr := localPreFirstTurnError()
	remoteErr := remoteOf(t, localErr)

	localClass, localOK := PreFirstTurnFault(localErr)
	remoteClass, remoteOK := PreFirstTurnFault(remoteErr)

	if !localOK {
		t.Fatal("precondition: the local failure did not classify as pre-first-turn")
	}
	if remoteOK != localOK {
		t.Fatalf("remote classified %v, local %v: a worker on another host burns its "+
			"attempt on the failure a local one is refunded for", remoteOK, localOK)
	}
	if remoteClass != localClass {
		t.Errorf("remote class = %q, local class = %q", remoteClass, localClass)
	}

	// The typed error survived, so anything downstream that asks by name gets
	// the same answer too.
	evidence, carried := EvidenceOf(remoteErr)
	if !carried {
		t.Fatal("the evidence did not survive the boundary as a typed error")
	}
	if evidence.TerminalReason != "api_error" || evidence.NumTurns != 1 {
		t.Errorf("evidence = %+v, want the CLI's own account", evidence)
	}
	if evidence.Usage == nil {
		t.Fatal("usage arrived nil; 'reported zero' and 'reported nothing' must stay " +
			"distinguishable across the wire or a turn that measured nothing is refunded")
	}
}

// TestRemoteTurnFailure_DoesNotPutThePayloadOnTheWire is the confidentiality
// half. The reason and the counts travel; the CLI's own text does not.
func TestRemoteTurnFailure_DoesNotPutThePayloadOnTheWire(t *testing.T) {
	secret := "auth failed for Bearer sk-ant-api03-0123456789abcdefghij"
	localErr := &PromptError{SessionID: "s", Cause: &StreamJSONError{
		Command: "claude",
		Result:  secret,
		TurnFailureEvidence: TurnFailureEvidence{
			TerminalReason: "api_error", NumTurns: 1, Usage: &TokenUsage{},
		},
	}}

	// The daemon maps the error to a status whose MESSAGE is the error string
	// — which withholds the result text already — and attaches the detail.
	onTheWire := AttachTurnFailureEvidence(
		status.Error(codes.Internal, localErr.Error()), localErr)

	st, ok := status.FromError(onTheWire)
	if !ok {
		t.Fatal("the daemon did not produce a status")
	}
	// ASSERTED FIRST, because without it this test passes when NOTHING is
	// attached — which is the failure mode it exists to rule out, inverted.
	if _, carried := TurnFailureEvidenceFromStatus(onTheWire); !carried {
		t.Fatal("no evidence was attached; a confidentiality assertion over an empty " +
			"payload proves nothing")
	}
	if strings.Contains(st.Message(), secret) {
		t.Errorf("the status message carried the CLI's result text: %s", st.Message())
	}
	for _, detail := range st.Details() {
		if strings.Contains(strings.ToLower(detailString(detail)), "sk-ant") {
			t.Errorf("a status detail carried credential-shaped text: %v", detail)
		}
	}
}

// detailString renders a status detail for the assertion above.
func detailString(detail any) string {
	if s, ok := detail.(interface{ String() string }); ok {
		return s.String()
	}
	return ""
}

// TestTurnFailureEvidence_SurvivesTheWire covers the codec on its own, with the
// nil/zero distinction as the case that matters.
func TestTurnFailureEvidence_SurvivesTheWire(t *testing.T) {
	tests := []struct {
		name string
		in   TurnFailureEvidence
		// wantCarried is whether anything should be put on the wire at all.
		wantCarried bool
	}{
		{
			name: "a reported-and-zero record",
			in: TurnFailureEvidence{
				TerminalReason: "api_error", NumTurns: 1, Usage: &TokenUsage{},
			},
			wantCarried: true,
		},
		{
			// THE ONE THAT DECIDES A REFUND. A message that always set usage
			// would arrive as a zero record and be refunded; usage_reported is
			// what keeps "the CLI measured nothing" distinct.
			name: "a reason with no accounting at all",
			in: TurnFailureEvidence{
				TerminalReason: "api_error", NumTurns: 1,
			},
			wantCarried: true,
		},
		{
			name: "a turn that reached the model",
			in: TurnFailureEvidence{
				TerminalReason: "model_error", NumTurns: 4,
				Usage: &TokenUsage{InputTokens: 48122, OutputTokens: 1204},
			},
			wantCarried: true,
		},
		{
			// Nothing to say: no detail is attached, so an older daemon and a
			// failure with no structured account are indistinguishable, which
			// is correct — both mean "classify this the way you always did".
			name:        "empty evidence",
			in:          TurnFailureEvidence{},
			wantCarried: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := turnFailureEvidenceToProto(tc.in)
			if (msg != nil) != tc.wantCarried {
				t.Fatalf("wire message present = %v, want %v", msg != nil, tc.wantCarried)
			}
			if !tc.wantCarried {
				return
			}

			got, carried := turnFailureEvidenceFromProto(msg)
			if !carried {
				t.Fatal("the message did not decode")
			}
			if got.TerminalReason != tc.in.TerminalReason {
				t.Errorf("terminal reason = %q, want %q", got.TerminalReason, tc.in.TerminalReason)
			}
			if got.NumTurns != tc.in.NumTurns {
				t.Errorf("num turns = %d, want %d", got.NumTurns, tc.in.NumTurns)
			}
			if (got.Usage == nil) != (tc.in.Usage == nil) {
				t.Fatalf("usage nil = %v, want %v: the nil/zero distinction is the "+
					"discriminator and must survive the trip", got.Usage == nil, tc.in.Usage == nil)
			}
			if got.Usage != nil && *got.Usage != *tc.in.Usage {
				t.Errorf("usage = %+v, want %+v", *got.Usage, *tc.in.Usage)
			}
			// The verdict is what actually has to match, so assert it too.
			if got.PreFirstTurn() != tc.in.PreFirstTurn() {
				t.Errorf("PreFirstTurn across the wire = %v, want %v",
					got.PreFirstTurn(), tc.in.PreFirstTurn())
			}
		})
	}
}

// TestTurnFailureEvidenceFromStatus_AbsentDetailIsNotAFault covers the
// backward-compatible direction: an older daemon attaches nothing, and the
// client must treat the failure exactly as it did before #235.
func TestTurnFailureEvidenceFromStatus_AbsentDetailIsNotAFault(t *testing.T) {
	plain := status.Error(codes.Internal, "acp: prompt failed: session s: something")

	if _, carried := TurnFailureEvidenceFromStatus(plain); carried {
		t.Error("a status with no detail reported evidence")
	}
	wrapped := wrapRemoteTurnFailure("Prompt", plain)
	if wrapped != plain {
		t.Errorf("a status with no detail was rewrapped as %T; an older daemon's "+
			"failures must arrive unchanged", wrapped)
	}
	if _, ok := PreFirstTurnFault(wrapped); ok {
		t.Error("a failure with no evidence classified as a pre-first-turn fault")
	}
	if _, carried := TurnFailureEvidenceFromStatus(nil); carried {
		t.Error("nil reported evidence")
	}
}

// TestRemoteTurnFailure_RendersOnlyCompiledConstants proves the allowlist
// governs the remote path too: the reason class is resolved on THIS side, so a
// daemon cannot make the control plane render a string of its choosing.
func TestRemoteTurnFailure_RendersOnlyCompiledConstants(t *testing.T) {
	hostile := &RemoteTurnFailureError{
		Op: "Prompt",
		TurnFailureEvidence: TurnFailureEvidence{
			TerminalReason: "sk_live_9f2a1c4e8b7d0356",
			Usage:          &TokenUsage{},
		},
		Err: status.Error(codes.Internal, "failed"),
	}

	if msg := hostile.Error(); strings.Contains(msg, "sk_live_9f2a1c4e8b7d0356") {
		t.Errorf("the remote error rendered a daemon-supplied string: %s", msg)
	}
	// And it is CHARGED: an unrecognised reason cannot bypass the budget, so a
	// daemon cannot buy an item free attempts by naming something new either.
	if _, ok := PreFirstTurnFault(hostile); ok {
		t.Error("a daemon-supplied reason this build does not recognise was refunded")
	}
	// The class is still named, so a human reading the charged failure sees
	// that the CLI reported something rather than nothing.
	if class := hostile.ReasonClass(); class != TerminalReasonUnclassified {
		t.Errorf("ReasonClass = %q, want %q", class, TerminalReasonUnclassified)
	}
}

// TestRemoteACPBackend_PromptReconstructsTheTurnFailure drives the REAL client
// path against a real gRPC daemon.
//
// The codec tests above prove the wire format round-trips and the fixtures
// prove the daemon attaches. Neither would notice if RemoteACPBackend.Prompt
// stopped calling wrapRemoteTurnFailure — the exact shape of omission this
// boundary has now produced three times. This test fails if that call is
// removed.
func TestRemoteACPBackend_PromptReconstructsTheTurnFailure(t *testing.T) {
	t.Parallel()

	// The daemon answers the way pkg/agent's Prompt handler does once it has
	// attached the evidence.
	local := localPreFirstTurnError()
	daemon := startBufconnDaemon(t, &fakeDaemonServer{
		promptErr: AttachTurnFailureEvidence(
			status.Error(codes.Internal, local.Error()), local),
	})
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	_, promptErr := backend.Prompt(context.Background(), &PromptRequest{
		SessionID: "sess-1", Prompt: "go",
	})
	if promptErr == nil {
		t.Fatal("a failing remote turn was reported as a successful prompt")
	}

	// THE EQUALITY, again: whatever a local worker concludes about its own
	// attempt, a remote one must conclude too.
	localClass, localOK := PreFirstTurnFault(local)
	remoteClass, remoteOK := PreFirstTurnFault(promptErr)
	if !localOK {
		t.Fatal("precondition: the local failure did not classify")
	}
	if remoteOK != localOK || remoteClass != localClass {
		t.Fatalf("remote classified (%q, %v), local (%q, %v): the client did not "+
			"reconstruct the daemon's evidence", remoteClass, remoteOK, localClass, localOK)
	}

	// The transport error is still reachable, so nothing that inspects the
	// status loses it.
	var rpcErr *RemoteBackendRPCError
	if !errors.As(promptErr, &rpcErr) {
		t.Errorf("err = %T, want the transport error still in the chain", promptErr)
	}
}

// TestRemoteACPBackend_SpawnReconstructsTheConfigurationFault is the same for
// the spawn half: a dispatch that started no process because the pool was
// empty must be classified as one whether the resolver ran here or elsewhere.
func TestRemoteACPBackend_SpawnReconstructsTheConfigurationFault(t *testing.T) {
	t.Parallel()

	resolverErr := &NoAccountAvailableError{
		TenantID: "acme", ProjectID: "proj-1", Provider: AccountProvider("claude"),
	}
	daemon := startBufconnDaemon(t, &fakeDaemonServer{
		spawnErr: AttachSpawnFaultEvidence(
			status.Error(codes.Internal, resolverErr.Error()), resolverErr),
	})
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	_, spawnErr := backend.Spawn(context.Background(), &SpawnRequest{
		ProjectID: "proj-1", AccountAuto: true,
	})
	if spawnErr == nil {
		t.Fatal("a spawn with no resolvable account was reported as successful")
	}
	// The sentinel is what the SDLC classifier asks about, so it is what has
	// to survive — not the type, and not the message.
	if !errors.Is(spawnErr, ErrNoAccountAvailable) {
		t.Errorf("err = %v; a remote dispatch with no account must classify as the "+
			"same configuration fault a local one does", spawnErr)
	}
}

// TestAttachTurnFailureEvidence_PreservesCodeAndMessage covers what attaching a
// detail must NOT do.
//
// The status carries the mapping mapRuntimeError chose — NotFound for a missing
// session, PermissionDenied for a refused mount — and every layer above reads
// that code to decide an HTTP status or a retry. A detail that rewrote either
// would turn a clean 404 into something else while appearing to add
// information.
func TestAttachTurnFailureEvidence_PreservesCodeAndMessage(t *testing.T) {
	local := localPreFirstTurnError()

	for _, code := range []codes.Code{
		codes.Internal, codes.NotFound, codes.PermissionDenied,
		codes.InvalidArgument, codes.FailedPrecondition, codes.Unimplemented,
	} {
		t.Run(code.String(), func(t *testing.T) {
			original := status.Error(code, "the original message")

			for name, attached := range map[string]error{
				"turn failure":        AttachTurnFailureEvidence(original, local),
				"configuration fault": AttachSpawnFaultEvidence(original, &NoAccountAvailableError{TenantID: "t"}),
			} {
				t.Run(name, func(t *testing.T) {
					st, ok := status.FromError(attached)
					if !ok {
						t.Fatal("attaching a detail destroyed the status")
					}
					if st.Code() != code {
						t.Errorf("code = %v, want %v", st.Code(), code)
					}
					if st.Message() != "the original message" {
						t.Errorf("message = %q, want it unchanged", st.Message())
					}
					if len(st.Details()) != 1 {
						t.Errorf("details = %d, want exactly the one attached", len(st.Details()))
					}
				})
			}
		})
	}
}

// TestAttachEvidence_LeavesANonStatusErrorAlone is the fail-safe: a caller that
// hands this a plain error gets it back, rather than a status that invents a
// code nobody chose.
func TestAttachEvidence_LeavesANonStatusErrorAlone(t *testing.T) {
	plain := errors.New("not a status")
	if got := AttachTurnFailureEvidence(plain, localPreFirstTurnError()); got != plain {
		t.Errorf("AttachTurnFailureEvidence rewrote a non-status error as %T", got)
	}
	if got := AttachSpawnFaultEvidence(plain, &NoAccountAvailableError{}); got != plain {
		t.Errorf("AttachSpawnFaultEvidence rewrote a non-status error as %T", got)
	}
	// And an error carrying nothing to attach passes straight through.
	st := status.Error(codes.Internal, "ordinary")
	if got := AttachTurnFailureEvidence(st, errors.New("ordinary")); got != st {
		t.Errorf("an error with no evidence was rewrapped as %T", got)
	}
	if got := AttachSpawnFaultEvidence(st, errors.New("ordinary")); got != st {
		t.Errorf("an error with no fault class was rewrapped as %T", got)
	}
}

// TestWrapRemoteSpawnFault_AnUnknownClassIsLeftAlone covers the
// forward-compatible direction: a daemon newer than this build names a class
// this build cannot map, and guessing would be worse than the pre-existing
// answer.
func TestWrapRemoteSpawnFault_AnUnknownClassIsLeftAlone(t *testing.T) {
	st, err := status.New(codes.Internal, "refused").
		WithDetails(&proto.SpawnFaultEvidence{FaultClass: "some_future_fault"})
	if err != nil {
		t.Fatalf("WithDetails: %v", err)
	}
	original := st.Err()
	if got := wrapRemoteSpawnFault("Spawn", original); got != original {
		t.Errorf("an unknown fault class was reconstructed as %T; guessing at a "+
			"configuration fault this build does not know is worse than charging "+
			"the attempt", got)
	}
}

// TestSpawnFaultClasses_RoundTripToTheirSentinels pins the class set to the
// sentinels it reconstructs, in both directions.
//
// The set is a JUDGEMENT about which spawn failures started no process, so it
// is the kind of thing that drifts: a class added to one map and not the other
// would silently stop reconstructing, and the item would be charged again.
func TestSpawnFaultClasses_RoundTripToTheirSentinels(t *testing.T) {
	faults := []struct {
		name     string
		err      error
		sentinel error
	}{
		{"an empty pool", &NoAccountAvailableError{TenantID: "t"}, ErrNoAccountAvailable},
		{"no resolver wired", &AccountResolverNotConfiguredError{Backend: "acp"}, ErrAccountResolverNotConfigured},
		{"a missing credential", &AccountCredentialNotFoundError{TenantID: "t", AccountID: "a"}, ErrAccountCredentialNotFound},
		{"no refresher for the provider", &AccountRefresherNotConfiguredError{Provider: "claude"}, ErrAccountRefresherNotConfigured},
		{"a pool that is merely cooling down", &AccountsCoolingDownError{TenantID: "t", Candidates: 2}, ErrAccountsCoolingDown},
	}

	for _, f := range faults {
		t.Run(f.name, func(t *testing.T) {
			class := spawnFaultClassOf(f.err)
			if class == "" {
				t.Fatalf("%T is not classified as a spawn fault", f.err)
			}
			sentinel, known := spawnFaultSentinels[class]
			if !known {
				t.Fatalf("class %q has no sentinel to reconstruct", class)
			}
			if sentinel != f.sentinel {
				t.Errorf("class %q reconstructs %v, want %v", class, sentinel, f.sentinel)
			}

			// And end to end through the wire, which is what actually has to
			// hold: the sentinel is what every downstream errors.Is asks.
			onTheWire := AttachSpawnFaultEvidence(status.Error(codes.Internal, "refused"), f.err)
			wrapped := wrapRemoteSpawnFault("Spawn", onTheWire)
			if !errors.Is(wrapped, f.sentinel) {
				t.Errorf("after a round trip, errors.Is(%v) is false", f.sentinel)
			}
		})
	}

	// Every sentinel in the map must be produced by some error, or the map has
	// a class nothing can ever send.
	produced := make(map[string]bool, len(faults))
	for _, f := range faults {
		produced[spawnFaultClassOf(f.err)] = true
	}
	for class := range spawnFaultSentinels {
		if !produced[class] {
			t.Errorf("class %q is in the sentinel map but no error produces it", class)
		}
	}
}

// TestSpawnFaultClassOf_TransientFailuresStayCharged is the asymmetric
// counterpart to the set above: these errors must NOT be classified, so the
// item is charged and retried rather than parked.
//
// WHAT THIS TEST PROVES IS THE CLASSIFICATION AND NOTHING ELSE. Review of this
// branch pointed out that an earlier version of this comment claimed more —
// that a refresh failure is transient, that the pinned-only errors are
// unreachable under AccountAuto — while the test only calls spawnFaultClassOf.
// Those are the REASONS for the list, established elsewhere, and stating them
// as though this test checked them is how a comment starts lying:
//
//   - AccountRefreshFailedError: transient in the case that matters (the OAuth
//     endpoint was briefly unreachable), and a permanent one self-corrects —
//     recordRefreshFailure marks the account AccountStatusError, which
//     Account.Selectable excludes, so the NEXT auto resolve either finds
//     another account or returns NoAccountAvailable and parks with a reason.
//     Charging one attempt for that is the right price.
//   - The four pinned-account errors: raised only for a named account, and the
//     SDLC worker always resolves with AccountAuto (see workerSpawnAccountAuto),
//     where Selectable filters them out before selection.
func TestSpawnFaultClassOf_TransientFailuresStayCharged(t *testing.T) {
	for _, err := range []error{
		&AccountRefreshFailedError{AccountID: "a", Cause: errors.New("i/o timeout")},
		&AccountRevokedError{AccountID: "a"},
		&AccountErroredError{AccountID: "a"},
		&AccountLimitedError{AccountID: "a"},
		&AccountProviderMismatchError{AccountID: "a"},
		errors.New("exec: \"claude\": executable file not found in $PATH"),
	} {
		if class := spawnFaultClassOf(err); class != "" {
			t.Errorf("%T classified as the configuration fault %q; it must be charged",
				err, class)
		}
	}
}

// TestRemoteACPBackend_SpawnForwardsTheWholeAccountRequest asserts every
// account field the caller stated actually crosses.
//
// ACCOUNTALLOW IS THE ONE THAT MATTERS AND IS ASSERTED FIRST. It is a
// RESTRICTION — empty means the whole tenant/project/provider pool is eligible
// — so dropping it does not fail, it silently broadens the session from the
// caller's chosen set to everything the tenant owns, and the session can bill
// an account its operator deliberately excluded. It was dropped until #235's
// review, the fourth thing this boundary lost.
//
// The whole set is asserted rather than just the field that was broken,
// because a per-field test is what let three of the four through.
func TestRemoteACPBackend_SpawnForwardsTheWholeAccountRequest(t *testing.T) {
	t.Parallel()

	fake := &fakeDaemonServer{spawnResp: &proto.SpawnSessionResponse{SessionId: "sess-1"}}
	daemon := startBufconnDaemon(t, fake)
	backend, err := NewRemoteACPBackend(RemoteACPBackendDeps{
		Registration: newDaemonRegistration(),
		Backend:      "acp",
		Logger:       &agentTestLogger{},
		ConnDialer:   daemon.dialer(),
	})
	if err != nil {
		t.Fatalf("NewRemoteACPBackend: %v", err)
	}
	t.Cleanup(func() { _ = backend.CloseAll() })

	if _, err := backend.Spawn(context.Background(), &SpawnRequest{
		ProjectID:       "proj-1",
		AccountAuto:     true,
		AccountStrategy: "least_usage",
		AccountAllow:    []string{"acct-1", "acct-2"},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	fake.spawnReqMu.Lock()
	got := fake.lastSpawnReq
	fake.spawnReqMu.Unlock()
	if got == nil {
		t.Fatal("the daemon received no spawn request")
	}

	if len(got.GetAccountAllow()) != 2 ||
		got.GetAccountAllow()[0] != "acct-1" || got.GetAccountAllow()[1] != "acct-2" {
		t.Errorf("account_allow = %v, want the caller's rotation pool; dropping a "+
			"RESTRICTION broadens the session to every account the tenant owns",
			got.GetAccountAllow())
	}
	if !got.GetAccountAuto() {
		t.Error("account_auto did not cross")
	}
	if got.GetAccountStrategy() != "least_usage" {
		t.Errorf("account_strategy = %q, want least_usage", got.GetAccountStrategy())
	}
}

// TestRemoteSpawnFault_LeavesTheTransportStatusObservable proves the
// reconstruction ADDS a classification without taking anything away.
//
// Everything above the backend reads the gRPC code to decide an HTTP status or
// whether to retry. A wrapper that hid it would trade one diagnosis for
// another rather than adding one.
func TestRemoteSpawnFault_LeavesTheTransportStatusObservable(t *testing.T) {
	resolverErr := &NoAccountAvailableError{TenantID: "acme", Provider: AccountProvider("claude")}
	onTheWire := AttachSpawnFaultEvidence(
		status.Error(codes.FailedPrecondition, "the original message"), resolverErr)
	wrapped := &RemoteBackendRPCError{Op: "Spawn", Err: wrapRemoteSpawnFault("Spawn", onTheWire)}

	// The new claim.
	if !errors.Is(wrapped, ErrNoAccountAvailable) {
		t.Error("the configuration fault is not reachable through the wrapper")
	}
	// Everything that was already there.
	st, ok := status.FromError(wrapped)
	if !ok {
		t.Fatal("the gRPC status is no longer reachable through the wrapper")
	}
	if st.Code() != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", st.Code())
	}
	var rpcErr *RemoteBackendRPCError
	if !errors.As(wrapped, &rpcErr) {
		t.Error("the transport error type is no longer reachable")
	}
}

// TestBothDetailsOnOneStatus covers the two detail types sharing one status.
//
// They cannot collide — gRPC details are repeated typed Any values and each
// decoder type-checks its own — but "cannot" is worth one test, because the
// alternative is finding out from a spawn that carried both.
func TestBothDetailsOnOneStatus(t *testing.T) {
	local := localPreFirstTurnError()
	resolverErr := &NoAccountAvailableError{TenantID: "acme"}

	both := AttachSpawnFaultEvidence(
		AttachTurnFailureEvidence(status.Error(codes.Internal, "both"), local),
		resolverErr)

	st, ok := status.FromError(both)
	if !ok {
		t.Fatal("attaching two details destroyed the status")
	}
	if len(st.Details()) != 2 {
		t.Fatalf("details = %d, want both", len(st.Details()))
	}

	evidence, carried := TurnFailureEvidenceFromStatus(both)
	if !carried || evidence.TerminalReason != "api_error" {
		t.Errorf("the turn evidence did not survive alongside the fault class: %+v", evidence)
	}
	if !errors.Is(wrapRemoteSpawnFault("Spawn", both), ErrNoAccountAvailable) {
		t.Error("the fault class did not survive alongside the turn evidence")
	}
}

// TestSpawnFaultEvidence_CooldownCarriesItsReturnTime is the round trip the
// sentinel test does not cover.
//
// The sentinel alone would survive with Candidates and AvailableAt dropped, and
// then a remote cooldown's park would read "every provider account is on a
// cooldown; none named a return time" — which, after #235's review, is the
// description of a pool that should have PARKED rather than requeued. The
// metadata is what keeps the remote and local answers the same sentence.
func TestSpawnFaultEvidence_CooldownCarriesItsReturnTime(t *testing.T) {
	// Truncated to the second, which is the wire's resolution.
	at := time.Now().UTC().Add(9 * time.Minute).Truncate(time.Second)
	local := &AccountsCoolingDownError{
		TenantID: "acme", ProjectID: "proj-1",
		Provider: AccountProvider("claude"), Candidates: 3, AvailableAt: at,
	}

	onTheWire := AttachSpawnFaultEvidence(status.Error(codes.Internal, "refused"), local)
	wrapped := wrapRemoteSpawnFault("Spawn", onTheWire)

	var cooling *AccountsCoolingDownError
	if !errors.As(wrapped, &cooling) {
		t.Fatalf("err = %T, want the typed cooldown rebuilt on this side", wrapped)
	}
	if cooling.Candidates != 3 {
		t.Errorf("candidates = %d, want 3: a reconstructed cooldown that lost the count "+
			"describes the pool wrongly", cooling.Candidates)
	}
	if !cooling.AvailableAt.Equal(at) {
		t.Errorf("available_at = %v, want %v: without it the remote park cannot say when "+
			"the pool returns", cooling.AvailableAt, at)
	}
	// The sentinels both still answer, so the classifier is unaffected.
	if !errors.Is(wrapped, ErrAccountsCoolingDown) || !errors.Is(wrapped, ErrNoAccountAvailable) {
		t.Error("the reconstructed cooldown does not satisfy both sentinels")
	}
}

// TestSpawnFaultEvidence_OtherClassesCarryNoCooldownMetadata is the control:
// the two extra fields belong to one class and are not filled for the others.
func TestSpawnFaultEvidence_OtherClassesCarryNoCooldownMetadata(t *testing.T) {
	onTheWire := AttachSpawnFaultEvidence(status.Error(codes.Internal, "refused"),
		&NoAccountAvailableError{TenantID: "acme"})

	st, _ := status.FromError(onTheWire)
	for _, detail := range st.Details() {
		m, ok := detail.(*proto.SpawnFaultEvidence)
		if !ok {
			continue
		}
		if m.GetCandidates() != 0 || m.GetAvailableAtUnix() != 0 {
			t.Errorf("an empty-pool fault carried cooldown metadata: %+v", m)
		}
	}
	var cooling *AccountsCoolingDownError
	if errors.As(wrapRemoteSpawnFault("Spawn", onTheWire), &cooling) {
		t.Error("an empty pool was reconstructed as a cooldown")
	}
}
