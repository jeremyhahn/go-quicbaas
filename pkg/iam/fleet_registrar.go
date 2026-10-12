// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import "context"

// =============================================================================
// Fleet node Registrar
// =============================================================================
//
// The FleetRegistrar interface is the narrow dependency the mTLS
// registration handler uses to publish newly-registered daemons into
// the central orchestrator's fleet registry. The interface is declared
// here (not imported from pkg/orchestration) to keep the iam package
// free of the orchestration dependency; the glue at
// pkg/server/fleet_registrar_adapter.go supplies an adapter that
// forwards to the real orchestration.FleetRegistry.

// FleetRegistrar publishes newly-registered fleet nodes into the fleet
// registry. Called once per successful registration redemption at the
// mTLS register endpoint.
type FleetRegistrar interface {
	// RegisterFleetNode publishes a fleet node into the fleet registry. The
	// request shape mirrors orchestration.FleetNode via string
	// / string-map fields so this interface carries no cross-package
	// dependency.
	RegisterFleetNode(ctx context.Context, req *FleetRegistrarRequest) error
}

// FleetRegistrarRequest is the data the register handler hands to the
// registrar. It is a flat struct designed to be trivially translatable
// into orchestration.FleetNode by the adapter in pkg/server.
type FleetRegistrarRequest struct {
	// AgentID is the opaque identifier assigned to the daemon when its
	// OAuth2 application was registered.
	NodeID string

	// TenantID is the tenant claim extracted from the daemon's mTLS
	// peer certificate.
	TenantID string

	// Name is a human-readable label for the registry row.
	Name string

	// Host is the daemon's reachable bind address.
	Host string

	// AdvertisedAddress is the address the daemon declared the control plane
	// should dial, when it declared one; Host is then the same address.
	AdvertisedAddress string

	// Reachability is the outcome of the control plane's probe of Host:
	// orchestration.FleetNodeReachable, or FleetNodeReachabilityUnverified
	// when no probe ran.
	Reachability string

	// Backends is the declared backend list from the register payload.
	Backends []string

	// AgentType is a short label identifying the kind of runtime
	// (e.g. "agent-runtime").
	NodeType string

	// Kind classifies the runtime ("daemon" for long-lived fleet nodes).
	Kind string

	// Metadata carries free-form context for audit / debugging.
	Metadata map[string]string
}

// DefaultFleetRuntimeScopes returns the fleet node runtime's SERVICE-SURFACE
// scopes: the three permissions a caller must hold to spawn, prompt and poll a
// session on a daemon. The daemon's own gRPC spawn server checks them
// (pkg/fleet GRPCSpawnServer.authorize), and the central's outbound dispatch
// client is granted them so it can reach a daemon at all. Kept in sync with
// pkg/orchestration/agent_identity.go.
//
// This is NOT the whole of a daemon's grant -- see FleetRuntimeGrant.
func DefaultFleetRuntimeScopes() []string {
	return []string{
		"agent:runtime:spawn",
		"agent:runtime:prompt",
		"agent:runtime:status",
	}
}

// Control-plane permissions a fleet node daemon exercises against the central.
// They are qrdb's canonical permission names (go-qrdb pkg/permission
// OrchestratorAgentHeartbeat / OrchestratorHook), restated here rather than
// imported for the same reason DefaultFleetRuntimeScopes is: pkg/iam does not
// depend on the orchestration or permission packages. TestAgentRuntimeGrant_*
// pins the literals against the routes that enforce them.
const (
	// FleetScopeControlHeartbeat is what POST
	// /api/v1/fleet/{nodeID}/heartbeat and the gRPC
	// OrchestratorService/FleetNodeHeartbeat method require. Without it an
	// enrolled daemon is refused on every heartbeat, never becomes
	// dispatchable, and the fleet registry never sees it (go-quicbaas#653).
	FleetScopeControlHeartbeat = "orchestrator:agent_heartbeat"

	// FleetScopeControlHook is what the gRPC OrchestratorService/PushEvents
	// bidi-stream requires; it is how a daemon forwards session events to the
	// central's session event log.
	FleetScopeControlHook = "orchestrator:hook"

	// FleetScopeAccountMaterialize is what POST
	// /api/v1/agent-accounts/{id}/materialize requires: the release of ONE
	// pooled provider account's credential envelope to the daemon that will
	// run the fleet node it belongs to.
	//
	// Without it a remote spawn that names a pooled account is refused
	// `no_account_resolver` on the worker host, because the daemon has no way
	// to reach the tenant's pool and the central deliberately never puts the
	// credential in the Spawn RPC (go-quicbaas#734).
	//
	// The literal is QuicBaaS's own contributed permission
	// (pkg/permission.AgentAccountMaterialize), restated here for the same
	// reason the two above are: pkg/iam does not depend on the permission or
	// orchestration packages. TestAgentRuntimeGrant_MatchesTheContributedVocabulary
	// pins the two spellings against each other.
	FleetScopeAccountMaterialize = "agent_accounts:materialize"

	// FleetScopeAccountRead and FleetScopeAccountList are what the pooled
	// account METADATA reads require: GET /api/v1/agent-accounts/{id} (and its
	// /usage sibling) and GET /api/v1/agent-accounts/.
	//
	// A daemon needs both because SELECTION runs on the daemon: it lists the
	// tenant's pool, applies the strategy and the allow/exclude policy the
	// central forwarded, and only then asks for the chosen account's
	// credential. Without the reads it has nothing to select over, and a
	// pooled spawn fails before it ever reaches the release verb.
	//
	// Neither read returns credential material — that is what makes them
	// separable from FleetScopeAccountMaterialize, and why holding them is not
	// a way to obtain a login.
	FleetScopeAccountRead = "agent_accounts:read"
	FleetScopeAccountList = "agent_accounts:list"
)

// FleetRuntimeGrant returns the complete permission set granted to an enrolled
// fleet node daemon, in qrdb's canonical "resource:action" vocabulary. It is the
// value stamped onto the daemon's OAuth2 application record, which bounds
// every token the daemon is issued, and the value the register endpoint
// advertises back to it.
//
// The set is deliberately two things at once, because a daemon is both a
// client and a server:
//
//   - the control-plane permissions it exercises OUTBOUND (heartbeat, event
//     push) -- the half that was missing and left the whole fleet refused 403;
//   - the service-surface permissions its own spawn server checks INBOUND,
//     which it reads off the identity enrollment handed it, so dropping them
//     would make it refuse the central's dispatch;
//   - the three pooled-account permissions it exercises OUTBOUND: two metadata
//     reads it SELECTS over (FleetScopeAccountRead / FleetScopeAccountList) and
//     the one release verb that hands it the chosen account's credential
//     (FleetScopeAccountMaterialize). Together they let it resolve a pooled
//     provider account against the tenant's central pool on the host that runs
//     the fleet node. Dropping any of them makes every pooled spawn on a remote
//     daemon fail closed, which is the defect go-quicbaas#734 recorded.
//
// It grants nothing else. A fleet node runtime holds no authority over tasks,
// jobs, other fleet nodes, inference, or its tenant's data and key material: it
// must not be able to administer the platform that runs it.
func FleetRuntimeGrant() []string {
	return append(DefaultFleetRuntimeScopes(),
		FleetScopeControlHeartbeat,
		FleetScopeControlHook,
		FleetScopeAccountMaterialize,
		FleetScopeAccountRead,
		FleetScopeAccountList,
	)
}
