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

package workerimage

import (
	"context"
	"time"
)

// The schedule's defaults.
const (
	// DefaultRefreshInterval is how often a pass runs by itself: daily, the
	// owner's cadence. It is also how often the npm registry is read — a pass
	// is the only thing that reads it, so there is no separate, more frequent
	// check (owner ruling on #1030: an hourly registry poll is too
	// aggressive). An operator who knows a release shipped asks for a pass.
	DefaultRefreshInterval = 24 * time.Hour

	// DefaultPassTimeout bounds one pass: a cold build of the image is tens of
	// minutes, the gate a few more.
	DefaultPassTimeout = time.Hour
)

// Schedule is how often the loop refreshes and how long a pass may take. A
// zero field takes its default.
type Schedule struct {
	RefreshInterval time.Duration
	PassTimeout     time.Duration
}

// withDefaults fills the zero fields.
func (s Schedule) withDefaults() Schedule {
	if s.RefreshInterval <= 0 {
		s.RefreshInterval = DefaultRefreshInterval
	}
	if s.PassTimeout <= 0 {
		s.PassTimeout = DefaultPassTimeout
	}
	return s
}

// Run drives the refresher until ctx ends: a full pass every RefreshInterval,
// and a full pass whenever one is requested (RequestRefresh — the CLI, the
// API, the web UI's "Check for updates now"). Passes run one at a time on this
// goroutine, so two can never race over the same bindings.
//
// NOTHING RUNS AT START. A daemon restarts far more often than a CLI ships,
// and a pass reads the npm registry; the first scheduled pass comes one
// interval after start, and an operator who cannot wait requests one.
func (r *Refresher) Run(ctx context.Context, schedule Schedule) {
	schedule = schedule.withDefaults()
	r.serving.Store(true)
	defer r.serving.Store(false)

	refresh := time.NewTicker(schedule.RefreshInterval)
	defer refresh.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			r.bounded(ctx, schedule, TriggerSchedule)
		case trigger := <-r.kick:
			r.bounded(ctx, schedule, trigger)
		}
	}
}

// bounded runs one pass under the schedule's timeout.
func (r *Refresher) bounded(ctx context.Context, schedule Schedule, trigger Trigger) {
	if ctx.Err() != nil {
		return
	}
	passCtx, cancel := context.WithTimeout(ctx, schedule.PassTimeout)
	defer cancel()
	r.Refresh(passCtx, trigger)
}
