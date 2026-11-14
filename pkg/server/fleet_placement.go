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
// supports is used: a session that landed on a fleet host is agent work, and
// a session with no recorded host ran on the control plane, which no
// placement spec governs.
func fleetPlacementSessionRole(record *orchestration.SessionRecord) string {
	if record.HostAffinity == "" {
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
	work, err := orchestration.NewSessionWorkSource(sessions, fleetPlacementSessionRole)
	if err != nil {
		return err
	}
	drift, err := orchestration.NewDriftReporter(specs, inventory, work)
	if err != nil {
		return err
	}
	binder.BindFleetPlacement(specs, drift)
	return nil
}
