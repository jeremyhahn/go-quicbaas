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

package rollout

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
)

var (
	_ SlotRuntime         = PlaneSlots{}
	_ NameSwitch          = PlaneNames{}
	_ serviceplane.Roller = PlaneRoller{}
)

// PlaneSlots is the SlotRuntime over a plane's own slot verbs: Fill is
// Plane.FillSlot and Drain is Plane.DrainSlot, which start and stop slot
// instances through the plane's launcher. The plane fences each call
// durably with the claim's epoch, so an older one is refused with ErrFenced
// (the plane's error, answered unchanged).
type PlaneSlots struct {
	Plane *serviceplane.Plane
}

// Fill starts r in slot under f's epoch.
func (s PlaneSlots) Fill(ctx context.Context, f Fence, slot Slot, r *serviceplane.Release) error {
	return s.Plane.FillSlot(ctx, f.Epoch, string(slot), r)
}

// Drain stops what slot runs under f's epoch.
func (s PlaneSlots) Drain(ctx context.Context, f Fence, slot Slot) error {
	return s.Plane.DrainSlot(ctx, f.Epoch, f.Tenant, f.Service, string(slot))
}

// PlaneNames is the NameSwitch over Plane.SwitchSlot: the switch applies
// the service's grants for the slot's manifest, which moves its registered
// name, under the claim's epoch.
type PlaneNames struct {
	Plane *serviceplane.Plane
}

// Switch makes slot the service's active instance under f's epoch.
func (n PlaneNames) Switch(ctx context.Context, f Fence, slot Slot) error {
	return n.Plane.SwitchSlot(ctx, f.Epoch, f.Tenant, f.Service, string(slot))
}

// PlaneRoller is the plane's Roller: it rolls a signed release out through
// the Controller, which verifies it against its keyring before any slot is
// filled.
type PlaneRoller struct {
	Controller *Controller
}

// Roll rolls sr out for tenant and answers the service it is for; on an
// error the service is answered when it is known.
func (p PlaneRoller) Roll(ctx context.Context, tenant string, sr *serviceplane.SignedRelease) (string, error) {
	slots, err := p.Controller.Roll(ctx, tenant, sr)
	var name string
	if slots != nil {
		name = slots.Service
	}
	return name, err
}

// ResumeAll resumes every rollout in flight through the controller; the
// node's recovery calls it so the leader takes over rollouts in flight.
func (p PlaneRoller) ResumeAll(ctx context.Context) ([]*Slots, error) {
	return p.Controller.ResumeAll(ctx)
}

// NewPlaneStrategy answers the ABSwitch that rolls releases through plane's
// slot verbs and appraises each filled slot with appraiser.
func NewPlaneStrategy(plane *serviceplane.Plane, appraiser Appraiser) ABSwitch {
	return ABSwitch{Slots: PlaneSlots{Plane: plane}, Appraiser: appraiser, Names: PlaneNames{Plane: plane}}
}
