<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  SchedulerView lists all registered cron/one-time jobs and allows creating,
  deleting, and immediately triggering them.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type { SchedulerJob, CreateJobRequest } from '@quicbaas/sdk';
  import { CalendarClock, Plus, RefreshCw, Trash2, Play, Pause, Square, Pencil, X } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';

  // ---------------------------------------------------------------------------
  // Job list state
  // ---------------------------------------------------------------------------

  let jobs = $state<SchedulerJob[]>([]);
  let loading = $state(false);
  let loadError = $state('');

  async function fetchJobs(): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.scheduler.listJobs({ limit: 100 });
      const raw = resp.jobs ?? [];
      const seen = new Set<string>();
      jobs = raw.filter((j) => {
        if (seen.has(j.id)) return false;
        seen.add(j.id);
        return true;
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to load jobs: ${msg}`;
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchJobs(); });

  // ---------------------------------------------------------------------------
  // Create job dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createName = $state('');
  let createSchedule = $state('');
  let createHandler = $state('');
  let createPayload = $state('');
  let createType = $state('cron');
  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    showCreateDialog = true;
    createName = '';
    createSchedule = '';
    createHandler = '';
    createPayload = '';
    createType = 'cron';
    createError = '';
  }

  function closeCreateDialog(): void {
    showCreateDialog = false;
  }

  async function handleCreate(): Promise<void> {
    if (!createName.trim()) {
      createError = 'Job name is required.';
      return;
    }
    if (!createHandler.trim()) {
      createError = 'Handler is required.';
      return;
    }
    if (createType === 'cron' && !createSchedule.trim()) {
      createError = 'Cron schedule is required for cron jobs.';
      return;
    }

    let parsedPayload: unknown = undefined;
    if (createPayload.trim()) {
      try {
        parsedPayload = JSON.parse(createPayload.trim()) as unknown;
      } catch {
        createError = 'Payload must be valid JSON.';
        return;
      }
    }

    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      await client.scheduler.createJob({
        name: createName.trim(),
        type: createType,
        schedule: createSchedule.trim() || undefined,
        handler: createHandler.trim(),
        payload: parsedPayload,
      });
      showToast({ type: 'success', message: `Job "${createName}" created.` });
      closeCreateDialog();
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      createError = `Failed to create job: ${msg}`;
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete job (with confirm modal)
  // ---------------------------------------------------------------------------

  let jobToDelete: SchedulerJob | null = $state(null);
  let deletingJob = $state(false);

  async function confirmDeleteJob(): Promise<void> {
    if (!jobToDelete) return;
    deletingJob = true;
    try {
      const client = await getSDKClient();
      await client.scheduler.deleteJob(jobToDelete.id);
      showToast({ type: 'success', message: `Job "${jobToDelete.name}" deleted.` });
      jobToDelete = null;
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Delete failed: ${msg}` });
    } finally {
      deletingJob = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Trigger job (Run Now)
  // ---------------------------------------------------------------------------

  let triggeringJob: number | null = $state(null);

  async function handleTrigger(job: SchedulerJob): Promise<void> {
    triggeringJob = job.id;
    try {
      const client = await getSDKClient();
      await client.scheduler.triggerJob(job.id);
      showToast({ type: 'success', message: `Job "${job.name}" triggered.` });
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Trigger failed: ${msg}` });
    } finally {
      triggeringJob = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Pause / Resume job
  // ---------------------------------------------------------------------------

  let togglingJob: number | null = $state(null);

  async function handlePauseResume(job: SchedulerJob): Promise<void> {
    togglingJob = job.id;
    try {
      const client = await getSDKClient();
      if (job.status === 'paused') {
        await client.scheduler.resumeJob(job.id);
        showToast({ type: 'success', message: `Job "${job.name}" resumed.` });
      } else {
        await client.scheduler.pauseJob(job.id);
        showToast({ type: 'success', message: `Job "${job.name}" paused.` });
      }
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Failed: ${msg}` });
    } finally {
      togglingJob = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit job dialog
  // ---------------------------------------------------------------------------

  let jobToEdit: SchedulerJob | null = $state(null);
  let editName = $state('');
  let editSchedule = $state('');
  let editHandler = $state('');
  let editPayload = $state('');
  let editError = $state('');
  let editingJob = $state(false);

  function openEditJob(job: SchedulerJob): void {
    jobToEdit = job;
    editName = job.name;
    editSchedule = job.schedule ?? '';
    editHandler = job.handler;
    editPayload = job.payload ? JSON.stringify(job.payload, null, 2) : '';
    editError = '';
  }

  async function submitEditJob(): Promise<void> {
    if (!jobToEdit) return;
    editError = '';
    let parsedPayload: unknown = undefined;
    if (editPayload.trim()) {
      try {
        parsedPayload = JSON.parse(editPayload.trim()) as unknown;
      } catch {
        editError = 'Payload must be valid JSON.';
        return;
      }
    }
    editingJob = true;
    try {
      const client = await getSDKClient();
      const req: Partial<CreateJobRequest> = {
        name: editName.trim() || undefined,
        schedule: editSchedule.trim() || undefined,
        handler: editHandler.trim() || undefined,
        payload: parsedPayload,
      };
      await client.scheduler.updateJob(jobToEdit.id, req);
      showToast({ type: 'success', message: `Job "${editName || jobToEdit.name}" updated.` });
      jobToEdit = null;
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      editError = `Update failed: ${msg}`;
    } finally {
      editingJob = false;
    }
  }

  function formatNextRun(nextRun?: string): string {
    if (!nextRun) return '—';
    const d = new Date(nextRun);
    if (isNaN(d.getTime())) return '—';
    return d.toLocaleString();
  }

  function statusVariant(status: string): string {
    if (status === 'active' || status === 'running') return 'badge-success';
    if (status === 'paused') return 'badge-warning';
    if (status === 'failed' || status === 'cancelled') return 'badge-error';
    return 'badge-ghost';
  }
</script>

<div class="flex h-full flex-col">
  <div class="p-6 pb-0">
    <PageHeader title="Scheduler" subtitle="Cron and one-time scheduled jobs" icon={CalendarClock}>
      {#snippet children()}
        <button
          class="btn btn-ghost btn-sm gap-2"
          onclick={fetchJobs}
          disabled={loading}
          aria-label="Refresh jobs"
        >
          <RefreshCw class={`h-4 w-4${loading ? ' animate-spin' : ''}`} />
          Refresh
        </button>
        <button
          class="btn btn-primary btn-sm gap-2"
          onclick={openCreateDialog}
          aria-label="Create Job"
        >
          <Plus class="h-4 w-4" />
          Create Job
        </button>
      {/snippet}
    </PageHeader>
  </div>

  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-6">

    <!-- Job Table -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body">

        {#if loadError}
          <div class="alert alert-error text-sm flex items-center justify-between">
            <span>{loadError}</span>
            <button class="btn btn-xs btn-ghost" onclick={() => { void fetchJobs(); }}>Retry</button>
          </div>
        {:else if loading}
          <div class="flex justify-center py-10">
            <span class="loading loading-spinner loading-md"></span>
          </div>
        {:else if jobs.length === 0}
          <EmptyState
            icon={CalendarClock}
            title="No scheduled jobs — Create Job is how you start."
            description="Schedule recurring cron jobs or one-time delayed tasks to automate your workflows."
            actionLabel="Create Job"
            onaction={openCreateDialog}
          />
        {:else}
          <div class="overflow-x-auto">
            <table class="table table-sm">
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Type</th>
                  <th>Schedule</th>
                  <th>Handler</th>
                  <th>Status</th>
                  <th>Next Run</th>
                  <th>Runs</th>
                  <th class="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each jobs as job (job.id)}
                  <tr>
                    <td class="font-semibold">{job.name}</td>
                    <td>
                      <span class="badge badge-outline badge-xs">{job.type}</span>
                    </td>
                    <td class="font-mono text-xs">{job.schedule ?? '—'}</td>
                    <td class="font-mono text-xs">{job.handler}</td>
                    <td>
                      <span class="badge badge-sm {statusVariant(job.status)}">{job.status}</span>
                    </td>
                    <td class="text-xs text-base-content/60">{formatNextRun(job.next_run)}</td>
                    <td class="text-xs">
                      {job.run_count}
                      {#if job.fail_count > 0}
                        <span class="text-error ml-1">({job.fail_count} failed)</span>
                      {/if}
                    </td>
                    <td class="text-right">
                      <div class="flex justify-end gap-1">
                        <button
                          class="btn btn-ghost btn-xs"
                          disabled={triggeringJob === job.id}
                          onclick={() => void handleTrigger(job)}
                          title={`Run job ${job.name} now`}
                          aria-label={`Run job ${job.name} now`}
                        >
                          <Play class="h-3 w-3" />
                        </button>
                        <button
                          class="btn btn-ghost btn-xs"
                          disabled={togglingJob === job.id}
                          onclick={() => void handlePauseResume(job)}
                          title={job.status === 'paused' ? `Resume job ${job.name}` : `Pause job ${job.name}`}
                          aria-label={job.status === 'paused' ? `Resume job ${job.name}` : `Pause job ${job.name}`}
                        >
                          {#if job.status === 'paused'}
                            <Square class="h-3 w-3" />
                          {:else}
                            <Pause class="h-3 w-3" />
                          {/if}
                        </button>
                        <button
                          class="btn btn-ghost btn-xs"
                          onclick={() => openEditJob(job)}
                          title={`Edit job ${job.name}`}
                          aria-label={`Edit job ${job.name}`}
                        >
                          <Pencil class="h-3 w-3" />
                        </button>
                        <button
                          class="btn btn-ghost btn-xs text-error"
                          onclick={() => { jobToDelete = job; }}
                          title={`Delete job ${job.name}`}
                          aria-label={`Delete job ${job.name}`}
                        >
                          <Trash2 class="h-3 w-3" />
                        </button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </section>

  </div>
</div>

<!-- Delete Job Confirm Dialog -->
{#if jobToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete job confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Job?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{jobToDelete.name}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { jobToDelete = null; }}>Cancel</button>
        <button class="btn btn-error gap-2" onclick={confirmDeleteJob} disabled={deletingJob}>
          {#if deletingJob}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <div class="modal-backdrop" onclick={() => { jobToDelete = null; }}></div>
  </div>
{/if}

<!-- Edit Job Dialog -->
{#if jobToEdit}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit Job">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Edit Job</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={() => { jobToEdit = null; }} aria-label="Close"><X class="h-4 w-4" /></button>
      </div>
      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Job Name</span></div>
          <input class="input input-bordered w-full" type="text" bind:value={editName} />
        </label>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Cron Schedule</span></div>
          <input class="input input-bordered w-full font-mono" type="text" bind:value={editSchedule} placeholder="0 0 * * *" />
        </label>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Handler</span></div>
          <input class="input input-bordered w-full font-mono" type="text" bind:value={editHandler} />
        </label>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Payload (JSON, optional)</span></div>
          <textarea class="textarea textarea-bordered w-full font-mono text-xs" rows="4" bind:value={editPayload}></textarea>
        </label>
        {#if editError}
          <div class="alert alert-error text-sm">{editError}</div>
        {/if}
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { jobToEdit = null; }}>Cancel</button>
        <button class="btn btn-primary gap-2" onclick={submitEditJob} disabled={editingJob}>
          {#if editingJob}<span class="loading loading-spinner loading-xs"></span>{/if}
          Save
        </button>
      </div>
    </div>
    <div class="modal-backdrop" onclick={() => { jobToEdit = null; }}></div>
  </div>
{/if}

<!-- Create Job Dialog -->
{#if showCreateDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create Job">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Create Scheduled Job</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={closeCreateDialog} aria-label="Close">
          <X class="h-4 w-4" />
        </button>
      </div>

      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Job Name</span></div>
          <input
            class="input input-bordered w-full"
            type="text"
            placeholder="e.g. nightly-cleanup"
            bind:value={createName}
          />
        </label>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Job Type</span></div>
          <select class="select select-bordered w-full" bind:value={createType}>
            <option value="cron">Cron (recurring)</option>
            <option value="one_time">One-time</option>
            <option value="delayed">Delayed</option>
          </select>
        </label>

        {#if createType === 'cron'}
          <label class="form-control w-full">
            <div class="label">
              <span class="label-text">Cron Expression</span>
              <span class="label-text-alt text-base-content/40">e.g. 0 0 * * *</span>
            </div>
            <input
              class="input input-bordered w-full font-mono"
              type="text"
              placeholder="0 0 * * *"
              bind:value={createSchedule}
            />
          </label>
        {/if}

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Handler</span></div>
          <input
            class="input input-bordered w-full font-mono"
            type="text"
            placeholder="e.g. cleanup.run"
            bind:value={createHandler}
          />
        </label>

        <label class="form-control w-full">
          <div class="label">
            <span class="label-text">Action Payload (JSON, optional)</span>
          </div>
          <textarea
            class="textarea textarea-bordered w-full font-mono text-xs"
            rows="4"
            placeholder='&#123;"target": "logs", "older_than_days": 30&#125;'
            bind:value={createPayload}
          ></textarea>
        </label>

        {#if createError}
          <div class="alert alert-error text-sm">{createError}</div>
        {/if}
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={closeCreateDialog}>Cancel</button>
        <button
          class="btn btn-primary gap-2"
          onclick={handleCreate}
          disabled={creating}
          aria-label="Create"
        >
          {#if creating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Create
        </button>
      </div>
    </div>
    <div class="modal-backdrop" onclick={closeCreateDialog}></div>
  </div>
{/if}
