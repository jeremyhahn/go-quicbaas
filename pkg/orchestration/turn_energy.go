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
	"sort"
	"sync"
	"time"
)

// =============================================================================
// Measured turn energy
// =============================================================================
//
// A local turn's energy is what its device drew while the turn ran, divided
// among the turns that shared the device. Both halves are measured where they
// can be:
//
//   - THE DRAW comes from the fleet node's device telemetry (fleet_telemetry.go)
//     for the host the session ran on and the device class its engine uses
//     (orchestration.pricing.engine_devices). A GPU's power draw is read from
//     its sensor; an NPU has no power sensor, so its configured draw is scaled
//     by the busy fraction its driver reports; a CPU package's power is derived
//     from its RAPL energy counter.
//   - THE SHARE comes from the turns this control plane ran on the same host and
//     device class at the same time (turnOccupancy). Over each stretch of the
//     window the device's energy is split evenly among the turns in flight, so
//     two concurrent turns on one GPU are not each charged the whole GPU.
//
// Only when there is no telemetry covering the window does the turn fall back
// to the configured draw times its wall-clock duration, and the ledger entry
// says which it was (ConsumptionEntry.EnergyProvenance).

// telemetryMaxGap is the widest stretch one telemetry point is allowed to
// speak for: three default heartbeat intervals. A window whose device was not
// sampled within it is not measured, because holding a stale reading across a
// longer gap would report a load change nobody saw.
const telemetryMaxGap = 90 * time.Second

// occupancyRetention is how long a finished turn's span is kept to share a
// later turn's window. It bounds the tracker's memory and must exceed the
// longest turn worth sharing with.
const occupancyRetention = 6 * time.Hour

// MeasuredEnergy is the energy a turn drew from its device, as the fleet's
// telemetry reports it.
type MeasuredEnergy struct {
	// MilliWh is the turn's share of the device's energy over its window,
	// in milliwatt-hours.
	MilliWh float64

	// Provenance is ProvenanceMeasured when every figure came from a power
	// sensor, and ProvenanceDerived when any came from a counter or from a
	// busy fraction scaled by the configured draw.
	Provenance Provenance

	// Kind is the device class the energy was drawn from, and Devices how
	// many devices of it the host reported.
	Kind    DeviceKind
	Devices int
}

// occupancyKey is the device class of one host. Turns from every tenant share
// it: a GPU does not know whose turn it is serving.
type occupancyKey struct {
	host string
	kind DeviceKind
}

// occupancySpan is one turn's stay on a device. A zero end is a turn still
// running.
type occupancySpan struct {
	start time.Time
	end   time.Time
}

// turnOccupancy records which turns ran on which device class of which host,
// and when, so a device's energy can be split among the turns that shared it.
//
// It holds state guarded by a mutex rather than a lock-free structure: every
// operation reads or rewrites a small map under one key, and turns begin and
// end at most a few times a second. The zero value is ready to use.
type turnOccupancy struct {
	mu    sync.Mutex
	spans map[occupancyKey]map[string]*occupancySpan
}

// begin records turnID starting on key at at, and prunes spans that finished
// longer ago than occupancyRetention.
func (o *turnOccupancy) begin(key occupancyKey, turnID string, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.spans == nil {
		o.spans = make(map[occupancyKey]map[string]*occupancySpan)
	}
	o.pruneLocked(at)
	turns, ok := o.spans[key]
	if !ok {
		turns = make(map[string]*occupancySpan)
		o.spans[key] = turns
	}
	turns[turnID] = &occupancySpan{start: at}
}

// end records turnID finishing on key at at. A turn never begun is ignored.
func (o *turnOccupancy) end(key occupancyKey, turnID string, at time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if span, ok := o.spans[key][turnID]; ok {
		span.end = at
	}
}

// sharers returns the spans of every turn other than turnID on key that
// overlap [from, to]. A running turn's span is closed at to.
func (o *turnOccupancy) sharers(key occupancyKey, turnID string, from, to time.Time) []occupancySpan {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []occupancySpan
	for id, span := range o.spans[key] {
		if id == turnID {
			continue
		}
		end := span.end
		if end.IsZero() || end.After(to) {
			end = to
		}
		if span.start.Before(to) && end.After(from) {
			out = append(out, occupancySpan{start: span.start, end: end})
		}
	}
	return out
}

// pruneLocked drops finished spans older than occupancyRetention.
func (o *turnOccupancy) pruneLocked(now time.Time) {
	horizon := now.Add(-occupancyRetention)
	for key, turns := range o.spans {
		for id, span := range turns {
			if !span.end.IsZero() && span.end.Before(horizon) {
				delete(turns, id)
			}
		}
		if len(turns) == 0 {
			delete(o.spans, key)
		}
	}
}

// integrateDeviceEnergy works out a turn's share of the energy devices drew
// over [from, to].
//
// Each device is integrated on its own and the results summed. The window is
// cut at every telemetry point and every sharer's start and end, and each piece
// is charged the device's draw at its midpoint divided by the number of turns
// in flight there. A device's draw is its power series when it has one, and
// otherwise drawWatts scaled by its utilisation series. A piece no point
// within telemetryMaxGap speaks for fails the whole integration: a measurement
// that covers part of a window is not a measurement of the window.
func integrateDeviceEnergy(devices []*deviceSeries, kind DeviceKind, from, to time.Time, sharers []occupancySpan, drawWatts float64) (*MeasuredEnergy, bool) {
	if len(devices) == 0 || !to.After(from) {
		return nil, false
	}
	energy := &MeasuredEnergy{Provenance: ProvenanceMeasured, Kind: kind, Devices: len(devices)}
	for _, device := range devices {
		joules, provenance, ok := integrateOneDevice(device, from, to, sharers, drawWatts)
		if !ok {
			return nil, false
		}
		energy.MilliWh += joules / 3.6
		if provenance == ProvenanceDerived {
			energy.Provenance = ProvenanceDerived
		}
	}
	return energy, true
}

// integrateOneDevice integrates one device's share over [from, to] in joules,
// and reports the weakest provenance of the figures it used.
func integrateOneDevice(device *deviceSeries, from, to time.Time, sharers []occupancySpan, drawWatts float64) (float64, Provenance, bool) {
	points := device.power
	scale := 1.0
	derived := false
	if len(points) == 0 {
		// No power sensor: the configured draw scaled by the busy fraction.
		if drawWatts <= 0 || len(device.utilization) == 0 {
			return 0, ProvenanceUnknown, false
		}
		points = device.utilization
		scale = drawWatts
		derived = true
	}

	cuts := []time.Time{from, to}
	for _, p := range points {
		if p.at.After(from) && p.at.Before(to) {
			cuts = append(cuts, p.at)
		}
	}
	for _, s := range sharers {
		for _, at := range []time.Time{s.start, s.end} {
			if at.After(from) && at.Before(to) {
				cuts = append(cuts, at)
			}
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })

	joules := 0.0
	for i := 0; i+1 < len(cuts); i++ {
		a, b := cuts[i], cuts[i+1]
		if !b.After(a) {
			continue
		}
		mid := a.Add(b.Sub(a) / 2)
		point, ok := pointAt(points, mid)
		if !ok {
			return 0, ProvenanceUnknown, false
		}
		if point.provenance == ProvenanceDerived {
			derived = true
		}
		inFlight := 1
		for _, s := range sharers {
			if !mid.Before(s.start) && mid.Before(s.end) {
				inFlight++
			}
		}
		joules += point.value * scale * b.Sub(a).Seconds() / float64(inFlight)
	}
	if derived {
		return joules, ProvenanceDerived, true
	}
	return joules, ProvenanceMeasured, true
}

// pointAt returns the point that speaks for at: the latest one at or before
// it, else the earliest one after it, provided it lies within telemetryMaxGap.
func pointAt(points []telemetryPoint, at time.Time) (telemetryPoint, bool) {
	idx := sort.Search(len(points), func(i int) bool { return points[i].at.After(at) })
	if idx > 0 {
		if before := points[idx-1]; at.Sub(before.at) <= telemetryMaxGap {
			return before, true
		}
	}
	if idx < len(points) {
		if after := points[idx]; after.at.Sub(at) <= telemetryMaxGap {
			return after, true
		}
	}
	return telemetryPoint{}, false
}

// =============================================================================
// Service wiring
// =============================================================================

// SetFleetTelemetryStore installs the store fleet device telemetry is filed in
// and turn energy is measured from. Without it every heartbeat's telemetry is
// dropped and every local turn's energy is estimated.
func (s *agentServiceImpl) SetFleetTelemetryStore(store *FleetTelemetryStore) {
	s.telemetry = store
}

// recordFleetTelemetry files one heartbeat's device samples. It is
// best-effort: a heartbeat is a liveness signal first, and a node must not be
// reported dead because its telemetry could not be stored. The failure is
// logged, and the turns it would have measured are estimated instead.
func (s *agentServiceImpl) recordFleetTelemetry(ctx context.Context, tenantID, agentID string, devices []DeviceSample) {
	if s.telemetry == nil || devices == nil || s.fleetRegistry == nil {
		return
	}
	registration, err := s.fleetRegistry.GetFleetNode(ctx, tenantID, agentID)
	if err != nil {
		s.logger.Warnf("fleet telemetry: node %s could not be read; its device telemetry is dropped: %s", agentID, err.Error())
		return
	}
	if err := s.telemetry.Record(ctx, tenantID, agentID, registration.Host, time.Now().UTC(), devices); err != nil {
		s.logger.Warnf("fleet telemetry: node %s: %s", agentID, err.Error())
	}
}

// turnOccupancyKey is the device class a turn on engine, in the session
// sessionID, occupies — or false when the engine is not bound to a device.
func (s *agentServiceImpl) turnOccupancyKey(engine, sessionID string) (occupancyKey, bool) {
	kind, ok := s.prices.EngineDevice(engine)
	if !ok {
		return occupancyKey{}, false
	}
	host := s.sessionOfTurn(completedTurn{sessionID: sessionID}).host
	if host == "" {
		return occupancyKey{}, false
	}
	return occupancyKey{host: host, kind: kind}, true
}

// beginTurnOccupancy records a turn starting on its device and returns the
// function that records it finishing.
func (s *agentServiceImpl) beginTurnOccupancy(turnID, engine, sessionID string, at time.Time) func(time.Time) {
	key, ok := s.turnOccupancyKey(engine, sessionID)
	if !ok || turnID == "" {
		return func(time.Time) {}
	}
	s.occupancy.begin(key, turnID, at)
	return func(end time.Time) { s.occupancy.end(key, turnID, end) }
}

// measureTurnEnergy integrates the fleet's telemetry over entry's window, or
// returns nil when nothing measured it: no telemetry store, an engine bound to
// no device, a turn with no duration, or a window the host's telemetry does not
// cover. A read failure is logged and treated as no measurement — the entry is
// still recorded, with its energy estimated and marked so.
func (s *agentServiceImpl) measureTurnEnergy(ctx context.Context, entry *ConsumptionEntry, settings *CostSettings) *MeasuredEnergy {
	if s.telemetry == nil || entry.DurationMillis <= 0 || entry.Host == "" {
		return nil
	}
	kind, ok := s.prices.EngineDevice(entry.Engine)
	if !ok {
		return nil
	}
	to := entry.OccurredAt.UTC()
	from := to.Add(-time.Duration(entry.DurationMillis) * time.Millisecond)
	devices, err := s.telemetry.deviceSeries(ctx, entry.TenantID, entry.Host, kind,
		from.Add(-telemetryMaxGap), to.Add(telemetryMaxGap))
	if err != nil {
		s.logger.Warnf("fleet telemetry: turn %s energy is estimated: %s", entry.EntryID, err.Error())
		return nil
	}
	draw, _ := s.prices.EngineDraw(entry.TenantID, entry.Engine, entry.Model, settings)
	sharers := s.occupancy.sharers(occupancyKey{host: entry.Host, kind: kind}, entry.EntryID, from, to)
	energy, ok := integrateDeviceEnergy(devices, kind, from, to, sharers, draw)
	if !ok {
		return nil
	}
	return energy
}
