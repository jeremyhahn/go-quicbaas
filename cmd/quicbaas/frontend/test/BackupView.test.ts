/**
 * BackupView.test.ts
 *
 * Verifies that BackupView lists backups through client.backups.list, shows
 * the empty state, creates a backup from the form, verifies one inline and
 * deletes one only after confirmation, and that its error alert retries.
 * Also checks that #/backup is routed to the view.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/svelte';

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import BackupView from '../src/views/cluster/BackupView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';
import { navigate } from '../src/stores/navigation.svelte.ts';
import { routerStore } from '../src/stores/router.svelte.ts';

const mockList = vi.fn();
const mockCreate = vi.fn();
const mockDelete = vi.fn();
const mockVerify = vi.fn();

const BACKUPS = [
  { backup_id: 'b1', name: 'nightly', timestamp: 1700000000, size: 2048, status: 'completed', shards: [1, 2] },
  { backup_id: 'b2', name: 'weekly', timestamp: 1700100000, size: 10, status: 'pending', shards: [] },
];

beforeEach(() => {
  vi.clearAllMocks();
  mockList.mockResolvedValue({ backups: BACKUPS, total_count: 2 });
  mockCreate.mockResolvedValue({ backup_id: 'b3', timestamp: 1, size: 0, status: 'pending' });
  mockDelete.mockResolvedValue({ success: true });
  mockVerify.mockResolvedValue({ valid: true, checksum_match: true });
  vi.mocked(getSDKClient).mockResolvedValue({
    backups: { list: mockList, create: mockCreate, delete: mockDelete, verify: mockVerify },
  } as never);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('BackupView', () => {
  it('renders the page heading with icon', async () => {
    const { container } = render(BackupView);
    await waitFor(() => {
      const h1 = container.querySelector('h1');
      expect(h1).not.toBeNull();
      expect(h1!.textContent).toContain('Backup');
      expect(container.querySelector('header.page-header svg')).not.toBeNull();
    });
  });

  it('renders a row per backup', async () => {
    render(BackupView);
    await waitFor(() => {
      expect(screen.getByText('nightly')).toBeInTheDocument();
      expect(screen.getByText('weekly')).toBeInTheDocument();
    });
    expect(screen.getByText('2.0 KiB')).toBeInTheDocument();
  });

  it('shows the empty state', async () => {
    mockList.mockResolvedValue({ backups: [], total_count: 0 });
    render(BackupView);
    await waitFor(() => expect(screen.getByText('No backups yet')).toBeInTheDocument());
  });

  it('creates a backup from the form and reloads', async () => {
    render(BackupView);
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));
    const button = screen.getByRole('button', { name: /^create$/i });
    expect(button).toBeDisabled();

    const nameInput = screen.getByLabelText('Name');
    const descInput = screen.getByLabelText('Description');
    await fireEvent.input(nameInput, { target: { value: 'manual' } });
    await fireEvent.input(descInput, { target: { value: 'before upgrade' } });
    expect(button).not.toBeDisabled();
    await fireEvent.click(button);

    await waitFor(() =>
      expect(mockCreate).toHaveBeenCalledWith({ name: 'manual', description: 'before upgrade' }),
    );
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('shows Valid after verify', async () => {
    render(BackupView);
    await waitFor(() => expect(screen.getByText('nightly')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Verify' })[0]);
    await waitFor(() => expect(mockVerify).toHaveBeenCalledWith('b1'));
    await waitFor(() => expect(screen.getByText('Valid')).toBeInTheDocument());
  });

  it('shows the issues when verify finds the backup invalid', async () => {
    mockVerify.mockResolvedValue({ valid: false, checksum_match: false, issues: ['checksum mismatch'] });
    render(BackupView);
    await waitFor(() => expect(screen.getByText('nightly')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Verify' })[0]);
    await waitFor(() => expect(screen.getByText('checksum mismatch')).toBeInTheDocument());
  });

  it('deletes after confirmation', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(BackupView);
    await waitFor(() => expect(screen.getByText('nightly')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('nightly'));
    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith('b1'));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('does not delete when confirmation is declined', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(BackupView);
    await waitFor(() => expect(screen.getByText('nightly')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
    expect(mockDelete).not.toHaveBeenCalled();
  });

  it('shows an error alert whose Retry lists again', async () => {
    mockList.mockRejectedValueOnce(new Error('boom'));
    render(BackupView);
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('boom'));
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByText('nightly')).toBeInTheDocument());
  });
});

describe('backup routing', () => {
  it('navigate("backup") sets the hash to #/backup', () => {
    window.location.hash = '#/dashboard';
    navigate('backup');
    expect(window.location.hash).toBe('#/backup');
  });

  it('the router maps #/backup to the backup route', async () => {
    window.location.hash = '#/backup';
    await waitFor(() => expect(routerStore.name).toBe('backup'));
  });
});
