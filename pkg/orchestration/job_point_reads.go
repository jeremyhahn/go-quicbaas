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

package orchestration

import (
	"context"
	"encoding/json"
	"sync"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// jobPointReadConcurrency bounds how many job records one listing reads at
// once. A page of sessions can name several hundred jobs; reading them one
// after another would trade the scan's cost for a round trip per job, and
// reading them all at once would let one page occupy the store. Sixteen keeps
// a few hundred reads to a few tens of milliseconds.
const jobPointReadConcurrency = 16

// readPersistedJobs reads the persisted records of the jobs filter names, by
// key, and returns those that match filter.
//
// It is the JobIDs half of collectJobs, and it keeps that function's rules:
// a job already listed from the running set (seen) is not read again, and a
// record that cannot be read or decoded is logged and left out rather than
// failing the listing. A job with no record is simply absent — the caller
// asked about an id, and "no such job" is the answer.
//
// The reads run in the job-record scope, exactly as the scan does: detached
// from the caller's tenant, bounded by the caller's deadline.
func (s *agentServiceImpl) readPersistedJobs(ctx context.Context, tenantID string, filter *JobFilter, seen map[string]struct{}) []*JobStatus {
	ids := make([]string, 0, len(filter.JobIDs))
	wanted := make(map[string]struct{}, len(filter.JobIDs))
	for _, id := range filter.JobIDs {
		if id == "" {
			continue
		}
		if _, running := seen[id]; running {
			continue
		}
		if _, repeated := wanted[id]; repeated {
			continue
		}
		wanted[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}

	readCtx, cancel := jobRecordScope(ctx)
	defer cancel()

	// Each worker writes only its own index, so the slice needs no lock and
	// the answer keeps the order the ids were asked in.
	found := make([]*JobStatus, len(ids))
	next := make(chan int)
	var wg sync.WaitGroup
	workers := min(jobPointReadConcurrency, len(ids))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				found[i] = s.readPersistedJob(readCtx, tenantID, ids[i])
			}
		}()
	}
	for i := range ids {
		next <- i
	}
	close(next)
	wg.Wait()

	jobs := make([]*JobStatus, 0, len(ids))
	for _, status := range found {
		if status != nil && filter.Matches(status) {
			jobs = append(jobs, status)
		}
	}
	return jobs
}

// readPersistedJob reads one job record, nil when there is none or it cannot
// be read. A failure other than absence is logged, as the scan logs its own.
func (s *agentServiceImpl) readPersistedJob(ctx context.Context, tenantID, jobID string) *JobStatus {
	data, err := s.kvStore.Get(ctx, jobPersistenceKey(tenantID, jobID))
	if err != nil {
		if !qerrors.IsNotFound(err) {
			s.logger.Warnf("orchestration: failed to read persisted job %s: %s", jobID, err.Error())
		}
		return nil
	}
	if data == nil {
		return nil
	}
	var status JobStatus
	if err := json.Unmarshal(data, &status); err != nil {
		s.logger.Warnf("orchestration: failed to unmarshal persisted job %s: %s", jobID, err.Error())
		return nil
	}
	return &status
}
