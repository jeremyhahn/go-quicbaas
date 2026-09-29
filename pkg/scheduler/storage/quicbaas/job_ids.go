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

package quicbaas

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/cas"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// =============================================================================
// Job ID allocation
// =============================================================================
//
// A job ID is the key its record is stored under, so an ID handed out twice is
// not a cosmetic duplicate: the second CreateJob overwrites the first job. The
// counter behind allocation is therefore durable, and every allocation is
// persisted BEFORE the ID is returned — an ID that was never persisted is an
// ID a restarted node can hand out again.
//
// The counter record holds the LAST ID allocated, as a JSON number. It only
// ever moves forward, which is what makes the clustered allocator's
// value-compare swap sound (cas.Writer.Swap): a value once written never
// recurs, so there is no ABA sequence for a stale reader to be fooled by.
//
// Allocation stays below scheduler.SystemJobIDFloor. IDs at or above it are
// reserved for platform-defined system jobs, which carry fixed IDs, so the two
// ranges can never meet.

// jobIDAllocator hands out job IDs that are unique across restarts and, for a
// clustered store, across nodes.
type jobIDAllocator interface {
	// initialize makes the durable counter readable, seeding it on first use.
	initialize(ctx context.Context) error

	// next allocates one ID and persists the counter before returning it.
	next(ctx context.Context) (uint64, error)
}

// localJobIDs allocates IDs for a single-node store.
//
// One process owns the store, so a mutex held across the increment AND the
// persist is the complete arbitration. Both halves must be under it: two
// allocations that incremented concurrently could persist out of order, leave
// the lower ID as the durable counter, and reissue the higher one after a
// restart.
type localJobIDs struct {
	kv interfaces.KVStore

	mu     sync.Mutex
	loaded bool
	last   uint64
}

// initialize loads the counter, seeding it when this store has never
// persisted one.
func (a *localJobIDs) initialize(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loadLocked(ctx)
}

// loadLocked reads the durable counter once. The caller holds a.mu.
func (a *localJobIDs) loadLocked(ctx context.Context) error {
	if a.loaded {
		return nil
	}

	data, err := a.kv.Get(ctx, JobIDCounterKey)
	if err != nil && !dberrors.IsNotFound(err) {
		// Starting from a guess on a read failure would reissue IDs that live
		// jobs already hold, and CreateJob would overwrite them.
		return &scheduler.ErrStorageError{Op: "InitializeJobIDCounter.get", Cause: err}
	}

	// An empty value is read as absent, the rule GetJob applies to job
	// records in this store: some KVStores answer a missing key with no
	// bytes rather than a not-found error. It is safe here because an absent
	// counter is SEEDED from the job records, never restarted from zero.
	var last uint64
	if err == nil && len(data) > 0 {
		last, err = decodeJobIDCounter(data)
		if err != nil {
			return &scheduler.ErrStorageError{Op: "InitializeJobIDCounter.decode", Cause: err}
		}
	} else {
		last, err = seedJobIDCounter(ctx, a.kv)
		if err != nil {
			return err
		}
		// Persisting the seed makes the scan a one-time cost.
		if err := a.kv.Put(ctx, JobIDCounterKey, encodeJobIDCounter(last)); err != nil {
			return &scheduler.ErrStorageError{Op: "InitializeJobIDCounter.put", Cause: err}
		}
	}

	a.last = last
	a.loaded = true
	return nil
}

// next allocates the next ID. The in-memory counter advances only once the
// store has accepted the new value, so a failed persist hands nothing out.
func (a *localJobIDs) next(ctx context.Context) (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.loadLocked(ctx); err != nil {
		return 0, err
	}
	id, err := nextJobID(a.last)
	if err != nil {
		return 0, err
	}
	if err := a.kv.Put(ctx, JobIDCounterKey, encodeJobIDCounter(id)); err != nil {
		return 0, &scheduler.ErrStorageError{Op: "AllocateJobID.put", Cause: err}
	}
	a.last = id
	return id, nil
}

// clusterJobIDs allocates IDs for a clustered store.
//
// Every allocation is a compare-and-swap on the counter record through
// go-qrdb's transaction plane — the same primitive that decides firing claims
// (pkg/cas) — so two nodes allocating at once cannot both advance the counter
// from the same value. There is no in-memory counter to go stale: the store
// is read on every allocation, which is what makes a peer's allocations
// visible here.
type clusterJobIDs struct {
	// kv holds the job records. It is read only to seed an absent counter.
	kv interfaces.KVStore

	reader ClaimReader
	writer *cas.Writer
}

// initialize ensures the counter record exists, seeding it on first use.
func (a *clusterJobIDs) initialize(ctx context.Context) error {
	_, err := a.read(ctx)
	return err
}

// read returns the counter record's current bytes, creating it from the seed
// when no node has written it yet.
func (a *clusterJobIDs) read(ctx context.Context) ([]byte, error) {
	held, err := a.reader.Get(ctx, JobIDCounterKey)
	if err == nil {
		if len(held) == 0 {
			return nil, &scheduler.ErrStorageError{Op: "AllocateJobID.read", Cause: ErrUnreadableJobIDCounter}
		}
		return held, nil
	}
	if !dberrors.IsNotFound(err) {
		return nil, &scheduler.ErrStorageError{Op: "AllocateJobID.read", Cause: err}
	}

	seed, err := seedJobIDCounter(ctx, a.kv)
	if err != nil {
		return nil, err
	}
	// Create is guarded on the key never having been written. Losing it
	// means a peer seeded first, which is equally good: the record exists
	// either way, and the read below returns whichever value won.
	if _, err := a.writer.Create(ctx, JobIDCounterKey, encodeJobIDCounter(seed)); err != nil {
		return nil, err
	}
	held, err = a.reader.Get(ctx, JobIDCounterKey)
	if err != nil {
		return nil, &scheduler.ErrStorageError{Op: "AllocateJobID.read", Cause: err}
	}
	if len(held) == 0 {
		return nil, &scheduler.ErrStorageError{Op: "AllocateJobID.read", Cause: ErrUnreadableJobIDCounter}
	}
	return held, nil
}

// next allocates one ID by advancing the counter from the value just read.
//
// A lost swap means a peer allocated in between, so the loop re-reads and
// tries again: every lost round is another node's successful allocation, so
// the system as a whole always makes progress, and the caller's context bounds
// how long this node keeps trying. A swap refused against a record that has
// NOT changed is contradictory — only a write can fail a compare against the
// exact bytes just read — and is reported rather than retried forever.
func (a *clusterJobIDs) next(ctx context.Context) (uint64, error) {
	var previous []byte
	for {
		if err := ctx.Err(); err != nil {
			return 0, &scheduler.ErrStorageError{Op: "AllocateJobID", Cause: err}
		}

		held, err := a.read(ctx)
		if err != nil {
			return 0, err
		}
		if previous != nil && bytes.Equal(previous, held) {
			return 0, &scheduler.ErrStorageError{Op: "AllocateJobID.swap", Cause: cas.ErrStalled}
		}

		last, err := decodeJobIDCounter(held)
		if err != nil {
			return 0, &scheduler.ErrStorageError{Op: "AllocateJobID.decode", Cause: err}
		}
		id, err := nextJobID(last)
		if err != nil {
			return 0, err
		}

		won, err := a.writer.Swap(ctx, JobIDCounterKey, held, encodeJobIDCounter(id))
		if err != nil {
			return 0, err
		}
		if won {
			return id, nil
		}
		previous = held
	}
}

// nextJobID returns the ID after last, refusing to cross into the range
// reserved for system jobs.
func nextJobID(last uint64) (uint64, error) {
	id := last + 1
	if id >= scheduler.SystemJobIDFloor {
		return 0, &scheduler.ErrStorageError{Op: "AllocateJobID", Cause: ErrJobIDSpaceExhausted}
	}
	return id, nil
}

// encodeJobIDCounter renders the counter record. A JSON number, the format the
// record has always had.
func encodeJobIDCounter(last uint64) []byte {
	return strconv.AppendUint(nil, last, 10)
}

// decodeJobIDCounter parses the counter record. An empty or non-numeric record
// is refused: allocating past a counter that cannot be read is allocating from
// a guess.
func decodeJobIDCounter(data []byte) (uint64, error) {
	if len(data) == 0 {
		return 0, ErrUnreadableJobIDCounter
	}
	var last uint64
	if err := json.Unmarshal(data, &last); err != nil {
		return 0, err
	}
	return last, nil
}

// jobKeyMarker is the path segment every job record key contains, system and
// tenant alike (keyForJob).
const jobKeyMarker = "scheduler/jobs/"

// tenantKeyPrefix begins every tenant-scoped key (TenantJobsPrefixFmt).
const tenantKeyPrefix = "t:"

// seedJobIDCounter returns the highest allocated job ID already present in
// the store, the value a counter that has never been persisted must start
// from.
//
// Job records predate the durable counter, so a store can hold jobs and no
// counter. Starting such a store from zero is exactly the reuse this counter
// exists to prevent, so the first initialisation scans for the highest ID in
// use — system-namespace jobs below SystemJobIDFloor and every tenant's jobs —
// and the result is persisted so the scan never repeats. IDs at or above the
// floor are fixed system-job IDs, never allocated, and are ignored.
//
// The ID is read from the key rather than the value, so the scan decodes no
// job records.
func seedJobIDCounter(ctx context.Context, kv interfaces.KVStore) (uint64, error) {
	var highest uint64
	visit := func(key string, _ []byte) error {
		// The marker must open a namespace segment ("__sys:" or
		// "t:{tenant}:"), so a tenant's own key that merely contains the
		// phrase is not read as a job.
		i := strings.LastIndex(key, jobKeyMarker)
		if i <= 0 || key[i-1] != ':' {
			return nil
		}
		id, err := strconv.ParseUint(key[i+len(jobKeyMarker):], 10, 64)
		if err != nil || id >= scheduler.SystemJobIDFloor {
			return nil
		}
		if id > highest {
			highest = id
		}
		return nil
	}

	if err := kv.Scan(ctx, SystemJobsPrefix, visit); err != nil {
		return 0, &scheduler.ErrStorageError{Op: "InitializeJobIDCounter.scan", Cause: err}
	}
	if err := kv.Scan(ctx, tenantKeyPrefix, visit); err != nil {
		return 0, &scheduler.ErrStorageError{Op: "InitializeJobIDCounter.scan", Cause: err}
	}
	return highest, nil
}
