# Scheduler — User Guide

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Task-oriented guide to creating, inspecting and controlling scheduled jobs.
> Handler contracts and clustering internals live in `engineering.md` and
> `architecture.md`.

The scheduler runs work on a timetable: once at a moment, repeatedly on a cron
expression, or after a delay. Jobs are stored server-side and survive restarts,
and in a cluster exactly one node owns the timetable at a time.

## Prerequisites

- A running server with the scheduler enabled:

  ```yaml
  scheduler:
    enabled: true
  ```

  Three settings accept an environment override, because they appear in the
  explicit list `pkg/config/loader.go` reads: `QUICBAAS_ENABLE_SCHEDULER`,
  `QUICBAAS_SCHEDULER_TICK_INTERVAL` and
  `QUICBAAS_SCHEDULER_MAX_CONCURRENT_JOBS`. Everything else in the `scheduler:`
  block is config-file-only.
- A **registered job handler** for whatever the job is supposed to do. The
  scheduler does not run shell commands; it invokes a handler compiled into the
  server. `engineering.md` covers writing and registering one. Creating a job
  whose handler is not registered succeeds — and then fails at every fire.
- Credentials carrying the per-verb permissions (`scheduler:create`,
  `scheduler:read`, `scheduler:trigger`, …). `api.md` maps each route to its
  exact `resource:action` pair.

## First run

```bash
# Create a recurring job.
quicbaas scheduler job create nightly-report

# See what is scheduled.
quicbaas scheduler job list

# Look at one job.
quicbaas scheduler job get <job-id>

# Run it right now without waiting for its schedule.
quicbaas scheduler job trigger <job-id>

# See what happened.
quicbaas scheduler job history <job-id>
```

Every verb after `create` takes a **job id**, not the job name. `list` is where
you get the id.

## Common tasks

### Check whether the scheduler is actually running

```bash
quicbaas scheduler status
```

In a cluster this tells you whether *this* node holds the scheduler lease. A
job that never fires while `list` shows it as active is usually a leadership
question, not a job question.

### Stop a job temporarily, then restart it

```bash
quicbaas scheduler job pause <job-id>
quicbaas scheduler job resume <job-id>
```

`pause` leaves the job in place and stops future fires. It does not stop a fire
already in flight — for that, use `cancel`.

### Stop a run that is in progress

```bash
quicbaas scheduler job cancel <job-id>
```

`cancel` targets the current execution. It is not the same as `delete`, and it
is not the same as `pause`. The three are routinely confused:

| Verb | Effect on the running execution | Effect on future fires | Job still exists |
|---|---|---|---|
| `cancel` | stopped | unchanged | yes |
| `pause` | unaffected | suppressed | yes |
| `delete` | unaffected | none | no |

### Remove a job

```bash
quicbaas scheduler job delete <job-id>
```

## Where a job runs

Two settings decide which node fires a job (`pkg/scheduler/types.go:22-110`):

- **No node affinity (the default).** The job is cluster-wide and fires only on
  the Raft leader for the scheduler's storage shard. Exactly one node runs it.
- **Node affinity set.** Only the node whose engine carries that id fires the
  job, leader or not. This is the right choice for work that is genuinely
  local to a machine — touching that host's disk, that host's device.

On a single-node deployment there is no `LeaderChecker`, and a nil
`LeaderChecker` is read as "always leader", so everything fires locally. That is
why a job that works standalone can go quiet the moment the node joins a
cluster and stops being leader.

## Ownership and tenancy

A job carries an owner type — `system`, `tenant` or `delegated` — and, for
tenant-scoped jobs, a target tenant. This is what keeps one tenant's jobs from
being listed or triggered by another. Pick the owner type at creation; it is
part of the job's identity, not a label you adjust afterwards.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Job never fires | The scheduler is disabled (`scheduler.enabled`), or this node does not hold the lease. Check `scheduler status` first. |
| Job fires and immediately errors | No handler is registered under the job's handler name. The scheduler accepts an unknown handler at create time and only discovers it at fire time. |
| Job fires twice | Known defect on leader churn: a scheduler double-fire during leadership transfer is a tracked issue in this repository, not a configuration mistake. |
| `job get` says not found for a name you can see | You passed the job **name**; every verb after `create` takes the **id** from `job list`. |
| History is empty for a job that has clearly run | History is per job id. If the job was deleted and recreated, the new id has its own, empty, history. |
| Nothing works over `--transport quic` | Nothing in this repository serves HTTP/3. See `../multiprotocol-api/architecture.md`. |

## What this feature does not do

- **It does not execute arbitrary commands.** Jobs invoke registered Go
  handlers. There is no shell, no container, and no remote exec here — that is
  the orchestrator's job (`../agent-orchestration/`).
- **It is not a workflow engine.** There is no dependency graph between jobs, no
  fan-out/fan-in, and no retry policy expressed at the schedule level. Chaining
  belongs in the handler or in an orchestration workflow.
