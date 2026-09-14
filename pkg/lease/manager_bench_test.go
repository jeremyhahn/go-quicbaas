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

package lease

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// WHAT IS WORTH MEASURING HERE.
//
// Validate is on the request path of every credential delivery: a pooled
// provider credential cannot be released without one, so its cost is paid per
// spawn and per refresh. ReapExpired is on no request path at all but scans the
// whole lease keyspace, so its cost scales with the number of leases rather than
// with the request rate -- which is the thing to watch as a fleet grows.
// =============================================================================

// benchLogger is the sink a benchmarked manager writes to. Errors only: an info
// logger would make the benchmark measure log formatting.
func benchLogger(b *testing.B) interfaces.Logger {
	b.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		b.Fatalf("logger.New() error = %v", err)
	}
	return logger.NewAdapter(log)
}

// newBenchManager builds a manager over qrdb's in-memory engine.
func newBenchManager(b *testing.B) *Manager {
	b.Helper()
	mgr, err := NewManager(&ManagerConfig{Store: kvstore.NewMemory(), Logger: benchLogger(b)})
	if err != nil {
		b.Fatalf("NewManager() error = %v", err)
	}
	b.Cleanup(func() {
		if err := mgr.Close(); err != nil {
			b.Fatalf("Close() error = %v", err)
		}
	})
	return mgr
}

// BenchmarkManager_CreateLease measures one grant: an ID from the CSPRNG, a JSON
// encode and one point write.
func BenchmarkManager_CreateLease(b *testing.B) {
	mgr := newBenchManager(b)
	ctx := context.Background()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.CreateLease(ctx, "bench/"+strconv.Itoa(i), time.Hour, nil); err != nil {
			b.Fatalf("CreateLease() error = %v", err)
		}
	}
}

// BenchmarkManager_Validate measures the ENFORCEMENT path: one point read, one
// JSON decode and the state test. It is what every credential delivery pays.
func BenchmarkManager_Validate(b *testing.B) {
	mgr := newBenchManager(b)
	ctx := context.Background()

	held, err := mgr.CreateLease(ctx, "bench/validate", time.Hour, nil)
	if err != nil {
		b.Fatalf("CreateLease() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mgr.Validate(ctx, held.LeaseID); err != nil {
			b.Fatalf("Validate() error = %v", err)
		}
	}
}

// BenchmarkManager_ValidateParallel measures the same path under concurrency,
// because a fleet's deliveries arrive in parallel and the manager's own locking
// must not be what serializes them.
func BenchmarkManager_ValidateParallel(b *testing.B) {
	mgr := newBenchManager(b)
	ctx := context.Background()

	held, err := mgr.CreateLease(ctx, "bench/validate-parallel", time.Hour, nil)
	if err != nil {
		b.Fatalf("CreateLease() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := mgr.Validate(ctx, held.LeaseID); err != nil {
				b.Fatalf("Validate() error = %v", err)
			}
		}
	})
}

// BenchmarkManager_ReapExpired measures one sweep over a populated keyspace,
// half of it already lapsed. It is the cost that grows with the fleet rather
// than with the request rate.
func BenchmarkManager_ReapExpired(b *testing.B) {
	const leases = 512
	ctx := context.Background()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		mgr := newBenchManager(b)
		for j := 0; j < leases; j++ {
			held, err := mgr.CreateLease(ctx, "bench/reap/"+strconv.Itoa(j), time.Hour, nil)
			if err != nil {
				b.Fatalf("CreateLease() error = %v", err)
			}
			if j%2 == 0 {
				// Lapse it by rewinding its expiry, which is how every expiry
				// assertion in this package advances the clock.
				if _, err := mgr.RenewLease(ctx, held.LeaseID, -time.Hour); err != nil {
					b.Fatalf("RenewLease() error = %v", err)
				}
			}
		}
		b.StartTimer()

		if _, _, err := mgr.ReapExpired(ctx); err != nil {
			b.Fatalf("ReapExpired() error = %v", err)
		}
	}
}
