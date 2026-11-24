/**
 * SSHAuthorizedKeysPanel.test.ts
 *
 * Verifies that AuthorizedKeysPanel reaches every authorized_keys route the
 * SDK builds: list, add, get, remove, emit (format a line) and verify (check
 * a fingerprint), and that it shows a not-available state when the
 * connection has no SSH client.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SSHAuthorizedEntry } from '@quicbaas/sdk';
import { SSHNotFoundError, SSHUnavailableError } from '@quicbaas/sdk';

const entries: SSHAuthorizedEntry[] = [
  {
    scope: 'user',
    owner: 'alice',
    name: 'deploy',
    line: 'ssh-ed25519 AAAAdeploy deploy@ci',
    options: ['restrict'],
    comment: 'ci deploy',
  },
  {
    scope: 'user',
    owner: 'alice',
    name: 'laptop',
    line: 'ssh-ed25519 AAAAlaptop alice@laptop',
  },
];

const {
  mockList, mockAdd, mockGet, mockRemove, mockEmit, mockVerify, mockGetSDKClient,
} = vi.hoisted(() => ({
  mockList: vi.fn(),
  mockAdd: vi.fn(),
  mockGet: vi.fn(),
  mockRemove: vi.fn(),
  mockEmit: vi.fn(),
  mockVerify: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import AuthorizedKeysPanel from '../src/views/security/ssh/AuthorizedKeysPanel.svelte';

function client() {
  return {
    ssh: {
      authorized: {
        list: mockList,
        add: mockAdd,
        get: mockGet,
        remove: mockRemove,
        emit: mockEmit,
        verify: mockVerify,
      },
    },
  };
}

describe('AuthorizedKeysPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockList.mockResolvedValue(entries);
    mockAdd.mockResolvedValue(undefined);
    mockGet.mockResolvedValue(entries[0]);
    mockRemove.mockResolvedValue(undefined);
    mockEmit.mockResolvedValue({ line: 'restrict ssh-ed25519 AAAAnew note' });
    mockVerify.mockResolvedValue({ entry: entries[1] });
    mockGetSDKClient.mockResolvedValue(client());
  });

  it('lists the entries for the scope and owner on mount', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => {
      expect(screen.getByText('deploy')).toBeInTheDocument();
      expect(screen.getByText('laptop')).toBeInTheDocument();
    });
    expect(mockList).toHaveBeenCalledWith({ scope: 'user', owner: 'alice' });
  });

  it('does not send an empty owner', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'service', owner: '' } });
    await waitFor(() => expect(mockList).toHaveBeenCalled());
    expect(mockList).toHaveBeenCalledWith({ scope: 'service' });
  });

  it('reloads on a scope change and drops a late answer for the old scope', async () => {
    let resolveOld: (v: SSHAuthorizedEntry[]) => void = () => {};
    mockList.mockImplementationOnce(
      () => new Promise<SSHAuthorizedEntry[]>((r) => { resolveOld = r; }),
    );
    const view = render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(1));

    mockList.mockResolvedValueOnce([
      { scope: 'tenant', owner: 'alice', name: 'tenant-entry', line: 'ssh-ed25519 AAAAt t@t' },
    ]);
    await view.rerender({ scope: 'tenant', owner: 'alice' });
    await waitFor(() => expect(screen.getByText('tenant-entry')).toBeInTheDocument());
    expect(mockList).toHaveBeenLastCalledWith({ scope: 'tenant', owner: 'alice' });

    resolveOld(entries);
    await new Promise((r) => setTimeout(r, 10));
    expect(screen.getByText('tenant-entry')).toBeInTheDocument();
    expect(screen.queryByText('deploy')).not.toBeInTheDocument();
  });

  it('adds an entry with trimmed options and a comment, then reloads', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'Add authorized entry' }));
    await fireEvent.input(screen.getByLabelText(/^Entry name/), { target: { value: ' ops ' } });
    await fireEvent.input(screen.getByLabelText(/^Entry line/), { target: { value: 'ssh-ed25519 AAAAops ops@x' } });
    await fireEvent.input(screen.getByLabelText(/^Entry options/), { target: { value: ' restrict , no-pty ,' } });
    await fireEvent.input(screen.getByLabelText(/^Entry comment/), { target: { value: 'ops box' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save authorized entry' }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith({
        scope: 'user',
        owner: 'alice',
        name: 'ops',
        line: 'ssh-ed25519 AAAAops ops@x',
        options: ['restrict', 'no-pty'],
        comment: 'ops box',
      });
    });
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('sends only name and line when options and comment are empty', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'Add authorized entry' }));
    await fireEvent.input(screen.getByLabelText(/^Entry name/), { target: { value: 'ops' } });
    await fireEvent.input(screen.getByLabelText(/^Entry line/), { target: { value: 'ssh-ed25519 AAAAops' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save authorized entry' }));

    await waitFor(() => {
      expect(mockAdd).toHaveBeenCalledWith({
        scope: 'user', owner: 'alice', name: 'ops', line: 'ssh-ed25519 AAAAops',
      });
    });
  });

  it('refuses an add with an empty name or line', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'Add authorized entry' }));
    await fireEvent.click(screen.getByRole('button', { name: 'Save authorized entry' }));
    expect(screen.getByText('Name is required')).toBeInTheDocument();

    await fireEvent.input(screen.getByLabelText(/^Entry name/), { target: { value: 'ops' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Save authorized entry' }));
    expect(screen.getByText('Line is required')).toBeInTheDocument();
    expect(mockAdd).not.toHaveBeenCalled();
  });

  it('views an entry through get', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'View authorized entry deploy' }));
    await waitFor(() => {
      expect(mockGet).toHaveBeenCalledWith('user', 'alice', 'deploy');
      expect(screen.getByLabelText('Authorized entry line')).toHaveTextContent('ssh-ed25519 AAAAdeploy deploy@ci');
    });
  });

  it('removes an entry after confirmation, then reloads', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'Remove authorized entry laptop' }));
    expect(screen.getByRole('dialog', { name: 'Remove authorized entry confirmation' })).toBeInTheDocument();
    expect(mockRemove).not.toHaveBeenCalled();

    await fireEvent.click(screen.getByRole('button', { name: 'Confirm remove authorized entry' }));
    await waitFor(() => {
      expect(mockRemove).toHaveBeenCalledWith('user', 'alice', 'laptop');
    });
    await waitFor(() => expect(mockList).toHaveBeenCalledTimes(2));
  });

  it('formats a line through emit', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.input(screen.getByLabelText(/^Public key/), { target: { value: 'ssh-ed25519 AAAAnew' } });
    await fireEvent.input(screen.getByLabelText(/^Line options/), { target: { value: 'restrict' } });
    await fireEvent.input(screen.getByLabelText(/^Line comment/), { target: { value: 'note' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Format line' }));

    await waitFor(() => {
      expect(mockEmit).toHaveBeenCalledWith({
        public_key: 'ssh-ed25519 AAAAnew', options: ['restrict'], comment: 'note',
      });
      expect(screen.getByLabelText('Formatted line')).toHaveTextContent('restrict ssh-ed25519 AAAAnew note');
    });
  });

  it('refuses to format an empty public key', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'Format line' }));
    expect(screen.getByText('Public key is required')).toBeInTheDocument();
    expect(mockEmit).not.toHaveBeenCalled();
  });

  it('checks a fingerprint through verify and shows the entry', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.input(screen.getByLabelText(/^Fingerprint/), { target: { value: 'SHA256:abc' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Check fingerprint' }));

    await waitFor(() => {
      expect(mockVerify).toHaveBeenCalledWith({ scope: 'user', owner: 'alice', fingerprint: 'SHA256:abc' });
      const result = screen.getByLabelText('Fingerprint check result');
      expect(result).toHaveTextContent('Matches laptop');
      expect(result).toHaveTextContent('ssh-ed25519 AAAAlaptop alice@laptop');
    });
  });

  it('shows "No authorized entry matches" on SSHNotFoundError', async () => {
    mockVerify.mockRejectedValue(new SSHNotFoundError('no match'));
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.input(screen.getByLabelText(/^Fingerprint/), { target: { value: 'SHA256:zzz' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Check fingerprint' }));
    await waitFor(() => {
      expect(screen.getByLabelText('Fingerprint check result')).toHaveTextContent('No authorized entry matches');
    });
  });

  it('shows another verify error inline', async () => {
    mockVerify.mockRejectedValue(new Error('verify exploded'));
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.input(screen.getByLabelText(/^Fingerprint/), { target: { value: 'SHA256:zzz' } });
    await fireEvent.click(screen.getByRole('button', { name: 'Check fingerprint' }));
    await waitFor(() => {
      expect(screen.getByText('verify exploded')).toBeInTheDocument();
    });
  });

  it('refuses an empty fingerprint', async () => {
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => screen.getByText('deploy'));

    await fireEvent.click(screen.getByRole('button', { name: 'Check fingerprint' }));
    expect(screen.getByText('Fingerprint is required')).toBeInTheDocument();
    expect(mockVerify).not.toHaveBeenCalled();
  });

  it('shows the not-available state when the client has no ssh', async () => {
    mockGetSDKClient.mockResolvedValue({ ssh: null });
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => {
      expect(screen.getByText(/Authorized keys are not available/)).toBeInTheDocument();
    });
    expect(screen.queryByRole('button', { name: 'Format line' })).not.toBeInTheDocument();
  });

  it('shows the not-available state on SSHUnavailableError', async () => {
    mockList.mockRejectedValue(new SSHUnavailableError('xkms not configured'));
    render(AuthorizedKeysPanel, { props: { scope: 'user', owner: 'alice' } });
    await waitFor(() => {
      expect(screen.getByText(/Authorized keys are not available/)).toBeInTheDocument();
    });
    expect(screen.queryByRole('button', { name: 'Add authorized entry' })).not.toBeInTheDocument();
    expect(mockList).toHaveBeenCalledTimes(1);
  });
});
