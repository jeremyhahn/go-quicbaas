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
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// Carrying the evidence across the daemon boundary
// =============================================================================
//
// THE DAEMON THAT RAN THE PROCESS IS THE ONLY PARTY THAT SAW THE CLI'S RESULT
// OBJECT, and until this file the control plane never heard about it.
//
// #235 taught the SDLC module to refund an attempt that never reached a model,
// on the CLI's own structured account of the failure. That works in-process,
// where the typed error travels by pointer. Over a remote daemon it did not:
// the daemon mapped the failure to a gRPC status (pkg/fleet, mapRuntimeError),
// the client wrapped the transport error, and the evidence was gone. A
// distributed worker went on burning its attempt on exactly the pre-model
// failure a local one has refunded since #235 — the same local-fixed,
// distributed-silently-not shape as #218's dropped argv and #235's own dropped
// account selection, which is now three occurrences of one mistake.
//
// So the evidence is forwarded, the way the account fields and the permission
// grant are forwarded: an additive, optional proto message that a daemon
// without it simply does not send and a client without it simply does not read.
//
// IT RIDES THE ERROR, NOT THE RESPONSE. A failed turn IS the RPC's error, so
// there is no response message to put it in; gRPC's own mechanism for
// structured error data is a status detail, and that is what this uses.
//
// WHAT IS DELIBERATELY LEFT BEHIND: the CLI's result / stderr text. It is the
// field most likely to hold a credential, it is already withheld from every
// error string, and putting it on the wire to be logged at the other end would
// undo that for no gain — nothing downstream reads it. Only the reason and the
// counts travel, and the reason is re-checked against this side's own allowlist
// on arrival (ReasonClass), so a daemon cannot make the control plane render a
// string of its choosing.

// RemoteTurnFailureError reports a turn that failed on a remote daemon,
// carrying the evidence that daemon's CLI produced.
//
// A DISTINCT TYPE RATHER THAN A REBUILT StreamJSONError, because it is a
// different claim about the world. StreamJSONError says "this process's
// stream-json protocol returned an error result" and owns fields — the command,
// the withheld result text — that this side never saw. Reconstructing one here
// would be inventing provenance. What this says is exactly what is known: a
// daemon reported a failed turn and handed over this account of it.
type RemoteTurnFailureError struct {
	// Op names the RPC, so a reader knows which call this came back from.
	Op string

	// TurnFailureEvidence is what the remote CLI reported. Its ReasonClass is
	// resolved against THIS process's allowlist, not the daemon's.
	TurnFailureEvidence

	// Err is the transport error this was attached to, preserved so the
	// original status code and message stay reachable.
	Err error
}

// Error implements the error interface.
//
// The reason class is rendered under the same rule every other carrier of this
// evidence follows: the compiled-in constant, never the wire value.
func (e *RemoteTurnFailureError) Error() string {
	msg := "remote_acp_backend: turn failed on " + e.Op
	if class := e.ReasonClass(); class != "" {
		msg += " terminal_reason=" + class
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the transport error so the gRPC status stays reachable.
func (e *RemoteTurnFailureError) Unwrap() error { return e.Err }

// turnFailureEvidenceToProto renders evidence for the wire, or nil when there
// is nothing to say.
//
// Nil for empty evidence rather than a zero message, so a daemon whose backend
// produced no structured account attaches no detail at all — the difference
// between "reported nothing" and "reported zeros" has to survive the trip, and
// an always-attached message would erase it.
func turnFailureEvidenceToProto(e TurnFailureEvidence) *proto.TurnFailureEvidence {
	if e.TerminalReason == "" && e.Usage == nil && e.NumTurns == 0 && e.ProviderLimit == nil && e.ProviderAuth == nil {
		return nil
	}
	out := &proto.TurnFailureEvidence{
		TerminalReason: e.TerminalReason,
		NumTurns:       int32(e.NumTurns),
	}
	// THE LIMIT VERDICT TRAVELS, because only this side read the stream it
	// was drawn from. Its signal is redacted again on the way out: it is the
	// one piece of result text that crosses, and it crosses clean.
	if limit := e.ProviderLimit.canonical(); limit != nil {
		out.ProviderLimitSource = string(limit.Source)
		out.ProviderLimitSignal = limit.Signal
		if !limit.ResetsAt.IsZero() {
			out.ProviderLimitResetsAtUnix = limit.ResetsAt.Unix()
		}
	}
	// THE LOGIN VERDICT TRAVELS TOO, redacted again on the way out.
	if auth := e.ProviderAuth.canonical(); auth != nil {
		out.ProviderAuthRejected = true
		out.ProviderAuthSignal = auth.Signal
	}
	if e.Usage != nil {
		out.UsageReported = true
		out.InputTokens = e.Usage.InputTokens
		out.OutputTokens = e.Usage.OutputTokens
		out.CacheCreationInputTokens = e.Usage.CacheCreationInputTokens
		out.CacheReadInputTokens = e.Usage.CacheReadInputTokens
	}
	return out
}

// turnFailureEvidenceFromProto rebuilds evidence received from a daemon.
//
// usage_reported is what reconstructs the nil/zero distinction: a message
// carrying false leaves Usage nil, so a turn whose CLI reported no accounting
// is not refunded as though it had reported zero.
func turnFailureEvidenceFromProto(m *proto.TurnFailureEvidence) (TurnFailureEvidence, bool) {
	if m == nil {
		return TurnFailureEvidence{}, false
	}
	out := TurnFailureEvidence{
		TerminalReason: m.GetTerminalReason(),
		NumTurns:       int(m.GetNumTurns()),
	}
	// An empty source is "no limit reported". A source this build does not
	// know is still a limit — canonical renames it rather than dropping it.
	if source := m.GetProviderLimitSource(); source != "" {
		limit := &ProviderLimitEvidence{
			Source: ProviderLimitSource(source),
			Signal: m.GetProviderLimitSignal(),
		}
		if at := m.GetProviderLimitResetsAtUnix(); at > 0 {
			limit.ResetsAt = time.Unix(at, 0).UTC()
		}
		out.ProviderLimit = limit.canonical()
	}
	if m.GetProviderAuthRejected() {
		out.ProviderAuth = (&ProviderAuthEvidence{Signal: m.GetProviderAuthSignal()}).canonical()
	}
	if m.GetUsageReported() {
		out.Usage = &TokenUsage{
			InputTokens:              m.GetInputTokens(),
			OutputTokens:             m.GetOutputTokens(),
			CacheCreationInputTokens: m.GetCacheCreationInputTokens(),
			CacheReadInputTokens:     m.GetCacheReadInputTokens(),
		}
		// A SECOND DECODE BOUNDARY, and it is not JSON, so UnmarshalJSON never
		// runs here. The counts arrive from a remote daemon's protobuf, which
		// got them from a CLI on that host — the same untrusted producer, one
		// hop further away. Saying the repair lives "where the value enters"
		// and then leaving this entrance open would make the claim false (#242).
		out.Usage.normalize()
	}
	return out, true
}

// AttachTurnFailureEvidence returns rpcErr with err's turn-failure evidence
// attached as a gRPC status detail, or rpcErr unchanged when there is none.
//
// Called by the daemon on the failure path. Best effort by construction: if the
// detail cannot be attached — a status that refuses details, a marshal failure
// — the original error is returned untouched, because a diagnostic that cannot
// be carried must not turn a failed turn into a failed RPC of a different kind.
func AttachTurnFailureEvidence(rpcErr error, err error) error {
	evidence, carried := EvidenceOf(err)
	if !carried {
		return rpcErr
	}
	detail := turnFailureEvidenceToProto(evidence)
	if detail == nil {
		return rpcErr
	}
	st, ok := status.FromError(rpcErr)
	if !ok {
		return rpcErr
	}
	withDetail, attachErr := st.WithDetails(detail)
	if attachErr != nil {
		return rpcErr
	}
	return withDetail.Err()
}

// TurnFailureEvidenceFromStatus reads the evidence a daemon attached to a
// failed RPC, if any.
//
// Called by the client on the failure path. Absent detail means an older
// daemon, or a failure that carried no structured account: both answer false,
// and the caller treats the failure exactly as it did before #235.
func TurnFailureEvidenceFromStatus(err error) (TurnFailureEvidence, bool) {
	if err == nil {
		return TurnFailureEvidence{}, false
	}
	st, ok := status.FromError(err)
	if !ok {
		return TurnFailureEvidence{}, false
	}
	for _, detail := range st.Details() {
		if m, isEvidence := detail.(*proto.TurnFailureEvidence); isEvidence {
			return turnFailureEvidenceFromProto(m)
		}
	}
	return TurnFailureEvidence{}, false
}

// wrapRemoteTurnFailure returns err carrying any turn-failure evidence the
// daemon attached, or err unchanged when it carried none.
//
// This is the client-side seam: everything downstream — the SDLC module's
// classification above all — then sees a typed error whose evidence answers
// PreFirstTurnFault identically whether the process ran here or on another
// host, which is the whole point.
func wrapRemoteTurnFailure(op string, err error) error {
	evidence, carried := TurnFailureEvidenceFromStatus(err)
	if !carried {
		return err
	}
	return &RemoteTurnFailureError{Op: op, TurnFailureEvidence: evidence, Err: err}
}

// =============================================================================
// The other half: a spawn that never started a process
// =============================================================================
//
// A dispatch whose pooled account cannot be resolved is the SAME accounting
// question as a CLI that died before its first turn — nothing was attempted, so
// nothing should be charged — and it arrives by a different route: the
// resolver's typed error, raised before the process is started
// (provisionAndSpawn). In-process that error travels by pointer and the SDLC
// classifier matches it with errors.Is. Across a daemon it did not, so a remote
// worker was charged for a process that was never created.
//
// The class travels; the error does not. The control plane maps the token back
// onto its OWN sentinel, so classifyEnvironmentFault needs no knowledge that a
// daemon was involved and every downstream consumer is unchanged.

// The spawn fault classes. A closed set, and deliberately not the sdlc
// package's fault names: those are that package's vocabulary for what to tell a
// human, while these identify which orchestration sentinel to reconstruct.
const (
	spawnFaultNoAccountAvailable    = "no_account_available"
	spawnFaultResolverNotConfigured = "resolver_not_configured"

	// The nolint is gosec's G101, which matches any identifier or literal
	// containing "cred" against a string constant. This is the NAME OF A
	// FAILURE MODE — the whole point of the design one file over is that a
	// credential never crosses this boundary — and renaming it to dodge the
	// pattern would cost the one word that says what failed. Same
	// justification the provider registry uses for its environment-variable
	// names (account_provider_registry.go).
	//
	// On the constant itself rather than the const block: a nolint on the
	// block's opening line does not cover the specs inside it, which is how
	// this reached CI once already.
	spawnFaultCredentialNotFound = "credential_not_found" //nolint:gosec // G101: a fault-class name, not a credential

	spawnFaultRefresherNotConfigured = "refresher_not_configured"

	// spawnFaultCoolingDown is the one class that does NOT mean somebody has
	// to act. It is carried for the opposite reason to the others: so the
	// control plane does not mistake a busy pool for an empty one and park
	// work that clears itself. See AccountsCoolingDownError.
	spawnFaultCoolingDown = "accounts_cooling_down"

	// spawnFaultSaturated is the cooldown's sibling: a pool whose selectable
	// accounts are all at their session cap. Busy, not empty, and it clears
	// when a session ends. See AccountsSaturatedError.
	spawnFaultSaturated = "accounts_saturated"

	// spawnFaultSpent is a pool whose selectable accounts have all spent
	// their own allowance. Busy, not empty, and it names when it frees up.
	// See AccountsSpentError.
	spawnFaultSpent = "accounts_spent"

	// spawnFaultSessionWorkspace is a named workspace the daemon could not
	// provision or find (go-quicbaas#981): a pinned checkout that is gone or
	// moved, a daemon with no projects root, a workspace of another tenant.
	// No process was started, and the job's checkout is not somewhere a
	// retry on another host could continue it.
	spawnFaultSessionWorkspace = "session_workspace"
)

// spawnFaultSentinels maps a wire class onto the sentinel it reconstructs.
//
// Same rule as knownTerminalReasons: the wire value is a KEY, never rendered.
// A class this build does not know is simply absent from the map and the
// failure stays whatever it already was.
var spawnFaultSentinels = map[string]error{
	spawnFaultNoAccountAvailable:     ErrNoAccountAvailable,
	spawnFaultResolverNotConfigured:  ErrAccountResolverNotConfigured,
	spawnFaultCredentialNotFound:     ErrAccountCredentialNotFound,
	spawnFaultRefresherNotConfigured: ErrAccountRefresherNotConfigured,
	spawnFaultCoolingDown:            ErrAccountsCoolingDown,
	spawnFaultSaturated:              ErrAccountsSaturated,
	spawnFaultSpent:                  ErrAccountsSpent,
	spawnFaultSessionWorkspace:       ErrSessionWorkspace,
}

// SpawnFaultClass returns the wire name of err's configuration-fault class, or
// "" when err is not one.
//
// It is the ONE vocabulary a spawn-path failure crosses a process boundary in:
// the gRPC spawn evidence below, and the REST error envelope's `fault_class` on
// the credential-release verb (pkg/api/rest/handlers_account.go), which an agent
// daemon maps back through SpawnFaultSentinel. Without it a daemon whose
// release was refused for a missing credential would see an opaque HTTP
// status, report no class, and the control plane would CHARGE an attempt it
// should have parked.
func SpawnFaultClass(err error) string {
	return spawnFaultClassOf(err)
}

// SpawnFaultSentinel returns the orchestration sentinel a wire class
// reconstructs. The second result is false for a class this build does not
// know, which callers must leave as whatever failure it already was.
func SpawnFaultSentinel(class string) (error, bool) {
	sentinel, ok := spawnFaultSentinels[class]
	return sentinel, ok
}

// spawnFaultClassOf names the class err belongs to, or "" when it is not a
// configuration fault.
// WHICH FAILURES ARE IN THIS SET IS A JUDGEMENT, so it is written down. All
// four say the same thing: the spawn started no process, and no amount of
// retrying will change that without a human. The first two are the pool having
// nothing to offer; the second two are an account the pool DID select whose
// credential could not be materialized, which the SDLC worker reaches on its
// ordinary auto path (AccountTokenManager.Resolve, after selection).
//
// DELIBERATELY ABSENT: AccountRefreshFailedError. A refresh can fail because
// the OAuth endpoint was briefly unreachable, and parking an item on a
// condition that fixes itself is worse than spending one of its attempts —
// the budget exists for exactly that kind of failure.
//
// Also absent, because the SDLC worker cannot reach them: revoked, errored,
// limited and provider-mismatched accounts. Each is raised only for a PINNED
// account, and the worker always resolves with AccountAuto, where
// Account.Selectable filters all four out before selection — an auto request
// against a pool of nothing but revoked accounts gets NoAccountAvailable,
// which is the first class here.
func spawnFaultClassOf(err error) string {
	switch {
	// FIRST, for the reason the SDLC classifier tests it first: a cooldown
	// unwraps to ErrNoAccountAvailable as well, so the narrower case has to be
	// asked about before the general one or a busy pool crosses the wire
	// labelled as an empty one and parks.
	case errors.Is(err, ErrAccountsCoolingDown):
		return spawnFaultCoolingDown
	case errors.Is(err, ErrAccountsSaturated):
		return spawnFaultSaturated
	case errors.Is(err, ErrAccountsSpent):
		return spawnFaultSpent
	case errors.Is(err, ErrNoAccountAvailable):
		return spawnFaultNoAccountAvailable
	case errors.Is(err, ErrAccountResolverNotConfigured):
		return spawnFaultResolverNotConfigured
	case errors.Is(err, ErrAccountCredentialNotFound):
		return spawnFaultCredentialNotFound
	case errors.Is(err, ErrAccountRefresherNotConfigured):
		return spawnFaultRefresherNotConfigured
	case errors.Is(err, ErrSessionWorkspace):
		return spawnFaultSessionWorkspace
	}
	return ""
}

// RemoteSpawnFaultError reports a spawn a remote daemon refused as a
// configuration fault.
//
// UNWRAP RETURNS THE SENTINEL, which is the whole design: the SDLC classifier
// asks errors.Is(err, ErrNoAccountAvailable) and gets the same answer whether
// the resolver ran here or on another host, without knowing a daemon exists.
//
// What it CANNOT reconstruct is the detail the local error carries — which
// tenant, which project, which provider — because none of that crosses. The
// reason it composes therefore says less than a local one; saying less is the
// price of not putting a daemon's strings into a public comment, and the class
// is what decides the accounting.
type RemoteSpawnFaultError struct {
	// Op names the RPC.
	Op string

	// Sentinel is the orchestration error this class reconstructs.
	Sentinel error

	// Err is the transport error, preserved so the status stays reachable.
	Err error
}

// Error implements the error interface.
func (e *RemoteSpawnFaultError) Error() string {
	msg := "remote_acp_backend: spawn refused on " + e.Op
	if e.Sentinel != nil {
		msg += ": " + e.Sentinel.Error()
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and the transport error, so errors.Is
// matches the configuration fault and the status alike.
func (e *RemoteSpawnFaultError) Unwrap() []error {
	out := make([]error, 0, 2)
	if e.Sentinel != nil {
		out = append(out, e.Sentinel)
	}
	if e.Err != nil {
		out = append(out, e.Err)
	}
	return out
}

// AttachSpawnFaultEvidence returns rpcErr with err's configuration-fault class
// attached, or rpcErr unchanged when err is not one.
//
// Best effort, for the reason AttachTurnFailureEvidence is: a diagnostic that
// cannot be carried must not change what kind of failure this is.
func AttachSpawnFaultEvidence(rpcErr error, err error) error {
	class := spawnFaultClassOf(err)
	if class == "" {
		return rpcErr
	}
	st, ok := status.FromError(rpcErr)
	if !ok {
		return rpcErr
	}
	detail := &proto.SpawnFaultEvidence{FaultClass: class}
	var cooling *AccountsCoolingDownError
	if errors.As(err, &cooling) {
		detail.Candidates = int32(cooling.Candidates)
		if !cooling.AvailableAt.IsZero() {
			detail.AvailableAtUnix = cooling.AvailableAt.UTC().Unix()
		}
	}
	var saturated *AccountsSaturatedError
	if errors.As(err, &saturated) {
		detail.Candidates = int32(saturated.Candidates)
	}
	var spent *AccountsSpentError
	if errors.As(err, &spent) {
		detail.Candidates = int32(spent.Candidates)
		if !spent.AvailableAt.IsZero() {
			detail.AvailableAtUnix = spent.AvailableAt.UTC().Unix()
		}
	}
	withDetail, attachErr := st.WithDetails(detail)
	if attachErr != nil {
		return rpcErr
	}
	return withDetail.Err()
}

// wrapRemoteSpawnFault returns err carrying any configuration-fault class the
// daemon attached, or err unchanged.
func wrapRemoteSpawnFault(op string, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	for _, detail := range st.Details() {
		m, isFault := detail.(*proto.SpawnFaultEvidence)
		if !isFault {
			continue
		}
		sentinel, known := spawnFaultSentinels[m.GetFaultClass()]
		if !known {
			// A class this build does not know. Treated exactly as before —
			// guessing at an unknown configuration fault would be worse than
			// charging the attempt, which is at least the pre-existing answer.
			return err
		}
		// A cooldown is rebuilt as the typed error rather than the bare
		// sentinel, so the reason the control plane composes can still say
		// WHEN the pool returns. The other classes carry nothing but
		// themselves, and a bare sentinel is the honest reconstruction.
		if m.GetFaultClass() == spawnFaultCoolingDown {
			cooling := &AccountsCoolingDownError{Candidates: int(m.GetCandidates())}
			if at := m.GetAvailableAtUnix(); at > 0 {
				cooling.AvailableAt = time.Unix(at, 0).UTC()
			}
			return &RemoteSpawnFaultError{Op: op, Sentinel: cooling, Err: err}
		}
		// Saturation is rebuilt as its typed error for the same reason: it
		// unwraps to ErrNoAccountAvailable too, and a bare sentinel would not.
		if m.GetFaultClass() == spawnFaultSaturated {
			saturated := &AccountsSaturatedError{Candidates: int(m.GetCandidates())}
			return &RemoteSpawnFaultError{Op: op, Sentinel: saturated, Err: err}
		}
		// A spent pool is rebuilt with its return time, for the cooldown's
		// reason: the task waits exactly that long.
		if m.GetFaultClass() == spawnFaultSpent {
			spent := &AccountsSpentError{Candidates: int(m.GetCandidates())}
			if at := m.GetAvailableAtUnix(); at > 0 {
				spent.AvailableAt = time.Unix(at, 0).UTC()
			}
			return &RemoteSpawnFaultError{Op: op, Sentinel: spent, Err: err}
		}
		return &RemoteSpawnFaultError{Op: op, Sentinel: sentinel, Err: err}
	}

	// NO EVIDENCE AND NO AGENT. Every branch above rebuilds a class from
	// SpawnFaultEvidence the AGENT attached to its status — which an agent that
	// is not listening cannot do. Unavailable is that case, and it is the one
	// spawn fault the agent can never report about itself, so the control plane
	// has to name it: the RPC did not arrive, so no session was created, no
	// container started and no token was spent.
	//
	// ONLY Unavailable. A DeadlineExceeded may have been a request the agent
	// received and was slow to answer, which can leave a real session behind;
	// calling that unreached would refund an attempt that did run. Everything
	// else keeps the pre-existing answer, which is to charge.
	if st.Code() == codes.Unavailable {
		return &RemoteSpawnFaultError{Op: op, Sentinel: ErrFleetNodeUnreachable, Err: err}
	}
	return err
}

// ErrFleetNodeUnreachable reports a spawn the fleet node daemon never received.
//
// A SENTINEL RATHER THAN A TYPE because it carries nothing beyond itself: the
// transport error it wraps already names the address that refused, and the
// class it maps to already carries the explanation an operator reads.
var ErrFleetNodeUnreachable = errors.New("orchestration: the fleet node daemon did not answer the spawn")

// WrapRemoteSpawnFaultForTest exposes wrapRemoteSpawnFault to the daemon
// package's tests, which drive a real gRPC server and need the control plane's
// own reconstruction to assert against.
//
// Test-facing and named so: the production caller is RemoteACPBackend.Spawn,
// and a second production entry point would be a second place for the two
// halves of this boundary to drift apart.
func WrapRemoteSpawnFaultForTest(op string, err error) error {
	return wrapRemoteSpawnFault(op, err)
}
