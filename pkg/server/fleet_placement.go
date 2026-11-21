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

package server

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// fleetPlacementSessionRole names the placement role of a session. A
// SessionRecord carries no role yet (open question 2 of
// docs/agent-fleet/placement.md), so the one classification the record
// supports is used: a local session (BackendLocal) ran on the control plane,
// which no placement spec governs, and a remote session is agent work, with
// or without a recorded host. A remote session with no recorded host is then
// refused by the drift report as unnameable, not dropped from it.
func fleetPlacementSessionRole(record *orchestration.SessionRecord) string {
	if record.BackendLocal {
		return ""
	}
	return "agent"
}

// bindFleetPlacement binds the facade's fleet placement service to a spec
// store over kv and a drift reporter over the fleet registry and the
// session store. A facade that serves no fleet placement is left alone.
func bindFleetPlacement(svc any, kv interfaces.KVStore, registry orchestration.FleetRegistry, sessions orchestration.AgentStore) error {
	provider, ok := svc.(service.FleetPlacementProvider)
	if !ok {
		return nil
	}
	binder, ok := provider.FleetPlacement().(service.FleetPlacementBinder)
	if !ok {
		return nil
	}
	specs, err := orchestration.NewPlacementStore(kv)
	if err != nil {
		return err
	}
	inventory, err := orchestration.NewFleetInventory(registry)
	if err != nil {
		return err
	}
	// Sessions carry no role, so only agent work is observed: a report for
	// any other role is refused rather than reported clean.
	work, err := orchestration.NewSessionWorkSourceForRoles(sessions, fleetPlacementSessionRole, "agent")
	if err != nil {
		return err
	}
	drift, err := orchestration.NewDriftReporter(specs, inventory, work)
	if err != nil {
		return err
	}
	consents, err := orchestration.NewConsentLedger(kv)
	if err != nil {
		return err
	}
	binder.BindFleetPlacement(specs, drift)
	if consentBinder, ok := provider.FleetPlacement().(service.FleetPlacementConsentBinder); ok {
		consentBinder.BindFleetPlacementConsent(consents)
	}
	return nil
}
