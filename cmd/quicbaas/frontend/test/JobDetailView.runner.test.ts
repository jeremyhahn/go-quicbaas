/**
 * JobDetailView.runner.test.ts
 *
 * The job page is built BY RUNNER (go-quicbaas#796).
 *
 * An sdlc job renders its attempt, the agent that ran it, the stage timeline,
 * the delivery and its review rounds, the verification output, the typed
 * terminal reason and the worker's last reply — and none of the workflow
 * runner's iteration gauge, task percentage or DAG, which it has no data for.
 * A workflow job still renders all of those.
 *
 * The fixtures are the shapes the server sends: pkg/orchestration's
 * JobStatus with its provenance fields, one per terminal state an sdlc job
 * can reach.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/svelte';

const { storeMock } = vi.hoisted(() => ({
  storeMock: {
    selectedJob: null as unknown,
    events: [] as unknown[],
    activeTenantID: 'acme',
    jobs: [] as unknown[],
    tasks: [] as unknown[],
  },
}));

vi.mock('../src/stores/orchestrator.svelte.ts', () => ({
  orchestratorStore: storeMock,
  selectJob: vi.fn(),
  isTerminalJobState: (state: string) => state === 'completed' || state === 'failed',
}));

vi.mock('../src/stores/router.svelte.ts', () => ({
  routeAgentDetail: vi.fn(),
  routeJobs: vi.fn(),
  routeSessionDetail: vi.fn(),
  routeTaskDetail: vi.fn(),
  routerStore: { route: { name: 'job-detail', params: { id: 'job-3' }, hash: '#/jobs/job-3' } },
}));

vi.mock('../src/stores/job-events.svelte.ts', () => ({
  startEventStream: vi.fn(),
  stopEventStream: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({ showToast: vi.fn() }));

const { readmitTaskMock } = vi.hoisted(() => ({ readmitTaskMock: vi.fn() }));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      cancelJob: vi.fn().mockResolvedValue(undefined),
      readmitTask: readmitTaskMock,
      listRunners: vi.fn().mockResolvedValue([]),
    },
  }),
}));

vi.mock('../src/components/orchestrator/JobStateMachine.svelte', () => ({ default: vi.fn() }));
vi.mock('../src/components/orchestrator/DAGGraph.svelte', () => ({ default: vi.fn() }));
vi.mock('../src/components/orchestrator/EventLog.svelte', () => ({ default: vi.fn() }));
vi.mock('../src/components/orchestrator/BudgetGauge.svelte', () => ({ default: vi.fn() }));

import JobDetailView from '../src/views/orchestrator/JobDetailView.svelte';
import { routeAgentDetail } from '../src/stores/router.svelte.ts';

/** MS is one millisecond in the server's Unix nanoseconds. */
const MS = 1_000_000;

/**
 * sdlcJob is one attempt at one issue, as GET
 * /api/v1/orchestrator/jobs/{id} returns it.
 */
function sdlcJob(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    job_id: 'job-3',
    tenant_id: 'acme',
    state: 'failed',
    iteration: 0,
    task_count: 0,
    completed: 0,
    failed: 0,
    description: 'go-truststrap#26: harden the bootstrap',
    started_at: 1_000 * MS,
    updated_at: 10_000 * MS,
    runner: 'sdlc',
    task_id: 'task-26',
    attempt: 3,
    node: 'quicbaasd-node-1',
    agent_id: 'agent-worker-04',
    lead_session_id: 'sess-worker-0001',
    session_ids: ['sess-worker-0001'],
    events: [
      { timestamp: 2_000 * MS, type: 'state_change', details: 'executing' },
      {
        timestamp: 8_000 * MS,
        type: 'sdlc:agent_turn',
        // Verbatim pkg/sdlc AgentTurn.Describe output (#787): a header of
        // counts, a blank line, then the worker's own words.
        details:
          'attempt=3 round=1 elapsed=42.5s reply_bytes=812 tool_uses=4 stop_reason=end_turn ' +
          'tokens_in=1843 tokens_out=204\n\nthe branch produced no changes',
      },
    ],
    stages: [
      { stage: 'prepare', entered_at: 1_000 * MS },
      { stage: 'reviewable', entered_at: 4_000 * MS },
      { stage: 'discard', entered_at: 9_500 * MS },
    ],
    attempts: [
      { attempt: 1, job_id: 'job-1' },
      { attempt: 2, job_id: 'job-2' },
      { attempt: 3, job_id: 'job-3', current: true },
    ],
    attempt_budget: 3,
    terminal_reason: 'budget_exhausted',
    readmit_allowed: false,
    delivery: {
      branch: 'issue/26-harden',
      head_sha: 'abcdef0123456789',
      pull_request_number: 57,
      pull_request_url: 'http://forge.example/jeremyhahn/go-truststrap/pulls/57',
      pull_request_state: 'merged',
      reviews: [
        { round: 1, reviewer: 'codex', verdict: 'request_changes', head_sha: 'abc', findings: 'needs a test' },
        { round: 2, reviewer: 'codex', verdict: 'approve', head_sha: 'def' },
      ],
    },
    verification: [
      { name: 'make build', passed: true, message: 'ok' },
      { name: 'make test', passed: false, message: 'FAIL pkg/foo' },
    ],
    ...over,
  };
}

/** workflowJob is the engine's own runner: a lead decomposing a description. */
function workflowJob(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    job_id: 'job-wf',
    tenant_id: 'acme',
    state: 'executing',
    iteration: 3,
    task_count: 10,
    completed: 5,
    failed: 0,
    description: 'Refactor the storage layer',
    started_at: 1_000 * MS,
    updated_at: 5_000 * MS,
    runner: 'workflow',
    workflow_name: 'delivery',
    config: { tenant_id: 'acme', description: 'x', lead_backend: 'claude', lead_model: 'opus', max_iterations: 10 },
    events: [{ timestamp: 2_000 * MS, type: 'state_change', details: 'executing' }],
    work_items: [],
    ...over,
  };
}

describe('JobDetailView — an sdlc job', () => {
  beforeEach(() => {
    storeMock.selectedJob = sdlcJob();
    storeMock.jobs = [];
    storeMock.tasks = [];
    readmitTaskMock.mockClear();
    vi.mocked(routeAgentDetail).mockClear();
  });

  it('names the attempt and its budget instead of an iteration gauge', () => {
    const { container } = render(JobDetailView);
    const attemptCard = container.querySelector('[data-testid="summary-attempt"]');
    expect(attemptCard).not.toBeNull();
    expect(attemptCard!.textContent).toContain('3 of 3');
    expect(attemptCard!.textContent).toContain('Budget: 3');
    expect(container.querySelector('[data-testid="iteration-progress"]')).toBeNull();
    expect(container.querySelector('progress[aria-label="Task completion progress"]')).toBeNull();
  });

  it('names the agent that ran it and links it into the fleet', async () => {
    render(JobDetailView);
    const link = screen.getByRole('link', { name: /agent-worker-04/ });
    expect(link.getAttribute('href')).toBe('#/agents/agent-worker-04');
    link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await Promise.resolve();
    expect(vi.mocked(routeAgentDetail)).toHaveBeenCalledWith('agent-worker-04');
  });

  it('falls back to the daemon when the work ran in-process', () => {
    storeMock.selectedJob = sdlcJob({ agent_id: undefined });
    const { container } = render(JobDetailView);
    const agentCard = container.querySelector('[data-testid="summary-agent"]');
    expect(agentCard!.textContent).toContain('quicbaasd-node-1');
    expect(screen.queryByRole('link', { name: /agent-worker-04/ })).toBeNull();
  });

  it('draws the stage timeline with a duration for every moment', () => {
    render(JobDetailView);
    const rows = screen.getByLabelText('Stage timeline').querySelectorAll('li');
    expect(rows.length).toBe(3);
    expect(rows[0].textContent).toContain('prepare');
    expect(rows[0].textContent).toContain('3.0s');
    expect(rows[2].textContent).toContain('discard');
  });

  it('links the pull request and lists every review round', () => {
    const { container } = render(JobDetailView);
    const panel = within(container.querySelector('[data-testid="delivery"]') as HTMLElement);
    const pr = panel.getByRole('link', { name: /#57/ });
    expect(pr.getAttribute('href')).toBe('http://forge.example/jeremyhahn/go-truststrap/pulls/57');
    expect(pr.textContent).toContain('merged');

    const rounds = screen.getByLabelText('Review rounds');
    expect(rounds.textContent).toContain('Round 1');
    expect(rounds.textContent).toContain('request_changes');
    expect(rounds.textContent).toContain('needs a test');
    expect(rounds.textContent).toContain('approve');
  });

  it('shows the branch and head even when nothing opened a pull request', () => {
    // The direct-push carve-out: a change class the strategy exempted from
    // review lands on the target with no pull request to open.
    storeMock.selectedJob = sdlcJob({
      delivery: { branch: 'hotfix/x', head_sha: 'deadbeefcafe1234' },
    });
    const { container } = render(JobDetailView);
    const delivery = container.querySelector('[data-testid="delivery"]');
    expect(delivery!.textContent).toContain('hotfix/x');
    expect(delivery!.textContent).toContain('deadbeefcafe');
    expect(screen.queryByLabelText('Review rounds')).toBeNull();
  });

  it('names a pull request the runner recorded without a number', () => {
    // The server accepts a pull_request artifact whose metadata carries no
    // number and leaves PullRequestNumber zero (pkg/orchestration's
    // applyPullRequestArtifact). "#0" is a pull request nobody can look up,
    // so both places that render the link say the word instead.
    storeMock.selectedJob = sdlcJob({
      delivery: {
        branch: 'issue/26-harden',
        head_sha: 'abcdef0123456789',
        pull_request_url: 'http://forge.example/jeremyhahn/go-truststrap/pulls/57',
        pull_request_state: 'open',
      },
    });
    const { container } = render(JobDetailView);

    const panel = container.querySelector('[data-testid="delivery"]') as HTMLElement;
    expect(panel.textContent).not.toContain('#0');
    expect(within(panel).getByRole('link', { name: /Pull request/ }).getAttribute('href')).toBe(
      'http://forge.example/jeremyhahn/go-truststrap/pulls/57',
    );

    const row = container.querySelector('[data-testid="summary-pull-request"]') as HTMLElement;
    expect(row.textContent).not.toContain('#0');
    expect(within(row).getByRole('link', { name: /Pull request/ })).toBeInTheDocument();
  });

  it('says the attempt published nothing rather than showing a blank delivery', () => {
    storeMock.selectedJob = sdlcJob({ delivery: null });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="delivery"]')!.textContent).toContain(
      'published nothing yet',
    );
  });

  it('shows what each verification command said', () => {
    render(JobDetailView);
    const results = screen.getByLabelText('Verification results');
    expect(results.textContent).toContain('make build');
    expect(results.textContent).toContain('make test');
    expect(results.textContent).toContain('FAIL pkg/foo');
  });

  it("shows the worker's own words, and the daemon's counts apart from them", () => {
    const { container } = render(JobDetailView);
    const panel = container.querySelector('[data-testid="worker-reply"]')!;
    // The reply is the worker's.
    expect(panel.querySelector('pre')!.textContent).toBe('the branch produced no changes');
    // The header of counts is the daemon's measurement and is rendered as
    // metrics, never inside the box labelled as the worker's reply.
    const metrics = panel.querySelector('[aria-label="Turn metrics"]')!;
    expect(metrics.textContent).toContain('attempt=3');
    expect(metrics.textContent).toContain('tokens_in=1843');
    expect(panel.querySelector('pre')!.textContent).not.toContain('tokens_in');
  });

  it('says a silent turn was silent rather than showing an empty box', () => {
    // Three attempts at go-truststrap#26 died of exactly this and the page
    // said nothing at all.
    storeMock.selectedJob = sdlcJob({
      events: [
        {
          timestamp: 8_000 * MS,
          type: 'sdlc:agent_turn',
          details: 'attempt=1 round=1 reply_bytes=0 tool_uses=0\n\n(the worker answered with no text)',
        },
      ],
    });
    const { container } = render(JobDetailView);
    const panel = container.querySelector('[data-testid="worker-reply"]')!;
    expect(panel.textContent).toContain('answered with no text');
    expect(panel.querySelector('pre')).toBeNull();
  });

  it('shows no reply panel for a job whose daemon records no agent turns', () => {
    storeMock.selectedJob = sdlcJob({
      events: [{ timestamp: 2_000 * MS, type: 'state_change', details: 'executing' }],
    });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="worker-reply"]')).toBeNull();
  });

  it('offers no DAG tab, which belongs to the workflow runner', () => {
    render(JobDetailView);
    expect(screen.queryByRole('tab', { name: /^Dag$/i })).toBeNull();
    expect(screen.getByRole('tab', { name: /^Overview$/i })).toBeInTheDocument();
  });

  it('KEEPS its cost tab, because spend is not the workflow runner\'s', () => {
    // go-quicbaas#796 hid this tab for the sdlc runner, and it was right to:
    // the tab then held nothing but the workflow runner's iteration gauge and
    // a job-memory token panel no production writer populates. Once it reads
    // the consumption ledger (go-quicbaas#794) the reason evaporates — every
    // runner burns provider tokens — and hiding it left an sdlc job's real
    // spend unreachable on its own page. The GAUGE stays runner-gated; see
    // JobDetailView.cost.test.ts.
    render(JobDetailView);
    expect(screen.getByRole('tab', { name: /^Cost$/i })).toBeInTheDocument();
  });
});

describe('JobDetailView — the terminal reason and what it allows', () => {
  beforeEach(() => {
    storeMock.jobs = [];
    storeMock.tasks = [];
    readmitTaskMock.mockClear();
  });

  it('names a spent budget and refuses the readmit, because failed is terminal', () => {
    storeMock.selectedJob = sdlcJob({ terminal_reason: 'budget_exhausted', readmit_allowed: false });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')!.textContent).toContain(
      'Budget exhausted',
    );
    const button = screen.getByRole('button', { name: /readmit task/i }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
  });

  it('names an abandoned attempt and offers the readmit, because it only parked', () => {
    storeMock.selectedJob = sdlcJob({ terminal_reason: 'abandoned', readmit_allowed: true });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')!.textContent).toContain(
      'Abandoned',
    );
    const button = screen.getByRole('button', { name: /readmit task/i }) as HTMLButtonElement;
    expect(button.disabled).toBe(false);
  });

  it('readmits the TASK, not the job, when the action is taken', async () => {
    readmitTaskMock.mockResolvedValue(undefined);
    storeMock.selectedJob = sdlcJob({ terminal_reason: 'abandoned', readmit_allowed: true });
    render(JobDetailView);
    const button = screen.getByRole('button', { name: /readmit task/i });
    button.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await vi.waitFor(() => expect(readmitTaskMock).toHaveBeenCalledWith('acme', 'task-26'));
  });

  it('names a completed run', () => {
    storeMock.selectedJob = sdlcJob({
      state: 'completed',
      terminal_reason: 'completed',
      readmit_allowed: false,
    });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')!.textContent).toContain(
      'Completed',
    );
  });

  it('names an environment fault that retrying will not clear, and offers the readmit', () => {
    storeMock.selectedJob = sdlcJob({ terminal_reason: 'fault_held', readmit_allowed: true });
    const { container } = render(JobDetailView);
    const panel = container.querySelector('[data-testid="terminal-reason"]')!;
    expect(panel.textContent).toContain('Environment fault held');
    expect(panel.textContent).toMatch(/repair the condition/i);
  });

  it('names an escalation the engine made', () => {
    storeMock.selectedJob = sdlcJob({ terminal_reason: 'parked_escalated', readmit_allowed: true });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')!.textContent).toContain(
      'Escalated',
    );
  });

  it('shows no outcome panel while the task is still running', () => {
    storeMock.selectedJob = sdlcJob({
      state: 'executing',
      terminal_reason: undefined,
      readmit_allowed: false,
    });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')).toBeNull();
    expect(screen.queryByRole('button', { name: /readmit task/i })).toBeNull();
  });
});

describe('JobDetailView — the record the page is given', () => {
  // go-quicbaas#796 second review finding. ListJobs returns the RAW persisted
  // statuses; only GetJobStatus lays the task's provenance over them. A page
  // populated from the list therefore renders every panel this change adds
  // empty — the whole feature missing, on the most common path into it.
  beforeEach(() => {
    storeMock.jobs = [];
    storeMock.tasks = [];
  });

  /** listRecord is what ListJobs returns: no provenance whatsoever. */
  function listRecord(): Record<string, unknown> {
    const job = sdlcJob();
    for (const key of [
      'attempts',
      'attempt_budget',
      'terminal_reason',
      'readmit_allowed',
      'delivery',
      'verification',
    ]) {
      delete job[key];
    }
    return job;
  }

  it('renders none of the provenance panels from an unenriched list record', () => {
    storeMock.selectedJob = listRecord();
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')).toBeNull();
    expect(container.querySelector('[data-testid="verification"]')).toBeNull();
    expect(container.querySelector('[data-testid="delivery"]')!.textContent).toContain(
      'published nothing yet',
    );
    expect(screen.queryByLabelText('Review rounds')).toBeNull();
    // Readmit appears for a failed job but is REFUSED, because the record
    // carries no readmit_allowed to permit it — so the one action an
    // operator has on a spent task is dead on the list path.
    const readmit = screen.getByRole('button', { name: /readmit task/i }) as HTMLButtonElement;
    expect(readmit.disabled).toBe(true);
    // The attempt ordinal still shows: it is on the raw record.
    expect(container.querySelector('[data-testid="summary-attempt"]')!.textContent).toContain('3');
  });

  it('renders every provenance panel once the enriched record arrives', () => {
    // This is what loadSelectedJob puts in the store: the same job, read
    // through GetJobStatus.
    storeMock.selectedJob = sdlcJob();
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="terminal-reason"]')!.textContent).toContain(
      'Budget exhausted',
    );
    expect(container.querySelector('[data-testid="verification"]')).not.toBeNull();
    expect(screen.getByLabelText('Review rounds')).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: /#57/ }).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: /readmit task/i })).toBeInTheDocument();
    expect(container.querySelector('[data-testid="summary-attempt"]')!.textContent).toContain(
      '3 of 3',
    );
  });
});

describe('JobDetailView — the pull request belongs to THIS attempt', () => {
  // go-quicbaas#796 third review finding. A retried task accumulates one
  // pull request per attempt under attempt-scoped artifact names, and
  // `pullRequestArtifacts` returns all of them on purpose — that is the task
  // page's list. The job page is per-attempt: reading the task's list there
  // showed attempt 3's page attempt 1's pull request and offered to open it,
  // which sends the operator to the wrong review.

  /** retriedTask is task-26 after three attempts, two of which opened a PR. */
  const retriedTask = {
    id: 'task-26',
    tenant_id: 'acme',
    subject: 'harden the bootstrap',
    status: 'failed',
    spawned_job_id: 'job-3',
    source: {
      kind: 'forge_issue',
      ref: 'jeremyhahn/go-truststrap#26',
      url: 'http://forge.example/jeremyhahn/go-truststrap/issues/26',
    },
    output_artifacts: [
      {
        kind: 'url',
        name: 'pull_request@a1',
        ref: { path: 'http://forge.example/jeremyhahn/go-truststrap/pulls/41' },
        metadata: { number: '41', state: 'closed' },
      },
      {
        kind: 'url',
        name: 'pull_request@a3',
        ref: { path: 'http://forge.example/jeremyhahn/go-truststrap/pulls/57' },
        metadata: { number: '57', state: 'merged' },
      },
    ],
  };

  beforeEach(() => {
    storeMock.jobs = [];
    storeMock.tasks = [retriedTask];
  });

  it("shows attempt 3's pull request and never the earlier attempt's", () => {
    // The fixture job is attempt 3; the server scoped its delivery to #57.
    storeMock.selectedJob = sdlcJob();
    const { container } = render(JobDetailView);

    const row = container.querySelector('[data-testid="summary-pull-request"]') as HTMLElement;
    const link = within(row).getByRole('link', { name: /#57/ });
    expect(link.getAttribute('href')).toBe(
      'http://forge.example/jeremyhahn/go-truststrap/pulls/57',
    );
    expect(row.textContent).toContain('merged');

    // Attempt 1's pull request is on the task and must appear nowhere on
    // this page — not as a link, not as text, and not behind "Open PR".
    expect(container.textContent).not.toContain('#41');
    expect(container.querySelectorAll('a[href*="/pulls/41"]').length).toBe(0);
    for (const open of screen.getAllByRole('link', { name: /Open PR/ })) {
      expect(open.getAttribute('href')).toBe(
        'http://forge.example/jeremyhahn/go-truststrap/pulls/57',
      );
    }
  });

  it("shows attempt 1's pull request on attempt 1's page, not the later one's", () => {
    // The same task, read from the job the FIRST attempt ran. Nothing about
    // the task changed; only the delivery the server scoped to the attempt.
    storeMock.selectedJob = sdlcJob({
      job_id: 'job-1',
      attempt: 1,
      delivery: {
        branch: 'issue/26-harden',
        head_sha: '1111111111111111',
        pull_request_number: 41,
        pull_request_url: 'http://forge.example/jeremyhahn/go-truststrap/pulls/41',
        pull_request_state: 'closed',
      },
    });
    const { container } = render(JobDetailView);

    const row = container.querySelector('[data-testid="summary-pull-request"]') as HTMLElement;
    expect(within(row).getByRole('link', { name: /#41/ }).getAttribute('href')).toBe(
      'http://forge.example/jeremyhahn/go-truststrap/pulls/41',
    );
    expect(container.querySelectorAll('a[href*="/pulls/57"]').length).toBe(0);
    expect(container.textContent).not.toContain('#57');
  });

  it('leaves the row empty when the attempt itself delivered nothing', () => {
    // Attempt 2 died before publishing. The task still carries #41 and #57,
    // and neither is this job's, so the row says so instead of borrowing one.
    storeMock.selectedJob = sdlcJob({ job_id: 'job-2', attempt: 2, delivery: null });
    const { container } = render(JobDetailView);

    const row = container.querySelector('[data-testid="summary-pull-request"]') as HTMLElement;
    expect(row.textContent).toContain('—');
    expect(within(row).queryByRole('link')).toBeNull();
    expect(screen.queryByRole('link', { name: /Open PR/ })).toBeNull();
  });
});

describe('JobDetailView — a workflow job keeps its own page', () => {
  beforeEach(() => {
    storeMock.selectedJob = workflowJob();
    storeMock.jobs = [];
    storeMock.tasks = [];
  });

  it('still renders the iteration gauge and the task progress', () => {
    const { container } = render(JobDetailView);
    const panel = container.querySelector('[data-testid="iteration-progress"]');
    expect(panel).not.toBeNull();
    expect(panel!.textContent).toContain('3 / 10 max');
    const bar = container.querySelector(
      'progress[aria-label="Task completion progress"]',
    ) as HTMLProgressElement;
    expect(bar.value).toBe(5);
    expect(bar.max).toBe(10);
  });

  it('still names the workflow and its lead backend', () => {
    const { container } = render(JobDetailView);
    const card = container.querySelector('[data-testid="summary-workflow"]');
    expect(card!.textContent).toContain('delivery');
    expect(card!.textContent).toContain('Lead backend: claude');
  });

  it('still offers the DAG and cost tabs', () => {
    render(JobDetailView);
    expect(screen.getByRole('tab', { name: /^Dag$/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /^Cost$/i })).toBeInTheDocument();
  });

  it('renders none of the sdlc panels, which it has no data for', () => {
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="stage-timeline"]')).toBeNull();
    expect(container.querySelector('[data-testid="delivery"]')).toBeNull();
    expect(container.querySelector('[data-testid="verification"]')).toBeNull();
    expect(container.querySelector('[data-testid="summary-attempt"]')).toBeNull();
  });

  it('is what a record that names no runner at all is treated as', () => {
    storeMock.selectedJob = workflowJob({ runner: undefined });
    const { container } = render(JobDetailView);
    expect(container.querySelector('[data-testid="iteration-progress"]')).not.toBeNull();
  });
});
