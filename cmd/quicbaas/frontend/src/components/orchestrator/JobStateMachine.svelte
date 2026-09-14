<!--
  JobStateMachine renders a horizontal stepper of the states the job's RUNNER
  declares, plus the two terminal states every job ends in.

  The engine has more than one shape of job — a lead-driven workflow, an
  issue-driven delivery — and each runner owns its own state sequence. The
  steps therefore come from the runner named by the job's `runner` field
  (absent means the engine's own "workflow" runner), read from
  GET /api/v1/orchestrator/runners, rather than from a copy of the engine's
  states kept here.

  The current state pulses; passed states are marked done; future states are
  dimmed.
-->
<script lang="ts">
  import type { JobRunnerInfo } from '@quicbaas/sdk';
  import type { JobState } from '../../stores/orchestrator.svelte.ts';

  let {
    state,
    runner = '',
    runners = [],
  }: { state: JobState; runner?: string; runners?: JobRunnerInfo[] } = $props();

  interface StepDef {
    id: string;
    label: string;
  }

  /** DEFAULT_RUNNER is the runner a job that names none was driven by. */
  const DEFAULT_RUNNER = 'workflow';

  /** TERMINAL_STEPS end every job, whatever runner drove it. */
  const TERMINAL_STEPS: StepDef[] = [
    { id: 'completed', label: 'Done' },
    { id: 'failed', label: 'Failed' },
  ];

  const steps = $derived.by((): StepDef[] => {
    const name = runner === '' ? DEFAULT_RUNNER : runner;
    const declared = runners.find((r) => r.name === name);
    const declaredSteps = (declared?.states ?? []).map((spec) => ({
      id: spec.state,
      label: spec.label || spec.state,
    }));
    return [...declaredSteps, ...TERMINAL_STEPS];
  });

  const stateOrder = $derived(steps.map((s) => s.id));

  function stepStatus(step: StepDef): 'active' | 'done' | 'future' {
    const ci = stateOrder.indexOf(state);
    const si = stateOrder.indexOf(step.id);
    if (si === ci) return 'active';
    if (si < ci) return 'done';
    return 'future';
  }
</script>

<div class="overflow-x-auto">
  {#if steps.length === TERMINAL_STEPS.length}
    <p class="text-xs text-base-content/40">
      No runner named "{runner === '' ? DEFAULT_RUNNER : runner}" is registered on this node, so its
      state sequence is unknown.
    </p>
  {/if}
  <ul class="steps steps-horizontal w-full text-xs">
    {#each steps as step (step.id)}
      {@const status = stepStatus(step)}
      <li
        class="step"
        class:step-success={status === 'done' && step.id !== 'failed'}
        class:step-error={step.id === 'failed' && (status === 'done' || status === 'active')}
        class:step-primary={status === 'active' && step.id !== 'failed'}
      >
        <span
          class="text-xs"
          class:opacity-30={status === 'future'}
          class:font-bold={status === 'active'}
          class:animate-pulse={status === 'active'}
          class:text-success={status === 'done' && step.id !== 'failed'}
          class:text-error={step.id === 'failed' && status !== 'future'}
        >
          {step.label}
        </span>
      </li>
    {/each}
  </ul>
</div>
