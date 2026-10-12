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

package server

import (
	"context"
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// fleetRegistrarAdapter bridges the narrow iam.FleetRegistrar interface to
// the richer orchestration.FleetRegistry. The iam package deliberately does
// not import pkg/orchestration to keep layer boundaries clean; this adapter
// lives in pkg/server where both packages meet.
//
// Used by the mTLS register handler (pkg/api/rest/handlers_fleet_enroll.go) to
// publish enrollment-time registrations. The orchestration layer gets the
// same registry instance via Server.fleetRegistry + SetFleetRegistry, so
// dispatch sees the daemon the moment enrollment completes — no waiting
// for the first heartbeat.
type fleetRegistrarAdapter struct {
	registry orchestration.FleetRegistry
}

// Compile-time interface check.
var _ iam.FleetRegistrar = (*fleetRegistrarAdapter)(nil)

// newFleetRegistrarAdapter constructs an adapter over the given registry.
func newFleetRegistrarAdapter(registry orchestration.FleetRegistry) *fleetRegistrarAdapter {
	return &fleetRegistrarAdapter{registry: registry}
}

// NewFleetRegistrar returns the fleet registrar the mTLS register handler is
// wired with, over the supplied registry.
//
// Exported so an end-to-end scenario drives the REGISTRAR THIS SERVER USES
// rather than a copy of it. A test that forks this translation cannot fail
// when the translation is wrong, which is exactly what it exists to catch.
func NewFleetRegistrar(registry orchestration.FleetRegistry) iam.FleetRegistrar {
	return newFleetRegistrarAdapter(registry)
}

// RegisterFleetNode translates the flat iam.FleetRegistrarRequest into a proper
// orchestration.FleetNode and publishes it to the fleet registry.
//
// Multiple backends on a single daemon yield a single registration whose
// Backend field holds the primary backend name plus a Capabilities list of
// all backends. This matches how container-script fleet nodes are recorded and
// lets ListFleetNodes(filter.Backend="ollama") find daemons that serve multiple
// backend kinds.
//
// A DAEMON THAT IS ALREADY IN THE REGISTRY IS REFRESHED, NOT REFUSED. A daemon
// re-registers on every restart, on certificate renewal and whenever its
// cached credentials are discarded, and the registration it sends is the
// current truth about it. The guarded create answers "agent already exists"
// for all of them, and this used to be the end of it: the handler logged the
// refusal and the record kept the values of the FIRST registration forever --
// the address a dispatcher dialled, the backends it matched on, the OAuth2
// client an operator revoked through (go-quicbaas#646). The second call
// converges the record instead, under the record's own principal.
//
// The create is attempted FIRST rather than probing for the record and
// choosing: of N nodes registering one fleet node ID exactly one create succeeds,
// and the losers all take the refresh path, which is the same outcome by a
// route that never has to decide a race itself.
func (a *fleetRegistrarAdapter) RegisterFleetNode(ctx context.Context, req *iam.FleetRegistrarRequest) error {
	if a.registry == nil {
		return nil // No-op when the central is running without orchestration
	}

	now := time.Now().UnixNano()
	primary := ""
	if len(req.Backends) > 0 {
		primary = req.Backends[0]
	}

	reg := &orchestration.FleetNode{
		ID:                req.NodeID,
		TenantID:          req.TenantID,
		Name:              req.Name,
		Host:              req.Host,
		AdvertisedAddress: req.AdvertisedAddress,
		Reachability:      orchestration.FleetNodeReachability(req.Reachability),
		Backend:           primary,
		NodeType:          req.NodeType,
		Kind:              orchestration.FleetNodeKind(req.Kind),
		Status:            orchestration.FleetNodeStatusRegistered,
		Capabilities:      req.Backends,
		LastSeen:          now,
		RegisteredAt:      now,
		Metadata:          req.Metadata,
	}

	err := a.registry.RegisterFleetNode(ctx, reg)
	var exists *orchestration.FleetNodeAlreadyExistsError
	if !errors.As(err, &exists) {
		return err
	}

	// The record the create lost to may be removed by an operator between the
	// two calls, in which case the refresh reports not-found. That is
	// reported, not retried: a removal is a deliberate act and a registrar
	// that raced it back into existence would undo it. The daemon's next
	// enrollment registers cleanly.
	return a.registry.RefreshFleetNode(ctx, reg)
}
