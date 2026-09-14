/**
 * jobDetailLib.test.ts
 *
 * The pure reading of a job record the detail page is built from: which
 * runner drove it, which attempt it was, how long each lifecycle moment took,
 * why it stopped, whether the work may run again, and what the worker last
 * said. go-quicbaas#796.
 */

import { describe, it, expect } from 'vitest';
import {
  AGENT_TURN_EVENT,
  SDLC_RUNNER,
  TERMINAL_REASONS,
  TURN_SILENT_MARKER,
  WORKFLOW_RUNNER,
  attemptLabel,
  currentAttempt,
  formatDuration,
  isTerminalJob,
  isWorkflowJob,
  lastAgentTurn,
  parseAgentTurn,
  readmitAllowed,
  reviewVerdictTone,
  runnerOf,
  stageTimeline,
  terminalReason,
  type JobProvenance,
} from '../src/lib/jobDetail.ts';

/** MS is one millisecond in the server's Unix nanoseconds. */
const MS = 1_000_000;

/** sdlcJob is an sdlc attempt with everything the server records. */
function sdlcJob(over: Partial<JobProvenance> = {}): JobProvenance {
  return {
    runner: SDLC_RUNNER,
    state: 'failed',
    attempt: 3,
    attempts: [
      { attempt: 1, job_id: 'job-1' },
      { attempt: 2, job_id: 'job-2' },
      { attempt: 3, job_id: 'job-3', current: true },
    ],
    attempt_budget: 3,
    updated_at: 10_000 * MS,
    stages: [
      { stage: 'prepare', entered_at: 1_000 * MS },
      { stage: 'reviewable', entered_at: 4_000 * MS },
      { stage: 'discard', entered_at: 9_500 * MS },
    ],
    terminal_reason: 'budget_exhausted',
    readmit_allowed: false,
    ...over,
  };
}

describe('runnerOf / isWorkflowJob', () => {
  it('names the runner a job records', () => {
    expect(runnerOf({ runner: SDLC_RUNNER })).toBe(SDLC_RUNNER);
    expect(isWorkflowJob({ runner: SDLC_RUNNER })).toBe(false);
  });

  it('treats a record that names no runner as the workflow runner', () => {
    // The server's own JobStatus.Runner comment: empty on a record written
    // before runners existed, which is a workflow job.
    expect(runnerOf({})).toBe(WORKFLOW_RUNNER);
    expect(runnerOf({ runner: '   ' })).toBe(WORKFLOW_RUNNER);
    expect(isWorkflowJob({})).toBe(true);
  });
});

describe('attemptLabel / currentAttempt', () => {
  it('reads the ordinal off the attempt the server marked current', () => {
    const job = sdlcJob();
    expect(currentAttempt(job)?.job_id).toBe('job-3');
    expect(attemptLabel(job)).toBe('3 of 3');
  });

  it('falls back to the ordinal on the record when no attempt is marked', () => {
    // A job read before the task's history caught up still knows what it was.
    expect(attemptLabel({ updated_at: 0, attempt: 2 })).toBe('2 of 2');
  });

  it('says nothing for a job that is not an attempt at a task', () => {
    expect(attemptLabel({ updated_at: 0 })).toBe('');
    expect(attemptLabel({ updated_at: 0, attempt: 0, attempts: [] })).toBe('');
    expect(currentAttempt({ updated_at: 0 })).toBeNull();
  });
});

describe('stageTimeline', () => {
  it('measures each moment to the next, and the last to the job\'s end', () => {
    const spans = stageTimeline(sdlcJob({ state: 'executing' }));
    expect(spans.map((s) => s.stage)).toEqual(['prepare', 'reviewable', 'discard']);
    expect(spans[0].durationMs).toBe(3_000);
    expect(spans[1].durationMs).toBe(5_500);
    expect(spans[2].durationMs).toBe(500);
    expect(spans.map((s) => s.open)).toEqual([false, false, true]);
  });

  it('leaves no span open on a job that has stopped', () => {
    // Being last is not being open: a finished job's final moment has nothing
    // after it precisely because the job stopped there. Keying `open` on
    // "nothing follows" had every completed job reporting "(still open)".
    for (const state of ['completed', 'failed']) {
      const spans = stageTimeline(sdlcJob({ state }));
      expect(spans.map((s) => s.open), state).toEqual([false, false, false]);
    }
  });

  it('keeps the last span open only while the job is still running', () => {
    for (const state of ['initializing', 'executing', 'reviewing', 'cleanup']) {
      const spans = stageTimeline(sdlcJob({ state }));
      expect(spans[spans.length - 1].open, state).toBe(true);
    }
  });

  it('treats a record carrying no state as still running', () => {
    // An absent state is not evidence the job stopped; claiming it did would
    // close a span the job may still be inside.
    const spans = stageTimeline(sdlcJob({ state: undefined }));
    expect(spans[spans.length - 1].open).toBe(true);
  });

  it('never reports a negative span for a record whose clocks disagree', () => {
    const spans = stageTimeline({
      updated_at: 0,
      stages: [{ stage: 'prepare', entered_at: 5_000 * MS }],
    });
    expect(spans[0].durationMs).toBe(0);
  });

  it('is empty for a job that has entered no moment', () => {
    expect(stageTimeline({ updated_at: 1 })).toEqual([]);
    expect(stageTimeline({ updated_at: 1, stages: [] })).toEqual([]);
  });
});

describe('isTerminalJob', () => {
  it('names the two states a job never leaves', () => {
    expect(isTerminalJob({ state: 'completed' })).toBe(true);
    expect(isTerminalJob({ state: 'failed' })).toBe(true);
  });

  it('treats every other state, and an absent one, as still running', () => {
    for (const state of ['executing', 'reviewing', 'cleanup', 'escalating', '']) {
      expect(isTerminalJob({ state }), state).toBe(false);
    }
    expect(isTerminalJob({})).toBe(false);
  });
});

describe('formatDuration', () => {
  it('scales from milliseconds to hours', () => {
    expect(formatDuration(340)).toBe('340ms');
    expect(formatDuration(1_400)).toBe('1.4s');
    expect(formatDuration(130_000)).toBe('2m 10s');
    expect(formatDuration(3_960_000)).toBe('1h 06m');
  });

  it('renders a zero or negative span as zero rather than as nonsense', () => {
    expect(formatDuration(0)).toBe('0ms');
    expect(formatDuration(-5)).toBe('0ms');
  });
});

describe('terminalReason', () => {
  it('renders every reason in the server\'s closed vocabulary', () => {
    // pkg/orchestration/task_conclusion.go's TaskConclusionReasons(). A reason
    // added there and not here is a missing key, caught by this test rather
    // than by an operator reading a raw wire string.
    const vocabulary = [
      'completed',
      'budget_exhausted',
      'abandoned',
      'fault_bounded',
      'fault_held',
      'parked_needs_info',
      'parked_blocked',
      'parked_escalated',
      'readmitted',
    ];
    expect(Object.keys(TERMINAL_REASONS).sort()).toEqual([...vocabulary].sort());
    for (const reason of vocabulary) {
      const view = terminalReason({ terminal_reason: reason });
      expect(view, reason).not.toBeNull();
      expect(view!.label, reason).not.toBe('');
      expect(view!.detail, reason).not.toBe('');
    }
  });

  it('is null while the task has not concluded', () => {
    expect(terminalReason({})).toBeNull();
    expect(terminalReason({ terminal_reason: '' })).toBeNull();
    expect(terminalReason({ terminal_reason: '   ' })).toBeNull();
  });

  it('shows a reason this build does not know rather than dropping it', () => {
    const view = terminalReason({ terminal_reason: 'invented_by_a_newer_server' });
    expect(view?.label).toBe('invented_by_a_newer_server');
    expect(view?.tone).toBe('info');
  });
});

describe('readmitAllowed', () => {
  it('takes the server\'s answer and nothing else', () => {
    // abandoned parks (readmittable) and budget_exhausted fails (terminal);
    // both are "the attempts ran out", so the reason cannot decide it.
    expect(readmitAllowed({ readmit_allowed: true })).toBe(true);
    expect(readmitAllowed({ readmit_allowed: false })).toBe(false);
  });

  it('refuses when the server said nothing', () => {
    expect(readmitAllowed({})).toBe(false);
  });
});

describe('parseAgentTurn', () => {
  // The fixtures are what pkg/sdlc's AgentTurn.Describe writes (#787): a
  // single line of space-separated key=value counts, a blank line, then the
  // reply. Anything less faithful would be testing an invented format.
  const DESCRIBE_OUTPUT =
    'attempt=3 round=1 elapsed=42.5s reply_bytes=812 tool_uses=4 blocks=text:2,tool_use:4 ' +
    'stop_reason=end_turn tokens_in=1843 tokens_out=204\n\n' +
    'I read the charter and the issue.\n\nThe tree already satisfies it.\n\nSDLC-STOP: owner-decision';

  it('splits the daemon\'s header of counts from the worker\'s own words', () => {
    const turn = parseAgentTurn(DESCRIBE_OUTPUT);
    expect(Object.fromEntries(turn.metrics.map((m) => [m.key, m.value]))).toEqual({
      attempt: '3',
      round: '1',
      elapsed: '42.5s',
      reply_bytes: '812',
      tool_uses: '4',
      blocks: 'text:2,tool_use:4',
      stop_reason: 'end_turn',
      tokens_in: '1843',
      tokens_out: '204',
    });
    // Only the FIRST blank line splits: the reply has blank lines of its own.
    expect(turn.reply).toBe(
      'I read the charter and the issue.\n\nThe tree already satisfies it.\n\nSDLC-STOP: owner-decision',
    );
    expect(turn.silent).toBe(false);
    expect(turn.truncated).toBe(false);
  });

  it('reads a silent turn as silent rather than as an empty reply', () => {
    const turn = parseAgentTurn(`attempt=1 round=1 reply_bytes=0 tool_uses=0\n\n${TURN_SILENT_MARKER}`);
    expect(turn.silent).toBe(true);
    expect(turn.reply).toBe('');
  });

  it('reports a reply the producer truncated', () => {
    const turn = parseAgentTurn('attempt=1 round=1 reply_bytes=9000 tool_uses=0 truncated=true\n\nthe head of it');
    expect(turn.truncated).toBe(true);
    expect(turn.reply).toBe('the head of it');
  });

  it('treats a body with no header as all reply rather than discarding it', () => {
    // The producer is on an unmerged branch; losing the worker's words
    // because an expected header was absent is the worse failure.
    const turn = parseAgentTurn('just the words, no counts');
    expect(turn.metrics).toEqual([]);
    expect(turn.reply).toBe('just the words, no counts');
    expect(turn.silent).toBe(false);
  });

  it('drops an unpaired header token rather than showing a key with no value', () => {
    const turn = parseAgentTurn('attempt=2 garbage =novalue round=1\n\nhello');
    expect(turn.metrics.map((m) => m.key)).toEqual(['attempt', 'round']);
  });

  it('survives an empty body', () => {
    const turn = parseAgentTurn('');
    expect(turn).toEqual({ metrics: [], reply: '', silent: false, truncated: false });
  });
});

describe('lastAgentTurn', () => {
  it('returns the most recent worker turn the job recorded', () => {
    const turn = lastAgentTurn({
      events: [
        { type: AGENT_TURN_EVENT, details: 'attempt=1 round=1\n\nfirst turn' },
        { type: 'state_change', details: 'executing' },
        { type: AGENT_TURN_EVENT, details: 'attempt=3 round=1\n\nthe branch produced no changes' },
      ],
    });
    expect(turn?.reply).toBe('the branch produced no changes');
    expect(turn?.metrics.find((m) => m.key === 'attempt')?.value).toBe('3');
  });

  it('returns null when the daemon recorded no turn', () => {
    // The event is read opportunistically: a daemon that does not emit it has
    // no turn to show, which is not an error.
    expect(lastAgentTurn({ events: [{ type: 'state_change', details: 'failed' }] })).toBeNull();
    expect(lastAgentTurn({ events: [] })).toBeNull();
    expect(lastAgentTurn({})).toBeNull();
    expect(lastAgentTurn({ events: null })).toBeNull();
    expect(lastAgentTurn({ events: [{ type: AGENT_TURN_EVENT, details: '   ' }] })).toBeNull();
  });
});

describe('reviewVerdictTone', () => {
  it('colours the verdicts the reviewing module produces', () => {
    expect(reviewVerdictTone('approve')).toBe('badge-success');
    expect(reviewVerdictTone('request_changes')).toBe('badge-warning');
    expect(reviewVerdictTone('error')).toBe('badge-error');
  });

  it('falls back to neutral for a verdict it does not know', () => {
    expect(reviewVerdictTone('shrug')).toBe('badge-ghost');
    expect(reviewVerdictTone('')).toBe('badge-ghost');
  });
});
