/**
 * KMSView.test.ts
 *
 * Verifies that KMSView:
 *   - Renders the "KMS" page heading.
 *   - Renders key rows from the API.
 *   - Shows the "+ Create Key" button.
 *   - Opens the Create Key dialog when clicked.
 *   - Calls sdk.kms.createKey with the correct shape.
 *   - Shows an error alert when listKeys fails.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import type { KMSKey } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockKeys: KMSKey[] = [
  {
    id: 'key-abc123',
    name: 'app-encryption-key',
    description: 'Primary application encryption key',
    algorithm: 'AES-256-GCM',
    key_size: 256,
    status: 'active',
    created_at: '2025-01-01T00:00:00Z',
    updated_at: '2025-01-01T00:00:00Z',
    exportable: false,
    tags: {},
  },
  {
    id: 'key-def456',
    name: 'signing-key',
    description: 'JWT signing key',
    algorithm: 'RSA-2048',
    key_size: 2048,
    status: 'active',
    created_at: '2025-01-02T00:00:00Z',
    updated_at: '2025-01-02T00:00:00Z',
    exportable: false,
    tags: {},
  },
];

// ---------------------------------------------------------------------------
// Hoisted mocks (safe to reference inside vi.mock factories)
// ---------------------------------------------------------------------------

const { mockCreateKey, mockRotateKey, mockDeleteKey, mockGetSDKClient } = vi.hoisted(() => ({
  mockCreateKey: vi.fn(),
  mockRotateKey: vi.fn(),
  mockDeleteKey: vi.fn().mockResolvedValue(undefined),
  mockGetSDKClient: vi.fn(),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: mockGetSDKClient,
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import KMSView from '../src/views/security/KMSView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('KMSView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCreateKey.mockResolvedValue({ key: mockKeys[0], timestamp: 0 });
    mockRotateKey.mockResolvedValue({ key: mockKeys[0], old_key_id: 'old', timestamp: 0 });
    mockGetSDKClient.mockResolvedValue({
      kms: {
        listKeys: vi.fn().mockResolvedValue({ keys: mockKeys, count: 2 }),
        createKey: mockCreateKey,
        rotateKey: mockRotateKey,
        deleteKey: mockDeleteKey,
      },
    });
  });

  it('renders the "KMS" page heading', async () => {
    render(KMSView);
    await waitFor(() => {
      expect(screen.getByText('KMS')).toBeInTheDocument();
    });
  });

  it('renders key rows returned by the API', async () => {
    render(KMSView);
    await waitFor(() => {
      expect(screen.getByText('app-encryption-key')).toBeInTheDocument();
      expect(screen.getByText('signing-key')).toBeInTheDocument();
    });
  });

  it('shows "AES-256-GCM" algorithm in the table', async () => {
    render(KMSView);
    await waitFor(() => {
      expect(screen.getByText('AES-256-GCM')).toBeInTheDocument();
    });
  });

  it('renders a "+ Create Key" button', async () => {
    render(KMSView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Create key/i })).toBeInTheDocument();
    });
  });

  it('opens the Create Key dialog when the button is clicked', async () => {
    render(KMSView);
    await waitFor(() => screen.getByRole('button', { name: /Create key/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create key/i }));
    expect(screen.getByRole('dialog', { name: /Create key dialog/i })).toBeInTheDocument();
  });

  it('calls kms.createKey with the correct shape on submit', async () => {
    render(KMSView);
    await waitFor(() => screen.getByRole('button', { name: /Create key/i }));
    await fireEvent.click(screen.getByRole('button', { name: /Create key/i }));

    // The dialog should have algorithm selector defaulting to AES-256-GCM.
    const createBtn = screen.getByRole('button', { name: /^Create$/ });
    await fireEvent.click(createBtn);

    await waitFor(() => {
      expect(mockCreateKey).toHaveBeenCalledWith(
        expect.objectContaining({
          key_attributes: expect.objectContaining({ algorithm: 'AES-256-GCM' }),
        }),
      );
    });
  });

  it('shows Rotate and Delete buttons per row', async () => {
    render(KMSView);
    await waitFor(() => {
      const rotateButtons = screen.getAllByRole('button', { name: /Rotate key/i });
      expect(rotateButtons.length).toBe(mockKeys.length);
      const deleteButtons = screen.getAllByRole('button', { name: /Delete key/i });
      expect(deleteButtons.length).toBe(mockKeys.length);
    });
  });

  it('wraps each row action in a daisyUI tooltip (#34)', async () => {
    const { container } = render(KMSView);
    await waitFor(() => {
      // Each per-row action is wrapped in `<div class="tooltip ...">` so
      // the action label is visible on hover. Five actions per row × N
      // rows means we should see at least 5 tooltips for one rendered row.
      const tooltips = container.querySelectorAll('div.tooltip');
      expect(tooltips.length).toBeGreaterThanOrEqual(5 * mockKeys.length);
    });
  });

  it('annotates row actions with title attributes for browser tooltips (#34)', async () => {
    const { container } = render(KMSView);
    await waitFor(() => {
      const titledButtons = container.querySelectorAll(
        'div.tooltip button[title]',
      );
      // At least the 5 row actions × N rows must each declare a `title`.
      expect(titledButtons.length).toBeGreaterThanOrEqual(5 * mockKeys.length);
    });
  });

  it('shows an error alert when listKeys fails', async () => {
    mockGetSDKClient.mockResolvedValueOnce({
      kms: {
        listKeys: vi.fn().mockRejectedValue(new Error('KMS unavailable')),
        createKey: vi.fn(),
        rotateKey: vi.fn(),
        deleteKey: vi.fn(),
      },
    });
    render(KMSView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('KMS unavailable');
    });
  });

  // -------------------------------------------------------------------------
  // Bug E regression: system-managed keys (algorithm='', created_at='') must
  // be filtered out and never rendered in the KMS table.
  // -------------------------------------------------------------------------

  it('filters out system-managed keys (algorithm="", created_at="")', async () => {
    const systemKey: KMSKey = {
      id: 'QuicBaaS Root CA',
      name: 'QuicBaaS Root CA',
      description: '',
      algorithm: '',          // empty = system key
      key_size: 0,
      status: 'enabled' as KMSKey['status'],
      created_at: '',         // empty = Go zero-time
      updated_at: '',
      exportable: false,
      tags: {},
    };
    const tenantKey: KMSKey = {
      id: 'quicbaas-tenant-dek-kek',
      name: 'quicbaas-tenant-dek-kek',
      description: '',
      algorithm: 'AES-256-GCM',
      key_size: 256,
      status: 'enabled' as KMSKey['status'],
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
      exportable: false,
      tags: {},
    };
    mockGetSDKClient.mockResolvedValueOnce({
      kms: {
        listKeys: vi.fn().mockResolvedValue({ keys: [systemKey, tenantKey], count: 2 }),
        createKey: vi.fn(),
        rotateKey: vi.fn(),
        deleteKey: vi.fn(),
      },
    });

    render(KMSView);

    await waitFor(() => {
      // Tenant key must appear.
      expect(screen.getByText('quicbaas-tenant-dek-kek')).toBeInTheDocument();
    });

    // System key must NOT appear.
    expect(screen.queryByText('QuicBaaS Root CA')).not.toBeInTheDocument();
  });

  it('does not render any key row with an empty algorithm cell', async () => {
    const keysWithSystem: KMSKey[] = [
      ...mockKeys,
      {
        id: 'sys-1',
        name: '127.0.0.1',
        description: '',
        algorithm: '',
        key_size: 0,
        status: 'enabled' as KMSKey['status'],
        created_at: '',
        updated_at: '',
        exportable: false,
        tags: {},
      },
    ];
    mockGetSDKClient.mockResolvedValueOnce({
      kms: {
        listKeys: vi.fn().mockResolvedValue({ keys: keysWithSystem, count: keysWithSystem.length }),
        createKey: vi.fn(),
        rotateKey: vi.fn(),
        deleteKey: vi.fn(),
      },
    });

    render(KMSView);

    await waitFor(() => {
      expect(screen.getByText('app-encryption-key')).toBeInTheDocument();
    });

    // The system key row must not be present.
    expect(screen.queryByText('127.0.0.1')).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Bug #88: Key Versions dialog – created_at must not display as 1970
  // -------------------------------------------------------------------------

  it('renders version created_at as a valid post-2000 date, not 1970 (bug #88)', async () => {
    // The SDK now converts unix-second timestamps to ISO strings before
    // returning them.  Simulate the SDK already having done that conversion:
    // 2025-05-03T00:00:00.000Z (the year must come back from new Date() correctly).
    const mockListKeyVersions = vi.fn().mockResolvedValue([
      { version: 1, status: 'ENABLED', created_at: '2025-05-03T00:00:00.000Z' },
    ]);
    mockGetSDKClient.mockResolvedValue({
      kms: {
        listKeys: vi.fn().mockResolvedValue({ keys: mockKeys, count: 2 }),
        createKey: mockCreateKey,
        rotateKey: mockRotateKey,
        deleteKey: mockDeleteKey,
        listKeyVersions: mockListKeyVersions,
        updateKeyStatus: vi.fn(),
        setDefaultKey: vi.fn(),
      },
    });

    render(KMSView);
    // Wait for keys to load, then click the first "View versions" button.
    await waitFor(() => screen.getByText('app-encryption-key'));
    const eyeButtons = screen.getAllByRole('button', { name: /View versions/i });
    await fireEvent.click(eyeButtons[0]);

    // The dialog should open and show the version date – must include "2025".
    await waitFor(() => {
      const cells = screen.getAllByText(/2025/);
      expect(cells.length).toBeGreaterThan(0);
    });

    // Must NOT contain a year-1970 date string anywhere in the dialog.
    expect(screen.queryByText(/1970/)).not.toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // Bug #89: Key Versions dialog – "enabled" badge must be badge-success
  // -------------------------------------------------------------------------

  it('renders the version status badge with badge-success for "ENABLED" status (bug #89)', async () => {
    const mockListKeyVersions = vi.fn().mockResolvedValue([
      { version: 1, status: 'ENABLED', created_at: '2025-05-03T00:00:00.000Z' },
    ]);
    mockGetSDKClient.mockResolvedValue({
      kms: {
        listKeys: vi.fn().mockResolvedValue({ keys: mockKeys, count: 2 }),
        createKey: mockCreateKey,
        rotateKey: mockRotateKey,
        deleteKey: mockDeleteKey,
        listKeyVersions: mockListKeyVersions,
        updateKeyStatus: vi.fn(),
        setDefaultKey: vi.fn(),
      },
    });

    const { container } = render(KMSView);
    await waitFor(() => screen.getByText('app-encryption-key'));
    const eyeButtons = screen.getAllByRole('button', { name: /View versions/i });
    await fireEvent.click(eyeButtons[0]);

    // Wait for the dialog to show the version row.
    const badge = await waitFor(() => screen.getByText('ENABLED'));

    // The badge wrapping "ENABLED" must itself carry badge-success (green), not
    // badge-warning (yellow). Assert on that specific element rather than the
    // first .badge.badge-success in the container (which is a key-status badge
    // in the table behind the dialog).
    expect(badge.className).toContain('badge');
    expect(badge.className).toContain('badge-success');
    expect(badge.textContent).toBe('ENABLED');
  });
});
