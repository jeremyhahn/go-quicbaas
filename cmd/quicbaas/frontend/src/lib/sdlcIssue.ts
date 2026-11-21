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

/**
 * Filing an issue on a bound project's repository, as the SDLC panel's
 * "File issue" action does it. The issue is written through the binding's own
 * forge token and audited; see pkg/orchestration/sdlc/issue_filing.go.
 */

import type { FileIssueRequest } from '@quicbaas/sdk';

/** MAX_TITLE mirrors the module's bound on an issue title. */
export const MAX_TITLE = 255;

/** FileIssueFormState is what the "File issue" form edits. */
export interface FileIssueFormState {
  title: string;
  body: string;
  /** Hand the issue straight to the factory with the binding's trigger label. */
  handToFactory: boolean;
  /** The issue this one was split out of; empty for none. */
  parent: string;
}

/** emptyFileIssueForm is what the form opens with. */
export function emptyFileIssueForm(): FileIssueFormState {
  return { title: '', body: '', handToFactory: false, parent: '' };
}

/**
 * validateFileIssueForm returns the first reason the issue cannot be filed,
 * or the empty string. It mirrors IssueFiling.Validate so a refusal is visible
 * before the round trip.
 */
export function validateFileIssueForm(form: FileIssueFormState): string {
  const title = form.title.trim();
  if (title === '') return 'An issue needs a title.';
  if (title.length > MAX_TITLE) return `The title must be at most ${MAX_TITLE} characters.`;
  if (/[\r\n]/.test(title)) return 'The title must be a single line.';
  const parent = form.parent.trim();
  if (parent !== '' && !/^[1-9][0-9]*$/.test(parent)) {
    return 'The parent must be an issue number.';
  }
  return '';
}

/** fileIssueRequestFrom builds the request the module is asked to file. */
export function fileIssueRequestFrom(form: FileIssueFormState, triggerLabel: string): FileIssueRequest {
  const parent = form.parent.trim();
  const trigger = triggerLabel.trim();
  return {
    title: form.title.trim(),
    body: form.body.trim() || undefined,
    labels: form.handToFactory && trigger !== '' ? [trigger] : undefined,
    parent: parent !== '' ? Number(parent) : undefined,
  };
}
