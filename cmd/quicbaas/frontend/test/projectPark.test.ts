/**
 * projectPark.test.ts
 *
 * The park control's payload carries the park switch and nothing else, and
 * the summary describes a parked project and stays silent for an open one.
 */

import { describe, it, expect } from 'vitest';
import type { Project } from '@quicbaas/sdk';
import { parkRequestFrom, parkSummary } from '../src/lib/projectPark.ts';

const project: Project = {
  id: 'p-1',
  tenant_id: 't-1',
  name: 'alpha',
  autonomous: true,
};

describe('parkRequestFrom', () => {
  it('parks the project and sends no other field', () => {
    expect(parkRequestFrom('t-1', project, true)).toEqual({
      tenant_id: 't-1',
      project_id: 'p-1',
      parked: true,
    });
  });

  it('resumes with an explicit false, never by omission', () => {
    const req = parkRequestFrom('t-1', { ...project, parked: true }, false);
    expect(req.parked).toBe(false);
    expect('autonomous' in req).toBe(false);
  });
});

describe('parkSummary', () => {
  it('is empty for an open project', () => {
    expect(parkSummary(project)).toBe('');
    expect(parkSummary({ ...project, parked: false, parked_by: 'ops' })).toBe('');
  });

  it('names who parked the project and that work in flight finishes', () => {
    const summary = parkSummary({ ...project, parked: true, parked_at: '2026-09-25T12:00:00Z', parked_by: 'ops' });
    expect(summary).toMatch(/^Parked since .+ by ops: /);
    expect(summary).toContain('work already in flight finishes');
  });

  it('omits a time it cannot read rather than printing Invalid Date', () => {
    const summary = parkSummary({ ...project, parked: true, parked_at: 'not-a-time' });
    expect(summary.startsWith('Parked: ')).toBe(true);
    expect(summary).not.toContain('Invalid');
  });
});
