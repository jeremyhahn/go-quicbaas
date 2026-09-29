/**
 * sdlcFileIssue.test.ts
 *
 * The SDLC panel's "File issue" action: the form is refused where the module
 * would refuse it, the request carries the trigger label only when asked and
 * the parent as a number, and a filed issue is shown with its link.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import {
  emptyFileIssueForm,
  fileIssueRequestFrom,
  validateFileIssueForm,
  MAX_TITLE,
} from '../src/lib/sdlcIssue.ts';
import SdlcFileIssue from '../src/components/orchestrator/SdlcFileIssue.svelte';

describe('file issue form', () => {
  it('builds the request the module files', () => {
    const form = { title: ' Follow-up ', body: ' rest ', handToFactory: true, parent: '12' };
    expect(fileIssueRequestFrom(form, 'task:ready')).toEqual({
      title: 'Follow-up',
      body: 'rest',
      labels: ['task:ready'],
      parent: 12,
    });
    expect(fileIssueRequestFrom({ ...emptyFileIssueForm(), title: 't' }, 'task:ready')).toEqual({
      title: 't',
      body: undefined,
      labels: undefined,
      parent: undefined,
    });
  });

  it('refuses what the module would refuse', () => {
    expect(validateFileIssueForm({ ...emptyFileIssueForm(), title: 'ok' })).toBe('');
    expect(validateFileIssueForm(emptyFileIssueForm())).toContain('title');
    expect(validateFileIssueForm({ ...emptyFileIssueForm(), title: 'x'.repeat(MAX_TITLE + 1) })).toContain('at most');
    expect(validateFileIssueForm({ ...emptyFileIssueForm(), title: 'a\nb' })).toContain('single line');
    expect(validateFileIssueForm({ ...emptyFileIssueForm(), title: 't', parent: '-3' })).toContain('issue number');
    expect(validateFileIssueForm({ ...emptyFileIssueForm(), title: 't', parent: 'abc' })).toContain('issue number');
  });
});

describe('SdlcFileIssue', () => {
  it('files through the handler and shows the filed issue', async () => {
    const onFile = vi.fn().mockResolvedValue({ number: 88, title: 'Follow-up', url: 'https://f/88' });
    render(SdlcFileIssue, { props: { idPrefix: 't', triggerLabel: 'task:ready', onFile } });
    await fireEvent.click(screen.getByText('File issue'));
    await fireEvent.input(screen.getByLabelText('Issue title'), { target: { value: 'Follow-up' } });
    await fireEvent.click(screen.getByLabelText(/Hand to the factory/));
    await fireEvent.click(screen.getByText('File'));
    await waitFor(() => expect(onFile).toHaveBeenCalledWith({ title: 'Follow-up', body: undefined, labels: ['task:ready'], parent: undefined }));
    expect((await screen.findByText('#88')).getAttribute('href')).toBe('https://f/88');
  });

  it('refuses an empty title without calling the handler, and shows a server refusal', async () => {
    const onFile = vi.fn().mockRejectedValue(new Error('labels[0]: repository defines no label named task:ready'));
    render(SdlcFileIssue, { props: { idPrefix: 't', triggerLabel: 'task:ready', onFile } });
    await fireEvent.click(screen.getByText('File issue'));
    await fireEvent.click(screen.getByText('File'));
    expect((await screen.findByTestId('sdlc-file-issue-error')).textContent).toContain('title');
    expect(onFile).not.toHaveBeenCalled();

    await fireEvent.input(screen.getByLabelText('Issue title'), { target: { value: 't' } });
    await fireEvent.click(screen.getByText('File'));
    expect((await screen.findByTestId('sdlc-file-issue-error')).textContent).toContain('no label named');
  });
});
