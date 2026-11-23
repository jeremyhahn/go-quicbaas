/**
 * BackupView.test.ts
 *
 * Verifies that BackupView lists every backup through client.backups.list
 * (asking again with total_count), shows ID and type, marks backups with no
 * shards, shows the empty state, creates a typed backup from the form, verifies one inline and
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
import { showToast } from '../src/stores/toast.svelte.ts';
import { navigate } from '../src/stores/navigation.svelte.ts';
import { routerStore } from '../src/stores/router.svelte.ts';

const mockList = vi.fn();
const mockCreate = vi.fn();
const mockDelete = vi.fn();
const mockVerify = vi.fn();

const BACKUPS = [
  { backup_id: 'backup-1', name: 'full', description: 'Backup created at 2023-11-14T22:13:20Z', timestamp: 1700000000000000000, size: 2048, status: 'completed', shards: [1, 2] },
  { backup_id: 'backup-2', name: 'incremental', description: 'Backup created at 2023-11-16T02:00:00Z', timestamp: 1700100000000000000, size: 10, status: 'completed', shards: [] },
];

function rows(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    backup_id: `backup-${i}`, name: 'full', timestamp: 1700000000000000000, size: 1, status: 'completed', shards: [1],
  }));
}

beforeEach(() => {
  vi.clearAllMocks();
  mockList.mockResolvedValue({ backups: BACKUPS, total_count: 2 });
  mockCreate.mockResolvedValue({ backup_id: 'backup-3', timestamp: 1, size: 0, status: 'pending' });
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
      expect(screen.getByText('backup-1')).toBeInTheDocument();
      expect(screen.getByText('backup-2')).toBeInTheDocument();
    });
    expect(screen.getByText('full')).toBeInTheDocument();
    expect(screen.getByText('incremental')).toBeInTheDocument();
    expect(screen.queryByText(/Backup created at/)).toBeNull();
    expect(screen.getByText('2.0 KiB')).toBeInTheDocument();
    expect(screen.getByText('2 backups')).toBeInTheDocument();
    expect(mockList.mock.calls[0]).toEqual([{ limit: 100 }]);
  });

  it('marks a backup that captured no shards', async () => {
    render(BackupView);
    await waitFor(() => expect(screen.getByText('backup-2')).toBeInTheDocument());
    expect(screen.getAllByText('no shard data')).toHaveLength(1);
    expect(screen.getByRole('note')).toHaveTextContent('0 shards');
  });

  it('fetches every backup through total_count', async () => {
    mockList
      .mockResolvedValueOnce({ backups: rows(100), total_count: 150 })
      .mockResolvedValueOnce({ backups: rows(150), total_count: 150 });
    render(BackupView);
    await waitFor(() => expect(screen.getByText('150 backups')).toBeInTheDocument());
    expect(mockList.mock.calls).toEqual([[{ limit: 100 }], [{ limit: 150 }]]);
  });

  it('says how many backups could not be read', async () => {
    mockList.mockResolvedValue({ backups: BACKUPS, total_count: 3 });
    render(BackupView);
    await waitFor(() =>
      expect(screen.getByText('2 of 3 backups; 1 could not be read')).toBeInTheDocument(),
    );
    expect(mockList).toHaveBeenCalledTimes(1);
  });

  it('shows the empty state', async () => {
    mockList.mockResolvedValue({ backups: [], total_count: 0 });
    render(BackupView);
    await waitFor(() => expect(screen.getByText('No backups yet')).toBeInTheDocument());
  });

  it('creates a backup from the form and reloads', async () => {
    render(BackupView);
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));
    expect(screen.queryByLabelText('Name')).toBeNull();
    expect(screen.queryByLabelText('Description')).toBeNull();
    const button = screen.getByRole('button', { name: /^create$/i });
    expect(button).not.toBeDisabled();

    await fireEvent.change(screen.getByLabelText('Type'), { target: { value: 'incremental' } });
    await fireEvent.click(button);

    await waitFor(() => expect(mockCreate).toHaveBeenCalledWith({ type: 'incremental' }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Backup backup-3 created'));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('shows Valid after verify', async () => {
    render(BackupView);
    await waitFor(() => expect(screen.getByText('backup-1')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Verify' })[0]);
    await waitFor(() => expect(mockVerify).toHaveBeenCalledWith('backup-1'));
    await waitFor(() => expect(screen.getByText('Valid')).toBeInTheDocument());
  });

  it('shows the issues when verify finds the backup invalid', async () => {
    mockVerify.mockResolvedValue({ valid: false, checksum_match: false, issues: ['checksum mismatch'] });
    render(BackupView);
    await waitFor(() => expect(screen.getByText('backup-1')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Verify' })[0]);
    await waitFor(() => expect(screen.getByText('checksum mismatch')).toBeInTheDocument());
  });

  it('deletes after confirmation', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(BackupView);
    await waitFor(() => expect(screen.getByText('backup-1')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
    expect(confirm).toHaveBeenCalledWith(expect.stringContaining('backup-1'));
    await waitFor(() => expect(mockDelete).toHaveBeenCalledWith('backup-1'));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('does not delete when confirmation is declined', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false);
    render(BackupView);
    await waitFor(() => expect(screen.getByText('backup-1')).toBeInTheDocument());
    await fireEvent.click(screen.getAllByRole('button', { name: 'Delete' })[0]);
    expect(mockDelete).not.toHaveBeenCalled();
  });

  it('shows an error alert whose Retry lists again', async () => {
    mockList.mockRejectedValueOnce(new Error('boom'));
    render(BackupView);
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('boom'));
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.getByText('backup-1')).toBeInTheDocument());
  });

  it('shows that backups are not available when the client has none', async () => {
    vi.mocked(getSDKClient).mockResolvedValue({ backups: null } as never);
    render(BackupView);
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Backups are not available over this connection'),
    );
    expect(mockList).not.toHaveBeenCalled();
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
