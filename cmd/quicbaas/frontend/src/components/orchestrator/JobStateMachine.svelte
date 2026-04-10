<!--
  JobStateMachine renders a horizontal stepper showing the 13 job states
  grouped into phases. The current state pulses; completed states have
  check icons; future states are dimmed.
-->
<script lang="ts">
  import type { JobState } from '../../stores/orchestrator.svelte.ts';

  let { state }: { state: JobState } = $props();

  interface StepDef {
    id: JobState;
    label: string;
    phase: string;
    phaseColor: string;
  }

  const steps: StepDef[] = [
    { id: 'initializing',   label: 'Init',       phase: 'Prepare',  phaseColor: 'text-info' },
    { id: 'decomposing',    label: 'Decompose',  phase: 'Prepare',  phaseColor: 'text-info' },
    { id: 'scheduling',     label: 'Schedule',   phase: 'Prepare',  phaseColor: 'text-info' },
    { id: 'assigning',      label: 'Assign',     phase: 'Execute',  phaseColor: 'text-primary' },
    { id: 'executing',      label: 'Execute',    phase: 'Execute',  phaseColor: 'text-primary' },
    { id: 'validating',     label: 'Validate',   phase: 'Verify',   phaseColor: 'text-accent' },
    { id: 'reviewing',      label: 'Review',     phase: 'Verify',   phaseColor: 'text-accent' },
    { id: 're-delegating',  label: 'Re-delegate',phase: 'Recover',  phaseColor: 'text-warning' },
    { id: 'escalating',     label: 'Escalate',   phase: 'Recover',  phaseColor: 'text-warning' },
    { id: 'completing',     label: 'Complete',   phase: 'Finish',   phaseColor: 'text-teal-400' },
    { id: 'cleanup',        label: 'Cleanup',    phase: 'Finish',   phaseColor: 'text-teal-400' },
    { id: 'completed',      label: 'Done',       phase: 'Terminal', phaseColor: 'text-success' },
    { id: 'failed',         label: 'Failed',     phase: 'Terminal', phaseColor: 'text-error' },
  ];

  const stateOrder = steps.map((s) => s.id);

  function currentIndex(): number {
    return stateOrder.indexOf(state);
  }

  function stepStatus(step: StepDef): 'active' | 'done' | 'future' {
    const ci = currentIndex();
    const si = stateOrder.indexOf(step.id);
    if (si === ci) return 'active';
    if (si < ci) return 'done';
    return 'future';
  }
</script>

<div class="overflow-x-auto">
  <ul class="steps steps-horizontal w-full text-xs">
    {#each steps as step}
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
