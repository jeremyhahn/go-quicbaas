// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

import { describe, it, expect } from 'vitest';
import {
  itemAgeInstant,
  itemAgeLabel,
  oldestAgeInstant,
  isEnvironmentalAlert,
  isParkedTask,
  kindLabel,
  blockKindLabel,
  conditionLabel,
  severityVariant,
  actionLabel,
  primaryAction,
  secondaryActions,
  takesText,
  submitLabel,
  hasIssueLink,
  parkedCountByProject,
  readDismissedAlerts,
  writeDismissedAlerts,
  visibleAlerts,
  filterAttention,
  fetchAllPages,
} from '../src/lib/attention.ts';

describe('itemAgeInstant / itemAgeLabel', () => {
  it('prefers status_changed_at over created_at', () => {
    const nowNs = Date.now() * 1_000_000;
    const item = { status_changed_at: nowNs - 60_000 * 1_000_000, created_at: nowNs - 3_600_000 * 1_000_000 };
    expect(itemAgeInstant(item)).toBe(item.status_changed_at);
    expect(itemAgeLabel(item)).toMatch(/m ago$/);
  });

  it('falls back to created_at when status_changed_at is absent', () => {
    // Two hours, not one: formatRelative's minute/hour boundary sits at
    // exactly 60 minutes, and a fixture pinned to precisely one hour ago
    // flakes on the milliseconds test execution itself takes, rounding
    // down to "59m ago".
    const nowNs = Date.now() * 1_000_000;
    const item = { created_at: nowNs - 7_200_000 * 1_000_000 };
    expect(itemAgeInstant(item)).toBe(item.created_at);
    expect(itemAgeLabel(item)).toMatch(/h ago$/);
  });

  it('renders an em-dash when neither timestamp is present', () => {
    expect(itemAgeLabel({})).toBe('—');
  });
});

describe('oldestAgeInstant', () => {
  it('returns the age instant of the oldest item', () => {
    const nowNs = Date.now() * 1_000_000;
    const items = [
      { id: 'a', status_changed_at: nowNs - 60_000 * 1_000_000 },
      { id: 'b', status_changed_at: nowNs - 7_200_000 * 1_000_000 },
      { id: 'c', status_changed_at: nowNs - 300_000 * 1_000_000 },
    ];
    expect(oldestAgeInstant(items)).toBe(items[1]!.status_changed_at);
  });

  it('returns undefined for an empty list', () => {
    expect(oldestAgeInstant([])).toBeUndefined();
  });
});

describe('isEnvironmentalAlert / isParkedTask', () => {
  it('classifies each attention kind exactly once', () => {
    expect(isParkedTask({ kind: 'parked_task' })).toBe(true);
    expect(isEnvironmentalAlert({ kind: 'parked_task' })).toBe(false);
    for (const kind of ['agent_offline', 'account_error', 'barrier_sealed', 'project_idle']) {
      expect(isEnvironmentalAlert({ kind })).toBe(true);
      expect(isParkedTask({ kind })).toBe(false);
    }
  });

  it('treats an unknown kind as neither', () => {
    expect(isEnvironmentalAlert({ kind: 'made-up' })).toBe(false);
    expect(isParkedTask({ kind: 'made-up' })).toBe(false);
  });
});

describe('label lookups', () => {
  it('renders known kinds, block kinds and conditions in plain English', () => {
    expect(kindLabel('agent_offline')).toBe('Agent offline');
    expect(kindLabel('project_idle')).toBe('Project idle');
    expect(blockKindLabel('needs_info')).toBe('Needs info');
    expect(conditionLabel('owner_ruling')).toBe('Waiting on an owner ruling');
  });

  it('falls back to the raw value for anything unrecognised', () => {
    expect(kindLabel('made-up')).toBe('made-up');
    expect(blockKindLabel('made-up')).toBe('made-up');
    expect(conditionLabel('made-up')).toBe('made-up');
    expect(blockKindLabel(undefined)).toBe('');
    expect(conditionLabel(undefined)).toBe('');
  });
});

describe('severityVariant', () => {
  it('maps critical and warning to their badge variants', () => {
    expect(severityVariant('critical')).toBe('error');
    expect(severityVariant('warning')).toBe('warning');
  });

  it('defaults anything else to info', () => {
    expect(severityVariant(undefined)).toBe('info');
    expect(severityVariant('made-up')).toBe('info');
  });
});

describe('primaryAction / secondaryActions', () => {
  it('prefers approve_recommendation when it is offered', () => {
    const item = { actions: ['readmit', 'approve_recommendation', 'close_as_done'] };
    expect(primaryAction(item)).toBe('approve_recommendation');
    expect(secondaryActions(item)).toEqual(['readmit', 'close_as_done']);
  });

  it('falls back to the first action when no recommendation exists', () => {
    const item = { actions: ['answer', 'readmit', 'close_as_done'] };
    expect(primaryAction(item)).toBe('answer');
    expect(secondaryActions(item)).toEqual(['readmit', 'close_as_done']);
  });

  it('is null for an item with no actions — an environmental alert', () => {
    expect(primaryAction({})).toBeNull();
    expect(secondaryActions({})).toEqual([]);
  });

  it('renders every action label', () => {
    expect(actionLabel('approve_recommendation')).toBe('Approve recommendation');
    expect(actionLabel('allow_another_round')).toBe('Allow another round');
    expect(actionLabel('note')).toBe('Add note');
    expect(actionLabel('made-up')).toBe('made-up');
  });
});

describe('takesText / submitLabel', () => {
  it('opens the text box for answer and note only', () => {
    expect(takesText('answer')).toBe(true);
    expect(takesText('note')).toBe(true);
    expect(takesText('readmit')).toBe(false);
    expect(takesText('made-up')).toBe(false);
  });

  it('names the submit button for each text action and falls back to the action label', () => {
    expect(submitLabel('answer')).toBe('Post answer');
    expect(submitLabel('note')).toBe('Post note');
    expect(submitLabel('readmit')).toBe('Readmit');
  });
});

describe('hasIssueLink', () => {
  it('is true only when issue_url is a non-empty string', () => {
    expect(hasIssueLink({ issue_url: 'https://forge.test/acme/api/issues/5' })).toBe(true);
    expect(hasIssueLink({ issue_url: '' })).toBe(false);
    expect(hasIssueLink({})).toBe(false);
  });
});

describe('parkedCountByProject', () => {
  it('counts only parked_task items, grouped by project', () => {
    const items = [
      { kind: 'parked_task', project_id: 'p1', project_name: 'API' },
      { kind: 'parked_task', project_id: 'p1', project_name: 'API' },
      { kind: 'parked_task', project_id: 'p2', project_name: 'Web' },
      { kind: 'agent_offline', project_id: 'p1', project_name: 'API' },
    ];
    const counts = parkedCountByProject(items);
    expect(counts.get('p1')).toEqual({ name: 'API', count: 2 });
    expect(counts.get('p2')).toEqual({ name: 'Web', count: 1 });
    expect(counts.size).toBe(2);
  });

  it('is empty for a list with no parked tasks', () => {
    expect(parkedCountByProject([{ kind: 'agent_offline', project_id: 'p1' }]).size).toBe(0);
  });
});

describe('dismissed alert persistence', () => {
  function memoryStorage(): Storage {
    const map = new Map<string, string>();
    return {
      getItem: (k: string) => map.get(k) ?? null,
      setItem: (k: string, v: string) => void map.set(k, v),
      removeItem: (k: string) => void map.delete(k),
      clear: () => map.clear(),
      key: () => null,
      length: 0,
    } as Storage;
  }

  it('round-trips a set of dismissed ids through storage', () => {
    const storage = memoryStorage();
    writeDismissedAlerts(storage, new Set(['agent_offline:a1', 'barrier_sealed:system']));
    const read = readDismissedAlerts(storage);
    expect(read.has('agent_offline:a1')).toBe(true);
    expect(read.has('barrier_sealed:system')).toBe(true);
    expect(read.size).toBe(2);
  });

  it('reads an empty set when storage is undefined or empty', () => {
    expect(readDismissedAlerts(undefined).size).toBe(0);
    expect(readDismissedAlerts(memoryStorage()).size).toBe(0);
  });

  it('reads an empty set from malformed stored JSON rather than throwing', () => {
    const storage = memoryStorage();
    storage.setItem('quicbaas.attention.dismissedAlerts', '{not json');
    expect(readDismissedAlerts(storage).size).toBe(0);
  });

  it('does not throw when storage.setItem throws (private browsing / full quota)', () => {
    const throwing: Pick<Storage, 'setItem'> = {
      setItem: () => {
        throw new Error('quota exceeded');
      },
    };
    expect(() => writeDismissedAlerts(throwing, new Set(['x']))).not.toThrow();
  });
});

describe('visibleAlerts', () => {
  it('keeps only environmental alerts not yet dismissed', () => {
    const items = [
      { id: 'agent_offline:a1', kind: 'agent_offline' },
      { id: 'account_error:a2', kind: 'account_error' },
      { id: 'task-1', kind: 'parked_task' },
    ];
    const got = visibleAlerts(items, new Set(['account_error:a2']));
    expect(got.map((i) => i.id)).toEqual(['agent_offline:a1']);
  });

  it('is empty when every alert is dismissed', () => {
    const items = [{ id: 'agent_offline:a1', kind: 'agent_offline' }];
    expect(visibleAlerts(items, new Set(['agent_offline:a1']))).toEqual([]);
  });
});

describe('filterAttention', () => {
  const items = [
    { id: 't1', kind: 'parked_task', project_id: 'p1' },
    { id: 't2', kind: 'parked_task', project_id: 'p2' },
    { id: 'a1', kind: 'agent_offline' },
  ];

  it('keeps everything when no filter is chosen', () => {
    expect(filterAttention(items, {}).map((i) => i.id)).toEqual(['t1', 't2', 'a1']);
  });

  it('narrows by project and by kind, together', () => {
    expect(filterAttention(items, { kind: 'parked_task' }).map((i) => i.id)).toEqual(['t1', 't2']);
    expect(filterAttention(items, { projectID: 'p2' }).map((i) => i.id)).toEqual(['t2']);
    expect(filterAttention(items, { projectID: 'p1', kind: 'agent_offline' })).toEqual([]);
  });

  it('never mutates the complete set it views', () => {
    filterAttention(items, { kind: 'agent_offline' });
    expect(items.length).toBe(3);
  });
});

describe('fetchAllPages', () => {
  it('follows next_offset to the end and keeps the first page\'s total and facets', async () => {
    const pages: Record<number, { items: number[]; next_offset?: number }> = {
      0: { items: [1, 2], next_offset: 2 },
      2: { items: [3, 4], next_offset: 4 },
      4: { items: [5] },
    };
    const asked: number[] = [];
    const set = await fetchAllPages<number>(async (offset) => {
      asked.push(offset);
      return { ...pages[offset], offset, total: 5, facets: offset === 0 ? { kind: { parked_task: 5 } } : undefined };
    });
    expect(asked).toEqual([0, 2, 4]);
    expect(set.items).toEqual([1, 2, 3, 4, 5]);
    expect(set.total).toBe(5);
    expect(set.facets).toEqual({ kind: { parked_task: 5 } });
  });

  it('stops on a next_offset that does not advance rather than looping', async () => {
    let calls = 0;
    const set = await fetchAllPages<number>(async () => {
      calls++;
      return { items: [1], offset: 0, next_offset: 0, total: 1 };
    });
    expect(calls).toBe(1);
    expect(set.items).toEqual([1]);
  });

  it('stops at the page cap for a server that never ends', async () => {
    let calls = 0;
    await fetchAllPages<number>(async (offset) => {
      calls++;
      return { items: [offset], offset, next_offset: offset + 1 };
    }, 3);
    expect(calls).toBe(3);
  });

  it('propagates a failed page instead of returning a partial set as complete', async () => {
    await expect(
      fetchAllPages<number>(async (offset) => {
        if (offset > 0) throw new Error('down');
        return { items: [1], offset: 0, next_offset: 1 };
      }),
    ).rejects.toThrow('down');
  });
});
