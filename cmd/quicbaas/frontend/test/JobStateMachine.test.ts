/**
 * JobStateMachine.test.ts
 *
 * The stepper renders the states the job's RUNNER declares — not a copy of
 * the engine's own thirteen — plus the two terminal states every job ends in.
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import type { JobRunnerInfo } from '@quicbaas/sdk';

import JobStateMachine from '../src/components/orchestrator/JobStateMachine.svelte';

const workflowRunner: JobRunnerInfo = {
  name: 'workflow',
  states: [
    { state: 'initializing', label: 'Initializing', session_backed: false },
    { state: 'decomposing', label: 'Decomposing', session_backed: false },
    { state: 'executing', label: 'Executing', session_backed: true },
    { state: 'completing', label: 'Completing', session_backed: false },
  ],
};

const sdlcRunner: JobRunnerInfo = {
  name: 'sdlc',
  states: [
    { state: 'initializing', label: 'Initializing', session_backed: false },
    { state: 'executing', label: 'Executing', session_backed: true },
    { state: 'validating', label: 'Validating', session_backed: false },
    { state: 'reviewing', label: 'Reviewing', session_backed: false },
    { state: 'cleanup', label: 'Cleanup', session_backed: false },
  ],
};

const runners = [workflowRunner, sdlcRunner];

/** stepLabels returns the rendered step labels in document order. */
function stepLabels(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll('li.step')).map((li) =>
    (li.textContent ?? '').trim(),
  );
}

describe('JobStateMachine', () => {
  it('renders the named runner’s declared states plus the terminal pair', () => {
    const { container } = render(JobStateMachine, {
      props: { state: 'executing', runner: 'sdlc', runners },
    });
    expect(stepLabels(container)).toEqual([
      'Initializing',
      'Executing',
      'Validating',
      'Reviewing',
      'Cleanup',
      'Done',
      'Failed',
    ]);
  });

  it('renders a different runner’s states for the same job state', () => {
    const { container } = render(JobStateMachine, {
      props: { state: 'executing', runner: 'workflow', runners },
    });
    expect(stepLabels(container)).toEqual([
      'Initializing',
      'Decomposing',
      'Executing',
      'Completing',
      'Done',
      'Failed',
    ]);
  });

  it('falls to the engine’s own runner when the job names none', () => {
    const { container } = render(JobStateMachine, {
      props: { state: 'initializing', runner: '', runners },
    });
    expect(stepLabels(container)).toContain('Decomposing');
  });

  it('marks the current state active, the passed states done and the rest future', () => {
    const { container } = render(JobStateMachine, {
      props: { state: 'validating', runner: 'sdlc', runners },
    });
    const steps = Array.from(container.querySelectorAll('li.step'));
    const byLabel = (label: string) =>
      steps.find((li) => (li.textContent ?? '').trim() === label) as HTMLElement;

    expect(byLabel('Validating').className).toContain('step-primary');
    expect(byLabel('Initializing').className).toContain('step-success');
    expect(byLabel('Reviewing').className).not.toContain('step-primary');
    expect(byLabel('Reviewing').querySelector('span')?.className).toContain('opacity-30');
  });

  it('marks a failed job with the error step rather than the success one', () => {
    const { container } = render(JobStateMachine, {
      props: { state: 'failed', runner: 'sdlc', runners },
    });
    const failed = Array.from(container.querySelectorAll('li.step')).find(
      (li) => (li.textContent ?? '').trim() === 'Failed',
    ) as HTMLElement;
    expect(failed.className).toContain('step-error');
  });

  it('names a state the runner declared but this UI has never heard of', () => {
    const custom: JobRunnerInfo = {
      name: 'sdlc',
      states: [{ state: 'publishing', label: 'Publishing', session_backed: false }],
    };
    const { container } = render(JobStateMachine, {
      props: { state: 'publishing', runner: 'sdlc', runners: [custom] },
    });
    expect(stepLabels(container)).toEqual(['Publishing', 'Done', 'Failed']);
  });

  it('says so, rather than inventing states, when the runner is not registered here', () => {
    const { container } = render(JobStateMachine, {
      props: { state: 'executing', runner: 'sdlc', runners: [workflowRunner] },
    });
    expect(stepLabels(container)).toEqual(['Done', 'Failed']);
    expect(screen.getByText(/no runner named "sdlc" is registered/i)).toBeInTheDocument();
  });

  it('falls back to the state id when a runner declares a state with no label', () => {
    const unlabelled: JobRunnerInfo = {
      name: 'workflow',
      states: [{ state: 'initializing', label: '', session_backed: false }],
    };
    const { container } = render(JobStateMachine, {
      props: { state: 'initializing', runner: '', runners: [unlabelled] },
    });
    expect(stepLabels(container)).toEqual(['initializing', 'Done', 'Failed']);
  });
});
