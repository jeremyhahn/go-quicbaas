// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import schedengine "github.com/jeremyhahn/go-quicbaas/pkg/scheduler"

// ---------------------------------------------------------------------------
// Scheduler ENGINE job model
//
// These are the in-process engine's job-model types, distinct from the
// Scheduler* CLIENT aliases (SchedulerClient / SchedulerJobInfo / ...) which are
// remote-call DTOs. A platform consumer that embeds the engine (obtained via the
// embedded server's SchedulerEngine accessor) and registers in-process
// JobHandlers authors them through these aliases, so it reaches the engine job
// model through the quicbaas SDK rather than importing go-quicbaas/pkg/scheduler
// directly (the Layer-5 SDK-only boundary).
// ---------------------------------------------------------------------------

type (
	// SchedulerEngine is the in-process job scheduler. Consumers receive an
	// instance from the embedded server and register handlers on it.
	SchedulerEngine = schedengine.SchedulerEngine
	// Job is a scheduled unit of work.
	Job = schedengine.Job
	// JobHandler executes a Job when the engine fires it.
	JobHandler = schedengine.JobHandler
	// JobQuery filters jobs in ListJobs.
	JobQuery = schedengine.JobQuery
	// JobResult is a handler's outcome.
	JobResult = schedengine.JobResult
	// JobType distinguishes one-shot / recurring / delayed jobs.
	JobType = schedengine.JobType
	// JobStatus is a job's lifecycle state.
	JobStatus = schedengine.JobStatus
	// OwnerType identifies who owns a job (system / tenant / ...).
	OwnerType = schedengine.OwnerType
)

// Scheduler engine constants.
const (
	JobTypeRecurring = schedengine.JobTypeRecurring
	JobStatusPending = schedengine.JobStatusPending
	OwnerTypeSystem  = schedengine.OwnerTypeSystem
)
