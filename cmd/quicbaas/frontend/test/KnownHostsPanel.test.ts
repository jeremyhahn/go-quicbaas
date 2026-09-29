/**
 * KnownHostsPanel.test.ts
 *
 * Verifies that KnownHostsPanel:
 *   - Renders collapsed by default with no pins visible.
 *   - Expands on click and shows loading then the pin list.
 *   - "Add Pin" button opens the add-pin dialog.
 *   - Submitting the add-pin form calls sdk.ssh.knownHosts.add.
 *   - "Remove" button opens the confirmation dialog.
 *   - Confirming removal calls sdk.ssh.knownHosts.remove.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { SSHPin } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockPins: SSHPin[] = [
  {
    host: 'github.com',
    fingerprint: 'SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s',
    algorithm: 'ecdsa-sha2-nistp256',
    scope: 'user',
    owner: 'default',
    source: 'manual',
    added_at: '2025-01-01T00:00:00Z',
  },
  {
    host: 'gitlab.com',
    fingerprint: 'SHA256:ROQFvPThGrW4RuWLoL9tq9I9zJ42fK09/APHLOFZwfM',
    algorithm: 'ecdsa-sha2-nistp256',
    scope: 'user',
    owner: 'default',
    source: 'manual',
    added_at: '2025-01-02T00:00:00Z',
  },
];

// ---------------------------------------------------------------------------
// Hoisted mocks
// ---------------------------------------------------------------------------

const { mockListPins, mockAddPin, mockRemovePin, mockGetSDKClient } = vi.hoisted(() => ({
  mockListPins: vi.fn(),
  mockAddPin: vi.fn(),
  mockRemovePin: vi.fn(),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import KnownHostsPanel from '../src/views/security/ssh/KnownHostsPanel.svelte';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('KnownHostsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListPins.mockResolvedValue({ items: mockPins });
    mockAddPin.mockResolvedValue(undefined);
    mockRemovePin.mockResolvedValue(undefined);
    mockGetSDKClient.mockResolvedValue({
      ssh: {
        knownHosts: {
          list: mockListPins,
          add: mockAddPin,
          remove: mockRemovePin,
          verify: vi.fn(),
        },
      },
    });
  });

  it('renders the "Known Hosts" collapse title', () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    expect(screen.getByText('Known Hosts')).toBeInTheDocument();
  });

  it('does not fetch pins before the panel is opened', () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    expect(mockListPins).not.toHaveBeenCalled();
  });

  it('fetches and renders pins after the panel is opened', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });

    await fireEvent.click(screen.getByText('Known Hosts'));

    await waitFor(() => {
      expect(screen.getByText('github.com')).toBeInTheDocument();
      expect(screen.getByText('gitlab.com')).toBeInTheDocument();
    });
  });

  it('shows "Add Pin" button after opening', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Add host pin/i })).toBeInTheDocument();
    });
  });

  it('opens the add-pin dialog when "Add Pin" is clicked', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));

    await waitFor(() => screen.getByRole('button', { name: /Add host pin/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Add host pin/i }));

    expect(screen.getByRole('dialog', { name: /Add host pin dialog/i })).toBeInTheDocument();
  });

  it('calls knownHosts.add with correct args on add-pin form submit', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));

    // The "+ Add Pin" button in the panel.
    await waitFor(() => screen.getByRole('button', { name: /Add host pin/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Add host pin/i }));

    // Wait for the add-pin dialog to mount.
    await waitFor(() => {
      expect(screen.getByRole('dialog', { name: /Add host pin dialog/i })).toBeInTheDocument();
    });

    const hostInput = document.getElementById('add-pin-host') as HTMLInputElement;
    const keyInput = document.getElementById('add-pin-key') as HTMLTextAreaElement;
    await fireEvent.input(hostInput, { target: { value: 'bitbucket.org' } });
    await fireEvent.input(keyInput, { target: { value: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample' } });

    // Use the dialog cancel button's sibling — target by the dialog element.
    const dialog = screen.getByRole('dialog', { name: /Add host pin dialog/i });
    const submitBtn = dialog.querySelector('button[aria-label="Add host pin"]') as HTMLButtonElement;
    await fireEvent.click(submitBtn);

    await waitFor(() => {
      expect(mockAddPin).toHaveBeenCalledWith(
        expect.objectContaining({
          host: 'bitbucket.org',
          public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample',
          scope: 'user',
        }),
      );
    });
  });

  it('refuses an add-pin form with no host key', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));
    await waitFor(() => screen.getByRole('button', { name: /Add host pin/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Add host pin/i }));

    const hostInput = document.getElementById('add-pin-host') as HTMLInputElement;
    await fireEvent.input(hostInput, { target: { value: 'bitbucket.org' } });
    const dialog = screen.getByRole('dialog', { name: /Add host pin dialog/i });
    await fireEvent.click(dialog.querySelector('button[aria-label="Add host pin"]') as HTMLButtonElement);

    await waitFor(() => {
      expect(screen.getByText('Host key is required')).toBeInTheDocument();
    });
    expect(mockAddPin).not.toHaveBeenCalled();
  });

  it('shows Remove button for each pin', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));

    await waitFor(() => {
      const removeButtons = screen.getAllByRole('button', { name: /Remove host pin/i });
      expect(removeButtons).toHaveLength(mockPins.length);
    });
  });

  it('opens remove confirmation dialog on Remove click', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));

    await waitFor(() => screen.getAllByRole('button', { name: /Remove host pin/i }));
    await fireEvent.click(screen.getAllByRole('button', { name: /Remove host pin/i })[0]);

    expect(
      screen.getByRole('dialog', { name: /Remove host pin confirmation/i }),
    ).toBeInTheDocument();
  });

  it('calls knownHosts.remove with correct host on confirmation', async () => {
    render(KnownHostsPanel, { props: { scope: 'user', owner: 'default' } });
    await fireEvent.click(screen.getByText('Known Hosts'));

    await waitFor(() => screen.getAllByRole('button', { name: /Remove host pin/i }));
    await fireEvent.click(screen.getAllByRole('button', { name: /Remove host pin/i })[0]);

    await fireEvent.click(screen.getByRole('button', { name: /^Remove$/ }));

    await waitFor(() => {
      expect(mockRemovePin).toHaveBeenCalledWith(
        'github.com',
        expect.objectContaining({ scope: 'user' }),
      );
    });
  });
});
