/**
 * ObjectsView.test.ts
 *
 * Verifies that ObjectsView lists objects, shows the upload dialog,
 * calls download and delete operations on the SDK client.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/svelte';
import { APIError } from '@quicbaas/sdk';

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

const mockObjects = [
  { key: 'uploads/report.pdf',  size: 204800, content_type: 'application/pdf', created_at: '2025-01-15T10:00:00Z' },
  { key: 'uploads/avatar.png',  size: 51200,  content_type: 'image/png',       created_at: '2025-01-15T09:00:00Z' },
];

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    objects: {
      listObjects:  vi.fn().mockResolvedValue({ objects: [] }),
      getObject:    vi.fn().mockResolvedValue(new Blob(['data'])),
      putObject:    vi.fn().mockResolvedValue(undefined),
      deleteObject: vi.fn().mockResolvedValue(undefined),
    },
  }),
}));

vi.mock('../src/stores/toast.svelte.ts', () => ({
  showToast: vi.fn(),
}));

import ObjectsView from '../src/views/data/ObjectsView.svelte';
import { getSDKClient } from '../src/stores/sdkClient.ts';

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ObjectsView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(getSDKClient).mockResolvedValue({
      objects: {
        listObjects:  vi.fn().mockResolvedValue({ objects: mockObjects }),
        getObject:    vi.fn().mockResolvedValue(new Blob(['data'])),
        putObject:    vi.fn().mockResolvedValue(undefined),
        deleteObject: vi.fn().mockResolvedValue(undefined),
      },
    } as never);
  });

  it('renders the page heading "Objects"', async () => {
    render(ObjectsView);
    await waitFor(() => {
      expect(screen.getByText('Objects')).toBeInTheDocument();
    });
  });

  it('renders object keys from the list', async () => {
    render(ObjectsView);
    await waitFor(() => {
      expect(screen.getByText('uploads/report.pdf')).toBeInTheDocument();
      expect(screen.getByText('uploads/avatar.png')).toBeInTheDocument();
    });
  });

  it('shows "Upload" button', async () => {
    render(ObjectsView);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /^Upload$/i })).toBeInTheDocument();
    });
  });

  it('opens the upload dialog when "Upload" is clicked', async () => {
    render(ObjectsView);
    await waitFor(() => screen.getByRole('button', { name: /^Upload$/i }));
    await fireEvent.click(screen.getByRole('button', { name: /^Upload$/i }));
    expect(screen.getByText('Upload Object')).toBeInTheDocument();
  });

  it('shows Download button for each object row', async () => {
    render(ObjectsView);
    await waitFor(() => {
      const downloadBtns = screen.getAllByRole('button', { name: /Download/i });
      expect(downloadBtns).toHaveLength(mockObjects.length);
    });
  });

  it('shows Delete button for each object row', async () => {
    render(ObjectsView);
    await waitFor(() => {
      const deleteBtns = screen.getAllByRole('button', { name: /Delete/i });
      expect(deleteBtns).toHaveLength(mockObjects.length);
    });
  });

  it('shows empty state when no objects exist', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockResolvedValueOnce({ objects: [] }),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);
    render(ObjectsView);
    await waitFor(() => {
      // EmptyState renders a title with "No objects yet" when the list is empty.
      expect(screen.getByText(/No objects yet/i)).toBeInTheDocument();
    });
  });

  // Bug D regression: when listObjects fails, show the error alert block.
  // The empty-state ("No objects yet") must NOT render simultaneously.

  it('shows an inline error alert (not toast) when listObjects fails', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockRejectedValueOnce(new Error('object storage service not available')),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);
    render(ObjectsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
      expect(screen.getByRole('alert')).toHaveTextContent('object storage service not available');
    });
  });

  it('does NOT show empty state when listObjects errors', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockRejectedValueOnce(new Error('service unavailable')),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);
    render(ObjectsView);
    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument();
    });
    // Empty state must not coexist with the error alert.
    expect(screen.queryByText(/No objects yet/i)).not.toBeInTheDocument();
  });

  it('does NOT show error alert when service is available but empty', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockResolvedValueOnce({ objects: [] }),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);
    render(ObjectsView);
    await waitFor(() => {
      expect(screen.getByText(/No objects yet/i)).toBeInTheDocument();
    });
    // Error alert must not appear alongside the empty state.
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  // ---------------------------------------------------------------------------
  // Issue #76: subsystem-disabled state must render info banner, not error
  // ---------------------------------------------------------------------------

  it('shows blue info banner (not error red) when subsystem returns 503 not available (#76)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockRejectedValueOnce(new APIError(503, 'object storage service not available')),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);

    render(ObjectsView);

    const alert = await screen.findByRole('alert');
    expect(alert.className).toContain('alert-info');
    expect(alert.className).not.toContain('alert-error');
    expect(alert.textContent).toMatch(/QUICBAAS_OBJECTSTORAGE_ENABLED/);
    expect(alert.textContent).toMatch(/object_storage\.enabled/);
  });

  it('shows blue info banner when subsystem returns 500 not configured (#76)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockRejectedValueOnce(new APIError(500, 'object storage service not configured')),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);

    render(ObjectsView);

    const alert = await screen.findByRole('alert');
    expect(alert.className).toContain('alert-info');
    expect(alert.className).not.toContain('alert-error');
  });

  it('Upload button is disabled when subsystem is not enabled (#76)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockRejectedValueOnce(new APIError(503, 'object storage service not available')),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);

    render(ObjectsView);

    await screen.findByRole('alert');
    const uploadBtn = screen.getByRole('button', { name: /Upload/i });
    expect(uploadBtn).toBeDisabled();
  });

  it('still renders a red error banner for a genuine HTTP 500 server error (#76)', async () => {
    vi.mocked(getSDKClient).mockResolvedValueOnce({
      objects: {
        listObjects:  vi.fn().mockRejectedValueOnce(new APIError(500, 'internal server error')),
        getObject:    vi.fn(),
        putObject:    vi.fn(),
        deleteObject: vi.fn(),
      },
    } as never);

    render(ObjectsView);

    const alert = await screen.findByRole('alert');
    expect(alert.className).toContain('alert-error');
    expect(alert.className).not.toContain('alert-info');
    expect(screen.getByRole('button', { name: /Retry/i })).toBeInTheDocument();
  });
});
