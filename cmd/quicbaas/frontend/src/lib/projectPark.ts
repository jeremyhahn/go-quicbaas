/**
 * projectPark.ts
 *
 * The Projects view's park control. Parking a project closes its intake —
 * nothing that has not started is dispatched, adopted or triaged — and lets
 * the work already in flight finish. It rides the ordinary project update, as
 * the one presence-based `parked` field, so it never carries any other edit.
 */

import type { Project, UpdateProjectRequest } from '@quicbaas/sdk';

/**
 * parkRequestFrom builds the update that parks (true) or resumes (false) one
 * project, and nothing else.
 */
export function parkRequestFrom(tenantID: string, project: Project, parked: boolean): UpdateProjectRequest {
  return { tenant_id: tenantID, project_id: project.id, parked };
}

/**
 * parkSummary says, for a tooltip or a detail line, what parking this project
 * means right now. An open project has nothing to say and answers ''.
 */
export function parkSummary(project: Project): string {
  if (project.parked !== true) return '';
  let since = '';
  if (project.parked_at) {
    const at = new Date(project.parked_at);
    if (!Number.isNaN(at.getTime())) since = ` since ${at.toLocaleString()}`;
  }
  const by = project.parked_by ? ` by ${project.parked_by}` : '';
  return `Parked${since}${by}: no new work starts; work already in flight finishes. Resume to reopen the intake.`;
}
